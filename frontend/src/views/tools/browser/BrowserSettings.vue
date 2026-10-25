<template>
    <!-- 浏览器设置抽屉：全局默认 + 空间列表。
         配置模型是「全局默认 + 空间覆盖」：空间里留空的项一律回落全局，
         所以这里每个空间字段都带「留空 = 跟随全局」的提示，不重复填值。 -->
    <a-drawer v-model:visible="innerVisible" :width="560" title="浏览器设置" :footer="true" :mask-closable="false"
        :unmount-on-close="false" @close="onClose">
        <a-alert class="bs-alert" type="info" :show-icon="true">
            空间是本程序的隔离单位：一空间一份 profile（登录态/缓存）+ 一套代理；
            同一空间里的多个标签共享登录态。代理与 UA 属于环境级参数，保存后需要重建网页区才生效。
        </a-alert>

        <!-- ==================== 全局默认 ==================== -->
        <a-card class="card-head-tool bs-card" size="small">
            <template #title>
                <span class="bs-title"><icon-settings /> 全局默认</span>
            </template>
            <template #extra>
                <a-tooltip content="空间里没单独设置的项都走这里的值">
                    <icon-question-circle class="bs-help" />
                </a-tooltip>
            </template>

            <a-form :model="draft" layout="vertical" size="small" class="bs-form">
                <a-row :gutter="8">
                    <a-col :span="8">
                        <a-form-item label="空间数量上限">
                            <a-input-number v-model="draft.maxSpaces" :min="1" :max="20" size="small"
                                :style="{ width: '100%' }" />
                        </a-form-item>
                    </a-col>
                    <a-col :span="16">
                        <a-form-item label="默认空间">
                            <a-select v-model="draft.defaultSpace" size="small" placeholder="启动时打开的空间"
                                :options="spaceOptions" />
                        </a-form-item>
                    </a-col>
                </a-row>

                <a-row :gutter="8">
                    <a-col :span="12">
                        <a-form-item label="默认首页">
                            <a-input v-model="draft.homepage" size="small" placeholder="about:blank" allow-clear />
                        </a-form-item>
                    </a-col>
                    <a-col :span="12">
                        <a-form-item label="搜索模板">
                            <a-input v-model="draft.searchURL" size="small" placeholder="https://www.bing.com/search?q=%s"
                                allow-clear />
                        </a-form-item>
                    </a-col>
                </a-row>

                <a-form-item label="代理模式">
                    <a-radio-group v-model="draft.proxyMode" type="button" size="small">
                        <a-radio value="system">跟随系统</a-radio>
                        <a-radio value="direct">直连</a-radio>
                        <a-radio value="custom">自定义代理</a-radio>
                    </a-radio-group>
                </a-form-item>

                <template v-if="draft.proxyMode === 'custom'">
                    <a-row :gutter="8">
                        <a-col :span="14">
                            <a-form-item label="代理地址">
                                <a-input v-model="draft.proxy" size="small" allow-clear
                                    placeholder="http://127.0.0.1:7890 或 socks5://127.0.0.1:1080" />
                            </a-form-item>
                        </a-col>
                        <a-col :span="10">
                            <a-form-item label="绕过列表">
                                <a-input v-model="draft.proxyBypass" size="small" allow-clear
                                    placeholder="localhost,127.0.0.1" />
                            </a-form-item>
                        </a-col>
                    </a-row>
                </template>

                <a-form-item label="自定义 UA">
                    <a-textarea v-model="draft.userAgent" size="small" :auto-size="{ minRows: 1, maxRows: 3 }"
                        placeholder="留空 = 默认 User-Agent" allow-clear />
                </a-form-item>
            </a-form>
        </a-card>

        <!-- ==================== 空间列表 ==================== -->
        <a-card class="card-head-tool bs-card" size="small">
            <template #title>
                <span class="bs-title"><icon-apps /> 空间</span>
                <span class="bs-count">{{ draft.spaces.length }} / {{ draft.maxSpaces }}</span>
            </template>
            <template #extra>
                <a-tooltip :content="atLimit ? `已达上限（${draft.maxSpaces} 个），可在上方调高` : '新建一个隔离空间'">
                    <a-button size="small" :disabled="atLimit" @click="newSpace">
                        <template #icon><icon-plus /></template>
                        新建空间
                    </a-button>
                </a-tooltip>
            </template>

            <a-collapse v-model:active-key="openKeys" :bordered="false" class="bs-collapse" destroy-on-hide>
                <a-collapse-item v-for="sp in draft.spaces" :key="sp.id">
                    <template #header>
                        <span class="bs-sp-head">
                            <span class="bs-sp-name">{{ sp.name || "未命名空间" }}</span>
                            <a-tag size="small" color="gray">{{ sp.id }}</a-tag>
                            <a-tag v-if="sp.id === draft.defaultSpace" size="small" color="arcoblue">默认</a-tag>
                            <a-tag size="small" color="green">{{ spaceProxyText(sp) }}</a-tag>
                            <a-tag v-if="sp.disableWebSecurity" size="small" color="orangered">同源关闭</a-tag>
                        </span>
                    </template>
                    <template #extra>
                        <a-popconfirm :content="`删除空间「${sp.name || sp.id}」？该空间的登录态目录会保留`"
                            :disabled="draft.spaces.length <= 1" @ok="removeSpace(sp.id)">
                            <a-tooltip :content="draft.spaces.length <= 1 ? '至少要保留一个空间' : '删除空间'">
                                <a-button size="small" status="danger" :disabled="draft.spaces.length <= 1">
                                    <template #icon><icon-delete /></template>
                                </a-button>
                            </a-tooltip>
                        </a-popconfirm>
                    </template>

                    <a-form :model="sp" layout="vertical" size="small" class="bs-form">
                        <a-row :gutter="8">
                            <a-col :span="12">
                                <a-form-item label="名称">
                                    <a-input v-model="sp.name" size="small" placeholder="如：工作 / 测试" allow-clear />
                                </a-form-item>
                            </a-col>
                            <a-col :span="12">
                                <a-form-item label="标识（profile 目录名）">
                                    <a-input v-model="sp.id" size="small" placeholder="小写字母/数字/-/_" allow-clear />
                                </a-form-item>
                            </a-col>
                        </a-row>

                        <a-form-item label="空间首页">
                            <a-input v-model="sp.homepage" size="small" allow-clear
                                :placeholder="`留空 = 跟随全局（${draft.homepage || 'about:blank'}）`" />
                        </a-form-item>

                        <a-row :gutter="8">
                            <a-col :span="12">
                                <a-form-item label="代理模式">
                                    <a-select v-model="sp.proxyMode" size="small" :options="spaceModeOptions" />
                                </a-form-item>
                            </a-col>
                            <a-col :span="12">
                                <a-form-item label="自定义 UA">
                                    <a-input v-model="sp.userAgent" size="small" allow-clear placeholder="留空 = 跟随全局" />
                                </a-form-item>
                            </a-col>
                        </a-row>

                        <template v-if="sp.proxyMode === 'custom'">
                            <a-row :gutter="8">
                                <a-col :span="14">
                                    <a-form-item label="代理地址">
                                        <a-input v-model="sp.proxy" size="small" allow-clear
                                            :placeholder="`留空 = 跟随全局（${draft.proxy || '未设置'}）`" />
                                    </a-form-item>
                                </a-col>
                                <a-col :span="10">
                                    <a-form-item label="绕过列表">
                                        <a-input v-model="sp.proxyBypass" size="small" allow-clear
                                            :placeholder="`留空 = 跟随全局`" />
                                    </a-form-item>
                                </a-col>
                            </a-row>
                        </template>

                        <a-form-item label="profile 目录">
                            <a-input v-model="sp.profileDir" size="small" allow-clear
                                placeholder="留空 = &lt;数据目录&gt;/browser/&lt;标识&gt;" />
                        </a-form-item>

                        <a-form-item>
                            <div class="bs-switch-row">
                                <a-switch v-model="sp.disableWebSecurity" size="small" />
                                <span class="bs-switch-label">关闭同源策略（仅调试用）</span>
                                <a-tooltip content="换空间或重启程序后生效；日常浏览务必保持关闭">
                                    <icon-question-circle class="bs-help" />
                                </a-tooltip>
                            </div>
                        </a-form-item>
                    </a-form>
                </a-collapse-item>
            </a-collapse>
        </a-card>

        <template #footer>
            <div class="bs-foot">
                <span class="bs-foot-tip">{{ draft.spaces.length }} / {{ draft.maxSpaces }} 个空间</span>
                <a-space :size="8">
                    <a-button size="small" @click="innerVisible = false">取消</a-button>
                    <a-button size="small" type="primary" :loading="saving" @click="save">保存</a-button>
                </a-space>
            </div>
        </template>
    </a-drawer>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";

/**
 * BrowserSettings 浏览器设置抽屉（M2）
 *
 * 数据流：打开时把父页面的 config 深拷贝成 draft（改一半取消不影响原配置），
 * 保存时整份交给 GoTools.BrowserSaveConfig，由后端归一化后落 config.json。
 * 空间里留空的项由后端 ResolveBrowserSpace 回落全局，所以这里不做合并。
 */
const props = defineProps<{ visible: boolean; config: any }>();
const emit = defineEmits<{
    (e: "update:visible", v: boolean): void;
    (e: "saved", cfg: any): void;
}>();

const {
    proxy: { $GoTools: GoTools },
} = getCurrentInstance() as any;

const saving = ref(false);
const openKeys = ref<string[]>([]);
const draft = ref<any>(emptyDraft());

/** 抽屉可见性转发给父组件的 v-model:visible */
const innerVisible = computed({
    get: () => props.visible,
    set: (v: boolean) => emit("update:visible", v),
});

const atLimit = computed(() => draft.value.spaces.length >= Number(draft.value.maxSpaces || 0));

const spaceOptions = computed(() =>
    (draft.value.spaces || []).map((s: any) => ({ label: s.name ? `${s.name}（${s.id}）` : s.id, value: s.id })),
);

/** 空间代理模式：值为空串表示「跟随全局」，跟后端归一化规则一致 */
const spaceModeOptions = computed(() => [
    { label: `跟随全局（${globalModeText(draft.value.proxyMode)}）`, value: "" },
    { label: "跟随系统", value: "system" },
    { label: "直连", value: "direct" },
    { label: "自定义代理", value: "custom" },
]);

function emptyDraft(): any {
    return {
        enabled: true,
        maxSpaces: 5,
        homepage: "about:blank",
        searchURL: "https://www.bing.com/search?q=%s",
        proxyMode: "system",
        proxy: "",
        proxyBypass: "",
        userAgent: "",
        defaultSpace: "",
        spaces: [],
    };
}

const clone = (v: any): any => JSON.parse(JSON.stringify(v ?? {}));

/** 全局代理模式的人话描述（用于「跟随全局」提示） */
function globalModeText(mode: string, proxy = ""): string {
    if (mode === "direct") return "直连";
    if (mode === "custom") return proxy ? `代理 ${proxy}` : "自定义代理未填";
    return "跟随系统";
}

/** 空间自己的代理描述：没单独设置时显式写出跟随的是哪一档 */
function spaceProxyText(sp: any): string {
    if (!sp.proxyMode) return `跟随全局：${globalModeText(draft.value.proxyMode, draft.value.proxy)}`;
    if (sp.proxyMode === "custom") return `代理 ${sp.proxy || draft.value.proxy || "未填"}`;
    return sp.proxyMode === "direct" ? "直连" : "跟随系统";
}

/** 打开时重灌草稿：取消过的修改不留到下一次 */
const resetDraft = () => {
    const src = clone(props.config);
    const d = emptyDraft();
    d.enabled = src.enabled !== false;
    d.maxSpaces = Number(src.maxSpaces) > 0 ? Number(src.maxSpaces) : 5;
    d.homepage = src.homepage || "about:blank";
    d.searchURL = src.searchURL || "https://www.bing.com/search?q=%s";
    d.proxyMode = ["system", "direct", "custom"].includes(src.proxyMode) ? src.proxyMode : "system";
    d.proxy = src.proxy || "";
    d.proxyBypass = src.proxyBypass || "";
    d.userAgent = src.userAgent || "";
    d.spaces = (src.spaces || []).map((s: any) => ({
        id: s.id || "",
        name: s.name || "",
        homepage: s.homepage || "",
        proxyMode: s.proxyMode || "",
        proxy: s.proxy || "",
        proxyBypass: s.proxyBypass || "",
        userAgent: s.userAgent || "",
        profileDir: s.profileDir || "",
        disableWebSecurity: !!s.disableWebSecurity,
    }));
    d.defaultSpace = d.spaces.some((s: any) => s.id === src.defaultSpace) ? src.defaultSpace : d.spaces[0]?.id || "";
    draft.value = d;
    openKeys.value = d.spaces.length ? [d.spaces[0].id] : [];
};

watch(
    () => props.visible,
    (v) => {
        if (v) resetDraft();
    },
    { immediate: true },
);

/** 上限调小到比现有空间少时，允许保存但给出明确提示（后端也会拒绝） */
const newSpace = () => {
    const d = draft.value;
    if (atLimit.value) {
        Message.warning(`空间数量已达上限（${d.maxSpaces} 个）`);
        return;
    }
    const used = new Set(d.spaces.map((s: any) => s.id));
    let seq = d.spaces.length + 1;
    while (used.has(`space${seq}`)) seq++;
    const sp = {
        id: `space${seq}`,
        name: `空间 ${seq}`,
        homepage: "",
        proxyMode: "",
        proxy: "",
        proxyBypass: "",
        userAgent: "",
        profileDir: "",
        disableWebSecurity: false,
    };
    d.spaces.push(sp);
    openKeys.value = [...openKeys.value, sp.id];
};

const removeSpace = (id: string) => {
    const d = draft.value;
    if (d.spaces.length <= 1) return;
    d.spaces = d.spaces.filter((s: any) => s.id !== id);
    if (d.defaultSpace === id) d.defaultSpace = d.spaces[0]?.id || "";
    openKeys.value = openKeys.value.filter((k) => k !== id);
};

const onClose = () => emit("update:visible", false);

const save = async () => {
    const d = draft.value;
    if (!d.spaces.length) {
        Message.warning("至少要有一个空间");
        return;
    }
    if (d.spaces.length > Number(d.maxSpaces || 0)) {
        Message.warning(`空间数量（${d.spaces.length}）超过上限（${d.maxSpaces}），先调高上限`);
        return;
    }
    if (!String(d.searchURL || "").includes("%s")) {
        Message.warning("搜索模板必须包含 %s（查询词占位）");
        return;
    }
    if (d.proxyMode === "custom" && !String(d.proxy || "").trim()) {
        Message.warning("全局代理选了「自定义代理」，但没填代理地址");
        return;
    }
    const seen = new Set<string>();
    for (const sp of d.spaces) {
        const id = String(sp.id || "").trim();
        const name = String(sp.name || "").trim();
        if (!name) {
            Message.warning("空间名称不能为空");
            return;
        }
        if (!/^[A-Za-z0-9_-]+$/.test(id)) {
            Message.warning(`空间标识「${id}」只能用字母/数字/-/_（它同时是 profile 目录名）`);
            return;
        }
        if (seen.has(id)) {
            Message.warning(`空间标识「${id}」重复了`);
            return;
        }
        seen.add(id);
        if (sp.proxyMode === "custom" && !String(sp.proxy || "").trim()) {
            Message.warning(`空间「${name}」选了自定义代理，但没填代理地址`);
            return;
        }
    }
    if (!seen.has(d.defaultSpace)) d.defaultSpace = d.spaces[0].id;

    saving.value = true;
    try {
        await GoTools.BrowserSaveConfig(d);
        Message.success("浏览器设置已保存");
        emit("saved", d);
        innerVisible.value = false;
    } catch (e: any) {
        Message.error(`保存失败：${String(e?.message || e)}`);
    } finally {
        saving.value = false;
    }
};
</script>

<style scoped>
/* 抽屉里两段卡片上下紧挨，靠间距而不是分割线区分 */
.bs-alert {
    margin-bottom: 8px;
    border-radius: var(--border-radius-medium);
}

.bs-card {
    margin-bottom: 8px;
    border-radius: var(--border-radius-medium);
}

.bs-title {
    display: inline-flex;
    align-items: center;
    gap: 6px;
}

.bs-count {
    margin-left: 8px;
    font-size: 12px;
    color: var(--color-text-3);
}

.bs-help {
    color: var(--color-text-3);
    cursor: help;
}

.bs-form :deep(.arco-form-item) {
    margin-bottom: 8px;
}

.bs-form :deep(.arco-form-item-label-col) {
    padding-bottom: 2px;
}

.bs-collapse :deep(.arco-collapse-item-header) {
    background: var(--color-fill-1);
    border-radius: var(--border-radius-small);
}

.bs-collapse :deep(.arco-collapse-item-content-box) {
    padding: 8px 0 0;
}

.bs-sp-head {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    max-width: 100%;
    overflow: hidden;
}

.bs-sp-name {
    max-width: 140px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 500;
}

.bs-switch-row {
    display: flex;
    align-items: center;
    gap: 8px;
}

.bs-switch-label {
    font-size: 12px;
    color: var(--color-text-2);
}

.bs-foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
}

.bs-foot-tip {
    font-size: 12px;
    color: var(--color-text-3);
}
</style>
