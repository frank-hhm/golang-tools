/**
 * 系统级桌面通知：Windows 桌面版直接弹系统原生 Toast（屏幕右下角卡片，
 * 自动进「通知中心」/历史，受专注助手控制）。
 *
 * 只给「后台完成 / 异常」这类用户可能没盯着窗口的事件用：安装完成、截图保存、
 * 备份生成、实例崩溃。页面内的即时反馈（保存成功、校验失败）继续用 Arco Message，
 * 不要为了统一把每条提示都弹到系统通知里。
 */
import { App } from "bindings/go-tools/services/app/index";
import { isNativeWindows } from "./caps";

/**
 * 弹系统原生通知。
 *
 * @returns 是否真的走了原生通道；面板/浏览器构建没有通知中心时返回 false，
 *          调用方可以据此退化成页内 Message。
 */
export function sysNotify(title: string, content: string): boolean {
  if (!isNativeWindows()) return false;
  const fn = (App as any)?.ShowSystemNotification;
  if (typeof fn !== "function") return false;
  try {
    // Go 侧异步执行（内部起 PowerShell 发 WinRT Toast），不阻塞调用方
    void fn(title, content);
    return true;
  } catch {
    return false;
  }
}
