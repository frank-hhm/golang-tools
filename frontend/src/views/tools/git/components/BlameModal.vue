<template>
  <a-modal :visible="visible" :title="title" width="86%" :esc-to-close="true" :footer="false"
    :body-style="{ padding: '0 16px', height: '72vh' }"
    @update:visible="(v: boolean) => emit('update:visible', v)">
    <blame-pane v-if="visible && path" :repo="repo" :path="path" :revision="revision" @commit="onCommit" />
  </a-modal>
</template>

<script setup lang="ts">
/**
 * BlameModal：逐行归属弹窗外壳（计划 P2 BlamePane 的入口）。
 * 父只给可见性 + 上下文（repo/path/revision），归属与展示由 BlamePane 自管。
 */
import { computed, defineAsyncComponent } from 'vue';

const BlamePane = defineAsyncComponent(() => import('./BlamePane.vue'));

const props = defineProps<{
  visible: boolean;
  repo: string;
  path: string;
  revision?: string;
}>();
const emit = defineEmits<{
  (e: 'update:visible', v: boolean): void;
  (e: 'commit', hash: string): void;
}>();

const title = computed(() => `追溯 - ${props.path}`);

function onCommit(hash: string) {
  emit('commit', hash);
}
</script>
