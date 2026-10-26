//go:build !linux

package sysx

import "os"

// binSearchDirs 非 Linux 平台不额外搜目录。
//
// Windows 的 PATHEXT 与 PATH 已经由 exec.LookPath 处理好（cmd 里能找到的
// 程序 LookPath 也能找到）；macOS 也不像 Linux 那样把守护进程只塞进 sbin。
func binSearchDirs() []string { return nil }

// executableFile 非 Linux 平台只看"是不是普通文件"。
//
// Windows 没有可执行位这个概念，按 0o111 判断会把所有 .exe 都判成不可用。
func executableFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// PkgManager 非 Linux 平台没有统一的系统包管理器。
func PkgManager() string { return "" }

// PackageCandidate 非 Linux 平台查不到"仓库里的版本"。
func PackageCandidate(pkg string) string { return "" }

// PackageCandidates 非 Linux 平台恒为空。
func PackageCandidates(pkgs []string) []PkgCandidate { return nil }

// InstallPackages 非 Linux 平台没有可用的系统包安装通道。
//
// Windows 上的四个服务走的是自带的绿色版 zip（plugins 目录），这条路本来
// 就不需要包管理器；调用方拿到 ErrUnsupported 应当回落到原流程。
func InstallPackages(pkgs []string, onLine func(string)) error { return ErrUnsupported }
