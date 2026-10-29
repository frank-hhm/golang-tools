// Package disk 本机磁盘与文件管理：盘符信息、目录浏览、新建/改名/删除、
// 复制移动、按名搜索与内容搜索。
//
// 这里只做文件系统本身的事，不含 UI 与协议；绑定层在 services/app/disk.go。
// Windows 专属能力（盘符枚举、回收站删除、资源管理器/命令行打开）在
// drive_windows.go，非 Windows 编译时由 drive_other.go 兜底。
package disk

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"Tools/services/config"
)

// Drive 一个盘符（卷）。
type Drive struct {
	Root    string  `json:"root"`    // 根路径，如 "C:\\"
	Label   string  `json:"label"`   // 卷标，可能为空
	FSType  string  `json:"fsType"`  // 文件系统，如 NTFS / exFAT
	Kind    string  `json:"kind"`    // 类型：fixed / removable / network / cdrom / ram / unknown
	Total   uint64  `json:"total"`   // 总容量（字节）
	Free    uint64  `json:"free"`    // 可用容量（字节）
	Used    uint64  `json:"used"`    // 已用容量（字节）
	UsedPct float64 `json:"usedPct"` // 已用占比 0-100
}

// Entry 目录里的一项（文件或目录）。
type Entry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size"`    // 文件字节数；目录固定 0，需要时用 DirSize 再算
	ModTime string `json:"modTime"` // 本地时间 "2006-01-02 15:04:05"
	Mode    string `json:"mode"`    // 权限位，"755" / "644" 这种（Windows 上仅供参考）
	Hidden  bool   `json:"hidden"`
	Link    bool   `json:"link"` // 符号链接或快捷方式
	Ext     string `json:"ext"`  // 小写扩展名（不含点）
}

// Part 路径面包屑的一段。
type Part struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Listing 一次目录浏览的结果。
type Listing struct {
	Path    string  `json:"path"`
	Parent  string  `json:"parent"`  // 上一级；已在根时为 ""
	Parts   []Part  `json:"parts"`   // 面包屑（从盘符开始）
	Entries []Entry `json:"entries"` // 目录在前、文件在后，各自按名称排序
	Dirs    int     `json:"dirs"`    // 当前层目录数
	Files   int     `json:"files"`   // 当前层文件数
	Size    int64   `json:"size"`    // 当前层文件大小合计（不含子目录内的文件）
	Drive   *Drive  `json:"drive"`   // 所在盘符信息，取不到时为 nil
}

// Match 内容搜索命中的一行。
type Match struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Line int    `json:"line"`
	Text string `json:"text"`
	Size int64  `json:"size"`
}

// 新建/改名时的名称上限与非法字符：Windows 文件名的硬限制，提前拦比让 os 报错更友好。
const maxNameLen = 200

var errBadName = errors.New(`名称不能为空、不能是 . 或 ..，也不能包含 \ / : * ? " < > |`)

// validateName 校验单段文件/目录名。
func validateName(name string) error {
	n := strings.TrimSpace(name)
	if n == "" || n == "." || n == ".." || len([]rune(n)) > maxNameLen {
		return errBadName
	}
	if strings.ContainsAny(n, `\/:*?"<>|`) {
		return errBadName
	}
	// Windows 不允许以空格或点结尾：看原串（已被 Trim 掉的话就永远碰不到）
	if strings.HasSuffix(name, " ") || strings.HasSuffix(name, ".") {
		return fmt.Errorf("名称不能以空格或点结尾")
	}
	return nil
}

// cleanDir 把用户传来的路径整理成绝对目录路径并确认存在。
func cleanDir(path string) (string, error) {
	p := strings.TrimSpace(path)
	if p == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = home
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", fmt.Errorf("%s 不是目录", abs)
	}
	return abs, nil
}

// modeText 权限位 → "755" 这种三段八进制。
func modeText(mode os.FileMode) string {
	return fmt.Sprintf("%03o", uint32(mode.Perm()))
}

// partsOf 生成面包屑：盘符 → 各级目录。
func partsOf(abs string) []Part {
	vol := filepath.VolumeName(abs) // "C:" 或 ""
	rest := strings.TrimPrefix(abs, vol)
	parts := make([]Part, 0, 8)
	if vol != "" {
		root := vol + string(filepath.Separator)
		parts = append(parts, Part{Name: root, Path: root})
	}
	cur := vol + string(filepath.Separator)
	for _, seg := range strings.Split(strings.Trim(rest, `\/`), string(filepath.Separator)) {
		if seg == "" {
			continue
		}
		cur = filepath.Join(cur, seg)
		parts = append(parts, Part{Name: seg, Path: cur})
	}
	if len(parts) == 0 {
		parts = append(parts, Part{Name: abs, Path: abs})
	}
	return parts
}

// List 浏览一个目录。
func List(path string) (Listing, error) {
	abs, err := cleanDir(path)
	if err != nil {
		return Listing{}, err
	}
	des, err := os.ReadDir(abs)
	if err != nil {
		return Listing{}, err
	}

	out := Listing{Path: abs, Parts: partsOf(abs)}
	if parent := filepath.Dir(abs); parent != abs {
		out.Parent = parent
	}
	if d, ok := DriveOf(abs); ok {
		out.Drive = &d
	}

	out.Entries = make([]Entry, 0, len(des))
	for _, de := range des {
		full := filepath.Join(abs, de.Name())
		e := Entry{
			Name:   de.Name(),
			Path:   full,
			IsDir:  de.IsDir(),
			Hidden: strings.HasPrefix(de.Name(), "."),
		}
		if !e.IsDir {
			e.Ext = strings.ToLower(strings.TrimPrefix(filepath.Ext(de.Name()), "."))
		}
		// Info 失败（权限不足/刚被删掉）不该让整页报错，保留其余项继续
		if info, ierr := de.Info(); ierr == nil {
			e.ModTime = info.ModTime().Format("2006-01-02 15:04:05")
			e.Mode = modeText(info.Mode())
			e.Link = info.Mode()&os.ModeSymlink != 0
			if !e.IsDir {
				e.Size = info.Size()
			}
		}
		if e.IsDir {
			out.Dirs++
		} else {
			out.Files++
			out.Size += e.Size
		}
		out.Entries = append(out.Entries, e)
	}
	// 目录在前、文件在后；同类按名称不区分大小写排序（和资源管理器一致）
	sort.SliceStable(out.Entries, func(i, j int) bool {
		a, b := out.Entries[i], out.Entries[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return out, nil
}

// Mkdir 在 parent 下建一个目录，返回新目录的完整路径。
func Mkdir(parent, name string) (string, error) {
	if err := validateName(name); err != nil {
		return "", err
	}
	dir, err := cleanDir(parent)
	if err != nil {
		return "", err
	}
	full := filepath.Join(dir, strings.TrimSpace(name))
	if _, err := os.Stat(full); err == nil {
		return "", fmt.Errorf("「%s」已存在", name)
	}
	if err := os.Mkdir(full, 0o755); err != nil {
		return "", err
	}
	return full, nil
}

// NewFile 在 parent 下建一个空文件，返回完整路径。
func NewFile(parent, name string) (string, error) {
	if err := validateName(name); err != nil {
		return "", err
	}
	dir, err := cleanDir(parent)
	if err != nil {
		return "", err
	}
	full := filepath.Join(dir, strings.TrimSpace(name))
	f, err := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("「%s」已存在", name)
		}
		return "", err
	}
	_ = f.Close()
	return full, nil
}

// Rename 在同级目录里改名（只改名字，不移动），返回新路径。
func Rename(path, newName string) (string, error) {
	if err := validateName(newName); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", err
	}
	target := filepath.Join(filepath.Dir(abs), strings.TrimSpace(newName))
	if strings.EqualFold(target, abs) {
		return abs, nil
	}
	if _, err := os.Stat(target); err == nil {
		return "", fmt.Errorf("「%s」已存在", newName)
	}
	if err := os.Rename(abs, target); err != nil {
		return "", err
	}
	return target, nil
}

// copyFile 复制单个文件，保留修改时间（权限位在 Windows 上意义有限，照抄即可）。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(dst, time.Now(), info.ModTime())
}

// copyItem 递归复制文件或目录到 dstDir 下（同名目录合并复制，同名文件直接报错）。
func copyItem(src, dstDir string) (string, error) {
	name := filepath.Base(src)
	dst := filepath.Join(dstDir, name)
	info, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		if _, err := os.Stat(dst); err == nil {
			return "", fmt.Errorf("目标目录已有同名文件「%s」", name)
		}
		return dst, copyFile(src, dst)
	}
	// 目录：目标已存在就进去合并，不存在就新建
	if err := os.MkdirAll(dst, info.Mode().Perm()|0o700); err != nil {
		return "", err
	}
	des, err := os.ReadDir(src)
	if err != nil {
		return "", err
	}
	for _, de := range des {
		if _, err := copyItem(filepath.Join(src, de.Name()), dst); err != nil {
			return "", err
		}
	}
	return dst, nil
}

// Copy 把一批路径复制到 dstDir，返回成功条数；遇到第一个错误就停下（避免复制一半还继续）。
func Copy(paths []string, dstDir string) (int, error) {
	dst, err := cleanDir(dstDir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, p := range paths {
		src := strings.TrimSpace(p)
		if src == "" {
			continue
		}
		// 不允许把目录复制到它自己里面（无限递归）
		if abs, aerr := filepath.Abs(src); aerr == nil {
			if st, serr := os.Stat(abs); serr == nil && st.IsDir() && isSubPath(dst, abs) {
				return n, fmt.Errorf("不能把「%s」复制到它自己的子目录里", filepath.Base(abs))
			}
		}
		if _, err := copyItem(src, dst); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// isSubPath 判断 child 是否在 parent 之下（同一路径算真）。
func isSubPath(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}

// Move 把一批路径移动到 dstDir，返回成功条数。
func Move(paths []string, dstDir string) (int, error) {
	dst, err := cleanDir(dstDir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, p := range paths {
		src := strings.TrimSpace(p)
		if src == "" {
			continue
		}
		abs, err := filepath.Abs(src)
		if err != nil {
			return n, err
		}
		if strings.EqualFold(filepath.Dir(abs), dst) {
			continue // 已经在目标目录里，算跳过而不是失败
		}
		target := filepath.Join(dst, filepath.Base(abs))
		if _, err := os.Stat(target); err == nil {
			return n, fmt.Errorf("目标目录已有同名项「%s」", filepath.Base(abs))
		}
		if err := os.Rename(abs, target); err != nil {
			// 跨盘符 rename 会失败，退回"复制 + 删源"
			if _, cerr := copyItem(abs, dst); cerr != nil {
				return n, err
			}
			if rerr := os.RemoveAll(abs); rerr != nil {
				return n, rerr
			}
		}
		n++
	}
	return n, nil
}

// PermanentDelete 直接删除（不进回收站）。行内调用前 UI 会二次确认。
func PermanentDelete(paths []string) (int, error) {
	n := 0
	for _, p := range paths {
		abs := strings.TrimSpace(p)
		if abs == "" {
			continue
		}
		if err := os.RemoveAll(abs); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// DirSize 递归统计目录（或文件）占用字节数。
func DirSize(path string) (int64, error) {
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return info.Size(), nil
	}
	var total int64
	err = filepath.WalkDir(abs, func(_ string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // 单个子项没权限不影响整体统计
		}
		if d.IsDir() {
			return nil
		}
		if fi, ierr := d.Info(); ierr == nil {
			total += fi.Size()
		}
		return nil
	})
	return total, err
}

// defaultMaxHits 搜索结果上限：目录树大的时候不设限会把内存打满。
const defaultMaxHits = 500

// Search 按名称搜索（不区分大小写），includeSub=false 只搜当前层。
func Search(root, keyword string, includeSub bool, maxHits int) ([]Entry, error) {
	dir, err := cleanDir(root)
	if err != nil {
		return nil, err
	}
	kw := strings.ToLower(strings.TrimSpace(keyword))
	if kw == "" {
		return []Entry{}, nil
	}
	if maxHits <= 0 {
		maxHits = defaultMaxHits
	}
	hits := make([]Entry, 0, 32)
	add := func(full string, de os.DirEntry) bool {
		if !strings.Contains(strings.ToLower(de.Name()), kw) {
			return false
		}
		e := Entry{Name: de.Name(), Path: full, IsDir: de.IsDir()}
		if !e.IsDir {
			e.Ext = strings.ToLower(strings.TrimPrefix(filepath.Ext(de.Name()), "."))
		}
		if info, ierr := de.Info(); ierr == nil {
			e.ModTime = info.ModTime().Format("2006-01-02 15:04:05")
			e.Mode = modeText(info.Mode())
			if !e.IsDir {
				e.Size = info.Size()
			}
		}
		hits = append(hits, e)
		return len(hits) >= maxHits
	}

	if !includeSub {
		des, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, de := range des {
			if add(filepath.Join(dir, de.Name()), de) {
				break
			}
		}
		return hits, nil
	}

	stop := errors.New("enough")
	err = filepath.WalkDir(dir, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if p == dir {
			return nil
		}
		if add(p, d) {
			return stop
		}
		// 命中上限后就不再往下走
		return nil
	})
	if err != nil && !errors.Is(err, stop) {
		return hits, err
	}
	return hits, nil
}

// contentSkipExt 内容搜索直接跳过的二进制/超大文本类型。
var contentSkipExt = map[string]bool{
	"exe": true, "dll": true, "so": true, "a": true, "lib": true, "bin": true,
	"png": true, "jpg": true, "jpeg": true, "gif": true, "bmp": true, "ico": true,
	"webp": true, "mp3": true, "mp4": true, "avi": true, "mkv": true, "flv": true,
	"wav": true, "zip": true, "rar": true, "7z": true, "gz": true, "tar": true,
	"pdf": true, "doc": true, "docx": true, "xls": true, "xlsx": true, "ppt": true,
	"pptx": true, "db": true, "class": true, "pyc": true, "wasm": true, "ttf": true,
	"woff": true, "woff2": true,
}

// 内容搜索的硬闸门：单文件最大 2MiB、最多看 20000 个文件、最多 200 条命中。
const (
	contentMaxFileSize = 2 << 20
	contentMaxFiles    = 20000
	contentMaxHits     = 200
)

// SearchContent 在文本文件里找关键字（不区分大小写），返回命中的行。
//
// 只做"够用"的实现：跳过常见二进制扩展名与超大文件，逐行扫描。
// 不引入 ripgrep 之外的重型依赖，也不追求 .gitignore 语义。
func SearchContent(root, keyword string, includeSub bool) ([]Match, error) {
	dir, err := cleanDir(root)
	if err != nil {
		return nil, err
	}
	kw := strings.ToLower(strings.TrimSpace(keyword))
	if kw == "" {
		return []Match{}, nil
	}
	hits := make([]Match, 0, 32)
	scanned := 0

	scanFile := func(p string, d os.DirEntry) bool {
		if d.IsDir() {
			return false
		}
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(p), "."))
		if contentSkipExt[ext] {
			return false
		}
		info, ierr := d.Info()
		if ierr != nil || info.Size() > contentMaxFileSize {
			return false
		}
		scanned++
		if scanned > contentMaxFiles {
			return true
		}
		f, ferr := os.Open(p)
		if ferr != nil {
			return false
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		lineNo := 0
		for sc.Scan() {
			lineNo++
			line := sc.Text()
			if !strings.Contains(strings.ToLower(line), kw) {
				continue
			}
			hits = append(hits, Match{
				Path: p,
				Name: filepath.Base(p),
				Line: lineNo,
				Text: strings.TrimSpace(line),
				Size: info.Size(),
			})
			if len(hits) >= contentMaxHits {
				return true
			}
		}
		return false
	}

	if !includeSub {
		des, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, de := range des {
			if scanFile(filepath.Join(dir, de.Name()), de) {
				break
			}
		}
		return hits, nil
	}

	stop := errors.New("enough")
	err = filepath.WalkDir(dir, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if p == dir {
			return nil
		}
		if scanFile(p, d) {
			return stop
		}
		return nil
	})
	if err != nil && !errors.Is(err, stop) {
		return hits, err
	}
	return hits, nil
}

// ============================================================
// 收藏夹：跟着配置目录走，不进 config.json（避免同步配置时把本机路径带到别的机器）
// ============================================================

// favoritesPath 收藏夹文件路径。
func favoritesPath() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "disk-favorites.json"), nil
}

// Favorites 读取收藏的目录列表（按收藏顺序）。
func Favorites() []string {
	p, err := favoritesPath()
	if err != nil {
		return []string{}
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return []string{}
	}
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return []string{}
	}
	out := make([]string, 0, len(list))
	for _, s := range list {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// SaveFavorites 覆盖写入收藏夹（去重、保序）。
func SaveFavorites(list []string) error {
	p, err := favoritesPath()
	if err != nil {
		return err
	}
	seen := make(map[string]bool, len(list))
	clean := make([]string, 0, len(list))
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		key := strings.ToLower(s)
		if seen[key] {
			continue
		}
		seen[key] = true
		clean = append(clean, s)
	}
	data, err := json.MarshalIndent(clean, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}
