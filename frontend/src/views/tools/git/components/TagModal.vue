<template>
  <a-modal :visible="visible" title="标签（Tag）" width="760px" :footer="false" :esc-to-close="true"
    @update:visible="(v: boolean) => emit('update:visible', v)">
    <div class="tg-form">
      <div class="tg-row">
        <a-input v-model="newName" size="small" placeholder="标签名，如 v1.2.0" allow-clear style="width: 180px" />
        <a-input v-model="newTarget" size="small" placeholder="起点（留空=HEAD）" allow-clear style="width: 160px" />
      </div>
      <div class="tg-row">
        <a-input v-model="newMsg" size="small" placeholder="标签说明（留空=轻量标签；填写=附注标签）" allow-clear
          style="flex: 1" @press-enter="doCreate" />
        <a-button size="small" type="primary" :disabled="!newName.trim()" :loading="creating" @click="doCreate">
          <template #icon><icon-plus /></template>创建标签
        </a-button>
        <a-button size="small" :loading="loading" @click="load">
          <template #icon><icon-refresh /></template>刷新
        </a-button>
      </div>
      <div class="tg-note">
        <icon-info-circle /> 本工具创建的附注标签一律<strong>不带 GPG 签名</strong>（不接管私钥/口令）；需签名请回到命令行用你自己的密钥操作。
      </div>
    </div>

    <a-table :data="list" :loading="loading" size="small" row-key="name" :pagination="false" class="tg-table"
      :scroll="{ y: 360 }">
      <template #columns>
        <a-table-column title="标签" data-index="name" :width="180">
          <template #cell="{ record }">
            <span class="tg-name">{{ record.name }}</span>
          </template>
        </a-table-column>
        <a-table-column title="类型" :width="80">
          <template #cell="{ record }">
            <a-tag size="small" :color="record.annotated ? 'purple' : 'gray'">
              {{ record.annotated ? '附注' : '轻量' }}
            </a-tag>
          </template>
        </a-table-column>
        <a-table-column title="提交" data-index="commit" :width="90" />
        <a-table-column title="说明" data-index="subject" ellipsis tooltip />
        <a-table-column title="操作" :width="80">
          <template #cell="{ record }">
            <a-button size="mini" type="secondary" status="danger" @click="doDelete(record.name)">删除</a-button>
          </template>
        </a-table-column>
      </template>
      <template #empty>
        <div class="tg-empty">暂无标签</div>
      </template>
    </a-table>
  </a-modal>
</template>

<script setup lang="ts">
/**
 * TagModal：git tag 的列/建/删。留空说明建轻量标签，填说明建附注标签（引擎强制不签名）。
 * 删除为破坏操作，二次确认；只删本地引用（不动远端）。
 */
import { getCurrentInstance, ref, watch } from 'vue';
import { Message, Modal } from '@arco-design/web-vue';
import type { Tag } from 'bindings/Tools/tools/gitcmd/index';

const props = defineProps<{ visible: boolean; repo: string }>();
const emit = defineEmits<{
  (e: 'update:visible', v: boolean): void;
  (e: 'changed'): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const list = ref<Tag[]>([]);
const loading = ref(false);
const creating = ref(false);
const newName = ref('');
const newMsg = ref('');
const newTarget = ref('');

async function load() {
  if (!props.repo) return;
  loading.value = true;
  try {
    list.value = (await GoTools.GitTagList(props.repo)) || [];
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '读取标签失败'), position: 'top', duration: 3000 });
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
    await GoTools.GitTagCreate(props.repo, name, newMsg.value.trim(), newTarget.value.trim());
    newName.value = '';
    newMsg.value = '';
    newTarget.value = '';
    Message.success({ content: `已创建 ${name}`, position: 'top', duration: 1500 });
    load();
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '创建标签失败'), position: 'top', duration: 4000 });
  } finally {
    creating.value = false;
  }
}

function doDelete(name: string) {
  Modal.warning({
    title: `删除标签 ${name}`,
    content: '仅删除本地标签引用（远端上的同名标签不受影响）。确认删除？',
    okText: '删除',
    hideCancel: false,
    onOk: async () => {
      try {
        await GoTools.GitTagDelete(props.repo, name);
        Message.success({ content: `已删除 ${name}`, position: 'top', duration: 1500 });
        load();
      } catch (e: any) {
        Message.error({ content: String(e?.message || e || '删除失败'), position: 'top', duration: 4000 });
      }
    },
  });
}
</script>

<style scoped>
.tg-form {
  margin-bottom: 10px;
}
.tg-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}
.tg-note {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 8px;
  border-radius: var(--base-radius-small);
  background: var(--color-fill-2);
  color: var(--color-text-3);
  font-size: 12px;
}
.tg-table :deep(.arco-table-cell) {
  font-size: 12px;
}
.tg-name {
  font-family: Consolas, "Courier New", monospace;
}
.tg-empty {
  padding: 20px;
  color: var(--color-text-3);
  font-size: 12px;
}
</style>
