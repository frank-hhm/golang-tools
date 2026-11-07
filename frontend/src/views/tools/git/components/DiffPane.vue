<template>
  <div class="dp">
    <!-- 顶栏：模式切换（并排/内联/补丁）+ 保存（仅工作区可编辑侧） -->
    <div class="dp-bar">
      <div class="dp-left">
        <a-tag color="arcoblue" size="small">{{ scopeText }}</a-tag>
        <span class="dp-path" :title="path">{{ path || '（整仓差异）' }}</span>
        <span v-if="dirty" class="dp-dot" title="有未保存的修改"></span>
      </div>
      <a-space :size="6">
        <a-radio-group v-model="mode" size="mini" type="button">
          <a-radio value="side">并排</a-radio>
          <a-radio value="inline">内联</a-radio>
          <a-radio value="patch">补丁</a-radio>
        </a-radio-group>
        <a-button v-if="canEdit" size="mini" type="primary" :loading="saving" :disabled="!dirty" @click="save">保存</a-button>
      </a-space>
    </div>

    <div v-if="loading" class="dp-state"><a-spin /><span>读取差异中…</span></div>
    <div v-else-if="err" class="dp-state dp-err">{{ err }}</div>
    <div v-else-if="binary" class="dp-state">
      <a-result status="warning" title="二进制文件" subtitle="内容不适合逐行比对，请在外部程序查看差异" />
    </div>
    <!-- 编辑器挂载点：并排/内联由 @codemirror/merge 动态建；补丁用兜底 <pre> -->
    <div v-else-if="mode === 'patch'" class="dp-patch-wrap">
      <pre class="dp-pre">{{ patch }}</pre>
    </div>
    <div v-else ref="hostEl" class="dp-host"></div>
  </div>
</template>

<script setup lang="ts">
/**
 * DiffPane：文件级差异的并排 / 内联 / 补丁三态视图（计划 P2）。
 *
 * 数据来自引擎的 GitShowBlob（取左右两栏全文）：
 *   - 工作区改动：左 = HEAD 版本，右 = 磁盘当前（可编辑，保存走 FileWriteText）；
 *   - 已暂存：左 = HEAD，右 = 暂存区（index，只读）；
 *   - 某笔提交：左 = 父（<hash>^），右 = 该笔（只读）；首提交的父不存在 → 左空 = 纯增。
 *
 * @codemirror/merge 走 dynamic import（计划要求，避免进主包）；CodeMirror 基础件与查看器
 * CodePane 同源（basicSetup + oneDark + 按扩展名动态语言包）。取不到全文或含 NUL 判二进制，
 * 兜底退化成 unified patch 文本视图（GitDiffText）。
 */
import { computed, getCurrentInstance, nextTick, onBeforeUnmount, ref, watch } from 'vue';
import { Message } from '@arco-design/web-vue';
import { EditorState } from '@codemirror/state';
import { EditorView } from '@codemirror/view';
import { basicSetup } from 'codemirror';
import { oneDark } from '@codemirror/theme-one-dark';

type DiffOpts = { staged?: boolean; commit?: string; path?: string };
const props = defineProps<{ repo: string; opts: DiffOpts }>();
const emit = defineEmits<{ (e: 'saved'): void }>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const hostEl = ref<HTMLElement>();
const loading = ref(false);
const saving = ref(false);
const err = ref('');
const binary = ref(false);
const dirty = ref(false);
const mode = ref<'side' | 'inline' | 'patch'>('side');
const patch = ref('');

// 左右全文 + 是否可编辑 + 落盘绝对路径（仅工作区侧）
let leftText = '';
let rightText = '';
let savePath = '';

const path = computed(() => props.opts?.path || '');
const canEdit = computed(() => !!savePath && !binary.value && !err.value);
const scopeText = computed(() => {
  if (props.opts?.commit) return `提交 ${String(props.opts.commit).slice(0, 8)}`;
  return props.opts?.staged ? '已暂存' : '工作区';
});

function extOf(p: string): string {
  return (p.split('.').pop() || '').toLowerCase();
}
function joinRepo(repo: string, p: string): string {
  return repo.replace(/[\\/]+$/, '') + '/' + p;
}

// 与 CodePane 同一口径：按扩展名动态加载语言包，只读浏览不需要命令历史
async function loadLanguage(e: string): Promise<any> {
  switch (e) {
    case 'js':
    case 'jsx':
      return (await import('@codemirror/lang-javascript')).javascript();
    case 'ts':
      return (await import('@codemirror/lang-javascript')).javascript({ typescript: true });
    case 'tsx':
      return (await import('@codemirror/lang-javascript')).javascript({ typescript: true, jsx: true });
    case 'vue':
      return (await import('@codemirror/lang-vue')).vue();
    case 'html':
    case 'htm':
      return (await import('@codemirror/lang-html')).html();
    case 'css':
      return (await import('@codemirror/lang-css')).css();
    case 'json':
      return (await import('@codemirror/lang-json')).json();
    case 'md':
      return (await import('@codemirror/lang-markdown')).markdown();
    case 'py':
      return (await import('@codemirror/lang-python')).python();
    case 'php':
      return (await import('@codemirror/lang-php')).php();
    case 'go':
      return (await import('@codemirror/lang-go')).go();
    case 'yaml':
    case 'yml':
      return (await import('@codemirror/lang-yaml')).yaml();
    default:
      return null;
  }
}

function hasNul(s: string): boolean {
  return s.indexOf('\u0000') >= 0;
}

let mergeView: any = null; // MergeView 或 EditorView（内联）
function destroyEditor() {
  if (mergeView) {
    mergeView.destroy();
    mergeView = null;
  }
}

const editableListener = EditorView.updateListener.of((up) => {
  if (up.docChanged) dirty.value = up.state.doc.toString() !== rightText;
});

async function buildEditor() {
  destroyEditor();
  if (!hostEl.value) return;
  hostEl.value.innerHTML = '';
  const lang = await loadLanguage(extOf(path.value));
  const common: any[] = [basicSetup, oneDark, EditorView.lineWrapping];
  if (lang) common.push(lang);

  const { MergeView, unifiedMergeView } = await import('@codemirror/merge');

  if (mode.value === 'inline') {
    const exts = [
      ...common,
      unifiedMergeView({
        original: leftText,
        highlightChanges: true,
        gutter: true,
        mergeControls: false,
        collapseUnchanged: { margin: 3, minSize: 4 },
      }),
    ];
    if (canEdit.value) exts.push(editableListener);
    else exts.push(EditorState.readOnly.of(true), EditorView.editable.of(false));
    mergeView = new EditorView({ doc: rightText, parent: hostEl.value, extensions: exts });
    return;
  }

  // 并排：a = 左（基线，只读），b = 右（当前；工作区侧可编辑）
  const aExt = [...common, EditorState.readOnly.of(true), EditorView.editable.of(false)];
  const bExt = canEdit.value
    ? [...common, editableListener]
    : [...common, EditorState.readOnly.of(true), EditorView.editable.of(false)];
  mergeView = new MergeView({
    a: { doc: leftText, extensions: aExt },
    b: { doc: rightText, extensions: bExt },
    parent: hostEl.value,
    highlightChanges: true,
    gutter: true,
    collapseUnchanged: { margin: 3, minSize: 4 },
  });
}

async function load() {
  destroyEditor();
  err.value = '';
  binary.value = false;
  dirty.value = false;
  savePath = '';
  leftText = '';
  rightText = '';
  patch.value = '';
  if (!props.repo || !props.opts) return;
  loading.value = true;
  try {
    const p = props.opts.path || '';
    if (!p) {
      // 没有单文件路径（整仓 diff）：并排/内联无意义，直接补丁兜底
      patch.value = (await GoTools.GitDiffText(props.repo, props.opts)) || '';
      mode.value = 'patch';
      return;
    }
    const commit = props.opts.commit || '';
    if (commit) {
      leftText = (await GoTools.GitShowBlob(props.repo, commit + '^', p)) || '';
      rightText = (await GoTools.GitShowBlob(props.repo, commit, p)) || '';
    } else if (props.opts.staged) {
      leftText = (await GoTools.GitShowBlob(props.repo, 'HEAD', p)) || '';
      rightText = (await GoTools.GitShowBlob(props.repo, '', p)) || ''; // 空 rev = 暂存区
    } else {
      leftText = (await GoTools.GitShowBlob(props.repo, 'HEAD', p)) || '';
      const r: any = await GoTools.FileReadText(joinRepo(props.repo, p));
      rightText = r?.binary ? '' : r?.content ?? '';
      // 仅在非截断、非二进制时允许就地编辑保存（与 CodePane 同一安全口径）
      if (!r?.binary && !r?.truncated) savePath = joinRepo(props.repo, p);
      else binary.value = !!r?.binary;
    }
    if (hasNul(leftText) || hasNul(rightText)) {
      binary.value = true;
      return;
    }
    loading.value = false;
    await nextTick();
    await buildEditor();
  } catch (e: any) {
    // 取全文失败（例如版本对象异常）时退化成补丁视图，尽量让用户还能看到差异
    try {
      patch.value = (await GoTools.GitDiffText(props.repo, props.opts)) || '';
      mode.value = 'patch';
    } catch {
      err.value = String(e?.message || e || '读取差异失败');
    }
  } finally {
    loading.value = false;
  }
}

async function save() {
  if (!canEdit.value || !mergeView || !dirty.value) return;
  const text = mergeView.state?.doc?.toString?.() ?? mergeView.b?.state?.doc?.toString?.();
  if (text == null) return;
  saving.value = true;
  try {
    await GoTools.FileWriteText(savePath, text);
    rightText = text;
    dirty.value = false;
    Message.success({ content: '已保存：' + path.value, position: 'top', duration: 1500 });
    emit('saved');
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '保存失败'), position: 'top', duration: 4000 });
  } finally {
    saving.value = false;
  }
}

watch(mode, async () => {
  if (mode.value === 'patch' || binary.value || err.value) {
    destroyEditor();
    return;
  }
  await nextTick();
  await buildEditor();
});
watch(
  () => [props.repo, props.opts],
  load,
  { deep: true, immediate: true }
);
onBeforeUnmount(destroyEditor);
</script>

<style scoped>
.dp {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}
.dp-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 6px 2px;
  border-bottom: 1px solid var(--color-border-2);
  flex: none;
}
.dp-left {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.dp-path {
  font-size: 12px;
  color: var(--color-text-1);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.dp-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: rgb(var(--danger-6));
  flex: none;
}
.dp-state {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: var(--color-text-3);
}
.dp-err {
  color: rgb(var(--danger-6));
}
.dp-host {
  flex: 1;
  min-height: 0;
  overflow: auto;
}
.dp-patch-wrap {
  flex: 1;
  min-height: 0;
  overflow: auto;
}
.dp-pre {
  margin: 0;
  padding: 10px 12px;
  color: var(--color-text-1);
  font-family: var(--code-font-family);
  font-size: 12px;
  line-height: 1.5;
  white-space: pre;
}
.dp-host :deep(.cm-editor) {
  font-size: 13px;
}
.dp-host :deep(.cm-mergeView) {
  height: 100%;
}
.dp-host :deep(.cm-mergeViewEditor) .cm-editor {
  height: auto;
}
</style>
