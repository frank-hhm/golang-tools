package gitcmd

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	binOnce sync.Once
	binPath string
	binErr  error
)

// bin 解析并缓存 git 可执行路径。找不到返回 ErrGitNotFound。
func bin() (string, error) {
	binOnce.Do(func() {
		p, err := exec.LookPath("git")
		if err != nil {
			binErr = ErrGitNotFound
			return
		}
		binPath = p
	})
	return binPath, binErr
}

var versionRe = regexp.MustCompile(`git version (\d+)\.(\d+)`)

// Check 探测本机 git：是否存在、版本是否达标。永不返回 error（结果在 Info 里），
// 让前端能拿到「未装 / 版本过旧」的具体原因去引导安装。
func Check(ctx context.Context) Info {
	p, err := bin()
	if err != nil {
		return Info{Found: false, OK: false, Reason: "未检测到 git 可执行文件，请安装 Git 并确保在 PATH 中"}
	}
	out, err := runRaw(ctx, "", p, "--version")
	if err != nil {
		return Info{Found: true, OK: false, Path: p, Reason: "git --version 执行失败：" + trimGitErr(out.Stderr)}
	}
	ver := strings.TrimSpace(out.Stdout)
	m := versionRe.FindStringSubmatch(ver)
	if m == nil {
		return Info{Found: true, OK: false, Path: p, Version: ver, Reason: "无法解析 git 版本输出"}
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	ok := major > minMajor || (major == minMajor && minor >= minMinor)
	info := Info{Found: true, OK: ok, Path: p, Version: ver}
	if !ok {
		info.Reason = "git 版本过旧（需 ≥ " + strconv.Itoa(minMajor) + "." + strconv.Itoa(minMinor) + "），请升级后重试"
	}
	return info
}

// defaultTimeout 给没有自带 deadline 的 ctx 兜一个上限，避免 git 卡在
// 认证/大仓库上把调用方拖死。
const defaultTimeout = 60 * time.Second
