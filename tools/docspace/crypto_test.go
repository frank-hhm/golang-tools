package docspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptUnlockRoundTrip(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	src := mkFile(t, dir, "secret.txt", "TOP-SECRET-CONTENT")

	entry, err := s.Encrypt(src, "hunter2")
	if err != nil {
		t.Fatal(err)
	}
	// 加密成功后原始明文应被删除
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("加密后原明文应被删除, stat=%v", err)
	}
	// 密文容器应存在
	if _, err := os.Stat(s.gtdPath(entry.ID)); err != nil {
		t.Fatalf("密文容器缺失: %v", err)
	}
	list, _ := s.ListSecure()
	if len(list) != 1 || list[0].ID != entry.ID {
		t.Fatalf("登记不对: %+v", list)
	}

	view, err := s.Unlock(entry.ID, "hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(view); string(b) != "TOP-SECRET-CONTENT" {
		t.Fatalf("解锁明文内容不对: %q", string(b))
	}
	if err := s.Lock(entry.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(view); !os.IsNotExist(err) {
		t.Fatalf("上锁后明文视图应被删除, stat=%v", err)
	}
}

func TestUnlockWrongPassword(t *testing.T) {
	s := newTestStore(t)
	src := mkFile(t, t.TempDir(), "a.bin", "data")
	entry, err := s.Encrypt(src, "right-pw")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Unlock(entry.ID, "wrong-pw"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("错口令应回 ErrWrongPassword, got %v", err)
	}
}

func TestChangePassword(t *testing.T) {
	s := newTestStore(t)
	src := mkFile(t, t.TempDir(), "a.txt", "PAYLOAD")
	entry, _ := s.Encrypt(src, "old-pw")

	if err := s.ChangePassword(entry.ID, "wrong", "new-pw"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("旧口令错时改密应失败, got %v", err)
	}
	if err := s.ChangePassword(entry.ID, "old-pw", "new-pw"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Unlock(entry.ID, "old-pw"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("改密后旧口令应失效, got %v", err)
	}
	view, err := s.Unlock(entry.ID, "new-pw")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(view); string(b) != "PAYLOAD" {
		t.Fatalf("改密后内容不对: %q", string(b))
	}
}

func TestExtractTo(t *testing.T) {
	s := newTestStore(t)
	src := mkFile(t, t.TempDir(), "note.md", "# hello")
	entry, _ := s.Encrypt(src, "pw")

	outDir := t.TempDir()
	dest := filepath.Join(outDir, "restored.md")
	if err := s.ExtractTo(entry.ID, "pw", dest); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "# hello" {
		t.Fatalf("取回内容不对: %q", string(b))
	}
	// 目标已存在应拒绝覆盖
	if err := s.ExtractTo(entry.ID, "pw", dest); err == nil {
		t.Fatal("已存在目标不应被覆盖")
	}
	// 错口令应失败且不写出文件
	dest2 := filepath.Join(outDir, "x.md")
	if err := s.ExtractTo(entry.ID, "bad", dest2); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("错口令提取应回 ErrWrongPassword, got %v", err)
	}
	if _, err := os.Stat(dest2); !os.IsNotExist(err) {
		t.Fatal("错口令不应写出文件")
	}
}

func TestRemoveSecure(t *testing.T) {
	s := newTestStore(t)
	src := mkFile(t, t.TempDir(), "a.txt", "x")
	entry, _ := s.Encrypt(src, "pw")
	if err := s.RemoveSecure(entry.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.gtdPath(entry.ID)); !os.IsNotExist(err) {
		t.Fatal("删除后密文容器应不存在")
	}
	list, _ := s.ListSecure()
	if len(list) != 0 {
		t.Fatalf("删除后登记应为空: %+v", list)
	}
	// 幂等：再删不存在的不报错
	if err := s.RemoveSecure(entry.ID); err != nil {
		t.Fatalf("重复删除应幂等, got %v", err)
	}
}

func TestEncryptRejectsBadInput(t *testing.T) {
	s := newTestStore(t)
	// 空口令
	if _, err := s.Encrypt(mkFile(t, t.TempDir(), "a.txt", "x"), ""); err == nil {
		t.Fatal("空口令应被拒绝")
	}
	// 文件不存在
	if _, err := s.Encrypt(filepath.Join(t.TempDir(), "nope.txt"), "pw"); err == nil {
		t.Fatal("不存在的文件应被拒绝")
	}
	// 目录
	if _, err := s.Encrypt(t.TempDir(), "pw"); err == nil {
		t.Fatal("目录不应被加密")
	}
}

// 低层容器：往返 + 篡改检测（GCM 认证必须挡住单字节翻转）。
func TestContainerTamperDetected(t *testing.T) {
	data, _, _, err := encryptToContainer([]byte("hello world"), "pw")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := decryptContainer(data, "pw")
	if err != nil || string(plain) != "hello world" {
		t.Fatalf("正常往返失败: %v %q", err, string(plain))
	}
	data[len(data)-1] ^= 0x01 // 翻转密文最后一字节
	if _, err := decryptContainer(data, "pw"); err == nil {
		t.Fatal("篡改密文必须被 GCM 认证挡住")
	}
}
