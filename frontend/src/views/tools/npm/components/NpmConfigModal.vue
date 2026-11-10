<template>
    <!-- npm 配置（托管 .npmrc）：从主页面拆出，自持配置项 / registry 切换 / 原文编辑 / 同步用户全局。 -->
    <a-modal :visible="visible" title="npm 配置（托管 .npmrc）" :width="720" :footer="false" unmount-on-close
        @update:visible="(v: boolean) => emit('update:visible', v)">
        <div class="cfg-registry">
            <div class="ver-title">镜像源快捷切换</div>
            <div class="cfg-mirror">
                <a-button v-for="r in commonRegistries" :key="r.value" size="small"
                    :type="registry === r.value ? 'primary' : 'outline'" @click="applyRegistry(r.value)">
                    {{ r.key }}
                </a-button>
            </div>
            <a-input-search v-model="registryInput" class="cfg-reg-input" placeholder="自定义 registry URL"
                search-button button-text="应用" @search="applyRegistry(registryInput)" />
        </div>
        <a-divider style="margin: 12px 0" />
        <div class="ver-title">
            配置项
            <a-button size="small" @click="loadConfigItems">刷新</a-button>
        </div>
        <a-table :data="configItems" size="small" :pagination="false" row-key="key" :columns="[
            { title: 'Key', dataIndex: 'key', width: 220 },
            { title: 'Value', dataIndex: 'value', slotName: 'value' },
        ]">
            <template #value="{ record }">
                <a-input v-model="record.value" @press-enter="setItem(record)" @blur="setItem(record)" />
            </template>
        </a-table>
        <a-row class="add-local" :gutter="8">
            <a-col :span="8"><a-input v-model="newKey" placeholder="新增 Key" /></a-col>
            <a-col :span="12"><a-input v-model="newVal" placeholder="Value" /></a-col>
            <a-col :span="4"><a-button size="small" block @click="addConfigItem">新增</a-button></a-col>
        </a-row>
        <a-divider style="margin: 12px 0" />
        <a-collapse>
            <a-collapse-item header=".npmrc 原文编辑" key="raw">
                <a-textarea v-model="npmrcText" :auto-size="{ minRows: 8, maxRows: 14 }" spellcheck="false" />
                <a-space style="margin-top:8px">
                    <a-button size="small" @click="loadNpmrcText">重新读取</a-button>
                    <a-button size="small" @click="saveNpmrcText">保存 .npmrc</a-button>
                </a-space>
            </a-collapse-item>
        </a-collapse>
        <div class="user-npmrc">
            <div class="ver-title">用户全局 ~/.npmrc</div>
            <div class="offline-tip">{{ userNpmrcPath || '（未创建）' }}
                <a-button size="small" style="margin-left:8px" @click="applyToUser">把托管配置同步过去（自动备份）</a-button>
            </div>
        </div>
    </a-modal>
</template>

<script setup lang="ts">
import { getCurrentInstance, ref, watch } from "vue";
import { Message, Modal } from "@arco-design/web-vue";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const props = defineProps<{
    visible: boolean;
    /** 当前生效 registry：用于快捷切换高亮（与主页共享，改后回传给父） */
    registry: string;
}>();

const emit = defineEmits<{
    (e: "update:visible", v: boolean): void;
    /** registry 变更后通知父页面刷新总览里显示的 registry */
    (e: "update:registry", url: string): void;
}>();

const configItems = ref<any[]>([]);
const commonRegistries = ref<any[]>([]);
const registryInput = ref("");
const newKey = ref("");
const newVal = ref("");
const npmrcText = ref("");
const userNpmrcPath = ref("");

async function loadConfigItems() {
    try { configItems.value = ((await GoTools.NpmConfigItems()) || []).map((x: any) => ({ ...x })); }
    catch (e: any) { Message.error("读取 npm 配置失败: " + (e?.message || e)); }
}
async function loadNpmrcText() {
    try { npmrcText.value = (await GoTools.NpmGetConfigText()) || ""; } catch (e) { npmrcText.value = ""; }
}
async function saveNpmrcText() {
    try { await GoTools.NpmSaveConfigText(npmrcText.value); Message.success("托管 .npmrc 已保存"); loadConfigItems(); }
    catch (e: any) { Message.error("保存失败: " + (e?.message || e)); }
}
async function setItem(rec: any) {
    if (!rec.key) return;
    try {
        await GoTools.NpmSetConfigItem(rec.key, rec.value);
        if (rec.key === "registry") emit("update:registry", rec.value);
    } catch (e: any) { Message.error("设置失败: " + (e?.message || e)); }
}
async function addConfigItem() {
    const k = newKey.value.trim();
    if (!k) { Message.warning("请填写 Key"); return; }
    await setItem({ key: k, value: newVal.value.trim() });
    newKey.value = ""; newVal.value = "";
    loadConfigItems();
}
async function applyRegistry(url: string) {
    url = String(url || "").trim();
    if (!url) return;
    try {
        await GoTools.NpmSetRegistry(url);
        emit("update:registry", url);
        Message.success("registry 已切换：" + url);
        loadConfigItems();
    } catch (e: any) { Message.error("切换失败: " + (e?.message || e)); }
}
async function applyToUser() {
    Modal.warning({
        title: "同步到用户全局", hideCancel: false,
        content: "将托管 .npmrc 的 registry 等配置写入用户全局 ~/.npmrc（写入前自动备份原文件）。确定继续？",
        onOk: async () => {
            try {
                const bak = await GoTools.NpmApplyToUserNpmrc();
                Message.success("已同步" + (bak ? "，原文件备份至 " + bak : ""));
            } catch (e: any) { Message.error("同步失败: " + (e?.message || e)); }
        },
    });
}

// 打开时拉一遍配置：与原来 openNpmConfig 的时序一致
watch(() => props.visible, async (open) => {
    if (!open) return;
    loadConfigItems();
    loadNpmrcText();
    try { commonRegistries.value = (await GoTools.NpmCommonRegistries()) || []; } catch (e) { /* ignore */ }
    registryInput.value = props.registry || "";
    try {
        const res = await GoTools.NpmUserNpmrc();
        // bindings 多返回值：可能是数组或对象，兼容取第一个
        const p = Array.isArray(res) ? res[0] : (res?.path || res?.result1 || "");
        userNpmrcPath.value = p || "";
    } catch (e) { /* ignore */ }
});
</script>

<style scoped>
.cfg-registry {
    margin-bottom: calc(var(--base-padding) / 2);
}

/* 镜像源：弹性换行，间距随密度联动，选中态主色填充 */
.cfg-mirror {
    display: flex;
    flex-wrap: wrap;
    gap: calc(var(--base-padding) * 2 / 3);
}

.cfg-reg-input {
    margin-top: var(--base-padding);
    width: 460px;
    max-width: 100%;
}

.user-npmrc {
    margin-top: var(--base-padding);
}

/* 分区标题：左侧主色短竖条 + 正文级字号，取代裸文字标题，层级更清晰 */
.ver-title {
    position: relative;
    display: flex;
    align-items: center;
    gap: 8px;
    padding-left: 10px;
    font-weight: 600;
    font-size: var(--base-size-default);
    color: var(--color-text-1);
    margin-bottom: calc(var(--base-padding) * 3 / 4);
}

.ver-title::before {
    content: "";
    position: absolute;
    left: 0;
    top: 50%;
    transform: translateY(-50%);
    width: 3px;
    height: 13px;
    border-radius: 2px;
    background: rgb(var(--primary-6));
}

.add-local {
    margin-top: var(--base-padding);
}

.offline-tip {
    margin-top: calc(var(--base-padding) / 2);
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    word-break: break-all;
}
</style>
