package gitcmd

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
)

// Out 一次 git 调用的输出。Stdout/Stderr 即便命令失败也尽量带回来，供上层判断。
type Out struct {
	Stdout string
	Stderr string
}

// commonPrefix 统一注入的全局旗标（必须排在子命令之前）：
//   - color.ui=false：关掉 ANSI，解析才干净
//   - core.quotepath=false：中文/非 ASCII 路径原样输出，不转成 \346\200 八进制
//   - --no-optional-locks：只读命令不去碰 index.lock，避免和编辑器/IDE 抢锁
var commonPrefix = []string{"-c", "color.ui=false", "-c", "core.quotepath=false", "--no-optional-locks"}

// runRaw 跑一条命令，不注入任何旗标（Check 的 --version 用）。dir 空则继承当前目录。
func runRaw(ctx context.Context, dir, name string, args ...string) (Out, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	applyHidden(cmd)
	var so, se bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	err := cmd.Run()
	out := Out{Stdout: so.String(), Stderr: se.String()}
	return out, err
}

// Run 在 repoDir 里跑一条 git 子命令，注入统一旗标；非零退出码返回带人话信息的 error
// （此时 Out 仍原样带回，调用方可读 Stderr）。ctx 无 deadline 时兜 defaultTimeout。
func Run(ctx context.Context, repoDir string, args ...string) (Out, error) {
	path, err := bin()
	if err != nil {
		return Out{}, err
	}
	full := append(append([]string{}, commonPrefix...), args...)
	used, cancel := withTimeout(ctx)
	defer cancel()
	out, rerr := runRaw(used, repoDir, path, full...)
	if rerr != nil {
		return out, fmt.Errorf("%s", humanize(out.Stderr, rerr))
	}
	return out, nil
}

// runStream 与 runRaw 同，但 stderr 逐行回调（git 进度用 CR 刷新，按 CR/LF 双分隔切行）。
// 不兜超时：调用方（长操作）自己给带 deadline 的 ctx，避免 60s 默认值砍断大仓推送。
func runStream(ctx context.Context, dir string, onLine func(string), args ...string) (Out, error) {
	path, err := bin()
	if err != nil {
		return Out{}, err
	}
	full := append(append([]string{}, commonPrefix...), args...)
	cmd := exec.CommandContext(ctx, path, full...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	applyHidden(cmd)
	var ob bytes.Buffer
	cmd.Stdout = &ob
	se, perr := cmd.StderrPipe()
	if perr != nil {
		return Out{}, perr
	}
	if err := cmd.Start(); err != nil {
		return Out{}, err
	}
	var sb bytes.Buffer
	sc := bufio.NewScanner(se)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	sc.Split(splitCRLF)
	for sc.Scan() {
		line := sc.Text()
		sb.WriteString(line)
		sb.WriteByte('\n')
		if onLine != nil && line != "" {
			onLine(line)
		}
	}
	werr := cmd.Wait()
	out := Out{Stdout: ob.String(), Stderr: sb.String()}
	if werr != nil {
		return out, fmt.Errorf("%s", humanize(out.Stderr, werr))
	}
	return out, nil
}

// splitCRLF 同时以 \r 和 \n 断行：进度行靠 \r 原地刷新，非 TTY 下两者混用。
func splitCRLF(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// RunArgs 与 Run 同，但不注入 commonPrefix（需要精确控制参数时用，如带 -c credential 的推送）。
func RunArgs(ctx context.Context, repoDir string, args ...string) (Out, error) {
	path, err := bin()
	if err != nil {
		return Out{}, err
	}
	used, cancel := withTimeout(ctx)
	defer cancel()
	out, rerr := runRaw(used, repoDir, path, args...)
	if rerr != nil {
		return out, fmt.Errorf("%s", humanize(out.Stderr, rerr))
	}
	return out, nil
}

// humanize 把退出错误翻译成人话：优先 git 自己 stderr 里的 fatal/error 行。
func humanize(stderr string, err error) string {
	if msg := trimGitErr(stderr); msg != "" {
		return msg
	}
	return err.Error()
}

// withTimeout：ctx 已有 deadline 就用原样，否则包一个 defaultTimeout 的上限。
// 返回的 cancel 必须被调用方 defer。
func withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, defaultTimeout)
}

// OutStr 便捷取 stdout 去尾换行。
func (o Out) OutStr() string { return trimTail(o.Stdout) }

func trimTail(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
