package sysx

import (
	"archive/tar"
	"archive/zip"
	"compress/flate"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ============================================================
// 归档压缩（zip / tar.gz 都用标准库，与 extract.go 对称）
//
// 落盘口径：一律先写 dst+".part"，全部成功后 rename 落名——
// 中途取消或出错不会留下一个"看着完整、实际缺条目"的压缩包，
// 失败路径上把 .part 删掉。
//
// 条目名统一用 / 分隔且不带前导 /，与解包侧 safeJoin 的假设一致；
// 两边对称才能做"压完再解回来逐字节比对"的往返测试。
// ============================================================

// 归档格式标识。对外（配置 / 前端 / 命令行）就用这两个短名，
// 扩展名映射在 tools/archive 里做，这里只认格式。
const (
	ArchiveZip   = "zip"   // .zip（Deflate）
	ArchiveTarGz = "targz" // .tar.gz（tar + gzip）
)

// 压缩级别：3 档对外，落到标准库的具体数值在这里收口，
// 免得调用方各处写 gzip.BestSpeed 这种实现细节。
const (
	LevelFast   = 1
	LevelNormal = 6
	LevelMax    = 9
)

// CompressStat 一次压缩的写出统计。
type CompressStat struct {
	Files   int      // 写入的文件条目数
	Dirs    int      // 写入的目录条目数（含空目录）
	Bytes   int64    // 计入归档的原始字节（压缩前）
	Skipped []string // 被跳过的条目名：符号链接、读不动的文件
}

// NormalizeLevel 把外部传入的级别规整到 1~9；<=0 用默认值。
func NormalizeLevel(level int) int {
	switch {
	case level <= 0:
		return LevelNormal
	case level > LevelMax:
		return LevelMax
	default:
		return level
	}
}

// CompressArchive 把 roots（文件或目录）压缩到 dst。
//
// format 取 ArchiveZip / ArchiveTarGz；level 取 1~9（内部按格式映射）。
// onProgress 每写完一个条目回调一次"累计原始字节"，可为 nil；它只用于进度显示，
// 返回值里的统计与它无关，所以传 nil 不影响正确性。
//
// ctx 在每条条目开始前检查：取消即中止，删除 .part 并返回 ctx.Err()。
// 单个条目的读取失败不中断整包（跳过并记账），只有"一个文件都没写进去"或
// 底层 I/O 崩了才报错——压缩一个目录时里面有几个被占用的文件是常态。
func CompressArchive(ctx context.Context, dst string, roots []string, format string, level int, onProgress func(written int64)) (CompressStat, error) {
	var st CompressStat
	if len(roots) == 0 {
		return st, fmt.Errorf("没有要压缩的条目")
	}
	switch format {
	case ArchiveZip, ArchiveTarGz:
	default:
		return st, fmt.Errorf("不支持的归档格式: %s（仅 %s / %s）", format, ArchiveZip, ArchiveTarGz)
	}

	// 绝对化的输出路径：用来在遍历中把自己排除掉（压缩整个目录时
	// .part 就在里面，不排除会写进包，还会边写边读）
	outAbs, err := filepath.Abs(dst + ".part")
	if err != nil {
		return st, fmt.Errorf("计算输出路径失败: %w", err)
	}

	f, err := os.OpenFile(dst+".part", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return st, fmt.Errorf("创建输出文件失败: %w", err)
	}
	part := f.Name()

	cw, cerr := newArchiveWriter(f, format, level)
	if cerr != nil {
		_ = f.Close()
		_ = os.Remove(part)
		return st, cerr
	}

	walkErr := walkRoots(roots, cw, &st, outAbs, onProgress, ctx)

	// 收尾顺序固定：先 Close 写索引（tar.gz 要 Flush 两层），再关文件句柄，
	// 出错才删 .part。反过来会把还没落盘的尾部丢掉。
	closeErr := cw.Close()
	fileErr := f.Close()
	if walkErr != nil {
		_ = os.Remove(part)
		return st, walkErr
	}
	if closeErr != nil || fileErr != nil {
		_ = os.Remove(part)
		if closeErr == nil {
			closeErr = fileErr
		}
		return st, fmt.Errorf("归档收尾失败: %w", closeErr)
	}
	if err := os.Rename(dst+".part", dst); err != nil {
		_ = os.Remove(part)
		return st, fmt.Errorf("输出落名失败: %w", err)
	}
	if st.Files == 0 && st.Dirs == 0 {
		_ = os.Remove(dst)
		return st, fmt.Errorf("没有可写入的条目（全部被跳过）")
	}
	return st, nil
}

// walkRoots 遍历每个根条目并交给 writer。
//
// 根条目自己的名字就是包内第一层目录名（文件则直接是文件名），
// 与 7-Zip "添加到压缩包" 的默认行为一致：多选时包内是并列的多个顶层项，
// 而不是把公共父目录整个塞进去。
func walkRoots(roots []string, cw *archiveWriter, st *CompressStat, outAbs string, onProgress func(int64), ctx context.Context) error {
	seen := map[string]bool{} // 同名根条目（两个同名文件夹）在包内会撞名

	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := os.Stat(root)
		if err != nil {
			return fmt.Errorf("无法访问 %s: %w", filepath.Base(root), err)
		}
		base, err := entryName(root)
		if err != nil {
			return err
		}
		for suffix := 2; seen[base]; suffix++ {
			base = uniqueEntryName(base, suffix)
		}
		seen[base] = true

		if !info.IsDir() {
			if err := cw.addFile(base, root, info); err != nil {
				return err
			}
			st.Files++
			st.Bytes += info.Size()
			if onProgress != nil {
				onProgress(st.Bytes)
			}
			continue
		}

		// 目录：先落自己的条目，空目录才不会丢
		if err := cw.addDir(base + "/"); err != nil {
			return err
		}
		st.Dirs++

		var absErr error
		err = filepath.WalkDir(root, func(p string, d os.DirEntry, werr error) error {
			if werr != nil {
				// 目录进不去（权限）就跳过整棵子树并记账；单文件读失败同理
				st.Skipped = append(st.Skipped, filepath.Base(p))
				if d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if err := ctx.Err(); err != nil {
				absErr = err
				return err
			}
			if p == root {
				return nil // 根目录已经在上面落过
			}
			abs, aerr := filepath.Abs(p)
			if aerr == nil && filepath.Clean(abs) == filepath.Clean(outAbs) {
				return nil // 自己的输出文件，绝不打包进去
			}
			rel, rerr := filepath.Rel(root, p)
			if rerr != nil {
				st.Skipped = append(st.Skipped, filepath.Base(p))
				return nil
			}
			name := base + "/" + filepath.ToSlash(rel)

			fi, ierr := d.Info()
			if ierr != nil {
				st.Skipped = append(st.Skipped, rel)
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			switch {
			case d.IsDir():
				if err := cw.addDir(name + "/"); err != nil {
					return err
				}
				st.Dirs++
			case d.Type()&os.ModeSymlink != 0:
				// 不跟随软链：Windows 上跟随有成环风险，zip/tar 的软链语义
				// 又两头不一致，解回来时最容易出问题，直接跳过并告知
				st.Skipped = append(st.Skipped, rel)
			case fi.Mode().IsRegular():
				if err := cw.addFile(name, p, fi); err != nil {
					st.Skipped = append(st.Skipped, rel)
					return nil // 单个文件读不动不该毁掉整包
				}
				st.Files++
				st.Bytes += fi.Size()
				if onProgress != nil {
					onProgress(st.Bytes)
				}
			default:
				// 设备文件、fifo、socket 等一律跳过
				st.Skipped = append(st.Skipped, rel)
			}
			return nil
		})
		if absErr != nil {
			return absErr
		}
		if err != nil {
			return fmt.Errorf("遍历 %s 失败: %w", filepath.Base(root), err)
		}
	}
	return nil
}

// entryName 算出某个路径在包内的顶层名字。
//
// 卷根（C:\）的 Base 带着分隔符与冒号，不能直接当条目名：改成小写字母，
// 既避开"条目名以盘符开头"的非法形态，也让解包后仍认得出是哪块盘。
func entryName(p string) (string, error) {
	clean := filepath.Clean(p)
	vol := filepath.VolumeName(clean)
	rest := clean[len(vol):]
	if vol != "" && strings.Trim(rest, `\/`) == "" {
		name := strings.ToLower(strings.TrimRight(vol, `:\`))
		if name == "" {
			return "", fmt.Errorf("无法为 %s 取名", p)
		}
		return name, nil
	}
	base := filepath.Base(clean)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "", fmt.Errorf("无法为 %s 取名", p)
	}
	return base, nil
}

// uniqueEntryName 给撞名的顶层条目加序号：dir → "dir (2)"，a.txt → "a (2).txt"。
// 保留扩展名，否则 .txt 变成 "a.txt (2)" 这种名字在两头都难看。
func uniqueEntryName(name string, n int) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	return fmt.Sprintf("%s (%d)%s", stem, n, ext)
}

// ============================================================
// 两种格式的写入差异收在这层：对外只有 addDir / addFile / Close 三个动作
// ============================================================

// archiveWriter 是 zip / tar.gz 共用的写出接口面。
type archiveWriter struct {
	z   *zip.Writer
	gz  *gzip.Writer
	tw  *tar.Writer
	f   io.Closer // gzip 层（tar.gz 下 tar 写在 gz 之上）
	fmt string
}

// newArchiveWriter 按格式建写出器。level 已由 NormalizeLevel 规整。
func newArchiveWriter(f *os.File, format string, level int) (*archiveWriter, error) {
	w := &archiveWriter{f: f, fmt: format}
	switch format {
	case ArchiveZip:
		w.z = zip.NewWriter(f)
		// zip.Writer 没有设级别的入口，标准做法是替换 Deflate 压缩器
		// （见 archive/zip 的 ExampleWriter_RegisterCompressor）
		w.z.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
			return flate.NewWriter(out, level)
		})
		return w, nil
	case ArchiveTarGz:
		gw, err := gzip.NewWriterLevel(f, level)
		if err != nil {
			return nil, fmt.Errorf("gzip 级别非法: %w", err)
		}
		w.gz = gw
		w.f = gw
		w.tw = tar.NewWriter(gw)
		return w, nil
	}
	return nil, fmt.Errorf("不支持的归档格式: %s", format)
}

// addDir 写一个目录条目。
func (w *archiveWriter) addDir(name string) error {
	if w.z != nil {
		_, err := w.z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		return err
	}
	return w.tw.WriteHeader(&tar.Header{
		Name:     name,
		Typeflag: tar.TypeDir,
		Mode:     0o755,
		Format:   tar.FormatPAX, // 允许超过 100 字节的深路径与中文名
	})
}

// addFile 写一个文件条目，name 为包内路径，src 为本地路径。
func (w *archiveWriter) addFile(name, src string, fi os.FileInfo) error {
	mode := fi.Mode().Perm()
	if mode == 0 {
		mode = 0o644
	}
	mod := fi.ModTime()

	if w.z != nil {
		hw, err := w.z.CreateHeader(&zip.FileHeader{
			Name:     name,
			Method:   zip.Deflate,
			Modified: mod,
		})
		if err != nil {
			return err
		}
		return copyInto(hw, src)
	}

	hdr := &tar.Header{
		Name:     name,
		Typeflag: tar.TypeReg,
		Mode:     int64(mode),
		Size:     fi.Size(),
		ModTime:  mod,
		Format:   tar.FormatPAX,
	}
	// 可执行位只在真有 POSIX 语义的平台上写：Windows 上 Mode 从文件读回来的
	// 0666/0444 不含 x，硬造一个 0755 反而会让解包侧误判
	if err := w.tw.WriteHeader(hdr); err != nil {
		return err
	}
	return copyInto(w.tw, src)
}

// copyInto 打开源文件并整份拷进 writer。句柄要显式关闭，
// 否则 Windows 上压缩完成后文件仍被自己锁着，删不掉也改不了。
func copyInto(dst io.Writer, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, in); err != nil {
		_ = in.Close()
		return fmt.Errorf("%s: %w", filepath.Base(src), err)
	}
	return in.Close()
}

// Close 按层从内到外收尾：tar → gzip（必须 Flush+Close，否则 gz 尾部没落盘）。
func (w *archiveWriter) Close() error {
	if w.z != nil {
		return w.z.Close()
	}
	if err := w.tw.Close(); err != nil {
		_ = w.gz.Close()
		return err
	}
	return w.gz.Close()
}
