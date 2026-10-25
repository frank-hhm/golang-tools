//go:build windows

package term

// ============================================================
// Windows DPAPI 加解密（SSH 密码落盘专用）
//
// 走 CryptProtectData / CryptUnprotectData：密文只能被同一用户在同一台机器上解开，
// 换账号或拷到别的机器都读不出来，所以密码不必明文写进 hosts.json。
// ============================================================

import (
	"encoding/base64"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

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

const cryptprotectUIFORBIDDEN = 0x1 // 后台调用必须禁掉任何 UI 弹窗

// protectSecret 明文 → Base64(DPAPI 密文)；空串原样返回
func protectSecret(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	in := []byte(plain)
	inBlob := dataBlob{cbData: uint32(len(in)), pbData: &in[0]}
	var out dataBlob
	r, _, err := procProtect.Call(
		uintptr(unsafe.Pointer(&inBlob)),
		0,
		0,
		0,
		0,
		cryptprotectUIFORBIDDEN,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return "", fmt.Errorf("加密密码失败: %v", err)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	buf := unsafe.Slice(out.pbData, int(out.cbData))
	cp := make([]byte, len(buf))
	copy(cp, buf)
	return base64.StdEncoding.EncodeToString(cp), nil
}

// unprotectSecret Base64(DPAPI 密文) → 明文
func unprotectSecret(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", fmt.Errorf("密码密文格式错误: %v", err)
	}
	if len(raw) == 0 {
		return "", nil
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
		return "", fmt.Errorf("解密密码失败（可能换了 Windows 账号或换过机器）: %v", callErr)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	buf := unsafe.Slice(out.pbData, int(out.cbData))
	return string(buf), nil
}
