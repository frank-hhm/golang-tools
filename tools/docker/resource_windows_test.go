//go:build windows

package docker

// ============================================================
// 网络 / 存储卷 / 编排的协议层单测。
//
// 单独起一个假引擎：主假引擎的 /containers/json 返回的容器没有 compose 标签，
// 往里塞会影响 TestListContainersWithStats 的条数断言，所以这里自己搭一套。
// ============================================================

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type resCalls struct {
	mu    sync.Mutex
	paths []string // "METHOD path?query body"
}

func (r *resCalls) add(s string) {
	r.mu.Lock()
	r.paths = append(r.paths, s)
	r.mu.Unlock()
}

func (r *resCalls) find(sub string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.paths {
		if strings.Contains(p, sub) {
			return true
		}
	}
	return false
}

// dropEndpoint 清掉端点快照，让下一轮按新的 DOCKER_HOST 重扫。
// 连接池是按 URL host（localhost）复用的，换端点后旧空闲连接还连着上一个假引擎，
// 不关掉会拿旧服务器的 404 当作新服务器的响应（全量跑时才会暴露）
func dropEndpoint() {
	endpMu.Lock()
	endpPinned = false
	endpFromEnv = false
	endpSweptAt = time.Time{}
	endpMu.Unlock()
	resetEngineCaches()
	if httpClient != nil {
		httpClient.CloseIdleConnections()
	}
	if streamCli != nil {
		streamCli.CloseIdleConnections()
	}
}

// pointEngineAt 把引擎指到指定假服务器，测试结束恢复原 DOCKER_HOST
func pointEngineAt(t *testing.T, srv *httptest.Server) {
	t.Helper()
	old, had := os.LookupEnv("DOCKER_HOST")
	os.Setenv("DOCKER_HOST", "tcp://"+strings.TrimPrefix(srv.URL, "http://"))
	dropEndpoint()
	t.Cleanup(func() {
		if had {
			os.Setenv("DOCKER_HOST", old)
		} else {
			os.Unsetenv("DOCKER_HOST")
		}
		dropEndpoint()
	})
}

func newResourceEngine(t *testing.T, calls *resCalls) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"Version":"27.1.1","ApiVersion":"1.47","Os":"linux","Arch":"x86_64"}`)
	})

	// ---- 容器列表：两个 compose 容器，同属 blog 项目，共用 blog-data 卷 ----
	mux.HandleFunc("/v1.47/containers/json", func(w http.ResponseWriter, r *http.Request) {
		calls.add("GET /containers/json")
		fmt.Fprint(w, `[
{"Id":"1111111111111111111111111111111111111111111111111111111111111111","Names":["/blog-web-1"],"Image":"nginx:alpine","Created":1700000100,"State":"running","Status":"Up 3 minutes",
 "Labels":{"com.docker.compose.project":"blog","com.docker.compose.service":"web","com.docker.compose.project.working_dir":"C:/work/blog","com.docker.compose.project.config_files":"C:/work/blog/docker-compose.yml"},
 "Mounts":[{"Type":"volume","Name":"blog-data"}]},
{"Id":"2222222222222222222222222222222222222222222222222222222222222222","Names":["/blog-db-1"],"Image":"mysql:8.4","Created":1700000000,"State":"exited","Status":"Exited (0) 1 hour ago",
 "Labels":{"com.docker.compose.project":"blog","com.docker.compose.service":"db"},
 "Mounts":[{"Type":"volume","Name":"blog-data"},{"Type":"bind","Name":""}]}
]`)
	})

	// ---- 容器动作：只需要记录被调到（面板拿到的是短 ID，12 位） ----
	webID, dbID := strings.Repeat("1", 12), strings.Repeat("2", 12)
	for _, act := range []string{"start", "stop", "restart"} {
		act := act
		mux.HandleFunc("/v1.47/containers/"+webID+"/"+act, func(w http.ResponseWriter, r *http.Request) {
			calls.add("POST /containers/web/" + act)
			w.WriteHeader(http.StatusNoContent)
		})
		mux.HandleFunc("/v1.47/containers/"+dbID+"/"+act, func(w http.ResponseWriter, r *http.Request) {
			calls.add("POST /containers/db/" + act)
			w.WriteHeader(http.StatusNoContent)
		})
	}
	mux.HandleFunc("/v1.47/containers/"+webID, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		calls.add("DELETE /containers/web")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/v1.47/containers/"+dbID, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		calls.add("DELETE /containers/db")
		w.WriteHeader(http.StatusNoContent)
	})

	// ---- 网络 ----
	mux.HandleFunc("/v1.47/networks", func(w http.ResponseWriter, r *http.Request) {
		calls.add("GET /networks")
		fmt.Fprint(w, `[
{"Name":"bridge","Id":"1111111111111111111111111111111111111111111111111111111111111111","Driver":"bridge","Scope":"local","Created":"2024-01-01T00:00:00Z","Labels":{},
 "IPAM":{"Config":[{"Subnet":"172.17.0.0/16","Gateway":"172.17.0.1"}]},"Containers":{"aaa":{"Name":"web"}}},
{"Name":"blog-net","Id":"2222222222222222222222222222222222222222222222222222222222222222","Driver":"bridge","Scope":"local","Created":"2024-02-01T00:00:00Z","Labels":{"go-tools":"docker"},"Internal":true,
 "IPAM":{"Config":[{"Subnet":"172.30.0.0/16","Gateway":"172.30.0.1"}]},"Containers":{}}
]`)
	})
	mux.HandleFunc("/v1.47/networks/create", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls.add("POST /networks/create " + string(body))
		fmt.Fprint(w, `{"Id":"3333333333333333333333333333333333333333333333333333333333333333"}`)
	})
	mux.HandleFunc("/v1.47/networks/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/v1.47/networks/")
		calls.add("DELETE /networks/" + name)
		if name == "busy" {
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, `{"message":"network busy has active endpoints"}`)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// ---- 存储卷 ----
	mux.HandleFunc("/v1.47/volumes", func(w http.ResponseWriter, r *http.Request) {
		calls.add("GET /volumes")
		fmt.Fprint(w, `{"Volumes":[
{"Name":"blog-data","Driver":"local","Mountpoint":"/var/lib/docker/volumes/blog-data/_data","CreatedAt":"2024-01-01T00:00:00Z","Scope":"local","Labels":{}},
{"Name":"orphan","Driver":"local","Mountpoint":"/var/lib/docker/volumes/orphan/_data","CreatedAt":"2024-02-01T00:00:00Z","Scope":"local","Labels":{}}
]}`)
	})
	mux.HandleFunc("/v1.47/volumes/create", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls.add("POST /volumes/create " + string(body))
		fmt.Fprint(w, `{"Name":"newvol","Driver":"local"}`)
	})
	mux.HandleFunc("/v1.47/volumes/prune", func(w http.ResponseWriter, r *http.Request) {
		calls.add("POST /volumes/prune")
		fmt.Fprint(w, `{"VolumesDeleted":["orphan"],"SpaceReclaimed":1024}`)
	})
	mux.HandleFunc("/v1.47/volumes/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/v1.47/volumes/")
		calls.add("DELETE /volumes/" + name)
		if name == "blog-data" {
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, `{"message":"remove blog-data: volume is in use - [abc123]"}`)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestListNetworks(t *testing.T) {
	calls := &resCalls{}
	srv := newResourceEngine(t, calls)
	pointEngineAt(t, srv)

	nets, err := ListNetworks()
	if err != nil {
		t.Fatal(err)
	}
	if len(nets) != 2 {
		t.Fatalf("网络数错误: %+v", nets)
	}
	// 内置网络沉底：自建的 blog-net 排第一
	if nets[0].Name != "blog-net" || nets[1].Name != "bridge" {
		t.Fatalf("排序错误（内置应沉底）: %+v", nets)
	}
	if !nets[1].Builtin || nets[0].Builtin {
		t.Fatal("Builtin 判定错误")
	}
	if nets[0].Subnet != "172.30.0.0/16" || nets[0].Gateway != "172.30.0.1" || !nets[0].Internal {
		t.Fatalf("IPAM/Internal 解析错误: %+v", nets[0])
	}
	if nets[0].OwnerTool != "docker" {
		t.Fatalf("归属标签回标错误: %+v", nets[0])
	}
	if nets[0].ID != "222222222222" {
		t.Fatalf("ID 应短化: %s", nets[0].ID)
	}
	if nets[1].Containers != 1 {
		t.Fatalf("接入容器数错误: %+v", nets[1])
	}
}

func TestCreateAndRemoveNetwork(t *testing.T) {
	calls := &resCalls{}
	srv := newResourceEngine(t, calls)
	pointEngineAt(t, srv)

	id, err := CreateNetwork(NetworkSpec{Name: "app-net", Driver: "bridge", Internal: true, Subnet: "172.31.0.0/16"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id, "33333333") {
		t.Fatalf("返回 ID 错误: %s", id)
	}
	calls.mu.Lock()
	body := ""
	for _, p := range calls.paths {
		if strings.HasPrefix(p, "POST /networks/create ") {
			body = p
		}
	}
	calls.mu.Unlock()
	for _, want := range []string{`"Name":"app-net"`, `"Driver":"bridge"`, `"Internal":true`, `"Subnet":"172.31.0.0/16"`} {
		if !strings.Contains(body, want) {
			t.Errorf("create 请求体缺少 %s\n实际: %s", want, body)
		}
	}

	if _, err := CreateNetwork(NetworkSpec{Name: "坏 名字"}); err == nil {
		t.Fatal("非法网络名应被拦下")
	}
	if err := RemoveNetwork("222222222222"); err != nil {
		t.Fatalf("删除网络应成功: %v", err)
	}
	// 有容器接入：错误要翻成可执行建议
	err = RemoveNetwork("busy")
	if err == nil || !strings.Contains(err.Error(), "先停止或断开") {
		t.Fatalf("409 应给出断开建议, 实际: %v", err)
	}
}

func TestListVolumes(t *testing.T) {
	calls := &resCalls{}
	srv := newResourceEngine(t, calls)
	pointEngineAt(t, srv)

	vols, err := ListVolumes()
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 2 {
		t.Fatalf("卷数错误: %+v", vols)
	}
	// 有引用（2 个容器都挂了 blog-data）的排前面
	if vols[0].Name != "blog-data" || vols[1].Name != "orphan" {
		t.Fatalf("排序错误: %+v", vols)
	}
	if vols[0].InUse != 2 || vols[1].InUse != 0 {
		t.Fatalf("引用计数错误: %+v", vols)
	}
	if vols[0].Mountpoint == "" || vols[0].Driver != "local" {
		t.Fatalf("字段解析错误: %+v", vols[0])
	}
}

func TestCreateRemovePruneVolume(t *testing.T) {
	calls := &resCalls{}
	srv := newResourceEngine(t, calls)
	pointEngineAt(t, srv)

	name, err := CreateVolume(VolumeSpec{Name: "newvol"})
	if err != nil || name != "newvol" {
		t.Fatalf("创建卷失败: %s %v", name, err)
	}
	if err := RemoveVolume("orphan"); err != nil {
		t.Fatalf("删除空卷应成功: %v", err)
	}
	err = RemoveVolume("blog-data")
	if err == nil || !strings.Contains(err.Error(), "仍被容器占用") {
		t.Fatalf("占用中的卷应给出提示, 实际: %v", err)
	}
	rep, err := PruneVolumes()
	if err != nil || len(rep.Deleted) != 1 || rep.Space != 1024 {
		t.Fatalf("清理结果错误: %+v %v", rep, err)
	}
}

func TestListComposeProjects(t *testing.T) {
	calls := &resCalls{}
	srv := newResourceEngine(t, calls)
	pointEngineAt(t, srv)

	list, err := ListComposeProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("编排项目数错误（没有 compose 标签的容器应被跳过）: %+v", list)
	}
	p := list[0]
	if p.Name != "blog" || p.Total != 2 || p.Running != 1 || p.Status != "partial" {
		t.Fatalf("项目聚合错误: %+v", p)
	}
	if len(p.Services) != 2 || p.Services[0].Name != "db" || p.Services[1].Name != "web" {
		t.Fatalf("服务应按名称排序: %+v", p.Services)
	}
	if p.WorkingDir != "C:/work/blog" || len(p.ConfigFiles) != 1 {
		t.Fatalf("工作目录/配置文件解析错误: %+v", p)
	}
	if p.Created != 1700000000 {
		t.Fatalf("项目时间应取最早容器: %d", p.Created)
	}
}

func TestComposeAction(t *testing.T) {
	calls := &resCalls{}
	srv := newResourceEngine(t, calls)
	pointEngineAt(t, srv)

	// 启动：只有 exited 的 db 需要起，running 的 web 跳过
	msg, err := ComposeAction("blog", "start")
	if err != nil {
		t.Fatal(err)
	}
	if msg != "已启动 1 个容器" {
		t.Fatalf("启动摘要错误: %s", msg)
	}
	if !calls.find("/containers/db/start") || calls.find("/containers/web/start") {
		t.Fatalf("启动调用不符合预期: %v", calls.paths)
	}

	// 停止：只有 running 的 web 需要停
	calls.mu.Lock()
	calls.paths = nil
	calls.mu.Unlock()
	msg, err = ComposeAction("blog", "stop")
	if err != nil {
		t.Fatal(err)
	}
	if msg != "已停止 1 个容器" {
		t.Fatalf("停止摘要错误: %s", msg)
	}
	if !calls.find("/containers/web/stop") || calls.find("/containers/db/stop") {
		t.Fatalf("停止调用不符合预期: %v", calls.paths)
	}

	// 移除：两个容器都要删
	calls.mu.Lock()
	calls.paths = nil
	calls.mu.Unlock()
	msg, err = ComposeAction("blog", "down")
	if err != nil {
		t.Fatal(err)
	}
	if msg != "已移除 2 个容器" {
		t.Fatalf("移除摘要错误: %s", msg)
	}
	if !calls.find("DELETE /containers/web") || !calls.find("DELETE /containers/db") {
		t.Fatalf("移除调用不符合预期: %v", calls.paths)
	}

	if _, err := ComposeAction("blog", "explode"); err == nil {
		t.Fatal("未知操作应报错")
	}
	if _, err := ComposeAction("missing", "start"); err == nil {
		t.Fatal("不存在的项目应报错")
	}
}
