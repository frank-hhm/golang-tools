<template>
    <layout-body>
        <a-space class="mt10">
            <a-button @click="onGetDevices">获取设备列表</a-button>
        </a-space>
        <div class="mt10">
            <a-table :data="deviceList" row-key="id" isLeaf :pagination="false" :table-layout-fixed="true">
                <template #columns>
                    <a-table-column title="名称" data-index="name" align="center" :width="400">
                        <template #cell="{ record }">
                            <div>{{ record.name }}</div>
                        </template>
                    </a-table-column>
                    <a-table-column title="ips" data-index="ips" :width="150">
                        <template #cell="{ record }">
                            <div v-for="(item, index) in record.ips">{{ item }}</div>
                        </template>
                    </a-table-column>
                    <a-table-column title="描述" data-index="description">
                        <template #cell="{ record }">
                            <div>{{ record.description }}</div>
                        </template>
                    </a-table-column>
                    <a-table-column title="pcapName" data-index="pcapName">
                        <template #cell="{ record }">
                            <div>{{ record.pcapName }}</div>
                        </template>
                    </a-table-column>

                    <a-table-column title="操作" align="center" :width="140">
                        <template #cell="{ record }">
                            <a-space>
                                <a-button size="mini" @click="onOpen(record)">连接</a-button>
                            </a-space>
                        </template>
                    </a-table-column>
                </template>
            </a-table>
        </div>
    </layout-body>
</template>
<script setup lang="ts">
import { getCurrentInstance, ref, onMounted, watch } from "vue";

const {
    proxy,
    proxy: { $utils, $GoTools },
} = getCurrentInstance() as any;


const deviceList = ref<any[]>([]);
const onGetDevices = () => {
    console.log("onGetDevices", $GoTools.GreetService);
    $GoTools.GetDevicesSniffer().then((res) => {
        console.log("res:", res);
        deviceList.value = res
    });
};

const selectedProtocols = ref<string[]>(['tcp', 'udp']);

const onOpen = (record: any) => {
    $GoTools.StartCaptureSniffer([record.pcapName], selectedProtocols.value, ['80']).then((res) => {
        console.log("res:", res);
    });
};

const packets = ref<any[]>([]);

onMounted(() => {
    onGetDevices();

    // 监听后端发来的 'packet:update' 事件
    // console.log("res:", $GoTools, $wailsRun);
    // $wailsRun.EventsOn('packet:update', (packetData) => {
    //     console.log('收到数据包:', packetData);
    //     // 将收到的数据包添加到列表中（注意：高频更新时注意性能）
    //     packets.value.push(packetData);
    // });
});
</script>
<style scoped></style>
