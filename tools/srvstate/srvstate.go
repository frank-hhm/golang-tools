// Package srvstate 记录「实例进程已就绪 / 正在被主动停止」这两个事实，
// 并统一描述实例异常退出事件。
//
// 为什么要单独记这两个事实：进程退出有两种语义——用户在面板点「停止」是预期内的，
// 进程自己崩掉是要提醒用户的。只凭 cmd.Wait() / 容器退出回调无法区分，
// 必须在拉起点与停止点各打一个标记，退出时才能判断该不该弹系统通知。
package srvstate

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// EventSuffix 实例异常退出事件的统一后缀（完整事件名 "<tool>:serverexit"，如 redis:serverexit）
const EventSuffix = ":serverexit"

// Event 实例异常退出事件的负载。
//
// 各工具包（redis / mysql / php / nginx）都发这一种形状，主程序只需解析一次，
// 不必为每个工具各写一份通知逻辑。
type Event struct {
	Tool     string `json:"tool"`     // redis / mysql / php / nginx
	Mode     string `json:"mode"`     // local | docker
	ID       string `json:"id"`       // 实例 ID
	Name     string `json:"name"`     // 实例名（给人看）
	Port     int    `json:"port"`     // 监听端口
	ExitCode int    `json:"exitCode"` // 退出码，-1 表示未知
	At       int64  `json:"at"`       // unix 毫秒
}

// Flag 就绪 / 主动停止标记，内嵌进各工具包的进程表条目：
//
//	type srvRunning struct {
//		cmd *exec.Cmd
//		srvstate.Flag
//	}
type Flag struct {
	ready    atomic.Bool // 已确认就绪：启动期失败由 StartInstance 返回错误，不重复通知
	stopping atomic.Bool // 主动停止（面板点停止 / 重启）：退出不通知
}

// MarkReady 标记实例已就绪（启动探测成功之后调用）
func (f *Flag) MarkReady() { f.ready.Store(true) }

// MarkStopping 标记实例正在被主动停止（杀进程 / 停容器之前调用）
func (f *Flag) MarkStopping() { f.stopping.Store(true) }

// Abnormal 退出是否属于「异常退出」：已就绪且不是主动停止。
//
// 启动期的退出不算：StartInstance 会把错误直接返回给用户，再弹一条系统通知只是重复。
func (f *Flag) Abnormal() bool { return f.ready.Load() && !f.stopping.Load() }

// NewEvent 组装一条异常退出事件
func NewEvent(tool, mode, id, name string, port, exitCode int) Event {
	return Event{
		Tool:     tool,
		Mode:     mode,
		ID:       id,
		Name:     name,
		Port:     port,
		ExitCode: exitCode,
		At:       time.Now().UnixMilli(),
	}
}

// ToolName 工具展示名（通知标题用）
func ToolName(tool string) string {
	switch tool {
	case "redis":
		return "Redis"
	case "mysql":
		return "MySQL"
	case "php":
		return "PHP"
	case "nginx":
		return "Nginx"
	case "npm":
		return "Node/npm"
	case "docker":
		return "Docker"
	}
	return tool
}

// Title 系统通知标题
func (e Event) Title() string { return ToolName(e.Tool) + " 异常退出" }

// Content 系统通知内容
func (e Event) Content() string {
	name := e.Name
	if name == "" {
		name = e.ID
	}
	s := fmt.Sprintf("%s（端口 %d）已退出", name, e.Port)
	if e.ExitCode >= 0 {
		s += fmt.Sprintf("，退出码 %d", e.ExitCode)
	}
	return s
}

// doneVerbs 任务终态通知的动词（按 tasks.Task.Kind 区分）
var doneVerbs = map[string]string{
	"install": "安装完成",
	"ext":     "安装完成",
	"setup":   "安装完成",
	"pull":    "下载完成",
	"op":      "操作完成",
}

// DoneTitle 任务成功通知的标题，如「Redis 安装完成」
func DoneTitle(tool, kind string) string {
	verb := doneVerbs[kind]
	if verb == "" {
		verb = "任务完成"
	}
	return ToolName(tool) + " " + verb
}

// FailTitle 任务失败通知的标题，如「PHP 安装失败」
func FailTitle(tool, kind string) string {
	verb := doneVerbs[kind]
	if verb == "" {
		return ToolName(tool) + " 任务失败"
	}
	return ToolName(tool) + " " + strings.TrimSuffix(verb, "完成") + "失败"
}

// ClipNotice 把通知正文压成一行并限长。
//
// 系统 Toast 只有标题 + 两行正文的位置：多行文本（如安装失败的命令输出）
// 会把卡片撑成一大块，先把换行换成空格再按 rune 截断。max <= 0 表示不限长。
func ClipNotice(s string, max int) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", " "), "\n", " ")
	s = strings.TrimSpace(s)
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}
