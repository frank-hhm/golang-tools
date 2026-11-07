/**
 * dom：DOM 事件与几何工具，1:1 复刻 Arco `_utils/dom.js`。
 * 保留 Arco 的全部导出（NOOP/on/off/contains/getDocumentSize/getElement/
 * getOverlay/getRelativeRect/getScrollBarWidth/isScroll/isServerRendering/querySelector），
 * 仅把类名前缀由 "arco" 换成 "m"（getOverlay）。
 */
import { isString } from "./is";

export const NOOP = (): undefined => {
  return undefined;
};

export const getDocumentSize = (): { height: number; width: number } => {
  const { body } = document;
  const html = document.documentElement;
  let topBody: HTMLElement | undefined;
  try {
    const topWindow = window.top || window.self || window;
    topBody = topWindow.document.body;
  } catch {
    // 跨域 iframe 拿不到 window.top.document，忽略即可（与 Arco 一致）
  }
  return {
    height: Math.max(
      body.scrollHeight,
      body.offsetHeight,
      html.clientHeight,
      html.scrollHeight,
      html.offsetHeight,
      topBody?.scrollHeight || 0,
      topBody?.clientHeight || 0,
    ),
    width: Math.max(
      body.scrollWidth,
      body.offsetWidth,
      html.clientWidth,
      html.scrollWidth,
      html.offsetWidth,
      topBody?.scrollWidth || 0,
      topBody?.clientWidth || 0,
    ),
  };
};

export const isServerRendering = (() => {
  try {
    return !(typeof window !== "undefined" && document !== undefined);
  } catch {
    return true;
  }
})();

type ListenTarget = Window | Document | HTMLElement;

export const on = (
  isServerRendering
    ? NOOP
    : (
        element: ListenTarget,
        event: string,
        handler: EventListenerOrEventListenerObject,
        options: boolean | AddEventListenerOptions = false,
      ) => {
        element.addEventListener(event, handler, options);
      }
) as (
  element: ListenTarget,
  event: string,
  handler: EventListenerOrEventListenerObject,
  options?: boolean | AddEventListenerOptions,
) => void;

export const off = (
  isServerRendering
    ? NOOP
    : (
        element: ListenTarget,
        type: string,
        handler: EventListenerOrEventListenerObject,
        options: boolean | AddEventListenerOptions = false,
      ) => {
        element.removeEventListener(type, handler, options);
      }
) as (
  element: ListenTarget,
  type: string,
  handler: EventListenerOrEventListenerObject,
  options?: boolean | AddEventListenerOptions,
) => void;

export const contains = (root?: Node | null, ele?: Node | null): boolean => {
  if (!root || !ele) {
    return false;
  }
  let node: Node | null = ele as Node;
  while (node) {
    if (node === root) {
      return true;
    }
    node = node.parentNode;
  }
  return false;
};

export const getOverlay = (type: string): HTMLElement => {
  const popper = document.createElement("div");
  popper.setAttribute("class", `m-overlay m-overlay-${type}`);
  return popper;
};

export const querySelector = (
  selectors: string,
  container?: Document | HTMLElement,
): HTMLElement | undefined => {
  if (isServerRendering) {
    return NOOP();
  }
  return (container ?? document).querySelector<HTMLElement>(selectors) ?? undefined;
};

export const getElement = (
  target: string | HTMLElement,
  container?: HTMLElement,
): HTMLElement | undefined => {
  if (isString(target)) {
    const selector = target[0] === "#" ? `[id='${target.slice(1)}']` : target;
    return querySelector(selector, container);
  }
  return target;
};

export const getRelativeRect = (
  target: HTMLElement,
  relative: HTMLElement,
): {
  top: number;
  bottom: number;
  left: number;
  right: number;
  width: number;
  height: number;
} => {
  const targetRect = target.getBoundingClientRect();
  const relativeRect = relative.getBoundingClientRect();
  return {
    top: targetRect.top - relativeRect.top,
    bottom: relativeRect.bottom - targetRect.bottom,
    left: targetRect.left - relativeRect.left,
    right: relativeRect.right - targetRect.right,
    width: targetRect.width,
    height: targetRect.height,
  };
};

export const isScroll = (element: HTMLElement): boolean => {
  return element.tagName === "BODY"
    ? document.documentElement.scrollHeight > window.innerHeight
    : element.scrollHeight > element.offsetHeight;
};

export const getScrollBarWidth = (element: HTMLElement): number => {
  return element.tagName === "BODY"
    ? window.innerWidth - getDocumentSize().width
    : element.offsetWidth - element.clientWidth;
};
