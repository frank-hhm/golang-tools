<template>
  <transition name="fade-slide">
    <div v-if="count > 0" class="ds-batch">
      <div class="ds-batch-left">
        <a-checkbox
          :model-value="allSelected"
          :indeterminate="count < total && count > 0"
          @change="toggleAll"
        >
          已选 <strong>{{ count }}</strong> / {{ total }} 项
        </a-checkbox>
        <span v-if="missing > 0" class="ds-batch-warn">{{ missing }} 项实体缺失，涉及文件本体的操作会逐条失败</span>
      </div>
      <a-space :size="6" wrap>
        <a-button size="mini" @click="$emit('tag')">标签</a-button>
        <a-button size="mini" @click="$emit('category')">改分类</a-button>
        <a-button size="mini" @click="$emit('snapshot')">存快照</a-button>
        <a-button size="mini" @click="$emit('export-list')">复制清单</a-button>
        <a-button size="mini" status="warning" @click="$emit('trash')">回收站</a-button>
        <a-button size="mini" status="danger" @click="$emit('remove')">移出</a-button>
        <a-button size="mini" @click="$emit('clear')">取消选择</a-button>
      </a-space>
    </div>
  </transition>
</template>

<script setup lang="ts">
/**
 * 批量操作条。
 * 只在有选中时出现（不常驻）：常驻会把列表高度挤掉一截，而多数浏览场景根本不选。
 * 「标签/改分类」开弹窗由父组件负责，这里只发意图，不掺业务。
 */
withDefaults(defineProps<{
  /** 已选条数 */
  count: number;
  /** 当前可见条数（全选判断按可见范围，不越过去动筛选外的条目） */
  total: number;
  /** 选中项里实体缺失的数量，用来提前说明"批量移回收站会跳掉几条" */
  missing?: number;
  allSelected?: boolean;
}>(), {
  missing: 0,
  allSelected: false,
});

const emit = defineEmits<{
  (e: "toggle-all", on: boolean): void;
  (e: "tag"): void;
  (e: "category"): void;
  (e: "snapshot"): void;
  (e: "export-list"): void;
  (e: "trash"): void;
  (e: "remove"): void;
  (e: "clear"): void;
}>();

function toggleAll(checked: boolean | (string | number | boolean)[]) {
  emit("toggle-all", checked === true);
}
</script>

<style scoped lang="less">
.ds-batch {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  padding: 6px 10px;
  margin-bottom: 8px;
  border-radius: 6px;
  /* 以前用 arcoblue-1/3：浅色下刚好，暗色下 arcoblue-1 是深藏蓝、整条变成一块压暗的脏色。
     改成以主色为基准的低透明度叠加，两套主题下都是“淡蓝提示条” */
  border: 1px solid rgba(var(--arcoblue-6), 0.32);
  background: rgba(var(--arcoblue-6), 0.08);
}

.ds-batch-left {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  font-size: 12px;
}

.ds-batch-warn {
  color: rgb(var(--orange-6));
  font-size: 12px;
}

.fade-slide-enter-active,
.fade-slide-leave-active {
  transition: opacity 0.15s, transform 0.15s;
}

.fade-slide-enter-from,
.fade-slide-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}
</style>
