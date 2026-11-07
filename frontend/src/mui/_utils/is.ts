/**
 * is：类型判定工具，1:1 复刻 Arco `_utils/is.js`。
 * 全部基于 Object.prototype.toString，行为与 Arco 完全一致（含 isNumber 排除 NaN、isExist 允许 0）。
 */
const opt = Object.prototype.toString;

export function isArray(obj: unknown): obj is unknown[] {
  return opt.call(obj) === "[object Array]";
}
export function isNull(obj: unknown): boolean {
  return opt.call(obj) === "[object Null]";
}
export function isBoolean(obj: unknown): obj is boolean {
  return opt.call(obj) === "[object Boolean]";
}
export function isObject(obj: unknown): obj is Record<string, unknown> {
  return opt.call(obj) === "[object Object]";
}
export const isPromise = <T = any>(obj: unknown): obj is Promise<T> => {
  return opt.call(obj) === "[object Promise]";
};
export function isString(obj: unknown): obj is string {
  return opt.call(obj) === "[object String]";
}
export function isNumber(obj: unknown): obj is number {
  return opt.call(obj) === "[object Number]" && obj === obj;
}
export function isUndefined(obj: unknown): obj is undefined {
  return obj === undefined;
}
export function isFunction(obj: unknown): obj is (...args: any[]) => any {
  return typeof obj === "function";
}
export function isEmptyObject(obj: unknown): boolean {
  return isObject(obj) && Object.keys(obj).length === 0;
}
export function isExist(obj: unknown): boolean {
  return (obj as any) || obj === 0;
}
export function isWindow(el: any): boolean {
  return el === window;
}
export const isComponentInstance = (value: any): boolean => {
  return value?.$ !== undefined;
};
