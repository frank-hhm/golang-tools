<template>
  <a-modal :visible="visible" title="远端（Remote）" width="820px" :footer="false" :esc-to-close="true"
    @update:visible="(v: boolean) => emit('update:visible', v)">
    <div class="rm-bar">
      <a-input v-model="newName" size="small" placeholder="名称，如 origin" allow-clear style="width: 160px" />
      <a-input v-model="newURL" size="small" placeholder="URL，如 https://github.com/user/repo.git" allow-clear
        style="flex: 1" @press-enter="doAdd" />
      <a-button size="small" type="primary" :disabled="!newName.trim() || !newURL.trim()" :loading="adding"
        @click="doAdd">
        <template #icon><icon-plus /></template>添加远端
      </a-button>
      <a-button size="small" :loading="loading" @click="load">
        <template #icon><icon-refresh /></template>刷新
      </a-button>
    </div>

    <a-table :data="list" :loading="loading" size="small" row-key="name" :pagination="false" class="rm-table"
      :scroll="{ y: 360 }">
      <template #columns>
        <a-table-column title="名称" data-index="name" :width="110">
          <template #cell="{ record }"><span class="rm-name">{{ record.name }}</span></template>
        </a-table-column>
        <a-table-column title="抓取地址（fetch）" data-index="fetchUrl" ellipsis tooltip>
          <template #cell="{ record }">
            <div v-if="editingName === record.name" class="rm-edit">
              <a-input v-model="editValue" size="mini" placeholder="新抓取地址" style="flex: 1"
                @press-enter="saveURL(record.name)" />
              <a-button size="mini" type="primary" :loading="saving" @click="saveURL(record.name)">保存</a-button>
              <a-button size="mini" @click="editingName = ''">取消</a-button>
            </div>
            <span v-else>{{ record.fetchUrl || '（空）' }}</span>
          </template>
        </a-table-column>
        <a-table-column title="推送地址（push）" data-index="pushUrl" ellipsis tooltip />
        <a-table-column title="操作" :width="160">
          <template #cell="{ record }">
            <a-space :size="4">
              <a-button v-if="editingName !== record.name" size="mini" type="secondary"
                @click="startEdit(record)">改地址</a-button>
              <a-button size="mini" type="secondary" status="danger" @click="doRemove(record.name)">删除</a-button>
            </a-space>
          </template>
        </a-table-column>
      </template>
      <template #empty>
        <div class="rm-empty">暂无远端（本地仓库常见；添加后即可推送）</div>
      </template>
    </a-table>
    <div class="rm-tip">「改地址」只改抓取地址；如需单独设推送地址请在命令行用 git remote set-url --push。</div>
  </a-modal>
</template>

<script setup lang="ts">
/**
 * RemoteModal：git remote 的列/增/删/改地址。删除为破坏操作，二次确认。
 * 凭据仍交系统 credential helper，这里只管理名字与 URL。
 */
import { getCurrentInstance, ref, watch } from 'vue';
import { Message, Modal } from '@arco-design/web-vue';
import type { Remote } from 'bindings/Tools/tools/gitcmd/index';

const props = defineProps<{ visible: boolean; repo: string }>();
const emit = defineEmits<{
  (e: 'update:visible', v: boolean): void;
  (e: 'changed'): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const list = ref<Remote[]>([]);
const loading = ref(false);
const adding = ref(false);
const saving = ref(false);
const newName = ref('');
const newURL = ref('');
// 行内改地址：一次只编辑一行
const editingName = ref('');
const editValue = ref('');

async function load() {
  if (!props.repo) return;
  loading.value = true;
  try {
    list.value = (await GoTools.GitRemoteList(props.repo)) || [];
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '读取远端失败'), position: 'top', duration: 3000 });
  } finally {
    loading.value = false;
  }
}

watch(() => props.visible, (v) => { if (v) load(); });

async function doAdd() {
  const name = newName.value.trim();
  const url = newURL.value.trim();
  if (!name || !url) return;
  adding.value = true;
  try {
    await GoTools.GitRemoteAdd(props.repo, name, url);
    newName.value = '';
    newURL.value = '';
    Message.success({ content: `已添加远端 ${name}`, position: 'top', duration: 1500 });
    emit('changed');
    load();
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '添加远端失败'), position: 'top', duration: 4000 });
  } finally {
    adding.value = false;
  }
}

function startEdit(record: Remote) {
  editingName.value = record.name;
  editValue.value = record.fetchUrl || '';
}

async function saveURL(name: string) {
  const v = editValue.value.trim();
  if (!v) {
    Message.warning({ content: '地址为空，已取消', position: 'top', duration: 2000 });
    return;
  }
  saving.value = true;
  try {
    await GoTools.GitRemoteSetURL(props.repo, name, v, false);
    Message.success({ content: `已更新 ${name} 地址`, position: 'top', duration: 1500 });
    editingName.value = '';
    emit('changed');
    load();
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '改地址失败'), position: 'top', duration: 4000 });
  } finally {
    saving.value = false;
  }
}

function doRemove(name: string) {
  Modal.warning({
    title: `删除远端 ${name}`,
    content: '删除远端不会删除其服务器上的内容，但本地将失去该 remote 与相关追踪引用（需重新 add 才能推送）。确认删除？',
    okText: '删除',
    hideCancel: false,
    onOk: async () => {
      try {
        await GoTools.GitRemoteRemove(props.repo, name);
        Message.success({ content: `已删除远端 ${name}`, position: 'top', duration: 1500 });
        emit('changed');
        load();
      } catch (e: any) {
        Message.error({ content: String(e?.message || e || '删除失败'), position: 'top', duration: 4000 });
      }
    },
  });
}
</script>

<style scoped>
.rm-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
}
.rm-table :deep(.arco-table-cell) {
  font-size: 12px;
}
.rm-name {
  font-family: var(--code-font-family);
}
.rm-empty {
  padding: 20px;
  color: var(--color-text-3);
  font-size: 12px;
}
.rm-tip {
  margin-top: 8px;
  color: var(--color-text-3);
  font-size: 12px;
}
.rm-edit {
  display: flex;
  align-items: center;
  gap: 4px;
}
</style>
