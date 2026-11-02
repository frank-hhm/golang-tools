// Package archivehost 把右键拉起的压缩任务转交给已经在跑的主程序。
//
// 通用机制（回环 TCP + crypto/rand 令牌 + 端点文件）已抽到
// Tools/services/singleinstance，与 Git 等其它特性共用；本包只剩一层
// archive 专属的薄封装：固定服务名 "archive"（→ 端点文件 data/archive/），
// 把 opaque JSON 负载收发为 archive.Options 形状的 Request。
//
// 为什么要转交：右键菜单把路径交给 go-tools.exe，而主程序这时候多半已经开着。
// 再启一个完整 GUI 会托盘重复、全局热键注册失败、配置并发写。所以只把
// 「要压哪几个路径」这一件事递过去，递不动就说明主程序没跑，本次调用自己
// 无界面压完也一样是对的。机制细节与取舍见 singleinstance 包注释。
package archivehost

import (
	"encoding/json"
	"fmt"

	"Tools/services/singleinstance"
	"Tools/tools/archive"
)

// service 本特性的服务名，决定端点文件落在 data/archive/。
const service = "archive"

// Request 一次转交的负载。
//
// 内嵌 archive.Options：JSON 会把它的字段平铺进来，正好是方案里定的
// {op, paths, format, level, outDir} 形状；内嵌而不是另抄一份字段，
// 是为了让「加一个选项」只改一处，不会出现新旧两版负载。
type Request struct {
	archive.Options
	From string `json:"from"` // verb / sendto / cli / shell，只在日志里用来分来源
}

// Server 正在监听的端点。内嵌通用 Server，Stop() 直接提升过来。
type Server struct {
	*singleinstance.Server
}

// Start 起转交监听。onJob 在每个连接上同步调用，主程序侧须自己异步化。
func Start(onJob func(Request)) (*Server, error) {
	if onJob == nil {
		return nil, fmt.Errorf("archivehost: 没有处理请求的回调")
	}
	s, err := singleinstance.Start(service, func(raw json.RawMessage) {
		var r Request
		if err := json.Unmarshal(raw, &r); err != nil {
			return // 负载坏了不是主程序的错，丢弃即可，调用方已收到 ack
		}
		onJob(r)
	})
	if err != nil {
		return nil, err
	}
	return &Server{s}, nil
}

// TrySend 把压缩请求递给正在监听的主程序；false 表示没有可转交的实例。
func TrySend(job Request) bool {
	return singleinstance.TrySend(service, job)
}
