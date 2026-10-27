package nginx

// ============================================================
// 系统 hosts 文件：域名绑定后就地指引用户把域名指到本机。
// 路径与"打开方式"是平台事实，收在 *_windows.go / *_other.go；
// 这里只保留跨平台一致的导出入口（绑定方法不能藏在 build tag 后面）。
// ============================================================

// HostsPath 返回本机系统 hosts 文件的绝对路径（供前端展示与拼接建议行）。
func HostsPath() string { return hostsPath }

// OpenHosts 打开本机 hosts 文件用于配置域名解析。
// 确实需要提权/图形环境的平台差异由平台实现如实反映（返回相应错误，不静默失败）。
func OpenHosts() error { return openHostsFile() }
