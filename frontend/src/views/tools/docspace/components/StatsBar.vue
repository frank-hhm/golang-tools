<template>
  <div class="ds-stats">
    <common-stat-card
      label="纳管条目"
      :value="ready ? String(st.total) : '--'"
      :detail="catDetail"
      :tip="catDetail"
      :ready="ready"
      tone="blue"
    >
      <template #icon><icon-folder /></template>
    </common-stat-card>

    <common-stat-card
      label="占用体积"
      :value="ready ? fmtSize(st.totalSize) : '--'"
      :detail="sizeDetail"
      :tip="sizeDetail"
      :ready="ready"
      tone="green"
    >
      <template #icon><icon-storage /></template>
    </common-stat-card>

    <common-stat-card
      label="文件类型"
      :value="ready ? `${kinds.length} 种` : '--'"
      :detail="extDetail"
      :tip="extTip"
      :ready="ready"
      tone="purple"
    >
      <template #icon><icon-apps /></template>
    </common-stat-card>

    <common-stat-card
      label="近 7 天动过"
      :value="ready ? String(st.updated7d) : '--'"
      :detail="tagDetail"
      :tip="updatedTip"
      :ready="ready"
      tone="cyan"
    >
      <template #icon><icon-history /></template>
    </common-stat-card>

    <!-- 缺失单独一张：它是"要处理"的信号，混进明细小字里很容易被忽略 -->
    <common-stat-card
      label="实体缺失"
      :value="ready ? String(st.missing) : '--'"
      :unit="st.missing > 0 ? '条' : ''"
      :detail="missingDetail"
      :tip="missingDetail"
      :ready="ready"
      :tone="st.missing > 0 ? 'orange' : 'gray'"
    >
      <template #icon><icon-exclamation-circle /></template>
    </common-stat-card>
  </div>
</template>

<script setup lang="ts">
/**
 * 文档中心总览统计条。
 * 数字全部来自后端 Overview（与类型 chips 同一份 Metas 汇总），
 * 前端不再自己数一遍——两处口径不一致比少一个卡片更糟。
 */
import { computed } from "vue";
import { fmtSize, type Counts, type KindCount, type Overview } from "../types";

const props = withDefaults(defineProps<{
  /** 后端总览；未取到时为 null，所有卡片走「未就绪」样式 */
  overview: Overview | null;
  /** 分类/回收站/密文计数（这些不在 Overview 里，来自 DocSpaceCounts） */
  counts: Counts;
  kinds?: KindCount[];
}>(), {
  kinds: () => [],
});

const ready = computed(() => props.overview !== null);
const st = computed(() => props.overview?.stats ?? {
  total: 0,
  totalSize: 0,
  byKind: {},
  kindSize: {},
  missing: 0,
  updated7d: 0,
  withTags: 0,
  withVersions: 0,
});

const catDetail = computed(() => `分类 ${props.counts.categories} · 回收站 ${props.counts.trash} · 密文 ${props.counts.secure}`);

const sizeDetail = computed(() => {
  const reachable = st.value.total - st.value.missing;
  if (!reachable) return "暂无可读取的实体";
  return `${reachable} 个实体可读 · 均 ${fmtSize(Math.round(st.value.totalSize / reachable))}`;
});

/** 热门格式：最多列 3 个，剩下的收进 tooltip，卡片不被撑破 */
const extDetail = computed(() => {
  const top = (props.overview?.topExts ?? []).slice(0, 3);
  if (!top.length) return "暂无扩展名信息";
  return top.map((e) => `${e.ext} ×${e.num}`).join(" · ");
});
const extTip = computed(() => {
  const all = props.overview?.topExts ?? [];
  return all.length ? `热门格式：${all.map((e) => `${e.ext} ×${e.num}`).join("、")}` : "";
});

const tagDetail = computed(() => `带标签 ${st.value.withTags} · 有版本 ${st.value.withVersions}`);

/** 口径得说清：后端的 updated7d 是“实体 mtime 或登记时间 7 日内有变化”，
 *  只写“近 7 天动过”容易被读成“只看文件本体”；明细行窄，完整说法放 tooltip。 */
const updatedTip = computed(
  () => `实体修改时间或登记时间在 7 天内的共 ${st.value.updated7d} 条；带标签 ${st.value.withTags} 条 · 有版本 ${st.value.withVersions} 条`,
);

const missingDetail = computed(() =>
  st.value.missing > 0 ? "原文件已删除或远端会话已关，点开看排查线索" : "全部实体都在",
);
</script>

<style scoped lang="less">
.ds-stats {
  display: grid;
  /* min(190px, 100%)：窗口比一张卡还窄时退成撑满可用宽，不把右侧顶出滚动条 */
  grid-template-columns: repeat(auto-fit, minmax(min(190px, 100%), 1fr));
  gap: 10px;
  margin-bottom: 12px;
}

/* grid 子项默认 min-width:auto，而卡片里的明细行是 nowrap：
   窄列时内容会把列顶宽，整条统计栏比容器宽，最右一张就被窗口切掉。这里强制可收缩。 */
.ds-stats > * {
  min-width: 0;
}
</style>
