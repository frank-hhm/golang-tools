/**
 * trigger/context：父子 Trigger 之间串联上下文的注入键，
 * 1:1 对应 Arco `trigger/context.js`（Arco 里只是一个 Symbol，这里补上 TS 类型）。
 *
 * 嵌套场景（如 tooltip 里再套 dropdown）时，子 Trigger 通过 inject 拿到父 Trigger 的
 * onMouseenter/onMouseleave/addChildRef/removeChildRef，从而把「鼠标移出整棵树才算离开」
 * 与「子孙弹层集合」向上冒泡，避免出现「鼠标移到子弹层就被判定为离开而关闭父弹层」。
 */
import type { InjectionKey, Ref } from "vue";

export interface TriggerContext {
  onMouseenter: (e: MouseEvent) => void;
  onMouseleave: (e: MouseEvent) => void;
  addChildRef: (ref: Ref<HTMLElement | undefined>) => void;
  removeChildRef: (ref: Ref<HTMLElement | undefined>) => void;
}

export const triggerInjectionKey: InjectionKey<TriggerContext> = Symbol("mTrigger");
