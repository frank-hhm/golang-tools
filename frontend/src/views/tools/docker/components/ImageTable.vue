<template>
    <!-- 镜像视角：工具条已并入页面卡片头，本地与线上共用一张表（列按视角切换） -->
    <div class="it-root">
        <!-- 提示占一行（不遮表头）：表头是 sticky 的，浮层盖上去会把「操作」和关闭按钮叠在一起 -->
        <a-alert v-if="remoteNotice" class="remote-alert" :type="remoteNotice.type" :title="remoteNotice.title"
            show-icon closable @close="emit('dismiss-notice')">
            <span class="notice-text">{{ remoteNotice.text }}</span>
        </a-alert>

        <!-- scroll.x 必须有，否则操作列的 fixed="right" 不生效（内容一宽就被切掉） -->
        <a-table class="it-table" :data="rows" :loading="busy" :pagination="false" size="small"
            :row-key="isLocal ? '_key' : 'name'" stripe :scroll="{ x: '100%', y: '100%' }">
            <template #empty>
                <a-empty :image="false" :description="emptyText" />
            </template>
            <template #columns>
                <a-table-column :title="isLocal ? '镜像' : '镜像名'" :width="300">
                    <template #cell="{ record }">
                        <a-space :size="6">
                            <!-- 名字长到只看到省略号时，悬停拿完整引用（项目统一用 a-tooltip，不用原生 title） -->
                            <a-tooltip :content="isLocal ? record.repo : record.name" position="tl">
                                <span class="cname">{{ isLocal ? record.repo : record.name }}</span>
                            </a-tooltip>
                            <a-tag v-if="isLocal" size="small" color="arcoblue">{{ record.tag }}</a-tag>
                            <a-tag v-if="isLocal && localRegistryOf(record.repo, registries)" size="small"
                                color="purple">{{ localRegistryOf(record.repo, registries) }}</a-tag>
                            <a-tag v-if="!isLocal && record.official" size="small" color="green">官方</a-tag>
                        </a-space>
                    </template>
                </a-table-column>

                <!-- 本地看体积与时间，线上看热度、来源与说明：列不一样，表只有一张 -->
                <a-table-column v-if="isLocal" title="镜像 ID" data-index="id" :width="120" ellipsis tooltip />
                <a-table-column v-if="isLocal" title="大小" :width="100">
                    <template #cell="{ record }">{{ fmtBytes(record.size) }}</template>
                </a-table-column>
                <a-table-column v-if="isLocal" title="创建时间" :width="160">
                    <template #cell="{ record }">{{ fmtTime(record.created) }}</template>
                </a-table-column>
                <a-table-column v-if="!isLocal" title="Star" :width="90">
                    <template #cell="{ record }">
                        <span v-if="record.stars"><icon-star /> {{ record.stars }}</span>
                        <span v-else class="dim">-</span>
                    </template>
                </a-table-column>
                <a-table-column v-if="!isLocal" title="来源" :width="140" ellipsis tooltip>
                    <template #cell="{ record }">
                        <a-tag size="small" :color="record.source === '加速站' ? 'orange' : 'gray'">
                            {{ record.source }}
                        </a-tag>
                    </template>
                </a-table-column>
                <a-table-column v-if="!isLocal" title="描述 / 说明" ellipsis tooltip>
                    <template #cell="{ record }">
                        <span class="dim">{{ record.description || '-' }}</span>
                        <div v-if="record.tags" class="tag-line mono">tag：{{ record.tags }}</div>
                    </template>
                </a-table-column>

                <!-- 固定列宽度按实际按钮排布给足：本地 创建容器/推送/导出/删除，线上 拉取/创建容器；
                     靠右对齐，按钮贴着表格右边（固定列本身已吸边） -->
                <a-table-column title="操作" :width="isLocal ? 300 : 180" fixed="right" align="right">
                    <template #cell="{ record }">
                        <a-space :size="2">
                            <a-button v-if="!isLocal" size="small" @click="emit('pull-remote', record)">
                                <template #icon><icon-download /></template>拉取
                            </a-button>
                            <a-button size="small" @click="emit('run', record)">
                                <template #icon><icon-play-arrow /></template>创建容器
                            </a-button>
                            <template v-if="isLocal">
                                <a-button size="small" @click="emit('push', record)">
                                    <template #icon><icon-upload /></template>推送
                                </a-button>
                                <a-button size="small" @click="emit('export-image', record)">
                                    <template #icon><icon-download /></template>导出
                                </a-button>
                                <a-popconfirm :content="`确定删除镜像「${record.repo}:${record.tag}」？有容器引用时会被拒绝。`"
                                    type="warning" position="lt" @ok="emit('remove', record)">
                                    <a-button size="small" status="danger">
                                        <template #icon><icon-delete /></template>删除
                                    </a-button>
                                </a-popconfirm>
                            </template>
                        </a-space>
                    </template>
                </a-table-column>
            </template>
        </a-table>

        <div v-if="!isLocal && remoteRows.length && !mirrorMode" class="remote-foot">
            <a-button size="small" long :loading="remoteLoading" @click="emit('load-more')">加载更多</a-button>
        </div>
    </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { fmtBytes, localRegistryOf } from "../helpers";

const props = defineProps<{
    /** 本地镜像（页面已补 _key） */
    images: any[];
    loading: boolean;
    /** 本地 / 线上视角：只决定取哪份数据、显哪几列 */
    scope: "local" | "remote";
    remoteRows: any[];
    /** 表格上沿的提示（错误 / 空结果 / 操作引导），null 则不显示 */
    remoteNotice: { type: "error" | "warning" | "info"; title: string; text: string } | null;
    remoteLoading: boolean;
    remoteSource: string;
    /** 已登记的私有仓库：本地表给带域名的镜像补个来源标签 */
    registries: any[];
}>();

const emit = defineEmits<{
    (e: "load-more"): void;
    /** 关掉表格上沿的提示 */
    (e: "dismiss-notice"): void;
    /** 行内操作：本地镜像 / 线上镜像各自回传整条记录 */
    (e: "run", rec: any): void;
    (e: "push", rec: any): void;
    (e: "export-image", rec: any): void;
    (e: "remove", rec: any): void;
    (e: "pull-remote", rec: any): void;
}>();

const isLocal = computed(() => props.scope !== "remote");

/** 来源是加速站（镜像名直达拉取），而不是 Hub 搜索或私有仓库目录 */
const mirrorMode = computed(() => String(props.remoteSource || "").startsWith("mirror:"));

// 一张表两个视角：数据与 loading 跟着视角走，列定义靠 isLocal 切
const rows = computed<any[]>(() => (isLocal.value ? props.images : props.remoteRows));
const busy = computed(() => (isLocal.value ? props.loading : props.remoteLoading));
const emptyText = computed(() => {
    if (isLocal.value) return "还没有本地镜像";
    return mirrorMode.value ? "输入镜像名后回车，直达加速站拉取" : "没有匹配的镜像";
});

function fmtTime(unixSec: number): string {
    if (!unixSec) return "-";
    const d = new Date(unixSec * 1000);
    const pad = (n: number) => n.toString().padStart(2, "0");
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}
</script>

<style scoped>
.it-root {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

/* 表格撑满剩余高度：滚动区跟着卡片走 */
.it-root :deep(.arco-table-container) {
    flex: 1;
}

.it-table {
    flex: 1;
    min-height: 0;
}

.cname {
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 240px;
}

/* 提示占一行：不再浮在表头之上（否则会盖住「操作」与行内按钮） */
.remote-alert {
    flex: 0 0 auto;
    margin: 8px 12px 0;
}

/* 长错误串（connectex、管道名）允许折行，但提示不能撑到半个屏幕 */
.remote-alert :deep(.arco-alert-content) {
    max-height: 72px;
    overflow-y: auto;
    word-break: break-all;
}

.remote-foot {
    flex: 0 0 auto;
    padding: 8px 12px;
    border-top: 1px solid var(--color-border-1);
}

.dim {
    color: var(--color-text-3);
}

.tag-line {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    margin-top: 2px;
}

.mono {
    font-family: Consolas, monospace;
}
</style>
