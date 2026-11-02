<template>
  <!-- Git 工作台：左列「仓库登记」，右列三段式（分支头 / 变更区 / 提交框），与终端页同款骨架 -->
  <a-row class="git-page" :gutter="6">
    <!-- 左：仓库登记 -->
    <a-col :span="6" class="side-col">
      <a-card class="side-card card-head-tool" size="small" :body-style="{ padding: 0 }">
        <template #title>仓库</template>
        <template #extra>
          <div class="git-head-actions">
            <a-button size="small" type="text" :loading="reposBusy" @click="refreshAll">
              <template #icon><icon-refresh /></template>
            </a-button>
            <a-button size="small" type="text" @click="addRepo">
              <template #icon><icon-plus /></template>
            </a-button>
          </div>
        </template>

        <a-list :bordered="false" size="small">
          <a-list-item v-for="r in repos" :key="r.path" :class="{ 'repo-active': r.path === cur.path }"
            @click="openRepo(r.path)">
            <a-tooltip :content="r.path">
              <span class="repo-name">{{ r.name }}</span>
            </a-tooltip>
            <template v-if="summaries[r.path]">
              <a-tag v-if="summaries[r.path].conflicted" size="small" color="red">冲突{{ summaries[r.path].conflicted }}</a-tag>
              <a-tag v-else-if="dirtyOf(r.path)" size="small" color="orangered">改动{{ dirtyOf(r.path) }}</a-tag>
              <a-tag v-if="summaries[r.path].behind" size="small" color="orange">↓{{ summaries[r.path].behind }}</a-tag>
              <a-tag v-if="summaries[r.path].ahead" size="small" color="green">↑{{ summaries[r.path].ahead }}</a-tag>
            </template>
            <template #actions>
              <a-dropdown trigger="hover" @select="(v: any) => onRepoAction(v, r)">
                <icon-more class="repo-more" @click.stop />
                <template #content>
                  <a-doption value="open"><icon-folder /> 打开工作台</a-doption>
                  <a-doption value="del"><icon-delete /> 移除登记</a-doption>
                </template>
              </a-dropdown>
            </template>
          </a-list-item>
          <a-list-item v-if="!repos.length">
            <div class="repo-empty">{{ reposBusy ? '读取中…' : '暂无仓库，点右上「+」添加' }}</div>
          </a-list-item>
        </a-list>
      </a-card>
    </a-col>

    <!-- 右：工作台 -->
    <a-col :span="18" class="main-col">
      <a-card v-if="!gitOk" class="main-panel empty-card card-head-tool">
        <a-empty>
          <a-space direction="vertical" :size="10">
            <a-typography-text type="secondary">{{ gitReason || '正在检测本机 git…' }}</a-typography-text>
            <a-button v-if="gitChecked" size="small" :loading="rechecking" @click="checkGit">
              <template #icon><icon-refresh /></template>重新检测
            </a-button>
          </a-space>
        </a-empty>
      </a-card>

      <a-card v-else-if="!cur.path" class="main-panel empty-card card-head-tool">
        <a-empty>
          <span>从左侧选择一个仓库打开工作台</span>
        </a-empty>
      </a-card>

      <a-space v-else direction="vertical" :size="6" class="work-col">
        <!-- 分支头 -->
        <a-card class="head-card card-head-tool" size="small">
          <div class="head-bar">
            <a-space :size="8">
              <a-tooltip :content="cur.path">
                <span class="head-repo">{{ baseName(cur.path) }}</span>
              </a-tooltip>
              <a-tag v-if="st && !detached" size="small" color="arcoblue">
                <icon-branch /> {{ st.branch }}
              </a-tag>
              <a-tag v-else-if="st" size="small" color="orange">头分离</a-tag>
              <a-space v-if="st && st.upstream" :size="4">
                <a-tag v-if="st.ahead" size="small" color="green">↑{{ st.ahead }}</a-tag>
                <a-tag v-if="st.behind" size="small" color="orange">↓{{ st.behind }}</a-tag>
              </a-space>
              <a-typography-text v-if="st && st.upstream" type="secondary" class="head-up">{{ st.upstream }}</a-typography-text>
            </a-space>
            <a-space :size="6">
              <a-button size="small" :loading="statusBusy" @click="refreshStatus">
                <template #icon><icon-refresh /></template>刷新状态
              </a-button>
              <a-button size="small" @click="branchVisible = true">
                <template #icon><icon-branches /></template>分支
              </a-button>
              <a-dropdown trigger="click" @select="(v: any) => onRemoteOp(String(v))">
                <a-button size="small" :loading="remoteBusy">
                  <template #icon><icon-send /></template>同步<icon-down />
                </a-button>
                <template #content>
                  <a-doption value="fetch"><icon-download /> 抓取（Fetch）</a-doption>
                  <a-doption value="pull"><icon-import /> 拉取（Pull）</a-doption>
                  <a-doption value="push"><icon-upload /> 推送（Push）</a-doption>
                </template>
              </a-dropdown>
            </a-space>
          </div>
          <common-task-progress v-if="prog" class="head-prog" :percent="prog.percent" :meta="prog.note"
            :status="prog.status" size="small" :show-percent="true" :name="progTitle" />
        </a-card>

        <!-- 变更区 -->
        <a-card class="changes-card card-head-tool" size="small" :body-style="{ padding: '6px 10px' }">
          <template #title>
            <a-space :size="8">
              <span>变更</span>
              <a-typography-text type="secondary" class="changes-count">
                暂存{{ st?.staged?.length || 0 }} / 未暂存{{ st?.unstaged?.length || 0 }} / 未跟踪{{ st?.untracked?.length || 0 }}<template v-if="st?.conflicted?.length"> / 冲突{{ st.conflicted.length }}</template>
              </a-typography-text>
            </a-space>
          </template>
          <template #extra>
            <a-space :size="6">
              <a-button size="mini" @click="doStageAll">全部暂存</a-button>
              <a-button size="mini" @click="doUnstageAll">全部取消暂存</a-button>
            </a-space>
          </template>

          <a-spin :loading="statusBusy" class="changes-spin">
            <div class="grp" v-if="st?.conflicted?.length">
              <div class="grp-head conflict">冲突（{{ st.conflicted.length }}）<span class="grp-tip">— P1 仅列出，请在别处完成合并/变基后刷新</span></div>
              <div v-for="e in st.conflicted" :key="'c' + e.path" class="file-row">
                <span class="file-xy uu">UU</span>
                <span class="file-path" @click="openDiff(e.path, true)">{{ e.path }}</span>
              </div>
            </div>
            <div class="grp">
              <div class="grp-head">已暂存（{{ st?.staged?.length || 0 }}）</div>
              <div v-for="e in st?.staged || []" :key="'s' + e.path" class="file-row">
                <a-checkbox size="mini" v-model="picked.staged[e.path]" />
                <span class="file-xy" :class="xyClass(e)">{{ xyText(e) }}</span>
                <span class="file-path" @click="openDiff(e.path, true)">
                  {{ e.path }}<template v-if="e.origPath"> ← {{ e.origPath }}</template>
                </span>
                <a-button size="mini" type="text" @click="doUnstage([e.path])">取消</a-button>
              </div>
            </div>
            <div class="grp">
              <div class="grp-head">未暂存（{{ st?.unstaged?.length || 0 }}）</div>
              <div v-for="e in st?.unstaged || []" :key="'u' + e.path" class="file-row">
                <a-checkbox size="mini" v-model="picked.unstaged[e.path]" />
                <span class="file-xy" :class="xyClass(e)">{{ xyText(e) }}</span>
                <span class="file-path" @click="openDiff(e.path, false)">
                  {{ e.path }}<template v-if="e.origPath"> ← {{ e.origPath }}</template>
                </span>
                <a-button size="mini" type="text" @click="doStage([e.path])">暂存</a-button>
              </div>
            </div>
            <div class="grp">
              <div class="grp-head">未跟踪（{{ st?.untracked?.length || 0 }}）</div>
              <div v-for="e in st?.untracked || []" :key="'t' + e.path" class="file-row">
                <a-checkbox size="mini" v-model="picked.untracked[e.path]" />
                <span class="file-xy add">??</span>
                <span class="file-path" @click="openDiff(e.path, false)">{{ e.path }}</span>
                <a-button size="mini" type="text" @click="doStage([e.path])">暂存</a-button>
              </div>
            </div>
            <div v-if="st && !st.staged?.length && !st.unstaged?.length && !st.untracked?.length && !st.conflicted?.length"
              class="changes-empty">
              工作区干净<template v-if="st.ahead || st.behind">（但有 {{ st.ahead }} 笔未推送 / {{ st.behind }} 笔远端新提交）</template>
            </div>
          </a-spin>
        </a-card>

        <!-- 提交框 -->
        <a-card class="commit-card card-head-tool" size="small" :body-style="{ padding: '8px 10px' }">
          <a-textarea v-model="msg" :auto-size="{ minRows: 2, maxRows: 5 }" placeholder="提交说明（Ctrl+Enter 提交）"
            @keydown.ctrl.enter="doCommit" />
          <div class="commit-bar">
            <a-space :size="8">
              <a-checkbox v-model="amend" size="small" :disabled="!st?.staged?.length && !amend">amend（改写上一笔）</a-checkbox>
              <a-typography-text v-if="lastCommit" type="success" class="commit-ok">已提交 {{ lastCommit }}</a-typography-text>
            </a-space>
            <a-button type="primary" size="small" :loading="commitBusy"
              :disabled="!msg.trim() || (!st?.staged?.length && !amend)" @click="doCommit">
              <template #icon><icon-send /></template>提交（{{ st?.staged?.length || 0 }}）
            </a-button>
          </div>
        </a-card>
      </a-space>
    </a-col>
  </a-row>

  <branch-modal v-model:visible="branchVisible" :repo="cur.path" @changed="refreshStatus" />
  <diff-modal v-model:visible="diffVisible" :repo="cur.path" :opts="diffOpts" />
</template>

<script setup lang="ts">
/**
 * Git 工作台（P1 核心集）：仓库登记、状态分组、暂存/提交、分支管理与 fetch/pull/push。
 *
 * 刷新策略与计划一致：手动刷新 + 进入仓库时拉一次，不做轮询（大仓 git status 本身就贵）。
 * push/pull/fetch 进度经 git:progress 事件（任务中心同源），这里只展示最近一条。
 */
import { computed, getCurrentInstance, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { Message } from '@arco-design/web-vue';
import { Events } from '@wailsio/runtime';
import BranchModal from './components/BranchModal.vue';
import DiffModal from './components/DiffModal.vue';
import type { RepoRef, Status } from 'bindings/Tools/tools/gitcmd/index';

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

/* ==================== git 检测 ==================== */
const gitOk = ref(false);
const gitChecked = ref(false);
const rechecking = ref(false);
const gitReason = ref('');

async function checkGit() {
  rechecking.value = true;
  try {
    const info = await GoTools.GitCheck();
    gitOk.value = !!info?.ok;
    gitReason.value = info?.reason || '';
  } catch {
    gitOk.value = false;
    gitReason.value = '检测 git 失败';
  } finally {
    gitChecked.value = true;
    rechecking.value = false;
  }
}

/* ==================== 仓库登记 ==================== */
const repos = ref<RepoRef[]>([]);
const reposBusy = ref(false);
const summaries = reactive<Record<string, Status>>({});

function baseName(p: string): string {
  const parts = p.replace(/[\\/]+$/, '').split(/[\\/]/);
  return parts[parts.length - 1] || p;
}
function dirtyOf(path: string): number {
  const s = summaries[path];
  if (!s) return 0;
  return (s.staged?.length || 0) + (s.unstaged?.length || 0) + (s.untracked?.length || 0);
}

async function loadRepos() {
  reposBusy.value = true;
  try {
    repos.value = (await GoTools.GitRepos()) || [];
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '读取仓库登记失败'), position: 'top', duration: 3000 });
  } finally {
    reposBusy.value = false;
  }
}

async function addRepo() {
  let dir = '';
  try {
    dir = (await GoTools.ChooseDir('选择 Git 仓库目录')) || '';
  } catch {
    return;
  }
  if (!dir) return;
  try {
    repos.value = (await GoTools.GitRepoAdd(dir)) || [];
    const last = repos.value[repos.value.length - 1];
    if (last) openRepo(last.path);
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '登记失败：目录可能不是 Git 仓库'), position: 'top', duration: 4000 });
  }
}

function onRepoAction(v: string, r: RepoRef) {
  if (v === 'open') openRepo(r.path);
  else if (v === 'del') {
    GoTools.GitRepoRemove(r.path)
      .then((list) => {
        repos.value = list || [];
        delete summaries[r.path];
        if (cur.value.path === r.path) {
          cur.value = { path: '' };
          st.value = null;
        }
      })
      .catch((e: any) => Message.error({ content: String(e?.message || e), position: 'top', duration: 3000 }));
  }
}

/** 逐个仓库拉一次状态做徽标（登记表通常个位数仓库，串行防大仓并发卡顿） */
async function refreshAll() {
  await loadRepos();
  for (const r of repos.value) {
    try {
      summaries[r.path] = await GoTools.GitStatus(r.path);
    } catch {
      delete summaries[r.path]; // 仓库可能已被移动/删除
    }
  }
  if (cur.value.path) refreshStatus();
}

/* ==================== 当前仓库 ==================== */
const cur = ref<{ path: string }>({ path: '' });
const st = ref<Status | null>(null);
const statusBusy = ref(false);
const picked = reactive({ staged: {} as Record<string, boolean>, unstaged: {} as Record<string, boolean>, untracked: {} as Record<string, boolean> });

function openRepo(path: string) {
  cur.value = { path };
  st.value = null;
  resetPicked();
  refreshStatus();
}

function resetPicked() {
  picked.staged = {};
  picked.unstaged = {};
  picked.untracked = {};
}

async function refreshStatus() {
  if (!cur.value.path) return;
  statusBusy.value = true;
  try {
    st.value = await GoTools.GitStatus(cur.value.path);
    if (st.value) summaries[cur.value.path] = st.value;
    resetPicked();
  } catch (e: any) {
    st.value = null;
    Message.error({ content: String(e?.message || e || '读取状态失败'), position: 'top', duration: 4000 });
  } finally {
    statusBusy.value = false;
  }
}

function pickedPaths(group: 'staged' | 'unstaged' | 'untracked'): string[] {
  return Object.keys(picked[group]).filter((p) => picked[group][p]);
}

/* ==================== 暂存与提交 ==================== */
const msg = ref('');
const amend = ref(false);
const commitBusy = ref(false);
const lastCommit = ref('');

async function doStage(paths: string[]) {
  try {
    await GoTools.GitStage(cur.value.path, paths);
    refreshStatus();
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '暂存失败'), position: 'top', duration: 4000 });
  }
}
async function doUnstage(paths: string[]) {
  try {
    await GoTools.GitUnstage(cur.value.path, paths);
    refreshStatus();
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '取消暂存失败'), position: 'top', duration: 4000 });
  }
}
async function doStageAll() {
  try {
    await GoTools.GitStageAll(cur.value.path);
    refreshStatus();
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '全部暂存失败'), position: 'top', duration: 4000 });
  }
}
async function doUnstageAll() {
  try {
    await GoTools.GitUnstageAll(cur.value.path);
    refreshStatus();
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '取消失败'), position: 'top', duration: 4000 });
  }
}

async function doCommit() {
  const m = msg.value.trim();
  if (!m) return;
  commitBusy.value = true;
  try {
    const hash = await GoTools.GitCommit(cur.value.path, m, amend.value);
    lastCommit.value = hash || '';
    msg.value = '';
    amend.value = false;
    Message.success({ content: `已提交 ${hash}`, position: 'top', duration: 2000 });
    refreshStatus();
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '提交失败'), position: 'top', duration: 5000 });
  } finally {
    commitBusy.value = false;
  }
}

/* ==================== 同步（fetch/pull/push）与进度 ==================== */
const remoteBusy = ref(false);
const branchVisible = ref(false);
const prog = ref<{ op: string; name: string; percent: number; note: string; status: string } | null>(null);

/** porcelain v2 游离 HEAD 时 branch.head 是 "(detached from …)"（LC_ALL=C 固定英文） */
const detached = computed(() => !st.value?.branch || st.value.branch.startsWith('('));

const progTitle = computed(() => {
  const verb = prog.value?.op === 'push' ? '推送' : prog.value?.op === 'pull' ? '拉取' : '抓取';
  return `${verb} ${prog.value?.name || ''}`;
});

let offEvent: (() => void) | null = null;

function onRemoteOp(op: string) {
  if (!cur.value.path || remoteBusy.value) return;
  remoteBusy.value = true;
  prog.value = { op, name: baseName(cur.value.path), percent: 0, note: '开始…', status: 'normal' };
  const fn = op === 'push' ? GoTools.GitPush : op === 'pull' ? GoTools.GitPull : GoTools.GitFetch;
  fn(cur.value.path)
    .then(() => {
      Message.success({ content: `${op} 完成`, position: 'top', duration: 2000 });
      refreshStatus();
    })
    .catch((e: any) => {
      Message.error({ content: String(e?.message || e || `${op} 失败`), position: 'top', duration: 5000 });
    })
    .finally(() => {
      remoteBusy.value = false;
      setTimeout(() => (prog.value = null), 1500);
    });
}

function onProgress(ev: any) {
  const p = ev?.data?.[0];
  if (!p) return;
  prog.value = {
    op: p.op,
    name: p.name || '',
    percent: p.percent || 0,
    note: p.note || '',
    status: p.stage === 'done' ? 'success' : p.stage === 'error' ? 'exception' : 'normal',
  };
}

/* ==================== 差异弹窗 ==================== */
const diffVisible = ref(false);
const diffOpts = ref<Record<string, unknown>>({});

function openDiff(path: string, staged: boolean) {
  diffOpts.value = { staged, commit: '', path };
  diffVisible.value = true;
}

/* ==================== 展示辅助 ==================== */
function xyText(e: any): string {
  const x = e.x === '.' ? ' ' : e.x || ' ';
  const y = e.y === '.' ? ' ' : e.y || ' ';
  return `${x}${y}`.trim() || '·';
}
function xyClass(e: any): string {
  if (e.x === 'A' || (e.x === '?' && e.y === '?')) return 'add';
  if (e.x === 'D' || e.y === 'D') return 'del';
  if (e.x === 'R' || e.y === 'R') return 'ren';
  return 'mod';
}

/* ==================== 生命周期 ==================== */
onMounted(async () => {
  await checkGit();
  if (gitOk.value) loadRepos();
  offEvent = Events.On('git:progress', onProgress);
});
onBeforeUnmount(() => {
  if (offEvent) offEvent();
});
</script>

<style scoped>
.git-page {
  height: 100%;
}
.side-col,
.main-col {
  height: 100%;
  display: flex;
  flex-direction: column;
}
.side-card {
  flex: 1;
  min-height: 0;
  background: var(--color-bg-1);
  border-radius: var(--base-radius-default, 8px);
  overflow: auto;
}
.main-panel {
  height: 100%;
  background: var(--color-bg-1);
  border-radius: var(--base-radius-default, 8px);
}
.git-head-actions {
  display: flex;
  align-items: center;
  gap: 2px;
}
.repo-active {
  background: var(--color-fill-2);
  box-shadow: inset 3px 0 0 rgb(var(--primary-6));
}
.repo-name {
  font-weight: 500;
  max-width: 150px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  display: inline-block;
}
.repo-more {
  color: var(--color-text-3);
}
.repo-empty {
  color: var(--color-text-3);
  font-size: 12px;
}
.work-col {
  width: 100%;
}
.head-card,
.changes-card,
.commit-card {
  background: var(--color-bg-1);
  border-radius: var(--base-radius-default, 8px);
}
.head-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 6px;
}
.head-repo {
  font-weight: 600;
}
.head-up {
  font-size: 12px;
}
.head-prog {
  margin-top: 8px;
}
.changes-spin {
  display: block;
  width: 100%;
  max-height: 46vh;
  overflow: auto;
}
.grp {
  margin-bottom: 8px;
}
.grp-head {
  font-size: 12px;
  font-weight: 600;
  color: var(--color-text-2);
  padding: 2px 0;
  border-bottom: 1px solid var(--color-fill-2);
  margin-bottom: 2px;
}
.grp-head.conflict {
  color: rgb(var(--red-6));
}
.grp-tip {
  font-weight: 400;
  color: var(--color-text-3);
}
.file-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 1px 2px;
  font-size: 12px;
}
.file-row:hover {
  background: var(--color-fill-1);
}
.file-xy {
  width: 26px;
  flex: none;
  font-family: Consolas, monospace;
  text-align: center;
  color: var(--color-text-3);
}
.file-xy.add {
  color: rgb(var(--green-6));
}
.file-xy.del {
  color: rgb(var(--red-6));
}
.file-xy.ren {
  color: rgb(var(--blue-6));
}
.file-xy.mod {
  color: rgb(var(--orangered-6));
}
.file-xy.uu {
  color: rgb(var(--red-6));
  font-weight: 600;
}
.file-path {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  cursor: pointer;
}
.file-path:hover {
  color: rgb(var(--primary-6));
  text-decoration: underline;
}
.changes-empty {
  color: var(--color-text-3);
  font-size: 12px;
  padding: 10px 0;
  text-align: center;
}
.changes-count {
  font-size: 12px;
  font-weight: 400;
}
.commit-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 6px;
}
.commit-ok {
  font-size: 12px;
}
</style>
