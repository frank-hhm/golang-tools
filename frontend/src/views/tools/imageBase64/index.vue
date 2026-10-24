<template>
    <a-row class="img-page" :gutter="6">
        <!-- 左侧约 25%：上传（上50%）+ 图片信息（下50%） -->
        <a-col :span="6" class="side-col">
            <a-row class="side-pages">
                <a-col class="side-cols" :span="24">
                    <a-card class="side-card" size="small" :body-style="{ padding: 0 }">
                        <template #title>上传图片</template>
                        <template #extra>
                            <a-tag size="small" :color="base64 ? 'green' : 'gray'">
                                {{ base64 ? "已选择" : "未选择" }}
                            </a-tag>
                        </template>
                        <div class="side-body">
                            <div class="dropzone" :class="{ 'is-over': dragOver }" @click="pickFile"
                                @dragover.prevent="dragOver = true" @dragleave.prevent="dragOver = false"
                                @drop.prevent="onDrop">
                                <icon-upload class="drop-icon" />
                                <div class="drop-text">
                                    拖放图片到这里，或
                                    <span class="drop-link">选择图片</span>
                                </div>
                                <div class="drop-hint">支持 PNG / JPG / GIF / WebP / SVG</div>
                            </div>
                            <input ref="fileInput" type="file" accept="image/*" style="display: none"
                                @change="onFileChange" />
                        </div>
                    </a-card>
                </a-col>
                <a-col class="side-cols mt6" :span="24">
                    <a-card class="side-card" size="small" :body-style="{ padding: 0 }">
                        <template #title>图片信息</template>
                        <template #extra>
                            <a-button v-if="base64" size="mini" type="text" status="danger" @click="reset">
                                <template #icon><icon-delete /></template>清空
                            </a-button>
                        </template>
                        <div class="side-body">
                            <template v-if="base64">
                                <div class="preview-wrap">
                                    <img :src="dataUri" class="preview" alt="preview" />
                                </div>
                                <a-descriptions :column="1" size="small" bordered>
                                    <a-descriptions-item label="文件名">{{ fileName }}</a-descriptions-item>
                                    <a-descriptions-item label="类型">{{ mimeType }}</a-descriptions-item>
                                    <a-descriptions-item label="大小">{{ fileSize }}</a-descriptions-item>
                                </a-descriptions>
                            </template>
                            <div v-else class="side-empty">
                                <a-empty description="尚未选择图片" />
                            </div>
                        </div>
                    </a-card>
                </a-col>
            </a-row>
        </a-col>

        <!-- 右侧：Base64 编码 -->
        <a-col :span="18" class="main-col">
            <a-card class="main-panel" :body-style="{ padding: 0 }">
                <template #title>
                    <a-space :size="8">
                        <span>Base64 编码</span>
                        <a-tag v-if="base64" size="small">{{ output.length }} 字符</a-tag>
                    </a-space>
                </template>
                <template #extra>
                    <a-space :size="8">
                        <a-switch v-model="withPrefix" type="round" :disabled="!base64">
                            <template #checked>含 data 前缀</template>
                            <template #unchecked>仅纯编码</template>
                        </a-switch>
                        <a-button type="primary" status="success" size="small" :disabled="!base64"
                            @click="copyText(output)">
                            <template #icon><icon-copy /></template>复制
                        </a-button>
                    </a-space>
                </template>
                <div class="panel-body">
                    <a-textarea v-if="base64" :model-value="output" class="fill-textarea" readonly />
                    <div v-else class="side-empty">
                        <a-empty description="选择图片后这里显示 Base64" />
                    </div>
                </div>
            </a-card>
        </a-col>
    </a-row>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import { Message } from "@arco-design/web-vue";

const fileInput = ref<HTMLInputElement | null>(null);
const dragOver = ref(false);
const withPrefix = ref(true);

const fileName = ref("");
const mimeType = ref("");
const fileSize = ref("");
const base64 = ref("");

/** 带前缀的完整 data URI（用于预览） */
const dataUri = computed(() =>
    base64.value ? `data:${mimeType.value};base64,${base64.value}` : ""
);

/** 实际输出（按开关决定是否含前缀） */
const output = computed(() =>
    base64.value
        ? withPrefix.value
            ? `data:${mimeType.value};base64,${base64.value}`
            : base64.value
        : ""
);

function formatSize(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(2)} KB`;
    return `${(bytes / 1024 / 1024).toFixed(2)} MB`;
}

const handleFile = (file: File | undefined) => {
    if (!file) return;
    if (!file.type.startsWith("image/")) {
        Message.warning("请选择图片文件");
        return;
    }
    fileName.value = file.name;
    mimeType.value = file.type;
    fileSize.value = formatSize(file.size);
    const reader = new FileReader();
    reader.onload = () => {
        const result = String(reader.result || "");
        // readAsDataURL 形如 data:image/png;base64,xxxx，取逗号后的纯编码
        const comma = result.indexOf(",");
        base64.value = comma >= 0 ? result.slice(comma + 1) : result;
    };
    reader.onerror = () => Message.error("读取文件失败");
    reader.readAsDataURL(file);
};

const pickFile = () => fileInput.value?.click();

const onFileChange = (e: Event) => {
    const input = e.target as HTMLInputElement;
    handleFile(input.files?.[0]);
    input.value = ""; // 允许重复选择同一文件
};

const onDrop = (e: DragEvent) => {
    dragOver.value = false;
    const file = e.dataTransfer?.files?.[0];
    handleFile(file);
};

const reset = () => {
    fileName.value = "";
    mimeType.value = "";
    fileSize.value = "";
    base64.value = "";
};

const copyText = async (text: string) => {
    if (!text) return;
    try {
        await navigator.clipboard.writeText(text);
        Message.success("已复制到剪贴板");
    } catch {
        const ta = document.createElement("textarea");
        ta.value = text;
        ta.style.position = "fixed";
        ta.style.opacity = "0";
        document.body.appendChild(ta);
        ta.select();
        document.execCommand("copy");
        document.body.removeChild(ta);
        Message.success("已复制到剪贴板");
    }
};
</script>

<style scoped>
/* 页面根容器：a-row 铺满工具窗口内容区 */
.img-page {
    height: 100%;
    min-height: 0;
    flex-wrap: nowrap;
    background: var(--color-fill-1);
}

/* 左侧列：上下两张卡片各占 50% 高 */
.side-col {
    height: 100%;
    min-width: 0;
    display: flex;
    flex-direction: column;
}

.side-pages {
    height: calc(100% - 6px);
}

.side-cols {
    height: 50%;
    width: 100%;
    display: flex;
    flex-direction: column;
}

.side-card {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

.side-card :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

.side-body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: var(--base-padding);
    padding: var(--base-padding);
    overflow-y: auto;
}

/* 拖放上传区（填满上传卡片） */
.dropzone {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 6px;
    padding: var(--base-padding);
    text-align: center;
    border: 1px dashed var(--color-border-2);
    border-radius: var(--base-radius-large);
    cursor: pointer;
    transition: border-color 0.2s, background-color 0.2s;
}

.dropzone:hover,
.dropzone.is-over {
    border-color: rgb(var(--green-6));
    background-color: var(--color-fill-2);
}

.drop-icon {
    font-size: 28px;
    color: var(--color-text-3);
}

.drop-text {
    color: var(--color-text-2);
    font-size: var(--base-size-default);
}

.drop-link {
    color: rgb(var(--green-6));
}

.drop-hint {
    color: var(--color-text-3);
    font-size: var(--base-size-small);
}

/* 预览与空状态 */
.preview-wrap {
    flex: 1;
    min-height: 120px;
    display: flex;
    align-items: center;
    justify-content: center;
    overflow: hidden;
    background: var(--color-fill-1);
    border-radius: var(--base-radius-large);
}

.preview {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
    border-radius: var(--base-radius-default);
}

.side-empty {
    flex: 1;
    min-height: 160px;
    display: flex;
    align-items: center;
    justify-content: center;
}

/* 右侧主列 */
.main-col {
    height: 100%;
    min-width: 0;
    display: flex;
    flex-direction: column;
}

.main-panel {
    width: 100%;
    height: 100%;
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    background: var(--color-bg-2);
}

.main-panel :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

/* Base64 文本域：填满卡片并内部滚动 */
.panel-body {
    flex: 1;
    min-height: 0;
    display: flex;
    padding: var(--base-padding);
}

.fill-textarea {
    flex: 1;
    min-height: 0;
    height: 100%;
}

.fill-textarea :deep(.arco-textarea) {
    height: 100%;
    resize: none;
    word-break: break-all;
    font-family: Consolas, Monaco, "Courier New", monospace;
    font-size: var(--base-size-small);
}
</style>
