//go:build windows

package docker

// ============================================================
// Windows DPAPI 加解密（仓库密码落盘用）
//
// 用 CryptProtectData / CryptUnprotectData：密文只能被同一用户在同一台机器上解开，
// 换成其它账号或拷到别的机器都读不出来。仓库密码因此不必明文写进配置文件。
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

const cryptprotectUIFORBIDDEN = 0x1 // 不弹任何 UI（后台调用必须）

func dpapiProtect(plain string) (string, error) {
	in := []byte(plain)
	if len(in) == 0 {
		return "", nil
	}
	inBlob := dataBlob{cbData: uint32(len(in)), pbData: &in[0]}
	var out dataBlob
	r, _, err := procProtect.Call(
		uintptr(unsafe.Pointer(&inBlob)),
		0, // szDataDescr
		0, // pOptionalEntropy
		0, // pvReserved
		0, // pPromptStruct
		cryptprotectUIFORBIDDEN,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return "", fmt.Errorf("CryptProtectData 失败: %v", err)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	buf := unsafe.Slice(out.pbData, int(out.cbData))
	cp := make([]byte, len(buf))
	copy(cp, buf)
	return base64.StdEncoding.EncodeToString(cp), nil
}

func dpapiUnprotect(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", fmt.Errorf("密文格式错误: %v", err)
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
		return "", fmt.Errorf("解密失败（可能换过 Windows 账号或换过机器）: %v", callErr)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	buf := unsafe.Slice(out.pbData, int(out.cbData))
	return string(buf), nil
}
