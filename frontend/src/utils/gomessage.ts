/**
 * Go 端消息提示桥接层（demo）
 *
 * Go 侧调用 App.ShowMessage(level, text) → 后端 Emit "ui:message" 事件 →
 * 本文件监听到后用 Arco Design 的 Message 组件弹出，效果等同前端直接调用：
 *   import { Message } from "@arco-design/web-vue"; Message.success("...")
 *
 * 注意：只做事件桥接，不改动前端现有的 Message 调用。
 */
import { Message } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";

/** Go 端推送的消息载荷（对应 app.go 的 UIMessage） */
export interface GoUIMessage {
  level: string; // info | success | warning | error
  text: string;
  duration: number; // 毫秒，0 = 前端默认
}

let installed = false;

/** 注册 ui:message 事件监听（幂等，main.ts 中调用一次即可） */
export function setupGoMessage() {
  if (installed) return;
  installed = true;
  Events.On("ui:message", (ev: any) => {
    const d: GoUIMessage | null = (ev?.data ?? ev) as GoUIMessage | null;
    if (!d || !d.text) return;
    const level = String(d.level ?? "info").toLowerCase();
    const show = (Message as any)[level] || Message.info;
    if (typeof d.duration === "number" && d.duration > 0) {
      show(String(d.text), { duration: d.duration });
    } else {
      show(String(d.text));
    }
  });
}
