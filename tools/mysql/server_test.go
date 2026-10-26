package mysql

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDataDirReady 判据必须是「系统表在不在」：目录非空但没系统表的实例启动必败，
// 若按「目录是否为空」判断，就会永远跳过初始化，实例再也起不来。
func TestDataDirReady(t *testing.T) {
	dir := t.TempDir()
	if dataDirReady(dir) {
		t.Fatal("空目录不应判为可用数据目录")
	}
	// 残缺目录（初始化被打断时 mysqld 自己写的两个文件）仍要判为不可用
	if err := os.WriteFile(filepath.Join(dir, "auto.cnf"), []byte("[auto]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "binlog.index"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if dataDirReady(dir) {
		t.Fatal("只有 auto.cnf/binlog.index 的残缺目录不应判为可用")
	}

	if err := os.MkdirAll(filepath.Join(dir, "mysql"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mysql", "mysql.ibd"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !dataDirReady(dir) {
		t.Fatal("有 mysql/mysql.ibd（8.0 数据字典）应判为可用")
	}

	// 老版本的数据目录（只有共享表空间）不能被当成无效目录挪走
	old := t.TempDir()
	if err := os.WriteFile(filepath.Join(old, "ibdata1"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !dataDirReady(old) {
		t.Fatal("有 ibdata1（5.7 数据目录）应判为可用")
	}
}

// TestDirIsEmpty 锁住 io.EOF 语义：空目录必须得到 (true, nil)。
// Readdirnames 在空目录上返回 io.EOF，若透传出去且调用方写成
// "err == nil && empty"，空目录会被误判成「非空」——首次初始化被跳过
// 正是本次事故的根源，这里把它固定住。
func TestDirIsEmpty(t *testing.T) {
	dir := t.TempDir()
	empty, err := dirIsEmpty(dir)
	if err != nil || !empty {
		t.Fatalf("空目录应返回 (true, nil): empty=%v err=%v", empty, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auto.cnf"), []byte("[auto]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if empty, err = dirIsEmpty(dir); err != nil || empty {
		t.Fatalf("非空目录应返回 (false, nil): empty=%v err=%v", empty, err)
	}
	if _, err := dirIsEmpty(filepath.Join(dir, "nope")); err == nil {
		t.Fatal("不存在的目录应返回错误")
	}
}

// TestBuildMyIni my.ini 关键项必须齐全，且不能含 skip-name-resolve：跳过反解后
// TCP 127.0.0.1 会被当成独立主机，而 --initialize-insecure 只建 root@localhost，
// 任何回环连接都会吃 Error 1130——本地实例「建好了却永远连不上」的元凶。
func TestBuildMyIni(t *testing.T) {
	base := filepath.Join("opt", "mysql")
	instDir := t.TempDir()
	dataDir := filepath.Join(instDir, "data")
	ini := buildMyIni(&InstanceConfig{Port: 3399, MaxConnections: 50}, base, instDir, dataDir)
	for _, want := range []string{
		"port=3399",
		"max_connections=50",
		"basedir=" + filepath.ToSlash(base),
		"datadir=" + filepath.ToSlash(dataDir),
	} {
		if !strings.Contains(ini, want) {
			t.Fatalf("my.ini 缺少 %q:\n%s", want, ini)
		}
	}
	if strings.Contains(ini, "skip-name-resolve") {
		t.Fatal("my.ini 不应含 skip-name-resolve：root@localhost 将无法经 127.0.0.1 连接（Error 1130）")
	}
	// 下限 20：面板自身要并发连接，配 1 这种值会让 UI 互相挤爆（Error 1040）
	low := buildMyIni(&InstanceConfig{Port: 3399, MaxConnections: 1}, base, instDir, dataDir)
	if !strings.Contains(low, "max_connections=20") {
		t.Fatalf("max_connections=1 应被抬到 20：\n%s", low)
	}
	// 0 = 跟随 MySQL 默认
	def := buildMyIni(&InstanceConfig{Port: 3399}, base, instDir, dataDir)
	if !strings.Contains(def, "max_connections=151") {
		t.Fatalf("未配置时应写默认 151：\n%s", def)
	}
}

// TestQuarantineDataDir 无效目录要改名留档，不能删：里面可能还有用户的表。
func TestQuarantineDataDir(t *testing.T) {
	// 目录不存在：建出来即可，无需备份
	missing := filepath.Join(t.TempDir(), "data")
	bak, err := quarantineDataDir(missing)
	if err != nil || bak != "" {
		t.Fatalf("不存在的目录不应产生备份: bak=%q err=%v", bak, err)
	}
	if fi, serr := os.Stat(missing); serr != nil || !fi.IsDir() {
		t.Fatalf("应重建出空目录: %v", serr)
	}

	// 本来就空的目录：原样留着
	if bak, err = quarantineDataDir(missing); err != nil || bak != "" {
		t.Fatalf("空目录不应产生备份: bak=%q err=%v", bak, err)
	}

	// 非空目录：改名留档 + 原路径重建为空目录
	if err = os.WriteFile(filepath.Join(missing, "ibdata1"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	bak, err = quarantineDataDir(missing)
	if err != nil {
		t.Fatal(err)
	}
	if bak == "" {
		t.Fatal("非空目录应产生备份路径")
	}
	if empty, _ := dirIsEmpty(missing); !empty {
		t.Fatal("原路径应被重建为空目录")
	}
	if _, serr := os.Stat(filepath.Join(bak, "ibdata1")); serr != nil {
		t.Fatalf("备份目录里应保留原文件: %v", serr)
	}
}

// TestAppendErrNote 说明要追加进 mysql.err（前端日志读的就是它），且不能覆盖已有内容。
func TestAppendErrNote(t *testing.T) {
	p := filepath.Join(t.TempDir(), "mysql.err")
	if err := os.WriteFile(p, []byte("原有日志\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	appendErrNote(p, "数据目录已备份到 X")
	appendErrNote(p, "第二条")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "原有日志") {
		t.Fatalf("追加不应覆盖原内容: %q", s)
	}
	if !strings.Contains(s, "[go-tools] 数据目录已备份到 X") || !strings.Contains(s, "[go-tools] 第二条") {
		t.Fatalf("说明未正确追加: %q", s)
	}
}
