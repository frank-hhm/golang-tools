<template>
  <a-modal
    :visible="visible"
    title="编辑标签"
    :ok-loading="saving"
    @update:visible="(v: boolean) => emit('update:visible', v)"
    @ok="submit"
    @cancel="close"
  >
    <div class="tip" v-if="item">为「{{ item.name }}」设置标签，可下拉选择已有标签或直接回车新建。</div>
    <a-select
      v-model="local"
      multiple
      allow-create
      :data="allTags"
      placeholder="输入后回车添加标签"
      style="width: 100%"
    />
  </a-modal>
</template>

<script setup lang="ts">
/** 标签编辑弹窗：只负责一个条目的标签增删，保存后通知父级刷新。 */
import { getCurrentInstance, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { errText, type DocItem } from "../types";

const props = defineProps<{ visible: boolean; item: DocItem | null; allTags: string[] }>();
const emit = defineEmits<{
  (e: "update:visible", v: boolean): void;
  (e: "saved"): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const local = ref<string[]>([]);
const saving = ref(false);

watch(
  () => props.visible,
  (v) => {
    if (v) local.value = [...(props.item?.tags ?? [])];
  },
);

function close() {
  emit("update:visible", false);
}

async function submit() {
  if (!props.item) return;
  saving.value = true;
  try {
    await GoTools?.DocSpaceSetItemTags(props.item.id, local.value);
    Message.success("标签已保存");
    emit("saved");
    close();
  } catch (e: any) {
    Message.error(`保存标签失败：${errText(e)}`);
  } finally {
    saving.value = false;
  }
}
</script>

<style scoped>
.tip {
  margin-bottom: 10px;
  color: var(--color-text-3);
  font-size: 12px;
}
</style>
