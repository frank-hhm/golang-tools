package archive

// 在线解压（P2）：只列包内条目、按需提取单个条目到临时目录，不整包解盘。
//
// 与 extract.go 的整包解包分开：这里服务查看器「压缩包」kind——用户点开一个
// 归档只想看里面有什么、或取某一个文件预览，不该把整包铺开落盘。
// 支持的格式与 sysx.ExtractArchive 完全一致（.zip / .tar.gz / .tgz），
// rar / 7z 等标准库读不了的格式不在此列（前端按 binary 处理并提示走本机程序）。
//
// 全程标准库、零 cgo、跨平台：条目路径一律经安全拼接挡住 zip-slip。

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"Tools/services/sysx"
)

// Entry 归档内的一个条目（文件或目录）。
type Entry struct {
	Name     string `json:"name"`     // 包内原始路径名（可能以 / 或 \ 分隔）
	DispPath string `json:"dispPath"` // 归一为 / 分隔的展示路径
	IsDir    bool   `json:"isDir"`
	Size     int64  `json:"size"`     // 解压后字节数；目录为 0
	Modified int64  `json:"modified"` // 修改时间（unix 毫秒）
}

// List 列出归档内的全部条目（按展示路径字典序稳定排序）。
// 仅支持 zip / tar.gz / tgz；其它格式返回带格式的明确错误。
func List(archivePath string) ([]Entry, error) {
	switch FormatFor(archivePath) {
	case sysx.ArchiveZip:
		return listZip(archivePath)
	case sysx.ArchiveTarGz:
		return listTarGz(archivePath)
	default:
		return nil, fmt.Errorf("不支持的归档格式（仅可列举 .zip / .tar.gz / .tgz）: %s", filepath.Base(archivePath))
	}
}

func listZip(archivePath string) ([]Entry, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, fmt.Errorf("打开 zip 失败: %w", err)
	}
	defer zr.Close()
	out := make([]Entry, 0, len(zr.File))
	for _, f := range zr.File {
		isDir := f.FileInfo().IsDir()
		var size int64
		if !isDir {
			size = int64(f.UncompressedSize64)
		}
		out = append(out, Entry{
			Name:     f.Name,
			DispPath: normalizeEntry(f.Name),
			IsDir:    isDir,
			Size:     size,
			Modified: f.Modified.UnixMilli(),
		})
	}
	sortEntries(out)
	return out, nil
}

func listTarGz(archivePath string) ([]Entry, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("打开归档失败: %w", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("gzip 解析失败: %w", err)
	}
	defer gz.Close()
	var out []Entry
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取 tar 失败: %w", err)
		}
		isDir := hdr.Typeflag == tar.TypeDir
		var size int64
		if !isDir {
			size = hdr.Size
		}
		out = append(out, Entry{
			Name:     hdr.Name,
			DispPath: normalizeEntry(hdr.Name),
			IsDir:    isDir,
			Size:     size,
			Modified: hdr.ModTime.UnixMilli(),
		})
	}
	sortEntries(out)
	return out, nil
}

// ExtractOne 把归档内名为 entryName 的单个文件提取到 destDir 下，返回落盘绝对路径。
//
// 只取请求的那一个条目（不解整包）；目录条目与越界名一律拒绝。
// destDir 由调用方给（通常是 appdirs 下的临时目录），本函数只保证不越出 destDir。
func ExtractOne(archivePath, entryName, destDir string) (string, error) {
	target, err := safeEntryJoin(destDir, entryName)
	if err != nil {
		return "", err
	}
	switch FormatFor(archivePath) {
	case sysx.ArchiveZip:
		if err := extractOneZip(archivePath, entryName, target); err != nil {
			return "", err
		}
	case sysx.ArchiveTarGz:
		if err := extractOneTarGz(archivePath, entryName, target); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("不支持的归档格式（仅可提取 .zip / .tar.gz / .tgz）: %s", filepath.Base(archivePath))
	}
	return target, nil
}

func extractOneZip(archivePath, entryName, target string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("打开 zip 失败: %w", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != entryName {
			continue
		}
		if f.FileInfo().IsDir() {
			return fmt.Errorf("目标是目录，无法单文件预览: %s", entryName)
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("读取 %s 失败: %w", entryName, err)
		}
		defer rc.Close()
		return writeEntryFile(rc, target)
	}
	return fmt.Errorf("归档内不存在该条目: %s", entryName)
}

func extractOneTarGz(archivePath, entryName, target string) error {
	f, err := os.Open(archivePath)
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
			break
		}
		if err != nil {
			return fmt.Errorf("读取 tar 失败: %w", err)
		}
		if hdr.Name != entryName {
			continue
		}
		if hdr.Typeflag == tar.TypeDir {
			return fmt.Errorf("目标是目录，无法单文件预览: %s", entryName)
		}
		if hdr.Typeflag != tar.TypeReg {
			return fmt.Errorf("不支持的条目类型（软链/设备文件等）: %s", entryName)
		}
		return writeEntryFile(tr, target)
	}
	return fmt.Errorf("归档内不存在该条目: %s", entryName)
}

// writeEntryFile 把 reader 内容写到 target（先建父目录，覆盖同名）。
func writeEntryFile(r io.Reader, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// normalizeEntry 把包内路径统一成 / 分隔并去掉末尾斜杠，用于展示与排序。
func normalizeEntry(name string) string {
	n := strings.ReplaceAll(name, `\`, "/")
	return strings.TrimRight(n, "/")
}

func sortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir // 目录在前
		}
		return entries[i].DispPath < entries[j].DispPath
	})
}

// safeEntryJoin 把包内相对名安全拼到 destDir 下，挡住 zip-slip（绝对路径 / .. 越界）。
// 判断按 POSIX 语义做（\ 先归一成 /），落盘用归一后的名字，跨平台行为一致。
func safeEntryJoin(destDir, name string) (string, error) {
	norm := strings.ReplaceAll(name, `\`, "/")
	if path.IsAbs(norm) || filepath.VolumeName(filepath.FromSlash(norm)) != "" {
		return "", fmt.Errorf("归档内含绝对路径: %s", name)
	}
	probe := path.Clean(norm)
	if probe == "." || probe == ".." || strings.HasPrefix(probe, "../") {
		return "", fmt.Errorf("归档内含非法路径: %s", name)
	}
	target := filepath.Join(destDir, filepath.FromSlash(probe))
	rel, err := filepath.Rel(destDir, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("归档内含越界路径: %s", name)
	}
	return target, nil
}
