// Package archive 文件压缩/解压的业务编排。
//
// 分层：真正的字节进出在 services/sysx（compress.go / extract.go，标准库实现），
// 这里只管"用户要的那个成品该叫什么、放在哪、进度怎么算、能不能取消"。
// 之所以不并进 sysx：命名避让与进度换算都是产品决策，换个工具就得重做，
// 而归档读写本身是系统原语。
package archive

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"Tools/services/sysx"
)

// 两种活儿。对外（命令行、前端、事件负载）一律用这两个短名。
const (
	OpCompress = "compress"
	OpExtract  = "extract"
)

// 进度阶段。done / error / canceled 是终态，前端据此收起进度条。
const (
	StageStart      = "start"
	StageScan       = "scan"
	StageWriting    = "writing"
	StageExtracting = "extracting"
	StageDone       = "done"
	StageError      = "error"
	StageCanceled   = "canceled"
)

// 条目状态（与 siteclone 的 Progress.Status 同口径，前端只认这一套）。
const (
	StatusOK   = "ok"
	StatusSkip = "skip"
	StatusFail = "fail"
)

// DefaultFallbackName 多条目又没有可用的父目录名时用的包名。
const DefaultFallbackName = "压缩包"

// Options 一次压缩/解压任务的全部输入。
type Options struct {
	Op     string   `json:"op"`     // OpCompress / OpExtract
	Paths  []string `json:"paths"`  // 压缩：要打包的条目；解压：归档文件（取第一个）
	Format string   `json:"format"` // 压缩用：sysx.ArchiveZip / sysx.ArchiveTarGz
	Level  int      `json:"level"`  // 1~9，<=0 走默认
	OutDir string   `json:"outDir"` // 输出目录；空=就地放在首个条目所在目录
	Name   string   `json:"name"`   // 输出文件名（不含扩展名）；空=按规则自动取名
	Dest   string   `json:"dest"`   // 解压目标目录；空=归档同级的同名文件夹
}

// Progress 进度事件负载，同时也是任务中心那一份的原料。
//
// 解压的 Percent 恒为 0：归档里到底有多少字节要读出来，不预先解一遍是不知道的，
// 而宁可让前端画不定进度条，也不报一个猜出来的百分比。
// 此时 Files / Bytes 仍在往上走，界面拿它们显示"已解出 N 项 / X MB"。
type Progress struct {
	JobID     string   `json:"jobId"`
	Op        string   `json:"op"`
	Stage     string   `json:"stage"`
	Name      string   `json:"name"`      // 当前条目：压缩是成品名，解压是归档名
	Out       string   `json:"out"`       // 成品/目标路径（done 时带上，供"打开目录"）
	Percent   int      `json:"percent"`   // 0~100；见上面的解压说明
	Bytes     int64    `json:"bytes"`     // 已处理原始字节
	Total     int64    `json:"total"`     // 预估总字节；0 表示未知
	Files     int      `json:"files"`     // 已写入/已解出的文件条目数
	Skipped   []string `json:"skipped"`   // 被跳过的条目名
	Msg       string   `json:"msg"`       // 最近一条说明
	Status    string   `json:"status"`    // ok / skip / fail
	ElapsedMs int64    `json:"elapsedMs"` // 已耗时
}

// Result 任务终态摘要。
type Result struct {
	Out       string   `json:"out"`
	Format    string   `json:"format"`
	Files     int      `json:"files"`
	Dirs      int      `json:"dirs"`
	Bytes     int64    `json:"bytes"`
	Skipped   []string `json:"skipped"`
	ElapsedMs int64    `json:"elapsedMs"`
}

// ExtFor 格式对应的扩展名（成品命名与格式识别都走它，避免两头写字符串）。
func ExtFor(format string) string {
	switch format {
	case sysx.ArchiveTarGz:
		return ".tar.gz"
	default:
		return ".zip"
	}
}

// FormatFor 按扩展名识别归档格式；认不出来返回空串。
//
// .tar.gz 要在 .gz 之前判：否则 "a.tar.gz" 会被读成 "gz" 这种不存在的格式。
func FormatFor(p string) string {
	low := strings.ToLower(filepath.Base(p))
	switch {
	case strings.HasSuffix(low, ".zip"):
		return sysx.ArchiveZip
	case strings.HasSuffix(low, ".tar.gz"), strings.HasSuffix(low, ".tgz"):
		return sysx.ArchiveTarGz
	default:
		return ""
	}
}

// stem 去掉扩展名后的主体名。压缩包自身的后缀要整段去掉：
// "log.tar.gz" 该得到 "log"，只剥最后一段会剩下难看的 "log.tar.zip"。
func stem(name string) string {
	low := strings.ToLower(name)
	for _, multi := range []string{".tar.gz", ".tar.bz2", ".tar.xz"} {
		if strings.HasSuffix(low, multi) {
			return name[:len(name)-len(multi)]
		}
	}
	if strings.HasSuffix(low, ".tgz") {
		return name[:len(name)-len(".tgz")]
	}
	if ext := filepath.Ext(name); ext != "" {
		return strings.TrimSuffix(name, ext)
	}
	return name
}

// Normalize 校验并补全输入：就地返回，错误表示这次请求没法做。
func Normalize(o *Options) error {
	if o.Op == "" {
		o.Op = OpCompress
	}
	switch o.Op {
	case OpCompress, OpExtract:
	default:
		return fmt.Errorf("不认识的操作类型: %s", o.Op)
	}

	kept := make([]string, 0, len(o.Paths))
	for _, p := range o.Paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		kept = append(kept, p)
	}
	o.Paths = kept
	if len(o.Paths) == 0 {
		return fmt.Errorf("没有选择任何文件或目录")
	}
	// 解压一次只做一个归档：多选时"解到同一个目录"的语义太含糊（同名会互相覆盖），
	// 交给调用方按需逐个发起
	if o.Op == OpExtract {
		if len(o.Paths) > 1 {
			o.Paths = o.Paths[:1]
		}
		if FormatFor(o.Paths[0]) == "" {
			return fmt.Errorf("不认得的归档类型: %s", filepath.Base(o.Paths[0]))
		}
		if _, err := os.Stat(o.Paths[0]); err != nil {
			return fmt.Errorf("打不开 %s: %w", filepath.Base(o.Paths[0]), err)
		}
		return nil
	}

	if o.Format == "" {
		o.Format = sysx.ArchiveZip
	}
	if o.Format != sysx.ArchiveZip && o.Format != sysx.ArchiveTarGz {
		return fmt.Errorf("不支持的压缩格式: %s", o.Format)
	}
	o.Level = sysx.NormalizeLevel(o.Level)
	for _, p := range o.Paths {
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("无法访问 %s: %w", filepath.Base(p), err)
		}
	}
	return nil
}

// Scan 预扫描：文件数与原始总字节，用来把压缩进度换算成百分比。
//
// 计数口径必须与 sysx.CompressArchive 一致（同样跳过软链与非普通文件），
// 否则分母偏大，进度条会永远走不满。
func Scan(paths []string) (files int, bytes int64) {
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if !st.IsDir() {
			if st.Mode().IsRegular() {
				files++
				bytes += st.Size()
			}
			continue
		}
		_ = filepath.WalkDir(p, func(wp string, d fs.DirEntry, err error) error {
			if err != nil {
				if d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			fi, ierr := d.Info()
			if ierr != nil || !fi.Mode().IsRegular() {
				return nil
			}
			files++
			bytes += fi.Size()
			return nil
		})
	}
	return files, bytes
}

// Preflight 开工前的预扫描结果（前端提示"这一下多大"用，扁平结构便于生成模型）。
type Preflight struct {
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
}

// PreflightOf 扫一遍给出一份 Preflight。
func PreflightOf(paths []string) Preflight {
	files, bytes := Scan(paths)
	return Preflight{Files: files, Bytes: bytes}
}

// SuggestOut 按命名规则算出成品完整路径（含重名避让）。
//
// 规则（“压缩到当前位置”的直觉，同时避免 proj/proj.zip 这种自己套自己）：
//   - 单条目：名字取条目名去扩展名（目录名不去），落在它自己所在目录
//   - 多选且都在同一个目录：首项名 + " 等N项"，就落在那个目录里
//   - 多选跨了子目录：用公共父目录的名字，但成品放到它的上一级
//     ——选 proj/a.txt + proj/sub/c.txt 时得到 proj.zip，放在 proj 旁边而不是里面
//   - 公共父目录是卷根、或 Desktop/Downloads 这类系统文件夹时不配当包名，
//     退化成“压缩包”，并且就落在它里面（本来就在用户正看着的那个文件夹）
//   - 已存在：name (1).ext、name (2).ext 递增
func SuggestOut(o Options) (string, error) {
	if len(o.Paths) == 0 {
		return "", fmt.Errorf("没有选择任何文件或目录")
	}
	// 用户明确指定的输出目录优先，且不再做目录推断
	name := strings.TrimSpace(o.Name)
	dir := strings.TrimSpace(o.OutDir)
	if dir != "" {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
		if st, err := os.Stat(dir); err != nil {
			return "", fmt.Errorf("输出目录不可用: %w", err)
		} else if !st.IsDir() {
			return "", fmt.Errorf("输出目录不是一个文件夹: %s", dir)
		}
	}

	if name != "" {
		if dir == "" {
			dir = filepath.Dir(o.Paths[0])
		}
		return uniquePath(filepath.Join(dir, stem(name)+ExtFor(o.Format))), nil
	}

	if len(o.Paths) == 1 {
		if dir == "" {
			dir = filepath.Dir(o.Paths[0])
		}
		return uniquePath(filepath.Join(dir, stem(filepath.Base(o.Paths[0]))+ExtFor(o.Format))), nil
	}

	container := commonParent(o.Paths)
	if container == "" {
		container = filepath.Dir(o.Paths[0]) // 只共享到根（甚至不同盘）：就地放，名字用兜底值
	}
	if sameDir(o.Paths) {
		// 同一个文件夹里多选：拿父目录名会变成 proj/proj.zip，用首项名+项数更直观
		return uniquePath(filepath.Join(container, multiSameDirName(o.Paths)+ExtFor(o.Format))), nil
	}
	base := filepath.Base(container)
	if base == "." || base == string(filepath.Separator) || base == ".." || genericFolderNames[strings.ToLower(base)] {
		return uniquePath(filepath.Join(container, DefaultFallbackName+ExtFor(o.Format))), nil
	}
	// 名字来自公共父目录，成品就得放在它的上一级，否则 proj 里又一个 proj.zip
	return uniquePath(filepath.Join(filepath.Dir(container), base+ExtFor(o.Format))), nil
}

// sameDir 所有条目是否都直接住在同一个目录里。
func sameDir(paths []string) bool {
	if len(paths) == 0 {
		return true
	}
	first := filepath.Dir(paths[0])
	for _, p := range paths[1:] {
		if filepath.Dir(p) != first {
			return false
		}
	}
	return true
}

// multiSameDirName 同目录多选的默认包名："a 等3项"。
// 首项名已经被 Windows 限制不带非法字符，拼上中文量词也不会越界。
func multiSameDirName(paths []string) string {
	return fmt.Sprintf("%s 等%d项", stem(filepath.Base(paths[0])), len(paths))
}

// genericFolderNames 这些目录名不配当包名：它们说的是“放哪”，不是“里面是什么”。
var genericFolderNames = map[string]bool{
	"desktop": true, "downloads": true, "documents": true, "temp": true,
	"\u684c\u9762": true, "\u4e0b\u8f7d": true, "\u6587\u6863": true, // 桌面 / 下载 / 文档
}

// commonParent 所有条目的公共父目录；只共享到卷根（或不同盘）时返回空串。
//
// 拿的是每个路径的“所在目录”：文件取父级，目录也取父级（选中的目录本身
// 已经是包内顶层了，再算进公共前缀会把包名撑成它自己的名字）。
//
// 不用 filepath.CommonPath：它对已归一的相对路径以外的情形行为没保证，
// 尤其是“跳盘”与“根段到底算不算一段”；自己切段能把这两个边界写死、也能单测。
func commonParent(paths []string) string {
	if len(paths) < 2 {
		return ""
	}
	var keep []string
	for i, p := range paths {
		segs := pathSegments(filepath.Dir(p))
		if i == 0 {
			keep = segs
			continue
		}
		n := 0
		for n < len(keep) && n < len(segs) && keep[n] == segs[n] {
			n++
		}
		keep = keep[:n]
		if len(keep) == 0 {
			return ""
		}
	}
	// 第一段是根（"C:\" 或 "/"）：只剩它等于“不同目录树”，没有一个可用的公共目录名
	if len(keep) <= 1 {
		return ""
	}
	return filepath.Join(keep...)
}

// pathSegments 把目录切成从根往下的段："C:\\proj\\a" → ["C:\\", "proj", "a"]。
func pathSegments(dir string) []string {
	dir = filepath.Clean(dir)
	var segs []string
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			segs = append([]string{dir}, segs...)
			break
		}
		segs = append([]string{filepath.Base(dir)}, segs...)
		dir = parent
	}
	return segs
}

// uniquePath 重名避让：x.zip → "x (1).zip" → "x (2).zip"
func uniquePath(p string) string {
	if _, err := os.Stat(p); err != nil {
		return p
	}
	dir, ext := filepath.Dir(p), filepath.Ext(p)
	base := strings.TrimSuffix(filepath.Base(p), ext)
	// .tar.gz 的 Ext 只给到 .gz，主体还带个 .tar：按整段归档后缀切一次
	if strings.HasSuffix(strings.ToLower(p), ".tar.gz") {
		dir, ext = filepath.Dir(p), ".tar.gz"
		base = strings.TrimSuffix(filepath.Base(p), ext)
	}
	for n := 1; ; n++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, n, ext))
		if _, err := os.Stat(candidate); err != nil {
			return candidate
		}
	}
}

// Run 执行一次任务，全程经 emit 报进度（emit 可为 nil）。
//
// 取消由调用方持有的 ctx 决定：sysx 那层每个条目检查一次，所以最大的粒度和
// 单个文件大小同量级，够用。
func Run(ctx context.Context, o Options, jobID string, emit func(Progress)) (Result, error) {
	var res Result
	if err := Normalize(&o); err != nil {
		return res, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	begin := time.Now()
	notify := func(p Progress) {
		if emit == nil {
			return
		}
		p.JobID = jobID
		p.Op = o.Op
		p.ElapsedMs = time.Since(begin).Milliseconds()
		emit(p)
	}

	switch o.Op {
	case OpExtract:
		return runExtract(ctx, o, begin, notify)
	default:
		return runCompress(ctx, o, begin, notify)
	}
}

func runCompress(ctx context.Context, o Options, begin time.Time, notify func(Progress)) (Result, error) {
	var res Result
	notify(Progress{Stage: StageScan, Status: StatusOK, Msg: "统计中"})

	out, err := SuggestOut(o)
	if err != nil {
		return res, err
	}
	_, total := Scan(o.Paths)
	notify(Progress{Stage: StageWriting, Name: filepath.Base(out), Out: out, Total: total, Status: StatusOK, Msg: "开始压缩"})

	// 节流：原语每个条目回调一次，十万文件的目录树就是十万个事件，
	// 直接把前端事件通道堵死。只按“百分比变了 且 距上次上报过 200ms”送出，
	// 上限 100 个过程事件，终态的 done 另走一次不受限。
	var lastPct int = -1
	var lastEmit time.Time
	st, err := sysx.CompressArchive(ctx, out, o.Paths, o.Format, o.Level, func(written int64) {
		p := 0
		if total > 0 {
			p = int(written * 100 / total)
			if p > 99 {
				p = 99 // 收尾前不满格：最后由 done 事件补成 100，看着更像真的完成了
			}
		}
		now := time.Now()
		if p == lastPct || (!lastEmit.IsZero() && now.Sub(lastEmit) < 200*time.Millisecond) {
			return
		}
		lastPct, lastEmit = p, now
		notify(Progress{Stage: StageWriting, Name: filepath.Base(out), Out: out, Percent: p,
			Bytes: written, Total: total})
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			notify(Progress{Stage: StageCanceled, Name: filepath.Base(out), Status: StatusFail, Msg: "已取消"})
			return res, ctxErr
		}
		notify(Progress{Stage: StageError, Name: filepath.Base(out), Status: StatusFail, Msg: err.Error()})
		return res, err
	}

	res = Result{Out: out, Format: o.Format, Files: st.Files, Dirs: st.Dirs,
		Bytes: st.Bytes, Skipped: st.Skipped, ElapsedMs: time.Since(begin).Milliseconds()}
	msg := fmt.Sprintf("完成：%d 个文件", st.Files)
	if len(st.Skipped) > 0 {
		msg += fmt.Sprintf("，跳过 %d 项", len(st.Skipped))
	}
	notify(Progress{Stage: StageDone, Name: filepath.Base(out), Out: out, Percent: 100,
		Bytes: st.Bytes, Total: total, Files: st.Files, Skipped: st.Skipped, Status: StatusOK, Msg: msg})
	return res, nil
}

func runExtract(ctx context.Context, o Options, begin time.Time, notify func(Progress)) (Result, error) {
	var res Result
	archive := o.Paths[0]
	name := filepath.Base(archive)

	dest := strings.TrimSpace(o.Dest)
	if dest == "" {
		dest = filepath.Join(filepath.Dir(archive), stem(name))
		if dest == filepath.Dir(archive) { // 去扩展名后撞回自己所在目录（"a" 这种无扩展名归档）
			dest = dest + "-解包"
		}
	}
	if abs, err := filepath.Abs(dest); err == nil {
		dest = abs
	}
	notify(Progress{Stage: StageExtracting, Name: name, Out: dest, Status: StatusOK, Msg: "开始解压"})

	var entries int
	var written int64
	var lastEmit time.Time
	err := sysx.ExtractArchive(ctx, archive, dest, func(n int, b int64) {
		entries, written = n, b
		// 与压缩同口径的节流：解压一个包里十万条目时不能逐个发事件
		now := time.Now()
		if !lastEmit.IsZero() && now.Sub(lastEmit) < 200*time.Millisecond {
			return
		}
		lastEmit = now
		notify(Progress{Stage: StageExtracting, Name: name, Out: dest, Files: entries, Bytes: written})
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			notify(Progress{Stage: StageCanceled, Name: name, Status: StatusFail, Msg: "已取消"})
			return res, ctxErr
		}
		notify(Progress{Stage: StageError, Name: name, Status: StatusFail, Msg: err.Error()})
		return res, err
	}
	res = Result{Out: dest, Format: FormatFor(archive), Files: entries, Bytes: written,
		ElapsedMs: time.Since(begin).Milliseconds()}
	notify(Progress{Stage: StageDone, Name: name, Out: dest, Percent: 100, Files: entries,
		Bytes: written, Status: StatusOK, Msg: fmt.Sprintf("完成：解出 %d 项", entries)})
	return res, nil
}

// SortPaths 稳定排序，前端多选顺序不定时（右键多选给的顺序不保证）用来消抖。
func SortPaths(paths []string) []string {
	out := append([]string(nil), paths...)
	sort.Strings(out)
	return out
}
