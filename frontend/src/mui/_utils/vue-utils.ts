/**
 * vue-utils：VNode 树遍历与合并工具，1:1 复刻 Arco `_utils/vue-utils.js`。
 * Trigger 用它把事件/类名合并进默认插槽的第一个真实节点（mergeFirstChild），
 * 并向上找到第一个可挂载事件的 DOM 元素（getFirstElementFrom*）。
 * 遍历逻辑与 Arco 完全一致，只在个别位置补 `any` 以通过 vue-tsc 严格类型。
 */
import { Fragment, cloneVNode, isVNode } from "vue";
import type { VNode, VNodeArrayChildren, VNodeProps } from "vue";
import { isArray, isFunction, isString } from "./is";

export const isElement = (vn: any): boolean => Boolean(vn && vn.shapeFlag & 1);

export const isComponent = (vn: any, _type?: unknown): boolean =>
  Boolean(vn && vn.shapeFlag & 6);

export const isTextChildren = (child: any, _children: unknown): boolean =>
  Boolean(child && child.shapeFlag & 8);

export const isArrayChildren = (vn: any, _children?: unknown): boolean =>
  Boolean(vn && vn.shapeFlag & 16);

export const isSlotsChildren = (vn: any, _children?: unknown): boolean =>
  Boolean(vn && vn.shapeFlag & 32);

export const getFirstComponent = (children?: VNodeArrayChildren): VNode | undefined => {
  if (!children) {
    return undefined;
  }
  for (const child of children as VNode[]) {
    if (isElement(child) || isComponent(child)) {
      return child;
    }
    if (isArrayChildren(child, (child as VNode).children)) {
      const result = getFirstComponent((child as VNode).children as VNodeArrayChildren);
      if (result) return result;
    } else if (isSlotsChildren(child, (child as VNode).children)) {
      const c = ((child as VNode).children as any)?.default?.();
      if (c) {
        const result = getFirstComponent(c);
        if (result) return result;
      }
    } else if (isArray(child)) {
      const result = getFirstComponent(child as VNodeArrayChildren);
      if (result) return result;
    }
  }
  return undefined;
};

export const isEmptyChildren = (children?: any[]): boolean => {
  if (!children) {
    return true;
  }
  for (const item of children) {
    if (item.children) {
      return false;
    }
  }
  return true;
};

export const mergeFirstChild = (
  children: VNodeArrayChildren | undefined,
  extraProps: Record<string, any> | ((child: VNode) => Record<string, any>),
): boolean => {
  if (children && children.length > 0) {
    for (let i = 0; i < children.length; i++) {
      const child = children[i] as VNode;
      if (isElement(child) || isComponent(child)) {
        const props = isFunction(extraProps) ? extraProps(child) : extraProps;
        (children as VNode[])[i] = cloneVNode(child, props as VNodeProps, true);
        return true;
      }
      const _children = getChildrenArray(child);
      if (_children && _children.length > 0) {
        const result = mergeFirstChild(_children, extraProps);
        if (result) return true;
      }
    }
  }
  return false;
};

export const getChildrenArray = (vn: any): VNodeArrayChildren | undefined => {
  if (isArrayChildren(vn, vn.children)) {
    return vn.children;
  }
  if (isArray(vn)) {
    return vn as unknown as VNodeArrayChildren;
  }
  return undefined;
};

export const getFirstElementFromVNode = (vn: any): HTMLElement | undefined => {
  if (isElement(vn)) {
    return vn.el;
  }
  if (isComponent(vn)) {
    if (vn.el?.nodeType === 1) {
      return vn.el;
    }
    if (vn.component?.subTree) {
      const ele = getFirstElementFromVNode(vn.component.subTree);
      if (ele) return ele;
    }
  } else {
    const children = getChildrenArray(vn);
    return getFirstElementFromChildren(children);
  }
  return undefined;
};

export const getFirstElementFromChildren = (
  children?: VNodeArrayChildren,
): HTMLElement | undefined => {
  if (children && children.length > 0) {
    for (const child of children) {
      const element = getFirstElementFromVNode(child);
      if (element) return element;
    }
  }
  return undefined;
};

export const getAllElements = (children?: any[], includeText = false): any[] => {
  const results: any[] = [];
  for (const item of children ?? []) {
    if (isElement(item) || isComponent(item) || (includeText && isTextChildren(item, item.children))) {
      results.push(item);
    } else if (isArrayChildren(item, item.children)) {
      results.push(...getAllElements(item.children, includeText));
    } else if (isSlotsChildren(item, item.children)) {
      results.push(...getAllElements((item.children as any)?.default?.(), includeText));
    } else if (isArray(item)) {
      results.push(...getAllElements(item, includeText));
    }
  }
  return results;
};

export function unFragment(nodeList: any[]): any[] {
  function loop(nodes: any[]): any[] {
    const unFragmentNodeList: any[] = [];
    nodes.forEach((node) => {
      if (isVNode(node) && node.type === Fragment) {
        if (isSlotsChildren(node, node.children)) {
          unFragmentNodeList.push(...loop(((node.children as any)?.default?.() || []) as any[]));
        } else if (isArrayChildren(node, node.children)) {
          unFragmentNodeList.push(...loop(node.children as any[]));
        } else if (isString(node.children)) {
          unFragmentNodeList.push(node.children);
        }
      } else {
        unFragmentNodeList.push(node);
      }
    });
    return unFragmentNodeList;
  }
  return loop(nodeList);
}

export const getSlotFunction = (param: any): (() => any) | undefined => {
  if (param) {
    if (isFunction(param)) return param;
    return () => param;
  }
  return undefined;
};

export const getComponentsFromVNode = (vn: any, name: string): number[] => {
  const components: number[] = [];
  if (isComponent(vn, vn.type)) {
    if (vn.type.name === name) {
      if (vn.component) {
        components.push(vn.component.uid);
      }
    } else if (vn.component?.subTree) {
      components.push(...getComponentsFromVNode(vn.component.subTree, name));
    }
  } else {
    const children = getChildrenArray(vn);
    if (children) {
      components.push(...getComponentsFromChildren(children as any[], name));
    }
  }
  return components;
};

export const getComponentsFromChildren = (
  children: any[] | undefined,
  name: string,
): number[] => {
  const components: number[] = [];
  if (children && children.length > 0) {
    for (const child of children) {
      components.push(...getComponentsFromVNode(child, name));
    }
  }
  return components;
};
