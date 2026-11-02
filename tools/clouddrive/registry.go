package clouddrive

// 适配器注册表：按账号 Kind 构造 Provider。新增一家只需在此登记一个工厂，
// 上层（绑定、docref）无需感知具体实现。

import (
	"fmt"
	"sync"
)

// factory 由账号 ID 构造一个已接好 token 存取与配置的 Provider。
type factory func(acctID string) (Provider, error)

var (
	regMu     sync.RWMutex
	factories = map[string]factory{}
)

// register 登记一种云盘的构造器（各适配器文件在 init 里调用）。
func register(kind string, f factory) {
	regMu.Lock()
	defer regMu.Unlock()
	factories[kind] = f
}

// lookupFactory 查注册器。
func lookupFactory(kind string) (factory, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	f, ok := factories[kind]
	return f, ok
}

// New 按账号构造 Provider：先查账号存在与种类，再交给对应工厂。
func New(acctID string) (Provider, error) {
	a, err := getAccount(acctID)
	if err != nil {
		return nil, err
	}
	f, ok := lookupFactory(a.Kind)
	if !ok {
		return nil, fmt.Errorf("暂不支持的云盘类型: %s", a.Kind)
	}
	return f(acctID)
}

// KindSupported 前端用：某类是否已落地（决定"添加账号"里可选项）。
func KindSupported(kind string) bool {
	_, ok := lookupFactory(kind)
	return ok
}
