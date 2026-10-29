import { computed, reactive } from "vue";
import { App } from "bindings/Tools/services/app/index";
import type { Capabilities } from "bindings/Tools/services/sysx/models";

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

/**
 * 读取平台能力（进程内只请求一次，重复调用复用同一个 Promise）。
 *
 * 失败时把 pending 清掉：预取可能发得比运行时就绪更早，一次失败不该让整个
 * 会话都停在保守默认值上（Windows 桌面版那样会丢掉所有原生窗口入口）。
 */
export function loadCaps(): Promise<Capabilities> {
    if (!pending) {
        pending = App.GetCapabilities()
            .then((c) => Object.assign(state, c || {}))
            .catch(() => { pending = null; /* 取不到就保持保守默认值，下次调用可重试 */ })
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
 * 系统包模式（Linux 的 hybrid/system）：版本与扩展都来自发行版仓库。
 *
 * 绿色版那套「下载 zip 解压到插件目录 / 往 ext 里放 dll」在这里一条也走不通，
 * 页面要据此换成包管理器引导与 pkg: 包名，而不是给一个点了必然失败的按钮。
 */
export function useSysPkg() {
    return computed(() => state.pkgMode !== "green");
}

/**
 * 在线版本条目的右侧文案：绿色版是下载体积，系统包模式是 pkg:<包名> 里的包名。
 * 四个工具（PHP / MySQL / Redis / Nginx）的版本管理都用这一份，避免各自拼。
 */
export function remoteVerText(r: any): string {
    const url = String(r?.url || "");
    if (url.startsWith("pkg:")) return url.slice(4);
    const size = Number(r?.size || 0);
    return size > 0 ? (size / 1024 / 1024).toFixed(0) + " MB" : "";
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
