<template>
  <!-- 进程守护：登记本地程序，守护引擎负责拉起、监控、意外退出后自动重启 -->
  <div class="gd-shell">
    <common-title-bar>
      <template #title>
        <span class="gd-sub">登记程序后自动拉起并监控，意外退出按退避策略重启</span>
      </template>
    </common-title-bar>
    <a-card class="gd-card">
      <template #title>
        <a-space>
          <a-button size="small" @click="openAdd">
            <template #icon><icon-plus /></template>新增
          </a-button>
          <a-button  size="small" @click="startAll">启用所有程序</a-button>
          <a-button size="small" @click="stopAll">关闭所有程序</a-button>
          <a-popconfirm content="确定清空全部守护登记项？正在运行的程序会一并停止。" type="warning" @ok="clearAll">
            <a-button  size="small" >清空</a-button>
          </a-popconfirm>
        </a-space>
      </template>
      <template #extra>
        <a-space>
          <a-checkbox :model-value="autoStart" @change="onAutoStart">开机启动</a-checkbox>
          <a-popover trigger="click" position="br" title="守护设置">
            <a-button   size="small">
              <template #icon><icon-settings /></template>守护设置
            </a-button>
            <template #content>
              <div class="gd-set">
                <div class="gd-set-row">
                  <span>重启间隔</span>
                  <a-input-number v-model="delayInput" :min="0" :max="3600" hide-button style="width: 120px">
                    <template #suffix>秒</template>
                  </a-input-number>
                </div>
                <div class="gd-set-row">
                  <span>连续失败上限</span>
                  <a-input-number v-model="maxFailInput" :min="1" :max="1000" hide-button style="width: 120px" />
                </div>
                <div class="gd-set-tip">进程运行不足 5 秒即退出计一次快速失败，达到上限则暂停该项守护。</div>
                <a-button size="small" long @click="saveSettings">保存</a-button>
              </div>
            </template>
          </a-popover>
        </a-space>
      </template>

      <!-- 列表 -->
      <a-table class="gd-table" :data="rows" :columns="columns" :loading="loading" :pagination="false" row-key="id"
        size="medium">
        <template #index="{ rowIndex }">{{ rowIndex + 1 }}</template>
        <template #name="{ record }">
          <span class="gd-name">{{ record.name }}</span>
        </template>
        <template #path="{ record }">
          <a-tooltip :content="record.path" mini>
            <span class="gd-path">{{ record.path }}</span>
          </a-tooltip>
          <div v-if="record.args" class="gd-args">{{ record.args }}</div>
        </template>
        <template #state="{ record }">
          <a-tag :color="stateColor(record.state)" size="small">{{ stateText(record.state) }}</a-tag>
          <span v-if="record.running && record.pid" class="gd-sub-inline">PID {{ record.pid }}</span>
          <span v-if="record.restarts > 0" class="gd-sub-inline">重启 {{ record.restarts }} 次</span>
          <div v-if="record.lastError && record.state !== 'running'" class="gd-err">{{ record.lastError }}</div>
        </template>
        <template #enabled="{ record }">
          <a-switch :model-value="record.enabled" type="round" size="small" :disabled="loading"
            @change="(v: any) => toggleEnabled(record, !!v)" />
        </template>
        <template #ops="{ record }">
          <a-space :size="4">
            <a-button v-if="!record.running" size="mini" @click="startOne(record)">启动</a-button>
            <a-button v-else size="mini" @click="stopOne(record)">停止</a-button>
            <a-button size="mini" @click="openEdit(record)">编辑</a-button>
            <a-popconfirm content="删除该守护项？正在运行会先停止。" @ok="remove(record)">
              <a-button size="mini" status="danger">删除</a-button>
            </a-popconfirm>
          </a-space>
        </template>
        <template #empty>
          <div class="gd-empty">暂无守护程序，点击「新增」添加要守护的程序</div>
        </template>
      </a-table>
    </a-card>

    <!-- 新增 / 编辑弹窗 -->
    <a-modal v-model:visible="modalVisible" :title="editingId ? '编辑守护程序' : '新增守护程序'" :ok-loading="saving"
      :mask-closable="false" @ok="save" @cancel="modalVisible = false">
      <a-form :model="form" layout="vertical">
        <a-form-item field="name" label="程序名称">
          <a-input v-model="form.name" placeholder="留空则取可执行文件名" allow-clear />
        </a-form-item>
        <a-form-item field="path" label="程序路径" required>
          <a-input-group compact>
            <a-input v-model="form.path" placeholder="要守护的可执行文件绝对路径" style="width: calc(100% - 72px)" />
            <a-button style="width: 72px" @click="browseProgram">浏览</a-button>
          </a-input-group>
        </a-form-item>
        <a-form-item field="args" label="启动参数">
          <a-input v-model="form.args" placeholder="如：-p 8080 --config app.yaml（可留空）" allow-clear />
        </a-form-item>
        <a-form-item field="workDir" label="工作目录">
          <a-input-group compact>
            <a-input v-model="form.workDir" placeholder="留空则用程序所在目录" style="width: calc(100% - 72px)" />
            <a-button style="width: 72px" @click="browseWorkDir">浏览</a-button>
          </a-input-group>
        </a-form-item>
        <a-form-item field="enabled" label="启用守护">
          <a-switch v-model="form.enabled" type="round"  size="small"/>
          <span class="gd-hint-inline">{{ form.enabled ? '随应用启动并自动重启' : '仅登记，不自动拉起' }}</span>
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { getCurrentInstance, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { Message } from '@arco-design/web-vue';
import { Events } from '@wailsio/runtime';

/**
 * 进程守护页。守护引擎全在 Go（tools/guard），本页发指令、订阅 guard:status、渲染表格。
 * 每次引擎状态变更都会回推全量快照，本页不主动轮询。
 */
const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

interface GuardRow {
  id: string;
  name: string;
  path: string;
  args: string;
  workDir: string;
  enabled: boolean;
  running: boolean;
  pid: number;
  paused: boolean;
  restarts: number;
  lastError: string;
  state: string;
}

const rows = ref<GuardRow[]>([]);
const loading = ref(false);
const autoStart = ref(false);
const delayInput = ref(3);
const maxFailInput = ref(5);

const columns: any[] = [
  { title: '序号', slotName: 'index', width: 64, align: 'center' },
  { title: '程序名称', slotName: 'name', width: 160, ellipsis: true, tooltip: true },
  { title: '程序路径', slotName: 'path', ellipsis: true },
  { title: '运行状态', slotName: 'state', width: 220 },
  { title: '启用守护', slotName: 'enabled', width: 96, align: 'center' },
  { title: '操作', slotName: 'ops', width: 200, align: 'center' },
];

const modalVisible = ref(false);
const saving = ref(false);
const editingId = ref('');
const form = reactive({ name: '', path: '', args: '', workDir: '', enabled: true });

let offEvent: any = null;

const errText = (e: any) => String(e?.message || e || '操作失败');

function stateText(s: string): string {
  return ({ running: '运行中', stopped: '已停止', paused: '守护暂停', disabled: '未启用' } as Record<string, string>)[s] || s;
}
function stateColor(s: string): string {
  return ({ running: 'green', paused: 'orange', disabled: 'gray', stopped: 'gray' } as Record<string, string>)[s] || 'gray';
}

function apply(s: any) {
  if (!s) return;
  rows.value = (s.rows || []) as GuardRow[];
  autoStart.value = !!s.autoStart;
  if (typeof s.restartDelaySec === 'number') delayInput.value = s.restartDelaySec;
  if (typeof s.maxFastFail === 'number') maxFailInput.value = s.maxFastFail;
}

function onStatus(raw: any) {
  apply(raw?.data ?? raw);
}

async function refresh() {
  loading.value = true;
  try {
    apply(await GoTools.GuardGetState());
  } catch (e) {
    Message.error({ content: errText(e), position: 'top', duration: 3000 });
  } finally {
    loading.value = false;
  }
}

function openAdd() {
  editingId.value = '';
  Object.assign(form, { name: '', path: '', args: '', workDir: '', enabled: true });
  modalVisible.value = true;
}

function openEdit(row: GuardRow) {
  editingId.value = row.id;
  Object.assign(form, { name: row.name, path: row.path, args: row.args, workDir: row.workDir, enabled: row.enabled });
  modalVisible.value = true;
}

async function browseProgram() {
  try {
    const p = await GoTools.GuardChooseProgram('选择要守护的程序');
    if (p) {
      form.path = p;
      if (!form.name) {
        const base = p.split(/[\\/]/).pop() || '';
        form.name = base.replace(/\.[^.]+$/, '');
      }
    }
  } catch (e) {
    Message.error({ content: errText(e), position: 'top', duration: 3000 });
  }
}

async function browseWorkDir() {
  try {
    const dir = await GoTools.FtpChooseLocalFolder('选择工作目录');
    if (dir) form.workDir = dir;
  } catch (e) {
    Message.error({ content: errText(e), position: 'top', duration: 3000 });
  }
}

async function save() {
  if (!form.path.trim()) {
    Message.warning({ content: '请选择或填写程序路径', position: 'top' });
    return;
  }
  saving.value = true;
  try {
    await GoTools.GuardUpsert({
      id: editingId.value,
      name: form.name.trim(),
      path: form.path.trim(),
      args: form.args.trim(),
      workDir: form.workDir.trim(),
      enabled: form.enabled,
    });
    modalVisible.value = false;
  } catch (e) {
    Message.error({ content: errText(e), position: 'top', duration: 4000 });
  } finally {
    saving.value = false;
  }
}

async function call(fn: () => Promise<any>) {
  try {
    await fn();
  } catch (e) {
    Message.error({ content: errText(e), position: 'top', duration: 3000 });
  }
}

const toggleEnabled = (row: GuardRow, v: boolean) => call(() => GoTools.GuardSetEnabled(row.id, v));
const startOne = (row: GuardRow) => call(() => GoTools.GuardStart(row.id));
const stopOne = (row: GuardRow) => call(() => GoTools.GuardStop(row.id));
const remove = (row: GuardRow) => call(() => GoTools.GuardRemove(row.id));
const startAll = () => call(() => GoTools.GuardStartAll());
const stopAll = () => call(() => GoTools.GuardStopAll());
const clearAll = () => call(() => GoTools.GuardClear());

async function onAutoStart(v: any) {
  const next = !!v;
  autoStart.value = next; // 乐观更新，失败或事件回推会校正
  await call(() => GoTools.GuardSetAutoStart(next));
}

async function saveSettings() {
  await call(() => GoTools.GuardSetSettings(delayInput.value, maxFailInput.value));
  Message.success({ content: '守护设置已保存', position: 'top', duration: 1500 });
}

onMounted(() => {
  offEvent = Events.On('guard:status', onStatus);
  refresh();
});
onBeforeUnmount(() => {
  offEvent?.();
});
</script>

<style scoped>
.gd-shell {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  box-sizing: border-box;
}

.gd-card {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.gd-card :deep(.arco-card-body) {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.gd-title {
  font-weight: 600;
}

.gd-sub {
  margin-left: 10px;
  color: var(--color-text-3);
  font-size: var(--base-size-small);
}

.gd-toolbar {
  flex: none;
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: var(--base-margin);
}

.gd-toolbar-right {
  display: flex;
  align-items: center;
  gap: 12px;
}

.gd-table {
  flex: 1;
  min-height: 0;
}

.gd-name {
  font-weight: 500;
}

.gd-path {
  color: var(--color-text-2);
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: bottom;
}

.gd-args {
  color: var(--color-text-3);
  font-size: var(--base-size-small);
}

.gd-sub-inline {
  margin-left: 8px;
  color: var(--color-text-3);
  font-size: var(--base-size-small);
}

.gd-err {
  margin-top: 2px;
  color: rgb(var(--red-6));
  font-size: var(--base-size-small);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.gd-empty {
  color: var(--color-text-4);
  padding: 40px 0;
  text-align: center;
}

.gd-hint-inline {
  margin-left: 8px;
  color: var(--color-text-3);
  font-size: var(--base-size-small);
}

.gd-set {
  width: 240px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.gd-set-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.gd-set-tip {
  color: var(--color-text-3);
  font-size: var(--base-size-small);
  line-height: 1.5;
}
</style>
