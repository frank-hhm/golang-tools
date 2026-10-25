// Package portcheck 统一判断端口能否给（新）服务用。
//
// 只看 net.Listen 是不够的：实例停着的时候端口号照样被它“登记”着，两个实例配同一个端口
// 只会在启动时才炸。这里把各工具（PHP/MySQL/Redis/Nginx/静态站点）实例已登记的端口和
// 系统实际监听一起看，前端选默认端口、保存前校验都走这里。
package portcheck

import (
	"fmt"
	"net"
	"sync"
)

// Owner 一条已登记的端口占用
type Owner struct {
	// Key 全局唯一，形如 "php:inst-1700..."；编辑实例时用来排除自身
	Key string
	// Label 展示名，如 PHP 实例「blog」
	Label string
	// Ports 该实例占用的端口（Nginx 会把 HTTP/HTTPS 都放进来）
	Ports []int
}

var (
	mu        sync.RWMutex
	providers []func() []Owner
)

// Register 由各工具包在 init 里登记自己的实例来源。真正读配置发生在调用时，
// 所以实例增删后不用重新登记。
func Register(p func() []Owner) {
	if p == nil {
		return
	}
	mu.Lock()
	providers = append(providers, p)
	mu.Unlock()
}

// Owners 汇总所有工具当前登记的端口占用
func Owners() []Owner {
	mu.RLock()
	ps := make([]func() []Owner, len(providers))
	copy(ps, providers)
	mu.RUnlock()
	var out []Owner
	for _, p := range ps {
		out = append(out, p()...)
	}
	return out
}

// Free 尝试绑定 127.0.0.1:port，能绑上即视为空闲
func Free(port int) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("端口非法: %d", port)
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	return ln.Close()
}

// Taken 报告端口是否不可用：先看有没有别的实例登记，再看系统里有没有进程监听。
// excludeKey 用于编辑场景排除自身；返回非空字符串即占用原因，空串表示可用。
func Taken(port int, excludeKey string) string {
	if port <= 0 || port > 65535 {
		return fmt.Sprintf("端口非法: %d", port)
	}
	owners := Owners()
	for _, o := range owners {
		if excludeKey != "" && o.Key == excludeKey {
			continue
		}
		for _, p := range o.Ports {
			if p == port {
				return fmt.Sprintf("端口 %d 已被%s占用", port, o.Label)
			}
		}
	}
	if Free(port) == nil {
		return ""
	}
	// 端口确实有人在监听：编辑自身且端口没改时，监听者大概率就是自己
	//（程序重启后已不再跟踪这个子进程，但配置里的端口仍是它）
	if excludeKey != "" {
		for _, o := range owners {
			if o.Key != excludeKey {
				continue
			}
			for _, p := range o.Ports {
				if p == port {
					return ""
				}
			}
		}
	}
	return fmt.Sprintf("端口 %d 已被其它进程占用", port)
}

// Suggest 从 from 起递增找一个既没被任何实例登记、系统也能绑定的端口；
// 找不到就退回 from（调用方仍会在启动/保存时报错）。
func Suggest(from, span int) int {
	if from <= 0 {
		from = 1024
	}
	if span <= 0 {
		span = 100
	}
	inUse := map[int]bool{}
	for _, o := range Owners() {
		for _, p := range o.Ports {
			if p > 0 {
				inUse[p] = true
			}
		}
	}
	for p := from; p < from+span && p <= 65535; p++ {
		if inUse[p] {
			continue
		}
		if Free(p) == nil {
			return p
		}
	}
	return from
}
