//go:build windows

package edge

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

type iCoreWebView2_3Vtbl struct {
	iCoreWebView2_2Vtbl
	TrySuspend                          ComProc
	Resume                              ComProc
	GetIsSuspended                      ComProc
	SetVirtualHostNameToFolderMapping   ComProc
	ClearVirtualHostNameToFolderMapping ComProc
}

type ICoreWebView2_3 struct {
	vtbl *iCoreWebView2_3Vtbl
}

func (i *ICoreWebView2_3) AddRef() uint32 {
	ret, _, _ := i.vtbl.AddRef.Call(uintptr(unsafe.Pointer(i)))

	return uint32(ret)
}

func (i *ICoreWebView2_3) Release() uint32 {
	ret, _, _ := i.vtbl.Release.Call(uintptr(unsafe.Pointer(i)))

	return uint32(ret)
}

func (i *ICoreWebView2_3) SetVirtualHostNameToFolderMapping(hostName, folderPath string, accessKind COREWEBVIEW2_HOST_RESOURCE_ACCESS_KIND) error {
	_hostName, err := windows.UTF16PtrFromString(hostName)
	if err != nil {
		return err
	}

	_folderPath, err := windows.UTF16PtrFromString(folderPath)
	if err != nil {
		return err
	}

	hr, _, _ := i.vtbl.SetVirtualHostNameToFolderMapping.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(unsafe.Pointer(_hostName)),
		uintptr(unsafe.Pointer(_folderPath)),
		uintptr(accessKind),
	)
	if windows.Handle(hr) != windows.S_OK {
		return windows.Errno(hr)
	}

	return nil
}

func (i *ICoreWebView2) GetICoreWebView2_3() *ICoreWebView2_3 {
	var result *ICoreWebView2_3

	iidICoreWebView2_3 := NewGUID("{A0D6DF20-3B92-416D-AA0C-437A9C727857}")
	_, _, _ = i.vtbl.QueryInterface.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(unsafe.Pointer(iidICoreWebView2_3)),
		uintptr(unsafe.Pointer(&result)))

	return result
}

func (e *Chromium) GetICoreWebView2_3() *ICoreWebView2_3 {
	return e.webview.GetICoreWebView2_3()
}

// ============================================================
// 挂起 / 恢复（go-tools 扩展）
//
// 非活动标签长时间不用时调用 TrySuspend，WebView2 会把渲染进程降下来
// 释放大部分内存；用户切回来时 Resume，页面状态不丢。
// vtable 里本来就有这几个槽位，上游没有导出。
// ============================================================

// trySuspendCallbackImpl 把「挂起完成」转成一个普通 Go 回调。
type trySuspendCallbackImpl struct {
	fn func(bool)
}

func (c *trySuspendCallbackImpl) QueryInterface(_, _ uintptr) uintptr { return 0 }

func (c *trySuspendCallbackImpl) AddRef() uintptr { return 1 }

func (c *trySuspendCallbackImpl) Release() uintptr { return 1 }

func (c *trySuspendCallbackImpl) TrySuspendCompleted(_ uintptr, ok bool) uintptr {
	if c.fn != nil {
		c.fn(ok)
	}
	return 0
}

// Suspend 尝试挂起，结果通过 callback 回来（true = 真的挂起了）。
//
// 页面在放音视频、持锁、或 WebView 还可见时可能挂起失败，这属于正常情况。
func (i *ICoreWebView2_3) Suspend(callback func(success bool)) error {
	h := newICoreWebView2TrySuspendCompletedHandler(&trySuspendCallbackImpl{fn: callback})
	return i.TrySuspend(h)
}

// TrySuspend 请求挂起。异步：真正的结果通过 handler 回来。
func (i *ICoreWebView2_3) TrySuspend(handler *iCoreWebView2TrySuspendCompletedHandler) error {
	hr, _, _ := i.vtbl.TrySuspend.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(unsafe.Pointer(handler)),
	)
	if windows.Handle(hr) != windows.S_OK {
		return windows.Errno(hr)
	}
	return nil
}

// Resume 从挂起状态恢复。
func (i *ICoreWebView2_3) Resume() error {
	hr, _, _ := i.vtbl.Resume.Call(uintptr(unsafe.Pointer(i)))
	if windows.Handle(hr) != windows.S_OK {
		return windows.Errno(hr)
	}
	return nil
}

// GetIsSuspended 当前是否处于挂起状态。
func (i *ICoreWebView2_3) GetIsSuspended() (bool, error) {
	var suspended int32 // BOOL
	hr, _, _ := i.vtbl.GetIsSuspended.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(unsafe.Pointer(&suspended)),
	)
	if windows.Handle(hr) != windows.S_OK {
		return false, windows.Errno(hr)
	}
	return suspended != 0, nil
}
