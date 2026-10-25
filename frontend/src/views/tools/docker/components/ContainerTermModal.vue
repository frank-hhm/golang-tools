<template>
    <!-- 容器终端：弹窗只是“外壳”，终端本体是通用组件 term-console，
         换一个 openSession（SSH → 容器）就复用了，行内按钮和总览卡片都能挂同一个入口 -->
    <a-modal :visible="visible" :width="1000" :footer="false" :body-style="{ height: '560px', padding: '8px' }"
        @cancel="close">
        <template #title>
            <a-space :size="6">
                <com-badge :status="dead ? 'normal' : 'success'" :animated="!dead" />
                <span>容器终端 · {{ name || id.slice(0, 12) }}</span>
                <a-tag v-if="dead" size="small" color="gray">已断开</a-tag>
                <a-button v-if="dead" size="small" @click="reconnect">
                    <template #icon><icon-refresh /></template>重新连接
                </a-button>
            </a-space>
        </template>

        <!-- :key 换一次等于重建一条会话（重新连接时用）；关弹窗会先主动 TermClose 再卸载 -->
        <div class="term-body">
            <term-console ref="consoleRef" v-if="visible" :key="runKey" :open-session="openSession" @ready="onReady"
                @exit="onExit" />
        </div>
    </a-modal>
</template>

<script setup lang="ts">
import { getCurrentInstance, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import TermConsole from "@/components/business/term-console/index.vue";

const props = defineProps<{
    visible: boolean;
    /** 容器 ID（exec 接口接受短 ID） */
    id: string;
    /** 容器名，仅用于标题 */
    name?: string;
}>();

const emit = defineEmits<{ (e: "update:visible", v: boolean): void }>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

/** 重建终端用的 key：点「重新连接」自增一次 */
const runKey = ref(0);
/** 终端组件句柄：关弹窗时直接让它结束会话，不等卸载的时机 */
const consoleRef = ref<{ closeSession: () => void } | null>(null);
/** 会话是否断开（断开后标题栏给一个重连入口） */
const dead = ref(false);
/** 会话是否连上过：连都没连上按错误提示，中途断开按警告提示 */
let connected = false;

/** 交给 term-console 的建会话回调：容器场景就是 docker exec */
function openSession(cols: number, rows: number) {
    return GoTools.TermOpenDocker(props.id, props.name || "", cols, rows) as Promise<{ id: string }>;
}

function onReady() {
    connected = true;
    dead.value = false;
}

function onExit(reason: string) {
    dead.value = true;
    const first = !connected;
    connected = false;
    if (!props.visible) return;
    const content = reason || (first ? "无法进入容器终端" : "容器终端已断开");
    if (first) Message.error({ content, position: "top", duration: 3000 });
    else Message.warning({ content, position: "top", duration: 3000 });
}

function reconnect() {
    dead.value = false;
    connected = false;
    runKey.value += 1;
}

function close() {
    // 弹窗只是外壳：先断会话（关掉远端 shell），再收起弹窗
    consoleRef.value?.closeSession();
    emit("update:visible", false);
}

// 从外面把 visible 置 false（父组件直接改 v-model）时也要断，不能只靠组件卸载
watch(
    () => props.visible,
    (v) => {
        if (!v) {
            consoleRef.value?.closeSession();
            return;
        }
        dead.value = false;
        connected = false;
    },
);
</script>

<style scoped>
/* 只做一件事：把弹窗正文的高度交给终端组件（xterm 自己按容器算行列） */
.term-body {
    height: 100%;
}
</style>
