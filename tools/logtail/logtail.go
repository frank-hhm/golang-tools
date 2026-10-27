package logtail

// ============================================================
// 日志尾部读取
//
// 各工具（php/nginx/npm/mysql/redis）都有「取日志最后 N 行」的需求，而且是在
// 状态轮询与日志弹窗里反复调用。以前每一处都写成 os.ReadFile + strings.Split，
// 等于为了拿 200 行把整个 access.log（本地站点跑几天就是几百 MB）读进内存，
// 再复制成 string、再切成几十万个字符串：一次刷新几百 MB 的瞬时分配，
// 轮询期间 GC 一直被压着，表现出来就是内存曲线打满 + CPU 抖动。
//
// 这里提供两种有界的读法：
//   - Lines/Tail：从文件末尾按块往前读，攒够行数就停，并有绝对上限；
//   - Scan：从给定偏移流式读增量（逐行处理完即丢），用于按日志做累计统计。
// ============================================================

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
)

const (
	// defaultLines n 传 0 或负数时的兜底行数
	defaultLines = 200
	// chunkStep 每轮往前扩读的块大小
	chunkStep = 64 << 10
	// maxTailBytes 一次最多取回的尾部字节。再多也远超任何面板能展示的行数，
	// 继续往前读只是把内存和磁盘 IO 花在没人看的内容上
	maxTailBytes = 8 << 20
	// maxScanBytes 一轮增量扫描最多处理的字节：日志被脚本刷爆时，
	// 宁可这一轮少算一点（下一轮接着算），也不在一次轮询里读完整本日志
	maxScanBytes = 64 << 20
)

// Lines 读取 path 的最后 n 行。文件不存在返回 nil, nil ——
// 调用方按「还没有日志」处理即可。
func Lines(path string, n int) ([]string, error) {
	if n <= 0 {
		n = defaultLines
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() || fi.Size() <= 0 {
		return nil, nil
	}

	var (
		collected []byte
		pos       = fi.Size()
		cut       = false // 没读到文件开头（首行可能只是半行）
	)
	nl := []byte{'\n'}
	for pos > 0 {
		chunk := int64(chunkStep)
		if chunk > pos {
			chunk = pos
		}
		pos -= chunk
		buf := make([]byte, chunk)
		if _, err := f.ReadAt(buf, pos); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		collected = append(buf, collected...)
		// 多要一个换行：末尾不带换行的最后一行不算分隔符，少一个就会缺行
		if bytes.Count(collected, nl) > n {
			break
		}
		if len(collected) >= maxTailBytes {
			cut = pos > 0
			break
		}
	}

	lines := strings.Split(strings.TrimRight(string(collected), "\r\n"), "\n")
	// Windows 下的日志普遍是 CRLF：按 \n 切完每行末尾还挂着一个 \r，
	// 面板里看着像多一个空白字符，解析数字（如 access.log 统计）也会被带 \r 的字段干扰
	for i, l := range lines {
		if strings.HasSuffix(l, "\r") {
			lines[i] = l[:len(l)-1]
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	// 只有“从块中间开始的那半行真的被留着”时才标省略号：
	// 正常情况下它会被裁掉（我们只要最后 n 行），留着的不该带假标记
	if cut && len(lines) > 0 {
		lines[0] = "…" + lines[0]
	}
	return lines, nil
}

// Tail 取最后 n 行并拼成字符串；读不到就返回空串（用于错误信息拼接的常见场景）
func Tail(path string, n int) string {
	lines, err := Lines(path, n)
	if err != nil || len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

// LinesAfter 读取 path 中「字节偏移 from 之后」内容的最后 n 行。
// 用于日志弹窗的「清空」：清空只记下一个截止偏移（清空那一刻的文件大小），
// 之后只展示该偏移之后新写入的日志，既不删磁盘历史、也不会被下一次轮询回填。
// from <= 0 等价于 Lines（从头算尾部 n 行）；文件被轮转/截断变短到 from 以下时返回空。
func LinesAfter(path string, from int64, n int) ([]string, error) {
	if from <= 0 {
		return Lines(path, n)
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() || fi.Size() <= from {
		return nil, nil // 截止偏移之后还没有新内容（或文件被截断）
	}
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return nil, err
	}
	var (
		collected []byte
		buf       = make([]byte, chunkStep)
	)
	for {
		rd, rerr := f.Read(buf)
		if rd > 0 {
			collected = append(collected, buf[:rd]...)
		}
		if rerr != nil {
			break // EOF 或读错：按已读到的内容收口
		}
	}
	lines := strings.Split(strings.TrimRight(string(collected), "\r\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimRight(l, "\r")
		if l == "" {
			continue
		}
		out = append(out, l)
	}
	if n > 0 && len(out) > n {
		out = out[len(out)-n:]
	}
	return out, nil
}

// Scan 从 offset 起逐行读取新增内容，处理完即丢弃（不把一段日志整体驻留内存）。
// 返回读到的新偏移：调用方存下来即可做增量统计；文件变短（轮转/截断）时
// 自动从 0 重来。单轮最多处理 maxScanBytes，剩下的留给下一轮。
func Scan(path string, offset int64, fn func(line string)) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if offset < 0 || offset > fi.Size() {
		offset = 0 // 轮转或截断：不能拿着不存在的偏移当增量起点
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}
	br := bufio.NewReaderSize(f, 64<<10)
	consumed := int64(0)
	for consumed < maxScanBytes {
		chunk, err := br.ReadString('\n')
		consumed += int64(len(chunk))
		if chunk != "" {
			fn(strings.TrimRight(chunk, "\r\n"))
		}
		if err != nil {
			break // EOF 或读错：都按已消费的位置收口
		}
	}
	return offset + consumed, nil
}
