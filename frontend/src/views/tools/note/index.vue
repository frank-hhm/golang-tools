<template>
    <div class="note-page">
        <!-- 左：笔记列表（数据来自文档中心「笔记」分类，实体落库内 notes/） -->
        <a-card class="np-side card-head-tool" size="small" :body-style="{ padding: 0 }">
            <template #title>笔记</template>
            <template #extra>
                <a-space :size="6">
                    <a-button size="small" :disabled="!selected" @click="removeSelected"><icon-delete /></a-button>
                    <a-button size="small" @click="reload"><icon-refresh /></a-button>
                    <a-button size="small"  @click="openNew"><template #icon><icon-plus /></template>新建</a-button>
                </a-space>
            </template>
            <div class="np-search">
                <a-input v-model="keyword"  placeholder="搜索笔记名称" allow-clear>
                    <template #suffix><icon-search /></template>
                </a-input>
            </div>
            <div class="np-list">
                <div v-if="loading" class="np-state"><a-spin /></div>
                <template v-else>
                    <common-item-card v-for="it in filtered" :key="it.id" class="np-item"
                        :active="!!selected && selected.id === it.id" @click="select(it)">
                        <template #status>
                            <common-file-icon :is-dir="false" :ext="extOf(it.name)" :size="18" />
                        </template>
                        <template #title>
                            <span class="np-item-name" :title="it.path">{{ it.name }}</span>
                        </template>
                        <span class="np-item-meta">{{ fmtAgo(it.updatedAt) }}</span>
                    </common-item-card>
                    <a-empty v-if="!filtered.length" :image="false" :description="keyword ? '无匹配笔记' : '还没有笔记，点右上新建'" />
                </template>
            </div>
        </a-card>

        <!-- 右：编辑区；未选中给空态 -->
        <a-card class="np-main card-head-tool" :body-style="{ padding: 0 }">
            <note-editor v-if="selected" :key="selected.path" :path="selected.path" :name="selected.name"
                @saved="afterSaved" />
            <div v-else class="np-empty">
                <a-empty description="从左侧选择一个笔记，或新建一个" />
            </div>
        </a-card>

        <!-- 新建笔记：名称 + 格式（Markdown / 富文本），建完直接选中进入编辑 -->
        <a-modal v-model:visible="newModal.open" title="新建笔记" :width="480" :ok-loading="newModal.saving" @ok="submitNew">
            <a-form :model="newModal" layout="vertical">
                <a-form-item field="name" label="名称">
                    <a-input v-model="newModal.name" placeholder="留空自动生成带时间戳的标题；无需后缀" />
                </a-form-item>
                <a-form-item field="format" label="格式">
                    <a-radio-group v-model="newModal.format" type="button" class="np-format">
                        <a-radio value="md">Markdown（.md）</a-radio>
                        <a-radio value="html">富文本（.html）</a-radio>
                    </a-radio-group>
                    <template #extra>
                        <div class="np-dim">笔记实体落库内 notes/ 目录，归入文档中心「笔记」分类；Markdown 用所见即所得编辑、保存回转 Markdown，富文本为无损 HTML。</div>
                    </template>
                </a-form-item>
            </a-form>
        </a-modal>
    </div>
</template>

<script setup lang="ts">
/**
 * 笔记工具页：从「文档中心」拆出的独立编辑模块。
 * 左列笔记清单（复用文档中心 DocSpace* 绑定，数据源仍是库内 notes/ 的「笔记」分类条目），
 * 右侧 NoteEditor 用 wangeditor 所见即所得，按后缀分派 md/html 存储。
 * 作为单例窗口被拉起时：新窗口靠 ?path= 定位，已开着靠 note:open-path 事件导航——与查看器同一套机制。
 */
import { computed, getCurrentInstance, onMounted, onUnmounted, reactive, ref } from "vue";
import { useRoute } from "vue-router";
import { Message, Modal } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import NoteEditor from "./components/NoteEditor.vue";
import { errText, fmtAgo, type Category, type DocItem } from "@/views/tools/docspace/types";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const route = useRoute();

const loading = ref(false);
const items = ref<DocItem[]>([]);
const keyword = ref("");
const selected = ref<DocItem | null>(null);
const newModal = reactive({ open: false, saving: false, name: "", format: "md" });

const extOf = (name: string): string => {
    const i = name.lastIndexOf(".");
    return i >= 0 ? name.slice(i + 1).toLowerCase() : "";
};

/** 「笔记」根分类及其全部子孙的 id 集合；笔记只认这个子树。 */
function noteCategoryIds(cats: Category[]): Set<string> {
    const root = cats.find((c) => c.name === "笔记" && !c.parent);
    const set = new Set<string>();
    if (!root) return set;
    set.add(root.id);
    // 逐层收子孙（笔记分类一般不深，简单两趟即可）
    for (let pass = 0; pass < cats.length; pass++) {
        let grew = false;
        for (const c of cats) {
            if (c.parent && set.has(c.parent) && !set.has(c.id)) { set.add(c.id); grew = true; }
        }
        if (!grew) break;
    }
    return set;
}

const filtered = computed(() => {
    const kw = keyword.value.trim().toLowerCase();
    const list = kw ? items.value.filter((it) => it.name.toLowerCase().includes(kw)) : items.value;
    return [...list].sort((a, b) => (b.updatedAt || 0) - (a.updatedAt || 0));
});

async function reload() {
    loading.value = true;
    try {
        const cats = ((await GoTools?.DocSpaceCategories()) ?? []) as Category[];
        const ids = noteCategoryIds(cats);
        const all = ((await GoTools?.DocSpaceItems()) ?? []) as DocItem[];
        items.value = all.filter((it) => it.category && ids.has(it.category));
    } catch (e: any) {
        Message.error(`载入笔记失败：${errText(e)}`);
    } finally {
        loading.value = false;
    }
}

function select(it: DocItem) {
    selected.value = it;
}

function openNew() {
    newModal.name = "";
    newModal.format = "md";
    newModal.open = true;
}

async function submitNew() {
    newModal.saving = true;
    try {
        const it = (await GoTools?.DocSpaceNewNote(newModal.name.trim(), newModal.format)) as DocItem | undefined;
        newModal.open = false;
        await reload();
        if (it) selected.value = it;
    } catch (e: any) {
        Message.error(`新建笔记失败：${errText(e)}`);
    } finally {
        newModal.saving = false;
    }
}

function removeSelected() {
    const it = selected.value;
    if (!it) return;
    Modal.confirm({
        title: "删除笔记",
        content: `把「${it.name}」移入回收站？（可在文档中心回收站还原）`,
        onOk: async () => {
            try {
                await GoTools?.DocSpaceRemoveItem(it.id);
                selected.value = null;
                await reload();
                Message.success("已移入回收站");
            } catch (e: any) {
                Message.error(`删除失败：${errText(e)}`);
            }
        },
    });
}

async function afterSaved() {
    // 保存后刷新更新时间，让列表排序反映最新改动
    await reload();
}

/** 按 path 定位并选中某条笔记（跨页拉起用）；找不到就按名补进列表再选。 */
async function openPath(path: string) {
    if (!path) return;
    let it = items.value.find((x) => x.path === path) || null;
    if (!it) {
        await reload();
        it = items.value.find((x) => x.path === path) || null;
    }
    if (it) selected.value = it;
}

let offOpenPath: any = null;
onMounted(async () => {
    try {
        offOpenPath = Events.On("note:open-path", (ev: any) => {
            const p = ev?.data ?? ev;
            if (p?.path) void openPath(String(p.path));
        });
    } catch { /* 非 Wails 环境忽略 */ }
    await reload();
    const q: any = route.query;
    if (q.path) void openPath(String(q.path));
});

onUnmounted(() => {
    if (offOpenPath) offOpenPath();
});
</script>

<style scoped>
.note-page {
    display: flex;
    height: 100%;
    min-height: 0;
    gap: var(--base-padding);
    box-sizing: border-box;
    /* 整页不滚动：只有左侧列表与右侧编辑内容各自内部滚动 */
    overflow: hidden;
}

/* 卡片外壳（背景/描边/圆角）交给 a-card 的主题表面，自动随玻璃/纹理/描边/密度联动 */
.np-side {
    width: 260px;
    flex-shrink: 0;
    display: flex;
    flex-direction: column;
    min-height: 0;
}

.np-side :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

.np-search {
    flex: none;
    padding:0 var(--base-padding) calc(var(--base-padding) * 0.5);
}

.np-list {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: calc(var(--base-padding) / 2);
    padding: 0 calc(var(--base-padding) / 2) var(--base-padding);
}

.np-state {
    display: flex;
    justify-content: center;
    padding: 16px 0;
}

/* 卡片外壳（边框/圆角/选中短竖条/hover 投影）由 common-item-card 提供，这里只留内容样式 */
.np-item-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--base-size-large);
    color: var(--color-text-1);
}

.np-item.is-active .np-item-name {
    font-weight: 600;
}

.np-item-meta {
    font-size: 12px;
    color: rgb(var(--gray-5));
}

.np-main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
}

.np-main :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

.np-empty {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
}

.np-dim {
    font-size: 12px;
    color: var(--color-text-3);
    line-height: 1.6;
}

/* 格式分段按钮：标签不换行（之前与右侧说明挤同一行被压成两行） */
.np-format :deep(.arco-radio) {
    white-space: nowrap;
}
</style>
