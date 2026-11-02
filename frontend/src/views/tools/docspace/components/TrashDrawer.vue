<template>
  <a-drawer
    :visible="visible"
    :width="520"
    title="文档回收站"
    unmount-on-close
    @update:visible="(v: boolean) => emit('update:visible', v)"
    @cancel="close"
  >
    <div class="bar">
      <a-button size="small" :loading="loading" @click="load">
        <template #icon><icon-refresh /></template>刷新
      </a-button>
      <a-popconfirm
        content="清空后回收站内所有实体将彻底删除，不可恢复。确认清空？"
        type="warning"
        :disabled="!entries.length"
        @ok="emptyAll"
      >
        <a-button size="small" status="danger" :disabled="!entries.length">
          <template #icon><icon-delete /></template>清空回收站
        </a-button>
      </a-popconfirm>
    </div>

    <a-alert type="warning" class="tip">
      回收站仅收纳「本机 local」源移入的实体；SFTP 远端文件不落入本机回收站。
    </a-alert>

    <a-spin :loading="loading" class="list-wrap">
      <a-empty v-if="!entries.length" description="回收站为空" />
      <a-list v-else :bordered="false">
        <a-list-item v-for="e in entries" :key="e.id">
          <div class="erow">
            <div class="emain">
              <div class="ename">{{ e.name }}</div>
              <div class="epath mono" :title="e.origin">{{ e.origin }}</div>
              <div class="etime">{{ fmtTime(e.deletedAt) }}</div>
            </div>
            <a-space class="eops">
              <a-link @click="restore(e)">还原</a-link>
              <a-popconfirm content="彻底删除该实体，不可恢复。确认？" type="warning" @ok="del(e)">
                <a-link status="danger">彻底删除</a-link>
              </a-popconfirm>
            </a-space>
          </div>
        </a-list-item>
      </a-list>
    </a-spin>
  </a-drawer>
</template>

<script setup lang="ts">
/** 文档回收站抽屉：列出移入的实体，支持还原到原路径 / 彻底删除 / 清空。 */
import { getCurrentInstance, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { errText, fmtTime, type TrashEntry } from "../types";

const props = defineProps<{ visible: boolean }>();
const emit = defineEmits<{
  (e: "update:visible", v: boolean): void;
  (e: "changed"): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const entries = ref<TrashEntry[]>([]);
const loading = ref(false);

watch(
  () => props.visible,
  (v) => {
    if (v) void load();
  },
);

async function load() {
  loading.value = true;
  try {
    const list = await GoTools?.DocSpaceTrash();
    entries.value = (list as TrashEntry[]) ?? [];
  } catch (e: any) {
    Message.error(`载入回收站失败：${errText(e)}`);
  } finally {
    loading.value = false;
  }
}

async function restore(e: TrashEntry) {
  try {
    await GoTools?.DocSpaceRestoreTrash(e.id);
    Message.success(`已还原「${e.name}」`);
    await load();
    emit("changed");
  } catch (err: any) {
    Message.error(`还原失败：${errText(err)}`);
  }
}

async function del(e: TrashEntry) {
  try {
    await GoTools?.DocSpaceDeleteTrash(e.id);
    Message.success("已彻底删除");
    await load();
    emit("changed");
  } catch (err: any) {
    Message.error(`删除失败：${errText(err)}`);
  }
}

async function emptyAll() {
  try {
    await GoTools?.DocSpaceEmptyTrash();
    Message.success("回收站已清空");
    await load();
    emit("changed");
  } catch (e: any) {
    Message.error(`清空失败：${errText(e)}`);
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
.tip {
  margin-bottom: 12px;
}
.list-wrap {
  display: block;
  min-height: 80px;
}
.erow {
  display: flex;
  width: 100%;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.ename {
  font-weight: 600;
}
.epath {
  color: var(--color-text-3);
  font-size: 12px;
  word-break: break-all;
  max-width: 360px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.etime {
  color: var(--color-text-3);
  font-size: 12px;
  margin-top: 2px;
}
.mono {
  font-family: Consolas, Menlo, monospace;
}
</style>
