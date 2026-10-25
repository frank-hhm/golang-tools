import { getCurrentInstance } from "vue";
import router from "@/router";
import { isNativeWindows } from "@/utils/caps";

/** 新窗口打开选项 */
export interface OpenToolWindowOptions {
    /** 是否单例窗口（true：已存在则复用/聚焦，不重复开） */
    single?: boolean;
    /** 新窗口是否铺满显示器（true：最大化启动；否则沿用常规尺寸） */
    fullscreen?: boolean;
    /** 新窗口视口宽度，<=0 或未设定则继承默认尺寸 */
    width?: number;
    /** 新窗口视口高度，<=0 或未设定则继承默认尺寸 */
    height?: number;
    /** 禁用全屏/最大化：为 true 时新窗口标题栏隐藏最大化按钮与“切换全屏”菜单 */
    disableFullscreen?: boolean;
}

/**
 * 统一的“工具窗口”打开入口。
 *
 * - 优先调用 Wails 后端 OpenToolWindow，在原生新窗口打开：走 /win 前缀的空白布局路由
 *   （无面包屑、LayoutBody 裸渲染）。
 * - 若调用失败（非 Wails 环境 / 方法不可用 / 后端报错），回退到 web 路由 router.push，
 *   该路由保留原本的 LayoutBody 与面包屑。
 */
export function useToolWindow() {
    const {
        proxy: { $GoTools },
    } = getCurrentInstance() as any;

    /**
     * @param path 常规路由路径，如 "/tools/timestamp"
     * @param title 新窗口标题
     * @param options 单例 / 全屏 / 视口尺寸等配置（见 OpenToolWindowOptions）
     */
    const openToolWindow = async (
        path: string,
        title: string,
        options: OpenToolWindowOptions = {}
    ) => {
        const { single = false, fullscreen = false, width = 0, height = 0, disableFullscreen = false } = options;
        // “禁用全屏”通过 URL query 传递（fs=0），由 BlankLayout 读取，无需改动后端方法签名
        const fullPath = disableFullscreen
            ? path + (path.includes("?") ? "&" : "?") + "fs=0"
            : path;
        // Web 运行模式（Linux / -tags server）没有原生窗口：直接走页面内路由，
        // 不必先调一次注定失败的 OpenToolWindow 再靠异常回退（那样控制台会刷告警）。
        if (!isNativeWindows()) {
            await router.push(fullPath);
            return;
        }
        try {
            if (typeof $GoTools?.OpenToolWindow !== "function") {
                throw new Error("OpenToolWindow 不可用");
            }
            // 新窗口统一用 /win 前缀的空白布局路由（标题栏/菜单由 BlankLayout 自绘）
            // width/height 为 0 时后端继承默认尺寸
            await $GoTools.OpenToolWindow(
                "/win" + fullPath,
                title,
                !!single,
                !!fullscreen,
                width > 0 ? Math.floor(width) : 0,
                height > 0 ? Math.floor(height) : 0
            );
        } catch (e) {
            console.warn("新窗口打开失败，回退到当前窗口路由:", e);
            await router.push(fullPath);
        }
    };

    return { openToolWindow };
}
