<template>
    <!--
        共享代理池 / 镜像池的行编辑器：逐行「名称 + URL + 删除」，末尾一个添加按钮。
        独立成子组件的原因：设置主页的 Volar 不把 v-for 条目变量泄进 @ 事件处理器作用域
        （:key / :model-value 这些 prop 绑定能解析，handler 里就报“Cannot find name”），
        而这里 v-model 直接写 rows 条目属性由 SFC 编译器就地处理，跨不作用域问题。
        rows 是父层 config.Download.ProxyPool / Mirrors 的草稿副本，父组件保存时回读。
    -->
    <div class="pe-rows">
        <div v-for="(row, i) in rows" :key="row.id" class="pe-row">
            <a-input v-model="row.name" :placeholder="namePlaceholder" :style="{ width: '130px' }" allow-clear />
            <a-input v-model="row.url" :placeholder="urlPlaceholder" allow-clear />
            <a-button size="mini" status="danger" @click="rows.splice(i, 1)">删除</a-button>
        </div>
        <a-button size="mini" @click="addRow">+ {{ addLabel }}</a-button>
    </div>
</template>

<script setup lang="ts">
export interface PoolRow {
    id: string;
    name: string;
    url: string;
}

const props = defineProps<{
    rows: PoolRow[];
    namePlaceholder?: string;
    urlPlaceholder?: string;
    addLabel?: string;
}>();

let seq = 0;
function addRow() {
    props.rows.push({ id: `r${Date.now()}${seq++}`, name: "", url: "" });
}
</script>

<style scoped>
.pe-rows {
    display: flex;
    flex-direction: column;
    gap: 8px;
    width: 100%;
}

.pe-row {
    display: flex;
    align-items: center;
    gap: 8px;
}

.pe-row .arco-input-wrapper {
    flex: 1;
}
</style>
