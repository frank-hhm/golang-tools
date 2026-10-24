<template>
    <div class="page-pager">
        <a-pagination v-model:current="page" v-model:page-size="limit" :total="total"
            :page-size-options="pageSizeOptions" size="mini" show-total show-page-size @change="changeCurPage"
            @page-size-change="changePageLimit" />
    </div>
</template>
<script lang="ts">
export default {
    name: "common-pager",
};
</script>
<script lang="ts" setup>
import { ref, watch } from "vue";
import { PageLimitType } from "@/types";
import { useSetting } from "@/hooks/useSetting";

const props = withDefaults(
    defineProps<{
        /** 分页数据源：{ total, page, limit? } */
        listPage: PageLimitType;
        /** 可选的每页条数 */
        pageSizeOptions?: number[];
    }>(),
    {
        listPage: () => ({ total: 0, page: 1, limit: 10 }),
        pageSizeOptions: () => [10, 20, 50, 100, 200],
    }
);

const emit = defineEmits<{
    (e: "change", payload: { total: number; page: number; limit: number }): void;
}>();

const setting = useSetting();

// 内部状态：初始化优先使用 props.listPage.limit，其次回退到全局设置记忆值
const total = ref<number>(props.listPage.total);
const page = ref<number>(props.listPage.page || 1);
const limit = ref<number>(props.listPage.limit || setting.PageLimit.value || 10);

/** 向父级抛出最新分页参数 */
const refresh = () => {
    emit("change", { total: total.value, page: page.value, limit: limit.value });
};

/** 页码变化 */
const changeCurPage = (val: number) => {
    page.value = val;
    refresh();
};

/** 每页条数变化：记忆到全局设置并回到第一页 */
const changePageLimit = (val: number) => {
    limit.value = val;
    setting.setPageLimit(val);
    page.value = 1;
    refresh();
};

// 外部数据源变化时同步 total / page（limit 由组件自身维护，不回写避免与用户操作冲突）
watch(
    () => props.listPage,
    () => {
        total.value = props.listPage.total;
        page.value = props.listPage.page || 1;
    },
    { deep: true }
);
</script>
