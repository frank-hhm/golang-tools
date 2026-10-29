module go-tools

go 1.25.0

require (
	github.com/go-sql-driver/mysql v1.8.1
	github.com/google/gopacket v1.1.19
	github.com/kbinani/screenshot v0.0.0-20250624051815-089614a94018
	github.com/pkg/sftp v1.13.12
	github.com/shirou/gopsutil/v3 v3.24.5
	github.com/wailsapp/go-webview2 v1.0.22
	github.com/wailsapp/wails/v3 v3.0.0-beta.12
	go.bug.st/serial v1.8.0
	golang.org/x/crypto v0.54.0
	golang.org/x/net v0.56.0
	golang.org/x/sys v0.47.0
	golang.org/x/text v0.40.0
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/adrg/xdg v0.5.3 // indirect
	github.com/coder/websocket v1.8.14 // indirect
	github.com/gen2brain/shm v0.1.0 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/jchv/go-winloader v0.0.0-20250406163304-c1995be93bd1 // indirect
	github.com/jezek/xgb v1.1.1 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/lxn/win v0.0.0-20210218163916-a377121e959e // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/power-devops/perfstat v0.0.0-20210106213030-5aafc221ea8c // indirect
	github.com/shoenig/go-m1cpu v0.1.6 // indirect
	github.com/tklauser/go-sysconf v0.3.12 // indirect
	github.com/tklauser/numcpus v0.6.1 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
)

// replace github.com/wailsapp/wails/v2 v2.14.0 => C:\Users\Administrator\go\pkg\mod

// 嵌入式浏览器用的 WebView2 宿主：官方 v1.0.22 的本地副本，只改了两处
// （见 third_party/go-webview2/pkg/edge/chromium.go 的 errorCallback 注释、
// ICoreWebView2Controller.Close 的补充）。升级时整目录替换 + 重打这两处补丁。
replace github.com/wailsapp/go-webview2 => ./third_party/go-webview2
