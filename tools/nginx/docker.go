package nginx

// ============================================================
// Nginx 实例的 Docker 运行模式（经 tools/docker 的 Engine API）
//
// 与本地模式的对应关系：
//   - 服务器：容器里跑同一条 nginx，配置仍是实例目录下的 conf/nginx.conf，
//     整个 conf 目录挂到 /go-tools/conf，用 `-c /go-tools/conf/nginx.conf` 指过去；
//     面板里改配置 → 保存 → 「重载」，容器模式下重载 = 重启容器（见下）
//   - 站点：docRoot 挂到 /usr/share/nginx/html，实例目录的 html/ 挂到 /go-tools/html
//     （50x 兜底页），与本地模式的 root 一一对应
//   - 日志：<实例目录>/logs 挂到容器 /var/log/nginx，access.log/error.log 直接落宿主机，
//     因此 Logs / errTail / estimateRps 这些按文件工作的逻辑无需分叉
//   - 端口：容器内固定监听 80（以及 443），发布到 127.0.0.1:<cfg.Port> 与 <cfg.SSLPort>，
//     所以改端口不动配置文件，配置里的 listen 永远是容器端口
//   - 归属：容器打标签 go-tools=nginx / go-tools.instance=<id>，应用重启后凭标签找回
//
// 与本地模式的差异（如实告知，不做静默兜底）：
//   - tools/docker 没有 exec 接口，进不了容器，因此 `nginx -t` 与 `nginx -s reload` 都不可用：
//     校验靠「启动结果 + 容器日志」（起不来就是把 error.log 与容器输出摊给用户看），
//     生效靠「重启容器」（ReloadInstance 在容器模式即 RestartInstance）
//   - 版本由镜像 tag 决定：本机 nginx 版本库管不到容器实例，配置里的 Version 在容器模式清空
//   - master/worker 进程在容器 namespace 里，PID 与 worker 数不再按本机进程枚举（会误计本机 nginx.exe）
//
// 变更生效：创建容器时把规格（镜像/端口发布/挂载/命令/标签）的签名记在实例目录，
// 下次启动不一致即重建容器（站点与日志都在宿主机目录，重建不丢数据）。
// ============================================================

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"go-tools/tools/docker"
	"go-tools/tools/srvstate"
)

const (
	// ModeLocal 本地模式（默认，历史配置无 mode 字段即此值）
	ModeLocal = "local"
	// ModeDocker 容器模式
	ModeDocker = "docker"
	// 容器内固定端口：与实例端口靠端口发布对接，不写进 nginx.conf
	containerPortHTTP  = 80
	containerPortHTTPS = 443
	// 容器内挂载点
	containerConfDir  = "/go-tools/conf"
	containerHTMLDir  = "/usr/share/nginx/html"
	containerFallback = "/go-tools/html"
	// DefaultDockerImage 表单默认镜像：官方 alpine 版自带 mime.types 与 /var/log/nginx 目录约定
	DefaultDockerImage = "nginx:alpine"
	// dockerConfName 容器专用配置模板落地的文件名（就是实例的 nginx.conf，与本地同名同位置）
	dockerSpecName = "nginx-docker.container"
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
	return "go-tools-nginx-" + cfg.ID
}

// dockerDocRoot 站点根目录（宿主绝对路径，确保存在）：与本地模式同一套默认值，
// 因此本地实例切成容器后，已有的站点文件直接就能被容器读到
func dockerDocRoot(cfg *InstanceConfig) (string, error) {
	dir := strings.TrimSpace(cfg.DocRoot)
	if dir == "" {
		dir = defaultDocRoot(cfg.ID, cfg.Name)
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

// dockerBinds 挂载表：配置目录、日志目录、站点根、50x 兜底页目录。
// 挂的是 <实例目录>/conf 本身（不是实例目录），因此宿主机 nginx.conf 直接就是
// 容器里的 /go-tools/conf/nginx.conf，证书与面板里编辑的文件是同一个
func dockerBinds(prefix, docRoot string) []string {
	confDir := filepath.Join(prefix, "conf")
	return []string{
		filepath.ToSlash(confDir) + ":" + containerConfDir,
		filepath.ToSlash(filepath.Join(prefix, "logs")) + ":/var/log/nginx",
		filepath.ToSlash(docRoot) + ":" + containerHTMLDir,
		filepath.ToSlash(filepath.Join(prefix, "html")) + ":" + containerFallback,
	}
}

// dockerArgsOf 容器内 nginx 的启动参数
// -g 'daemon off;' 让 nginx 留在前台（官方镜像的默认 CMD 也是这一条，覆盖 Cmd 后必须自己带上）
func dockerArgsOf() []string {
	return []string{"nginx", "-g", "daemon off;", "-c", containerConfDir + "/nginx.conf"}
}

// dockerSpecOf 由实例配置生成容器创建参数：双监听（HTTP + HTTPS）用 ExtraPorts 带上第二个端口
func dockerSpecOf(cfg *InstanceConfig, prefix, docRoot string) docker.ContainerSpec {
	spec := docker.ContainerSpec{
		Name:          containerNameOf(cfg),
		Image:         strings.TrimSpace(cfg.Image),
		Binds:         dockerBinds(prefix, docRoot),
		Labels:        map[string]string{docker.LabelTool: "nginx", docker.LabelInstance: cfg.ID},
		ContainerPort: containerPortHTTP,
		HostPort:      cfg.Port,
		HostIP:        "127.0.0.1",
		Cmd:           dockerArgsOf(),
	}
	if cfg.SSLPort > 0 {
		spec.ExtraPorts = []docker.PortPublish{{Container: containerPortHTTPS, Host: cfg.SSLPort}}
	}
	return spec
}

// dockerConfText 容器内的 nginx.conf。
// 与本地模板的三点差异：listen 用容器端口、路径全指挂载点、mime.types/fastcgi_params 取镜像内置那份。
// 其余（站点根、index、try_files、stub_status、HTTPS）与本地模式同一套结构，保证两种模式行为一致
func dockerConfText(cfg *InstanceConfig) string {
	workers := "auto"
	if cfg.Workers > 0 {
		workers = fmt.Sprintf("%d", cfg.Workers)
	}
	var b strings.Builder
	b.WriteString(confMarkerDocker + "\n")
	fmt.Fprintf(&b, "worker_processes  %s;\n", workers)
	// 日志写挂载到宿主机的 /var/log/nginx，面板按文件读，docker logs 也留着兜底
	b.WriteString("error_log  /var/log/nginx/error.log warn;\n")
	b.WriteString("pid        /var/run/nginx.pid;\n")
	b.WriteString("events {\n    worker_connections  1024;\n}\n")
	b.WriteString("http {\n")
	b.WriteString("    access_log      /var/log/nginx/access.log;\n")
	// mime.types 在镜像里（本机的 nginxConfSrcDir 在容器内不存在，不能沿用）
	b.WriteString("    include       /etc/nginx/mime.types;\n")
	b.WriteString("    default_type  application/octet-stream;\n")
	b.WriteString("    sendfile        on;\n")
	b.WriteString("    keepalive_timeout  65;\n")
	// 临时目录指镜像自带的位置（宿主 temp 目录挂进 Linux 容器没意义，且权限更易踩坑）
	b.WriteString("    client_body_temp_path /var/cache/nginx/client_temp;\n")
	b.WriteString("    proxy_temp_path       /var/cache/nginx/proxy_temp;\n")
	b.WriteString("    fastcgi_temp_path     /var/cache/nginx/fastcgi_temp;\n")
	b.WriteString("    uwsgi_temp_path       /var/cache/nginx/uwsgi_temp;\n")
	b.WriteString("    scgi_temp_path        /var/cache/nginx/scgi_temp;\n")
	b.WriteString("    server {\n")
	fmt.Fprintf(&b, "        listen       %d;\n", containerPortHTTP)
	fmt.Fprintf(&b, "        server_name  %s;\n", serverNameOf(cfg.Domain))
	fmt.Fprintf(&b, "        root         \"%s\";\n", containerHTMLDir)
	b.WriteString("        index  index.html index.php index.htm;\n")
	b.WriteString("        location / {\n            try_files $uri $uri/ /index.php?$query_string;\n        }\n")
	// PHP 跟本地模式同一结构，但容器里的 127.0.0.1 不是宿主机的 127.0.0.1：
	// 本工具不隐式把容器接到宿主 PHP-FPM（要监听 0.0.0.0 + host.docker.internal，属于用户环境决策），
	// 因此在生成的配置里写明改法，而不是默默生成一个 502 的 fastcgi_pass
	b.WriteString("        # PHP-FPM：容器内没有 fpm，需要站点跑 PHP 时把下行改成\n")
	b.WriteString("        # host.docker.internal:<端口>（同时让 PHP-FPM 监听 0.0.0.0）\n")
	b.WriteString("        location ~ \\.php$ {\n")
	b.WriteString("            fastcgi_pass   127.0.0.1:9000;\n")
	b.WriteString("            fastcgi_index  index.php;\n")
	b.WriteString("            fastcgi_param  SCRIPT_FILENAME $document_root$fastcgi_script_name;\n")
	b.WriteString("            include        /etc/nginx/fastcgi_params;\n        }\n")
	if cfg.StubStatus {
		b.WriteString("        location = /stub_status {\n            stub_status;\n            access_log off;\n        }\n")
	}
	b.WriteString("        error_page   500 502 503 504  /50x.html;\n")
	fmt.Fprintf(&b, "        location = /50x.html {\n            root   \"%s\";\n        }\n", containerFallback)
	b.WriteString("    }\n")
	if cfg.SSLPort > 0 {
		// 证书按本地模式的约定放在实例 conf 目录里，随挂载出现在容器内
		b.WriteString("    server {\n")
		fmt.Fprintf(&b, "        listen       %d ssl;\n", containerPortHTTPS)
		fmt.Fprintf(&b, "        server_name  %s;\n", serverNameOf(cfg.Domain))
		b.WriteString("        ssl_certificate      " + containerConfDir + "/server.crt;\n")
		b.WriteString("        ssl_certificate_key  " + containerConfDir + "/server.key;\n")
		fmt.Fprintf(&b, "        root         \"%s\";\n", containerHTMLDir)
		b.WriteString("        location / {\n            try_files $uri $uri/ =404;\n        }\n")
		b.WriteString("    }\n")
	}
	b.WriteString("}\n")
	return b.String()
}

const confMarkerDocker = "# go-tools 为 Docker 实例生成的 nginx.conf（listen 用容器端口，改端口请改实例配置）"

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

// ensureDockerConf 配置不存在则按实例参数生成；存在且未被用户接管时跟随实例参数刷新；
// 用户改过就从此不再动（与本地模式 ensureConf 同等待遇）。
// 刷新这一步是必需的：开启/关闭 HTTPS 会增减一个 443 ssl server 段，
// 配置不跟着变的话，要么端口发了没人监听，要么没有证书时 nginx 直接起不来
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

// sslListenRe 配置里是否存在 443 ssl 监听（容器内 HTTPS 入口）
var sslListenRe = regexp.MustCompile(`(?m)^\s*listen\s+443\s+ssl|^\s*listen\s+ssl\s+443`)

// dockerPrecheck 启动前的宿主机侧检查：把「容器里 nginx 一定起不来」的情况提前说清楚
func dockerPrecheck(cfg *InstanceConfig, prefix string) error {
	if cfg.SSLPort <= 0 {
		return nil
	}
	confPath := filepath.Join(prefix, "conf", "nginx.conf")
	var missing []string
	for _, f := range []string{"server.crt", "server.key"} {
		if st, err := os.Stat(filepath.Join(prefix, "conf", f)); err != nil || st.IsDir() {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("HTTPS 端口 %d 已开启，但实例 conf 目录缺少证书：%s（把文件放进 %s 后重试）",
			cfg.SSLPort, strings.Join(missing, "、"), filepath.Join(prefix, "conf"))
	}
	// 证书齐了但配置里没有 ssl 监听：端口会发布出去但没人服务（配置已被用户接管时不会自动补）
	if data, err := os.ReadFile(confPath); err == nil && !sslListenRe.Match(data) {
		return fmt.Errorf("HTTPS 端口 %d 已开启，但 %s 里没有 443 的 ssl server 段，发布的端口不会有人服务\n"+
			"在配置里补上（证书路径用 %s/server.crt 与 %s/server.key），或删掉该文件让工具重新生成",
			cfg.SSLPort, confPath, containerConfDir, containerConfDir)
	}
	return nil
}

// dockerContainerID 优先取进程表，退回按标签查找（应用重启后仍能操作旧容器）
func dockerContainerID(cfg *InstanceConfig) string {
	if p := trackedSrv(cfg.ID); p != nil && p.containerID != "" {
		return p.containerID
	}
	id, found, err := docker.FindContainerByLabel("nginx", cfg.ID)
	if err != nil || !found {
		return ""
	}
	return id
}

// specSignature 容器规格签名：用于判断已有容器是否还跟得上当前配置。
// nginx.conf 由用户接管，不计入签名（改配置只需重启容器，不必重建）；
// 但镜像、端口发布、挂载点（站点根 / 实例目录）变了就必须重建
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
	confPath := filepath.Join(prefix, "conf", "nginx.conf")
	if err := ensureDockerConf(cfg, confPath); err != nil {
		return fmt.Errorf("准备 nginx.conf 失败: %v", err)
	}
	if err := dockerPrecheck(cfg, prefix); err != nil {
		return err
	}
	spec := dockerSpecOf(cfg, prefix, docRoot)
	sig := specSignature(spec)

	// 镜像缺失则先拉取（首次可能几分钟，进度经 "docker:pull" 事件推送）
	if err := docker.EnsureImage(image); err != nil {
		return fmt.Errorf("准备镜像 %s 失败: %v", image, err)
	}

	id, found, err := docker.FindContainerByLabel("nginx", cfg.ID)
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

	p := &srvRunning{containerID: id, done: make(chan struct{}), started: time.Now()}
	srvRunMu.Lock()
	srvProcs[cfg.ID] = p
	srvRunMu.Unlock()
	watchDockerExit(cfg, id, p)

	// nginx 配置报错时容器会立刻退出，等端口之外还要盯容器真实状态：
	// Docker 的端口转发在容器启动瞬间就可能先把端口打开，只看 tcpUp 会把「起来就退」误报成成功
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-p.done:
			return fmt.Errorf("容器已退出，请查看日志：\n%s", dockerFailTail(id, prefix))
		default:
		}
		cs, err := docker.InspectContainer(id)
		if err == nil && !cs.Running && !cs.Restarting {
			forgetSrv(cfg.ID, p)
			return fmt.Errorf("容器已退出（状态 %s，退出码 %d），请查看日志：\n%s",
				cs.State, cs.ExitCode, dockerFailTail(id, prefix))
		}
		if tcpUp(cfg.Port) && (err != nil || cs.Running) {
			p.MarkReady()
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("启动超时：60s 内端口 %d 未监听，请查看日志：\n%s", cfg.Port, dockerFailTail(id, prefix))
}

// dockerFailTail 起不来时能拿到的证据：宿主侧 error.log 尾部 + 容器输出
func dockerFailTail(id, prefix string) string {
	parts := []string{}
	if t := errTail(prefix, 12); t != "" {
		parts = append(parts, t)
	}
	if logs := dockerLogTail(id, 12); len(logs) > 0 {
		parts = append(parts, "—— 容器输出 ——\n"+strings.Join(logs, "\n"))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}

// watchDockerExit 跟踪容器退出（与本地模式的 cmd.Wait 语义对齐：退出即从进程表摘除）。
// 引擎一时问不到（管道忙/熔断/自更新）时由共享看护退避重试，不误判为退出。
func watchDockerExit(cfg *InstanceConfig, containerID string, p *srvRunning) {
	docker.WatchContainerExit(containerID, func() {
		// 就绪后非主动停止的退出＝容器崩了：发事件让主程序弹系统通知
		if p.Abnormal() {
			srvEmit("nginx"+srvstate.EventSuffix, srvstate.NewEvent("nginx", "docker", cfg.ID, cfg.Name, cfg.Port, -1))
		}
		forgetSrv(cfg.ID, p)
	})
}

// forgetSrv 从进程表摘除该实例并关 done 通道（幂等：只有仍指向同一对象时才删）
func forgetSrv(instID string, p *srvRunning) {
	srvRunMu.Lock()
	if srvProcs[instID] == p {
		delete(srvProcs, instID)
	}
	srvRunMu.Unlock()
	select {
	case <-p.done:
	default:
		close(p.done)
	}
}

// dockerStop 停止容器（保留容器本体，下次启动复用）：优先进程表，退回按标签查找
func dockerStop(cfg *InstanceConfig) error {
	id := dockerContainerID(cfg)
	if id == "" {
		return fmt.Errorf("未找到该实例对应的容器（可能已被删除）")
	}
	// 先标记主动停止：停容器会触发退出回调，不发「异常退出」通知
	if p := trackedSrv(cfg.ID); p != nil {
		p.MarkStopping()
	}
	if err := docker.StopContainer(id, 10); err != nil {
		return fmt.Errorf("停止容器失败: %v", err)
	}
	if p := trackedSrv(cfg.ID); p != nil {
		forgetSrv(cfg.ID, p)
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

// dockerReload 容器模式的「重载配置」：先停掉已有容器（未运行或已被外部删掉都当已停），再起。
// 端口仍监听时不能假称成功：旧容器还在跑，新配置并不会生效
func dockerReload(cfg *InstanceConfig) error {
	if trackedSrv(cfg.ID) != nil || dockerContainerID(cfg) != "" {
		if err := dockerStop(cfg); err != nil && tcpUp(cfg.Port) {
			return err
		}
	}
	return dockerStart(cfg)
}

// dockerRemove 删除容器（实例目录与站点目录保留，与本地模式删除实例的语义一致）
func dockerRemove(cfg *InstanceConfig) error {
	prefix, _ := instanceDir(cfg.ID)
	if prefix != "" {
		_ = os.Remove(specSigPath(prefix)) // 规格签名随容器一起作废
	}
	id := dockerContainerID(cfg)
	if id == "" {
		return nil // 从未起过容器
	}
	// 删容器会触发退出回调：先标记主动操作，避免误报「异常退出」
	if p := trackedSrv(cfg.ID); p != nil {
		p.MarkStopping()
	}
	if err := docker.RemoveContainer(id, true); err != nil {
		return fmt.Errorf("删除容器失败: %v", err)
	}
	return nil
}

func dockerLogTail(id string, n int) []string {
	lines, err := docker.GetContainerLogs(id, n)
	if err != nil {
		return []string{err.Error()}
	}
	return lines
}

// dockerLogsBlend 容器实例日志：主用挂载到宿主机 logs 目录里的 access/error.log
// （与本地模式同一份数据，按文件读的逻辑不用分叉）；文件还没落下来时补一段容器输出
func dockerLogsBlend(cfg *InstanceConfig, prefix string, n int) ([]string, error) {
	out, err := logFilesOf(prefix, n)
	if err != nil {
		return nil, err
	}
	if len(out) > 0 {
		return out, nil
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

// fillDockerStatus 补充 Docker 模式的状态字段。
// PID 与 worker 数都不按本机进程取：容器里的 nginx 进程在 VM namespace，
// 而本机 nginx.exe 可能是别的实例，报出去只会误导
func fillDockerStatus(cfg *InstanceConfig, st *InstanceStatus) {
	st.Mode = ModeDocker
	st.Image = strings.TrimSpace(cfg.Image)
	st.Container = containerNameOf(cfg)
	st.Version = displayVersionOf(cfg)
	st.Workers = 0
	st.PID = 0
	id := dockerContainerID(cfg)
	if id == "" {
		return
	}
	st.ContainerID = id
	if cs, err := docker.InspectContainer(id); err == nil {
		st.ContainerState = cs.State
		if cs.Running {
			st.UptimeSeconds = cs.UptimeSeconds
		}
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

// DockerImages 本地 nginx 系镜像 tag（表单下拉候选）
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
