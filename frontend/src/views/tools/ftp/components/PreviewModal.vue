<template>
  <a-modal :visible="visible" :title="entry?.name || '预览'" :width="880" :footer="false" unmount-on-close
    @cancel="close" @update:visible="(v: boolean) => emit('update:visible', v)">
    <div class="pv">
      <a-spin v-if="loading" class="pv-spin" />
      <template v-else-if="preview">
        <pre v-if="preview.kind === 'text'" class="pv-text">{{ preview.data }}</pre>
        <div v-else-if="preview.kind === 'image'" class="pv-image">
          <img :src="preview.data" :alt="entry?.name || ''" />
        </div>
        <a-result v-else-if="preview.kind === 'tooLarge'" status="warning"
          :title="`文件过大（${fmtSize(preview.size)}），不支持在线预览`" sub-title="请下载后用本地工具打开">
        </a-result>
        <a-result v-else status="info" title="二进制文件不支持预览" sub-title="请下载后用本地工具打开" />
      </template>
    </div>
    <div v-if="preview" class="pv-foot">
      <span class="dim">{{ fmtSize(preview.size) }} · {{ entry?.mode || '' }}</span>
      <span v-if="preview.truncated" class="warn">内容超过 1MB，已截断显示</span>
    </div>
  </a-modal>
</template>

<script setup lang="ts">
import { getCurrentInstance, ref, watch } from 'vue';
import { Message } from '@arco-design/web-vue';
import { fmtSize } from '@/views/tools/disk/helpers';
import type { FtpEntry, FtpPreview } from '../types';

const props = defineProps<{ visible: boolean; sessionId: string; entry: FtpEntry | null }>();
const emit = defineEmits<{ (e: 'update:visible', v: boolean): void }>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const loading = ref(false);
const preview = ref<FtpPreview | null>(null);

watch(
  () => props.visible,
  async (v) => {
    if (!v || !props.entry) return;
    preview.value = null;
    loading.value = true;
    try {
      preview.value = await GoTools.FtpPreview(props.sessionId, props.entry.path);
    } catch (e: any) {
      Message.error({ content: String(e?.message || e || '预览失败'), position: 'top', duration: 3000 });
      emit('update:visible', false);
    } finally {
      loading.value = false;
    }
  },
);

function close() {
  emit('update:visible', false);
}
</script>

<style scoped>
.pv {
  min-height: 120px;
  max-height: 62vh;
  overflow: auto;
}

.pv-spin {
  display: flex;
  justify-content: center;
  padding: 40px 0;
}

.pv-text {
  margin: 0;
  padding: 10px 12px;
  background: var(--color-fill-1);
  border-radius: 4px;
  font-family: Consolas, 'Cascadia Mono', 'Microsoft YaHei', monospace;
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
}

.pv-image {
  display: flex;
  justify-content: center;
  padding: 8px 0;
}

.pv-image img {
  max-width: 100%;
  max-height: 58vh;
  object-fit: contain;
}

.pv-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding-top: 8px;
  font-size: 12px;
}

.dim {
  color: var(--color-text-3);
}

.warn {
  color: rgb(var(--warning-6));
}
</style>
