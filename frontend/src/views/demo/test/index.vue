<template>
    <!-- 测试/演示入口集中页：主页那个入口只在 dev 显示，页面与路由本身打包后仍在（只是没入口） -->
    <!-- 不用 height="full"：那种模式 LayoutBody 是 height:100% + overflow:hidden（主页自己管内部滚动），
         普通长页面用了会把超出部分裁掉、外层 .layout-content 的滚动条永不出现 -->
    <layout-body title="组件测试">
        <div class="hub-wrap">
            <div class="hub-tip">
                开发期的组件测试台集中从这里进。以后有新的公共组件要验，往下追加一项即可（内联的直接放，路由型的在
                <code>router/modules/base.ts</code> 补 <code>/demo/*</code> 与 <code>/win/demo/*</code> 两条路由）。
            </div>

            <common-item-card v-for="it in items" :key="it.path" class="hub-row" :clickable="false" pad="12px">
                <template #status>
                    <span class="hub-dot" :class="it.inline ? 'is-inline' : 'is-route'"></span>
                </template>
                <template #title>
                    <span class="hub-name">{{ it.name }}</span>
                    <a-tag size="small" :color="it.inline ? 'arcoblue' : 'purple'">
                        {{ it.inline ? "本页内联" : "跳转页面" }}
                    </a-tag>
                </template>
                <template #extra>
                    <a-space>
                        <a-button size="mini" @click="openWin(it)">新窗口打开</a-button>
                        <a-button v-if="!it.inline" size="mini" type="primary" @click="openHere(it)">本页打开</a-button>
                    </a-space>
                </template>
                <div class="hub-desc">{{ it.desc }}</div>
            </common-item-card>

            <!-- 内联项：切到「任务进度组件」才挂载，避免其它项浏览时白跑一个定时器 -->
            <task-progress-demo v-if="activeInline" class="hub-inline" />
        </div>
    </layout-body>
</template>

<script setup lang="ts">
/**
 * 组件测试台入口列表。
 *
 * 数据驱动：新增一个测试项只往 items 里加一条（inline=true 就把组件挂在本页，
 * 否则给一条 /demo/* 路由）。主页的入口按钮只在 dev 显示，这里不重复判环境——
 * 打包后没有入口，但路由仍在，需要时手输地址照样能开。
 */
import { ref } from "vue";
import { useRouter } from "vue-router";
import TaskProgressDemo from "./TaskProgressDemo.vue";
import { useToolWindow } from "@/views/index/useToolWindow";

interface TestItem {
    name: string;
    desc: string;
    /** 常规路由路径；openToolWindow 在桌面端会自动加 /win 前缀开原生窗 */
    path: string;
    /** true = 组件直接嵌在本页下方，不开新页 */
    inline?: boolean;
}

const items: TestItem[] = [
    {
        name: "common-task-progress 任务进度行",
        desc: "六态 + 不定进度、plain/bare 两形态、divided / show-text 开关、#meta / #left / #actions 插槽、实时模拟任务（速度、ETA、默认动作切换）。",
        path: "/demo/test",
        inline: true,
    },
    {
        name: "Go → Arco Message 消息桥接",
        desc: "验证后端 Events  emitted 的消息能在前端落成 Arco Message（未全局推广，仅供对照）。",
        path: "/demo/gomessage",
    },
    {
        name: "common-canvas-clock 时钟组件",
        desc: "Canvas 指针时钟：预设配色、自定义配色弹窗、参数说明。",
        path: "/demo/clock",
    },
];

const router = useRouter();
const { openToolWindow } = useToolWindow();
const activeInline = ref<TestItem | null>(items.find((i) => i.inline) || null);

function openWin(it: TestItem) {
    if (it.inline) { activeInline.value = it; return; }
    void openToolWindow(it.path, it.name, { single: true, width: 1180, height: 820 });
}

function openHere(it: TestItem) {
    void router.push(it.path);
}
</script>

<style scoped>
.hub-wrap {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: calc(var(--base-padding) / 2);
}

.hub-tip {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    line-height: 1.7;
}

.hub-tip code {
    padding: 1px 5px;
    border-radius: var(--base-radius-small);
    background: var(--color-fill-2);
    color: var(--color-text-2);
    font-family: Consolas, Monaco, "Courier New", monospace;
    font-size: var(--base-size-small);
}

.hub-row {
    border-radius: var(--base-radius-default);
}

.hub-dot {
    display: block;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--color-fill-4);
}

.hub-dot.is-inline {
    background: rgb(var(--arcoblue-6));
}

.hub-dot.is-route {
    background: rgb(var(--purple-6));
}

.hub-name {
    font-weight: 600;
    color: var(--color-text-1);
}

.hub-desc {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    line-height: 1.7;
}

.hub-inline {
    padding: calc(var(--base-padding) / 2);
    border: 1px dashed var(--color-border-2);
    border-radius: var(--base-radius-default);
}
</style>
