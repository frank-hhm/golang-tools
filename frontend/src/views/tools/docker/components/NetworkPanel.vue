<template>
    <!-- 网络视角：只负责列表与表单，接口调用和刷新留在页面 -->
    <div class="res-root">
        <!-- scroll.x 必须有，否则操作列的 fixed="right" 不生效 -->
        <a-table class="res-table" :data="networks" :loading="loading" :pagination="false" size="small" row-key="id"
            stripe :scroll="{ x: 970, y: '100%' }">
            <template #empty>
                <a-empty :image="false" description="还没有自定义网络" />
            </template>
            <template #columns>
                <a-table-column title="名称" :width="240">
                    <template #cell="{ record }">
                        <a-space :size="6">
                            <a-tooltip :content="record.name">
                                <span class="cname">{{ record.name }}</span>
                            </a-tooltip>
                            <a-tag v-if="record.builtin" size="small">内置</a-tag>
                            <a-tag v-if="record.internal" size="small" color="orange">无外网</a-tag>
                        </a-space>
                    </template>
                </a-table-column>
                <a-table-column title="驱动" data-index="driver" :width="90" />
                <a-table-column title="子网" :width="150">
                    <template #cell="{ record }">
                        <span class="mono dim">{{ record.subnet || '-' }}</span>
                    </template>
                </a-table-column>
                <a-table-column title="网关" :width="130">
                    <template #cell="{ record }">
                        <span class="mono dim">{{ record.gateway || '-' }}</span>
                    </template>
                </a-table-column>
                <a-table-column title="接入容器" :width="90" align="center">
                    <template #cell="{ record }">
                        <span :class="{ dim: !record.containers }">{{ record.containers || 0 }}</span>
                    </template>
                </a-table-column>
                <a-table-column title="创建时间" :width="160">
                    <template #cell="{ record }">{{ fmtRFC(record.created) }}</template>
                </a-table-column>
                <!-- 固定列宽度按实际按钮核算：删除（7px*2 + 12 图标 + 4 gap + 2 字 ≈ 62）+ 左右内边距 32 -->
                <a-table-column title="操作" :width="110" fixed="right" align="right">
                    <template #cell="{ record }">
                        <a-button v-if="record.builtin" size="small" status="danger" disabled>删除</a-button>
                        <a-popconfirm v-else
                            :content="record.containers ? `网络「${record.name}」还有 ${record.containers} 个容器在接入，确定删除？` : `确定删除网络「${record.name}」？`"
                            type="warning" position="lt" @ok="emit('remove', record)">
                            <a-button size="small" status="danger">
                                <template #icon><icon-delete /></template>删除
                            </a-button>
                        </a-popconfirm>
                    </template>
                </a-table-column>
            </template>
        </a-table>

        <a-modal :visible="createVisible" title="新建网络" :width="460" :on-before-ok="submit"
            @cancel="createVisible = false" @update:visible="createVisible = $event">
            <a-form :model="form" layout="vertical">
                <a-form-item field="name" label="网络名称" required>
                    <a-input v-model="form.name" placeholder="如 app-net（字母/数字/下划线/点/短横线）" allow-clear />
                </a-form-item>
                <a-form-item field="driver" label="驱动">
                    <a-select v-model="form.driver" :options="driverOptions" />
                </a-form-item>
                <a-form-item field="subnet" label="子网">
                    <a-input v-model="form.subnet" placeholder="如 172.30.0.0/16，留空由 Docker 分配" allow-clear />
                </a-form-item>
                <a-form-item field="internal" label="内部网络">
                    <a-space :size="8">
                        <a-switch v-model="form.internal" size="small" />
                        <span class="fmt-tip">开启后容器只能访问同网络内的容器，没有外网出口</span>
                    </a-space>
                </a-form-item>
            </a-form>
        </a-modal>
    </div>
</template>

<script setup lang="ts">
import { reactive, ref } from "vue";
import { Message } from "@arco-design/web-vue";
import { fmtRFC } from "../helpers";

defineProps<{
    networks: any[];
    loading: boolean;
}>();

const emit = defineEmits<{
    (e: "create", spec: { name: string; driver: string; subnet: string; internal: boolean }): void;
    (e: "remove", record: any): void;
}>();

const createVisible = ref(false);
const driverOptions = [
    { label: "bridge（默认，单机桥接）", value: "bridge" },
    { label: "host（共享宿主网络）", value: "host" },
    { label: "overlay（跨主机，需 Swarm）", value: "overlay" },
];
const form = reactive({ name: "", driver: "bridge", subnet: "", internal: false });

/** 打开新建弹窗（父页面通过 ref 调用，避免把表单状态放到页面里） */
function open() {
    form.name = "";
    form.driver = "bridge";
    form.subnet = "";
    form.internal = false;
    createVisible.value = true;
}

defineExpose({ open });

function submit(): boolean {
    const name = form.name.trim();
    if (!name) {
        Message.warning("网络名称不能为空");
        return false;
    }
    emit("create", {
        name,
        driver: form.driver,
        subnet: form.subnet.trim(),
        internal: form.internal,
    });
    return true;
}
</script>

<style scoped>
.res-root {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

.res-table {
    flex: 1;
    min-height: 0;
}

.res-root :deep(.arco-table-container) {
    flex: 1;
}

.cname {
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 170px;
}

.dim {
    color: var(--color-text-3);
}

.mono {
    font-family: Consolas, monospace;
}

.fmt-tip {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}
</style>
