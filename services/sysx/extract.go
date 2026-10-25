package sysx

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ============================================================
// 归档解包（zip / tar.gz 都用标准库，天然跨平台）
//
// 安全要点：归档里的条目名一律经 safeJoin 校验，挡住 zip-slip
// （../ 或绝对路径把文件写到目标目录之外）。
// ============================================================

// extractArchive 按扩展名解包到 dst（dst 必须为空或不存在）。
func extractArchive(archive, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("创建解包目录失败: %w", err)
	}
	switch name := strings.ToLower(archive); {
	case strings.HasSuffix(name, ".zip"):
		return extractZip(archive, dst)
	case strings.HasSuffix(name, ".tar.gz"), strings.HasSuffix(name, ".tgz"):
		return extractTarGz(archive, dst)
	default:
		return fmt.Errorf("不支持的归档格式（仅支持 .zip / .tar.gz / .tgz）: %s", filepath.Base(archive))
	}
}

// extractZip 解 zip 包。
func extractZip(archive, dst string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("打开 zip 失败: %w", err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		target, err := safeJoin(dst, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("读取 %s 失败: %w", f.Name, err)
		}
		// 保留归档里的权限位（unix 包里 nginx/mysql 的可执行位靠它）
		mode := f.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		err = writeReader(target, rc, mode)
		_ = rc.Close()
		if err != nil {
			return fmt.Errorf("解包 %s 失败: %w", f.Name, err)
		}
	}
	return nil
}

// extractTarGz 解 tar.gz 包。
func extractTarGz(archive, dst string) error {
	f, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("打开归档失败: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip 解析失败: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("读取 tar 失败: %w", err)
		}
		target, err := safeJoin(dst, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(hdr.Mode).Perm()
			if mode == 0 {
				mode = 0o644
			}
			if err := writeReader(target, tr, mode); err != nil {
				return fmt.Errorf("解包 %s 失败: %w", hdr.Name, err)
			}
		case tar.TypeSymlink:
			// 只允许指向包内相对路径的软链（php-fpm 这类发行包会带一些）
			if filepath.IsAbs(hdr.Linkname) || strings.Contains(hdr.Linkname, "..") {
				continue
			}
			_ = os.Symlink(hdr.Linkname, target)
		default:
			// 设备文件、fifo 等一律跳过
		}
	}
}

// writeReader 写文件（截断 + 指定权限）。
func writeReader(target string, r io.Reader, perm os.FileMode) error {
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// 显式 chmod：umask 会削弱 perm，可执行位必须落实
	return os.Chmod(target, perm)
}

// safeJoin 把归档内的相对路径安全地拼到 dst 下（拦 zip-slip）。
//
// 绝对路径一律拒绝而不是剥掉前导 /：带这类条目的包基本可以判定为恶意或损坏，
// 直接报错让安装失败，比静默改写路径安全（剥前缀虽不越界，但会掩盖问题）。
//
// 安全判断统一按 / 分隔的名字做，事先把 \ 也归一成 /：
//   - 归档条目规范上总是 / 分隔，但 Windows 打包工具常写成 \；
//   - Linux 上 \ 是合法文件名字符，不归一的话 `..\evil` 会被当成普通文件名
//     放行——不越界但两端行为不一致，同一份包会有两种结果。
//
// 归一只用于判断；落盘仍用原始名字，避免把 POSIX 下合法的 a\b 改写成 a/b。
//
// 判断全程用 path 包（POSIX 语义）而不是 filepath：filepath 的语义随 GOOS 变，
// 那样同一份断言在 Windows 上通过不等于 Linux 也通过，测试就失去意义了。
func safeJoin(dst, name string) (string, error) {
	norm := strings.ReplaceAll(name, `\`, "/")
	if path.IsAbs(norm) || filepath.VolumeName(filepath.FromSlash(norm)) != "" {
		return "", fmt.Errorf("归档内含绝对路径: %s", name)
	}
	probe := path.Clean(norm)
	if probe == ".." || strings.HasPrefix(probe, "../") {
		return "", fmt.Errorf("归档内含非法路径: %s", name)
	}

	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || clean == "" {
		return dst, nil
	}
	target := filepath.Join(dst, clean)
	rel, err := filepath.Rel(dst, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("归档内含越界路径: %s", name)
	}
	return target, nil
}
