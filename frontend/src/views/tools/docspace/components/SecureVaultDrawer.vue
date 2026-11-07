<template>
  <a-drawer
    :visible="visible"
    :width="560"
    title="加密保险库"
    unmount-on-close
    @update:visible="(v: boolean) => emit('update:visible', v)"
    @cancel="close"
  >
    <div class="bar">
      <a-button size="small" :loading="loading" @click="load">
        <template #icon><icon-refresh /></template>刷新
      </a-button>
    </div>

    <a-alert type="warning" class="tip">
      加密仅支持「本机 local」文件；加密成功后原明文会被删除，只保留密文。
      口令<strong>不落盘、不可找回</strong>——忘记口令即永久丢失内容。解锁只是临时明文视图，关闭本页会自动上锁删除。
    </a-alert>

    <a-spin :loading="loading" class="list-wrap">
      <a-empty v-if="!entries.length" description="保险库为空" />
      <a-list v-else :bordered="false">
        <a-list-item v-for="e in entries" :key="e.id">
          <div class="erow">
            <div class="emain">
              <div class="ename">{{ baseName(e.path) }}</div>
              <div class="epath mono" :title="e.path">{{ e.path }}</div>
              <div class="etime">
                加密于 {{ fmtTime(e.createdAt) }}
                <a-tag v-if="unlocked[e.id]" size="small" color="orangered" class="lock-tag">已解锁</a-tag>
              </div>
            </div>
            <a-space class="eops" wrap :size="6">
              <a-link v-if="!unlocked[e.id]" @click="askUnlock(e)">解锁预览</a-link>
              <a-link v-else status="warning" @click="lock(e)">上锁</a-link>
              <a-link @click="askExtract(e)">取回明文</a-link>
              <a-link @click="askChange(e)">改口令</a-link>
              <a-popconfirm content="彻底删除该密文条目，连原明文早已删除，绝不可恢复。确认？" type="warning" @ok="remove(e)">
                <a-link status="danger">删除</a-link>
              </a-popconfirm>
            </a-space>
          </div>
        </a-list-item>
      </a-list>
    </a-spin>

    <!-- 口令/操作弹窗：解锁、改密、取回共用 -->
    <a-modal
      v-model:visible="prompt.open"
      :title="promptTitle"
      :ok-loading="prompt.busy"
      :mask-closable="false"
      @ok="submitPrompt"
      @cancel="prompt.open = false"
    >
      <a-form :model="prompt" layout="vertical">
        <a-form-item v-if="prompt.mode === 'change'" field="pw" label="旧口令">
          <a-input-password v-model="prompt.pw" placeholder="当前口令" />
        </a-form-item>
        <a-form-item v-else field="pw" label="口令">
          <a-input-password v-model="prompt.pw" placeholder="解密口令" />
        </a-form-item>
        <a-form-item v-if="prompt.mode === 'change'" field="pw2" label="新口令">
          <a-input-password v-model="prompt.pw2" placeholder="至少一位，妥善保管" />
        </a-form-item>
        <a-form-item v-if="prompt.mode === 'extract'" field="dest" label="写到本机路径">
          <a-input v-model="prompt.dest" placeholder="明文永久写出的绝对路径（已存在则不覆盖）" />
        </a-form-item>
      </a-form>
    </a-modal>
  </a-drawer>
</template>

<script setup lang="ts">
/**
 * 加密保险库抽屉：列出密文条目，支持解锁临时预览 / 上锁删除明文 / 取回明文 / 改口令 / 彻底删除。
 * 全部经 $GoTools 的 SecStore* 代理调用（跨平台存在）；解锁走 openToolWindow 开查看器看明文临时视图。
 */
import { computed, getCurrentInstance, reactive, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import { useToolWindow } from "@/views/index/useToolWindow";
import { baseName, errText, fmtTime, type SecureEntry } from "../types";

const props = defineProps<{ visible: boolean }>();
const emit = defineEmits<{
  (e: "update:visible", v: boolean): void;
  (e: "changed"): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const { openToolWindow } = useToolWindow();

const entries = ref<SecureEntry[]>([]);
const loading = ref(false);
// 已解锁条目的临时明文路径：id -> tempPath
const unlocked = reactive<Record<string, string>>({});

const prompt = reactive({
  open: false,
  busy: false,
  mode: "" as "" | "unlock" | "change" | "extract",
  entry: null as SecureEntry | null,
  pw: "",
  pw2: "",
  dest: "",
});

const promptTitle = computed(
  () =>
    prompt.mode === "unlock"
      ? "解锁预览（临时明文视图）"
      : prompt.mode === "change"
        ? "修改口令"
        : "取回明文",
);

watch(
  () => props.visible,
  (v) => {
    if (v) void load();
    else void lockAll(); // 关闭抽屉即上锁，删掉所有已解出的明文
  },
);

async function load() {
  loading.value = true;
  try {
    const list = await GoTools?.SecStoreList();
    entries.value = (list as SecureEntry[]) ?? [];
  } catch (e: any) {
    Message.error(`载入保险库失败：${errText(e)}`);
  } finally {
    loading.value = false;
  }
}

function openViewer(tempPath: string) {
  openToolWindow(`/tools/viewer?path=${encodeURIComponent(tempPath)}`, "文件查看器", { single: true });
  try {
    Events.Emit("viewer:open-path", { path: tempPath });
  } catch {
    /* 非 Wails 环境忽略 */
  }
}

function askUnlock(e: SecureEntry) {
  prompt.mode = "unlock";
  prompt.entry = e;
  prompt.pw = "";
  prompt.pw2 = "";
  prompt.dest = "";
  prompt.open = true;
}
function askChange(e: SecureEntry) {
  prompt.mode = "change";
  prompt.entry = e;
  prompt.pw = "";
  prompt.pw2 = "";
  prompt.dest = "";
  prompt.open = true;
}
function askExtract(e: SecureEntry) {
  prompt.mode = "extract";
  prompt.entry = e;
  prompt.pw = "";
  prompt.pw2 = "";
  prompt.dest = e.path; // 默认写回原路径
  prompt.open = true;
}

async function submitPrompt() {
  const e = prompt.entry;
  if (!e) return;
  if (!prompt.pw) {
    Message.warning("口令不能为空");
    return;
  }
  prompt.busy = true;
  try {
    if (prompt.mode === "unlock") {
      const temp = await GoTools?.SecStoreUnlock(e.id, prompt.pw);
      if (!temp) throw new Error("解锁未返回明文路径");
      unlocked[e.id] = String(temp);
      prompt.pw = "";
      prompt.open = false;
      openViewer(String(temp));
      Message.success("已解锁，明文仅临时存在，用完请上锁");
    } else if (prompt.mode === "change") {
      if (!prompt.pw2) {
        Message.warning("新口令不能为空");
        return;
      }
      await GoTools?.SecStoreChangePassword(e.id, prompt.pw, prompt.pw2);
      // 改密后原解锁的明文视图已不代表最新密文，直接上锁
      if (unlocked[e.id]) await lockById(e.id);
      Message.success("已修改口令");
      prompt.open = false;
      await load();
    } else if (prompt.mode === "extract") {
      const dest = prompt.dest.trim();
      if (!dest) {
        Message.warning("请填写目标路径");
        return;
      }
      await GoTools?.SecStoreExtractTo(e.id, prompt.pw, dest);
      Message.success(`已取回明文：${dest}`);
      prompt.open = false;
    }
  } catch (err: any) {
    Message.error(`操作失败：${errText(err)}`);
  } finally {
    prompt.busy = false;
  }
}

async function lockById(id: string) {
  try {
    await GoTools?.SecStoreLock(id);
  } catch {
    /* 尽力删除，失败忽略 */
  }
  delete unlocked[id];
}

async function lock(e: SecureEntry) {
  await lockById(e.id);
  Message.success("已上锁，明文视图已删除");
}

async function lockAll() {
  const ids = Object.keys(unlocked);
  for (const id of ids) await lockById(id);
}

async function remove(e: SecureEntry) {
  try {
    await GoTools?.SecStoreRemove(e.id);
    delete unlocked[e.id];
    Message.success("已彻底删除密文条目");
    await load();
    emit("changed");
  } catch (err: any) {
    Message.error(`删除失败：${errText(err)}`);
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
.emain {
  min-width: 0;
}
.ename {
  font-weight: 600;
}
.epath {
  color: var(--color-text-3);
  font-size: 12px;
  word-break: break-all;
  max-width: 320px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.etime {
  color: var(--color-text-3);
  font-size: 12px;
  margin-top: 2px;
}
.lock-tag {
  margin-left: 6px;
}
.eops {
  flex-shrink: 0;
}
.mono {
  font-family: var(--code-font-family);
}
</style>
