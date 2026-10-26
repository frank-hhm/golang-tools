package sysx

import (
	"bufio"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ============================================================
// 系统层：摸清"这台机器上已经装了什么"
//
// 与 Pkg（自带绿色版那套目录约定）互补：Pkg 管的是我们下载/解包进
// plugins 的版本，这里问的是操作系统自己装的东西。Linux 上四个服务
// 都没有官方便携二进制（PHP、Redis 尤其如此），只能走系统包，所以
// 这条路径才是 Linux 侧的安装正路。
// ============================================================

// PkgCandidate 一个"系统包 → 可安装版本"的查询结果。
//
// 定义在公共文件里：Linux 用它汇报发行版仓库的答案，Windows 侧永远拿到空
// 切片（没有"系统包"这个概念），但两边都要能编译通过。
type PkgCandidate struct {
	Pkg     string // 包名，如 mysql-server
	Version string // 仓库里的版本号；空表示该包在仓库里查不到
}

// PkgScheme 标记“这个版本来自系统包管理器”而不是可下载归档。
//
// 工具包把“可安装版本”统一表示为 RemoteVersion{Version,URL}：Windows 上
// URL 是 zip 地址，Linux 上则写成 pkg:<包名>。安装时认前缀就能分清该下载
// 还是该调包管理器，前端又能原样展示版本号，不必给两套结构。
const PkgScheme = "pkg:"

// VersionProbeTimeout 探版本号时给二进制的超时。
//
// mysqld --version 是秒回的，但万一 PATH 里那个同名文件是个脚本或卡住的
// 包装器，没有超时会把 ListVersions 整个拖住。
const VersionProbeTimeout = 5 * time.Second

// Which 在 PATH 与系统标准目录里找可执行文件，命中即返回绝对路径。
//
// 为什么不能只用 exec.LookPath：面板常被 systemd 或 root 的登录 shell 拉起，
// 这些环境的 PATH 与普通用户不同（/usr/local/sbin、/usr/sbin 经常缺席），
// 而 mysqld、nginx 恰恰只装在这类目录里。平台标准目录见 binSearchDirs。
func Which(names ...string) string {
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if p, err := exec.LookPath(name); err == nil && p != "" {
			return p
		}
		for _, dir := range binSearchDirs() {
			p := filepath.Join(dir, name)
			if executableFile(p) {
				return p
			}
		}
	}
	return ""
}

// verNumRe 匹配 x.y 或 x.y.z 形式的版本号。
//
// mysqld/redis-server/nginx/php 的 --version 文案各不相同，但第一段版本号
// 的位置规律一致，所以只在调用处决定要不要再校验。
var verNumRe = regexp.MustCompile(`\d+\.\d+(?:\.\d+)?`)

// TrimVersion 从任意文本里取第一个版本号（"Redis server v=7.0.15 sha=..." → "7.0.15"）。
func TrimVersion(s string) string { return verNumRe.FindString(s) }

// ProbeVersion 跑一次 "<bin> <args...>"，从合并输出里提取版本号，失败返回空串。
//
// 系统安装的版本没有"目录名"可当版本号用，只能问二进制自己。注意不能
// 因为非零退出码就放弃：nginx -v 把版本打到 stderr、部分包装器也会返回非 0
// 但输出有效，所以有输出就解析。
func ProbeVersion(bin string, args ...string) string {
	if strings.TrimSpace(bin) == "" {
		return ""
	}
	type result struct {
		out []byte
	}
	done := make(chan result, 1)
	go func() {
		out, _ := exec.Command(bin, args...).CombinedOutput()
		done <- result{out: out}
	}()
	select {
	case r := <-done:
		return TrimVersion(string(r.out))
	case <-time.After(VersionProbeTimeout):
		return ""
	}
}

// RunStreaming 同步执行命令，把 stdout/stderr 合并后逐行交给 onLine。
//
// 安装/卸载一跑就是几十秒到几分钟、输出上百行，整段读进内存既浪费又看不到
// 中间进度。onLine 传 nil 时等价于"只关心成功失败"。
func RunStreaming(timeout time.Duration, name string, args []string, env []string, onLine func(string)) error {
	cmd := exec.Command(name, args...)
	if len(env) > 0 {
		cmd.Env = append(cmd.Environ(), env...)
	}
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout // 单管道读，保证输出顺序
	if err := cmd.Start(); err != nil {
		return err
	}

	tails := make([]string, 0, 32) // 留尾部若干行，失败时能说清原因
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if onLine != nil {
			onLine(line)
		}
		tails = append(tails, line)
		if len(tails) > 20 {
			tails = tails[1:]
		}
	}

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	var err2 error
	if timeout > 0 {
		select {
		case err2 = <-waitErr:
		case <-time.After(timeout):
			_ = killTree(cmd.Process)
			return fmt.Errorf("%s 执行超时（%s）", filepath.Base(name), timeout)
		}
	} else {
		err2 = <-waitErr
	}
	if err2 != nil {
		tail := strings.Join(tails, "\n")
		if tail == "" {
			return fmt.Errorf("%s 失败: %w", filepath.Base(name), err2)
		}
		return fmt.Errorf("%s 失败: %w\n%s", filepath.Base(name), err2, tail)
	}
	return nil
}
