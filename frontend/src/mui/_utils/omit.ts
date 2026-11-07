/** omit：1:1 复刻 Arco `_utils/omit.js`，按 key 数组浅拷贝剔除。 */
export const omit = <T extends Record<string, any>, K extends keyof T | string>(
  object: T,
  path: readonly K[],
): Partial<T> => {
  const result: any = { ...object };
  for (const item of path) {
    if (item in result) {
      delete result[item as string];
    }
  }
  return result;
};
