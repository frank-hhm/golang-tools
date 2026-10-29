<template>
    <!-- 本地服务：实例编辑。从主页面拆出，自持表单 / 镜像候选 / 引擎状态 / 端口探测 / 保存与切换重启确认。 -->
    <a-modal :visible="visible" :title="instForm.id ? '编辑实例' : '新建实例'" :width="520"
        :body-style="{ maxHeight: '72vh' }" :on-before-ok="saveInstance"
        @update:visible="(v: boolean) => emit('update:visible', v)">
        <!-- 引擎没起时先把原因说清楚，否则要到启动那一步才报错；吸顶不占表单位 -->
        <common-modal-tip v-if="instForm.mode === 'docker' && dockerErr" :text="dockerErr">
            <a-button size="small" @click="openDockerWindow">打开 Docker 面板</a-button>
        </common-modal-tip>
        <a-form :model="instForm" layout="vertical" class="inst-form">
            <a-form-item field="name" label="实例名称">
                <a-input v-model="instForm.name" placeholder="例如 local-dev" />
            </a-form-item>
            <a-form-item field="mode" label="运行模式">
                <a-radio-group size="medium" v-model="instForm.mode" type="button" @change="onModeChange">
                    <a-radio value="local"><icon-computer /> 本地</a-radio>
                    <a-radio value="docker"><icon-experiment /> Docker</a-radio>
                </a-radio-group>
                <template #extra>
                    <template v-if="instForm.mode === 'docker'">
                        容器内跑 redis-server，数据目录挂载到 /data；首次启动会自动拉取镜像（可能几分钟）
                    </template>
                    <template v-else>使用本机插件目录下的 redis-server，可在「版本管理」获取</template>
                </template>
            </a-form-item>
            <!-- 本地：版本单占一行；Docker：镜像 + 容器名同一行，两种模式行数一致，切模式时弹窗高度不跳 -->
            <a-form-item v-if="instForm.mode !== 'docker'" field="version" label="Redis 版本" tooltip="需已安装，可在「版本管理」获取">
                <a-select v-model="instForm.version" placeholder="需已安装，可在「版本管理」获取">
                    <a-option v-for="v in localVersions" :key="v.version" :value="v.version">
                        {{ v.version }}（{{ srcLabel(v.source) }}）
                    </a-option>
                </a-select>
            </a-form-item>
            <a-row v-else :gutter="12">
                <a-col :span="14">
                    <a-form-item class="img-item" field="image" label="镜像" required>
                        <div class="img-row">
                            <a-select v-model="instForm.image" class="img-select" placeholder="选择已安装镜像"
                                allow-create filterable :loading="dockerImagesLoading">
                                <!-- 自闭合的 a-option 没有文字，label 会是空串，下拉里全是空白行 —— 显式写上文字 -->
                                <a-option v-for="im in imageOptions" :key="im" :value="im">{{ im }}</a-option>
                                <template #empty>
                                    <a-empty :image="false" description="无可用镜像" />
                                </template>
                            </a-select>
                            <!-- 配镜像时随手打开 Docker 管理窗口（拉取 / 看进度） -->
                            <a-tooltip content="打开 Docker 管理窗口：拉镜像、看拉取进度">
                                <a-button size="small" class="img-open" @click="openDockerWindow">
                                    <template #icon><icon-storage /></template>
                                </a-button>
                            </a-tooltip>
                        </div>
                        <template #help>
                            <span v-if="dockerImagesLoading">正在读取本地镜像…</span>
                            <span v-else-if="!imageOptions.length && !instForm.image" class="img-none">
                                本地无可用镜像，<a-link @click="openDockerWindow">点击去安装</a-link>
                            </span>
                        </template>
                    </a-form-item>
                </a-col>
                <a-col :span="10">
                    <a-form-item field="containerName" label="容器名" tooltip="留空自动生成 Tools-redis-实例ID">
                        <a-input v-model="instForm.containerName" placeholder="留空自动生成" />
                    </a-form-item>
                </a-col>
            </a-row>
            <a-row :gutter="12">
                <a-col :span="12">
                    <a-form-item field="port" label="端口" :error="portError"
                        tooltip="只监听 127.0.0.1，Docker 与本地模式一样不对外暴露">
                        <a-input-number v-model="instForm.port" :min="1" :max="65535" @change="portError = ''"
                            @blur="checkPortTaken" />
                    </a-form-item>
                </a-col>
                <a-col :span="12">
                    <a-form-item field="maxMemoryMB" label="最大内存 (MB)" tooltip="0 = 不限">
                        <a-input-number v-model="instForm.maxMemoryMB" :min="0" />
                    </a-form-item>
                </a-col>
            </a-row>
            <a-form-item field="password" label="密码" tooltip="留空 = 不设置 requirepass">
                <a-input-password v-model="instForm.password" placeholder="留空则不设置" />
            </a-form-item>
            <a-form-item field="appendOnly" label="持久化 AOF" tooltip="开启后每次写入追加到 appendonly.aof，重启不丢数据">
                <a-switch v-model="instForm.appendOnly" />
            </a-form-item>
            <a-form-item field="dataDir" label="数据目录"
                :tooltip="instForm.mode === 'docker' ? 'Docker 模式该目录会挂载进容器 /data，RDB/AOF 都落在这儿' : '留空 = 程序数据目录下按实例 ID 分配'">
                <a-input v-model="instForm.dataDir" placeholder="空 = 程序数据目录下按 ID 分配" />
            </a-form-item>
        </a-form>
    </a-modal>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, reactive, ref, watch } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { useToolWindow } from "@/views/index/useToolWindow";
import { srcLabel, type SrvInstanceConfig } from "../helpers";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const { openToolWindow } = useToolWindow();

const props = defineProps<{
    visible: boolean;
    /** 编辑目标实例；新建传 null */
    edit: SrvInstanceConfig | null;
    /** 从版本管理「新建实例」带入的镜像：非空则直接进 Docker 模式 */
    presetImage?: string;
    srvInstances: SrvInstanceConfig[];
    srvStates: Record<string, any>;
}>();

const emit = defineEmits<{
    (e: "update:visible", v: boolean): void;
    /** 保存/重启后需要页面重拉实例列表与状态 */
    (e: "saved"): void;
}>();

const instForm = reactive<SrvInstanceConfig>({
    id: "", name: "", version: "", port: 6379, password: "", appendOnly: false, maxMemoryMB: 0, dataDir: "",
    mode: "local", image: "", containerName: "",
});
const portError = ref("");

const localVersions = ref<any[]>([]);
const dockerImageList = ref<string[]>([]);
const dockerImagesLoading = ref(false);
const dockerErr = ref("");
const imageOptions = computed(() => Array.from(new Set<string>(dockerImageList.value)));

async function loadLocalVersions() {
    try { localVersions.value = (await GoTools.RedisSrvListVersions()) || []; }
    catch (e) { localVersions.value = []; }
}

// 每次打开按 edit / presetImage 初始化表单
watch(() => props.visible, async (open) => {
    if (!open) return;
    portError.value = "";
    dockerErr.value = "";
    if (props.edit) {
        Object.assign(instForm, {
            ...props.edit,
            mode: props.edit.mode === "docker" ? "docker" : "local",
            image: props.edit.image || "",
            containerName: props.edit.containerName || "",
        });
    } else {
        let port = 6379;
        try { port = (await GoTools.RedisSrvSuggestPort(6379)) || port; } catch (e) { /* ignore */ }
        Object.assign(instForm, { id: "", name: "", version: localVersions.value[0]?.version || "", port, password: "", appendOnly: false, maxMemoryMB: 0, dataDir: "", mode: "local", image: "", containerName: "" });
        if (props.presetImage) { instForm.mode = "docker"; instForm.image = props.presetImage; }
    }
    if (localVersions.value.length === 0) loadLocalVersions();
    if (instForm.mode === "docker") await onModeChange("docker");
});

/** 切换运行模式：Docker 读本机镜像候选 + 引擎状态，不干预已有输入 */
async function onModeChange(v: any) {
    const mode = String(v ?? instForm.mode ?? "local");
    instForm.mode = mode;
    if (mode !== "docker") return;
    loadImageOptions();
    loadDockerEngine();
}

async function loadImageOptions() {
    dockerImagesLoading.value = true;
    try { dockerImageList.value = (await GoTools.RedisSrvDockerImages()) || []; }
    catch (e) { dockerImageList.value = []; }
    dockerImagesLoading.value = false;
    if (instForm.mode === "docker" && !instForm.image && dockerImageList.value.length) instForm.image = dockerImageList.value[0];
}

async function loadDockerEngine() {
    try {
        const [ok, msg] = await GoTools.RedisSrvDockerAvailable();
        dockerErr.value = ok ? "" : (msg || "Docker 引擎不可用");
    } catch (e: any) { dockerErr.value = e?.message || String(e); }
}

/** 打开 Docker 管理窗口（独立 webview，单例复用） */
function openDockerWindow() {
    openToolWindow("/tools/docker", "Docker管理", { single: true, width: 1200, height: 800 });
}

async function checkPortTaken() {
    if (!instForm.port) { portError.value = ""; return; }
    try {
        const reason = await GoTools.RedisSrvPortTaken(instForm.port, instForm.id || "");
        if (!reason) { portError.value = ""; return; }
        const suggest = await GoTools.RedisSrvSuggestPort(instForm.port);
        portError.value = suggest && suggest !== instForm.port ? `${reason}，可用：${suggest}` : reason;
    } catch (e) { /* ignore */ }
}

async function saveInstance(): Promise<boolean> {
    if (!instForm.name) { Message.warning("请填写实例名称"); return false; }
    if (instForm.mode === "docker") {
        if (!instForm.image) { Message.warning("Docker 模式请选择或输入镜像"); return false; }
    } else if (!instForm.version) { Message.warning("请选择 Redis 版本"); return false; }
    if (!instForm.port) { Message.warning("请填写端口"); return false; }
    try {
        const reason = await GoTools.RedisSrvPortTaken(instForm.port, instForm.id || "");
        if (reason) {
            const suggest = await GoTools.RedisSrvSuggestPort(instForm.port);
            portError.value = suggest && suggest !== instForm.port ? `${reason}，可用：${suggest}` : reason;
            Message.warning(portError.value);
            return false;
        }
    } catch (e) { /* 探测失败不阻断，后端兜底 */ }
    try {
        const old = instForm.id ? props.srvInstances.find((i) => i.id === instForm.id) : null;
        const switched = !!old && (old.mode !== instForm.mode || old.image !== instForm.image || old.version !== instForm.version);
        const wasRunning = !!old && !!props.srvStates[old.id]?.running;
        await GoTools.RedisSrvSaveInstance({ ...instForm });
        emit("update:visible", false);
        Message.success("已保存");
        emit("saved");
        if (switched && wasRunning) setTimeout(() => askRestartAfterSwitch({ ...instForm }), 240);
        return true;
    } catch (e: any) { Message.error("保存失败: " + (e?.message || e)); return false; }
}

/** 切了版本/镜像又没重启：跑着的还是旧版本，问一句要不要现在就换过去 */
function askRestartAfterSwitch(inst: SrvInstanceConfig) {
    const what = inst.mode === "docker" ? `镜像 ${inst.image}` : `Redis ${inst.version}`;
    Modal.confirm({
        title: "版本已切换",
        content: `${what} 已写入配置，但实例「${inst.name}」还在跑旧版本；重启后才会生效（期间会短暂中断服务）。`,
        okText: "立即重启",
        cancelText: "稍后手动重启",
        onOk: async () => {
            try {
                await GoTools.RedisSrvStop(inst.id);
                await GoTools.RedisSrvStart(inst.id);
                Message.success(`实例 ${inst.name} 已重启，新版本已生效`);
            } catch (e: any) { Message.error("重启失败: " + (e?.message || e)); }
            emit("saved");
        },
    });
}
</script>

<style scoped>
/* Docker 镜像行：下拉占满剩余宽度，右侧一个方形按钮打开 Docker 管理窗口
 * Arco 的竖排表单项内容区默认按内容宽度收缩（align-items: flex-start），空下拉会被挤成一个小方块，
 * 这里显式把内容区拉成整栏宽 */
.inst-form :deep(.img-item .arco-form-item-wrapper-col) {
    align-items: stretch;
}

.inst-form :deep(.img-item .arco-form-item-content) {
    width: 100%;
}

.img-row {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
}

.img-select {
    flex: 1;
    min-width: 0;
}

.img-open {
    flex: none;
    width: 32px;
    padding: 0;
}

/* 本地没有镜像时的提示：橙色一点，提醒可以点进去装 */
.img-none {
    color: rgb(var(--orange-6));
}

/* #help 里的“去安装”链接：跟提示文字同字号，不把行高撑开 */
.img-none :deep(.arco-link) {
    padding: 0;
    font-size: inherit;
    line-height: inherit;
}
</style>
