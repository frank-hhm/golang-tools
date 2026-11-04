<template>
  <!-- Git 工作台：左列「仓库登记」，右列三段式（分支头 / 变更区 / 提交框），与终端页同款骨架 -->
  <a-row class="git-page" :gutter="6">
    <!-- 左：仓库登记 -->
    <a-col :span="6" class="side-col">
      <a-card class="side-card card-head-tool" size="small" :body-style="{ padding: 0 }">
        <template #title>仓库</template>
        <template #extra>
          <div class="git-head-actions">
            <a-button size="small" :loading="reposBusy" @click="refreshAll">
              <template #icon><icon-refresh /></template>
            </a-button>
            <a-button size="small" @click="addRepo">
              <template #icon><icon-plus /></template>
            </a-button>
          </div>
        </template>

        <a-list :bordered="false" size="small">
          <a-list-item v-for="r in repos" :key="r.path" :class="{ 'repo-active': r.path === cur.path }"
            @click="openRepo(r.path)">
            <!-- 仓库状态圆点：呼应右侧历史面板的 DAG 节点，选中时点亮为主色 -->
            <span class="repo-dot"></span>
            <a-tooltip :content="r.path">
              <span class="repo-name">{{ r.name }}</span>
            </a-tooltip>
            <template v-if="summaries[r.path]">
              <!-- conflicted 是数组：空数组也是 truthy，以前直接 v-if 会让每个仓库都挂上
                   「冲突」标签，而插值把数组渲染成 [] 就成了截图里的「冲突[]」 -->
              <a-tag v-if="conflictOf(r.path)" size="small" color="red">冲突{{ conflictOf(r.path) }}</a-tag>
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
            <div class="repo-empty">
              <template v-if="reposBusy">读取中…</template>
              <template v-else>
                <icon-folder class="repo-empty-ic" />
                <div>暂无仓库</div>
                <div class="repo-empty-s">点右上「+」登记一个 Git 仓库目录</div>
              </template>
            </div>
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

      <!-- 不用 a-space：它会把每张卡包进 .arco-space-item，下面要靠 flex 把变更区撑满剩余高度，
           隔一层包裹选择器写起来很脆；普通 div + gap 更直接（同文档面板的 .body 写法） -->
      <div v-else class="work-col">
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
              <a-typography-text v-if="st && st.upstream" type="secondary" class="head-up">
                上游 {{ st.upstream }}
              </a-typography-text>
            </a-space>
            <a-space :size="6">
              <a-button size="small" :loading="statusBusy" @click="refreshStatus">
                <template #icon><icon-refresh /></template>刷新状态
              </a-button>
              <a-button size="small" @click="branchVisible = true">
                <template #icon><icon-branches /></template>分支
              </a-button>
              <a-button v-if="st?.conflicted?.length" size="small" status="danger" @click="openConflict()">
                <template #icon><icon-exclamation-circle-fill /></template>解决冲突{{ st.conflicted.length }}
              </a-button>
              <a-dropdown trigger="click" @select="(v: any) => onTools(String(v))">
                <a-button size="small">
                  <template #icon><icon-tool /></template>工具<icon-down />
                </a-button>
                <template #content>
                  <a-doption value="stash"><icon-save /> 暂存栈（Stash）</a-doption>
                  <a-doption value="tag"><icon-tag /> 标签（Tag）</a-doption>
                  <a-doption value="remote"><icon-relation /> 远端（Remote）</a-doption>
                </template>
              </a-dropdown>
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

        <!-- 变更区与提交历史并排：右侧那一大片空白本来就该给 log。
             两个子块保持同一缩进层级，免得 mid-row 这层包装把 60 行变更区整块重排 -->
        <div class="mid-row">
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
              <div class="grp-head conflict">冲突<span class="grp-n">{{ st.conflicted.length }}</span><span class="grp-tip">三方选边 / 手动编辑 / 标记已解决</span></div>
              <div v-for="e in st.conflicted" :key="'c' + e.path" class="file-row">
                <span class="file-xy uu">UU</span>
                <span class="file-path" @click="openConflict()">{{ e.path }}</span>
                <a-link class="file-op" @click="openConflict()">解决</a-link>
              </div>
            </div>
            <!-- 空分组不再列出标题行：三个「（0）」孤零零摆在那儿，只会把真正的改动埋掉 -->
            <div class="grp" v-if="st?.staged?.length">
              <div class="grp-head">已暂存<span class="grp-n">{{ st.staged.length }}</span></div>
              <div v-for="e in st.staged" :key="'s' + e.path" class="file-row">
                <a-checkbox size="mini" v-model="picked.staged[e.path]" />
                <span class="file-xy" :class="xyClass(e)">{{ xyText(e) }}</span>
                <span class="file-path" @click="openDiff(e.path, true)">
                  {{ e.path }}<template v-if="e.origPath"> ← {{ e.origPath }}</template>
                </span>
                <a-link class="file-op" @click="doUnstage([e.path])">取消</a-link>
              </div>
            </div>
            <div class="grp" v-if="st?.unstaged?.length">
              <div class="grp-head">未暂存<span class="grp-n">{{ st.unstaged.length }}</span></div>
              <div v-for="e in st.unstaged" :key="'u' + e.path" class="file-row">
                <a-checkbox size="mini" v-model="picked.unstaged[e.path]" />
                <span class="file-xy" :class="xyClass(e)">{{ xyText(e) }}</span>
                <span class="file-path" @click="openDiff(e.path, false)">
                  {{ e.path }}<template v-if="e.origPath"> ← {{ e.origPath }}</template>
                </span>
                <a-link class="file-op" @click="doStage([e.path])">暂存</a-link>
              </div>
            </div>
            <div class="grp" v-if="st?.untracked?.length">
              <div class="grp-head">未跟踪<span class="grp-n">{{ st.untracked.length }}</span></div>
              <div v-for="e in st.untracked" :key="'t' + e.path" class="file-row">
                <a-checkbox size="mini" v-model="picked.untracked[e.path]" />
                <span class="file-xy add">??</span>
                <span class="file-path" @click="openDiff(e.path, false)">{{ e.path }}</span>
                <a-link class="file-op" @click="doStage([e.path])">暂存</a-link>
              </div>
            </div>
            <div v-if="st && !st.staged?.length && !st.unstaged?.length && !st.untracked?.length && !st.conflicted?.length"
              class="changes-empty">
              <icon-check-circle-fill class="changes-empty-ic" />
              <div class="changes-empty-t">工作区干净<template v-if="st.ahead || st.behind">（但有 {{ st.ahead }} 笔未推送 / {{ st.behind }} 笔远端新提交）</template></div>
              <div class="changes-empty-s">没有待提交的改动，可直接同步（Fetch / Pull / Push）</div>
            </div>
          </a-spin>
        </a-card>

        <history-pane ref="histRef" :repo="cur.path" @diff="openCommitDiff" @blame="openBlame" @changed="onHistChanged" />
        </div>

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
      </div>
    </a-col>
  </a-row>

  <branch-modal v-model:visible="branchVisible" :repo="cur.path" @changed="refreshStatus" />
  <diff-modal v-model:visible="diffVisible" :repo="cur.path" :opts="diffOpts" @saved="refreshStatus" />
  <blame-modal v-model:visible="blameVisible" :repo="cur.path" :path="blameTarget.path" :revision="blameTarget.revision" />
  <stash-modal v-model:visible="stashVisible" :repo="cur.path" @changed="refreshStatus" />
  <tag-modal v-model:visible="tagVisible" :repo="cur.path" />
  <remote-modal v-model:visible="remoteVisible" :repo="cur.path" @changed="refreshStatus" />
  <conflict-modal v-model:visible="conflictVisible" :repo="cur.path" @changed="refreshStatus" />
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
import HistoryPane from './components/HistoryPane.vue';
import BlameModal from './components/BlameModal.vue';
import StashModal from './components/StashModal.vue';
import TagModal from './components/TagModal.vue';
import RemoteModal from './components/RemoteModal.vue';
import ConflictModal from './components/ConflictModal.vue';
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
  return (s.staged?.length || 0) + (s.unstaged?.length || 0) + (s.untracked?.length || 0)
    + (s.conflicted?.length || 0);
}
/**
 * 冲突文件数。左侧列表项上那个红标的 v-if 与插值都读它，而不是在模板里直接
 * `summaries[path].conflicted.length`：索引访问在 v-if 里收窄不了，vue-tsc 会报
 * Object is possibly 'null'。
 * 顺带说明为何 dirtyOf 要把 conflicted 也算进去：porcelain v2 的 "u " 行只进
 * Conflicted、不重复计入 Unstaged，漏算会让“只剩冲突待解决”的仓库读成 0 改动。
 */
function conflictOf(path: string): number {
  return summaries[path]?.conflicted?.length || 0;
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
    // 刚提交完历史必然多一笔，不刷新的话新提交要等到下次进仓库才出现
    histRef.value?.load();
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
      // fetch/pull 会带回新提交，历史面板跟着重拉
      histRef.value?.load();
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

/* ==================== 提交历史 ==================== */
const histRef = ref<InstanceType<typeof HistoryPane> | null>(null);

/* ==================== 差异弹窗 ==================== */
const diffVisible = ref(false);
const diffOpts = ref<Record<string, unknown>>({});

function openDiff(path: string, staged: boolean) {
  diffOpts.value = { staged, commit: '', path };
  diffVisible.value = true;
}

/** 历史面板里点某笔提交的文件：DiffModal 的 opts 早就支持 commit 字段 */
function openCommitDiff(opts: { commit: string; path: string }) {
  diffOpts.value = { staged: false, commit: opts.commit, path: opts.path };
  diffVisible.value = true;
}

/* ==================== 逐行归属（blame） ==================== */
const blameVisible = ref(false);
const blameTarget = ref<{ path: string; revision: string }>({ path: '', revision: '' });

function openBlame(path: string) {
  blameTarget.value = { path, revision: '' };
  blameVisible.value = true;
}

/* ==================== P3：暂存栈 / 标签 / 远端 / 冲突 ==================== */
const stashVisible = ref(false);
const tagVisible = ref(false);
const remoteVisible = ref(false);
const conflictVisible = ref(false);

function onTools(v: string) {
  if (v === 'stash') stashVisible.value = true;
  else if (v === 'tag') tagVisible.value = true;
  else if (v === 'remote') remoteVisible.value = true;
}

function openConflict() {
  conflictVisible.value = true;
}

/** 历史面板里的破坏性操作（reset/checkout/cherry-pick…）改了仓库：状态与历史一起刷 */
function onHistChanged() {
  refreshStatus();
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
let offGitOpen: (() => void) | null = null;

/**
 * P4 右键集成的定位入口：无实例冷启动带来的 pending 与运行实例收到的 git:open
 * 转交都汇到这里。Go 侧已把仓库登记进表并完成 rev-parse 上下文判断，这里只需
 * 刷新列表、定位到该仓库；branch 操作顺带打开分支弹窗（不在这里盲跑网络操作）。
 */
async function applyGitOpen(req: { op?: string; repo?: string } | null) {
  const repo = (req?.repo || '').trim();
  if (!repo || !gitOk.value) return;
  await loadRepos();
  openRepo(repo);
  if (req?.op === 'branch') branchVisible.value = true;
}

onMounted(async () => {
  await checkGit();
  if (gitOk.value) loadRepos();
  offEvent = Events.On('git:progress', onProgress);
  // 无实例冷启动：右键拉起本进程时 Go 记下的定位目标，取一次即消费
  try {
    const p = await GoTools.GetPendingGit?.();
    if (p) await applyGitOpen(JSON.parse(String(p)));
  } catch { /* 非桌面 / 无 pending / 负载坏了：忽略 */ }
  // 运行实例：另一个被右键拉起的短进程把 {op,repo} 转交进来
  try {
    const r = Events.On('git:open', (ev: any) => {
      void applyGitOpen(ev?.data?.[0] ?? ev?.data ?? null);
    });
    if (typeof r === 'function') offGitOpen = r as () => void;
  } catch { /* */ }
});
onBeforeUnmount(() => {
  if (offEvent) offEvent();
  if (offGitOpen) offGitOpen();
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
.side-card :deep(.arco-list-item) {
  align-items: center;
  gap: 4px;
  padding-right: 6px;
  padding-left: 10px;
  cursor: pointer;
}
/* 仓库名吃掉可用宽度，徽标不被挤掉：窄栏下让名称出省略号，比把徽标截走有用 */
.repo-name {
  display: block;
  flex: 1 1 auto;
  min-width: 0;
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
/* 仓库行首的状态圆点：平时是灰环，选中点亮成主色实心带微光晕 */
.repo-dot {
  flex: none;
  width: 8px;
  height: 8px;
  border: 2px solid rgb(var(--gray-4));
  border-radius: var(--border-radius-full);
  transition: border-color 0.12s ease, background-color 0.12s ease;
}
.repo-active .repo-dot {
  background: rgb(var(--primary-6));
  border-color: rgb(var(--primary-6));
  box-shadow: 0 0 0 2px rgba(var(--primary-6), 0.18);
}
.side-card :deep(.arco-list-item .arco-tag) {
  flex: none;
  margin-left: 0;
}
/* 选中态：短竖条 + 淡底，不做整行深边框（与 MySQL 库树、文档面板分类树同一口径） */
.repo-active {
  position: relative;
  background: var(--color-fill-2);
}
.repo-active::before {
  content: "";
  position: absolute;
  top: 50%;
  left: 0;
  width: 3px;
  height: 16px;
  margin-top: -8px;
  border-radius: 0 var(--base-radius-small) var(--base-radius-small) 0;
  background: rgb(var(--primary-6));
}
.repo-more {
  flex: none;
  color: var(--color-text-3);
}
.repo-more:hover {
  color: var(--color-text-1);
}
.repo-empty {
  display: flex;
  width: 100%;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  padding: 10px 0;
  color: var(--color-text-3);
  font-size: 12px;
  text-align: center;
}
.repo-empty-ic {
  color: rgb(var(--gray-5));
  font-size: 22px;
}
.repo-empty-s {
  color: rgb(var(--gray-5));
  font-size: 11px;
}
.work-col {
  display: flex;
  flex-direction: column;
  gap: 6px;
  width: 100%;
  height: 100%;
  min-height: 0;
}
.head-card,
.commit-card {
  flex: none;
}
/* 变更 / 历史两块平分中段宽度；min-width:0 是必须的，否则长路径会把对方顶出去 */
.mid-row {
  display: flex;
  flex: 1;
  gap: 6px;
  min-height: 0;
}
.mid-row > * {
  flex: 1 1 0;
  min-width: 0;
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
/* 变更区吃掉右侧剩余高度：以前靠 max-height:46vh 封顶，改动一多内部滚，
   改动一少下面就永远空一大片，整个工作台看着像还没加载完 */
.changes-card {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-height: 0;
  /* 改动少时按内容收高，不再撑成半屏空 bordered 卡片（顶部与满高历史卡对齐）；
     改动多时到 max-height 上限后内部滚，两块仍共享同一行高基准 */
  align-self: flex-start;
  max-height: 100%;
}
.changes-card :deep(.arco-card-header) {
  flex: none;
}
.changes-card :deep(.arco-card-body) {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}
.changes-spin {
  display: block;
  flex: 1;
  width: 100%;
  min-height: 0;
  overflow: auto;
}
/* a-spin 会把内容再包一层，不把它拉成 flex 列，下面的 flex:1 撑满就接不上 */
.changes-spin :deep(.arco-spin-children) {
  display: flex;
  flex-direction: column;
  min-height: 100%;
}
.grp {
  margin-bottom: 6px;
}
.grp-head {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 3px 2px;
  margin-bottom: 2px;
  border-bottom: 1px solid var(--color-border-1);
  color: var(--color-text-1);
  font-size: 12px;
  font-weight: 600;
}
/* 计数做成小圆块，不再挤在括号里：一排“（0）”当标题后缀时读起来像噪声 */
.grp-n {
  min-width: 18px;
  padding: 0 5px;
  border-radius: var(--border-radius-full);
  background: var(--color-fill-2);
  color: var(--color-text-2);
  font-size: 11px;
  font-weight: 500;
  line-height: 16px;
  text-align: center;
}
.grp-head.conflict {
  color: rgb(var(--red-6));
}
.grp-head.conflict .grp-n {
  background: rgba(var(--red-6), 0.14);
  color: rgb(var(--red-6));
}
.grp-tip {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: 400;
  color: var(--color-text-3);
}
.file-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 2px 4px;
  border-radius: var(--base-radius-small);
  font-size: 12px;
}
.file-row:hover {
  background: var(--color-fill-2);
}
.file-xy {
  display: inline-flex;
  width: 24px;
  height: 16px;
  flex: none;
  align-items: center;
  justify-content: center;
  border-radius: var(--base-radius-small);
  font-family: Consolas, monospace;
  font-size: 11px;
  color: var(--color-text-3);
}
.file-xy.add {
  background: rgba(var(--green-6), 0.14);
  color: rgb(var(--green-6));
}
.file-xy.del {
  background: rgba(var(--red-6), 0.14);
  color: rgb(var(--red-6));
}
.file-xy.ren {
  background: rgba(var(--blue-6), 0.14);
  color: rgb(var(--blue-6));
}
.file-xy.mod {
  background: rgba(var(--orangered-6), 0.14);
  color: rgb(var(--orangered-6));
}
.file-xy.uu {
  background: rgb(var(--red-6));
  color: #fff;
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
  display: flex;
  flex: 1;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 4px;
  padding: 24px 12px;
  font-size: 12px;
  text-align: center;
}
.changes-empty-ic {
  color: rgb(var(--green-6));
  font-size: 26px;
}
.changes-empty-t {
  color: var(--color-text-1);
  font-size: 13px;
}
.changes-empty-s {
  color: var(--color-text-3);
}
/* 行内操作按钮平时藏起来，悬停或键盘聚焦到行上才现身：
   一屏全是「暂存 / 取消」时，眼睛先读到的是按钮而不是改了哪些文件 */
.file-op {
  opacity: 0;
  /* 隐形不等于不存在：不挡住鼠标时，没悬停也可能误点到「暂存 / 取消」 */
  pointer-events: none;
  transition: opacity 0.12s ease;
}
.file-row:hover .file-op,
.file-row:focus-within .file-op {
  opacity: 1;
  pointer-events: auto;
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
