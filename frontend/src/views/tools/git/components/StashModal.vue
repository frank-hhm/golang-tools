<template>
  <a-modal :visible="visible" title="暂存栈（Stash）" width="760px" :footer="false" :esc-to-close="true"
    @update:visible="(v: boolean) => emit('update:visible', v)">
    <div class="sm-bar">
      <a-input v-model="newMsg" size="small" placeholder="暂存说明（留空则 WIP on <分支>）" allow-clear style="flex: 1"
        @press-enter="doPush" />
      <a-tooltip content="连同「未跟踪」文件一并收走（这些文件当前不在版本控制下，勾选后会被暂存栈带走、工作区消失）">
        <a-checkbox v-model="inclUntracked" size="small">
          <icon-exclamation-circle-fill class="sm-warn-ic" />含未跟踪
        </a-checkbox>
      </a-tooltip>
      <a-button size="small" type="primary" :loading="pushing" @click="doPush">
        <template #icon><icon-save /></template>暂存改动
      </a-button>
      <a-button size="small" :loading="loading" @click="load">
        <template #icon><icon-refresh /></template>刷新
      </a-button>
    </div>

    <a-table :data="list" :loading="loading" size="small" row-key="index" :pagination="false" class="sm-table"
      :scroll="{ y: 380 }">
      <template #columns>
        <a-table-column title="#" data-index="index" :width="46" />
        <a-table-column title="说明" data-index="message" ellipsis tooltip />
        <a-table-column title="时间" data-index="date" :width="180" ellipsis />
        <a-table-column title="操作" :width="190">
          <template #cell="{ record }">
            <a-space :size="4">
              <a-button size="mini" type="secondary" @click="doApply(record.index)">应用</a-button>
              <a-button size="mini" type="secondary" @click="doPop(record.index)">弹栈</a-button>
              <a-button size="mini" type="secondary" status="danger" @click="doDrop(record.index)">丢弃</a-button>
            </a-space>
          </template>
        </a-table-column>
      </template>
      <template #empty>
        <div class="sm-empty">暂无暂存记录</div>
      </template>
    </a-table>
    <div class="sm-tip">
      「应用」保留栈记录、「弹栈」应用后出栈；二者都可能与工作区现存改动冲突，失败时请先处理工作区再试。
    </div>
  </a-modal>
</template>

<script setup lang="ts">
/**
 * StashModal：git stash 栈的列/暂存/应用/弹栈/丢弃。
 * 暂存可勾选「含未跟踪」，会一并收走未跟踪文件（UI 已明确提示）。丢弃为破坏操作，二次确认。
 * 任何改动后 emit('changed') 让父页刷新状态（工作区随 apply/pop/drop 变化）。
 */
import { getCurrentInstance, ref, watch } from 'vue';
import { Message, Modal } from '@arco-design/web-vue';
import type { StashEntry } from 'bindings/Tools/tools/gitcmd/index';

const props = defineProps<{ visible: boolean; repo: string }>();
const emit = defineEmits<{
  (e: 'update:visible', v: boolean): void;
  (e: 'changed'): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const list = ref<StashEntry[]>([]);
const loading = ref(false);
const pushing = ref(false);
const newMsg = ref('');
const inclUntracked = ref(false);

async function load() {
  if (!props.repo) return;
  loading.value = true;
  try {
    list.value = (await GoTools.GitStashList(props.repo)) || [];
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '读取暂存栈失败'), position: 'top', duration: 3000 });
  } finally {
    loading.value = false;
  }
}

watch(() => props.visible, (v) => { if (v) load(); });

async function doPush() {
  pushing.value = true;
  try {
    await GoTools.GitStashPush(props.repo, newMsg.value.trim(), inclUntracked.value);
    newMsg.value = '';
    Message.success({ content: '已暂存改动', position: 'top', duration: 1500 });
    emit('changed');
    load();
  } catch (e: any) {
    Message.warning({ content: String(e?.message || e || '暂存失败'), position: 'top', duration: 3500 });
  } finally {
    pushing.value = false;
  }
}

async function doApply(idx: number) {
  try {
    await GoTools.GitStashApply(props.repo, String(idx));
    Message.success({ content: `已应用 stash@{${idx}}`, position: 'top', duration: 1500 });
    emit('changed');
    load();
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '应用失败'), position: 'top', duration: 4000 });
  }
}

async function doPop(idx: number) {
  try {
    await GoTools.GitStashPop(props.repo, String(idx));
    Message.success({ content: `已弹栈 stash@{${idx}}`, position: 'top', duration: 1500 });
    emit('changed');
    load();
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '弹栈失败'), position: 'top', duration: 4000 });
  }
}

function doDrop(idx: number) {
  Modal.warning({
    title: `丢弃 stash@{${idx}}`,
    content: '丢弃后该暂存不可恢复（除非记得其 commit 并用 git fsck 找回）。确认丢弃？',
    okText: '丢弃',
    hideCancel: false,
    onOk: async () => {
      try {
        await GoTools.GitStashDrop(props.repo, String(idx));
        Message.success({ content: `已丢弃 stash@{${idx}}`, position: 'top', duration: 1500 });
        emit('changed');
        load();
      } catch (e: any) {
        Message.error({ content: String(e?.message || e || '丢弃失败'), position: 'top', duration: 4000 });
      }
    },
  });
}
</script>

<style scoped>
.sm-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
}
.sm-warn-ic {
  color: rgb(var(--warning-6));
}
.sm-table :deep(.arco-table-cell) {
  font-size: 12px;
}
.sm-empty {
  padding: 20px;
  color: var(--color-text-3);
  font-size: 12px;
}
.sm-tip {
  margin-top: 8px;
  color: var(--color-text-3);
  font-size: 12px;
}
</style>
