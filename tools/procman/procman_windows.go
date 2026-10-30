//go:build windows

package procman

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"sync"
	"time"
	"unsafe"

	"Tools/services/winrun"

	"golang.org/x/sys/windows"
)

const (
	processQueryLimitedInformation = 0x1000
	processTerminate               = 0x0001
	processSetInformation          = 0x0200
	processSuspendResume           = 0x0800
	processCreateThread            = 0x0002
	processVmOperation             = 0x0008
)

var (
	modPsapi       = windows.NewLazySystemDLL("psapi.dll")
	modNtdll       = windows.NewLazyDLL("ntdll.dll")
	modVersion     = windows.NewLazySystemDLL("version.dll")
	modUser32      = windows.NewLazySystemDLL("user32.dll")
	modGdi32       = windows.NewLazySystemDLL("gdi32.dll")
	modShell32     = windows.NewLazySystemDLL("shell32.dll")
	procGetMemInfo = modPsapi.NewProc("GetProcessMemoryInfo")
	procNtSuspend  = modNtdll.NewProc("NtSuspendProcess")
	procNtResume   = modNtdll.NewProc("NtResumeProcess")
	procVerSize    = modVersion.NewProc("GetFileVersionInfoSizeW")
	procVerGet     = modVersion.NewProc("GetFileVersionInfoW")
	procVerQuery   = modVersion.NewProc("VerQueryValueW")
	// ExtractIconExW 由 shell32.dll 导出（SDK 里就链在 Shell32.lib），
	// 挂到 user32 上时 LazyProc 找不到函数会直接 panic。
	procExtractIcon  = modShell32.NewProc("ExtractIconExW")
	procDrawIcon     = modUser32.NewProc("DrawIconEx")
	procDestroyIcon  = modUser32.NewProc("DestroyIcon")
	procSysMetric    = modUser32.NewProc("GetSystemMetrics")
	procShellExecW   = modShell32.NewProc("ShellExecuteW")
	procGetDC        = modUser32.NewProc("GetDC")
	procReleaseDC    = modUser32.NewProc("ReleaseDC")
	procCreateCompDC = modGdi32.NewProc("CreateCompatibleDC")
	procCreateDIB    = modGdi32.NewProc("CreateDIBSection")
	procSelectObj    = modGdi32.NewProc("SelectObject")
	procDeleteObj    = modGdi32.NewProc("DeleteObject")
	procDeleteDC     = modGdi32.NewProc("DeleteDC")
)

// filetime100ns 把 FILETIME（100ns 刻度）还原成 uint64。
func filetime100ns(ft windows.Filetime) uint64 {
	return uint64(ft.HighDateTime)<<32 | uint64(ft.LowDateTime)
}

// filetimeToMs FILETIME -> Unix 毫秒时间戳
func filetimeToMs(ft windows.Filetime) int64 {
	ns := filetime100ns(ft)
	if ns < 116444736000000000 {
		return 0
	}
	return int64((ns - 116444736000000000) / 10000)
}

// pmcEx PROCESS_MEMORY_COUNTERS_EX（PrivateUsage 即「私有字节」= 提交内存）
type pmcEx struct {
	Cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
	PrivateUsage               uintptr
}

// collectProcs 一次 Toolhelp32 进程快照 + 一次线程快照拿全表；
// 不用 gopsutil 的 p.Name()/p.NumThreads()（每个进程都要重开全量快照，几百进程扛不住）。
func collectProcs() []rawProc {
	// 线程数：一次 SNAPTHREAD 快照按属主进程累加（gopsutil 的 NumThreads 每进程都要重开快照）
	threadCnt := map[uint32]int{}
	if ts, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0); err == nil {
		var te windows.ThreadEntry32
		te.Size = uint32(unsafe.Sizeof(te))
		for err := windows.Thread32First(ts, &te); err == nil; err = windows.Thread32Next(ts, &te) {
			threadCnt[te.OwnerProcessID]++
		}
		windows.CloseHandle(ts)
	}

	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)

	out := make([]rawProc, 0, 256)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	for err := windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		row := ProcRow{
			Pid:     int(pe.ProcessID),
			Ppid:    int(pe.ParentProcessID),
			Name:    windows.UTF16ToString(pe.ExeFile[:]),
			Threads: threadCnt[pe.ProcessID],
		}
		r := rawProc{row: row}
		h, err := windows.OpenProcess(processQueryLimitedInformation, false, pe.ProcessID)
		if err == nil {
			var imgBuf [windows.MAX_PATH]uint16
			imgSize := uint32(len(imgBuf))
			if err := windows.QueryFullProcessImageName(h, 0, &imgBuf[0], &imgSize); err == nil {
				row.Exe = windows.UTF16ToString(imgBuf[:imgSize])
			}
			var creation, exit, kernel, user windows.Filetime
			if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err == nil {
				row.StartedAt = filetimeToMs(creation)
				cpu100ns := filetime100ns(kernel) + filetime100ns(user)
				r.cpuSec = float64(cpu100ns) / 1e7
				r.ok = true
			}
			var pmc pmcEx
			pmc.Cb = uint32(unsafe.Sizeof(pmc))
			if ret, _, _ := procGetMemInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&pmc)), uintptr(pmc.Cb)); ret != 0 {
				row.WorkingSet = uint64(pmc.WorkingSetSize)
				row.PrivateBytes = uint64(pmc.PrivateUsage)
			}
			windows.CloseHandle(h)
		}
		r.row = row
		out = append(out, r)
	}
	return out
}

// killProcs 先直接 TerminateProcess；权限不足的集中收集，一次提权 taskkill 收尾
// （树杀时避免每个受保护进程都弹一次 UAC）。
func killProcs(pids []int) error {
	var denied []int
	var hardErr string
	for _, pid := range pids {
		h, err := windows.OpenProcess(processTerminate, false, uint32(pid))
		if err == nil {
			if err := windows.TerminateProcess(h, 1); err != nil {
				if isAccessDenied(err) {
					denied = append(denied, pid)
				} else if hardErr == "" {
					hardErr = err.Error()
				}
			}
			windows.CloseHandle(h)
			continue
		}
		if isAccessDenied(err) {
			denied = append(denied, pid)
		} else if err == windows.ERROR_INVALID_PARAMETER {
			// 进程已退出：当成功
			continue
		} else if hardErr == "" {
			hardErr = err.Error()
		}
	}
	if len(denied) == 0 {
		if hardErr != "" {
			return fmt.Errorf("结束进程失败: %s", hardErr)
		}
		return nil
	}
	params := "/F"
	for _, p := range denied {
		params += fmt.Sprintf(" /PID %d", p)
	}
	code, err := winrun.RunElevatedWait(winrun.Elevated{
		File: "taskkill.exe", Params: params, Show: windows.SW_HIDE, Timeout: time.Minute,
	})
	if err != nil {
		if winrun.IsUACCancelled(err) {
			return fmt.Errorf("已取消管理员授权，%d 个受保护进程未结束", len(denied))
		}
		return fmt.Errorf("提权结束进程失败: %w", err)
	}
	if code != 0 && hardErr != "" {
		return fmt.Errorf("结束进程失败: %s", hardErr)
	}
	return nil
}

func isAccessDenied(err error) bool {
	return err == windows.ERROR_ACCESS_DENIED
}

// suspendProc NtSuspendProcess/NtResumeProcess（未公开 API，任务管理器同款路径）。
func suspendProc(pid int, on bool) error {
	h, err := windows.OpenProcess(processSuspendResume|processQueryLimitedInformation|processCreateThread|processVmOperation, false, uint32(pid))
	if err != nil {
		return needAdmin(err)
	}
	defer windows.CloseHandle(h)
	proc := procNtSuspend
	if on {
		proc = procNtSuspend
	} else {
		proc = procNtResume
	}
	if r, _, _ := proc.Call(uintptr(h)); r != 0 {
		return needAdmin(fmt.Errorf("ntstatus 0x%x", r))
	}
	return nil
}

// restartProc 先杀再起：命令行用 CIM 查一次（Toolhelp 拿不到）；
// 普通启动失败再走 runas 一次（原进程可能是管理员权限起的）。
func restartProc(pid int) error {
	mu.Lock()
	exe := lastRows[pid].Exe
	mu.Unlock()
	if exe == "" {
		return fmt.Errorf("未取到进程的可执行文件路径，无法重启")
	}
	cmdline := queryCmdline(pid)
	args := stripExeFromCmdline(cmdline, exe)
	if err := killProcs([]int{pid}); err != nil {
		return fmt.Errorf("重启前先结束进程失败: %w", err)
	}
	// 稍等半秒让句柄释放，个别程序锁文件
	time.Sleep(500 * time.Millisecond)
	if r := shellRun("open", exe, args, parentDir(exe)); r > 32 {
		return nil
	}
	if err := winrun.OpenElevated(exe, args); err != nil {
		if winrun.IsUACCancelled(err) {
			return fmt.Errorf("进程已结束；重启需要管理员授权，已取消")
		}
		return fmt.Errorf("进程已结束，但重新启动失败: %w", err)
	}
	return nil
}

func setProcPriority(pid int, class string) error {
	var m uint32
	switch class {
	case "high":
		m = 0x00000080 // HIGH_PRIORITY_CLASS
	case "low":
		m = 0x00000040 // IDLE_PRIORITY_CLASS
	case "normal":
		m = 0x00000020 // NORMAL_PRIORITY_CLASS
	default:
		return fmt.Errorf("未知优先级: %s", class)
	}
	h, err := windows.OpenProcess(processSetInformation|processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return needAdmin(err)
	}
	defer windows.CloseHandle(h)
	if err := windows.SetPriorityClass(h, m); err != nil {
		return needAdmin(err)
	}
	return nil
}

func procInfo(pid int) (ProcInfo, error) {
	mu.Lock()
	row, hit := lastRows[pid]
	mu.Unlock()
	if !hit {
		return ProcInfo{}, fmt.Errorf("进程 %d 不在最近快照里，请先刷新", pid)
	}
	info := ProcInfo{ProcRow: row}
	if row.Description == "" || row.Company == "" {
		d, c := cachedOrQueryVer(row.Exe)
		info.Description, info.Company = d, c
	}
	info.Cmdline = queryCmdline(pid)
	info.User = queryUser(pid)
	info.Bitness = queryBitness(pid)
	mu.Lock()
	info.ParentName = lastRows[row.Ppid].Name
	mu.Unlock()
	return info, nil
}

// needAdmin 把权限类错误翻译成人话，其他错误原样走。
func needAdmin(err error) error {
	return fmt.Errorf("需要管理员权限（%v）", err)
}

// queryCmdline 用一次隐藏 PowerShell 查 CIM 拿命令行（只在重启/属性时付出这笔开销）。
func queryCmdline(pid int) string {
	script := fmt.Sprintf(`(Get-CimInstance Win32_Process -Filter "ProcessId=%d" -ErrorAction SilentlyContinue).CommandLine`, pid)
	out, _ := winrun.RunHidden(20*time.Second, "powershell.exe",
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	return strings.TrimSpace(out)
}

// queryUser OpenProcessToken -> GetTokenUser -> LookupAccountSid（DOMAIN\user）。
func queryUser(pid int) string {
	h, err := windows.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	var tok windows.Token
	if err := windows.OpenProcessToken(h, windows.TOKEN_QUERY, &tok); err != nil {
		return ""
	}
	defer tok.Close()
	tu, err := tok.GetTokenUser()
	if err != nil || tu == nil {
		return ""
	}
	account, domain := lookupAccountName(tu.User.Sid)
	if account == "" {
		return ""
	}
	if domain == "" {
		return account
	}
	return domain + "\\" + account
}

// lookupAccountName 裸 LookupAccountSid 两段式调用（先探长度再取串）。
func lookupAccountName(sid *windows.SID) (string, string) {
	if sid == nil {
		return "", ""
	}
	var nameLen, domainLen, use uint32
	if err := windows.LookupAccountSid(nil, sid, nil, &nameLen, nil, &domainLen, &use); err != nil && nameLen == 0 {
		return "", ""
	}
	if domainLen == 0 {
		domainLen = 1
	}
	name := make([]uint16, nameLen)
	domain := make([]uint16, domainLen)
	if err := windows.LookupAccountSid(nil, sid, &name[0], &nameLen, &domain[0], &domainLen, &use); err != nil {
		return "", ""
	}
	return windows.UTF16ToString(name), windows.UTF16ToString(domain)
}

// queryBitness IsWow64Process -> "32 位" / "64 位"。
func queryBitness(pid int) string {
	h, err := windows.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	var wow bool
	if err := windows.IsWow64Process(h, &wow); err != nil {
		return ""
	}
	if wow {
		return "32 位"
	}
	return "64 位"
}

// ============================================================
// 版本信息（描述 / 公司名）
// ============================================================

func queryVer(path string) (string, string) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", ""
	}
	size, _, _ := procVerSize.Call(uintptr(unsafe.Pointer(p)), 0)
	if size == 0 {
		return "", ""
	}
	buf := make([]byte, size)
	if r, _, _ := procVerGet.Call(uintptr(unsafe.Pointer(p)), 0, size, uintptr(unsafe.Pointer(&buf[0]))); r == 0 {
		return "", ""
	}
	q := func(name string) string {
		for _, loc := range []string{`080404b0`, `040904b0`, `000004b0`} {
			key, _ := windows.UTF16PtrFromString(`\StringFileInfo\` + loc + `\` + name)
			var lp unsafe.Pointer
			var l uint32
			if r, _, _ := procVerQuery.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&lp)), uintptr(unsafe.Pointer(&l))); r != 0 && lp != nil {
				return windows.UTF16PtrToString((*uint16)(lp))
			}
		}
		return ""
	}
	return q("FileDescription"), q("CompanyName")
}

// cachedOrQueryVer 属性弹窗同步补齐版本信息（就一次文件 IO，点属性才走到，值得等）。
func cachedOrQueryVer(path string) (string, string) {
	if path == "" {
		return "", ""
	}
	mu.Lock()
	defer mu.Unlock()
	if v, hit := verCache[normPath(path)]; hit {
		return v[0], v[1]
	}
	d, c := queryVer(path)
	verCache[normPath(path)] = [2]string{d, c}
	return d, c
}

// ============================================================
// 图标 / 外链 / 路径
// ============================================================

var (
	// ProcIcon 是被前端并发调的（图标加载限流 6 路），裸 map 并发读写
	// 是 fatal error，recover 都拦不住，整个应用会直接挂掉。
	iconMu    sync.Mutex
	iconCache = map[string]string{} // 大写 exe 路径 -> dataURL
)

// lazyOK 探一下导出函数真的存在。LazyProc.Call 找不到函数是 panic 而非返回错误，
// 图标这种 best-effort 路径绝不能把 panic 抛进绑定调用。
func lazyOK(p *windows.LazyProc) bool { return p.Find() == nil }

// iconOfExe exe 首图标 -> 32x32 PNG dataURL（DrawIconEx 画进 32bpp DIB 再读像素）。
func iconOfExe(exe string) string {
	if exe == "" {
		return ""
	}
	key := normPath(exe)
	iconMu.Lock()
	s, hit := iconCache[key]
	iconMu.Unlock()
	if hit {
		return s
	}
	url := extractIconDataURL(exe) // 取图标是磁盘 IO，不持锁，让其他 pid 并行走
	iconMu.Lock()
	iconCache[key] = url
	iconMu.Unlock()
	return url
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

func extractIconDataURL(exe string) string {
	if !lazyOK(procExtractIcon) || !lazyOK(procDrawIcon) || !lazyOK(procCreateDIB) {
		return "" // 系统 DLL 真缺函数时留空，不拿 panic 去炸绑定调用
	}
	p, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return ""
	}
	size, _, _ := procSysMetric.Call(11) // SM_CXICON
	if size == 0 {
		size = 32
	}
	sz := int32(size)
	var large, small uintptr
	// ExtractIconExW(lpszFileName, nIconIndex, phiconLarge, phiconSmall, nIcons)
	n, _, _ := procExtractIcon.Call(uintptr(unsafe.Pointer(p)), 0,
		uintptr(unsafe.Pointer(&large)), uintptr(unsafe.Pointer(&small)), 1)
	if n == 0 {
		if small != 0 {
			procDestroyIcon.Call(small)
		}
		return ""
	}
	hicon := large
	if hicon == 0 {
		hicon = small
	}

	screenDC, _, _ := procGetDC.Call(0)
	memDC, _, _ := procCreateCompDC.Call(screenDC)
	var bmi bitmapInfoHeader
	bmi = bitmapInfoHeader{
		Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: sz,
		Height: -sz, Planes: 1, BitCount: 32,
	}
	var bitsPtr unsafe.Pointer
	dib, _, _ := procCreateDIB.Call(memDC, uintptr(unsafe.Pointer(&bmi)), 0,
		uintptr(unsafe.Pointer(&bitsPtr)), 0, 0)
	dataURL := ""
	if dib != 0 && bitsPtr != nil {
		old, _, _ := procSelectObj.Call(memDC, dib)
		procDrawIcon.Call(memDC, 0, 0, hicon, uintptr(sz), uintptr(sz), 0, 0, 3) // DI_NORMAL
		pixels := unsafe.Slice((*byte)(bitsPtr), int(sz)*int(sz)*4)
		dataURL = encodeBGRA(pixels, int(sz))
		procSelectObj.Call(memDC, old)
		procDeleteObj.Call(dib)
	}
	procDeleteDC.Call(memDC)
	procReleaseDC.Call(screenDC, 0)
	if large != 0 {
		procDestroyIcon.Call(large)
	}
	if small != 0 && small != large {
		procDestroyIcon.Call(small)
	}
	return dataURL
}

// encodeBGRA DIB 像素（BGRA，DrawIconEx 输出按黑底合成）转 PNG dataURL；
// alpha 全 0 的老式图标整幅按不透明处理。
func encodeBGRA(px []byte, sz int) string {
	if len(px) < sz*sz*4 {
		return ""
	}
	allZero := true
	for i := 3; i < len(px); i += 4 {
		if px[i] != 0 {
			allZero = false
			break
		}
	}
	img := image.NewRGBA(image.Rect(0, 0, sz, sz))
	for i := 0; i < sz*sz; i++ {
		b, g, r, a := px[i*4], px[i*4+1], px[i*4+2], px[i*4+3]
		if allZero {
			a = 255
		}
		img.SetRGBA(i%sz, i/sz, color.RGBA{R: r, G: g, B: b, A: a})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// shellRun ShellExecuteW 普通启动（verb 传 "open"），返回值 >32 为成功。
func shellRun(verb, file, args, dir string) int {
	vp, _ := windows.UTF16PtrFromString(verb)
	fp, err := windows.UTF16PtrFromString(file)
	if err != nil {
		return 0
	}
	var ap, dp *uint16
	if args != "" {
		ap, _ = windows.UTF16PtrFromString(args)
	}
	if dir != "" {
		dp, _ = windows.UTF16PtrFromString(dir)
	}
	r, _, _ := procShellExecW.Call(uintptr(unsafe.Pointer(vp)), uintptr(unsafe.Pointer(fp)),
		uintptr(unsafe.Pointer(ap)), uintptr(unsafe.Pointer(dp)), uintptr(1)) // SW_SHOWNORMAL
	return int(int32(r))
}

// explorerSelectLine 拼 explorer「打开所在目录并选中该文件」的命令行。
// 引号只能包路径、不能包整个参数：走 exec.Command 传参时，Go 会把含空格的
// 整段（连 /select, 前缀一起）用引号括起来，explorer 自己的命令行解析认不出来，
// 结果是弹个默认页而不是定位到目标文件（带空格的路径必现）。
func explorerSelectLine(exe string) string {
	return `explorer.exe /select,"` + exe + `"`
}

func openExePath(exe string) error {
	if err := startRaw(explorerSelectLine(exe)); err != nil {
		return fmt.Errorf("打开文件位置失败: %w", err)
	}
	return nil
}

// startRaw 用 CreateProcess 原样投递命令行（不经 Go 的参数转义），不等退出。
// explorer 起来就退、退出码还常常是 1，所以只当“拉起来了”看待，不等结果。
func startRaw(cmdLine string) error {
	lp, err := windows.UTF16PtrFromString(cmdLine)
	if err != nil {
		return err
	}
	var si windows.StartupInfo
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi windows.ProcessInformation
	if err := windows.CreateProcess(nil, lp, nil, nil, false, 0, nil, nil, &si, &pi); err != nil {
		return err
	}
	windows.CloseHandle(pi.Process)
	windows.CloseHandle(pi.Thread)
	return nil
}

func parentDir(p string) string {
	if i := strings.LastIndexAny(p, "\\/"); i > 0 {
		return p[:i]
	}
	return ""
}

// stripExeFromCmdline 从完整命令行里剥掉开头的可执行文件段，返回剩余参数。
func stripExeFromCmdline(cmdline, exe string) string {
	c := strings.TrimSpace(cmdline)
	if c == "" {
		return ""
	}
	if strings.HasPrefix(c, `"`) {
		if i := strings.Index(c[1:], `"`); i >= 0 {
			return strings.TrimSpace(c[i+2:])
		}
		return ""
	}
	// 无引号：第一段就是 exe（可能带路径），按空格切掉第一段
	fields := strings.SplitN(c, " ", 2)
	if len(fields) < 2 {
		return ""
	}
	return strings.TrimSpace(fields[1])
}
