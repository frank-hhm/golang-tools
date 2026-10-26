//go:build linux

package sysx

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// binSearchDirs Linux 上系统包会把守护进程装进这些目录，而不一定进 PATH。
//
// 顺序有意义：/usr/local 前缀优先，与 shell 的 PATH 直觉一致。
func binSearchDirs() []string {
	return []string{"/usr/local/sbin", "/usr/local/bin", "/usr/sbin", "/usr/bin", "/sbin", "/bin"}
}

// executableFile Linux 上要真的带可执行位才算数（包里常有几个空壳占位）。
func executableFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Mode().Perm()&0o111 != 0
}

// PkgManager 返回可用的系统包管理器可执行文件，优先级 dnf > yum > apt-get。
//
// 返回空串表示"这台机器没有能用的包管理器"（精简镜像、Alpine 之类），
// 调用方应当据此如实告诉用户"请自行安装"，而不是抛一个语焉不详的失败。
func PkgManager() string {
	for _, name := range []string{"dnf", "yum", "apt-get"} {
		if p, err := exec.LookPath(name); err == nil && p != "" {
			return p
		}
	}
	return ""
}

// isAptManager 统一判断当前包管理器是不是 apt 系（查询与安装命令都不同）。
func isAptManager(mgr string) bool { return strings.HasSuffix(mgr, "apt-get") }

// dnfVersionRe 匹配 dnf/yum info 输出里的 Version 行（Epoch 那行也带冒号，所以锚定行首）。
var dnfVersionRe = regexp.MustCompile(`(?m)^Version\s*:\s*(\S+)`)

// aptCandidateRe 匹配 apt-cache policy 的 Candidate 行。
var aptCandidateRe = regexp.MustCompile(`(?m)^\s*Candidate:\s*(\S+)`)

// PackageCandidate 查询一个系统包在当前仓库里可安装的版本，查不到返回空串。
//
// 只用来给"在线安装"列表填版本号：Linux 上没有"某版本的 tar 包"这回事，
// 装哪个版本取决于发行版仓库，所以这里问的是仓库的答案。
func PackageCandidate(pkg string) string {
	mgr := PkgManager()
	if mgr == "" || strings.TrimSpace(pkg) == "" {
		return ""
	}
	if isAptManager(mgr) {
		out, err := exec.Command(mgr, "-qq", "policy", pkg).Output()
		if err != nil && len(out) == 0 {
			return ""
		}
		if m := aptCandidateRe.FindStringSubmatch(string(out)); len(m) == 2 {
			return m[1]
		}
		return ""
	}
	out, err := exec.Command(mgr, "-q", "info", pkg).Output()
	if err != nil && len(out) == 0 {
		return ""
	}
	if m := dnfVersionRe.FindStringSubmatch(string(out)); len(m) == 2 {
		return m[1]
	}
	return ""
}

// PackageHint 系统包候选全为空时给用户的下一步。
//
// 两种情况的返回值都是空列表，但动作完全不同：没有 apt-get/dnf/yum 的镜像
// （容器常见）得先装包管理器；有包管理器却查不到包，多半是软件源没启用。
// 工具包把这句话当成错误返回，前端就能直接把命令行展示出来。
func PackageHint(pkgs []string) string {
	names := strings.Join(pkgs, " / ")
	mgr := PkgManager()
	if mgr == "" {
		return fmt.Sprintf("未检测到 apt-get / dnf / yum，无法从发行版仓库取版本列表；请手工安装 %s 后刷新", names)
	}
	if len(pkgs) == 0 {
		return fmt.Sprintf("发行版仓库里没有可安装的包（管理器 %s）", filepath.Base(mgr))
	}
	return fmt.Sprintf("发行版仓库里没有 %s：请在服务器执行 sudo %s install -y %s 后刷新，或确认软件源已启用",
		names, filepath.Base(mgr), pkgs[len(pkgs)-1])
}

// PackageCandidates 按给定顺序批量查询（保持顺序，去掉查不到的）。
func PackageCandidates(pkgs []string) []PkgCandidate {
	out := make([]PkgCandidate, 0, len(pkgs))
	seen := make(map[string]bool, len(pkgs))
	for _, pkg := range pkgs {
		pkg = strings.TrimSpace(pkg)
		if pkg == "" || seen[pkg] {
			continue
		}
		seen[pkg] = true
		ver := PackageCandidate(pkg)
		if ver == "" {
			continue
		}
		out = append(out, PkgCandidate{Pkg: pkg, Version: ver})
	}
	return out
}

// InstallPackages 用系统包管理器安装指定的包，输出逐行交给 onLine。
//
// 必须 root：非 root 时这里会拿到明确的权限错误，由调用方原样展示。
// 不传 --refresh/update：仓库元数据过期是用户该决定的事，面板不擅自更新。
func InstallPackages(pkgs []string, onLine func(string)) error {
	mgr := PkgManager()
	if mgr == "" {
		return ErrUnsupported
	}
	if len(pkgs) == 0 {
		return fmt.Errorf("没有要安装的包")
	}
	args := []string{"install", "-y"}
	env := []string(nil)
	if isAptManager(mgr) {
		// apt 在非交互环境里会为 debconf 卡住等输入
		env = append(env, "DEBIAN_FRONTEND=noninteractive")
	}
	args = append(args, pkgs...)
	return RunStreaming(30*time.Minute, mgr, args, env, onLine)
}
