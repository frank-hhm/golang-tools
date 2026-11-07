/**
 * use-resize-observer：复刻 Arco `_hooks/use-resize-observer.js`。
 * 唯一实现差异：Arco 用 resize-observer-polyfill（为兼容旧浏览器），这里用原生 ResizeObserver。
 * 现代 Chromium（本项目 WebView2 / 浏览器面板目标）原生即支持，行为一致，不引入额外依赖。
 */
import type { Ref } from "vue";
import { isFunction } from "../_utils/is";

export interface UseResizeObserverOptions {
  elementRef: Ref<HTMLElement | undefined>;
  onResize?: (entry: ResizeObserverEntry) => void;
}

export const useResizeObserver = ({ elementRef, onResize }: UseResizeObserverOptions) => {
  let resizeObserver: ResizeObserver | null = null;
  const createResizeObserver = () => {
    if (!elementRef.value) return;
    resizeObserver = new ResizeObserver((entries) => {
      const entry = entries[0];
      isFunction(onResize) && onResize!(entry);
    });
    resizeObserver.observe(elementRef.value);
  };
  const destroyResizeObserver = () => {
    if (resizeObserver) {
      resizeObserver.disconnect();
      resizeObserver = null;
    }
  };
  return {
    createResizeObserver,
    destroyResizeObserver,
  };
};
