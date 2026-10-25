package npm

// 临时真机验证：需要本机 Docker 引擎。验完即删，不进仓库。
// 跑法：go test -buildvcs=false -run TestZzlNpmLive -v -timeout 300s ./tools/npm/

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go-tools/tools/docker"
)

func zzGet(t *testing.T, port int, path string) (int, string) {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, string(data)
}

func TestZzlNpmLive(t *testing.T) {
	useTempData(t)
	if ok, msg := DockerAvailable(); !ok {
		t.Skipf("Docker 引擎不可用: %s", msg)
	}

	// 站点目录：index.html + 一个带扩展名资源 + 一个没有 index 的子目录（测列目录）
	site := t.TempDir()
	if err := os.WriteFile(filepath.Join(site, "index.html"), []byte("<h1>go-tools docker site</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "app.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(site, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "sub", "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	port := SuggestPort(18080)
	cfg := InstanceConfig{Name: "live-site", Mode: ModeDocker, Image: DefaultDockerImage, Port: port,
		DocRoot: site, SPA: true, Directory: true}
	if err := SaveInstance(cfg); err != nil {
		t.Fatal(err)
	}
	list, _ := ListInstances()
	cfg = list[len(list)-1]
	t.Logf("实例 %s 端口 %d 站点 %s", cfg.ID, cfg.Port, site)

	started := time.Now()
	if err := StartInstance(cfg.ID); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	t.Logf("启动耗时 %dms", time.Since(started).Milliseconds())

	st, err := GetInstanceStatus(cfg.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("状态 running=%v mode=%s image=%s ver=%q container=%s cid=%s state=%s pid=%d files=%d",
		st.Running, st.Mode, st.Image, st.Version, st.Container, st.ContainerID, st.ContainerState, st.PID, st.Files)
	if !st.Running || st.Mode != ModeDocker || st.ContainerID == "" {
		t.Fatalf("状态不对: %+v", st)
	}
	if st.PID != 0 {
		t.Errorf("容器模式不该报本机 PID: %d", st.PID)
	}

	prefix, _ := instanceDir(cfg.ID)
	t.Logf("容器内配置：%s", filepath.Join(prefix, "conf", "nginx.conf"))

	// 1) 首页
	if code, body := zzGet(t, cfg.Port, "/"); code != 200 || !strings.Contains(body, "docker site") {
		t.Fatalf("首页不对: %d %s", code, body)
	}
	// 2) SPA 回退：无扩展名的未知路由回 index.html
	if code, body := zzGet(t, cfg.Port, "/user/profile"); code != 200 || !strings.Contains(body, "docker site") {
		t.Fatalf("SPA 回退不对: %d %s", code, body)
	}
	// 3) 带扩展名的缺失资源不回退（浏览器把 HTML 当 JS 解析会报语法错）
	if code, _ := zzGet(t, cfg.Port, "/missing.js"); code != 404 {
		t.Fatalf("缺失资源应 404, 得到 %d", code)
	}
	// 4) 列目录：没有 index.html 的子目录应列出内容
	if code, body := zzGet(t, cfg.Port, "/sub/"); code != 200 || !strings.Contains(body, "note.txt") {
		t.Fatalf("列目录不对: %d %s", code, body)
	}
	// 5) 真实资源
	if code, _ := zzGet(t, cfg.Port, "/app.js"); code != 200 {
		t.Fatalf("app.js 应 200, 得到 %d", code)
	}

	// 6) 日志落宿主机 + 按日志统计请求数与流量
	lines, err := Logs(cfg.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("宿主 access.log 行数 %d，末行 %q", len(lines), lastOf(lines))
	if len(lines) < 5 {
		t.Fatalf("容器写入的 access.log 应能在宿主机读到, 得到 %d 行", len(lines))
	}
	req, bytes := dockerAccessStat(prefix)
	t.Logf("按日志统计：请求 %d 流量 %d 字节", req, bytes)
	if req < 5 || bytes <= 0 {
		t.Fatalf("请求数/流量统计不对: %d / %d", req, bytes)
	}
	if st2, err := GetInstanceStatus(cfg.ID); err != nil || st2.Requests < 5 || st2.Bytes <= 0 {
		t.Fatalf("状态面板的计数应来自日志: %+v (%v)", st2, err)
	}
	// 刚起不到一秒时 req/s 本来就是 0，等一秒再验运行时长与速率算得出来
	time.Sleep(1500 * time.Millisecond)
	if st2, err := GetInstanceStatus(cfg.ID); err != nil || st2.UptimeSeconds <= 0 || st2.RequestsPerSec <= 0 {
		t.Fatalf("运行时长/速率应可算出: %+v (%v)", st2, err)
	} else {
		t.Logf("运行时长 %ds，req/s %.2f", st2.UptimeSeconds, st2.RequestsPerSec)
	}

	// 7) 发布端口情况（静态站点只该有一个）
	cs, err := docker.InspectContainer(st.ContainerID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("已发布端口: %+v 主端口 %d", cs.Published, cs.HostPort)
	if len(cs.Published) != 1 || cs.Published[0].Host != cfg.Port {
		t.Errorf("端口发布不对: %+v", cs.Published)
	}

	// 8) 关掉 SPA 与列目录 → 重启容器：生成的配置跟随刷新且立刻生效
	cfg.SPA, cfg.Directory = false, false
	if err := SaveInstance(cfg); err != nil {
		t.Fatal(err)
	}
	if err := RestartInstance(cfg.ID); err != nil {
		t.Fatalf("重启失败: %v", err)
	}
	if code, _ := zzGet(t, cfg.Port, "/user/profile"); code != 404 {
		t.Fatalf("关闭 SPA 后未知路由应 404, 得到 %d", code)
	}
	if code, _ := zzGet(t, cfg.Port, "/sub/"); code != 403 {
		t.Fatalf("关闭列目录后无 index 的目录应 403, 得到 %d", code)
	}
	t.Log("开关变更已随重启生效（未重建容器）")

	// 9) 改端口 → 重建容器
	oldCID := st.ContainerID
	np := SuggestPort(cfg.Port + 1)
	cfg.Port = np
	if err := SaveInstance(cfg); err != nil {
		t.Fatal(err)
	}
	if err := RestartInstance(cfg.ID); err != nil {
		t.Fatalf("换端口重启失败: %v", err)
	}
	st3, _ := GetInstanceStatus(cfg.ID)
	t.Logf("端口 %d 后容器 %s → %s", np, shortOf(oldCID), shortOf(st3.ContainerID))
	if st3.ContainerID == oldCID {
		t.Fatal("改端口未重建容器")
	}
	if code, _ := zzGet(t, np, "/"); code != 200 {
		t.Fatalf("新端口首页应 200, 得到 %d", code)
	}

	// 10) 停止
	if err := StopInstance(cfg.ID); err != nil {
		t.Fatalf("停止失败: %v", err)
	}
	st4, _ := GetInstanceStatus(cfg.ID)
	t.Logf("停止后 running=%v state=%q", st4.Running, st4.ContainerState)
	if st4.Running {
		t.Fatal("停止后仍报运行中")
	}

	// 11) 删除实例连带清理容器
	if err := RemoveInstance(cfg.ID); err != nil {
		t.Fatalf("删除实例失败: %v", err)
	}
	if id, found, err := docker.FindContainerByLabel("npm", cfg.ID); err != nil || found {
		t.Fatalf("容器未清理: %s found=%v err=%v", id, found, err)
	}
	t.Log("删除实例已连带清理容器")
}

func lastOf(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}

func shortOf(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
