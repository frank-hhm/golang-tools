//go:build windows

package edge

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

type _ICoreWebView2SourceChangedEventArgsVtbl struct {
	_IUnknownVtbl
	GetIsNewDocument ComProc
}

type ICoreWebView2SourceChangedEventArgs struct {
	vtbl *_ICoreWebView2SourceChangedEventArgsVtbl
}

func (i *ICoreWebView2SourceChangedEventArgs) AddRef() uint32 {
	ret, _, _ := i.vtbl.AddRef.Call(uintptr(unsafe.Pointer(i)))

	return uint32(ret)
}

func (i *ICoreWebView2SourceChangedEventArgs) Release() uint32 {
	ret, _, _ := i.vtbl.Release.Call(uintptr(unsafe.Pointer(i)))

	return uint32(ret)
}

// GetIsNewDocument 这次 URL 变化是否伴随新的文档（false 表示是同文档内的
// pushState/replaceState 之类的路由切换）。
func (i *ICoreWebView2SourceChangedEventArgs) GetIsNewDocument() (bool, error) {
	var isNewDocument int32 // BOOL
	hr, _, _ := i.vtbl.GetIsNewDocument.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(unsafe.Pointer(&isNewDocument)),
	)
	if windows.Handle(hr) != windows.S_OK {
		return false, windows.Errno(hr)
	}
	return isNewDocument != 0, nil
}
