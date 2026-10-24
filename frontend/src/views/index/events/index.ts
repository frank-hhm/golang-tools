import { useScreenshotEvent } from "./screenshot";

/**
 * 工具卡片可直接触发的事件类型（无需路由跳转）。
 * 新增事件时：在此登记 id，并在 useToolEvents 中补充对应封装与 handler。
 */
export type ToolEventId = "screenshot";

/** 单个工具项：路由跳转(path) 与 直接触发事件(event) 二选一 */
export interface ToolItem {
  name: string;
  /** 跳转到指定路由 */
  path?: string;
  /** 触发已封装的工具事件 */
  event?: ToolEventId;
  /** 窗口是否单例：true 时重复打开会复用（聚焦）已存在的窗口；默认 false 允许多开 */
  single?: boolean;
  /** 新窗口是否铺满显示器（最大化启动）；默认 false（未配置则沿用常规尺寸） */
  fullscreen?: boolean;
  /** 禁用全屏/最大化：为 true 时新窗口标题栏隐藏最大化按钮与“切换全屏”菜单 */
  disableFullscreen?: boolean;
  /** 新窗口视口宽度；未设定（<=0）则继承默认尺寸 */
  width?: number;
  /** 新窗口视口高度；未设定（<=0）则继承默认尺寸 */
  height?: number;
}

/**
 * 组合所有工具事件处理器，并暴露统一的派发入口。
 * 返回的引用（ref）由调用方通过 reactive() 包裹后即可在模板中深层访问。
 */
export function useToolEvents() {
  const screenshot = useScreenshotEvent();

  /** 事件 id -> 处理函数 的注册表 */
  const handlers: Record<ToolEventId, () => void | Promise<void>> = {
    screenshot: () => screenshot.capture(),
  };

  /** 按事件 id 派发；未知 id 给出告警 */
  const runEvent = (id: ToolEventId) => {
    const handler = handlers[id];
    if (!handler) {
      console.warn(`未注册的工具事件: ${id}`);
      return;
    }
    return handler();
  };

  return {
    screenshot,
    runEvent,
  };
}
