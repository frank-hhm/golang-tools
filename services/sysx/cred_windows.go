//go:build windows

package sysx

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ============================================================
// Windows DPAPI 加密存储（仓库密码、令牌等敏感短串）
//
// CryptProtectData / CryptUnprotectData：密文只能被同一用户在同一台机器解开，
// 换账号、拷到别的机器都读不出来，因此不必把密码明文写进配置文件。
// ============================================================

var (
	modCrypt32  = windows.NewLazySystemDLL("crypt32.dll")
	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procProtect   = modCrypt32.NewProc("CryptProtectData")
	procUnprotect = modCrypt32.NewProc("CryptUnprotectData")
	procLocalFree = modKernel32.NewProc("LocalFree")
)

// dataBlob 对应 Win32 的 DATA_BLOB
type dataBlob struct {
	cbData uint32
	pbData *byte
}

const cryptprotectUIFORBIDDEN = 0x1 // 不弹任何 UI（后台调用必须）

// dpapiCred DPAPI 实现：磁盘上存 base64 密文，密钥由系统按当前用户派生。
type dpapiCred struct{}

// platformCred 返回 Windows 的 DPAPI 实现。
func platformCred() Cred { return dpapiCred{} }

// Save 加密后原子落盘（临时文件 + rename）。
func (dpapiCred) Save(path string, secret []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	enc, err := dpapiProtect(secret)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(enc), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load 解密读取；文件不存在时返回 (nil, nil)，调用方不必区分"没存过"和"空值"。
func (dpapiCred) Load(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return dpapiUnprotect(string(raw))
}

// Delete 删除密文；不存在不算错误。
func (dpapiCred) Delete(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// dpapiProtect 加密并返回 base64 文本。
func dpapiProtect(plain []byte) (string, error) {
	if len(plain) == 0 {
		return "", nil
	}
	inBlob := dataBlob{cbData: uint32(len(plain)), pbData: &plain[0]}
	var out dataBlob
	r, _, callErr := procProtect.Call(
		uintptr(unsafe.Pointer(&inBlob)),
		0, // szDataDescr
		0, // pOptionalEntropy
		0, // pvReserved
		0, // pPromptStruct
		cryptprotectUIFORBIDDEN,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return "", fmt.Errorf("CryptProtectData 失败: %v", callErr)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	buf := unsafe.Slice(out.pbData, int(out.cbData))
	cp := make([]byte, len(buf))
	copy(cp, buf)
	return base64.StdEncoding.EncodeToString(cp), nil
}

// dpapiUnprotect 解密 base64 密文。
func dpapiUnprotect(enc string) ([]byte, error) {
	if enc == "" {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return nil, fmt.Errorf("密文格式错误: %v", err)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	inBlob := dataBlob{cbData: uint32(len(raw)), pbData: &raw[0]}
	var out dataBlob
	r, _, callErr := procUnprotect.Call(
		uintptr(unsafe.Pointer(&inBlob)),
		0, 0, 0, 0,
		cryptprotectUIFORBIDDEN,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, fmt.Errorf("解密失败（可能换过 Windows 账号或换过机器）: %v", callErr)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	buf := unsafe.Slice(out.pbData, int(out.cbData))
	cp := make([]byte, len(buf))
	copy(cp, buf)
	return cp, nil
}
