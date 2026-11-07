<template>
  <a-drawer
    :visible="visible"
    :width="640"
    title="备份计划"
    unmount-on-close
    @update:visible="(v: boolean) => emit('update:visible', v)"
    @cancel="close"
  >
    <div class="bar">
      <a-button size="small" type="primary" @click="openAdd">
        <template #icon><icon-plus /></template>新建计划
      </a-button>
      <a-button size="small" :loading="loading" @click="load">
        <template #icon><icon-refresh /></template>刷新
      </a-button>
      <a-space :size="6" class="autosw">
        <a-switch :model-value="autoOn" size="small" :loading="autoLoading" @change="toggleAuto" />
        <span class="autosw-txt">自动调度</span>
        <a-tooltip content="全局总闸：打开后，启用且设了间隔的计划会由后台协程自动到点备份（一分钟粒度轮询）；关掉则只认手动。">
          <icon-info-circle-fill />
        </a-tooltip>
      </a-space>
    </div>

    <a-alert type="info" class="tip">
      备份为 <strong>纯增量拷贝</strong>：按 sha256 跳过未变化文件，只新增/覆盖，<strong>绝不删除目标</strong>里已有的多余文件。
      「立即备份」在后台执行，进度可在任务中心查看；自动调度需上方总闸打开且计划设了间隔。
    </a-alert>

    <a-table :data="plans" :loading="loading" :pagination="false" row-key="id" size="small">
      <template #columns>
        <a-table-column title="名称" data-index="name" :width="120" :ellipsis="true" :tooltip="true" />
        <a-table-column title="源 → 目标" :ellipsis="true">
          <template #cell="{ record }">
            <div class="mono pathline" :title="`${record.src} → ${record.dst}`">
              {{ record.src }}<span class="arrow">→</span>{{ record.dst }}
            </div>
          </template>
        </a-table-column>
        <a-table-column title="自动间隔" :width="110">
          <template #cell="{ record }">
            <a-select
              :model-value="record.intervalSec || 0"
              size="mini"
              :bordered="false"
              @change="(v: any) => setInterval(record, Number(v))"
            >
              <a-option :value="0">仅手动</a-option>
              <a-option :value="3600">1 小时</a-option>
              <a-option :value="21600">6 小时</a-option>
              <a-option :value="86400">1 天</a-option>
              <a-option :value="604800">7 天</a-option>
            </a-select>
          </template>
        </a-table-column>
        <a-table-column title="上次结果" :width="150">
          <template #cell="{ record }">
            <a-tag v-if="!record.lastRunAt" size="small" color="gray">未跑过</a-tag>
            <a-tag v-else-if="record.lastStatus === 'ok'" size="small" color="green">
              成 {{ record.copied }} / 跳 {{ record.skipped }}
            </a-tag>
            <a-tag v-else size="small" color="red" :title="record.lastStatus">失败</a-tag>
            <div class="etime">{{ fmtTime(record.lastRunAt) }}</div>
          </template>
        </a-table-column>
        <a-table-column title="操作" :width="200" fixed="right">
          <template #cell="{ record }">
            <a-space>
              <a-link @click="runNow(record)">立即备份</a-link>
              <a-switch
                :model-value="record.enabled"
                size="small"
                @change="(v: any) => toggle(record, !!v)"
              />
              <a-popconfirm content="仅删除计划登记，不动已备份出去的实体。确认删除？" type="warning" @ok="del(record)">
                <a-link status="danger">删除</a-link>
              </a-popconfirm>
            </a-space>
          </template>
        </a-table-column>
      </template>
      <template #empty>
        <a-empty description="暂无备份计划" />
      </template>
    </a-table>

    <!-- 新建计划 -->
    <a-modal v-model:visible="addModal.open" title="新建备份计划" :ok-loading="addModal.saving" @ok="submitAdd">
      <a-form :model="addForm" layout="vertical">
        <a-form-item field="name" label="名称">
          <a-input v-model="addForm.name" placeholder="留空取源目录名" />
        </a-form-item>
        <a-form-item field="src" label="源目录（须已存在）">
          <a-input v-model="addForm.src" placeholder="如 D:\docs 或 /srv/docs" />
        </a-form-item>
        <a-form-item field="dst" label="目标目录（可暂不存在，跑时创建）">
          <a-input v-model="addForm.dst" placeholder="如 E:\backup\docs 或 /mnt/nas/docs" />
        </a-form-item>
      </a-form>
    </a-modal>
  </a-drawer>
</template>

<script setup lang="ts">
/**
 * 备份计划抽屉（P4 + 自动调度补做）：列表 / 新建 / 删除 / 启用位 / 间隔 / 立即备份 / 全局总闸。
 * 执行是异步的：BackupRunNow 立即返回，进度经 backup:progress 事件进任务中心，
 * 完成后点「刷新」查看上次结果。删除只删登记，不动已备份实体。
 * prefill：磁盘页「备份设为源/目标」跨页入口带进来的路径，打开新建弹窗时预填。
 */
import { getCurrentInstance, reactive, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { errText, fmtTime, type BackupEntry } from "../types";

const props = defineProps<{ visible: boolean; prefill?: { src?: string; dst?: string } }>();
const emit = defineEmits<{
  (e: "update:visible", v: boolean): void;
  (e: "changed"): void;
  (e: "prefill-consumed"): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const plans = ref<BackupEntry[]>([]);
const loading = ref(false);
const autoOn = ref(false);
const autoLoading = ref(false);

async function loadAuto() {
  try {
    const s = await GoTools?.DocSpaceSettings();
    autoOn.value = !!s?.backupAuto;
  } catch {
    autoOn.value = false;
  }
}

async function toggleAuto(v: any) {
  autoLoading.value = true;
  try {
    await GoTools?.SetDocSpaceBackupAuto(!!v);
    autoOn.value = !!v;
    Message.success(v ? "自动调度已开启：启用且设了间隔的计划会到点自动备份" : "自动调度已关闭：只剩手动备份");
  } catch (e: any) {
    Message.error(`切换失败：${errText(e)}`);
  } finally {
    autoLoading.value = false;
  }
}

watch(
  () => props.visible,
  (v) => {
    if (v) {
      void load();
      void loadAuto();
    }
  },
);

async function load() {
  loading.value = true;
  try {
    const list = await GoTools?.BackupList();
    plans.value = (list as BackupEntry[]) ?? [];
  } catch (e: any) {
    Message.error(`载入备份计划失败：${errText(e)}`);
  } finally {
    loading.value = false;
  }
}

const addModal = reactive({ open: false, saving: false });
const addForm = reactive({ name: "", src: "", dst: "" });
function openAdd() {
  addForm.name = "";
  // 跨页预填（磁盘页「备份设为源/目标」）：有 prefill 就带上，用完即清，避免下次新建串旧值
  addForm.src = props.prefill?.src ?? "";
  addForm.dst = props.prefill?.dst ?? "";
  if (props.prefill) emit("prefill-consumed");
  addModal.open = true;
}
async function submitAdd() {
  if (!addForm.src.trim() || !addForm.dst.trim()) {
    Message.warning("源目录与目标目录都不能为空");
    return;
  }
  addModal.saving = true;
  try {
    await GoTools?.BackupAdd(addForm.name.trim(), addForm.src.trim(), addForm.dst.trim());
    Message.success("已新建备份计划");
    addModal.open = false;
    await load();
    emit("changed");
  } catch (e: any) {
    Message.error(`新建计划失败：${errText(e)}`);
  } finally {
    addModal.saving = false;
  }
}

async function del(p: BackupEntry) {
  try {
    await GoTools?.BackupRemove(p.id);
    Message.success("已删除计划");
    await load();
    emit("changed");
  } catch (e: any) {
    Message.error(`删除计划失败：${errText(e)}`);
  }
}

async function toggle(p: BackupEntry, on: boolean) {
  try {
    await GoTools?.BackupSetEnabled(p.id, on);
    p.enabled = on;
  } catch (e: any) {
    Message.error(`切换启用位失败：${errText(e)}`);
  }
}

async function setInterval(p: BackupEntry, secs: number) {
  try {
    await GoTools?.BackupSetInterval(p.id, secs);
    p.intervalSec = secs;
    Message.success(secs > 0 ? `「${p.name}」将按间隔自动备份（需总闸打开）` : `「${p.name}」已改为仅手动`);
  } catch (e: any) {
    Message.error(`设置间隔失败：${errText(e)}`);
  }
}

async function runNow(p: BackupEntry) {
  try {
    await GoTools?.BackupRunNow(p.id);
    Message.success(`已启动「${p.name}」备份，进度见任务中心；完成后点刷新查看结果`);
  } catch (e: any) {
    Message.error(`启动备份失败：${errText(e)}`);
  }
}

function close() {
  emit("update:visible", false);
}
</script>

<style scoped lang="less">
.bar {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}
.autosw {
  margin-left: auto;
}
.autosw-txt {
  font-size: 13px;
  color: var(--color-text-2);
}
.tip {
  margin-bottom: 12px;
}
.mono {
  font-family: var(--code-font-family);
}
.pathline {
  font-size: 12px;
  color: var(--color-text-3);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.pathline .arrow {
  margin: 0 6px;
  color: var(--color-text-4);
}
.etime {
  color: var(--color-text-3);
  font-size: 12px;
  margin-top: 2px;
}
</style>
