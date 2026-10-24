<template>
    <a-dropdown trigger="click" v-model:popup-visible="visible" ref="iconDropdown">
        <span></span>
        <template #content>
            <div class="select-icon-modal" >
                <div class="select-icon-search">
                    <a-radio-group v-model="curType" type="button" size="medium" class="select-icon-source">
                        <a-radio value="iconfont">图标库</a-radio>
                        <a-radio value="arco">Arco</a-radio>
                    </a-radio-group>
                    <a-input v-model="searchInput" placeholder="搜索图标">
                        <template #prefix>
                            <icon-search />
                        </template>
                    </a-input>
                </div>
                <div class="select-icon-list">
                    <template v-for="(item, index) in iconLists" :key="index">
                        <template v-if="Number(index) + 1 == curPage">
                            <template v-for="(items, idx) in item" :key="idx">
                                <a-tooltip>
                                    <template #content>
                                        <component v-if="isArco" :is="items.comp" class="select-icon-item-tip" />
                                        <i v-else class="select-icon-item-tip icon" :class="'icon-' + items.fontClass"></i>
                                    </template>
                                    <div class="select-icon-item" @click="selectItem(items)">
                                        <component v-if="isArco" :is="items.comp" />
                                        <i v-else class="icon" :class="'icon-' + items.fontClass"></i>
                                    </div>
                                </a-tooltip>
                            </template>
                        </template>
                    </template>
                    <div v-if="iconLists.length == 0" class="select-icon-empty">
                        无数据
                    </div>
                </div>
                <div class="select-icon-page">
                    <div class="select-icon-page-count">
                        <span id="select-icon-page-current">{{ curPage }}</span>
                        /<span id="select-icon-page-pages">{{ countPage }}</span> (共<span
                            id="select-icon-page-length">{{
        total }}</span>个)
                    </div>
                    <div class="select-icon-page-operate">
                        <div class="select-icon-close"></div>
                        <a-button size="small" @click="close">关闭</a-button>
                        <a-button class="ml10" size="small" @click="prevClick"
                            :disabled="curPage == 1 || countPage == 0">
                            <icon-left /></a-button>
                        <a-button class="ml10" size="small" @click="nextClick"
                            :disabled="countPage == curPage || countPage == 0"><icon-right /></a-button>
                    </div>
                </div>
            </div>
        </template>
    </a-dropdown>
</template>
<script lang="ts">
export default {
    name: "select-icon-modal",
};
</script>
<script setup lang="ts">
import { computed, ref, watch } from "vue";
import iconData from "./icon.json";
import * as ArcoIconComps from "@arco-design/web-vue/es/icon";

const props = withDefaults(
    defineProps<{
        /** 图标源初始类型：iconfont（本地字体图标，默认）或 arco（Arco Design 图标库）；面板内可切换 */
        type?: "iconfont" | "arco";
    }>(),
    {
        type: "iconfont",
    }
);

// 当前图标源（可由 type 属性初始化，也可在面板内通过单选切换）
const curType = ref<"iconfont" | "arco">(props.type);
const isArco = computed(() => curType.value === "arco");

const visible = ref<boolean>(false);

const emit = defineEmits(["change"]);

const open = () => {
    init();
    visible.value = true;
};
const close = () => {
    visible.value = false;
};

const init = () => {
    searchInput.value = "";
    curPage.value = 1;
    // 每次打开重置为外部指定的初始图标源
    curType.value = props.type;
};

/* ---------------- 图标源：iconfont 本地 json + Arco 图标库 ---------------- */
interface IconItem {
    label: string; // 搜索/提示展示文本
    value: string; // 选中后 emit 的值
    fontClass?: string; // iconfont 模式：class 后缀
    comp?: any; // arco 模式：图标组件
}

/** IconClockCircle -> clock-circle */
const toKebab = (pascal: string) =>
    pascal.replace(/([a-z0-9])([A-Z])/g, "$1-$2").toLowerCase();

const arcoIcons: IconItem[] = Object.keys(ArcoIconComps)
    .filter((k) => /^Icon[A-Z]/.test(k))
    .map((k) => {
        const kebab = toKebab(k.slice(4));
        return { label: kebab, value: kebab, comp: (ArcoIconComps as any)[k] };
    });

const iconfontIcons: IconItem[] = (iconData as any[]).map((o) => ({
    label: `${o.name ?? ""} ${o.font_class ?? ""}`.trim(),
    value: "icon-" + o.font_class,
    fontClass: o.font_class,
}));

const sourceIcons = computed<IconItem[]>(() =>
    isArco.value ? arcoIcons : iconfontIcons
);

/* ---------------- 查询 & 分页 ---------------- */
const searchInput = ref<string>("");
const total = ref<number>(0);
const pageLimit = 24;
const countPage = ref<number>(0);
const curPage = ref<number>(1);

// 当前总数据（过滤后）
const icons = ref<IconItem[]>([]);
// 当前分页数据（按 pageLimit 分块）
const iconLists = ref<IconItem[][]>([]);

//上一页
const prevClick = () => {
    if (curPage.value > 1 && countPage.value > 0) {
        curPage.value--;
    }
};
//下一页
const nextClick = () => {
    if (curPage.value < countPage.value && countPage.value > 0) {
        curPage.value++;
    }
};

const getIcons = () => {
    iconLists.value = [];
    for (var i = 0; i < countPage.value; i++) {
        // 按limit分块
        let lm: IconItem[] = [];
        for (
            var j = i * pageLimit;
            j < (i + 1) * pageLimit && j < total.value;
            j++
        ) {
            lm.push(icons.value[j]);
        }
        iconLists.value.push(lm);
    }
};

const getData = () => {
    icons.value = [];
    const kw = (searchInput.value || "").toLowerCase();
    const list = sourceIcons.value;
    for (var i = 0; i < list.length; i++) {
        var obj = list[i];
        // 判断是否模糊查询
        if (
            kw &&
            obj.label.toLowerCase().indexOf(kw) === -1 &&
            obj.value.toLowerCase().indexOf(kw) === -1
        ) {
            continue;
        }
        icons.value.push(obj);
    }
    total.value = icons.value.length;
    countPage.value =
        total.value % pageLimit === 0
            ? total.value / pageLimit
            : parseInt(String(total.value / pageLimit + 1));
    curPage.value = 1;
    getIcons();
};

getData();

watch(
    () => searchInput.value,
    () => {
        getData();
    }
);

// 外部 type 变化时同步当前图标源
watch(
    () => props.type,
    (val) => {
        curType.value = val;
    }
);

// 切换图标源（单选切换或外部同步）时重新加载
watch(
    () => curType.value,
    () => {
        getData();
    }
);

const selectItem = (item: IconItem) => {
    emit("change", item.value);
    close();
};

defineExpose({ open, close });
</script>
<style scoped>
.select-icon-modal {
    width: 320px;
    height: 198px;
    overflow: hidden;
}
.select-icon-modal.mobile{
    width: 260px;
}

.select-icon-search {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 10px 5px;
}

.select-icon-search :deep(.select-icon-source) {
    flex: none;
}

.select-icon-search :deep(.arco-input-wrapper) {
    flex: 1;
    min-width: 0;
}

.select-icon-page {
    border-top: 1px solid var(--color-border-1);
    height: 30px;
    line-height: 30px;
    padding: 10px 0 0;
    margin: 5px 10px 0;
}

.select-icon-page .select-icon-page-count {
    display: inline-block;
    font-size: 12px;
    color: var(--color-text-3);
}

.select-icon-page .select-icon-page-operate {
    display: flex;
    float: right;
    cursor: default;
}

.select-icon-list {
    margin: 0 5px;
    height: 108px;
}

.select-icon-item {
    display: inline-block;
    width: calc(12.4% - 10px);
    line-height: 26px;
    text-align: center;
    cursor: pointer;
    vertical-align: top;
    height: 26px;
    margin: 4px;
    border: 1px solid var(--color-border-1);
    border-radius: 2px;
    color: var(--color-text-1);
    font-size: 16px;
}

.select-icon-item .icon {
    font-size: 14px;
}

.select-icon-empty {
    text-align: center;
    line-height: 60px;
    color: var(--color-text-3);
}

.select-icon-item:hover {
    border-color: rgba(var(--primary-6));
    background: rgba(var(--primary-6));
    color: var(--color-white);
}

.select-icon-item-tip {
    font-size: 40px;
}
</style>