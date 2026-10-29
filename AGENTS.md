# AGENTS.md

## 硬约束：所有功能必须同时兼容 Windows 与 Linux

Tools 是同一份代码跑三种构建：

| 目标 | 构建命令 | 形态 |
| --- | --- | --- |
| Windows 桌面 | `go build .` | Wails + WebView2，有原生窗口 |
| Linux 面板 | `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags server .` | 无头，浏览器访问 |
| Windows Web | `go build -tags server .` | 同上，只是跑在 Windows 上 |

**任何新增或修改的功能，Windows 与 Linux 两条命令都必须是 0 退出码。**

`CGO_ENABLED=0` 不是建议而是硬要求：Linux 服务器经常没装 gcc，一旦引入 cgo 就整个编不出来。

提交前自查（PowerShell 写法，Bash 自行换成前缀形式）：

```powershell
gofmt -l services tools                                     # 自己改的文件不能出现在这里
go vet ./...                                                # 不应引入新问题
go test ./tools/... -count=1
go build .                                                  # Windows 桌面
$env:GOOS="linux"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"
go build -tags server -o bin/_linux_check .                  # Linux 面板
```

`bin/` 已被忽略，编译产物丢这里，不要污染工作区。

## 平台差异怎么写

按差异的**性质**分三层，不许混用：

| 差异性质 | 做法 | 例子 |
| --- | --- | --- |
| 实现不同 | 公共层写接口与调用方，平台文件各给一份：`xxx_windows.go` / `xxx_other.go` | 命名管道 vs unix socket；DPAPI vs 无 |
| 常量不同 | 平台文件里各给一个 `const` | `rootfs_windows.go`（`"C:"`）/ `rootfs_other.go`（`"/"`）；`daemon_windows.go` / `daemon_other.go` |
| 运行时事实 | 收进 `sysx.Capabilities`，`sysx.Caps()` 是唯一探测入口 | 是否 root、有无 systemd、能不能连 docker、能不能开原生窗口 |

反例，不要这样写：

```go
if runtime.GOOS == "linux" { ... }   // 业务代码里按 os 字符串分叉
path := "C:\\data"                    // 写死盘符或反斜杠
```

`sysx.Caps()` 内部有 `sync.Once`，把它当纯函数用没关系，但**不要在它里面做会变的探测**（比如"现在 docker 在不在跑"），那类问题交给各自模块的缓存与重扫机制。

## 绑定方法不能藏在 build tag 后面

Wails 的方法 id 由方法名决定。`services/app` 里被前端调用的方法一旦加了 `//go:build windows`，Linux 构建里就整个消失，前端调用时报 `unknown bound method id <数字>`——页面能渲染但数据全是 `--`，排查成本极高（这个坑已经踩过一次）。

- 对前端暴露的方法**必须跨平台存在**。
- 确实只支持某个平台（WebView2 内嵌浏览器、Win32 抓屏）时，方法照常保留，在非支持平台返回 `sysx.ErrUnsupported`，不要用 build tag 让它消失。
- 新增或改名方法后重新生成绑定：`wails3 generate bindings -clean=true -ts -i`，并把 `frontend/bindings` 的变更一起提交。

## 前端按能力渲染，不按平台字符串

依赖桌面能力的新入口，先在 `sysx.Capabilities` 加字段（参考 `nativeWindows`），前端读 `@/utils/caps` 的 `useCaps()` / `isToolAvailable()` 决定显隐。

`caps.os` 可以用来调整**文案**（"命名管道" vs "unix socket"、"Windows DPAPI" vs "文件权限保护"），但决定**功能有无**的必须是能力字段。

调用 `$GoTools.OpenToolWindow` 这类原生窗口 API 前先判断能力，不要靠"调用失败再回退"——Web 模式下它可能不报错却什么也不做（`NewWithOptions` 返回成功但窗口不出现）。

## 权限与现实差异要如实说明

- `/var/run/docker.sock` 需要 root 或在 docker 组内；rootless 的端点在 `$XDG_RUNTIME_DIR/docker.sock`。
- DPAPI 在 Linux 没有等价物，仓库密码退化为 base64 + 文件权限。UI 与注释都必须写明"不提供机密性"，不允许含糊成"已加密"。
- Linux 上引擎的安装与启停交给 systemd 和包管理器，面板只负责连上去并给出排查线索。

## 不接受"编译通过即兼容"

- 新增 `tools/*` 包要带 `go test`，并把测试跑起来。
- 交付说明里如实写清验证到了哪一层：只编译通过 / 跑过单测 / 真机端到端跑过。**没有 Linux 环境就明确说没有**。

## 其它一致性要求

- 用 Write 新建 Go 文件后必须 `gofmt -w`（Write 产物是空格缩进，`gofmt -l` 会发现）。
- 平台文件命名统一 `*_windows.go` / `*_other.go`，build tag 表达式与既有文件保持一致（`//go:build windows` / `//go:build !windows`，需要区分 server 构建时用 `windows && !server` / `!windows || server`）。
- 前端改动会进 `frontend/dist`，而 `dist` 不入库：交付时要提醒重新构建前端（`cd frontend && npm run build`），否则服务器上看到的仍是旧页面。
