<template>
  <!-- 单侧文件栏：本地与远端共用同一套骨架，靠 side 决定调哪套绑定 -->
  <div class="file-pane">
    <div class="pane-head">
      <div class="pane-title">
        <span class="pane-label">{{ title }}</span>
        <span v-if="listing" class="pane-stat">{{ listing.dirs }} 目录 · {{ listing.files }} 文件</span>
      </div>
      <a-space :size="4">
        <a-tooltip content="上一级">
          <a-button size="mini" :disabled="!listing?.parent" @click="load(listing?.parent || '')">
            <template #icon><icon-arrow-up /></template>
          </a-button>
        </a-tooltip>
        <a-tooltip content="刷新">
          <a-button size="mini" :loading="loading" @click="load(curPath)">
            <template #icon><icon-refresh /></template>
          </a-button>
        </a-tooltip>
        <a-tooltip content="新建目录">
          <a-button size="mini" @click="openMkdir">
            <template #icon><icon-folder-add /></template>
          </a-button>
        </a-tooltip>
        <a-tooltip :content="side === 'remote' ? '上传到当前目录' : uploadDir ? '上传到右侧远端目录' : '请先在右侧打开远端目录'">
          <a-button size="mini" type="primary" :disabled="side === 'local' && !uploadDir" @click="pickUpload">
            <template #icon><icon-upload /></template>上传
          </a-button>
        </a-tooltip>
        <a-tooltip content="显示隐藏文件">
          <a-button size="mini" :type="showHidden ? 'primary' : 'secondary'" @click="showHidden = !showHidden">
            <template #icon><icon-eye /></template>
          </a-button>
        </a-tooltip>
      </a-space>
    </div>

    <div class="pane-path">
      <a-input v-model="pathInput" size="mini" class="path-input" placeholder="输入路径后回车跳转"
        @press-enter="load(pathInput)">
        <template #prefix><icon-folder /></template>
      </a-input>
    </div>

    <div class="pane-body">
      <a-table :data="rows" :loading="loading" row-key="path" :pagination="false" size="mini"
        :bordered="false" :hoverable="true" :stripe="false">
        <template #columns>
          <a-table-column title="名称">
            <template #cell="{ record }">
              <div class="name-cell">
                <span class="name-icon" :class="{ dir: record.isDir }">
                  <component :is="'icon-' + iconOf(record)" />
                </span>
                <a-tooltip :content="record.path">
                  <span class="name-text" :class="{ dir: record.isDir }" @click="activate(record)">{{ record.name }}</span>
                </a-tooltip>
                <a-tag v-if="record.link" size="small" color="orange">链接</a-tag>
              </div>
            </template>
          </a-table-column>
          <a-table-column title="大小" :width="76" align="right">
            <template #cell="{ record }"><span class="dim">{{ record.isDir ? '-' : fmtSize(record.size) }}</span></template>
          </a-table-column>
          <a-table-column title="修改时间" :width="140">
            <template #cell="{ record }"><span class="dim">{{ record.modTime }}</span></template>
          </a-table-column>
          <a-table-column title="权限" :width="60">
            <template #cell="{ record }"><span class="dim">{{ record.mode }}</span></template>
          </a-table-column>
          <a-table-column title="" :width="126" align="right">
            <template #cell="{ record }">
              <a-space :size="2">
                <a-button v-if="side === 'remote'" size="mini" type="text" @click="download(record)">下载</a-button>
                <a-button v-else size="mini" type="text" :disabled="!uploadDir" @click="uploadOne(record)">上传</a-button>
                <a-button size="mini" type="text" @click="openRename(record)">重命名</a-button>
                <a-button size="mini" type="text" status="danger" @click="remove(record)">删除</a-button>
              </a-space>
            </template>
          </a-table-column>
        </template>
      </a-table>
      <a-empty v-if="!loading && !rows.length" class="pane-empty">
        <span>{{ keyword ? '没有匹配的文件' : '这里还没有文件' }}</span>
      </a-empty>
    </div>

    <div class="pane-foot">
      <a-input-search v-model="keyword" size="mini" placeholder="按名称过滤" allow-clear style="width: 180px" />
      <span class="dim">{{ listing?.path || '—' }}</span>
    </div>

    <a-modal v-model:visible="nameVisible" :title="nameMode === 'mkdir' ? '新建目录' : '重命名'" unmount-on-close
      :ok-loading="nameBusy" @ok="submitName">
      <a-input v-model="nameValue" placeholder="名称" allow-clear @press-enter="submitName" />
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onMounted, ref, watch } from 'vue';
import { Message, Modal } from '@arco-design/web-vue';
import { fmtSize, extIcon } from '@/views/tools/disk/helpers';
import type { FtpEntry, FtpListing } from '../types';

/**
 * 文件栏：side=local 走 Disk* 绑定，side=remote 走 Ftp* 绑定。
 * 两端返回的 Listing/Entry 形状刻意保持一致，所以表格、排序、格式化逻辑只有一份。
 */
const props = withDefaults(
  defineProps<{
    side: 'local' | 'remote';
    title: string;
    sessionId?: string;
    /** 远端栏打开时的起始目录 */
    startPath?: string;
    /** 本地栏上传时的目标远端目录（由父组件同步远端栏当前路径） */
    uploadDir?: string;
    /** 下载默认落到哪个本地目录（留空则弹目录选择框） */
    downloadDir?: string;
  }>(),
  { sessionId: '', startPath: '', uploadDir: '', downloadDir: '' },
);

const emit = defineEmits<{
  (e: 'preview', entry: FtpEntry): void;
  (e: 'path-change', path: string): void;
  (e: 'transferred'): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const listing = ref<FtpListing | null>(null);
const loading = ref(false);
const curPath = ref('');
const pathInput = ref('');
const keyword = ref('');
const showHidden = ref(false);
const localCacheKey = 'ftp:localPath';

const rows = computed<FtpEntry[]>(() => {
  const all = listing.value?.entries || [];
  const k = keyword.value.trim().toLowerCase();
  return all.filter((e) => {
    if (!showHidden.value && e.hidden) return false;
    if (!k) return true;
    return e.name.toLowerCase().includes(k);
  });
});

function iconOf(e: FtpEntry): string {
  return e.isDir ? 'folder' : extIcon(e.ext);
}

function isRemote(): boolean {
  return props.side === 'remote';
}

async function load(path?: string) {
  const target = String(path ?? '').trim();
  loading.value = true;
  try {
    const res = isRemote()
      ? await GoTools.FtpListDir(props.sessionId, target)
      : await GoTools.DiskList(target);
    const next = res as FtpListing;
    listing.value = next;
    curPath.value = next.path;
    pathInput.value = next.path;
    keyword.value = '';
    if (isRemote()) emit('path-change', next.path);
    else if (next.path) localStorage.setItem(localCacheKey, next.path);
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '读取目录失败'), position: 'top', duration: 3000 });
  } finally {
    loading.value = false;
  }
}

/** 名称单击：目录进入；远端文件预览，本地文件交给系统默认程序打开 */
async function activate(e: FtpEntry) {
  if (e.isDir) {
    await load(e.path);
    return;
  }
  if (isRemote()) {
    emit('preview', e);
    return;
  }
  try {
    await GoTools.DiskOpenFile(e.path);
  } catch (err: any) {
    Message.error({ content: String(err?.message || err || '打开文件失败'), position: 'top', duration: 3000 });
  }
}

/* ==================== 上传 / 下载 ==================== */

async function targetUploadDir(): Promise<string> {
  if (isRemote()) return curPath.value;
  return props.uploadDir || '';
}

async function startUpload(files: string[], dir: string) {
  if (!dir) {
    Message.warning({ content: '请先在右侧打开远端目标目录', position: 'top', duration: 3000 });
    return;
  }
  try {
    const list = await GoTools.FtpUpload(props.sessionId, files, dir);
    if (list?.length) {
      Message.info({ content: `已加入上传队列（${list.length} 项）`, position: 'top', duration: 2000 });
      emit('transferred');
    }
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '上传失败'), position: 'top', duration: 3000 });
  }
}

async function pickUpload() {
  const dir = await targetUploadDir();
  if (!dir) {
    Message.warning({ content: '请先在右侧打开远端目标目录', position: 'top', duration: 3000 });
    return;
  }
  try {
    const files = await GoTools.FtpChooseLocalFiles();
    if (files?.length) await startUpload(files, dir);
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '选择文件失败'), position: 'top', duration: 3000 });
  }
}

async function uploadOne(entry: FtpEntry) {
  const dir = await targetUploadDir();
  await startUpload([entry.path], dir);
}

async function download(entry: FtpEntry) {
  try {
    let dir = props.downloadDir;
    if (!dir) dir = await GoTools.FtpChooseLocalFolder('选择下载到');
    if (!dir) return;
    const list = await GoTools.FtpDownload(props.sessionId, [entry.path], dir);
    if (list?.length) {
      Message.info({ content: `已加入下载队列（${list.length} 项）`, position: 'top', duration: 2000 });
      emit('transferred');
    }
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '下载失败'), position: 'top', duration: 3000 });
  }
}

/* ==================== 新建 / 重命名 / 删除 ==================== */

const nameVisible = ref(false);
const nameMode = ref<'mkdir' | 'rename'>('mkdir');
const nameValue = ref('');
const nameBusy = ref(false);
const renameTarget = ref<FtpEntry | null>(null);

function openMkdir() {
  nameMode.value = 'mkdir';
  nameValue.value = '';
  renameTarget.value = null;
  nameVisible.value = true;
}

function openRename(entry: FtpEntry) {
  nameMode.value = 'rename';
  nameValue.value = entry.name;
  renameTarget.value = entry;
  nameVisible.value = true;
}

async function submitName() {
  const name = nameValue.value.trim();
  if (!name) return;
  nameBusy.value = true;
  try {
    if (nameMode.value === 'mkdir') {
      if (isRemote()) await GoTools.FtpMkdir(props.sessionId, curPath.value, name);
      else await GoTools.DiskMkdir(curPath.value, name);
      Message.success({ content: '目录已创建', position: 'top', duration: 2000 });
    } else if (renameTarget.value) {
      if (isRemote()) await GoTools.FtpRename(props.sessionId, renameTarget.value.path, name);
      else await GoTools.DiskRename(renameTarget.value.path, name);
      Message.success({ content: '已重命名', position: 'top', duration: 2000 });
    }
    nameVisible.value = false;
    await load(curPath.value);
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '操作失败'), position: 'top', duration: 3000 });
  } finally {
    nameBusy.value = false;
  }
}

function remove(entry: FtpEntry) {
  const tip = isRemote()
    ? '远端删除不可恢复，确定删除？'
    : '将移动到回收站，确定删除？';
  Modal.warning({
    title: `删除「${entry.name}」`,
    content: tip,
    hideCancel: false,
    onOk: async () => {
      try {
        if (isRemote()) await GoTools.FtpRemove(props.sessionId, [entry.path]);
        else await GoTools.DiskDelete([entry.path], false);
        Message.success({ content: '已删除', position: 'top', duration: 2000 });
        await load(curPath.value);
      } catch (e: any) {
        Message.error({ content: String(e?.message || e || '删除失败'), position: 'top', duration: 3000 });
      }
    },
  });
}

/* ==================== 会话切换 ==================== */

watch(
  () => props.sessionId,
  (id) => {
    if (!isRemote()) return;
    if (!id) {
      listing.value = null;
      curPath.value = '';
      pathInput.value = '';
      return;
    }
    void load(props.startPath || '');
  },
);

onMounted(() => {
  if (isRemote()) {
    if (props.sessionId) void load(props.startPath || '');
    return;
  }
  const cached = localStorage.getItem(localCacheKey) || '';
  void load(cached);
});

defineExpose({ refresh: () => load(curPath.value) });
</script>

<style scoped>
.file-pane {
  height: 100%;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.pane-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 6px 8px;
  border-bottom: 1px solid var(--color-border-1);
}

.pane-title {
  display: flex;
  align-items: baseline;
  gap: 8px;
  min-width: 0;
}

.pane-label {
  font-size: 13px;
  font-weight: 600;
}

.pane-stat,
.dim {
  color: var(--color-text-3);
  font-size: 12px;
}

.pane-path {
  padding: 6px 8px;
  border-bottom: 1px solid var(--color-border-1);
}

.path-input {
  width: 100%;
}

.pane-body {
  flex: 1;
  min-height: 0;
  overflow: auto;
}

.pane-empty {
  padding: 18px 0;
}

.pane-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 5px 8px;
  border-top: 1px solid var(--color-border-1);
}

.pane-foot .dim {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.name-cell {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.name-icon {
  color: var(--color-text-3);
  flex: none;
}

.name-icon.dir {
  color: rgb(var(--primary-6));
}

.name-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  cursor: pointer;
}

.name-text.dir {
  font-weight: 500;
}

.name-text:hover {
  color: rgb(var(--primary-6));
}
</style>
