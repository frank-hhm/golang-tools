<template>
  <div class="rb">
    <!-- 头栏：左标题右动作，替代原先浮在文本区里的一角按钮；正文走 --code-* 表面色与全站代码面板同肤色 -->
    <div class="rb-head">
      <div v-if="label" class="rb-label">
        <span>{{ label }}</span>
        <a-typography-text v-if="hint" type="secondary" class="rb-hint">{{ hint }}</a-typography-text>
      </div>
      <span v-else />
      <div class="rb-actions">
        <a-button size="mini" type="text" :disabled="!text" @click="doCopy">
          <template #icon><icon-copy /></template>复制
        </a-button>
        <a-button size="mini" type="text" :disabled="!text || !dir || !filename" :loading="saving" @click="doSave">
          <template #icon><icon-save /></template>另存
        </a-button>
      </div>
    </div>
    <textarea ref="taRef" class="rb-text" :class="{ 'is-mono': mono }" :value="text" readonly spellcheck="false" />
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
  border: 1px solid var(--code-border);
  border-radius: var(--base-radius-small);
  overflow: hidden;
  margin-bottom: 12px;
  background: var(--code-bg);
}

.rb-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 3px 4px 3px 10px;
  border-bottom: 1px solid var(--code-border);
  background: var(--color-fill-1);
}

.rb-label {
  display: flex;
  align-items: baseline;
  gap: 8px;
  min-width: 0;
  font-size: var(--base-size-small);
  color: var(--color-text-2);
}

.rb-hint {
  font-size: var(--base-size-small);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.rb-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  flex: none;
}

.rb-text {
  display: block;
  width: 100%;
  min-height: 44px;
  padding: 8px 10px;
  box-sizing: border-box;
  border: 0;
  resize: none;
  background: transparent;
  color: var(--code-text);
  font-size: var(--base-size-default);
  line-height: 1.5;
  outline: none;
  cursor: text;
}

.rb-text:focus {
  box-shadow: none;
}

.rb-text.is-mono {
  font-family: var(--code-font-family);
}
</style>
