/**
 * use-pick-slots：1:1 复刻 Arco `_hooks/use-pick-slots.js`。
 * 抓住具名插槽的引用，并在 updated 时同步（插槽函数引用变化时才更新）。
 */
import { onUpdated, ref } from "vue";
import type { Ref, Slot, Slots } from "vue";

export default function usePickSlots(slots: Slots, slotName: string): Ref<Slot | undefined> {
  const slot = ref(slots[slotName]) as Ref<Slot | undefined>;
  onUpdated(() => {
    const newSlot = slots[slotName];
    if (slot.value !== newSlot) {
      slot.value = newSlot;
    }
  });
  return slot;
}
