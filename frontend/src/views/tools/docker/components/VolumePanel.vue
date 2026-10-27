<template>
    <!-- 存储卷视角：列表 + 新建表单；清理未使用卷的入口在页面卡片头的工具条里 -->
    <div class="res-root">
        <a-table class="res-table" :data="volumes" :loading="loading" :pagination="false" size="small" row-key="name"
            stripe :scroll="{ x: 900, y: '100%' }">
            <template #empty>
                <a-empty :image="false" description="还没有存储卷" />
            </template>
            <template #columns>
                <a-table-column title="名称" :width="240">
                    <template #cell="{ record }">
                        <a-tooltip :content="record.name">
                            <span class="cname">{{ record.name }}</span>
                        </a-tooltip>
                    </template>
                </a-table-column>
                <a-table-column title="驱动" data-index="driver" :width="90" />
                <a-table-column title="引用容器" :width="100" align="center">
                    <template #cell="{ record }">
                        <span v-if="record.inUse" class="inuse">{{ record.inUse }} 个</span>
                        <span v-else class="dim">未使用</span>
                    </template>
                </a-table-column>
                <a-table-column title="挂载点" ellipsis tooltip>
                    <template #cell="{ record }">
                        <span class="mono dim">{{ record.mountpoint || '-' }}</span>
                    </template>
                </a-table-column>
                <a-table-column title="创建时间" :width="160">
                    <template #cell="{ record }">{{ fmtRFC(record.created) }}</template>
                </a-table-column>
                <!-- 固定列宽度按实际按钮核算：删除 ≈ 62 + 左右内边距 32 -->
                <a-table-column title="操作" :width="110" fixed="right" align="right">
                    <template #cell="{ record }">
                        <a-popconfirm
                            :content="record.inUse ? `「${record.name}」还被 ${record.inUse} 个容器挂着，删除会被 Docker 拒绝。仍要尝试？` : `确定删除存储卷「${record.name}」？卷里的数据会一起删除。`"
                            type="warning" position="lt" @ok="emit('remove', record)">
                            <a-button size="small" status="danger">
                                <template #icon><icon-delete /></template>删除
                            </a-button>
                        </a-popconfirm>
                    </template>
                </a-table-column>
            </template>
        </a-table>

        <a-modal :visible="createVisible" title="新建存储卷" :width="440" :on-before-ok="submit"
            @cancel="createVisible = false" @update:visible="createVisible = $event">
            <a-form :model="form" layout="vertical">
                <a-form-item field="name" label="卷名称" required>
                    <a-input v-model="form.name" placeholder="如 blog-data（字母/数字/下划线/点/短横线）" allow-clear />
                </a-form-item>
                <a-form-item field="driver" label="驱动">
                    <a-input v-model="form.driver" placeholder="留空 = local（本机目录）" allow-clear />
                </a-form-item>
                <div class="vol-tip">
                    Docker 用 named volume 管理数据时，先建卷再把容器挂到它上面；卷名要和容器里的挂载名一致。
                </div>
            </a-form>
        </a-modal>
    </div>
</template>

<script setup lang="ts">
import { reactive, ref } from "vue";
import { Message } from "@arco-design/web-vue";
import { fmtRFC } from "../helpers";

defineProps<{
    volumes: any[];
    loading: boolean;
}>();

const emit = defineEmits<{
    (e: "create", spec: { name: string; driver: string }): void;
    (e: "remove", record: any): void;
}>();

const createVisible = ref(false);
const form = reactive({ name: "", driver: "" });

function open() {
    form.name = "";
    form.driver = "";
    createVisible.value = true;
}

defineExpose({ open });

function submit(): boolean {
    const name = form.name.trim();
    if (!name) {
        Message.warning("卷名称不能为空");
        return false;
    }
    emit("create", { name, driver: form.driver.trim() });
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
    display: inline-block;
    vertical-align: bottom;
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 210px;
}

.inuse {
    color: rgb(var(--arcoblue-6));
}

.dim {
    color: var(--color-text-3);
}

.mono {
    font-family: Consolas, monospace;
}

.vol-tip {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    line-height: 1.6;
}
</style>
