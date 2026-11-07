/**
 * use-z-index：m-* 原生组件库的浮层层级管理器，1:1 复刻 Arco `_hooks/use-popup-manager.js` 的机制。
 *
 * 为什么需要它（历史返工根因）：Arco 所有浮层（modal/drawer/tooltip/select/dropdown/popover/message/notification）
 * 都走这一个全局管理器，「后打开的浮层自动压过先打开的」，且 dialog 与 popup 共用同一条编号栈。
 * 之前 m-* 各自硬编码 8000/9000/9100，丢了这个语义，才出现「弹窗里残留的 tooltip 盖在确认框上」这类串层 bug。
 *
 * 与 Arco 的唯一有意差异：基准值。Arco 用 POPUP_BASE=1000 / MESSAGE_BASE=5000，
 * 但本项目的应用外壳（标题栏 1000、布局遮罩 2000）本身就压在 1000~2000 之间，
 * 若照搬 1000 会让弹窗掉到布局遮罩之下（新的回归）。故这里把基准抬到 8000/9000，
 * 机制（递增栈 / dialog 并入 popup 栈 / isLastDialog）与 Arco 完全一致。
 */
import { onBeforeUnmount, onMounted, readonly, ref, watch } from "vue";
import type { Ref } from "vue";

export type PopupType = "popup" | "dialog" | "message";

/** 浮层基准层级：普通浮层（tooltip/select/dropdown/popover） */
const POPUP_BASE_Z_INDEX = 8000;
/** 浮层基准层级：对话框类（modal/drawer），与 popup 共用编号栈但单独记账用于 isLastDialog */
const DIALOG_BASE_Z_INDEX = 8000;
/** 浮层基准层级：全局消息/通知，恒高于一切 */
const MESSAGE_BASE_Z_INDEX = 9000;
/** 每开一个浮层递增的步长 */
const Z_INDEX_STEP = 1;

class PopupManager {
  private popupStack: Record<PopupType, Set<number>> = {
    popup: new Set<number>(),
    dialog: new Set<number>(),
    message: new Set<number>(),
  };

  /** 取下一个可用层级：同栈栈顶 + step，空栈则用各自基准 */
  getNextZIndex = (type: PopupType): number => {
    let current: number;
    if (type === "message") {
      current = Array.from(this.popupStack.message).pop() || MESSAGE_BASE_Z_INDEX;
    } else if (type === "dialog") {
      current = Array.from(this.popupStack.popup).pop() || DIALOG_BASE_Z_INDEX;
    } else {
      current = Array.from(this.popupStack.popup).pop() || POPUP_BASE_Z_INDEX;
    }
    return current + Z_INDEX_STEP;
  };

  add = (type: PopupType): number => {
    const zIndex = this.getNextZIndex(type);
    this.popupStack[type].add(zIndex);
    // dialog 同时占用 popup 栈，保证「弹窗」与「弹窗内浮层」在同一编号序列里
    if (type === "dialog") {
      this.popupStack.popup.add(zIndex);
    }
    return zIndex;
  };

  delete = (zIndex: number, type: PopupType): void => {
    this.popupStack[type].delete(zIndex);
    if (type === "dialog") {
      this.popupStack.popup.delete(zIndex);
    }
  };

  /** 是否为最顶层 dialog（Esc 关闭、遮罩点击等只应作用于最上层弹窗） */
  isLastDialog = (zIndex: number): boolean => {
    if (this.popupStack.dialog.size > 1) {
      return zIndex === Array.from(this.popupStack.dialog).pop();
    }
    return true;
  };
}

const popupManager = new PopupManager();

export interface UseZIndexOptions {
  /** 受控可见性；true→占用层级，false→释放 */
  visible?: Ref<boolean>;
  /** 无 visible 时是否在 mounted 即占用（常驻浮层用） */
  runOnMounted?: boolean;
}

/**
 * 组合式：为某个浮层实例申请/释放层级。
 * @returns zIndex 当前生效层级（0 表示尚未占用，交调用方回退到 CSS 默认）
 */
export function useZIndex(type: PopupType, options: UseZIndexOptions = {}) {
  const { visible, runOnMounted } = options;
  const zIndex = ref(0);

  const open = () => {
    zIndex.value = popupManager.add(type);
  };
  const close = () => {
    popupManager.delete(zIndex.value, type);
    zIndex.value = 0;
  };
  const isLastDialog = () => (type === "dialog" ? popupManager.isLastDialog(zIndex.value) : false);

  if (visible) {
    watch(
      () => visible.value,
      (v) => (v ? open() : close()),
      { immediate: true },
    );
  }
  if (runOnMounted) {
    onMounted(open);
    onBeforeUnmount(close);
  }

  return {
    zIndex: readonly(zIndex),
    open,
    close,
    isLastDialog,
  };
}

export default useZIndex;
