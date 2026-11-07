/**
 * use-teleport-container：1:1 复刻 Arco `_hooks/use-teleport-container.js`。
 * 解析 popupContainer → Teleport 的 to 目标（teleportContainer）与定位参照容器（containerRef）。
 * documentContainer=true 时参照根用 document.documentElement（trigger 即此用法）。
 */
import { onMounted, ref, watch } from "vue";
import type { Ref } from "vue";
import { getElement } from "../_utils/dom";

export interface UseTeleportContainerOptions {
  popupContainer: Ref<string | HTMLElement | undefined>;
  visible: Ref<boolean>;
  defaultContainer?: string;
  documentContainer?: boolean;
}

export const useTeleportContainer = ({
  popupContainer,
  visible,
  defaultContainer = "body",
  documentContainer,
}: UseTeleportContainerOptions) => {
  const teleportContainer = ref<string | HTMLElement | undefined>(popupContainer.value);
  const containerRef = ref<HTMLElement>();
  const getContainer = () => {
    const element = getElement(popupContainer.value as any);
    const _teleportContainer = element ? popupContainer.value : defaultContainer;
    const _containerElement =
      element ?? (documentContainer ? document.documentElement : getElement(defaultContainer as any));
    if (_teleportContainer !== teleportContainer.value) {
      teleportContainer.value = _teleportContainer;
    }
    if (_containerElement !== containerRef.value) {
      containerRef.value = _containerElement;
    }
  };
  onMounted(() => getContainer());
  watch(visible, (v) => {
    if (teleportContainer.value !== popupContainer.value && v) {
      getContainer();
    }
  });
  return {
    teleportContainer,
    containerRef,
  };
};
