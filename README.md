# Welcome to Your New Wails3 Project!

Congratulations on generating your Wails3 application! This README will guide you through the next steps to get your project up and running.

## Getting Started

1. Navigate to your project directory in the terminal.

2. To run your application in development mode, use the following command:

   ```
   wails3 dev
   ```

   This will start your application and enable hot-reloading for both frontend and backend changes.

3. To build your application for production, use:

   ```
   wails3 build
   ```

   This will create a production-ready executable in the `build` directory.

## Exploring Wails3 Features

Now that you have your project set up, it's time to explore the features that Wails3 offers:

1. **Check out the examples**: The best way to learn is by example. Visit the `examples` directory in the `v3/examples` directory to see various sample applications.

2. **Run an example**: To run any of the examples, navigate to the example's directory and use:

   ```
   go run .
   ```

   Note: Some examples may be under development during the alpha phase.

3. **Explore the documentation**: Visit the [Wails3 documentation](https://v3.wails.io/) for in-depth guides and API references.

4. **Join the community**: Have questions or want to share your progress? Join the [Wails Discord](https://discord.gg/JDdSxwjhGf) or visit the [Wails discussions on GitHub](https://github.com/wailsapp/wails/discussions).

## Project Structure

Take a moment to familiarize yourself with your project structure:

- `frontend/`: Contains your frontend code (HTML, CSS, JavaScript/TypeScript)
- `main.go`: The entry point of your Go backend
- `app.go`: Define your application structure and methods here
- `wails.json`: Configuration file for your Wails project

### Go 侧目录布局

根目录只留「绑定面」：`*App` 的方法与它签名里出现的结构体会被 Wails 生成到
`frontend/bindings/go-tools/`，所以这些文件必须留在 package main：

| 根目录文件 | 作用 |
| --- | --- |
| `main.go` / `main_other.go` | 入口（windows / 非 windows 各一份） |
| `app.go` | `*App` 绑定方法 + 托盘/窗口/生命周期 + `nativeHost` 适配 |
| `capture.go` | 截图浮层窗口的创建、预热、显隐与收场 |
| `config.go` | `AppConfig` 等配置类型（设置页数据契约）与读写 |
| `sysinfo.go` | `SystemInfo` 等系统信息类型与采集 |
| `greetservice.go` | 脚手架示例服务 |

实现层按需下沉，新增能力请放对应目录而不要再往根目录堆：

- `services/applog/` 全局可配置日志（`applog.Infof/Warnf/Errorf/Debugf/Fail`，调用点带 tag）
- `services/winui/` 原生 Win32 界面：桌面浮动球 + 全局热键（不读配置，配置/回调经 `Host` 由 `nativeHost` 注入）
- `appdirs/` 三类基础目录的解析与标准布局落建（`Set`/`ApplyLayout`/旧布局迁移）
- `tools/<服务名>/` 各工具后端（php/nginx/mysql/redis/npm/sniffer/usbserial/screenshot/ocr…）

## Redis 本地服务管理（内置版本目录）

Redis 工具页支持直接管理本机 `redis-server` 实例，版本二进制有三个渠道：程序内置、在线下载（GitHub redis-windows，≥ 7.0）、手动添加本机目录。

如需随程序内置某个版本（开箱可用，无需下载），发布时将解压后的 `redis-server.exe` 放入程序目录的插件目录即可：

```
<发布目录>/
├── go-tools.exe
├── logs/                            # 默认日志目录（按级别+日期自动分文件）
├── data/
│   ├── videos/                      # 录制视频另存默认位置
│   └── screenshots/                 # 截图保存 PNG 默认位置
└── plugins/
    ├── redis/
    │   ├── 7.4.4/                   # 内置版本（或直接把 zip 放在这层离线安装）
    │   │   └── redis-server.exe
    │   └── Redis-7.4.4-Windows-x64-msys2.zip
    └── mysql/
        ├── 8.4.6/                   # 内置 MySQL 版本
        │   └── bin/mysqld.exe
        └── mysql-8.4.6-winx64.zip # 离线安装 zip（约 230MB）
```

- 程序启动时自动扫描 `<exe 所在目录>/plugins/redis/<版本号>/redis-server.exe`，识别为“内置”来源（兼容旧目录 `<exe>/redis-servers/`）；内置版本不可在版本管理中删除。
- 在线获取/离线 zip 安装的版本统一落在 `<exe>/plugins/redis/<版本号>/`（内含 `.go-tools-installed` 标记，列表显示为“下载”、可删）；无标记的同目录版本视为发布内置（显示“内置”、不可删）。旧版缓存 `%UserConfigDir%/go-tools/redis-servers/` 会在首次列出版本时自动迁移到插件目录。
- 不要把 redis 二进制提交进代码仓库；仅在打包发布时放入。实例配置与运行数据在 `%UserConfigDir%/go-tools/` 下的 `redis-instances.json` 和 `redis-instances/<实例id>/`。
- 离线安装兜底：将发行版 zip（如 `Redis-7.4.4-Windows-x64-msys2.zip`）直接放入 `<exe>/plugins/redis/`，在版本管理弹窗点对应版本的“安装”即可不经网络完成安装（弹窗内有实时安装日志）。

### MySQL 本地服务管理

与 Redis 对称，支持多实例 MySQL 服务管理。官方免安装 zip（约 230MB，解压约 1.5GB）。

- 插件目录 `<exe>/plugins/mysql/<版本号>/`；安装版写 `.go-tools-installed` 标记，可删；无标记=内置不可删。
- 实例配置 `%UserConfigDir%/go-tools/mysql-instances.json`；数据/日志 `mysql-instances/<id>/`（my.ini + data/ + mysql.err）。
- 首次启动自动 `mysqld --initialize-insecure`（root 空密码）；配置了 RootPassword 则启动后 ALTER USER 设置。
- 离线安装：将 `mysql-<ver>-winx64.zip` 放入 `<exe>/plugins/mysql/`，点“安装”即可。
- 依赖 VC++ 运行库（Visual C++ 2019 Redistributable）；若启动失败请检查 mysql.err 尾部是否有 “VCRUNTIME140” 提示。
- 不要把 mysql 二进制提交进代码仓库；仅在打包发布时放入。

## Next Steps

1. Modify the frontend in the `frontend/` directory to create your desired UI.
2. Add backend functionality in `main.go`.
3. Use `wails3 dev` to see your changes in real-time.
4. When ready, build your application with `wails3 build`.

Happy coding with Wails3! If you encounter any issues or have questions, don't hesitate to consult the documentation or reach out to the Wails community.
