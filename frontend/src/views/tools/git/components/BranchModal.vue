<template>
  <a-modal :visible="visible" title="分支管理" width="720px" :footer="false" :esc-to-close="true"
    @update:visible="(v: boolean) => emit('update:visible', v)">
    <div class="bm-bar">
      <a-input v-model="newName" size="small" placeholder="新分支名" allow-clear style="width: 200px"
        @press-enter="doCreate" />
      <a-checkbox v-model="createCheckout" size="small">创建后切换</a-checkbox>
      <a-button size="small" type="primary" :disabled="!newName.trim()" :loading="creating" @click="doCreate">
        <template #icon><icon-plus /></template>创建分支
      </a-button>
      <a-button size="small" :loading="loading" @click="load">
        <template #icon><icon-refresh /></template>刷新
      </a-button>
    </div>

    <a-table :data="branches" :loading="loading" size="small" row-key="name" :pagination="false"
      class="bm-table" :scroll="{ y: 360 }">
      <template #columns>
        <a-table-column title="分支" data-index="name" :width="200">
          <template #cell="{ record }">
            <a-space :size="6">
              <icon-check-circle v-if="record.current" class="bm-cur" />
              <span class="bm-name">{{ record.name }}</span>
              <a-tag v-if="record.remote" size="small" color="gray">远程</a-tag>
            </a-space>
          </template>
        </a-table-column>
        <a-table-column title="追踪" data-index="upstream" :width="150" ellipsis tooltip />
        <a-table-column title="领先/落后" :width="90">
          <template #cell="{ record }">
            <span v-if="record.remote" class="bm-ab-na">—</span>
            <a-space v-else :size="4">
              <a-tag v-if="record.ahead" size="small" color="green">+{{ record.ahead }}</a-tag>
              <a-tag v-if="record.behind" size="small" color="orangered">-{{ record.behind }}</a-tag>
              <span v-if="!record.ahead && !record.behind" class="bm-ab-na">=</span>
            </a-space>
          </template>
        </a-table-column>
        <a-table-column title="最新提交" data-index="subject" ellipsis tooltip />
        <a-table-column title="操作" :width="130">
          <template #cell="{ record }">
            <a-space :size="4">
              <a-button v-if="!record.current && !record.remote" size="mini" type="secondary"
                @click="doCheckout(record.name)">切换</a-button>
              <a-button v-if="!record.remote" size="mini" type="secondary" status="danger"
                @click="doDelete(record.name)">删除</a-button>
            </a-space>
          </template>
        </a-table-column>
      </template>
    </a-table>
  </a-modal>
</template>

<script setup lang="ts">
/**
 * BranchModal：分支列表（本地+远程）与 切换/创建/删除。
 * 删除未合并分支由后端 git branch -d 先试，失败（未合并）时问用户是否强删。
 */
import { getCurrentInstance, ref, watch } from 'vue';
import { Message, Modal } from '@arco-design/web-vue';
import type { Branch } from 'bindings/Tools/tools/gitcmd/index';

const props = defineProps<{ visible: boolean; repo: string }>();
const emit = defineEmits<{
  (e: 'update:visible', v: boolean): void;
  (e: 'changed'): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const branches = ref<Branch[]>([]);
const loading = ref(false);
const creating = ref(false);
const newName = ref('');
const createCheckout = ref(true);

async function load() {
  if (!props.repo) return;
  loading.value = true;
  try {
    branches.value = (await GoTools.GitBranchList(props.repo)) || [];
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '读取分支失败'), position: 'top', duration: 3000 });
  } finally {
    loading.value = false;
  }
}

watch(() => props.visible, (v) => { if (v) load(); });

async function doCreate() {
  const name = newName.value.trim();
  if (!name) return;
  creating.value = true;
  try {
    await GoTools.GitCreateBranch(props.repo, name, createCheckout.value);
    newName.value = '';
    Message.success({ content: `已创建 ${name}`, position: 'top', duration: 1500 });
    emit('changed');
    load();
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '创建分支失败'), position: 'top', duration: 4000 });
  } finally {
    creating.value = false;
  }
}

async function doCheckout(name: string) {
  try {
    await GoTools.GitCheckout(props.repo, name);
    Message.success({ content: `已切换到 ${name}`, position: 'top', duration: 1500 });
    emit('changed');
    load();
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '切换失败'), position: 'top', duration: 4000 });
  }
}

function doDelete(name: string) {
  Modal.warning({
    title: `删除分支 ${name}`,
    content: '未合并的分支会删除失败；确认后可选择强制删除（改动只能从 reflog 找回）。',
    okText: '删除',
    hideCancel: false,
    onOk: async () => {
      try {
        await GoTools.GitDeleteBranch(props.repo, name, false);
        Message.success({ content: `已删除 ${name}`, position: 'top', duration: 1500 });
        emit('changed');
        load();
      } catch (e: any) {
        const msg = String(e?.message || e);
        if (msg.includes('not fully merged')) {
          Modal.warning({
            title: '分支未合并',
            content: `git 拒绝删除未合并分支：${msg}。强制删除？`,
            okText: '强制删除',
            hideCancel: false,
            onOk: async () => {
              try {
                await GoTools.GitDeleteBranch(props.repo, name, true);
                Message.success({ content: `已强制删除 ${name}`, position: 'top', duration: 1500 });
                emit('changed');
                load();
              } catch (e2: any) {
                Message.error({ content: String(e2?.message || e2 || '删除失败'), position: 'top', duration: 4000 });
              }
            },
          });
        } else {
          Message.error({ content: msg || '删除失败', position: 'top', duration: 4000 });
        }
      }
    },
  });
}
</script>

<style scoped>
.bm-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
}
.bm-table :deep(.arco-table-cell) {
  font-size: 12px;
}
.bm-cur {
  color: rgb(var(--green-6));
}
.bm-name {
  font-family: var(--code-font-family);
}
.bm-ab-na {
  color: var(--color-text-3);
}
</style>
