<template>
    <!-- 静态站点：新建/编辑实例。从主页面拆出，自持表单 / 镜像候选 / 引擎状态 / 端口探测 / 站点目录选择与保存。 -->
    <a-modal :visible="visible" :title="instForm.id ? '编辑站点' : '新建站点'" :width="520"
        :body-style="{ maxHeight: '72vh' }" :on-before-ok="saveInstance"
        @update:visible="(v: boolean) => emit('update:visible', v)">
        <!-- 引擎没起时先把原因说清楚，否则要到启动那一步才报错；吸顶不占表单位 -->
        <common-modal-tip v-if="instForm.mode === 'docker' && dockerErr" :text="dockerErr">
            <a-button size="small" @click="openDockerWindow">打开 Docker 面板</a-button>
        </common-modal-tip>
        <a-form :model="instForm" layout="vertical" class="inst-form">
            <a-form-item field="name" label="站点名称" required>
                <a-input v-model="instForm.name" placeholder="如 docs-site" />
            </a-form-item>
            <a-form-item field="mode" label="运行模式">
                <a-radio-group size="medium" v-model="instForm.mode" type="button" @change="onModeChange">
                    <a-radio value="local"><icon-computer /> 本地</a-radio>
                    <a-radio value="docker"><icon-experiment /> Docker</a-radio>
                </a-radio-group>
                <template #extra>
                    <template v-if="instForm.mode === 'docker'">
                        容器里跑 nginx：站点目录挂到 /usr/share/nginx/html，conf 与 logs 一并挂载；改「SPA /
                        列目录」后点重启即生效（不重建容器），首次启动会自动拉镜像
                    </template>
                    <template v-else>内置 Go 静态服务器，随本程序一起启停；Node 版本用于展示与配套 npm</template>
                </template>
            </a-form-item>
            <!-- 说明文字全部移到 label 右侧的图标 tooltip 里，label 只留短名：表单不被长括号挤成两行 -->
            <!-- 两种模式的行数与列宽对齐（Docker 只把站点根目录分一半给容器名），切模式时弹窗高度不跳 -->
            <a-row :gutter="12">
                <a-col :span="12">
                    <a-form-item v-if="instForm.mode !== 'docker'" field="version" label="Node.js 版本" required
                        tooltip="用于展示与配套 npm；实际跑站点的是这个版本自带的 node.exe">
                        <a-select v-model="instForm.version" placeholder="选择已安装版本" :options="versionOptions" />
                    </a-form-item>
                    <a-form-item v-else class="img-item" field="image" label="镜像" required>
                        <div class="img-row">
                            <a-select v-model="instForm.image" class="img-select" placeholder="选择已拉取镜像" allow-create
                                filterable :loading="dockerImagesLoading">
                                <!-- 自闭合的 a-option 没有文字，下拉里会全是空白行 —— 显式写上文字 -->
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
                <a-col :span="12">
                    <a-form-item field="port" label="端口" :error="portError"
                        :tooltip="instForm.mode === 'docker' ? '宿主机发布端口（只绑 127.0.0.1）；容器内固定监听 80，改这里不动配置文件' : '内置服务器监听的端口'">
                        <a-input-number v-model="instForm.port" :min="1" :max="65535" @change="portError = ''"
                            @blur="checkPortTaken" />
                    </a-form-item>
                </a-col>
            </a-row>
            <a-row :gutter="12">
                <a-col :span="instForm.mode === 'docker' ? 12 : 24">
                    <a-form-item field="docRoot" label="站点根目录"
                        :tooltip="instForm.mode === 'docker' ? '留空 = 实例目录下的 www/；该目录挂载进容器，Docker Desktop 需能访问所在磁盘（Settings → Resources → File sharing）' : '留空 = 实例目录下的 www/'">
                        <a-input-search v-model="instForm.docRoot" placeholder="如 D:\www\dist" search-button
                            button-text="选择目录" @search="chooseDocRoot" />
                    </a-form-item>
                </a-col>
                <a-col v-if="instForm.mode === 'docker'" :span="12">
                    <a-form-item field="containerName" label="容器名" tooltip="留空自动生成 go-tools-npm-实例ID">
                        <a-input v-model="instForm.containerName" placeholder="留空自动生成" />
                    </a-form-item>
                </a-col>
            </a-row>
            <a-row :gutter="12">
                <a-col :span="8">
                    <a-form-item field="spa" label="SPA 模式" tooltip="找不到文件时回退到 index.html">
                        <a-switch v-model="instForm.spa" />
                    </a-form-item>
                </a-col>
                <a-col :span="8">
                    <a-form-item field="directory" label="允许列目录" tooltip="无 index.html 时返回目录列表">
                        <a-switch v-model="instForm.directory" />
                    </a-form-item>
                </a-col>
                <a-col :span="8">
                    <a-form-item field="openBrowser" label="自动开浏览器" tooltip="启动成功后自动在浏览器打开站点">
                        <a-switch v-model="instForm.openBrowser" />
                    </a-form-item>
                </a-col>
            </a-row>
        </a-form>
    </a-modal>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, reactive, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { useToolWindow } from "@/views/index/useToolWindow";
import { srcLabel, type InstConfig } from "../helpers";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const { openToolWindow } = useToolWindow();

const props = defineProps<{
    visible: boolean;
    /** 编辑目标站点；新建传 null */
    edit: InstConfig | null;
    /** 从版本管理「新建实例」带入的镜像：非空则直接进 Docker 模式 */
    presetImage?: string;
    instances: InstConfig[];
    states: Record<string, any>;
}>();

const emit = defineEmits<{
    (e: "update:visible", v: boolean): void;
    /** 保存成功后需要页面重拉实例列表与状态 */
    (e: "saved"): void;
}>();

const instForm = reactive<InstConfig>({
    id: "", name: "", version: "", port: 3000, docRoot: "", spa: true, directory: false, openBrowser: false,
    mode: "local", image: "", containerName: "",
});
const portError = ref("");
const localVersions = ref<any[]>([]);
/* Docker 模式表单：镜像候选只列本机已有的 nginx 系镜像（不猜预置 tag），为空就引导去拉 */
/* 名字不能回退成 dockerImages：script setup 下 kebab 标签 docker-images 会优先拿同名 setup 变量当组件，整个 Docker 区会渲染成空白 */
const dockerImageList = ref<string[]>([]);
const dockerImagesLoading = ref(false);
const dockerErr = ref("");

const versionOptions = computed(() => localVersions.value.map((v: any) => ({ label: `${v.version}（${srcLabel(v.source)}）`, value: v.version })));
const imageOptions = computed(() => Array.from(new Set<string>(dockerImageList.value)));

async function loadLocalVersions() {
    try { localVersions.value = (await GoTools.NpmSrvListVersions()) || []; }
    catch (e) { localVersions.value = []; }
}

// 每次打开按 edit / presetImage 初始化表单
watch(() => props.visible, async (open) => {
    if (!open) return;
    portError.value = "";
    dockerErr.value = "";
    if (props.edit) {
        // 历史配置没 mode/image/containerName 字段，归一化后再进表单，避免 radio 两档都没选中
        Object.assign(instForm, {
            ...props.edit,
            mode: props.edit.mode === "docker" ? "docker" : "local",
            image: props.edit.image || "",
            containerName: props.edit.containerName || "",
        });
    } else {
        // 默认端口也要避开已被其它站点登记 / 已监听的端口
        let port = 3000;
        try { port = (await GoTools.NpmSrvSuggestPort(3000)) || 3000; } catch (e) { /* ignore */ }
        Object.assign(instForm, { id: "", name: "", version: localVersions.value[0]?.version || "", port, docRoot: "", spa: true, directory: false, openBrowser: false, mode: "local", image: "", containerName: "" });
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
    try { dockerImageList.value = (await GoTools.NpmSrvDockerImages()) || []; }
    catch (e) { dockerImageList.value = []; } // 引擎不可用时当作没有镜像，走“去安装”引导
    dockerImagesLoading.value = false;
    // 还没选值且有候选时先填第一个（默认 nginx:alpine），省一步点击
    if (instForm.mode === "docker" && !instForm.image && dockerImageList.value.length) instForm.image = dockerImageList.value[0];
}

async function loadDockerEngine() {
    try {
        const [ok, msg] = await GoTools.NpmSrvDockerAvailable();
        dockerErr.value = ok ? "" : (msg || "Docker 引擎不可用");
    } catch (e: any) { dockerErr.value = e?.message || String(e); }
}

/** 打开 Docker 管理窗口（独立 webview，单例复用）：配镜像时直接过去拉，不用回主页找入口 */
function openDockerWindow() {
    openToolWindow("/tools/docker", "Docker管理", { single: true, width: 1200, height: 800 });
}

async function checkPortTaken() {
    if (!instForm.port) { portError.value = ""; return; }
    try {
        const reason = await GoTools.NpmSrvPortTaken(instForm.port, instForm.id || "");
        if (reason) {
            const suggest = await GoTools.NpmSrvSuggestPort(instForm.port);
            portError.value = suggest && suggest !== instForm.port ? `${reason}，可用：${suggest}` : reason;
        } else {
            portError.value = "";
        }
    } catch (e) { /* ignore */ }
}

async function chooseDocRoot() {
    try { const dir = await GoTools.NpmSrvChooseDir(); if (dir) instForm.docRoot = dir; }
    catch (e) { /* cancel */ }
}

async function saveInstance(): Promise<boolean> {
    if (!instForm.name) { Message.warning("请填写站点名称"); return false; }
    // 两种模式的必填项不一样：本地看本机版本库，容器看镜像（后端也会再校一次）
    if (instForm.mode === "docker") {
        if (!instForm.image) { Message.warning("Docker 模式请选择或输入镜像，如 nginx:alpine"); return false; }
    } else if (!instForm.version) { Message.warning("请选择 Node.js 版本"); return false; }
    try {
        const reason = await GoTools.NpmSrvPortTaken(instForm.port, instForm.id || "");
        if (reason) { portError.value = reason; Message.error(reason); return false; }
        await GoTools.NpmSrvSaveInstance({ ...instForm });
        Message.success(instForm.mode === "docker"
            ? "已保存；容器里的 nginx 监听 80，改这里只改宿主端口；SPA / 列目录开关在下次重启时写回 nginx.conf"
            : "已保存");
        emit("saved");
        return true;
    } catch (e: any) { Message.error("保存失败: " + (e?.message || e)); return false; }
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
