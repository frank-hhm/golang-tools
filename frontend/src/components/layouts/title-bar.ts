import { shallowRef, type ShallowRef } from "vue";

/**
 * 全局标题栏的「页面注入挂载点」。
 *
 * 布局层（主窗口 layouts/index.vue、工具窗 BlankLayout）里的标题栏与页面内容（router-view）
 * 是兄弟节点，页面没法用普通 slot 往上塞东西，所以这里登记两个 DOM 挂载点：
 *   - titleMount：标题文字右侧（靠左，可占满剩余宽度，适合地址栏/标签/面包屑）
 *   - extraMount：全局按钮区左侧（靠右，适合本工具自己的按钮与标记）
 * 页面侧一律经 `common-title-bar` 的 #title / #extra 往这两处 Teleport。
 *
 * 每个窗口是独立 webview（独立 JS 上下文），一个文档里只会有一个 WinTitleBar，
 * 因此用模块级单例登记即可，不需要 provide/inject 穿透布局层。
 */
const titleMount = shallowRef<HTMLElement | null>(null);
const extraMount = shallowRef<HTMLElement | null>(null);

/** 页面侧取挂载点（响应式：标题栏尚未挂载时先原位渲染，挂载后自动搬进去） */
export function useTitleBarMounts(): {
    titleMount: ShallowRef<HTMLElement | null>;
    extraMount: ShallowRef<HTMLElement | null>;
} {
    return { titleMount, extraMount };
}

/**
 * WinTitleBar 挂载时登记挂载点，返回注销函数。
 * 注销时比对自己当初登记的元素：窗口内容热替换可能出现「新的已登记、旧的才注销」，
 * 无条件清空会让页面注入无处可去。
 */
export function registerTitleBarMounts(titleEl: HTMLElement, extraEl: HTMLElement): () => void {
    titleMount.value = titleEl;
    extraMount.value = extraEl;
    return () => {
        if (titleMount.value === titleEl) titleMount.value = null;
        if (extraMount.value === extraEl) extraMount.value = null;
    };
}
