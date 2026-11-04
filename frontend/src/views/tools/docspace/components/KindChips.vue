<template>
  <div v-if="kinds.length" class="ds-chips">
    <button
      class="ds-chip"
      :class="{ on: !modelValue }"
      type="button"
      @click="$emit('update:modelValue', '')"
    >
      全部<em>{{ total }}</em>
    </button>
    <button
      v-for="k in kinds"
      :key="k.kind"
      class="ds-chip"
      :class="{ on: modelValue === k.kind }"
      type="button"
      :title="`${k.label} · ${fmtSize(k.size)}`"
      @click="$emit('update:modelValue', modelValue === k.kind ? '' : k.kind)"
    >
      <i class="ds-dot" :style="{ background: kindColor(k.kind) }" />
      {{ k.label }}<em>{{ k.num }}</em>
    </button>
  </div>
</template>

<script setup lang="ts">
/**
 * 类型筛选 chips。
 * 计数来自全量 Overview 而不是当前筛选结果：筛选后计数会自己变小，
 * 再点另一个类型就"越点越少"，看不出库里到底有多少。所以这里显示全量口径，
 * 点击只做「再筛一层」的事。
 */
import { fmtSize, type KindCount } from "../types";
import { kindColor } from "../useDocMeta";

withDefaults(defineProps<{
  kinds: KindCount[];
  /** 全量条目数，用于「全部」chip */
  total?: number;
  /** 当前选中的 kind，空串表示不筛 */
  modelValue?: string;
}>(), {
  total: 0,
  modelValue: "",
});

defineEmits<{ (e: "update:modelValue", kind: string): void }>();
</script>

<style scoped lang="less">
.ds-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 10px;
}

.ds-chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 2px 10px;
  font-size: 12px;
  line-height: 20px;
  color: var(--color-text-2);
  background: var(--color-fill-2);
  border: 1px solid transparent;
  border-radius: 12px;
  cursor: pointer;
  transition: all 0.15s;
}

.ds-chip:hover {
  background: var(--color-fill-3);
}

/* 选中态：描边走主色 + 文字加深；不整块填高饱和色，一排 chips 才不会抢视线 */
.ds-chip.on {
  color: var(--color-text-1);
  font-weight: 600;
  background: var(--color-bg-2);
  border-color: rgb(var(--arcoblue-6));
}

.ds-chip em {
  font-style: normal;
  font-variant-numeric: tabular-nums;
  color: var(--color-text-3);
  font-weight: 400;
}

.ds-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex: none;
}
</style>
