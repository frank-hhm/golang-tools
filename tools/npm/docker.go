package npm

// ============================================================
// 静态站点实例的 Docker 运行模式（经 tools/docker 的 Engine API）
//
// 为什么这一档值得有：内置 Go 静态服务器胜在零依赖，但它跑在本进程里 ——
// 站点与程序同生命周期，程序一退站点就停。容器模式改用 nginx:alpine 承载同一份
// 站点目录，端口/挂载/标签由本工具登记，进程隔离且可以被 Docker 自己拉起。
//
// 与本地模式的对应关系：
//   - 站点：docRoot 挂到 /usr/share/nginx/html，与内置服务器读的同一个目录，
//     本地实例切成容器后站点文件直接就能被容器读到（默认根仍是实例目录下的 www/）
//   - 配置：工具按实例参数生成 <实例目录>/conf/nginx.conf，挂到 /go-tools/conf，
//     用 `-c /go-tools/conf/nginx.conf` 指过去；容器里 listen 固定 80，
//     发布到 127.0.0.1:<cfg.Port>，所以改端口不动配置文件
//   - 日志：access.log / error.log 直接落宿主机 <实例目录>/logs，
//     面板按文件读，Logs 不必分叉；请求数与字节数也从这份日志统计回来
//   - 归属：容器打标签 go-tools=npm / go-tools.instance=<id>，应用重启后凭标签找回
//
// 与本地模式的差异（如实告知，不做静默兜底）：
//   - /__gonfo__ 状态探针是内置 Go 服务器实现的，容器里没有：改由 access.log 统计
//   - SPA 回退与列目录用 nginx 原生指令等价实现（try_files / autoindex），
//     行为对齐内置服务器：带扩展名的资源请求找不到就 404，不回退 index.html
//   - Node 版本与 npm registry 跟站点服务无关了：容器模式清空 Version，
//     展示版本从镜像 tag 推导（nginx:1.27.4-alpine → 1.27.4，取不到就显示镜像名）
//   - 站点在容器 namespace 里，本机 PID 不再有意义（报的是进程本体的 PID 会误导）
//
// 变更生效：创建容器时把规格（镜像/端口发布/挂载/命令/标签）的签名记在实例目录，
// 下次启动不一致即重建容器（站点与日志都在宿主机目录，重建不丢数据）。
// 生成配置跟随实例参数刷新：SPA / 列目录两个开关会改写 location 段，
// 已被用户改过的配置不再覆盖（见 confIsToolGenerated）。
// ============================================================

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-tools/tools/docker"
	"go-tools/tools/logtail"
)

const (
	// ModeLocal 本地模式（内置 Go 静态服务器；历史配置无 mode 字段即此值）
	ModeLocal = "local"
	// ModeDocker 容器模式（nginx:alpine 承载站点目录）
	ModeDocker = "docker"
	// 容器内固定监听端口：与实例端口靠端口发布对接，不写进 nginx.conf
	containerPortHTTP = 80
	// 容器内挂载点
	containerConfDir = "/go-tools/conf"
	containerHTMLDir = "/usr/share/nginx/html"
	// DefaultDockerImage 表单默认镜像：官方 alpine 版自带 mime.types 与 /var/log/nginx 约定
	DefaultDockerImage = "nginx:alpine"
	// dockerSpecName 容器规格签名文件名（落在实例目录）
	dockerSpecName = "npm-docker.container"
	// dockerLogFormat 生成的 access log 格式：末尾三段固定为 status / body_bytes_sent / request_time，
	// 状态面板按这个约定从行尾取数，$request 里有空格也不影响
	dockerLogFormat = "gonfo"
)

// isDockerMode 判定实例运行模式（零值/其他值一律按本地）
func isDockerMode(cfg *InstanceConfig) bool {
	return strings.EqualFold(strings.TrimSpace(cfg.Mode), ModeDocker)
}

// containerNameOf 容器名（留空则按实例 ID 生成，保证唯一且可反查）
func containerNameOf(cfg *InstanceConfig) string {
	if n := strings.TrimSpace(cfg.ContainerName); n != "" {
		return n
	}
	return "go-tools-npm-" + cfg.ID
}

// dockerDocRoot 站点根目录（宿主绝对路径，确保存在）：与本地模式同一套默认值，
// 因此本地实例切成容器后，已有的站点文件直接就能被容器读到
func dockerDocRoot(cfg *InstanceConfig) (string, error) {
	dir := strings.TrimSpace(cfg.DocRoot)
	if dir == "" {
		prefix, err := instanceDir(cfg.ID)
		if err != nil {
			return "", err
		}
		dir = filepath.Join(prefix, "www")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("站点根目录不可用: %s（%v）", dir, err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("站点根目录不合法: %s（%v）", dir, err)
	}
	return abs, nil
}

// dockerBinds 挂载表：配置目录、日志目录、站点根。
// 挂的是 <实例目录>/conf 本身（不是实例目录），宿主机生成的 nginx.conf 就是容器里的
// /go-tools/conf/nginx.conf，一个文件两处可见
func dockerBinds(prefix, docRoot string) []string {
	return []string{
		filepath.ToSlash(filepath.Join(prefix, "conf")) + ":" + containerConfDir,
		filepath.ToSlash(filepath.Join(prefix, "logs")) + ":/var/log/nginx",
		filepath.ToSlash(docRoot) + ":" + containerHTMLDir,
	}
}

// dockerArgsOf 容器内 nginx 的启动参数
// -g 'daemon off;' 让 nginx 留在前台（官方镜像默认 CMD 也是这条，覆盖 Cmd 后必须自己带上）
func dockerArgsOf() []string {
	return []string{"nginx", "-g", "daemon off;", "-c", containerConfDir + "/nginx.conf"}
}

// dockerSpecOf 由实例配置生成容器创建参数：静态站点只有 HTTP 一个端口
func dockerSpecOf(cfg *InstanceConfig, prefix, docRoot string) docker.ContainerSpec {
	return docker.ContainerSpec{
		Name:          containerNameOf(cfg),
		Image:         strings.TrimSpace(cfg.Image),
		Binds:         dockerBinds(prefix, docRoot),
		Labels:        map[string]string{docker.LabelTool: "npm", docker.LabelInstance: cfg.ID},
		ContainerPort: containerPortHTTP,
		HostPort:      cfg.Port,
		HostIP:        "127.0.0.1",
		Cmd:           dockerArgsOf(),
	}
}

// dockerConfText 容器内的 nginx.conf：把内置 Go 服务器的三种行为原样翻译成 nginx 指令
//   - SPA 回退：try_files $uri $uri/ /index.html，但带扩展名的路径不回退（与 isAssetPath 一致）
//   - 列目录：autoindex on（无 index.html 时目录可浏览；关掉时 nginx 同样回 403）
//   - 根目录：容器挂载点，不是宿主机路径（宿主机路径在 Linux 容器里没有意义）
func dockerConfText(cfg *InstanceConfig) string {
	var b strings.Builder
	b.WriteString(confMarkerDocker + "\n")
	b.WriteString("worker_processes  auto;\n")
	// 日志写挂载到宿主机的 /var/log/nginx，面板按文件读，docker logs 也留着兜底
	b.WriteString("error_log  /var/log/nginx/error.log warn;\n")
	b.WriteString("pid        /var/run/nginx.pid;\n")
	b.WriteString("events {\n    worker_connections  1024;\n}\n")
	b.WriteString("http {\n")
	// log_format 必须先于 access_log 引用，否则 nginx 解析时报 unknown log format
	b.WriteString("    log_format " + dockerLogFormat +
		" '$remote_addr - [$time_local] \"$request\" $status $body_bytes_sent $request_time';\n")
	b.WriteString("    access_log      /var/log/nginx/access.log " + dockerLogFormat + ";\n")
	// mime.types 在镜像里（本机插件目录的路径在容器内不存在，不能沿用）
	b.WriteString("    include       /etc/nginx/mime.types;\n")
	b.WriteString("    default_type  application/octet-stream;\n")
	b.WriteString("    sendfile        on;\n")
	b.WriteString("    keepalive_timeout  65;\n")
	// 临时目录指镜像自带的位置（宿主 temp 目录挂进 Linux 容器没意义，且权限更容易出问题）
	b.WriteString("    client_body_temp_path /var/cache/nginx/client_temp;\n")
	b.WriteString("    proxy_temp_path       /var/cache/nginx/proxy_temp;\n")
	b.WriteString("    fastcgi_temp_path     /var/cache/nginx/fastcgi_temp;\n")
	b.WriteString("    uwsgi_temp_path       /var/cache/nginx/uwsgi_temp;\n")
	b.WriteString("    scgi_temp_path        /var/cache/nginx/scgi_temp;\n")
	b.WriteString("    server {\n")
	fmt.Fprintf(&b, "        listen       %d;\n", containerPortHTTP)
	b.WriteString("        server_name  localhost;\n")
	fmt.Fprintf(&b, "        root         \"%s\";\n", containerHTMLDir)
	b.WriteString("        index  index.html index.htm;\n")
	if cfg.Directory {
		b.WriteString("        autoindex on;\n")
	}
	if cfg.SPA {
		// 资源请求（带扩展名）缺失就 404：回退 index.html 会让浏览器把 HTML 当 JS/CSS 解析
		b.WriteString("        location ~* \\.\\w+$ {\n            try_files $uri =404;\n        }\n")
		b.WriteString("        location / {\n            try_files $uri $uri/ /index.html;\n        }\n")
	} else {
		// 不回退：目录请求交给 index / autoindex，都不是就 404（与内置服务器同一结果）
		b.WriteString("        location / {\n            try_files $uri $uri/ =404;\n        }\n")
	}
	b.WriteString("        # /__gonfo__ 状态探针是内置 Go 服务器实现的，容器里没有这一条：\n")
	b.WriteString("        # 面板的请求数与流量按挂载出来的 access.log 统计\n")
	b.WriteString("    }\n")
	b.WriteString("}\n")
	return b.String()
}

const confMarkerDocker = "# go-tools 为 Docker 静态站点生成的 nginx.conf（listen 用容器端口，改端口请改实例配置）"

// dockerGenMarker 记录「上次由工具生成的那份配置」的哈希，与 nginx.conf 同目录。
// 只用来区分「还是工具生成的」与「已被用户接管」：前者可以跟随实例参数刷新，后者不再覆盖
func dockerGenMarker(confPath string) string { return confPath + ".go-tools-generated" }

func textHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// confIsToolGenerated 当前内容是否仍是工具生成的那一份（未被面板/手写修改过）
func confIsToolGenerated(confPath, current string) bool {
	mark, err := os.ReadFile(dockerGenMarker(confPath))
	return err == nil && strings.TrimSpace(string(mark)) == textHash(current)
}

// dockerConfPath 实例的容器配置路径（同时确保 conf 目录存在）
func dockerConfPath(prefix string) (string, error) {
	confDir := filepath.Join(prefix, "conf")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(confDir, "nginx.conf"), nil
}

// ensureDockerConf 配置不存在则按实例参数生成；存在且未被用户接管时跟随实例参数刷新；
// 用户改过就从此不再动。刷新是必需的：SPA / 列目录开关会改写 location 段，
// 配置不跟着变的话，面板上拨了开关但容器行为还是老的
func ensureDockerConf(cfg *InstanceConfig, confPath string) error {
	want := dockerConfText(cfg)
	data, err := os.ReadFile(confPath)
	if err != nil || len(data) == 0 {
		if err := os.WriteFile(confPath, []byte(want), 0o644); err != nil {
			return err
		}
		return os.WriteFile(dockerGenMarker(confPath), []byte(textHash(want)), 0o644)
	}
	cur := string(data)
	if cur == want || !confIsToolGenerated(confPath, cur) {
		return nil
	}
	if err := os.WriteFile(confPath, []byte(want), 0o644); err != nil {
		return err
	}
	return os.WriteFile(dockerGenMarker(confPath), []byte(textHash(want)), 0o644)
}

/* ============================ 容器跟踪 ============================ */

// dockerRun 容器实例的跟踪项（与内置 Go 服务器的 StaticServer 分开放：
// 后者带 http.Server 与日志文件句柄，容器实例套用会在这两处空指针）
type dockerRun struct {
	containerID string
	done        chan struct{}
	started     time.Time
}

var (
	dockerMu   sync.Mutex
	dockerRuns = map[string]*dockerRun{}
)

func trackedDocker(id string) *dockerRun {
	dockerMu.Lock()
	defer dockerMu.Unlock()
	return dockerRuns[id]
}

func forgetDocker(id string, r *dockerRun) {
	dockerMu.Lock()
	if dockerRuns[id] == r {
		delete(dockerRuns, id)
	}
	dockerMu.Unlock()
	select {
	case <-r.done:
	default:
		close(r.done)
	}
}

func dockerRunIDs() []string {
	dockerMu.Lock()
	defer dockerMu.Unlock()
	ids := make([]string, 0, len(dockerRuns))
	for id := range dockerRuns {
		ids = append(ids, id)
	}
	return ids
}

// dockerContainerID 优先取跟踪表，退回按标签查找（应用重启后仍能操作旧容器）
func dockerContainerID(cfg *InstanceConfig) string {
	if r := trackedDocker(cfg.ID); r != nil && r.containerID != "" {
		return r.containerID
	}
	id, found, err := docker.FindContainerByLabel("npm", cfg.ID)
	if err != nil || !found {
		return ""
	}
	return id
}

// specSignature 容器规格签名：用于判断已有容器是否还跟得上当前配置。
// 站点目录与开关都体现在挂载/生成的配置上，因此签名按创建参数整体计算：
// 改端口 / 换镜像 / 换站点根 / 换容器名都会重建容器（数据都在宿主机目录，重建不丢）
func specSignature(spec docker.ContainerSpec) string {
	data, err := json.Marshal(spec)
	if err != nil {
		return fmt.Sprintf("%v", spec)
	}
	return string(data)
}

func specSigPath(prefix string) string { return filepath.Join(prefix, dockerSpecName) }

// specMatchesLast 与上次创建容器时的规格是否一致（没记过就算不一致）
func specMatchesLast(prefix, sig string) bool {
	data, err := os.ReadFile(specSigPath(prefix))
	return err == nil && string(data) == sig
}

func recordSpec(prefix, sig string) {
	_ = os.WriteFile(specSigPath(prefix), []byte(sig), 0o644)
}

/* ============================ 生命周期 ============================ */

// dockerStart 创建（或复用）并启动容器，等待端口就绪
func dockerStart(cfg *InstanceConfig) error {
	image := strings.TrimSpace(cfg.Image)
	if image == "" {
		return fmt.Errorf("Docker 模式需指定镜像，如 %s", DefaultDockerImage)
	}
	prefix, err := instanceDir(cfg.ID)
	if err != nil {
		return err
	}
	docRoot, err := dockerDocRoot(cfg)
	if err != nil {
		return err
	}
	confPath, err := dockerConfPath(prefix)
	if err != nil {
		return err
	}
	if err := ensureDockerConf(cfg, confPath); err != nil {
		return fmt.Errorf("准备 nginx.conf 失败: %v", err)
	}
	spec := dockerSpecOf(cfg, prefix, docRoot)
	sig := specSignature(spec)

	// 镜像缺失则先拉取（首次可能几分钟，进度经 "docker:pull" 事件推送）
	if err := docker.EnsureImage(image); err != nil {
		return fmt.Errorf("准备镜像 %s 失败: %v", image, err)
	}

	id, found, err := docker.FindContainerByLabel("npm", cfg.ID)
	if err != nil {
		return err
	}
	if found {
		st, err := docker.InspectContainer(id)
		if err != nil {
			return err
		}
		if st.Running {
			return fmt.Errorf("容器已在运行")
		}
		if st.Image != image || st.HostPort != cfg.Port || !specMatchesLast(prefix, sig) {
			// 镜像/端口/挂载任一变更：重建容器（站点与日志都在宿主机目录，不受影响）
			if err := docker.RemoveContainer(id, true); err != nil {
				return fmt.Errorf("重建容器失败: %v", err)
			}
			found = false
		}
	}
	if !found {
		if id, err = docker.CreateContainer(spec); err != nil {
			return fmt.Errorf("创建容器失败: %v", err)
		}
		recordSpec(prefix, sig)
	}
	if err := docker.StartContainer(id); err != nil {
		return fmt.Errorf("启动容器失败: %v（容器日志：%s）", err, strings.Join(dockerLogTail(id, 6), " | "))
	}

	r := &dockerRun{containerID: id, done: make(chan struct{}), started: time.Now()}
	dockerMu.Lock()
	dockerRuns[cfg.ID] = r
	dockerMu.Unlock()
	watchDockerExit(cfg.ID, id, r)

	// nginx 配置报错时容器会立刻退出，光等端口不够：
	// Docker 的端口转发在容器启动瞬间就可能先把端口打开，只看 tcpUp 会把「起来就退」报成成功
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-r.done:
			return fmt.Errorf("容器已退出，请查看日志：\n%s", dockerFailTail(id, prefix))
		default:
		}
		cs, err := docker.InspectContainer(id)
		if err == nil && !cs.Running && !cs.Restarting {
			forgetDocker(cfg.ID, r)
			return fmt.Errorf("容器已退出（状态 %s，退出码 %d），请查看日志：\n%s",
				cs.State, cs.ExitCode, dockerFailTail(id, prefix))
		}
		if tcpUp(cfg.Port) && (err != nil || cs.Running) {
			if cfg.OpenBrowser {
				openURL(fmt.Sprintf("http://127.0.0.1:%d/", cfg.Port))
			}
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	forgetDocker(cfg.ID, r)
	return fmt.Errorf("启动超时：60s 内端口 %d 未监听，请查看日志：\n%s", cfg.Port, dockerFailTail(id, prefix))
}

// dockerStop 停止容器（保留容器本体，下次启动复用）
func dockerStop(cfg *InstanceConfig) error {
	id := dockerContainerID(cfg)
	if id == "" {
		return fmt.Errorf("未找到该实例对应的容器（可能已被删除）")
	}
	if err := docker.StopContainer(id, 10); err != nil {
		return fmt.Errorf("停止容器失败: %v", err)
	}
	if r := trackedDocker(cfg.ID); r != nil {
		forgetDocker(cfg.ID, r)
	}
	// 端口释放有延迟（Docker Desktop 的端口转发进程在容器停后才拆链接），
	// 刚下命令就查端口会把「已停干净」误报成「停止失败」
	for i := 0; i < 40 && tcpUp(cfg.Port); i++ {
		time.Sleep(200 * time.Millisecond)
	}
	if tcpUp(cfg.Port) {
		return fmt.Errorf("停止失败：端口 %d 仍在监听", cfg.Port)
	}
	return nil
}

// dockerReload 容器模式没有平滑重载通道（tools/docker 未提供 exec）：重载 = 重启容器
func dockerReload(cfg *InstanceConfig) error {
	if trackedDocker(cfg.ID) != nil || dockerContainerID(cfg) != "" {
		if err := dockerStop(cfg); err != nil && tcpUp(cfg.Port) {
			return err
		}
	}
	return dockerStart(cfg)
}

// dockerRemove 删除容器（实例目录与站点目录保留，与删除实例的语义一致）
func dockerRemove(cfg *InstanceConfig) error {
	if prefix, err := instanceDir(cfg.ID); err == nil {
		_ = os.Remove(specSigPath(prefix)) // 规格签名随容器一起作废
	}
	id := dockerContainerID(cfg)
	if id == "" {
		return nil // 从未起过容器
	}
	if err := docker.RemoveContainer(id, true); err != nil {
		return fmt.Errorf("删除容器失败: %v", err)
	}
	return nil
}

// watchDockerExit 跟踪容器退出（退出即从跟踪表摘除，与本地模式 Shutdown 语义对齐）。
// 引擎一时问不到（管道忙/熔断/自更新）时由共享看护退避重试，不误判为退出。
func watchDockerExit(instID, containerID string, r *dockerRun) {
	docker.WatchContainerExit(containerID, func() { forgetDocker(instID, r) })
}

/* ============================ 日志与状态 ============================ */

func dockerLogTail(id string, n int) []string {
	lines, err := docker.GetContainerLogs(id, n)
	if err != nil {
		return []string{err.Error()}
	}
	return lines
}

// dockerErrTail 宿主机侧 error.log 尾部（容器里 nginx 起不来的原因基本都在这里）
func dockerErrTail(prefix string, n int) string {
	return logtail.Tail(filepath.Join(prefix, "logs", "error.log"), n)
}

// dockerFailTail 起不来时能拿到的证据：宿主侧 error.log 尾部 + 容器输出
func dockerFailTail(id, prefix string) string {
	var parts []string
	if t := dockerErrTail(prefix, 12); t != "" {
		parts = append(parts, t)
	}
	if logs := dockerLogTail(id, 12); len(logs) > 0 {
		parts = append(parts, "—— 容器输出 ——\n"+strings.Join(logs, "\n"))
	}
	return strings.Join(parts, "\n")
}

// dockerLogsBlend 容器实例日志：主用挂载到宿主机 logs 目录里的 access.log
// （与本地模式同一个文件，按行尾读的逻辑不用分叉）；文件还没落下来时补一段容器输出
func dockerLogsBlend(cfg *InstanceConfig, prefix string, n int) ([]string, error) {
	lines, err := tailFile(filepath.Join(prefix, "logs", "access.log"), n)
	if err != nil {
		return nil, err
	}
	if len(lines) > 0 {
		return lines, nil
	}
	id := dockerContainerID(cfg)
	if id == "" {
		return nil, nil
	}
	logs := dockerLogTail(id, n)
	if len(logs) == 0 {
		return nil, nil
	}
	return append([]string{"—— 容器输出 ——"}, logs...), nil
}

// accessCursor 某个实例的 access.log 已读位置与累计值
type accessCursor struct {
	size     int64
	requests int64
	bytes    int64
}

var (
	accessMu      sync.Mutex
	accessCursors = map[string]*accessCursor{}
)

// accessCursorMax 缓存条目上限：正常等于实例个数，防实例被删后目录键无限积累
const accessCursorMax = 256

// dockerAccessStat 从 access.log 统计请求数与响应字节：
// 容器模式没有内置 Go 服务器的原子计数器，但日志格式末尾固定是
// status / body_bytes_sent / request_time，按行尾取数即可（$request 含空格也不影响）。
// 状态面板是轮询的，所以按偏移只读新增部分：不每轮重读整本日志，
// 也不把一段日志整体留在内存里（以前几百 MB 的 access.log 每次轮询都全量入堆）。
func dockerAccessStat(prefix string) (requests int64, bytes int64) {
	path := filepath.Join(prefix, "logs", "access.log")
	key := filepath.Clean(path)
	accessMu.Lock()
	defer accessMu.Unlock()
	cur := accessCursors[key]
	if cur == nil {
		if len(accessCursors) >= accessCursorMax {
			for k := range accessCursors {
				delete(accessCursors, k)
			}
		}
		cur = &accessCursor{}
		accessCursors[key] = cur
	}
	next, err := logtail.Scan(path, cur.size, func(line string) {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			return
		}
		cur.requests++
		// 倒数第二段是 body_bytes_sent；304/HEAD 会写成 "-"，跳过即可
		if v, perr := strconv.ParseInt(fields[len(fields)-2], 10, 64); perr == nil && v > 0 {
			cur.bytes += v
		}
	})
	if err != nil {
		// 读不动（日志被占用等）：拿已有的累计值继续展示，不让状态面板报错
		return cur.requests, cur.bytes
	}
	cur.size = next
	return cur.requests, cur.bytes
}

// fillDockerStatus 补充 Docker 模式的状态字段。
// PID 不报进程本体（那是 go-tools 自己，不是容器里的 nginx）；
// Node 相关字段在容器模式下没有意义，保持零值让面板显示占位
func fillDockerStatus(cfg *InstanceConfig, st *InstanceStatus, prefix string) {
	st.Mode = ModeDocker
	st.Image = strings.TrimSpace(cfg.Image)
	st.Container = containerNameOf(cfg)
	st.Version = displayVersionOf(cfg)
	st.PID = 0
	st.NodePath = ""
	st.NpmVer = ""
	st.Registry = ""
	id := dockerContainerID(cfg)
	if id != "" {
		st.ContainerID = id
		if cs, err := docker.InspectContainer(id); err == nil {
			st.ContainerState = cs.State
			if cs.Running {
				st.UptimeSeconds = cs.UptimeSeconds
			}
		}
	}
	if r := trackedDocker(cfg.ID); r != nil && st.UptimeSeconds == 0 {
		st.UptimeSeconds = int64(time.Since(r.started).Seconds())
	}
	st.Requests, st.Bytes = dockerAccessStat(prefix)
	if st.UptimeSeconds > 0 {
		st.RequestsPerSec = float64(st.Requests) / float64(st.UptimeSeconds)
	}
}

// displayVersionOf Docker 实例展示用的版本号：从镜像 tag 里取，取不到退回镜像名
func displayVersionOf(cfg *InstanceConfig) string {
	if v := versionFromImage(cfg.Image); v != "" {
		return v
	}
	if v := strings.TrimSpace(cfg.Version); v != "" {
		return v
	}
	return strings.TrimSpace(cfg.Image)
}

// versionFromImage 从镜像名里抽版本号（nginx:1.27.4-alpine → 1.27.4；取不到返回空）
func versionFromImage(image string) string {
	tag := image
	if i := strings.LastIndex(image, ":"); i >= 0 && !strings.Contains(image[i+1:], "/") {
		tag = image[i+1:]
	} else {
		return ""
	}
	var out []byte
	for _, ch := range tag {
		if (ch >= '0' && ch <= '9') || ch == '.' {
			out = append(out, byte(ch))
			continue
		}
		break
	}
	v := strings.Trim(string(out), ".")
	if !strings.Contains(v, ".") {
		return "" // "latest"、"alpine" 这类不算版本号，交给调用方回退镜像名
	}
	return v
}

/* ============================ 引擎与镜像 ============================ */

// DockerAvailable 引擎是否可用（前端据此提示，不阻断保存）
func DockerAvailable() (bool, string) {
	info, err := docker.GetEngineStatus()
	if err != nil {
		return false, err.Error()
	}
	if !info.Available {
		return false, info.Error
	}
	return true, ""
}

// DockerImages 本地 nginx 系镜像 tag（表单下拉候选：静态站点由 nginx 容器承载）
func DockerImages() ([]string, error) {
	images, err := docker.ListImages()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(images))
	for _, im := range images {
		if im.Tag == "" || im.Tag == "<none>" {
			continue
		}
		if strings.Contains(strings.ToLower(im.Repo), "nginx") {
			out = append(out, im.Repo+":"+im.Tag)
		}
	}
	return out, nil
}
