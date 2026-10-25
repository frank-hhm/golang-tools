import { reactive } from "vue";
import { App } from "bindings/go-tools/services/app/index";
import type { Capabilities } from "bindings/go-tools/services/sysx/models";

/**
 * 平台能力：从 Go 侧读一次，前端按能力渲染，而不是按 os 字符串硬编码分支。
 *
 * 重要的一条是 nativeWindows —— 它表示"当前构建能不能真的弹出原生窗口"。
 * Linux 与 Windows 的 -tags server 构建都是 false，此时所有"开新窗口"的入口
 * （工具图标、截图、内嵌浏览器）都要走页面内路由或直接隐藏。
 */

/** 初始值取最保守的 Web 模式：探测返回前不闪出一堆点了没反应的入口 */
const state = reactive<Capabilities>({
    os: "",
    arch: "",
    initSystem: "direct",
    isRoot: false,
    hasDocker: false,
    pkgMode: "green",
    nativeWindows: false,
});

let pending: Promise<Capabilities> | null = null;

/** 读取平台能力（进程内只请求一次，重复调用复用同一个 Promise） */
export function loadCaps(): Promise<Capabilities> {
    if (!pending) {
        pending = App.GetCapabilities()
            .then((c) => Object.assign(state, c || {}))
            .catch(() => { /* 取不到就保持保守默认值 */ })
            .then(() => ({ ...state } as Capabilities));
    }
    return pending;
}

/** 响应式能力快照：模板里用 caps.nativeWindows 等字段即可 */
export function useCaps() {
    return state;
}

/** 能真正开原生窗口（Windows 桌面版） */
export function isNativeWindows() {
    return state.nativeWindows;
}

/**
 * 完全依赖原生窗口的工具路由：内嵌浏览器靠 WebView2 嵌窗口，截图靠 Win32 抓屏。
 * 这两种能力在 Linux / -tags server 构建里根本没有对应绑定，进得去也用不了。
 */
const DESKTOP_ONLY_PATHS = new Set(["/tools/browser", "/tools/screenshot"]);

/** 当前平台能不能用这个工具（route path 或 event 二选一） */
export function isToolAvailable(path?: string, event?: string) {
    if (state.nativeWindows) return true;
    if (event === "screenshot") return false;
    return !(path && DESKTOP_ONLY_PATHS.has(path));
}
