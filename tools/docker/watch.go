package docker

// ============================================================
// 容器看护与错误分型（平台无关）
//
// 以前 php/mysql/redis/nginx/npm 五个工具各自复制了一份「每 1.5 秒 Inspect，
// 一旦 err != nil 就当作容器已退出」的看护协程。问题在于 Inspect 失败的原因
// 绝大多数不是「容器没了」，而是引擎一时答不上：命名管道忙、闸门排队超时、
// 熔断静默窗口、Desktop 自更新、容器被 pause……这些抖动会把还在正常运行的实例
// 从进程表里抹掉，面板当场显示「已停止」，日志跟随也一起断。
//
// 这里把两件事收口：错误分型（404 才算对象没了）+ 一份带退避的共享看护。
// ============================================================

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"go-tools/services/applog"
)

// ErrUnsupported 表示当前平台压根不提供该能力：它不是「引擎抖动」，重试没有意义。
// 注意与 docker_other.go 的分工：Linux 上引擎的安装/启停是真实现的，只有 WSL2、
// Hyper-V、Desktop 安装器这类概念才归到这个错误上（且现在改成具体文案而不是复用它）。
var ErrUnsupported = errors.New("非 Windows 平台暂不支持 Docker 管理")

// APIStatusError 带 HTTP 状态码的 Engine API 错误。
// 只想知道「是不是失败了」的调用方照旧拿 error；需要区分
// 「对象确实不存在(404)」与「引擎一时答不上」的调用方用 IsNotFound / IsTransient。
type APIStatusError struct {
	Status  int
	Message string
}

func (e *APIStatusError) Error() string { return e.Message }

// StatusCode 取回原始 HTTP 状态码（非状态类错误返回 0）
func StatusCode(err error) int {
	var se *APIStatusError
	if errors.As(err, &se) {
		return se.Status
	}
	return 0
}

// IsNotFound 引擎明确回答「没有这个对象」——容器/镜像已被删除，看护可以收场了
func IsNotFound(err error) bool {
	var se *APIStatusError
	return errors.As(err, &se) && se.Status == http.StatusNotFound
}

// IsTransient 一次抖动：传输层失败、管道忙、闸门排队、熔断静默、5xx。
// 含义是「现在问不出答案」，不是「答案是否定的」——不能拿它改对象状态。
func IsTransient(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrUnsupported) {
		return false
	}
	var se *APIStatusError
	if errors.As(err, &se) {
		// 4xx 是引擎给了明确答复（不存在/冲突/参数不对），只有 5xx 算它一时不行
		return se.Status >= http.StatusInternalServerError
	}
	return true
}

const (
	watchEvery    = 1500 * time.Millisecond // 正常节奏：与本地模式 cmd.Wait 的响应感对齐
	watchMaxEvery = 15 * time.Second        // 问不出答案时的退避上限，别在引擎打瞌睡时贴上去问
)

// WatchContainerExit 后台跟踪容器退出，只在容器停止/被删除时回调 onExit。
//
// onExit 会在后台协程里被调用一次，调用方负责把自己的进程表摘除与 done 通道关闭写成幂等。
// 引擎抖动期间不回调、只退避重试（1.5s → 3s → 6s … 最大 15s），恢复后回到正常节奏。
func WatchContainerExit(containerID string, onExit func()) {
	go func() {
		interval := watchEvery
		blamed := false // 抖动原因只在进入退避时记一条，避免每轮刷日志
		for {
			time.Sleep(interval)
			st, err := InspectContainer(containerID)
			if err == nil {
				if blamed {
					applog.Infof("docker", "容器 %s 状态查询恢复，继续看护", briefID(containerID))
					blamed = false
				}
				interval = watchEvery
				if !st.Running {
					onExit()
					return
				}
				continue
			}
			if IsNotFound(err) {
				onExit() // 容器已被删除（docker rm），与停止同样收场
				return
			}
			if !IsTransient(err) {
				onExit()
				return
			}
			if !blamed {
				applog.Warnf("docker", "容器 %s 状态一时问不到，暂按运行中处理，退避重试：%v", briefID(containerID), err)
				blamed = true
			}
			if interval *= 2; interval > watchMaxEvery {
				interval = watchMaxEvery
			}
		}
	}()
}

// briefID 日志用的短容器 ID（本包内自给自足，不依赖 windows 实现里的 shortID）
func briefID(id string) string {
	if len(id) >= 12 {
		return id[:12]
	}
	return id
}

// apiStatusErr 供 apiError 构造带码错误，消息文本与改造前保持一致
func apiStatusErr(status int, msg string) error {
	switch status {
	case http.StatusNotFound:
		return &APIStatusError{status, fmt.Sprintf("Docker 中不存在该对象: %s", msg)}
	case http.StatusConflict:
		return &APIStatusError{status, fmt.Sprintf("Docker 操作冲突: %s", msg)}
	default:
		return &APIStatusError{status, fmt.Sprintf("Docker API 错误(%d): %s", status, msg)}
	}
}
