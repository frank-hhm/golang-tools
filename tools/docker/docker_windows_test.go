//go:build windows

package docker

// ============================================================
// 无 Docker 环境下的协议层单测：用假 Engine API（tcp 端点）验证
// 端点解析、版本协商、容器/镜像列表、创建请求体、拉取流、日志解帧。
// 处理器内不调用 t.*（避免跨 goroutine panic），改为记录请求供断言。
// ============================================================

import (
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	cID      = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	cShort   = "aaaaaaaaaaaa"
	exited   = "1111111111111111111111111111111111111111111111111111111111111111"
	createID = "cccccccccccccccccccccccccccccccc"
)

type capturedRec struct {
	mu         sync.Mutex
	createBody string
	createName string
	pullQuery  string
	logsFollow string
}

var (
	fakeServer *httptest.Server
	rec        = &capturedRec{}
)

func startFakeEngine(t *testing.T) {
	t.Helper()
	if fakeServer != nil {
		return
	}
	mux := http.NewServeMux()

	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"Version":"27.1.1","ApiVersion":"1.47","Os":"linux","Arch":"x86_64","KernelVersion":"6.6.5","Platform":{"Name":"Docker Desktop 4.34.0 (165345)"}}`)
	})
	mux.HandleFunc("/v1.47/info", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"OperatingSystem":"Docker Desktop","Name":"my-pc","NCPU":8,"MemTotal":16000000000}`)
	})
	mux.HandleFunc("/v1.47/containers/json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("filters") != "" {
			fmt.Fprintf(w, `[{"Id":"%s","Names":["/go-tools-mysql-inst-1"],"Image":"mysql:8.4","State":"exited","Status":"Exited (0) 2 hours ago","Labels":{"go-tools":"mysql","go-tools.instance":"inst-1"}}]`, exited)
			return
		}
		fmt.Fprintf(w, `[{"Id":"%s","Names":["/web"],"Image":"nginx:alpine","ImageID":"sha256:bbbb","Command":"\"nginx -g\"","Created":1700000000,"State":"running","Status":"Up 5 minutes","Ports":[{"IP":"127.0.0.1","PrivatePort":80,"PublicPort":8080,"Type":"tcp"}],"Labels":{}}]`, cID)
	})
	mux.HandleFunc("/v1.47/containers/"+cID+"/stats", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"cpu_stats":{"cpu_usage":{"total_usage":2000},"system_cpu_usage":8000,"online_cpus":2},`+
			`"precpu_stats":{"cpu_usage":{"total_usage":1000},"system_cpu_usage":4000},`+
			`"memory_stats":{"usage":104857600,"limit":1048576000,"stats":{"cache":10485760}},`+
			`"blkio_stats":{"io_service_bytes_recursive":[{"op":"read","value":4096},{"op":"write","value":8192},{"op":"Sync","value":1000}]},`+
			`"networks":{"eth0":{"rx_bytes":2048,"tx_bytes":1024},"eth1":{"rx_bytes":100,"tx_bytes":200}},`+
			`"pids_stats":{"current":12}}`)
	})
	mux.HandleFunc("/v1.47/containers/"+cID+"/json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"Id":"%s","Name":"/web","Config":{"Image":"nginx:alpine","Tty":false},"State":{"Status":"running","Running":true,"ExitCode":0,"StartedAt":"%s","Pid":4242}}`,
			cID, time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano))
	})
	mux.HandleFunc("/v1.47/containers/"+cID+"/logs", func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.logsFollow = r.URL.Query().Get("follow")
		rec.mu.Unlock()
		fl, _ := w.(http.Flusher)
		if r.URL.Query().Get("follow") == "1" {
			// 跟随流：先给两帧历史，稍后再推一帧（验证增量解帧），保持连接直到客户端关闭
			w.Write(demuxFrame(1, "hello stdout\n"))
			w.Write(demuxFrame(2, "boom stderr\n"))
			if fl != nil {
				fl.Flush()
			}
			select {
			case <-time.After(150 * time.Millisecond):
				w.Write(demuxFrame(1, "later line\n"))
				if fl != nil {
					fl.Flush()
				}
			case <-r.Context().Done():
				return
			}
			<-r.Context().Done()
			return
		}
		w.Write(demuxFrame(1, "hello stdout\n"))
		w.Write(demuxFrame(2, "boom stderr\n"))
	})
	mux.HandleFunc("/v1.47/containers/create", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.createBody = string(body)
		rec.createName = r.URL.Query().Get("name")
		rec.mu.Unlock()
		fmt.Fprintf(w, `{"Id":"%s"}`, createID)
	})
	mux.HandleFunc("/v1.47/containers/"+strings.Repeat("d", 64)+"/start", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/v1.47/images/json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"Id":"sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee","RepoTags":["mysql:8.4","mysql:latest"],"Created":1700000000,"Size":600000000}]`)
	})
	mux.HandleFunc("/v1.47/images/create", func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.pullQuery = r.URL.RawQuery
		rec.mu.Unlock()
		fmt.Fprint(w, `{"status":"Pulling from mysql/8.4","id":"abc"}`+"\n")
		fmt.Fprint(w, `{"status":"Downloading","id":"abc","progressDetail":{"current":50,"total":100},"progress":"[==>]"} `+"\n")
		fmt.Fprint(w, `{"status":"Pull complete","id":"abc"}`+"\n")
		fmt.Fprint(w, `{"status":"Digest: sha256:123"}`+"\n")
	})
	mux.HandleFunc("/v1.47/containers/missing/json", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"No such container: missing"}`)
	})

	fakeServer = httptest.NewServer(mux)
	os.Setenv("DOCKER_HOST", "tcp://"+strings.TrimPrefix(fakeServer.URL, "http://"))
}

func demuxFrame(stream byte, text string) []byte {
	h := make([]byte, 8)
	h[0] = stream
	binary.BigEndian.PutUint32(h[4:], uint32(len(text)))
	return append(h, text...)
}

func TestEngineStatus(t *testing.T) {
	startFakeEngine(t)
	info, err := GetEngineStatus()
	if err != nil {
		t.Fatal(err)
	}
	if !info.Available || info.Version != "27.1.1" || info.APIVersion != "1.47" {
		t.Fatalf("引擎状态解析错误: %+v", info)
	}
	if !info.IsDesktop || info.CPUs != 8 {
		t.Fatalf("Desktop/CPU 判定错误: %+v", info)
	}
	if info.Endpoint == "" {
		t.Fatal("端点描述为空")
	}
}

func TestListContainersWithStats(t *testing.T) {
	startFakeEngine(t)
	list, err := ListContainers(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("容器数错误: %+v", list)
	}
	c := list[0]
	if c.ID != cShort || c.Name != "web" || c.State != "running" {
		t.Fatalf("容器字段错误: %+v", c)
	}
	if len(c.Ports) != 1 || c.Ports[0].PublicPort != 8080 {
		t.Fatalf("端口映射错误: %+v", c.Ports)
	}
	// cpu: (2000-1000)/(8000-4000)*2*100 = 50
	if c.CPUPct < 49.9 || c.CPUPct > 50.1 {
		t.Fatalf("CPU%% 计算错误: %v", c.CPUPct)
	}
	if c.MemPct < 9.9 || c.MemPct > 10.1 {
		t.Fatalf("内存%% 计算错误: %v", c.MemPct)
	}
	if !strings.Contains(c.MemUsage, "100.0 MiB / 1000.0 MiB") {
		t.Fatalf("内存展示错误: %s", c.MemUsage)
	}
}

func TestContainerMetrics(t *testing.T) {
	startFakeEngine(t)
	m, err := ContainerMetricsOf(cID)
	if err != nil {
		t.Fatal(err)
	}
	if m.At <= 0 {
		t.Fatal("采样时间缺失")
	}
	if m.CPUPercent < 49.9 || m.CPUPercent > 50.1 {
		t.Fatalf("CPU%% 计算错误: %v", m.CPUPercent)
	}
	// 内存取工作集：usage(100 MiB) 减 cache(10 MiB)
	if m.MemUsed != 104857600-10485760 {
		t.Fatalf("内存工作集错误: %d", m.MemUsed)
	}
	if m.MemCache != 10485760 || m.MemLimit != 1048576000 {
		t.Fatalf("缓存/限额错误: %+v", m)
	}
	// blkio 只累加 read/write，Sync 这类条目不计入
	if m.DiskRead != 4096 || m.DiskWrite != 8192 {
		t.Fatalf("磁盘IO 聚合错误: %+v", m)
	}
	// 多网卡求和
	if m.NetRx != 2148 || m.NetTx != 1224 {
		t.Fatalf("网络IO 聚合错误: %+v", m)
	}
	if m.Pids != 12 {
		t.Fatalf("进程数错误: %d", m.Pids)
	}
}

func TestFindAndInspect(t *testing.T) {
	startFakeEngine(t)
	id, found, err := FindContainerByLabel("mysql", "inst-1")
	if err != nil || !found {
		t.Fatalf("按标签查找失败: %v %v", found, err)
	}
	if !strings.HasPrefix(id, exited) {
		t.Fatalf("容器 ID 错误: %s", id)
	}
	st, err := InspectContainer(cID)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Running || st.State != "running" || !strings.HasPrefix(st.Status, "Up ") {
		t.Fatalf("inspect 状态错误: %+v", st)
	}
	if up := ContainerUptime(cID); up < 3500 {
		t.Fatalf("uptime 应约 1 小时, 实际 %d", up)
	}
}

func TestCreateContainerPayload(t *testing.T) {
	startFakeEngine(t)
	id, err := CreateContainer(ContainerSpec{
		Name:          "go-tools-mysql-inst-9",
		Image:         "mysql:8.4",
		Env:           []string{"MYSQL_ROOT_PASSWORD=pw"},
		Binds:         []string{"C:/data/mysql:/var/lib/mysql"},
		Labels:        map[string]string{LabelTool: "mysql", LabelInstance: "inst-9"},
		ContainerPort: 3306,
		HostPort:      3307,
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != createID {
		t.Fatalf("返回 ID 错误: %s", id)
	}
	rec.mu.Lock()
	body, name := rec.createBody, rec.createName
	rec.mu.Unlock()
	if name != "go-tools-mysql-inst-9" {
		t.Fatalf("create 缺少 name 参数: %s", name)
	}
	for _, want := range []string{`"Image":"mysql:8.4"`, `"HostPort":"3307"`, `"go-tools":"mysql"`, `C:/data/mysql:/var/lib/mysql`, `"Env":["MYSQL_ROOT_PASSWORD=pw"]`, `"3306/tcp"`, `"RestartPolicy"`} {
		if !strings.Contains(body, want) {
			t.Errorf("create 请求体缺少 %s\n实际: %s", want, body)
		}
	}
	if err := StartContainer(strings.Repeat("d", 64)); err != nil {
		t.Fatalf("启动容器应成功: %v", err)
	}
}

func TestListImagesAndExists(t *testing.T) {
	startFakeEngine(t)
	images, err := ListImages()
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 2 || images[0].Repo != "mysql" || images[0].Tag != "8.4" {
		t.Fatalf("镜像列表错误: %+v", images)
	}
	if !ImageExists("mysql:8.4") || ImageExists("mysql:9.9") {
		t.Fatal("ImageExists 判定错误")
	}
	if repo, tag := splitRepoTag("mysql"); repo != "mysql" || tag != "latest" {
		t.Fatalf("无 tag 镜像名解析错误: %s %s", repo, tag)
	}
	if repo, tag := splitRepoTag("registry.example.com:5000/mysql:8.4"); repo != "registry.example.com:5000/mysql" || tag != "8.4" {
		t.Fatalf("带仓库端口镜像名解析错误: %s %s", repo, tag)
	}
}

func TestPullProgressEvents(t *testing.T) {
	startFakeEngine(t)
	var got []PullProgress
	SetEmitter(func(name string, data ...any) {
		if name == "docker:pull" {
			got = append(got, data[0].(PullProgress))
		}
	})
	defer SetEmitter(nil)
	if err := PullImage("mysql:8.4"); err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	q := rec.pullQuery
	rec.mu.Unlock()
	if !strings.Contains(q, "fromImage=mysql") || !strings.Contains(q, "tag=8.4") {
		t.Fatalf("pull 参数错误: %s", q)
	}
	if len(got) < 5 {
		t.Fatalf("进度事件过少: %v", got)
	}
	if got[0].Stage != "info" || got[len(got)-1].Stage != "done" {
		t.Fatalf("首尾 stage 错误: %v", got)
	}
	foundPct := false
	for _, g := range got {
		if g.Percent == 50 {
			foundPct = true
		}
	}
	if !foundPct {
		t.Fatalf("未换算出 50%% 进度: %v", got)
	}
}

func TestGetContainerLogsDemux(t *testing.T) {
	startFakeEngine(t)
	lines, err := GetContainerLogs(cID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lines, []string{"hello stdout", "boom stderr"}) {
		t.Fatalf("日志解帧结果错误: %#v", lines)
	}
}

func TestStreamContainerLogs(t *testing.T) {
	startFakeEngine(t)
	var mu sync.Mutex
	var events []ContainerLogLine
	SetEmitter(func(name string, data ...any) {
		if name == "docker:serverlog" {
			mu.Lock()
			events = append(events, data[0].(ContainerLogLine))
			mu.Unlock()
		}
	})
	defer SetEmitter(nil)
	if err := StreamContainerLogs(cID); err != nil {
		t.Fatal(err)
	}
	defer StopContainerLogs(cID)
	if err := StreamContainerLogs(cID); err != nil { // 幂等：重复开启不产生第二条流
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(ContainerLogRing(cID, 50)) >= 3 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	ring := ContainerLogRing(cID, 50)
	if len(ring) < 3 {
		t.Fatalf("环形缓冲应有 3 行, 实际 %d: %#v", len(ring), ring)
	}
	for _, key := range []string{"hello stdout", "boom stderr", "later line"} {
		found := false
		for _, l := range ring {
			if strings.HasSuffix(l, key) {
				found = true
			}
		}
		if !found {
			t.Errorf("缺少日志行 %q, ring=%#v", key, ring)
		}
	}
	mu.Lock()
	n := len(events)
	mu.Unlock()
	if n < 3 {
		t.Fatalf("事件推送过少: %d", n)
	}
	StopContainerLogs(cID)
	if ContainerLogRing(cID, 10) != nil {
		t.Fatal("停止跟随后环形缓冲应随之释放")
	}
}

func TestNotFoundSurfaced(t *testing.T) {
	startFakeEngine(t)
	_, err := InspectContainer("missing")
	if err == nil || !strings.Contains(err.Error(), "No such container") {
		t.Fatalf("404 应透传消息, 实际: %v", err)
	}
}

func TestParseDOCKERHOST(t *testing.T) {
	cases := map[string]string{
		`npipe:////./pipe/docker_engine`: `\\.\pipe\docker_engine`,
		`npipe:////./pipe/custom-engine`: `\\.\pipe\custom-engine`,
	}
	for in, want := range cases {
		ep, err := parseDOCKERHOST(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if ep.network != "pipe" || ep.addr != want {
			t.Fatalf("%s → %q, 期望 %q", in, ep.addr, want)
		}
	}
	ep, err := parseDOCKERHOST("tcp://192.168.1.10:2375")
	if err != nil || ep.network != "tcp" || ep.addr != "192.168.1.10:2375" {
		t.Fatalf("tcp 端点解析错误: %+v %v", ep, err)
	}
	if ep, err = parseDOCKERHOST("tcp://192.168.1.10"); err != nil || ep.addr != "192.168.1.10:2375" {
		t.Fatalf("tcp 缺省端口应为 2375: %+v %v", ep, err)
	}
	if _, err := parseDOCKERHOST("unix:///var/run/docker.sock"); err == nil {
		t.Fatal("Windows 应拒绝 unix:// 端点")
	}
}

// pullHint 针对的是 daemon 回 500 + registry 超时这种对用户没有信息量的原文：
// 必须归到“引擎出不去网”这个真原因上，并给出代理/加速器的下一步。
func TestPullHint(t *testing.T) {
	t.Run("registry 超时", func(t *testing.T) {
		raw := fmt.Errorf(`Docker API 错误(500): Get "https://registry-1.docker.io/v2/": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`)
		msg := pullHint(raw).Error()
		for _, kw := range []string{"引擎连不上镜像仓库", "加速站", "Proxies"} {
			if !strings.Contains(msg, kw) {
				t.Errorf("提示缺少 %q：%s", kw, msg)
			}
		}
		if !strings.Contains(msg, "context deadline exceeded") {
			t.Errorf("原文要保留便于排查：%s", msg)
		}
	})
	t.Run("镜像名不存在", func(t *testing.T) {
		msg := pullHint(fmt.Errorf("Docker API 错误(404): manifest unknown")).Error()
		if !strings.Contains(msg, "找不到这个镜像或 tag") || strings.Contains(msg, "引擎连不上") {
			t.Errorf("归类错误：%s", msg)
		}
	})
	t.Run("限流", func(t *testing.T) {
		msg := pullHint(fmt.Errorf("拉取失败: toomanyrequests: You have reached your pull rate limit")).Error()
		if !strings.Contains(msg, "限流") {
			t.Errorf("应归为限流：%s", msg)
		}
	})
	t.Run("认不出的原样返回", func(t *testing.T) {
		raw := fmt.Errorf("Docker API 错误(400): invalid reference format")
		if got := pullHint(raw); got != raw {
			t.Errorf("不该改写：%v", got)
		}
		if pullHint(nil) != nil {
			t.Error("nil 应原样返回 nil")
		}
	})
}

func TestDaemonUnavailableMessage(t *testing.T) {
	// 未启动引擎时（管道不存在）应给出可操作提示，而不是裸系统错误
	saved := os.Getenv("DOCKER_HOST")
	os.Setenv("DOCKER_HOST", "tcp://127.0.0.1:1")
	defer os.Setenv("DOCKER_HOST", saved)
	// 端点已缓存，这里直接构造一次拨号失败路径
	c, err := dialPipe(`\\.\pipe\nope-does-not-exist`)
	if err == nil {
		c.Close()
		t.Skip("本机意外存在该管道")
	}
	if msg := unavailableErr(err).Error(); !strings.Contains(msg, "无法连接 Docker 引擎") {
		t.Fatalf("拨号失败信息未归一: %s", msg)
	}
}
