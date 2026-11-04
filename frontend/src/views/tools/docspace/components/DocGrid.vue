<template>
  <a-spin :loading="loading" class="ds-grid-spin">
    <div v-if="rows.length" class="ds-grid">
      <div
        v-for="row in rows"
        :key="row.id"
        class="ds-card"
        :class="{ sel: isSelected(row.id), gone: row.missing }"
        @click="toggle(row.id)"
        @dblclick="$emit('action', 'preview', row)"
      >
        <div class="ds-thumb">
          <a-checkbox
            class="ds-check"
            :model-value="isSelected(row.id)"
            @click.stop
            @change="toggle(row.id)"
          />
          <!-- 图片优先出真缩略图；取不到（非图片 / 后端没给 URL / 加载失败）就退回类型大图标 -->
          <img
            v-if="row.thumb && !failed.has(row.id)"
            :src="row.thumb"
            :alt="row.name"
            loading="lazy"
            class="ds-img"
            :class="{ raw: isSmall.get(row.id) === true }"
            @error="markFailed(row.id)"
            @load="onImgLoad($event, row.id)"
          />
          <common-file-icon v-else :is-dir="false" :ext="row.ext" :size="54" />

          <span class="ds-kind" :style="{ background: kindColor(row.kind) }">{{ row.kindLabel }}</span>
          <span v-if="row.missing" class="ds-gone">实体缺失</span>
          <span v-if="row.version > 0" class="ds-ver">v{{ row.version }}</span>
        </div>

        <div class="ds-info">
          <a-tooltip :content="row.path">
            <div class="ds-name">{{ row.name }}</div>
          </a-tooltip>
          <div class="ds-meta">
            <span class="ds-size">{{ row.hasMeta && !row.missing ? fmtSize(row.size) : "--" }}</span>
            <span class="ds-dot-sep">·</span>
            <a-tooltip :content="fmtTime(row.modTime || row.updatedAt)">
              <span>{{ fmtAgo(row.modTime || row.updatedAt) }}</span>
            </a-tooltip>
            <span class="ds-dot-sep">·</span>
            <span>{{ srcLabel(row.src) }}</span>
          </div>
          <div v-if="row.tags && row.tags.length" class="ds-tags">
            <a-tag v-for="t in row.tags.slice(0, 3)" :key="t" size="small" color="arcoblue">{{ t }}</a-tag>
            <a-tooltip v-if="row.tags.length > 3" :content="row.tags.slice(3).join('、')">
              <a-tag size="small">+{{ row.tags.length - 3 }}</a-tag>
            </a-tooltip>
          </div>
        </div>

        <div class="ds-ops" @click.stop>
          <a-button size="mini" type="primary" @click="$emit('action', 'preview', row)">预览</a-button>
          <a-dropdown trigger="hover" :popup-max-height="false" @select="(k: any) => $emit('action', String(k), row)">
            <a-button size="mini">更多</a-button>
            <template #content>
              <a-doption v-for="op in rowOps(row)" :key="op.k" :value="op.k">
                <span :class="op.tone ? `ds-op-${op.tone}` : ''">{{ op.t }}</span>
              </a-doption>
            </template>
          </a-dropdown>
        </div>
      </div>
    </div>
    <a-empty v-else-if="!loading" class="ds-empty">
      <template #description>
        <div class="ds-empty-t">没有符合条件的条目</div>
        <div class="ds-empty-s">换个关键词、清空类型筛选，或从上方「纳管文件」把已有文件登记进库</div>
      </template>
    </a-empty>
  </a-spin>
</template>

<script setup lang="ts">
/**
 * 文档中心网格视图：以缩略图/类型图标为主的卡片墙。
 *
 * 单击=选中（与磁盘页同一套交互），双击=预览，操作收在「更多」下拉里，
 * 卡片底部只留「预览」这一个主操作，免得十来个链接把卡片挤满。
 */
import { reactive, watch } from "vue";
import { fmtAgo, fmtSize, fmtTime, type DocRow } from "../types";
import { kindColor, rowOps, srcLabel } from "../useDocMeta";

const props = withDefaults(defineProps<{
  rows: DocRow[];
  selected?: string[];
  loading?: boolean;
}>(), {
  selected: () => [],
  loading: false,
});

const emit = defineEmits<{
  (e: "action", key: string, row: DocRow): void;
  (e: "update:selected", ids: string[]): void;
}>();

const selectedSet = reactive(new Set<string>());

/**
 * 选中集以 props 为唯一事实源，本地只镜像一份 Set：卡片多到几百张时，
 * 每张卡每次重渲都跑一遍 Array.includes 会白白耗掉一轮渲染。
 * （所以不在本地“先改了再等 props 回合”，避免两边不一致。）
 */
watch(
  () => props.selected,
  (ids) => {
    selectedSet.clear();
    for (const id of ids ?? []) selectedSet.add(id);
  },
  { immediate: true, deep: true },
);

const failed = reactive(new Set<string>());
function markFailed(id: string) {
  failed.add(id);
}

/** 小图名单：原图不够盖满缩略图框（盖不满就得放大），48×48 的 logo 拉到
 *  190×116 必然发虚，不如按原尺寸居中显示——宁可不填满，也不把图搞模糊。 */
const isSmall = reactive(new Map<string, boolean>());
const BOX_W = 190;
const BOX_H = 116;
function onImgLoad(ev: Event, id: string) {
  const img = ev.target as HTMLImageElement;
  if (img.naturalWidth && img.naturalHeight) {
    isSmall.set(id, img.naturalWidth < BOX_W || img.naturalHeight < BOX_H);
  }
}

function isSelected(id: string): boolean {
  return selectedSet.has(id);
}

function toggle(id: string) {
  const next = [...selectedSet];
  const i = next.indexOf(id);
  if (i >= 0) next.splice(i, 1);
  else next.push(id);
  emit("update:selected", next);
}
</script>

<style scoped lang="less">
.ds-grid-spin {
  display: block;
  width: 100%;
}

.ds-grid {
  display: grid;
  /* 卡片宽度自适应：窄窗 3 列也不挤，宽窗自动铺满。
     min(188px, 100%) ：窗口比一张卡还窄时退成“撑满可用宽”，
     否则 188px 这个下限会把右栏顶出一条横向滚动条 */
  grid-template-columns: repeat(auto-fill, minmax(min(188px, 100%), 1fr));
  gap: 12px;
}

.ds-card {
  display: flex;
  flex-direction: column;
  border: 1px solid var(--color-border-2);
  border-radius: 8px;
  background: var(--color-bg-2);
  overflow: hidden;
  cursor: pointer;
  transition: box-shadow 0.16s, border-color 0.16s, transform 0.16s;
}

.ds-card:hover {
  box-shadow: var(--shadow-2-bottom);
  transform: translateY(-1px);
}

.ds-card.sel {
  border-color: rgb(var(--arcoblue-6));
  box-shadow: 0 0 0 1px rgb(var(--arcoblue-6)) inset, var(--shadow-2-bottom);
}

/* 实体缺失：整卡压暗 + 名称划线，一眼看出这条已经"空挂"了 */
.ds-card.gone {
  opacity: 0.66;
}

.ds-card.gone .ds-name {
  text-decoration: line-through;
}

/* 缩略图区：没有图时只剩一个居中的类型图标，整块像一块死黑。
   加一层斜纹底给空白处一点纹理，图标再带个淡影把它“托”起来。 */
.ds-thumb {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  height: 116px;
  background-color: var(--color-fill-1);
  background-image: repeating-linear-gradient(135deg, rgba(128, 128, 128, 0.07) 0 6px, transparent 6px 12px);
  overflow: hidden;
}

/* common-file-icon 的根是内联 SVG（.w11-icon），不是 div */
.ds-thumb > :deep(.w11-icon) {
  filter: drop-shadow(0 2px 4px rgba(0, 0, 0, 0.18));
}

.ds-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

/* 原图小于框：不放大不裁剪，按真实尺寸居中（背景由 .ds-thumb 提供） */
.ds-img.raw {
  object-fit: none;
  object-position: center;
}

.ds-check {
  position: absolute;
  top: 6px;
  left: 6px;
  z-index: 2;
  padding: 0 4px;
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.72);
  opacity: 0;
  transition: opacity 0.15s;
}

.ds-card:hover .ds-check,
.ds-card.sel .ds-check {
  opacity: 1;
}

/* Arco 把勾画在 8×14 的容器里，内层 svg 按 1em 取号；卡片继承了 12px 字号，
   svg 就顶出 14px 的方框（实测 scrollWidth 19 > clientWidth 14），这里按容器锁死 */
.ds-check :deep(.arco-checkbox-icon-check) {
  display: flex;
  align-items: center;
  justify-content: center;
}

.ds-check :deep(.arco-checkbox-icon-check > svg) {
  width: 8px;
  height: 8px;
}

.ds-kind {
  position: absolute;
  left: 6px;
  bottom: 6px;
  padding: 0 7px;
  font-size: 11px;
  font-weight: 500;
  letter-spacing: 0.2px;
  line-height: 18px;
  color: #fff;
  border-radius: 9px;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.28);
}

.ds-gone,
.ds-ver {
  position: absolute;
  right: 6px;
  bottom: 6px;
  padding: 0 6px;
  font-size: 11px;
  line-height: 17px;
  border-radius: 9px;
  background: var(--color-fill-3);
  color: var(--color-text-2);
}

.ds-gone {
  background: rgb(var(--orange-6));
  color: #fff;
}

.ds-ver {
  font-variant-numeric: tabular-nums;
}

.ds-info {
  flex: 1;
  min-width: 0;
  padding: 8px 8px 4px;
}

.ds-name {
  font-size: 13px;
  font-weight: 500;
  line-height: 1.4;
  color: var(--color-text-1);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.ds-meta {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-top: 2px;
  font-size: 12px;
  color: var(--color-text-3);
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}

.ds-size {
  color: var(--color-text-2);
}

.ds-dot-sep {
  color: var(--color-text-4);
}

.ds-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 6px;
}

.ds-ops {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
  padding: 6px 8px;
  background: var(--color-fill-1);
  border-top: 1px solid var(--color-border-1);
  cursor: default;
}

/* 空态：除了“没数据”，还得告诉用户下一步做什么 */
.ds-empty {
  padding: 32px 0;
}

.ds-empty-t {
  font-size: 13px;
  color: var(--color-text-2);
}

.ds-empty-s {
  margin-top: 4px;
  font-size: 12px;
  color: var(--color-text-4);
}
</style>
