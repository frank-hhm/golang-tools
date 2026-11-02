<template>
  <a-modal :visible="visible" :title="title" width="80%" :esc-to-close="true" :footer="false"
    @update:visible="(v: boolean) => emit('update:visible', v)">
    <div class="df-bar">
      <a-space :size="8">
        <a-tag v-if="stat" color="arcoblue">{{ stat }}</a-tag>
        <a-typography-text v-if="loading" type="secondary">读取差异中…</a-typography-text>
        <a-typography-text v-else-if="!text" type="secondary">无差异内容（未跟踪文件没有可比对的基线）</a-typography-text>
      </a-space>
      <a-button size="small" :disabled="!text" @click="doCopy">
        <template #icon><icon-copy /></template>复制补丁
      </a-button>
    </div>
    <pre v-if="text" class="df-pre">{{ text }}</pre>
  </a-modal>
</template>

<script setup lang="ts">
/**
 * DiffModal：一个文件/一段差异的 unified patch 文本视图（P1 简化口径）。
 * P2 会换成 @codemirror/merge 并排视图，这里先保证「点开文件能看到差异」闭环。
 */
import { computed, getCurrentInstance, ref, watch } from 'vue';
import { Message } from '@arco-design/web-vue';

const props = defineProps<{
  visible: boolean;
  /** 仓库路径 */
  repo: string;
  /** gitcmd.DiffOpts 形状：{staged, commit, path} */
  opts: Record<string, unknown>;
}>();
const emit = defineEmits<{ (e: 'update:visible', v: boolean): void }>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const text = ref('');
const loading = ref(false);

const title = computed(() => (props.opts.path ? `差异 - ${props.opts.path}` : '差异'));
const stat = computed(() => {
  if (props.opts.commit) return `提交 ${String(props.opts.commit).slice(0, 8)}`;
  return props.opts.staged ? '已暂存改动' : '工作区改动';
});

watch(
  () => props.visible,
  async (v) => {
    if (!v) return;
    text.value = '';
    if (!props.repo || !props.opts) return;
    loading.value = true;
    try {
      text.value = (await GoTools.GitDiffText(props.repo, props.opts)) || '';
    } catch (e: any) {
      Message.error({ content: String(e?.message || e || '读取差异失败'), position: 'top', duration: 3000 });
    } finally {
      loading.value = false;
    }
  }
);

async function doCopy() {
  try {
    await navigator.clipboard.writeText(text.value);
    Message.success({ content: '已复制', position: 'top', duration: 1500 });
  } catch {
    const ta = document.createElement('textarea');
    ta.value = text.value;
    document.body.appendChild(ta);
    ta.select();
    document.execCommand('copy');
    document.body.removeChild(ta);
    Message.success({ content: '已复制', position: 'top', duration: 1500 });
  }
}
</script>

<style scoped>
.df-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}
.df-pre {
  max-height: 60vh;
  overflow: auto;
  margin: 0;
  padding: 10px 12px;
  border-radius: var(--base-radius-default, 6px);
  background: var(--color-fill-1);
  color: var(--color-text-1);
  font-family: Consolas, "Courier New", monospace;
  font-size: 12px;
  line-height: 1.5;
  white-space: pre;
}
</style>
