package downloader

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// ============================================================
// 展速：给「多镜像 / 多代理」各测一次延迟与吞吐，前端展示后由用户手动选。
// 本期不做自动切换（那是 PR4）：这里只负责「测得准、测得完不卡住」。
// ============================================================

// SourceStat 一个镜像源的探测结果。
type SourceStat struct {
	URL       string  `json:"url"`
	OK        bool    `json:"ok"`
	LatencyMs int64   `json:"latencyMs"` // 首字节延迟
	Speed     float64 `json:"speed"`     // 样本吞吐（字节/秒）
	Size      int64   `json:"size"`      // 内容长度（探到才填）
	Err       string  `json:"err"`
}

// ProxyStat 一个代理的探测结果。
type ProxyStat struct {
	URL       string  `json:"url"`
	OK        bool    `json:"ok"`
	LatencyMs int64   `json:"latencyMs"`
	Speed     float64 `json:"speed"`
	Err       string  `json:"err"`
}

// ProbeSources 并发探测多个镜像：HEAD 取长度，再限时 GET 前若干 KB 估速。
func ProbeSources(mirrors []string) []SourceStat {
	out := make([]SourceStat, len(mirrors))
	var wg sync.WaitGroup
	for i, u := range mirrors {
		wg.Add(1)
		go func(i int, u string) {
			defer wg.Done()
			out[i] = probeSource(u)
		}(i, u)
	}
	wg.Wait()
	return out
}

func probeSource(u string) SourceStat {
	st := SourceStat{URL: u}
	client := newClient("", 15*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 先量首字节延迟：GET 带 Range 取一点点，测到 resp 即记 latency。
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		st.Err = err.Error()
		return st
	}
	req.Header.Set("Range", "bytes=0-"+strconv.Itoa(probeReadBytes-1))
	resp, err := client.Do(req)
	if err != nil {
		st.Err = err.Error()
		return st
	}
	defer resp.Body.Close()
	st.LatencyMs = time.Since(start).Milliseconds()
	if resp.StatusCode >= 400 {
		st.Err = "HTTP " + strconv.Itoa(resp.StatusCode)
		return st
	}
	if v := resp.Header.Get("Content-Range"); v != "" {
		st.Size = parseTotal(v)
	} else if v := resp.Header.Get("Content-Length"); v != "" {
		st.Size = parseSize(v)
	}
	n, err := io.Copy(io.Discard, io.LimitReader(resp.Body, probeReadBytes))
	if err != nil && n == 0 {
		st.Err = err.Error()
		return st
	}
	elapsed := time.Since(start).Seconds()
	if elapsed > 0 {
		st.Speed = float64(n) / elapsed
	}
	st.OK = true
	return st
}

// ProbeProxies 经各代理访问 testURL，测延迟/吞吐/可用性。
// testURL 为空时用 httpbin 风格的一个小资源；无外网环境会全部 OK=false（如实呈现）。
func ProbeProxies(list []string, testURL string) []ProxyStat {
	if testURL == "" {
		testURL = "https://www.google.com/generate_204"
	}
	out := make([]ProxyStat, len(list))
	var wg sync.WaitGroup
	for i, px := range list {
		wg.Add(1)
		go func(i int, px string) {
			defer wg.Done()
			out[i] = probeProxy(px, testURL)
		}(i, px)
	}
	wg.Wait()
	return out
}

func probeProxy(px, testURL string) ProxyStat {
	st := ProxyStat{URL: px}
	client := newClient(px, 12*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, testURL, nil)
	if err != nil {
		st.Err = err.Error()
		return st
	}
	resp, err := client.Do(req)
	if err != nil {
		st.Err = err.Error()
		return st
	}
	defer resp.Body.Close()
	st.LatencyMs = time.Since(start).Milliseconds()
	if resp.StatusCode >= 500 {
		st.Err = "HTTP " + strconv.Itoa(resp.StatusCode)
		return st
	}
	n, _ := io.Copy(io.Discard, io.LimitReader(resp.Body, probeReadBytes))
	elapsed := time.Since(start).Seconds()
	if elapsed > 0 {
		st.Speed = float64(n) / elapsed
	}
	st.OK = true
	return st
}
