<template>
    <div class="nt-root" :class="{ 'is-disabled': disabled }">
        
        <template v-for="(row, idx) in rows" :key="idx">
            <!-- 分隔线 -->
            <span v-if="row.kind === 'sep'" class="nt-sep"></span>

            <!-- 纯按钮组：用 a-dropdown 收纳低频按钮（更多 / 对齐 / 缩进） -->
            <a-dropdown v-else-if="row.kind === 'group'" trigger="click" position="bottom" :disabled="disabled">
                <a-button size="small" type="text" class="nt-btn nt-group">
                    {{ row.title }}<icon-down />
                </a-button>
                <template #content>
                    <a-doption v-for="ck in row.keys" :key="ck" :disabled="disabled || isDisabled(ck)"
                        :class="{ 'is-on': isActive(ck) }" @click="exec(ck)">
                        <span class="nt-ico" v-html="iconOf(ck)"></span>
                        <span class="nt-do-title">{{ titleOf(ck) }}</span>
                        <icon-check v-if="isActive(ck)" class="nt-do-check" />
                    </a-doption>
                </template>
            </a-dropdown>

            <!-- 叶子菜单：按实例特性自动判定类型 -->
            <template v-else>
                <!-- 按钮 -->
                <a-tooltip v-if="typeOf(row.k) === 'button'" :content="tipOf(row.k)">
                    <a-button size="small" type="text" class="nt-btn" :class="{ 'is-on': isActive(row.k) }"
                        :disabled="disabled || isDisabled(row.k)" @click="exec(row.k)">
                        <span class="nt-ico" v-html="iconOf(row.k)"></span>
                    </a-button>
                </a-tooltip>

                <!-- 下拉选择（标题 / 字号 / 字体 / 行高）：Arco a-select 是 inheritAttrs:false，
                     class 不会落到 flex 根上，width 也就失效而撑满整行；改用外层定宽 div + flex:none 收拢。
                     a-select 会把选中值硬编码成原生 title（input-label.js），与站内 a-tooltip 重复，用 v-strip-title 剥掉 -->
                <div v-else-if="typeOf(row.k) === 'select'" v-strip-title class="nt-selectbox"
                    :style="{ width: selectWidth(row.k) }">
                    <a-tooltip :content="titleOf(row.k)">
                        <a-select size="small" :disabled="disabled || isDisabled(row.k)" :model-value="valueOf(row.k)"
                            :options="optionsOf(row.k)" @change="(v: any) => selectExec(row.k, v)" />
                    </a-tooltip>
                </div>

                <!-- 下拉面板（取色 / 背景色 / 表情 / 表格 / 上传）：a-popover 承载 wangeditor 原生面板 DOM -->
                <a-popover v-else-if="typeOf(row.k) === 'droppanel'" trigger="click" position="bottom"
                    :popup-visible="panelOpen[row.k]" :content-style="{ padding: '8px' }"
                    @popup-visible-change="(v: boolean) => onPanelToggle(row.k, v)">
                    <a-button size="small" type="text" class="nt-btn" :disabled="disabled || isDisabled(row.k)">
                        <span class="nt-ico" v-html="iconOf(row.k)"></span>
                    </a-button>
                    <template #content>
                        <div class="nt-panel-host" :ref="(el: any) => setHost('p' + row.k, el)"
                            @click="onPanelClick(row.k)"></div>
                    </template>
                </a-popover>

                <!-- 模态（插入链接 / 图片 / 视频）：a-modal 承载 wangeditor 原生表单 DOM -->
                <template v-else-if="typeOf(row.k) === 'modal'">
                    <a-tooltip :content="tipOf(row.k)">
                        <a-button size="small" type="text" class="nt-btn" :disabled="disabled || isDisabled(row.k)"
                            @click="onModalToggle(row.k, true)">
                            <span class="nt-ico" v-html="iconOf(row.k)"></span>
                        </a-button>
                    </a-tooltip>
                    <a-modal :visible="modalOpen[row.k]" :title="titleOf(row.k)" :footer="false" :width="460"
                        @update:visible="(v: boolean) => onModalToggle(row.k, v)">
                        <div class="nt-modal-host" :ref="(el: any) => setHost('m' + row.k, el)"
                            @click="(e: MouseEvent) => onModalClick(e, row.k)"></div>
                    </a-modal>
                </template>
            </template>
        </template>
    </div>
</template>

<script setup lang="ts">
/**
 * 笔记富文本编辑器的自绘工具栏：用 Arco a-* 组件重建 wangeditor 的全部默认菜单。
 * 关键：不重写任何命令逻辑——复用隐藏 <Toolbar> 实例化出来的菜单对象
 * （DomEditor.getToolbar(editor).getMenus()），它们都实现了
 * getValue/isActive/isDisabled/exec（通用）+ getOptions（select）
 * + getPanelContentElem（drop-panel）+ getModalContentElem（modal），
 * 且这些方法只操作 editor、不依赖它自己的按钮 DOM。
 * 面板 / 弹窗内容仍是 wangeditor 原生 DOM，挂进 a-popover / a-modal 后靠 --w-e-* 变量主题化。
 */
import { computed, nextTick, onBeforeUnmount, reactive, ref, watch, type Directive } from "vue";
import { DomEditor, type IDomEditor } from "@wangeditor/editor";

const props = defineProps<{ editor?: IDomEditor; disabled?: boolean }>();

/**
 * 剥掉子树里的原生 title 属性：Arco a-select 把选中值硬编码成 title（input-label.js 里 mergeProps 后者胜），
 * 无法用 prop/attrs 关闭，且与站内 a-tooltip 重复。用 MutationObserver 覆盖首次渲染与每次选中重渲染。
 */
const stripTitle: Directive = {
    mounted(el: HTMLElement) {
        const clean = () => {
            el.querySelectorAll("[title]").forEach((n) => n.removeAttribute("title"));
        };
        clean();
        const mo = new MutationObserver(clean);
        mo.observe(el, { attributes: true, attributeFilter: ["title"], subtree: true, childList: true });
        (el as any).__stripMo = mo;
    },
    unmounted(el: HTMLElement) {
        const mo = (el as any).__stripMo as MutationObserver | undefined;
        mo?.disconnect();
    },
};

/** 布局：对齐 wangeditor genDefaultToolbarKeys（image/video 组拆成行内独立按钮，避免 dropdown 内嵌 overlay） */
type LayoutItem =
    | { sep: true }
    | { group: true; title: string; keys: string[] }
    | { k: string };
const LAYOUT: LayoutItem[] = [
    { k: "headerSelect" },
    { k: "blockquote" },
    { sep: true },
    { k: "bold" },
    { k: "underline" },
    { k: "italic" },
    { group: true, title: "更多", keys: ["through", "code", "sup", "sub", "clearStyle"] },
    { k: "color" },
    { k: "bgColor" },
    { sep: true },
    { k: "fontSize" },
    { k: "fontFamily" },
    { k: "lineHeight" },
    { sep: true },
    { k: "bulletedList" },
    { k: "numberedList" },
    { k: "todo" },
    { group: true, title: "对齐", keys: ["justifyLeft", "justifyRight", "justifyCenter", "justifyJustify"] },
    { group: true, title: "缩进", keys: ["indent", "delIndent"] },
    { sep: true },
    { k: "emotion" },
    { k: "insertLink" },
    { k: "insertImage" },
    { k: "uploadImage" },
    { k: "insertVideo" },
    { k: "uploadVideo" },
    { k: "insertTable" },
    { k: "codeBlock" },
    { k: "divider" },
    { sep: true },
    { k: "undo" },
    { k: "redo" },
    { sep: true },
    { k: "fullScreen" },
];

// 上传类面板含文件输入，点击内部不应自动收起；其余面板一次点击即完成动作，动作后收起
const NO_AUTO_CLOSE = new Set(["uploadImage", "uploadVideo"]);

// 把联合类型的 LAYOUT 归一为字段齐全、模板访问安全的 rows（避免 vue-tsc 对分支字段报错）
interface Row {
    kind: "sep" | "group" | "leaf";
    k: string;
    title: string;
    keys: string[];
}
const rows = computed<Row[]>(() =>
    LAYOUT.map((it): Row => {
        if ("sep" in it) return { kind: "sep", k: "", title: "", keys: [] };
        if ("group" in it) return { kind: "group", k: "", title: it.title, keys: it.keys };
        return { kind: "leaf", k: it.k, title: "", keys: [] };
    }),
);

const tick = ref(0);
const menus = ref<Record<string, any>>({});
const panelOpen = reactive<Record<string, boolean>>({});
const modalOpen = reactive<Record<string, boolean>>({});
const hosts: Record<string, HTMLElement | null> = {};

function setHost(name: string, el: any) {
    hosts[name] = (el as HTMLElement) || null;
}

function ed(): IDomEditor | undefined {
    return props.editor;
}

function typeOf(k: string): "button" | "select" | "droppanel" | "modal" {
    const m = menus.value[k];
    if (!m) return "button";
    if (typeof m.getOptions === "function") return "select";
    if (typeof m.getPanelContentElem === "function") return "droppanel";
    if (typeof m.getModalContentElem === "function") return "modal";
    return "button";
}

function isActive(k: string): boolean {
    void tick.value;
    const m = menus.value[k];
    const e = ed();
    if (!m || !e) return false;
    try {
        return !!m.isActive(e);
    } catch {
        return false;
    }
}

function isDisabled(k: string): boolean {
    void tick.value;
    const m = menus.value[k];
    const e = ed();
    if (!m || !e) return true;
    try {
        return !!m.isDisabled(e);
    } catch {
        return true;
    }
}

function valueOf(k: string): string {
    void tick.value;
    const m = menus.value[k];
    const e = ed();
    if (!m || !e) return "";
    try {
        const v = m.getValue(e);
        return typeof v === "string" ? v : "";
    } catch {
        return "";
    }
}

function optionsOf(k: string): { label: string; value: string }[] {
    void tick.value;
    const m = menus.value[k];
    const e = ed();
    if (!m || !e || typeof m.getOptions !== "function") return [];
    try {
        return (m.getOptions(e) as any[]).map((o) => ({ label: o.text, value: o.value }));
    } catch {
        return [];
    }
}

/** 各下拉选择按内容长短给固定宽，避免撑满整行；字体名最长故留宽 */
function selectWidth(k: string): string {
    switch (k) {
        case "headerSelect":
            return "88px";
        case "fontSize":
            return "96px";
        case "fontFamily":
            return "150px";
        case "lineHeight":
            return "104px";
        default:
            return "120px";
    }
}

function titleOf(k: string): string {
    const m = menus.value[k];
    return m?.title || k;
}

function tipOf(k: string): string {
    const m = menus.value[k];
    if (!m) return k;
    return m.hotkey ? `${m.title || k} (${m.hotkey})` : m.title || k;
}

function iconOf(k: string): string {
    const m = menus.value[k];
    return m?.iconSvg || "";
}

function exec(k: string) {
    const m = menus.value[k];
    const e = ed();
    if (!m || !e) return;
    try {
        m.exec(e, "");
    } catch {
        /* 忽略单命令异常 */
    }
}

function selectExec(k: string, v: string) {
    const m = menus.value[k];
    const e = ed();
    if (!m || !e) return;
    try {
        m.exec(e, v);
    } catch {
        /* 忽略 */
    }
}

function onPanelToggle(k: string, v: boolean) {
    panelOpen[k] = v;
    if (v) nextTick(() => mountPanel(k));
}

function mountPanel(k: string) {
    const m = menus.value[k];
    const e = ed();
    const host = hosts["p" + k];
    if (!m || !e || !host || typeof m.getPanelContentElem !== "function") return;
    try {
        host.replaceChildren(m.getPanelContentElem(e));
    } catch {
        /* 忽略 */
    }
}

function onPanelClick(k: string) {
    if (NO_AUTO_CLOSE.has(k)) return;
    panelOpen[k] = false;
}

function onModalToggle(k: string, v: boolean) {
    modalOpen[k] = v;
    if (v) nextTick(() => mountModal(k));
}

function mountModal(k: string) {
    const m = menus.value[k];
    const e = ed();
    const host = hosts["m" + k];
    if (!m || !e || !host || typeof m.getModalContentElem !== "function") return;
    try {
        host.replaceChildren(m.getModalContentElem(e));
    } catch {
        /* 忽略 */
    }
}

// 模态内 wangeditor 表单的确认/取消按钮执行完各自处理后会冒泡到这里，同步关闭我们的 a-modal
function onModalClick(e: MouseEvent, k: string) {
    const t = e.target as HTMLElement | null;
    if (t && t.closest("button")) modalOpen[k] = false;
}

function bump() {
    tick.value++;
}

async function loadMenus() {
    for (let i = 0; i < 20; i++) {
        const e = ed();
        if (e) {
            const tb = DomEditor.getToolbar(e as IDomEditor);
            const ms = tb && typeof tb.getMenus === "function" ? tb.getMenus() : null;
            if (ms && Object.keys(ms).length) {
                menus.value = ms as Record<string, any>;
                bump();
                return;
            }
        }
        await nextTick();
    }
}

let curEd: IDomEditor | null = null;
watch(
    () => props.editor,
    (e) => {
        if (curEd) {
            try {
                curEd.off("change", bump);
            } catch {
                /* 忽略 */
            }
        }
        curEd = e || null;
        if (e) {
            try {
                e.on("change", bump);
            } catch {
                /* 忽略 */
            }
        }
        void loadMenus();
    },
    { immediate: true },
);

onBeforeUnmount(() => {
    if (curEd) {
        try {
            curEd.off("change", bump);
        } catch {
            /* 忽略 */
        }
    }
});
</script>

<style scoped>
.nt-root {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px 3px;
    padding: 5px 8px;
    background-color: var(--color-fill-1);
}

.nt-btn {
    border-radius: var(--base-radius-default);
}

.nt-btn.is-on,
.nt-group.is-on {
    background-color: rgba(var(--primary-6), 0.14);
    color: rgb(var(--primary-6));
}

/* wangeditor 菜单图标是 svg 字符串，注入后统一继承按钮文字色、按字号缩放 */
.nt-ico {
    display: inline-flex;
    align-items: center;
    line-height: 0;
}

.nt-ico :deep(svg) {
    width: 1em;
    height: 1em;
    fill: currentColor !important;
}

.nt-ico :deep(svg path),
.nt-ico :deep(svg rect) {
    fill: currentColor !important;
}

.nt-selectbox {
    flex: none;
}

/* Arco a-select 根撑满定宽包裹器，与按钮行高对齐 */
.nt-selectbox :deep(.arco-select) {
    width: 100%;
}

.nt-sep {
    width: 1px;
    height: 20px;
    margin: 0 4px;
    background-color: var(--color-border-2);
}

.nt-do-title {
    margin-left: 6px;
}

.nt-do-check {
    margin-left: 8px;
    color: rgb(var(--primary-6));
}

/* 承载 wangeditor 原生面板 / 表单 DOM：把 --w-e-* 变量映射到主题令牌，
   弹层已 teleport 到 body，脱离 .ne-editor-wrap，必须在这里重新声明变量 */
.nt-panel-host,
.nt-modal-host {
    --w-e-toolbar-bg-color: var(--color-bg-5);
    --w-e-toolbar-color: var(--color-text-1);
    --w-e-toolbar-active-color: rgb(var(--primary-6));
    --w-e-toolbar-active-bg-color: var(--color-fill-2);
    --w-e-toolbar-disabled-color: rgb(var(--gray-5));
    --w-e-toolbar-border-color: var(--color-border-2);
    --w-e-textarea-bg-color: var(--color-bg-5);
    --w-e-textarea-color: var(--color-text-1);
    --w-e-textarea-border-color: var(--color-border-2);
    --w-e-textarea-slight-border-color: var(--color-border-1);
    --w-e-textarea-slight-color: var(--color-text-3);
    --w-e-textarea-slight-bg-color: var(--color-fill-2);
    --w-e-textarea-selected-border-color: rgb(var(--primary-3));
    --w-e-textarea-handler-bg-color: rgb(var(--primary-6));
    --w-e-modal-button-bg-color: var(--color-fill-2);
    --w-e-modal-button-border-color: var(--color-border-2);
}

.nt-panel-host :deep(.w-e-panel-content-color li),
.nt-panel-host :deep(.w-e-panel-content-emotion li) {
    color: var(--color-text-1);
}

.nt-modal-host :deep(.w-e-modal) {
    position: static;
    border: none;
    box-shadow: none;
    padding: 0;
    background: transparent;
}

.nt-modal-host :deep(.w-e-modal .btn-close) {
    display: none;
}
</style>
