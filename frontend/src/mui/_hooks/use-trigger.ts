/**
 * use-trigger：受控/非受控浮层显隐的共享逻辑，1:1 复刻 Arco `_hooks/use-trigger.js`。
 * 与 Arco 完全一致：内部 _popupVisible 兜底 defaultPopupVisible，computedPopupVisible 优先取
 * 受控 popupVisible；handlePopupVisibleChange 仅在值变化时 emit update:popupVisible + popupVisibleChange；
 * watch(computedPopupVisible) 让受控值回写内部态。dropdown / select 等浮层组件共用此 hook。
 */
import { computed, ref, watch } from "vue";
import type { Ref } from "vue";

export interface UseTriggerOptions {
  popupVisible?: Ref<boolean | undefined>;
  defaultPopupVisible?: Ref<boolean>;
  emit: (event: "update:popupVisible" | "popupVisibleChange", visible: boolean) => void;
}

export const useTrigger = ({
  popupVisible,
  defaultPopupVisible,
  emit,
}: UseTriggerOptions) => {
  const _popupVisible = ref<boolean>(defaultPopupVisible?.value ?? false);
  const computedPopupVisible = computed<boolean>(
    () => popupVisible?.value ?? _popupVisible.value,
  );

  const handlePopupVisibleChange = (visible: boolean) => {
    if (visible !== computedPopupVisible.value) {
      _popupVisible.value = visible;
      emit("update:popupVisible", visible);
      emit("popupVisibleChange", visible);
    }
  };

  watch(computedPopupVisible, (visible) => {
    if (_popupVisible.value !== visible) {
      _popupVisible.value = visible;
    }
  });

  return {
    computedPopupVisible,
    handlePopupVisibleChange,
  };
};
