<template>
    <a-modal :visible="visible" :title="title" :width="620" :ok-loading="false" :mask-closable="false"
        :ok-text="okText" @cancel="close" @ok="submit">
        <div class="spm">
            <div class="spm-row">
                <span class="spm-label">文件名</span>
                <a-input v-model="fileName" size="small" placeholder="含扩展名，如 a-编辑.jpg" @press-enter="submit" />
            </div>
            <div class="spm-row">
                <span class="spm-label">目录</span>
                <a-input v-model="dir" size="small" placeholder="输出目录（下面点目录也可以）" @press-enter="browse(dir)" />
            </div>
            <file-picker :paths="[]" :multiple="false" :pick-dir="true" :start-dir="dir" @dir="onDir" />
            <div class="spm-tip">
                <icon-exclamation-circle />
                <span>同名文件会被直接覆盖（写回经 FileWriteBinary 原子落盘），需要保留原图请改个名字。</span>
            </div>
            <div v-if="err" class="spm-err">{{ err }}</div>
            <div v-else class="spm-full">完整路径：{{ full }}</div>
        </div>
    </a-modal>
</template>

<script setup lang="ts">
/*
输出路径选择弹窗：文件名 + 目录（内嵌目录浏览，点目录即切）。
编辑产物、拼接/转PDF/合并/拆分的保存目标都走它，最终只把一条完整路径交回宿主。
*/
import { computed, ref, watch } from "vue";
import FilePicker from "./FilePicker.vue";
import { joinPath } from "./editx";

const props = withDefaults(defineProps<{
    visible: boolean;
    dir?: string;
    name?: string;
    title?: string;
    okText?: string;
}>(), {
    dir: "",
    name: "",
    title: "保存到…",
    okText: "保存",
});

const emit = defineEmits<{
    (e: "update:visible", v: boolean): void;
    (e: "confirm", path: string): void;
}>();

const dir = ref(props.dir);
const fileName = ref(props.name);
const err = ref("");

const full = computed(() => (dir.value && fileName.value ? joinPath(dir.value, fileName.value) : ""));

watch(() => props.visible, (v) => {
    if (v) {
        dir.value = props.dir;
        fileName.value = props.name;
        err.value = "";
    }
});

function onDir(d: string) { if (d) dir.value = d; }
function browse(d: string) { if (d) dir.value = d; }

function close() { emit("update:visible", false); }

function submit() {
    if (!fileName.value.trim()) { err.value = "请填写文件名"; return; }
    if (!dir.value) { err.value = "请选择输出目录"; return; }
    if (/[\\/:*?"<>|]/.test(fileName.value.replace(/^[a-zA-Z]:/, ""))) {
        err.value = "文件名含非法字符";
        return;
    }
    emit("confirm", full.value);
    close();
}
</script>

<style scoped>
.spm {
    display: flex;
    flex-direction: column;
    gap: 8px;
}
.spm-row {
    display: flex;
    align-items: center;
    gap: 8px;
}
.spm-label {
    flex: none;
    width: 48px;
    color: var(--color-text-3);
    font-size: var(--base-size-12);
}
.spm-tip {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--color-text-3);
    font-size: var(--base-size-12);
}
.spm-err {
    color: rgb(var(--danger-6));
    font-size: var(--base-size-12);
}
.spm-full {
    color: var(--color-text-2);
    font-size: var(--base-size-12);
    word-break: break-all;
}
</style>
