<template>
  <div class="rb">
    <div v-if="label" class="rb-label">
      <span>{{ label }}</span>
      <a-typography-text v-if="hint" type="secondary" class="rb-hint">{{ hint }}</a-typography-text>
    </div>
    <div class="rb-body">
      <textarea ref="taRef" class="rb-text" :class="{ 'is-mono': mono }" :value="text" readonly spellcheck="false" />
      <div class="rb-actions">
        <a-button size="mini" :disabled="!text" @click="doCopy">
          <template #icon><icon-copy /></template>复制
        </a-button>
        <a-button size="mini" :disabled="!text || !dir || !filename" :loading="saving" @click="doSave">
          <template #icon><icon-save /></template>另存
        </a-button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { getCurrentInstance, ref } from 'vue';
import { Message } from '@arco-design/web-vue';
import { IconCopy, IconSave } from '@arco-design/web-vue/es/icon';

/**
 * SSH 生成助手内的只读结果区：固定「复制 + 另存」两个动作。
 *  - 复制走 navigator.clipboard，失败降级为选中 textarea + execCommand（与 launcher 同口径）。
 *  - 另存把文本写进 props.dir（父组件统一取 SSHDefaultDir，可 ChooseDir 改），文件名用 props.filename。
 * 本组件不发后端请求以外的逻辑，dir/filename 为空时对应动作禁用。
 */
const props = defineProps<{
  label?: string;
  text: string;
  filename?: string;
  dir: string;
  mono?: boolean;
  hint?: string;
}>();

const emit = defineEmits<{ (e: 'saved', path: string): void }>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const taRef = ref<HTMLTextAreaElement | null>(null);
const saving = ref(false);

async function doCopy() {
  const t = props.text;
  if (!t) return;
  try {
    await navigator.clipboard.writeText(t);
    Message.success({ content: '已复制到剪贴板', position: 'top', duration: 1500 });
  } catch {
    // 降级：选中只读 textarea 再执行原生复制
    const el = taRef.value;
    if (el) {
      el.focus();
      el.select();
      try {
        document.execCommand('copy');
        Message.success({ content: '已复制到剪贴板', position: 'top', duration: 1500 });
        return;
      } catch {
        /* 落到下方失败提示 */
      }
    }
    Message.error({ content: '复制失败，请手动选中文本', position: 'top', duration: 2000 });
  }
}

/** 目录 + 文件名拼接：Windows 目录用反斜杠，其余用正斜杠 */
function joinPath(dir: string, name: string): string {
  const win = dir.includes('\\') && !dir.includes('/');
  const sep = win ? '\\' : '/';
  const trimmed = dir.endsWith(sep) ? dir : dir + sep;
  return trimmed + name;
}

async function doSave() {
  if (!props.text || !props.dir || !props.filename) return;
  saving.value = true;
  try {
    const path = joinPath(props.dir, props.filename);
    await GoTools.FileWriteText(path, props.text);
    Message.success({ content: '已保存到 ' + path, position: 'top', duration: 2500 });
    emit('saved', path);
  } catch (e: any) {
    Message.error({ content: '保存失败：' + String(e?.message || e), position: 'top', duration: 3000 });
  } finally {
    saving.value = false;
  }
}
</script>

<style scoped>
.rb {
  margin-bottom: 12px;
}

.rb-label {
  display: flex;
  align-items: baseline;
  gap: 8px;
  font-size: var(--base-size-small);
  color: var(--color-text-2);
  margin-bottom: 4px;
}

.rb-hint {
  font-size: var(--base-size-small);
}

.rb-body {
  position: relative;
}

.rb-text {
  width: 100%;
  min-height: 56px;
  padding: 8px 10px;
  padding-bottom: 34px;
  box-sizing: border-box;
  resize: vertical;
  border: 1px solid var(--color-border-2);
  border-radius: var(--base-radius-default);
  background: var(--color-bg-1);
  color: var(--color-text-1);
  font-size: var(--base-size-default);
  line-height: 1.5;
  outline: none;
}

.rb-text.is-mono {
  font-family: var(--code-font-family);
}

.rb-actions {
  position: absolute;
  right: 8px;
  bottom: 7px;
  display: flex;
  gap: 6px;
}
</style>
