<template>
  <a-modal :visible="visible" :title="title" width="90%" :esc-to-close="true" :footer="false"
    :body-style="{ padding: '0 16px', height: '72vh' }"
    @update:visible="(v: boolean) => emit('update:visible', v)">
    <diff-pane v-if="visible" :repo="repo" :opts="opts" @saved="emit('saved')" />
  </a-modal>
</template>

<script setup lang="ts">
/**
 * DiffModal：差异弹窗外壳，把可见性与上下文（repo/opts）传给 DiffPane。
 * 编辑器/模式切换/保存全部由 DiffPane 自管（与查看器 CodePane 同一「父只给上下文」约定）。
 */
import { computed, defineAsyncComponent } from 'vue';

// DiffPane 内含 CodeMirror/merge，异步加载成独立 chunk（计划 P2：@codemirror/merge 必须 dynamic import）
const DiffPane = defineAsyncComponent(() => import('./DiffPane.vue'));

const props = defineProps<{
  visible: boolean;
  repo: string;
  opts: Record<string, unknown>;
}>();
const emit = defineEmits<{ (e: 'update:visible', v: boolean): void; (e: 'saved'): void }>();

const title = computed(() => (props.opts.path ? `差异 - ${props.opts.path}` : '差异'));
</script>
