package downloader

import (
	"sync/atomic"
)

// partState 一个分块的运行态：done 用原子计数，供并发 worker 各写各的、汇总时读。
type partState struct {
	index      int
	start, end int64 // end=-1 表示未定长（单流）
	done       atomic.Int64
}

// length 该块总字节；未定长返回 0。
func (p *partState) length() int64 {
	if p.end < 0 {
		return 0
	}
	return p.end - p.start + 1
}

// planParts 把 [0,size) 尽量均分成 conns 段（末段吸收余数）。
func planParts(size int64, conns int) []partState {
	if conns < 1 {
		conns = 1
	}
	if int64(conns) > size {
		conns = int(size)
	}
	if conns < 1 {
		conns = 1
	}
	seg := size / int64(conns)
	parts := make([]partState, 0, conns)
	var pos int64
	for i := 0; i < conns; i++ {
		start := pos
		var end int64
		if i == conns-1 {
			end = size - 1
		} else {
			end = start + seg - 1
		}
		parts = append(parts, partState{index: i, start: start, end: end})
		pos = end + 1
		if pos >= size {
			break
		}
	}
	return parts
}

// emitProgress 汇总各块进度并回调（分块路径）。
func emitProgress(p dlParams, size int64, parts []partState) {
	if p.onProgress == nil {
		return
	}
	var dl int64
	out := make([]Part, len(parts))
	for i := range parts {
		pt := &parts[i]
		d := pt.done.Load()
		dl += d
		out[i] = Part{Index: pt.index, Start: pt.start, End: pt.end, Done: d}
	}
	p.onProgress(size, dl, out)
}

// emitProgressSingle 单流路径回调（size 可能为 0=未知长度）。
func emitProgressSingle(p dlParams, size, dl int64) {
	if p.onProgress == nil {
		return
	}
	p.onProgress(size, dl, []Part{{Index: 0, Start: 0, End: -1, Done: dl}})
}
