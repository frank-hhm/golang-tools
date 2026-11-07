<template>
  <div class="bp">
    <div class="bp-bar">
      <div class="bp-left">
        <a-tag color="purple" size="small">Blame</a-tag>
        <span class="bp-path" :title="path">{{ path }}</span>
        <a-typography-text v-if="revision" type="secondary" class="bp-rev">@ {{ revision.slice(0, 8) }}</a-typography-text>
      </div>
      <a-space :size="6">
        <a-checkbox v-model="showAll" size="mini">每行显示归属</a-checkbox>
        <a-button size="mini" :loading="loading" @click="load">刷新</a-button>
      </a-space>
    </div>

    <div v-if="loading" class="bp-state"><a-spin /><span>读取逐行归属…</span></div>
    <div v-else-if="err" class="bp-state bp-err">{{ err }}</div>
    <div v-else-if="binary" class="bp-state">二进制文件，无法逐行归属</div>
    <div v-else class="bp-body">
      <div v-for="ln in lines" :key="ln.n" class="bp-line" :class="{ 'bp-seg': ln.segStart }">
        <span class="bp-no">{{ ln.n }}</span>
        <span v-if="showAll || ln.segStart" class="bp-anno" :style="{ borderLeftColor: ln.color }">
          <template v-if="ln.seg">{{ ln.seg.author }} · {{ rel(ln.seg.time) }} · </template>
          <span class="bp-short">{{ ln.seg?.short }}</span>
          <template v-if="ln.segStart"> {{ ln.seg?.summary }}</template>
        </span>
        <span v-else class="bp-anno bp-anno-ghost"></span>
        <span class="bp-code">{{ ln.text }}</span>
      </div>
      <div v-if="!lines.length" class="bp-state">该文件为空或无归属信息</div>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * BlamePane：git blame --line-porcelain 聚合出的「每段作者/时间/commit」逐行展示（计划 P2）。
 *
 * 引擎把连续同一提交的行聚成段（startLine + count），这里读文件正文（FileReadText）按行号对齐，
 * 段首显示完整归属、段内其它行只在「每行显示归属」打开时补哈希。点击段哈希可上抛给父级看该提交。
 */
import { computed, getCurrentInstance, ref, watch } from 'vue';
import { Message } from '@arco-design/web-vue';
import type { BlameSegment } from 'bindings/Tools/tools/gitcmd/index';

const props = defineProps<{ repo: string; path: string; revision?: string }>();
const emit = defineEmits<{ (e: 'commit', hash: string): void }>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const loading = ref(false);
const err = ref('');
const binary = ref(false);
const showAll = ref(false);
const segs = ref<BlameSegment[]>([]);
const rawLines = ref<string[]>([]);

const SEG_VARS = ['--arcoblue-6', '--green-6', '--orangered-6', '--purple-6', '--cyan-6', '--gold-6', '--pinkpurple-6', '--lime-6'];
function colorOf(hash: string): string {
  let h = 0;
  for (let i = 0; i < hash.length; i++) h = (h * 31 + hash.charCodeAt(i)) | 0;
  return `rgb(var(${SEG_VARS[Math.abs(h) % SEG_VARS.length]}))`;
}

type Line = { n: number; text: string; seg: BlameSegment | null; segStart: boolean; color: string };
const lines = computed<Line[]>(() => {
  // 行号(1-based) -> 覆盖它的段 / 是否段首
  const owner = new Map<number, BlameSegment>();
  const starts = new Set<number>();
  for (const s of segs.value) {
    starts.add(s.startLine);
    for (let k = 0; k < s.count; k++) owner.set(s.startLine + k, s);
  }
  return rawLines.value.map((text, i) => {
    const n = i + 1;
    const seg = owner.get(n) || null;
    return { n, text, seg, segStart: starts.has(n), color: seg ? colorOf(seg.hash) : 'var(--color-border-3)' };
  });
});

function rel(ts: number): string {
  if (!ts) return '';
  const d = Math.floor(Date.now() / 1000) - ts;
  if (d < 60) return '刚刚';
  if (d < 3600) return `${Math.floor(d / 60)} 分钟前`;
  if (d < 86400) return `${Math.floor(d / 3600)} 小时前`;
  if (d < 86400 * 30) return `${Math.floor(d / 86400)} 天前`;
  const dt = new Date(ts * 1000);
  const p = (n: number) => String(n).padStart(2, '0');
  return `${dt.getFullYear()}-${p(dt.getMonth() + 1)}-${p(dt.getDate())}`;
}

function joinRepo(repo: string, p: string): string {
  return repo.replace(/[\\/]+$/, '') + '/' + p;
}

async function load() {
  err.value = '';
  binary.value = false;
  segs.value = [];
  rawLines.value = [];
  if (!props.repo || !props.path) return;
  loading.value = true;
  try {
    const [r, bl] = await Promise.all([
      GoTools.FileReadText(joinRepo(props.repo, props.path)),
      GoTools.GitBlame(props.repo, props.path, props.revision || ''),
    ]);
    if (r?.binary) {
      binary.value = true;
      return;
    }
    rawLines.value = String(r?.content ?? '').split('\n');
    segs.value = bl || [];
  } catch (e: any) {
    err.value = String(e?.message || e || '读取 blame 失败');
    Message.error({ content: err.value, position: 'top', duration: 3000 });
  } finally {
    loading.value = false;
  }
}

watch(() => [props.repo, props.path, props.revision], load, { immediate: true });
</script>

<style scoped>
.bp {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}
.bp-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 6px 2px;
  border-bottom: 1px solid var(--color-border-2);
  flex: none;
}
.bp-left {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.bp-path {
  font-size: 12px;
  color: var(--color-text-1);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.bp-rev {
  font-size: 11px;
  font-family: var(--code-font-family);
}
.bp-state {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: var(--color-text-3);
  font-size: 12px;
}
.bp-err {
  color: rgb(var(--danger-6));
}
.bp-body {
  flex: 1;
  min-height: 0;
  overflow: auto;
}
.bp-line {
  display: flex;
  align-items: stretch;
  font-family: var(--code-font-family);
  font-size: 12px;
  line-height: 1.55;
}
.bp-line.bp-seg {
  border-top: 1px solid var(--color-border-1);
}
.bp-no {
  flex: none;
  width: 40px;
  padding-right: 6px;
  text-align: right;
  color: var(--color-text-3);
  user-select: none;
}
.bp-anno {
  flex: none;
  width: 300px;
  padding: 0 8px;
  border-left: 3px solid transparent;
  color: var(--color-text-2);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.bp-anno-ghost {
  border-left-color: transparent;
}
.bp-short {
  color: rgb(var(--arcoblue-6));
}
.bp-code {
  flex: 1;
  min-width: 0;
  padding-right: 8px;
  color: var(--color-text-1);
  white-space: pre;
}
</style>
