package nginx

// ============================================================
// 在线版本发现（nginx.org 官方 Windows zip）
//
// 策略：
//  1. 抓 https://nginx.org/en/download.html，从中提取各产品线（mainline / stable）
//     公布的版本号；
//  2. 补充按系列线性 HEAD 探测（nginx.org/download/nginx-<ver>.zip 命名稳定）；
//  3. 全部失败时回退内置常量清单。
// ============================================================

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const nginxDownloadPage = "https://nginx.org/en/download.html"

// zipURL 官方 Windows zip 直链
func zipURL(ver string) string {
	return fmt.Sprintf("https://nginx.org/download/nginx-%s.zip", ver)
}

// 官方下载页上的版本链接形如：<a href="nginx-1.27.4.zip">1.27.4</a>
var nginxLinkRe = regexp.MustCompile(`href="[^"]*nginx-(\d+\.\d+(?:\.\d+)?)\.zip[^"]*"`)

// nginxSeries 各产品线补丁号探测区间（下载页解析失败时兜底）
type seriesProbe struct {
	series string
	lo, hi int
}

var nginxSeries = []seriesProbe{
	{"1.24", 0, 8},
	{"1.25", 0, 5},
	{"1.26", 0, 9},
	{"1.27", 0, 5},
	{"1.28", 0, 1},
	{"1.29", 0, 5},
}

// 网络全失败时的回退推荐清单
var nginxFallback = []string{"1.29.1", "1.28.0", "1.27.5", "1.26.3", "1.25.5", "1.24.0"}

// FetchRemoteVersions 返回可在线安装的 Nginx 版本（按版本降序）
func FetchRemoteVersions() ([]RemoteVersion, error) {
	cands := map[string]bool{}
	if body, err := httpGet(nginxDownloadPage); err == nil {
		for _, m := range nginxLinkRe.FindAllStringSubmatch(body, -1) {
			cands[normalizeVersionName(m[1])] = true
		}
	}
	// 补充系列探测（并发 HEAD，只保留每个 series 的最新一个）
	var wg sync.WaitGroup
	probed := make([]RemoteVersion, len(nginxSeries))
	hit := make([]bool, len(nginxSeries))
	for i, p := range nginxSeries {
		wg.Add(1)
		go func(i int, p seriesProbe) {
			defer wg.Done()
			for patch := p.hi; patch >= p.lo; patch-- {
				ver := fmt.Sprintf("%s.%d", p.series, patch)
				if ok, size := headExists(zipURL(ver)); ok {
					probed[i] = RemoteVersion{Version: ver, URL: zipURL(ver), Size: size, Release: p.series}
					hit[i] = true
					cands[ver] = true
					return
				}
			}
		}(i, p)
	}
	wg.Wait()

	out := map[string]RemoteVersion{}
	for i := range nginxSeries {
		if hit[i] {
			out[probed[i].Version] = probed[i]
		}
	}
	for ver := range cands {
		if _, ok := out[ver]; ok {
			continue
		}
		if ok, size := headExists(zipURL(ver)); ok {
			out[ver] = RemoteVersion{Version: ver, URL: zipURL(ver), Size: size, Release: seriesOf(ver)}
		}
	}
	if len(out) == 0 {
		for _, v := range nginxFallback {
			out[v] = RemoteVersion{Version: v, URL: zipURL(v), Release: seriesOf(v)}
		}
	}
	list := make([]RemoteVersion, 0, len(out))
	for _, r := range out {
		list = append(list, r)
	}
	sort.Slice(list, func(i, j int) bool { return compareVersion(list[i].Version, list[j].Version) > 0 })
	return list, nil
}

func seriesOf(ver string) string {
	parts := strings.Split(ver, ".")
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return ver
}

func httpGet(url string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent())
	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func headExists(url string) (bool, int64) {
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return false, 0
	}
	req.Header.Set("User-Agent", userAgent())
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, 0
	}
	defer resp.Body.Close()
	ok := resp.StatusCode < 400 || resp.StatusCode == 405
	return ok, resp.ContentLength
}
