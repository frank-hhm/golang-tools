import { createApp } from 'vue'
import App from "@/App.vue"
import router from '@/router'
import "@/router/permission"

// load
import { loadPlugins } from "@/plugins"
//css
import "@/styles/index.scss"

import * as Utils from '@/utils'
import { setupGoMessage } from '@/utils/gomessage'
import { initTheme, syncThemeFromConfig } from '@/utils/theme'
import * as GoToolsBindings from "bindings/go-tools/services/app/index";
import { ScreenshotService as GoScreenshot } from "bindings/go-tools/tools/screenshot/index";

const app = createApp(App);

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

router.isReady().then(() => {
    app.mount("#app")
    // 主题：同步应用本地记忆（多窗口即时同步），再用后端配置作为首次运行回退
    initTheme()
    GoToolsBindings.App.GetConfig?.()
        .then((cfg: any) => syncThemeFromConfig(cfg?.general?.theme))
        .catch(() => { /* */ })
}).catch(err => {
    console.log(err)
})

