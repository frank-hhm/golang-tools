<template>
  <a-modal :visible="visible" title="主机密钥（known_hosts）" :width="640" :mask-closable="false" :footer="false"
    @cancel="close" @update:visible="(v: boolean) => emit('update:visible', v)">
    <a-alert type="info" class="kh-alert">
      首次连接按策略记录服务端指纹；已记录但指纹变化会硬阻断（可能被中间人）。校验策略影响所有 SSH 连接。
    </a-alert>

    <div class="kh-policy">
      <span class="kh-label">校验策略</span>
      <a-radio-group v-model="policy" type="button" size="small" @change="savePolicy">
        <a-radio value="strict">严格（未知即拒）</a-radio>
        <a-radio value="tofu">首次信任（默认）</a-radio>
        <a-radio value="off">不校验（兼容）</a-radio>
      </a-radio-group>
      <a-button size="small" :loading="importing" @click="doImport">
        <template #icon><icon-import /></template>导入系统 known_hosts
      </a-button>
    </div>

    <a-table :data="entries" :loading="loading" size="small" :pagination="false" row-key="address"
      class="kh-table" :scroll="{ y: 260 }">
      <template #columns>
        <a-table-column title="地址" data-index="address" :width="170" ellipsis tooltip />
        <a-table-column title="类型" data-index="keyType" :width="130" />
        <a-table-column title="指纹 (SHA256)" data-index="fingerprint" ellipsis tooltip />
        <a-table-column title="操作" :width="70">
          <template #cell="{ record }">
            <a-button size="mini" type="text" status="danger" @click="removeOne(record.address)">删除</a-button>
          </template>
        </a-table-column>
      </template>
    </a-table>
    <div class="kh-foot">共 {{ entries.length }} 条记录</div>
  </a-modal>
</template>

<script setup lang="ts">
import { getCurrentInstance, ref, watch } from 'vue';
import { Message, Modal } from '@arco-design/web-vue';
import type { TermKnownHost } from '../types';

const props = defineProps<{ visible: boolean }>();
const emit = defineEmits<{ (e: 'update:visible', v: boolean): void }>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const policy = ref('tofu');
const entries = ref<TermKnownHost[]>([]);
const loading = ref(false);
const importing = ref(false);

async function loadPolicy() {
  try {
    policy.value = (await GoTools?.TermHostKeyPolicy()) || 'tofu';
  } catch {
    policy.value = 'tofu';
  }
}

async function loadEntries() {
  loading.value = true;
  try {
    entries.value = (await GoTools?.TermListKnownHosts()) || [];
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '读取失败'), position: 'top', duration: 3000 });
  } finally {
    loading.value = false;
  }
}

watch(() => props.visible, (v) => { if (v) { loadPolicy(); loadEntries(); } });

async function savePolicy() {
  try {
    policy.value = (await GoTools.TermSetHostKeyPolicy(policy.value)) || policy.value;
    Message.success('策略已更新：' + policyText(policy.value));
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '更新失败'), position: 'top', duration: 3000 });
  }
}

function policyText(p: string) {
  return p === 'strict' ? '严格' : p === 'off' ? '不校验' : '首次信任';
}

async function doImport() {
  importing.value = true;
  try {
    const n = await GoTools.TermImportKnownHosts();
    Message.success(n ? `已导入 ${n} 条` : '系统 known_hosts 无新增内容');
    await loadEntries();
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '导入失败'), position: 'top', duration: 3000 });
  } finally {
    importing.value = false;
  }
}

function removeOne(address: string) {
  Modal.warning({
    title: '删除主机密钥',
    content: `删除「${address}」后，下次连接将按当前策略重新确认。`,
    hideCancel: false,
    onOk: async () => {
      try {
        entries.value = (await GoTools.TermDeleteKnownHost(address)) || [];
      } catch (e: any) {
        Message.error({ content: String(e?.message || e || '删除失败'), position: 'top', duration: 3000 });
      }
    },
  });
}

function close() {
  emit('update:visible', false);
}
</script>

<style scoped>
.kh-alert {
  margin-bottom: 10px;
}

.kh-policy {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 10px;
  flex-wrap: wrap;
}

.kh-label {
  color: var(--color-text-2);
  font-size: var(--base-size-small);
}

.kh-table :deep(.arco-table-cell) {
  font-size: var(--base-size-small);
}

.kh-foot {
  margin-top: 8px;
  text-align: right;
  color: var(--color-text-3);
  font-size: var(--base-size-small);
}
</style>
