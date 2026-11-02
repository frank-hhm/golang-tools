<template>
  <a-drawer
    :visible="visible"
    :width="480"
    title="版本历史"
    unmount-on-close
    @update:visible="(v: boolean) => emit('update:visible', v)"
    @cancel="close"
  >
    <div class="head" v-if="item">
      <div class="name">{{ item.name }}</div>
      <div class="sub mono">{{ item.path }}</div>
    </div>

    <div class="snapbar">
      <a-input v-model="note" placeholder="本次快照备注（可留空）" size="small" />
      <a-button type="primary" size="small" :loading="snapshotting" @click="snapshot">
        <template #icon><icon-save /></template>存为新版
      </a-button>
    </div>

    <a-spin :loading="loading" class="list-wrap">
      <a-empty v-if="!versions.length" description="尚无版本快照" />
      <a-timeline v-else reverse>
        <a-timeline-item
          v-for="v in versions"
          :key="v.n"
          :dot-color="v.n === latest ? 'green' : 'arcoblue'"
        >
          <div class="vrow">
            <div class="vmain">
              <span class="vno">v{{ v.n }}</span>
              <a-tag v-if="v.n === latest" size="small" color="green">最新</a-tag>
              <span class="vsize">{{ fmtSize(v.size) }}</span>
            </div>
            <div class="vnote" v-if="v.note">{{ v.note }}</div>
            <div class="vtime mono">{{ fmtTime(v.createdAt) }} · 源改动 {{ fmtTime(v.srcMtime) }}</div>
            <div class="vsha mono" :title="v.sha256">sha256 {{ v.sha256.slice(0, 12) }}…</div>
            <div class="vops">
              <a-link :disabled="v.n === latest" @click="restore(v)">还原到此版</a-link>
            </div>
          </div>
        </a-timeline-item>
      </a-timeline>
    </a-spin>

    <a-alert type="info" class="foot-tip">
      还原会先把「当前内容」自动存为一版，再写回原路径；不会丢未保存的改动。
    </a-alert>
  </a-drawer>
</template>

<script setup lang="ts">
/** 版本抽屉：列出某条目全部快照，支持手动存版与还原。还原前后端自动存当前版。 */
import { computed, getCurrentInstance, ref, watch } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { errText, fmtTime, type DocItem, type Version } from "../types";

const props = defineProps<{ visible: boolean; item: DocItem | null }>();
const emit = defineEmits<{
  (e: "update:visible", v: boolean): void;
  (e: "changed"): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const versions = ref<Version[]>([]);
const loading = ref(false);
const snapshotting = ref(false);
const note = ref("");

const latest = computed(() => (versions.value.length ? versions.value[versions.value.length - 1].n : 0));

watch(
  () => props.visible,
  (v) => {
    if (v) {
      note.value = "";
      void load();
    }
  },
);

async function load() {
  if (!props.item) return;
  loading.value = true;
  try {
    const list = await GoTools?.DocSpaceVersions(props.item.id);
    versions.value = (list as Version[]) ?? [];
  } catch (e: any) {
    Message.error(`载入版本历史失败：${errText(e)}`);
  } finally {
    loading.value = false;
  }
}

async function snapshot() {
  if (!props.item) return;
  snapshotting.value = true;
  try {
    const v = await GoTools?.DocSpaceSnapshot(props.item.id, note.value.trim());
    Message.success(`已存为 v${(v as Version)?.n}`);
    note.value = "";
    await load();
    emit("changed");
  } catch (e: any) {
    Message.error(`存快照失败：${errText(e)}`);
  } finally {
    snapshotting.value = false;
  }
}

function restore(v: Version) {
  if (!props.item) return;
  Modal.warning({
    title: `还原到 v${v.n}`,
    content: `将用 v${v.n} 的快照覆盖原路径当前内容（覆盖前会自动先存一版当前内容）。确认继续？`,
    okText: "还原",
    onOk: async () => {
      try {
        await GoTools?.DocSpaceRestore(props.item!.id, v.n);
        Message.success(`已还原到 v${v.n}`);
        await load();
        emit("changed");
      } catch (e: any) {
        Message.error(`还原失败：${errText(e)}`);
      }
    },
  });
}

function close() {
  emit("update:visible", false);
}

function fmtSize(n: number): string {
  if (!n && n !== 0) return "--";
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}
</script>

<style scoped lang="less">
.head {
  margin-bottom: 12px;
}
.head .name {
  font-weight: 600;
  font-size: 15px;
}
.head .sub {
  color: var(--color-text-3);
  font-size: 12px;
  word-break: break-all;
}
.snapbar {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 14px;
}
.list-wrap {
  display: block;
  min-height: 80px;
}
.vrow .vmain {
  display: flex;
  align-items: center;
  gap: 8px;
}
.vno {
  font-weight: 600;
}
.vsize {
  color: var(--color-text-3);
  font-size: 12px;
}
.vnote {
  margin-top: 2px;
  font-size: 13px;
}
.vtime {
  color: var(--color-text-3);
  font-size: 12px;
  margin-top: 2px;
}
.vsha {
  color: var(--color-text-3);
  font-size: 11px;
  margin-top: 2px;
}
.vops {
  margin-top: 4px;
}
.foot-tip {
  margin-top: 16px;
}
.mono {
  font-family: Consolas, Menlo, monospace;
}
</style>
