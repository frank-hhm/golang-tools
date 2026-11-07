/**
 * resize-observer：包裹默认插槽、监听首个子元素尺寸变化并 emit resize，
 * 1:1 复刻 Arco `_components/resize-observer-v2.js`（name 仍为 "ResizeObserver"）。
 * 与 Arco 唯一实现差异同上：用原生 ResizeObserver 替代 resize-observer-polyfill。
 */
import { defineComponent, onBeforeUnmount, watch } from "vue";
import { useFirstElement } from "../_hooks/use-first-element";

export default defineComponent({
  name: "ResizeObserver",
  props: {
    watchOnUpdated: Boolean,
  },
  emits: ["resize"],
  setup(_props, { emit, slots }) {
    const { children, firstElement } = useFirstElement();
    let resizeObserver: ResizeObserver | null = null;
    const createResizeObserver = (target?: HTMLElement) => {
      if (!target) return;
      resizeObserver = new ResizeObserver((entries) => {
        const entry = entries[0];
        emit("resize", entry);
      });
      resizeObserver.observe(target);
    };
    const destroyResizeObserver = () => {
      if (resizeObserver) {
        resizeObserver.disconnect();
        resizeObserver = null;
      }
    };
    watch(firstElement, (element) => {
      if (resizeObserver) destroyResizeObserver();
      if (element) {
        createResizeObserver(element);
      }
    });
    onBeforeUnmount(() => {
      if (resizeObserver) destroyResizeObserver();
    });
    return () => {
      children.value = slots.default?.();
      return children.value as any;
    };
  },
});
