//go:build windows

package edge

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

type _ICoreWebView2NewWindowRequestedEventArgsVtbl struct {
	_IUnknownVtbl
	GetUri       ComProc
	PutNewWindow ComProc
	GetNewWindow ComProc
	PutHandled   ComProc
	GetHandled   ComProc
}

type ICoreWebView2NewWindowRequestedEventArgs struct {
	vtbl *_ICoreWebView2NewWindowRequestedEventArgsVtbl
}

func (i *ICoreWebView2NewWindowRequestedEventArgs) AddRef() uint32 {
	ret, _, _ := i.vtbl.AddRef.Call(uintptr(unsafe.Pointer(i)))

	return uint32(ret)
}

func (i *ICoreWebView2NewWindowRequestedEventArgs) Release() uint32 {
	ret, _, _ := i.vtbl.Release.Call(uintptr(unsafe.Pointer(i)))

	return uint32(ret)
}

// GetUri 页面希望打开的地址。
func (i *ICoreWebView2NewWindowRequestedEventArgs) GetUri() (string, error) {
	var uri *uint16
	hr, _, _ := i.vtbl.GetUri.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(unsafe.Pointer(&uri)),
	)
	if windows.Handle(hr) != windows.S_OK {
		return "", windows.Errno(hr)
	}
	if uri == nil {
		return "", nil
	}
	result := windows.UTF16PtrToString(uri)
	windows.CoTaskMemFree(unsafe.Pointer(uri))
	return result, nil
}

// PutHandled 置 true 告诉 WebView2「这个新窗口请求我处理了」，
// 从而压掉它默认弹出的新窗口。
func (i *ICoreWebView2NewWindowRequestedEventArgs) PutHandled(handled bool) error {
	var v int32 // BOOL
	if handled {
		v = 1
	}
	hr, _, _ := i.vtbl.PutHandled.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(v),
	)
	if windows.Handle(hr) != windows.S_OK {
		return windows.Errno(hr)
	}
	return nil
}
