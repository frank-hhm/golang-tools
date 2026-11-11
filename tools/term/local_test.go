package term

// 本地 CMD / PowerShell 默认项单测：
//   - ensureLocalDefaults 幂等补种（缺则补、在则不重复、按稳定 id 命中）；
//   - SaveHost 对 local 的校验分支（只认 Shell、清地址/凭据、Name 兜底）；
//   - ListHosts 每次加载确保两条常驻默认项存在。
// 均先把 appdirs 指到 t.TempDir()，不碰真实数据；不触发进程/ConPTY，跨平台安全。

import (
	"path/filepath"
	"testing"

	"Tools/appdirs"
)

func useTempDataLocal(t *testing.T) {
	t.Helper()
	base := t.TempDir()
	if err := appdirs.Set(filepath.Join(base, "plugins"), filepath.Join(base, "data"), filepath.Join(base, "logs")); err != nil {
		t.Fatalf("切换临时数据目录失败: %v", err)
	}
}

/* ==================== ensureLocalDefaults 幂等 ==================== */

func TestEnsureLocalDefaultsSeedsWhenEmpty(t *testing.T) {
	next, added := ensureLocalDefaults([]hostRecord{})
	if !added {
		t.Fatal("空列表应补种两条默认项")
	}
	if len(next) != 2 {
		t.Fatalf("应补种 2 条，得到 %d", len(next))
	}
	// 前置顺序：CMD 先、PowerShell 后（seed 调用顺序）
	if next[0].ID != LocalIDCmd || next[0].Shell != ShellCmd || next[0].Kind != KindLocal {
		t.Errorf("首条应为本地 CMD: %+v", next[0].Host)
	}
	if next[1].ID != LocalIDPS || next[1].Shell != ShellPS || next[1].Kind != KindLocal {
		t.Errorf("次条应为本地 PowerShell: %+v", next[1].Host)
	}
	// 再跑一次：已存在则不动、不重复
	next2, added2 := ensureLocalDefaults(next)
	if added2 {
		t.Error("两条都在时不应报告新增")
	}
	if len(next2) != 2 {
		t.Fatalf("重复补种不应翻倍，得到 %d", len(next2))
	}
}

func TestEnsureLocalDefaultsRespectsExisting(t *testing.T) {
	// 已有一条同 id 的本地记录：只补缺失的那条，不重复、不动既有
	recs := []hostRecord{{Host: Host{ID: LocalIDCmd, Name: "我的 CMD", Kind: KindLocal, Shell: ShellCmd}}}
	next, added := ensureLocalDefaults(recs)
	if !added {
		t.Fatal("缺 PowerShell 应补种")
	}
	if len(next) != 2 {
		t.Fatalf("应总共 2 条，得到 %d", len(next))
	}
	var cmdCount, psCount int
	var cmdName string
	for _, r := range next {
		switch r.ID {
		case LocalIDCmd:
			cmdCount++
			cmdName = r.Name
		case LocalIDPS:
			psCount++
		}
	}
	if cmdCount != 1 || psCount != 1 {
		t.Fatalf("按 id 命中应各 1 条：cmd=%d ps=%d", cmdCount, psCount)
	}
	if cmdName != "我的 CMD" {
		t.Errorf("既有本地记录应保留不被覆盖，名称变了吗: %q", cmdName)
	}
}

func TestEnsureLocalDefaultsKeepsSSH(t *testing.T) {
	// 远程 SSH 记录不受影响（Kind 为空）
	recs := []hostRecord{{Host: Host{ID: "h1", Name: "服务器", Address: "1.1.1.1", Port: 22}}}
	next, _ := ensureLocalDefaults(recs)
	if len(next) != 3 {
		t.Fatalf("应保留 SSH 并补 2 条本地，得到 %d", len(next))
	}
	found := false
	for _, r := range next {
		if r.ID == "h1" && r.Kind == "" && r.Address == "1.1.1.1" {
			found = true
		}
	}
	if !found {
		t.Error("SSH 记录被改动了")
	}
}

/* ==================== SaveHost 的 local 校验分支 ==================== */

func TestSaveHostLocalBranch(t *testing.T) {
	useTempDataLocal(t)

	// shell 非法应报错
	if _, err := SaveHost(Host{Kind: KindLocal, Shell: "zsh"}); err == nil {
		t.Fatal("本地连接给了非法 shell 应被拒绝")
	}

	// cmd + 未填名称：Shell 归一、地址/凭据清空、Name 兜底「本地 CMD」
	list, err := SaveHost(Host{
		Kind: KindLocal, Shell: "cmd",
		Address: "1.2.3.4", Port: 22, User: "root", Auth: AuthPassword, Password: "leak", CredID: "c1",
	})
	if err != nil {
		t.Fatalf("SaveHost 本地 cmd 失败: %v", err)
	}
	var got *Host
	for i := range list {
		if list[i].Kind == KindLocal {
			got = &list[i]
		}
	}
	if got == nil {
		t.Fatal("未找到刚保存的本地记录")
	}
	if got.Shell != ShellCmd {
		t.Errorf("Shell 应归一为 cmd，得到 %q", got.Shell)
	}
	if got.Name != "本地 CMD" {
		t.Errorf("Name 应兜底为「本地 CMD」，得到 %q", got.Name)
	}
	if got.Address != "" || got.Port != 0 || got.User != "" || got.Auth != "" || got.CredID != "" {
		t.Errorf("本地连接不应保留地址/凭据字段: %+v", got)
	}
	if got.System != SystemWindows {
		t.Errorf("本地连接 System 应为 windows，得到 %q", got.System)
	}
}

func TestSaveHostLocalKeepsGivenName(t *testing.T) {
	useTempDataLocal(t)
	list, err := SaveHost(Host{Kind: KindLocal, Shell: "powershell", Name: "我的终端"})
	if err != nil {
		t.Fatalf("SaveHost 本地 powershell 失败: %v", err)
	}
	var found bool
	for _, h := range list {
		if h.Kind == KindLocal && h.Shell == ShellPS {
			found = true
			if h.Name != "我的终端" {
				t.Errorf("已填名称应保留，得到 %q", h.Name)
			}
		}
	}
	if !found {
		t.Fatal("未找到 powershell 本地记录")
	}
}

/* ==================== ListHosts 每次确保默认项常驻 ==================== */

func TestListHostsEnsuresDefaults(t *testing.T) {
	useTempDataLocal(t)
	hosts, err := ListHosts()
	if err != nil {
		t.Fatalf("ListHosts 失败: %v", err)
	}
	countDefaults := func() (int, int) {
		var cmd, ps int
		for _, h := range hosts {
			switch h.ID {
			case LocalIDCmd:
				cmd++
			case LocalIDPS:
				ps++
			}
		}
		return cmd, ps
	}
	if c, p := countDefaults(); c != 1 || p != 1 {
		t.Fatalf("首次加载应各含 1 条默认项，得到 cmd=%d ps=%d", c, p)
	}

	// 再加载一次：落盘后仍幂等，不翻倍
	hosts, _ = ListHosts()
	if c, p := countDefaults(); c != 1 || p != 1 {
		// 重新统计（hosts 已刷新）
		var cmd, ps int
		for _, h := range hosts {
			switch h.ID {
			case LocalIDCmd:
				cmd++
			case LocalIDPS:
				ps++
			}
		}
		t.Fatalf("重复加载默认项应仍各 1 条，得到 cmd=%d ps=%d", cmd, ps)
	}
}
