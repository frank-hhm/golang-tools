/**
 * use-first-element：1:1 复刻 Arco `_hooks/use-first-element.js`。
 * children 是普通对象（非 ref），render 里直接赋值 .value；首元素在 mounted/updated 时求取。
 */
import { onMounted, onUpdated, ref } from "vue";
import type { Ref, VNodeArrayChildren } from "vue";
import { getFirstElementFromChildren } from "../_utils/vue-utils";

export interface FirstElementChildren {
  value?: VNodeArrayChildren;
}

export const useFirstElement = () => {
  const children: FirstElementChildren = {};
  const firstElement = ref<HTMLElement>();
  const getFirstElement = () => {
    const element = getFirstElementFromChildren(children.value);
    if (element !== firstElement.value) {
      firstElement.value = element;
    }
  };
  onMounted(() => getFirstElement());
  onUpdated(() => getFirstElement());
  return {
    children,
    firstElement,
  };
};

export type UseFirstElementResult = {
  children: FirstElementChildren;
  firstElement: Ref<HTMLElement | undefined>;
};
