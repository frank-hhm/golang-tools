package lanshare

// 传输进度与流式打包（计划 §四：目录走流式 zip，进度接 services/tasks）。
//
// emitter 与 downloader/archive 同形：main 里 SetEmitter(emit) 注入，
// emitProgress 既回前端又经 tasks.Observe 记账（事件名 lanshare:progress）。
// 负载只带 Name/Stage/Op/Percent/Note——正好是 tasks.payload 认得的键。

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// Progress 一次上传/下载/打包的进度事件负载。
type Progress struct {
	Op      string `json:"op"`      // upload / download / pack
	Name    string `json:"name"`    // 条目名（任务中心据此并行为一行）
	Stage   string `json:"stage"`   // running / done / error / canceled
	Percent int    `json:"percent"` // 0~100；大小未知时为 0（前端画不定进度）
	Note    string `json:"note"`
	// ID / Status 供切片上传接台账：ID=sid 当主键（接收上传全程稳定一行、
	// 重名避让出的多任务不误并），Status 是细粒度相位（queued/running/paused/done/error/canceled）。
	// 与 services/tasks.payload 的 ID/Status 键对齐；下载/非切片上传不带，为空不影响记账。
	ID     string `json:"id"`
	Status string `json:"status"`
}

var (
	emitMu  sync.RWMutex
	emitter func(name string, data ...any)
)

// SetEmitter 注入事件发射器（与其余工具包一致，main 里统一接 tasks.Observe + 前端）。
func SetEmitter(fn func(name string, data ...any)) {
	emitMu.Lock()
	emitter = fn
	emitMu.Unlock()
}

func emitProgress(p Progress) {
	emitMu.RLock()
	em := emitter
	emitMu.RUnlock()
	if em != nil {
		em("lanshare:progress", p)
	}
}

// writeZipStream 把 root（目录或单文件）流式打包成 zip 写进 w。
// 目录：根名作为顶层文件夹，逐项 walk 写入；单文件：直接 CREATE 一个条目。
// 软链/特殊文件跳过（与 tools/archive 计数口径一致，避免跟随成环）。
// 每写一个文件回一次 running 进度（按已写字节 / walk 预估总字节）。
func writeZipStream(ctx context.Context, w io.Writer, root, name string, totalBytes int64) error {
	zw := zip.NewWriter(w)
	base := filepath.Base(root)
	st, err := os.Stat(root)
	if err != nil {
		return err
	}

	var written int64
	report := func(stage, note string, force bool) {
		pct := 0
		if totalBytes > 0 {
			pct = int(written * 100 / totalBytes)
			if pct > 99 {
				pct = 99
			}
		}
		if force || stage != "running" || pct > 0 {
			emitProgress(Progress{Op: "pack", Name: name, Stage: stage, Percent: pct, Note: note})
		}
	}

	if !st.IsDir() {
		fw, err := zw.Create(base)
		if err != nil {
			return err
		}
		f, err := os.Open(root)
		if err != nil {
			return err
		}
		defer f.Close()
		n, err := io.Copy(fw, f)
		written += n
		if err != nil {
			return err
		}
		return zw.Close()
	}

	report("running", "开始打包", true)
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // 单个条目读不到不阻断整包
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		if info.IsDir() {
			_, err := zw.Create(base + "/" + filepath.ToSlash(rel) + "/")
			return err
		}
		if !info.Mode().IsRegular() {
			return nil // 软链/设备文件跳过
		}
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return nil
		}
		hdr.Name = base + "/" + filepath.ToSlash(rel)
		hdr.Method = zip.Deflate
		fw, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		n, err := io.Copy(fw, f)
		_ = f.Close()
		written += n
		report("running", "", true)
		return err
	})
	if err != nil {
		if ctx.Err() != nil {
			report("canceled", "客户端中断", true)
		} else {
			report("error", err.Error(), true)
		}
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	report("done", "打包完成", true)
	return nil
}

// dirBytes 估算目录总字节（含子目录，跳过软链），用于把流式打包换算成百分比。
// 深目录扫一遍有成本，仅在授权目录首次打包时调用一次即可。
func dirBytes(root string) int64 {
	var total int64
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if !info.IsDir() && info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total
}
