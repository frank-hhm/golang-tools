// Package githost 把右键 verb 拉起的「在某个仓库打开 Git 工作台」请求转交给
// 已经在跑的主程序。
//
// 通用机制（回环 TCP + crypto/rand 令牌 + 端点文件）复用 Tools/services/singleinstance，
// 与压缩（archivehost）各用各的服务名与端点文件（本包 → data/git/endpoint.json），
// 彼此互不干扰。为什么要转交：右键把路径交给 exe，而主程序这时候多半已经开着；
// 再启一个完整 GUI 会托盘重复、热键注册失败、配置并发写。所以只把「要在哪个仓库
// 打开、做什么」递过去，递不动就说明主程序没跑，本次启动自己起 GUI 并定位过去。
//
// 与压缩不同，Git 的落点是「开工作台页面」而非「无界面跑完一件事」：收到请求的
// 实例把仓库登记进表、把主窗口拉到前台、路由到 Git 页并广播 git:open。
package githost

import (
	"encoding/json"
	"fmt"

	"Tools/services/singleinstance"
)

// service 本特性的服务名，决定端点文件落在 data/git/。
const service = "git"

// Request 一次转交的负载。Op 是操作名（status/commit/push/pull/branch），
// Repo 是已解析到的仓库根（或空串=靠 cwd），From 仅用于日志分来源。
type Request struct {
	Op   string `json:"op"`
	Repo string `json:"repo"`
	From string `json:"from"`
}

// Server 正在监听的端点。内嵌通用 Server，Stop() 直接提升过来。
type Server struct {
	*singleinstance.Server
}

// Start 起转交监听。onReq 在每个连接上同步调用，主程序侧须自己异步化。
func Start(onReq func(Request)) (*Server, error) {
	if onReq == nil {
		return nil, fmt.Errorf("githost: 没有处理请求的回调")
	}
	s, err := singleinstance.Start(service, func(raw json.RawMessage) {
		var r Request
		if err := json.Unmarshal(raw, &r); err != nil {
			return // 负载坏了不是主程序的错，丢弃即可，调用方已收到 ack
		}
		onReq(r)
	})
	if err != nil {
		return nil, err
	}
	return &Server{s}, nil
}

// TrySend 把打开请求递给正在监听的主程序；false 表示没有可转交的实例。
func TrySend(job Request) bool {
	return singleinstance.TrySend(service, job)
}
