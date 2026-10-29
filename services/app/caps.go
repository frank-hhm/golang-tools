package app

import "Tools/services/sysx"

// GetCapabilities 返回平台能力快照，供前端按能力渲染。
//
// 前端不再靠 os 字符串硬编码分支：能不能开原生窗口、服务是不是 systemd 托管、
// 包管理走绿色版还是系统包，全都读这里。字段定义见 sysx.Capabilities。
func (a *App) GetCapabilities() sysx.Capabilities {
	return sysx.Caps()
}
