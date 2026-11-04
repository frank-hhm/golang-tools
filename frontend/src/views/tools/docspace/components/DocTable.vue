<template>
  <a-table
    :data="rows"
    :loading="loading"
    :pagination="pagination"
    :row-selection="{ type: 'checkbox', showCheckedAll: true }"
    v-model:selected-keys="selModel"
    row-key="id"
    size="small"
    :column-resizable="true"
    :scroll="{ x: 1080, y: 'max-content' }"
    @row-dblclick="(r: any) => $emit('action', 'preview', r as DocRow)"
  >
    <template #columns>
      <a-table-column title="名称" data-index="name" :width="240" :ellipsis="true" :tooltip="true" fixed="left">
        <template #cell="{ record }">
          <div class="ds-t-name">
            <common-file-icon :is-dir="false" :ext="record.ext" :size="18" />
            <a-tooltip :content="record.path">
              <span :class="{ gone: record.missing }">{{ record.name }}</span>
            </a-tooltip>
            <a-tag v-if="record.missing" size="small" color="orangered">缺失</a-tag>
          </div>
        </template>
      </a-table-column>

      <a-table-column title="类型" data-index="kindLabel" :width="92">
        <template #cell="{ record }">
          <span class="ds-t-kind">
            <i class="ds-dot" :style="{ background: kindColor(record.kind) }" />{{ record.kindLabel }}
          </span>
        </template>
      </a-table-column>

      <a-table-column title="大小" data-index="size" :width="100" :sortable="{ sortDirections: ['descend', 'ascend'], sorter: sizeSorter }" align="right">
        <template #cell="{ record }">
          <span class="ds-t-num">{{ record.hasMeta && !record.missing ? fmtSize(record.size) : "--" }}</span>
        </template>
      </a-table-column>

      <a-table-column title="更新时间" data-index="modTime" :width="150" :sortable="{ sortDirections: ['descend', 'ascend'], sorter: timeSorter }">
        <template #cell="{ record }">
          <!-- 排序按实体时间，展示也按实体时间；取不到实体才退回登记时间 -->
          <a-tooltip :content="fmtTime(record.modTime || record.updatedAt)">
            <span>{{ fmtAgo(record.modTime || record.updatedAt) }}</span>
          </a-tooltip>
        </template>
      </a-table-column>

      <a-table-column title="标签" :width="180">
        <template #cell="{ record }">
          <a-space wrap :size="4">
            <a-tag v-for="t in (record.tags ?? []).slice(0, 3)" :key="t" size="small" color="arcoblue">{{ t }}</a-tag>
            <a-tooltip v-if="(record.tags ?? []).length > 3" :content="(record.tags ?? []).slice(3).join('、')">
              <a-tag size="small">+{{ (record.tags ?? []).length - 3 }}</a-tag>
            </a-tooltip>
            <span v-if="!(record.tags && record.tags.length)" class="ds-t-muted">--</span>
          </a-space>
        </template>
      </a-table-column>

      <a-table-column title="源" data-index="src" :width="80">
        <template #cell="{ record }">{{ srcLabel(record.src) }}</template>
      </a-table-column>

      <a-table-column title="版本" data-index="version" :width="70" :sortable="{ sortDirections: ['descend', 'ascend'], sorter: versionSorter }" align="center">
        <template #cell="{ record }">
          <span v-if="record.version > 0" class="ds-t-num">v{{ record.version }}</span>
          <span v-else class="ds-t-muted">--</span>
        </template>
      </a-table-column>

      <a-table-column title="路径" data-index="path" :min-width="240" :ellipsis="true" :tooltip="true" />

      <a-table-column title="操作" :width="140" fixed="right">
        <template #cell="{ record }">
          <div class="ds-t-ops">
            <a-link @click="$emit('action', 'preview', record as DocRow)">预览</a-link>
            <a-dropdown trigger="hover" :popup-max-height="false" @select="(k: any) => $emit('action', String(k), record as DocRow)">
              <a-link>更多</a-link>
              <template #content>
                <a-doption v-for="op in rowOps(record as DocRow)" :key="op.k" :value="op.k">
                  <span :class="op.tone ? `ds-op-${op.tone}` : ''">{{ op.t }}</span>
                </a-doption>
              </template>
            </a-dropdown>
          </div>
        </template>
      </a-table-column>
    </template>
  </a-table>
</template>

<script setup lang="ts">
/**
 * 文档中心表格视图：需要看路径、批量核对、排序时比网格好用。
 * 只负责呈现与发意图，所有动作回父组件统一处理；操作清单也取自 rowOps，
 * 与网格同一份，不会两处各维一份“能做什么”而漂移。
 */
import { computed } from "vue";
import { fmtAgo, fmtSize, fmtTime, type DocRow } from "../types";
import { kindColor, rowOps, srcLabel } from "../useDocMeta";

const props = withDefaults(defineProps<{
  rows: DocRow[];
  selected?: string[];
  loading?: boolean;
  pageSize?: number;
}>(), {
  selected: () => [],
  loading: false,
  pageSize: 20,
});

const emit = defineEmits<{
  (e: "action", key: string, row: DocRow): void;
  (e: "update:selected", ids: string[]): void;
}>();

/** 分页由表格自己管：Arco 的排序/分页是在整份 data 上做的，父层再切一刀就成双重分页 */
const pagination = computed(() => ({
  pageSize: props.pageSize,
  showTotal: true,
  showJumper: true,
  sizeCanChange: true,
  pageSizeChangeResetCurrent: true,
}));

/** 选中集以父组件为唯一事实源，这里只做过一遍类型转换的 v-model 代理 */
const selModel = computed<string[]>({
  get: () => props.selected,
  set: (ids) => emit("update:selected", ids),
});

/** 点列排序。
 *  Arco 传了自定义 sorter 后**不会**再按方向反序（table.js 里直接把 sorter 的返回值当结果），
 *  所以方向得自己乘：ascend=正常、其余一律降序。取不到真实值的行不管哪个方向
 *  都排到末尾——它们不是“最小”，是“不知道”，拿 0 冒充只会让排序骗人。 */
type SortCtx = { dataIndex?: string; direction?: string };
const sign = (ctx?: SortCtx) => (ctx?.direction === "ascend" ? 1 : -1);

function byNumber(pick: (r: any) => { v: number; ok: boolean }) {
  return (a: any, b: any, ctx?: SortCtx): number => {
    const av = pick(a);
    const bv = pick(b);
    if (av.ok !== bv.ok) return av.ok ? 1 : -1;
    if (!av.ok) return 0;
    return sign(ctx) * (av.v - bv.v);
  };
}

const sizeSorter = byNumber((r) => ({ v: r.size, ok: !!r.hasMeta && !r.missing }));
const timeSorter = byNumber((r) => ({ v: r.modTime || r.updatedAt, ok: !!r.hasMeta }));
const versionSorter = byNumber((r) => ({ v: r.version || 0, ok: true }));
</script>

<style scoped lang="less">
/* 表头：中性底 + 正文色加粗。暗色下 Arco 默认表头与表体同色，列名靠不住 */
:deep(.arco-table-th) {
  background-color: var(--color-fill-2);
  color: var(--color-text-1);
  font-weight: 600;
}

.ds-t-name {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.ds-t-name span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 实体缺失：名称划线 + 红标，与网格同一口径 */
.ds-t-name span.gone {
  text-decoration: line-through;
  color: var(--color-text-3);
}

.ds-t-kind {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}

.ds-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  flex: none;
}

.ds-t-num {
  font-variant-numeric: tabular-nums;
}

.ds-t-muted {
  color: var(--color-text-4);
}

.ds-t-ops {
  display: flex;
  align-items: center;
  gap: 10px;
  white-space: nowrap;
}
</style>
