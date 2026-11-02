<template>
  <a-modal :visible="visible" title="从 ~/.ssh/config 导入" :width="680" :mask-closable="false"
    ok-text="导入所选" cancel-text="取消" :ok-loading="busy" :ok-button-props="{ disabled: !selected.length }"
    @ok="doImport" @cancel="close" @update:visible="(v: boolean) => emit('update:visible', v)">
    <a-alert type="warning" class="ic-alert">
      仅导入为可编辑主机记录；<b>ProxyJump / ForwardAgent / 端口转发</b>等多跳与隧道指令本期不生效，导入后需自行在主机上补关联凭据。
    </a-alert>

    <a-table :data="drafts" :loading="loading" size="small" row-key="alias" :pagination="false"
      :scroll="{ y: 320 }" v-model:selectedKeys="selected" :row-selection="{ type: 'checkbox', showCheckedAll: true }">
      <template #columns>
        <a-table-column title="别名" data-index="alias" :width="120" ellipsis tooltip>
          <template #cell="{ record }">
            <a-space :size="4">
              <span>{{ record.alias }}</span>
              <a-tag v-if="record.wildcard" size="small" color="orange">通配</a-tag>
            </a-space>
          </template>
        </a-table-column>
        <a-table-column title="HostName" data-index="hostName" :width="140" ellipsis tooltip />
        <a-table-column title="账号" data-index="user" :width="90" ellipsis tooltip />
        <a-table-column title="端口" data-index="port" :width="70" />
        <a-table-column title="私钥" data-index="identityFile" ellipsis tooltip>
          <template #cell="{ record }">
            <span class="mono">{{ record.identityFile || '—' }}</span>
          </template>
        </a-table-column>
        <a-table-column title="忽略项" :width="150">
          <template #cell="{ record }">
            <a-space wrap :size="2">
              <a-tag v-for="u in record.unsupported" :key="u" size="small" color="red">{{ u }}</a-tag>
              <span v-if="!record.unsupported || !record.unsupported.length" class="muted">—</span>
            </a-space>
          </template>
        </a-table-column>
      </template>
    </a-table>
    <div class="ic-foot">解析到 {{ drafts.length }} 个 Host 段，已选 {{ selected.length }} 个</div>
  </a-modal>
</template>

<script setup lang="ts">
import { getCurrentInstance, ref, watch } from 'vue';
import { Message } from '@arco-design/web-vue';
import type { TermHost, TermHostDraft } from '../types';

const props = defineProps<{ visible: boolean }>();
const emit = defineEmits<{ (e: 'update:visible', v: boolean): void; (e: 'imported', list: TermHost[]): void }>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const drafts = ref<TermHostDraft[]>([]);
const selected = ref<string[]>([]);
const loading = ref(false);
const busy = ref(false);

async function load() {
  loading.value = true;
  selected.value = [];
  try {
    drafts.value = (await GoTools?.TermParseSSHConfig()) || [];
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '解析失败'), position: 'top', duration: 3000 });
  } finally {
    loading.value = false;
  }
}

watch(() => props.visible, (v) => { if (v) load(); });

async function doImport() {
  const chosen = drafts.value.filter((d) => selected.value.includes(d.alias));
  if (!chosen.length) return;
  busy.value = true;
  let lastList: TermHost[] | null = null;
  let ok = 0;
  const failed: string[] = [];
  try {
    for (const d of chosen) {
      const address = d.hostName || d.alias;
      if (!address || address.includes('*') || address.includes('?')) {
        failed.push(d.alias);
        continue;
      }
      const hasKey = !!d.identityFile;
      const h: TermHost = {
        id: '',
        name: d.alias,
        system: 'linux',
        address,
        port: d.port || 22,
        user: d.user || '',
        auth: hasKey ? 'key' : 'password',
        keyPath: hasKey ? d.identityFile : '',
        note: '来自 ~/.ssh/config',
      };
      // 无凭据、无密码：SaveHost 的旧式分支会因缺账号/私钥校验——有 keyPath 即可；纯密码留空需后续补
      if (h.auth === 'password' && !h.user) {
        failed.push(d.alias);
        continue;
      }
      try {
        lastList = (await GoTools.TermSaveHost(h)) || [];
        ok++;
      } catch {
        failed.push(d.alias);
      }
    }
    if (ok) {
      Message.success(`已导入 ${ok} 个主机${failed.length ? `，${failed.length} 个跳过` : ''}`);
      emit('imported', lastList || []);
      emit('update:visible', false);
    } else {
      Message.warning('没有可导入的主机（多数缺少账号或需先建凭据）');
    }
  } finally {
    busy.value = false;
  }
}

function close() {
  emit('update:visible', false);
}
</script>

<style scoped>
.ic-alert {
  margin-bottom: 10px;
}

.mono {
  font-family: var(--font-family-mono, monospace);
  font-size: var(--base-size-small);
}

.muted {
  color: var(--color-text-4);
}

.ic-foot {
  margin-top: 8px;
  text-align: right;
  color: var(--color-text-3);
  font-size: var(--base-size-small);
}
</style>
