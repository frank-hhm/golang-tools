import { createApp } from 'vue'
import App from "@/App.vue"
import router from '@/router'
import { installRouterProgress } from "@/router/permission"

// load
import { loadPlugins } from "@/plugins"
//css
import "@/styles/index.scss"

import * as Utils from '@/utils'
import { setupGoMessage } from '@/utils/gomessage'
import { initTheme, syncThemeFromConfig } from '@/utils/theme'
import { loadCaps } from '@/utils/caps'
import { installPanelGuard } from '@/utils/panel'
import { bindPrefetchRouter, prefetchAllRoutes } from '@/router/prefetch'
import * as GoToolsBindings from "bindings/Tools/services/app/index";
import { ScreenshotService as GoScreenshot } from "bindings/Tools/tools/screenshot/index";

const app = createApp(App);

/**
 * 守卫与预热都依赖同一个 router 实例，这里显式注入。
 * 以前 permission.ts 在模块顶层 import router 后直接 beforeEach，而它的使用方
 * （layouts → SideNav）又在 router 的导入链上；生产构建合并 chunk 后会在 router
 * 赋值前执行，抛 TypeError 导致整个 SPA 不挂载。
 */
installRouterProgress(router)
bindPrefetchRouter(router)

/**
 * 平台能力预取：与路由解析、首屏 chunk 加载并行跑。
 *
 * 各页面据此决定显不显示 Windows 专属入口，所以挂载前必须拿到；但没必要等
 * router.isReady() 之后才发请求（那样白等一个完整往返）。这里先发起、后 await。
 */
const capsReady = loadCaps();

/** 启用 Go → Arco Message 消息桥接（demo，监听 ui:message 事件） */
setupGoMessage();

/** 加载插件 */
loadPlugins(app)

app.config.globalProperties.$utils = Utils;
/**
 * $GoTools 封装 Wails v3 暴露的 Golang 方法：
 *  - 直接平铺 App / ScreenshotService 等方法，可用 $GoTools.GetSerialPorts() / $GoTools.CaptureSelectedRegion() 等形式直接调用
 *  - 同时保留命名空间（$GoTools.App / $GoTools.ScreenshotService），便于多服务区分与类型提示
 */
app.config.globalProperties.$GoTools = {
    ...GoToolsBindings.App,
    ...GoScreenshot,
    ...GoToolsBindings,
    ScreenshotService: GoScreenshot,
};

router.isReady().then(async () => {
    let caps = await capsReady
    // 预取失败（运行时比请求晚就绪）时补一次，否则整会话都停在保守默认值上
    if (!caps.os) caps = await loadCaps()
    app.mount("#app")
    // 主题：同步应用本地记忆（多窗口即时同步），再用后端配置作为首次运行回退
    initTheme()
    if (!caps.nativeWindows) {
        // Web 模式：登录态过期自动回登录页；随后空闲预热各工具页 chunk
        installPanelGuard()
        prefetchAllRoutes()
    }
    GoToolsBindings.App.GetConfig?.()
        .then((cfg: any) => syncThemeFromConfig(cfg?.general?.theme))
        .catch(() => { /* */ })
}).catch(err => {
    console.log(err)
})

