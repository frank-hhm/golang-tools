/** raf/caf：1:1 复刻 Arco `_utils/raf.js`，SSR 安全取 window/global。 */
const target = typeof window === "undefined" ? (globalThis as any) : window;
export const raf = target.requestAnimationFrame as (cb: FrameRequestCallback) => number;
export const caf = target.cancelAnimationFrame as (id: number) => void;
