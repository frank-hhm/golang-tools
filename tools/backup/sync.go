// Package backup 增量目录备份引擎。
//
// 按文件内容 sha256 比对，只把「新增 / 变化」的文件从 Src 拷到 Dst：
//   - 纯增量：绝不删除 Dst 里 Src 已不存在的文件（目标是累积历史，不随源收缩）；
//   - 只认内容：先比大小，大小相同再比 sha256，一致就跳过，避免无谓拷贝；
//   - 原子落盘：写临时 .part 再改名，中断不会留下半个文件。
//
// 全程标准库、无平台差异，因此不带 build tag；Linux 面板同样能跑。
// Src / Dst 都是本机可见目录（「异地」即网络挂载盘路径），真正的网络协议
// 推送（webdav/云盘）不在本引擎职责内——那属于 docref 与 M6–M9 的 provider 层。
package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Stats 一次同步的计数结果。
type Stats struct {
	Scanned     int   `json:"scanned"`     // 扫描到的源文件总数
	Copied      int   `json:"copied"`      // 新增或更新并被拷贝的文件数
	Skipped     int   `json:"skipped"`     // 与目标内容一致而跳过的文件数
	BytesCopied int64 `json:"bytesCopied"` // 累计拷贝字节
}

// Progress 进度事件负载（同时也是任务中心 tasks.Observe 的原料，字段名与其对齐）。
// Stage 取 copying / done / error；Op 固定 "backup"，任务中心据此显示「备份 <Name>」。
type Progress struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Op      string `json:"op"`
	Stage   string `json:"stage"`
	Percent int    `json:"percent"`
	Note    string `json:"note"`
}

// EventProgress 进度事件名。main 里 RegisterEvent + 注入 emitter 后，前端订阅它即可。
const EventProgress = "backup:progress"

var (
	emitterMu sync.RWMutex
	emitter   func(name string, data ...any)
)

// SetEmitter 注入事件发射器（与 ftp/downloader 等包同一范式，main 统一接线）。
func SetEmitter(fn func(name string, data ...any)) {
	emitterMu.Lock()
	emitter = fn
	emitterMu.Unlock()
}

func emitEvent(p Progress) {
	emitterMu.RLock()
	em := emitter
	emitterMu.RUnlock()
	if em != nil {
		em(EventProgress, p)
	}
}

// sha256File 流式计算文件内容哈希，避免整读进内存。
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// copyFile 流式拷贝 src→dst（先写 .part 再改名，原子）。返回写入字节。
func copyFile(src, dst string) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	tmp := dst + ".part"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, in)
	if err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return 0, err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return n, err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return n, err
	}
	return n, nil
}

// collectFiles 递归收集 src 下的普通文件相对路径（POSIX 分隔），跳过符号链接。
func collectFiles(src string) ([]string, error) {
	var rels []string
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil // 不跟随软链，避免环路与非本仓内容
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return rerr
		}
		rels = append(rels, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rels, nil
}

// Sync 把 srcDir 增量同步进 dstDir（纯增量，不删目标）。
// onProgress 每处理一个文件回调一次（done 已处理数、total 总数、rel 当前相对路径），可为 nil。
// srcDir 必须是已存在的目录；拒绝 src==dst 或 dst 落在 src 内（否则会自我递归）。
func Sync(srcDir, dstDir string, onProgress func(done, total int, rel string)) (Stats, error) {
	var st Stats
	srcAbs, err := filepath.Abs(srcDir)
	if err != nil {
		return st, err
	}
	dstAbs, err := filepath.Abs(dstDir)
	if err != nil {
		return st, err
	}
	if srcAbs == dstAbs {
		return st, fmt.Errorf("源目录与目标目录相同")
	}
	// dst 不能位于 src 之内，否则每次写入又被下次扫描纳入 → 无限膨胀
	if within(srcAbs, dstAbs) {
		return st, fmt.Errorf("目标目录不能位于源目录之内: %s ⊂ %s", dstAbs, srcAbs)
	}
	si, err := os.Stat(srcAbs)
	if err != nil {
		return st, err
	}
	if !si.IsDir() {
		return st, fmt.Errorf("源必须是目录: %s", srcAbs)
	}
	rels, err := collectFiles(srcAbs)
	if err != nil {
		return st, err
	}
	st.Scanned = len(rels)
	if err := os.MkdirAll(dstAbs, 0o755); err != nil {
		return st, err
	}

	for i, rel := range rels {
		cur := filepath.FromSlash(rel)
		srcPath := filepath.Join(srcAbs, cur)
		dstPath := filepath.Join(dstAbs, cur)

		need, err := needCopy(srcPath, dstPath)
		if err != nil {
			return st, err
		}
		if need {
			n, err := copyFile(srcPath, dstPath)
			if err != nil {
				return st, fmt.Errorf("拷贝 %s 失败: %w", rel, err)
			}
			st.Copied++
			st.BytesCopied += n
		} else {
			st.Skipped++
		}
		if onProgress != nil {
			onProgress(i+1, st.Scanned, rel)
		}
	}
	return st, nil
}

// needCopy 判断目标是否需要（重新）写入：缺失 / 大小不同 / 内容哈希不同 → 需要。
func needCopy(srcPath, dstPath string) (bool, error) {
	sst, err := os.Stat(srcPath)
	if err != nil {
		return false, err
	}
	dst, err := os.Stat(dstPath)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	if dst.IsDir() {
		return false, fmt.Errorf("目标路径上是目录，无法覆盖为文件: %s", dstPath)
	}
	if sst.Size() != dst.Size() {
		return true, nil
	}
	if sst.Size() == 0 {
		return false, nil // 都是空文件，视为一致
	}
	sHash, err := sha256File(srcPath)
	if err != nil {
		return false, err
	}
	dHash, err := sha256File(dstPath)
	if err != nil {
		return false, err
	}
	return sHash != dHash, nil
}

// within 判断 child 是否等于或位于 parent 之内（按分隔符边界判断，非纯前缀）。
func within(parent, child string) bool {
	if parent == child {
		return true
	}
	p := filepath.Clean(parent)
	c := filepath.Clean(child)
	if len(c) > len(p) && strings.HasPrefix(c, p) {
		sep := c[len(p)]
		return sep == os.PathSeparator || sep == filepath.Separator
	}
	return false
}

// Run 执行一次同步并把进度通过包级 emitter 广播成 backup:progress 事件。
// 供绑定层调用：立即返回与否由调用方决定，这里同步跑完并回传 Stats。
func Run(id, name, srcDir, dstDir string) (Stats, error) {
	if name == "" {
		name = filepath.Base(srcDir)
	}
	emitEvent(Progress{ID: id, Name: name, Op: "backup", Stage: "copying", Percent: 0, Note: "开始备份"})
	st, err := Sync(srcDir, dstDir, func(done, total int, rel string) {
		pct := 0
		if total > 0 {
			pct = done * 100 / total
		}
		emitEvent(Progress{ID: id, Name: name, Op: "backup", Stage: "copying", Percent: pct, Note: rel})
	})
	if err != nil {
		emitEvent(Progress{ID: id, Name: name, Op: "backup", Stage: "error", Note: err.Error()})
		return st, err
	}
	emitEvent(Progress{ID: id, Name: name, Op: "backup", Stage: "done", Percent: 100,
		Note: fmt.Sprintf("完成：新增/更新 %d，跳过 %d", st.Copied, st.Skipped)})
	return st, nil
}
