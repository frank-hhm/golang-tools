/**
 * throttle-by-raf：1:1 复刻 Arco `_utils/throttle-by-raf.js`。
 * 用 requestAnimationFrame 合帧节流，附带 cancel()。
 */
import { caf, raf } from "./raf";

export function throttleByRaf<T extends (...args: any[]) => void>(cb: T) {
  let timer = 0;
  const throttle = (...args: Parameters<T>) => {
    if (timer) {
      caf(timer);
    }
    timer = raf(() => {
      cb(...args);
      timer = 0;
    });
  };
  throttle.cancel = () => {
    caf(timer);
    timer = 0;
  };
  return throttle;
}
