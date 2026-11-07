import { isArray, isObject, isUndefined } from "./is";

/**
 * 按路径读写嵌套对象值，1:1 复刻 Arco `_utils/get-value-by-path.js`。
 * 支持 a.b / a[0].b / a["x"] 形式：方括号数字/字符串键先归一化为点号再逐段下钻。
 */
export const getValueByPath = (obj: unknown, path?: string): unknown => {
  if (!obj || !path) {
    return undefined;
  }
  path = path.replace(/\[(\w+)\]/g, ".$1");
  const keys = path.split(".");
  if (keys.length === 0) {
    return undefined;
  }
  let temp: any = obj;
  for (let i = 0; i < keys.length; i++) {
    if ((!isObject(temp) && !isArray(temp)) || !keys[i]) {
      return undefined;
    }
    if (i !== keys.length - 1) {
      temp = temp[keys[i]];
    } else {
      return temp[keys[i]];
    }
  }
  return undefined;
};

export const setValueByPath = (
  obj: unknown,
  path: string | undefined,
  value: unknown,
  { addPath }: { addPath?: boolean } = {},
): void => {
  if (!obj || !path) {
    return;
  }
  path = path.replace(/\[(\w+)\]/g, ".$1");
  const keys = path.split(".");
  if (keys.length === 0) {
    return;
  }
  let temp: any = obj;
  for (let i = 0; i < keys.length; i++) {
    if ((!isObject(temp) && !isArray(temp)) || !keys[i]) {
      return;
    }
    if (i !== keys.length - 1) {
      if (addPath && isUndefined(temp[keys[i]])) {
        temp[keys[i]] = {};
      }
      temp = temp[keys[i]];
    } else {
      temp[keys[i]] = value;
    }
  }
};
