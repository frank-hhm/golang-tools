<template>
  <a-modal :visible="visible" title="冲突解决" width="980px" :footer="false" :esc-to-close="true"
    @update:visible="(v: boolean) => emit('update:visible', v)">
    <!-- 顶栏：操作中断态提示 + rebase 续跑/放弃 -->
    <div class="cf-top">
      <a-space :size="8">
        <a-tag v-if="opState.rebase" color="orange">变基（rebase）进行中</a-tag>
        <a-tag v-else-if="opState.merge" color="arcoblue">合并（merge）进行中</a-tag>
        <a-tag v-else-if="opState.cherryPick" color="purple">拣选（cherry-pick）进行中</a-tag>
        <a-tag v-else-if="opState.revert" color="red">回退（revert）进行中</a-tag>
        <span class="cf-count">待解决 {{ conflicts.length }} 个文件</span>
      </a-space>
      <a-space :size="6">
        <a-button size="small" :loading="loading" @click="load">
          <template #icon><icon-refresh /></template>刷新
        </a-button>
        <template v-if="opState.rebase">
          <a-button size="small" type="primary" :disabled="conflicts.length > 0" :loading="continuing"
            @click="doContinue">继续变基</a-button>
          <a-button size="small" status="danger" :loading="aborting" @click="doAbort">放弃变基</a-button>
        </template>
      </a-space>
    </div>

    <div v-if="opState.rebase" class="cf-warn">
      <icon-exclamation-circle-fill /> 变基语境下「我方 / 对方」与合并相反：这里“我方”=正在被变基到的分支（上游），“对方”=正在重放的你的提交。
    </div>

    <div class="cf-body">
      <!-- 左：冲突文件列表 -->
      <div class="cf-files">
        <div v-for="p in conflicts" :key="p" class="cf-file" :class="{ 'cf-file-on': p === selected }"
          @click="select(p)">
          <span class="cf-file-p" :title="p">{{ p }}</span>
        </div>
        <div v-if="!conflicts.length" class="cf-files-empty">
          <icon-check-circle-fill /> 没有冲突文件
        </div>
      </div>

      <!-- 右：所选文件的三方 / 手动编辑 -->
      <div class="cf-detail">
        <template v-if="!selected">
          <a-empty><span>从左侧选择一个冲突文件</span></a-empty>
        </template>
        <template v-else-if="editMode">
          <div class="cf-edit-bar">
            <span class="cf-edit-title">手动编辑：{{ selected }}（工作区当前含冲突标记，改好后保存）</span>
            <a-button size="mini" @click="editMode = false">返回三方视图</a-button>
          </div>
          <div class="cf-edit-host">
            <diff-pane :repo="repo" :opts="editOpts" @saved="onSaved" />
          </div>
          <div class="cf-actions">
            <a-button type="primary" size="small" :loading="marking" @click="doMark">标记已解决</a-button>
          </div>
        </template>
        <template v-else>
          <div class="cf-cols">
            <div class="cf-col">
              <div class="cf-col-h">基线（共同祖先）</div>
              <pre class="cf-pre">{{ sides?.base || '（无）' }}</pre>
            </div>
            <div class="cf-col">
              <div class="cf-col-h">
                {{ opState.rebase ? '我方（上游）' : '我方（当前分支）' }}
                <a-button size="mini" type="outline" :loading="busy === 'ours'" @click="doResolve('ours')">采用我方</a-button>
              </div>
              <pre class="cf-pre">{{ sides?.ours || '（无）' }}</pre>
            </div>
            <div class="cf-col">
              <div class="cf-col-h">
                {{ opState.rebase ? '对方（重放提交）' : '对方（被并入）' }}
                <a-button size="mini" type="outline" :loading="busy === 'theirs'" @click="doResolve('theirs')">采用对方</a-button>
              </div>
              <pre class="cf-pre">{{ sides?.theirs || '（无）' }}</pre>
            </div>
          </div>
          <div class="cf-actions">
            <a-space :size="8">
              <a-button size="small" @click="enterEdit">手动编辑…</a-button>
              <a-button size="small" :loading="marking" @click="doMark">标记已解决</a-button>
            </a-space>
            <span class="cf-hint">选边会立即用该行版本覆写工作区并标记已解决；手动编辑需保存后再「标记已解决」。</span>
          </div>
        </template>
      </div>
    </div>

    <div v-if="!conflicts.length && started" class="cf-done">
      <template v-if="opState.rebase">全部冲突已解决，点上方「继续变基」完成。</template>
      <template v-else-if="opState.merge || opState.cherryPick || opState.revert">
        全部冲突已解决，回到工作台填写提交说明完成本次合并/拣选/回退。
      </template>
      <template v-else>当前没有进行中的合并操作。</template>
    </div>
  </a-modal>
</template>

<script setup lang="ts">
/**
 * ConflictModal：冲突三方查看与解决（计划 §七 P3）。
 *   - GitConflicts 列冲突文件；GitConflictSides 取 base/ours/theirs 三方内容；
 *   - 采用我方/对方走 GitResolveConflict（内部 checkout --ours/--theirs + add）；
 *   - 手动编辑复用 DiffPane（工作区侧可编辑、FileWriteText 保存），保存后 GitMarkResolved；
 *   - rebase 语境额外给「继续变基 / 放弃变基」（GitRebaseContinue/Abort，后者二次确认）。
 * 破坏性/结束操作后 emit('changed') 让父页刷状态。
 */
import { defineAsyncComponent, getCurrentInstance, reactive, ref, watch } from 'vue';
import { Message, Modal } from '@arco-design/web-vue';
import type { ConflictSides, OpState } from 'bindings/Tools/tools/gitcmd/index';

const DiffPane = defineAsyncComponent(() => import('./DiffPane.vue'));

const props = defineProps<{ visible: boolean; repo: string }>();
const emit = defineEmits<{
  (e: 'update:visible', v: boolean): void;
  (e: 'changed'): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const conflicts = ref<string[]>([]);
const selected = ref('');
const sides = ref<ConflictSides | null>(null);
const opState = reactive<OpState>({ merge: false, rebase: false, cherryPick: false, revert: false, bisect: false });
const loading = ref(false);
const started = ref(false);
const editMode = ref(false);
const editOpts = ref<Record<string, unknown>>({});
const busy = ref('');
const marking = ref(false);
const continuing = ref(false);
const aborting = ref(false);

async function load() {
  if (!props.repo) return;
  loading.value = true;
  started.value = true;
  try {
    conflicts.value = (await GoTools.GitConflicts(props.repo)) || [];
    const os = await GoTools.GitOpState(props.repo);
    if (os) Object.assign(opState, os);
    if (selected.value && !conflicts.value.includes(selected.value)) selected.value = '';
    if (!selected.value && conflicts.value.length) select(conflicts.value[0]);
    else if (selected.value) await loadSides();
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '读取冲突失败'), position: 'top', duration: 3000 });
  } finally {
    loading.value = false;
  }
}

async function select(p: string) {
  selected.value = p;
  editMode.value = false;
  await loadSides();
}

async function loadSides() {
  if (!selected.value) {
    sides.value = null;
    return;
  }
  try {
    sides.value = await GoTools.GitConflictSides(props.repo, selected.value);
  } catch (e: any) {
    sides.value = null;
    Message.error({ content: String(e?.message || e || '读取三方内容失败'), position: 'top', duration: 3000 });
  }
}

function enterEdit() {
  editOpts.value = { staged: false, commit: '', path: selected.value };
  editMode.value = true;
}

async function onSaved() {
  emit('changed');
  await loadSides();
}

async function doResolve(side: 'ours' | 'theirs') {
  if (!selected.value) return;
  busy.value = side;
  try {
    await GoTools.GitResolveConflict(props.repo, selected.value, side);
    Message.success({ content: `已采用${side === 'ours' ? '我方' : '对方'}：${selected.value}`, position: 'top', duration: 1500 });
    emit('changed');
    await load();
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '解决失败'), position: 'top', duration: 4000 });
  } finally {
    busy.value = '';
  }
}

async function doMark() {
  if (!selected.value) return;
  marking.value = true;
  try {
    await GoTools.GitMarkResolved(props.repo, selected.value);
    Message.success({ content: `已标记解决：${selected.value}`, position: 'top', duration: 1500 });
    emit('changed');
    await load();
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '标记失败'), position: 'top', duration: 4000 });
  } finally {
    marking.value = false;
  }
}

async function doContinue() {
  continuing.value = true;
  try {
    await GoTools.GitRebaseContinue(props.repo);
    Message.success({ content: '变基已继续', position: 'top', duration: 1500 });
    emit('changed');
    await load();
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '继续变基失败'), position: 'top', duration: 5000 });
  } finally {
    continuing.value = false;
  }
}

function doAbort() {
  Modal.warning({
    title: '放弃变基',
    content: '将中止本次 rebase，分支回到 rebase 开始前的状态。确认放弃？',
    okText: '放弃变基',
    hideCancel: false,
    onOk: async () => {
      aborting.value = true;
      try {
        await GoTools.GitRebaseAbort(props.repo);
        Message.success({ content: '已放弃变基', position: 'top', duration: 1500 });
        emit('changed');
        await load();
      } catch (e: any) {
        Message.error({ content: String(e?.message || e || '放弃失败'), position: 'top', duration: 4000 });
      } finally {
        aborting.value = false;
      }
    },
  });
}

watch(() => props.visible, (v) => { if (v) load(); });
</script>

<style scoped>
.cf-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}
.cf-count {
  color: var(--color-text-3);
  font-size: 12px;
}
.cf-warn {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 8px;
  padding: 6px 8px;
  border-radius: var(--base-radius-small);
  background: rgba(var(--orange-6), 0.12);
  color: rgb(var(--orange-6));
  font-size: 12px;
}
.cf-body {
  display: flex;
  gap: 8px;
  height: 460px;
}
.cf-files {
  flex: none;
  width: 220px;
  overflow: auto;
  border-right: 1px solid var(--color-border-2);
  padding-right: 6px;
}
.cf-file {
  padding: 5px 8px;
  border-radius: var(--base-radius-small);
  font-size: 12px;
  cursor: pointer;
}
.cf-file:hover {
  background: var(--color-fill-2);
}
.cf-file-on {
  background: var(--color-fill-2);
}
.cf-file-p {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.cf-files-empty {
  padding: 16px 8px;
  color: rgb(var(--green-6));
  font-size: 12px;
  text-align: center;
}
.cf-detail {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.cf-cols {
  display: flex;
  gap: 8px;
  flex: 1;
  min-height: 0;
}
.cf-col {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.cf-col-h {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  padding: 4px 2px;
  font-size: 12px;
  font-weight: 600;
  color: var(--color-text-1);
  border-bottom: 1px solid var(--color-border-1);
}
.cf-pre {
  flex: 1;
  margin: 0;
  padding: 8px;
  overflow: auto;
  background: var(--color-fill-1);
  border-radius: var(--base-radius-small);
  font-family: Consolas, "Courier New", monospace;
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-all;
}
.cf-actions {
  flex: none;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-top: 8px;
}
.cf-hint {
  color: var(--color-text-3);
  font-size: 12px;
}
.cf-edit-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 6px;
}
.cf-edit-title {
  font-size: 12px;
  color: var(--color-text-2);
}
.cf-edit-host {
  flex: 1;
  min-height: 0;
}
.cf-done {
  margin-top: 8px;
  padding: 8px;
  border-radius: var(--base-radius-small);
  background: var(--color-fill-2);
  color: var(--color-text-1);
  font-size: 12px;
}
</style>
