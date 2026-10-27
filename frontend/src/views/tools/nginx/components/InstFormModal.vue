<template>
    <!-- 新建 / 编辑实例弹窗：从主页面拆出，内部自管表单、版本/镜像/PHP 候选与端口校验；
         保存成功后 emit('saved') 让父组件重载列表。父只传可见性 + 编辑目标（或预置镜像）。 -->
    <a-modal v-model:visible="visible" :title="instForm.id ? '编辑实例' : '新建实例'" :width="520"
        :body-style="{ maxHeight: '72vh' }" @before-ok="saveInstance">
        <!-- 引擎没起时先把原因说清楚，否则要到启动那一步才报错；吸顶不占表单位 -->
        <common-modal-tip v-if="instForm.mode === 'docker' && dockerErr" :text="dockerErr">
            <a-button size="small" @click="openDockerWindow">打开 Docker 面板</a-button>
        </common-modal-tip>
        <a-form :model="instForm" layout="vertical" class="inst-form">
            <a-form-item field="name" label="实例名称" required>
                <a-input v-model="instForm.name" placeholder="如 site-dev" />
            </a-form-item>
            <a-form-item field="mode" label="运行模式">
                <a-radio-group size="medium" v-model="instForm.mode" type="button" @change="onModeChange">
                    <a-radio value="local"><icon-computer /> 本地</a-radio>
                    <a-radio value="docker"><icon-experiment /> Docker</a-radio>
                </a-radio-group>
                <template #extra>
                    <template v-if="instForm.mode === 'docker'">
                        容器里跑 nginx：实例的 conf/ 与 logs/ 以及站点目录都挂载进容器，改配置后点「重载」（重启容器）生效；首次启动会自动拉镜像
                    </template>
                    <template v-if="sysPkg">使用系统安装的 nginx（发行版包），可在「版本管理」选已安装的版本</template>
                    <template v-else>使用本机插件目录下的 nginx，可在「版本管理」获取</template>
                </template>
            </a-form-item>
            <!-- 两种模式的行数和列宽对齐（Docker 只多一个容器名），切模式时弹窗高度不跳 -->
            <a-row :gutter="12">
                <a-col :span="14">
                    <a-form-item v-if="instForm.mode !== 'docker'" field="version" label="Nginx 版本" required
                        tooltip="需已安装，可在「版本管理」获取">
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
                <a-col :span="10">
                    <a-form-item field="port" label="HTTP 端口" :error="portError"
                        :tooltip="instForm.mode === 'docker' ? '宿主机发布端口（只绑 127.0.0.1）；容器内固定监听 80，改这里不动配置文件' : 'nginx.conf 里的 listen 端口'">
                        <a-input-number v-model="instForm.port" :min="1" :max="65535" @change="portError = ''"
                            @blur="checkPortTaken" />
                    </a-form-item>
                </a-col>
            </a-row>
            <a-row :gutter="12">
                <a-col v-if="instForm.mode === 'docker'" :span="8">
                    <a-form-item field="containerName" label="容器名" tooltip="留空自动生成 go-tools-nginx-实例ID">
                        <a-input v-model="instForm.containerName" placeholder="留空自动生成" />
                    </a-form-item>
                </a-col>
                <a-col :span="instForm.mode === 'docker' ? 8 : 12">
                    <a-form-item field="sslPort" label="HTTPS 端口"
                        :tooltip="instForm.mode === 'docker' ? '0 = 不启用；>0 时容器内监听 443 并发布到该端口，证书放到实例 conf 目录的 server.crt/server.key' : '0 = 不启用 HTTPS'"
                        :error="sslPortError">
                        <a-input-number v-model="instForm.sslPort" :min="0" :max="65535" @change="sslPortError = ''"
                            @blur="checkSslPortTaken" />
                    </a-form-item>
                </a-col>
                <a-col :span="instForm.mode === 'docker' ? 8 : 12">
                    <a-form-item field="workers" label="worker_processes" tooltip="0 = auto（按 CPU 核数自动）">
                        <a-input-number v-model="instForm.workers" :min="0" :max="64" />
                    </a-form-item>
                </a-col>
            </a-row>
            <a-form-item field="docRoot" label="站点根目录"
                :tooltip="instForm.mode === 'docker' ? '留空 = 程序目录 data/www/实例名；该目录会挂载到容器 /usr/share/nginx/html' : '留空 = 程序目录 data/www/实例名'">
                <a-input-search v-model="instForm.docRoot" placeholder="如 D:\www\site" search-button button-text="选择目录"
                    @search="chooseDocRoot" />
            </a-form-item>
            <a-form-item v-if="instForm.mode !== 'docker'" label="PHP 支持"
                tooltip="选 PHP 后会自动创建一个关联的 PHP 实例（在 PHP 面板可见，来源标注为本 Nginx 网站），启停/删除随本站点联动；改 PHP 后端后需重启实例才会重写 nginx.conf（已自定义过的 conf 不会被覆盖）">
                <a-radio-group size="medium" v-model="phpOn" type="button">
                    <a-radio :value="false">纯静态</a-radio>
                    <a-radio :value="true">PHP 网站</a-radio>
                </a-radio-group>
            </a-form-item>
            <a-row v-if="instForm.mode !== 'docker' && phpOn" :gutter="12">
                <a-col :span="12">
                    <a-form-item field="phpMode" label="接入方式">
                        <a-select v-model="instForm.phpMode">
                            <a-option value="fastcgi">FastCGI（php-cgi）</a-option>
                            <a-option value="proxy">反向代理（php -S）</a-option>
                        </a-select>
                    </a-form-item>
                </a-col>
                <a-col :span="12">
                    <a-form-item field="phpVersion" label="PHP 版本">
                        <a-select v-model="instForm.phpVersion" placeholder="选择已安装 PHP 版本"
                            :disabled="!phpVersions.length">
                            <a-option v-for="v in phpVersions" :key="v.version" :value="v.version">{{ v.version }}</a-option>
                        </a-select>
                    </a-form-item>
                </a-col>
            </a-row>
            <div v-if="instForm.mode !== 'docker' && phpOn && !phpVersions.length" class="php-warn">
                未检测到已安装的 PHP 版本，请先到「PHP 管理」安装后再关联。
            </div>
            <div v-if="instForm.mode !== 'docker' && phpOn && instForm.phpPort" class="php-warn">
                关联 PHP 后端监听端口：{{ instForm.phpPort }}（保存/重启后生效）
            </div>
            <a-form-item field="domain" label="绑定域名"
                tooltip="写入 nginx.conf 的 server_name；可填多个，用空格或逗号分隔；留空则仅本机 localhost。改域名后需重启实例才会重写配置（已自定义过的 nginx.conf 不会被覆盖）">
                <a-input v-model="instForm.domain" placeholder="如 local.test，多个用空格分隔" />
                <template #extra>
                    <div v-if="instForm.domain" class="hosts-hint">
                        <span>想本机通过该域名访问，需在 hosts 里加一行：</span>
                        <a-link copyable :content="hostsLine" class="hosts-line">{{ hostsLine }}</a-link>
                        <a-link @click="openHosts">
                            <template #icon><icon-edit /></template>打开 hosts 配置</a-link>
                        <span v-if="hostsPath" class="hosts-path">{{ hostsPath }}</span>
                    </div>
                </template>
            </a-form-item>
            <a-form-item field="stubStatus" label="开放 /stub_status 状态页" tooltip="开启后可用工具面板读取 nginx 连接数/请求数">
                <a-switch v-model="instForm.stubStatus" />
            </a-form-item>
        </a-form>
    </a-modal>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, reactive, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { useToolWindow } from "@/views/index/useToolWindow";
import { useSysPkg } from "@/utils/caps";
import type { InstConfig } from "../types";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const sysPkg = useSysPkg();
const { openToolWindow } = useToolWindow();

const props = withDefaults(defineProps<{
    /** 可见性（v-model） */
    modelValue: boolean;
    /** 编辑目标：传入实例即编辑，null 即新建 */
    instance?: InstConfig | null;
    /** 新建时预置为 Docker 模式并选定的镜像（版本管理「新建实例」用） */
    presetImage?: string;
}>(), { instance: null, presetImage: "" });

const emit = defineEmits<{
    (e: "update:modelValue", v: boolean): void;
    /** 保存成功：父组件据此重载列表；switched 表示版本/镜像变了需要提示重启 */
    (e: "saved", payload: { config: InstConfig; switched: boolean }): void;
}>();

const visible = computed({
    get: () => props.modelValue,
    set: (v: boolean) => emit("update:modelValue", v),
});

/* ============================ 表单状态 ============================ */
const instForm = reactive<InstConfig>({
    id: "", name: "", version: "", port: 8090, sslPort: 0, domain: "", docRoot: "", workers: 0, stubStatus: true,
    mode: "local", image: "", containerName: "",
    phpMode: "", phpVersion: "", phpPort: 0, phpInstanceId: "",
});
const phpVersions = ref<any[]>([]);
/** 表单里是否启用 PHP：phpMode 非空即启用；开启时默认 fastcgi，关闭清空 */
const phpOn = computed({
    get: () => !!instForm.phpMode,
    set: (v: boolean) => {
        instForm.phpMode = v ? "fastcgi" : "";
        if (v && !instForm.phpVersion) instForm.phpVersion = phpVersions.value[0]?.version || "";
    },
});
const hostsPath = ref("");
/** 建议写入 hosts 的一行：hosts 支持一行映多个域名，故把表单里的域名全部跟在 127.0.0.1 后 */
const hostsLine = computed(() => {
    const ds = String(instForm.domain || "").split(/[\s,;]+/).map((s) => s.trim()).filter((s) => s && s !== "localhost");
    return `127.0.0.1    ${ds.join(" ") || "example.test"}`;
});
const portError = ref("");
const sslPortError = ref("");

/* 版本 / 镜像候选 */
const localVersions = ref<any[]>([]);
const versionOptions = computed(() => localVersions.value.map((v: any) => ({ label: `${v.version}（${srcLabel(v.source)}）`, value: v.version })));
const dockerImageList = ref<string[]>([]);
const dockerImagesLoading = ref(false);
const dockerErr = ref("");
const imageOptions = computed(() => Array.from(new Set<string>(dockerImageList.value || [])));

function srcLabel(s: string): string {
    return s === "bundled" ? "内置" : s === "downloaded" ? "下载" : s === "system" ? "系统包" : "本机";
}

/* ============================ 打开时初始化 ============================ */
watch(() => props.modelValue, async (open) => {
    if (!open) return;
    await initForm();
});

async function initForm() {
    portError.value = "";
    sslPortError.value = "";
    dockerErr.value = "";
    if (!hostsPath.value) {
        try { hostsPath.value = (await GoTools.NginxSrvHostsPath()) || ""; } catch (e) { /* ignore */ }
    }
    if (!phpVersions.value.length) loadPhpVersions();
    if (localVersions.value.length === 0) loadLocalVersions();
    const inst = props.instance;
    if (inst) {
        // 历史配置没 mode/image/containerName 字段，归一化后再进表单，避免 radio 两档都没选中
        Object.assign(instForm, {
            ...inst,
            mode: inst.mode === "docker" ? "docker" : "local",
            image: inst.image || "",
            containerName: inst.containerName || "",
            phpMode: inst.phpMode || "",
            phpVersion: inst.phpVersion || "",
            phpPort: inst.phpPort || 0,
            phpInstanceId: inst.phpInstanceId || "",
        });
    } else {
        // 默认端口也要能用：避开其它实例已登记 / 系统已监听的
        let port = 8090;
        try { port = (await GoTools.NginxSrvSuggestPort(8090)) || port; } catch (e) { /* ignore */ }
        Object.assign(instForm, { id: "", name: "", version: localVersions.value[0]?.version || "", port, sslPort: 0, domain: "", docRoot: "", workers: 0, stubStatus: true, mode: "local", image: "", containerName: "", phpMode: "", phpVersion: "", phpPort: 0, phpInstanceId: "" });
    }
    if (instForm.mode === "docker") await onModeChange("docker");
    else if (props.presetImage && !inst) {
        // 版本管理「新建实例」：预置为 Docker + 指定镜像
        instForm.mode = "docker";
        instForm.image = props.presetImage;
        await onModeChange("docker");
    }
}

async function loadLocalVersions() {
    try { localVersions.value = (await GoTools.NginxSrvListVersions()) || []; }
    catch (e) { localVersions.value = []; }
}
async function loadPhpVersions() {
    try { phpVersions.value = (await GoTools.PhpSrvListVersions()) || []; }
    catch (e) { phpVersions.value = []; }
}

/* ============================ 模式 / 镜像 ============================ */
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
    try { dockerImageList.value = (await GoTools.NginxSrvDockerImages()) || []; }
    catch (e) { dockerImageList.value = []; } // 引擎不可用时当作没有镜像，走“去安装”引导
    dockerImagesLoading.value = false;
    // 还没选值且有候选时先填第一个，省一步点击
    if (instForm.mode === "docker" && !instForm.image && dockerImageList.value.length) instForm.image = dockerImageList.value[0];
}
async function loadDockerEngine() {
    try {
        const [ok, msg] = await GoTools.NginxSrvDockerAvailable();
        dockerErr.value = ok ? "" : (msg || "Docker 引擎不可用");
    } catch (e: any) { dockerErr.value = e?.message || String(e); }
}
/** 打开 Docker 管理窗口（独立 webview，单例复用） */
function openDockerWindow() {
    openToolWindow("/tools/docker", "Docker管理", { single: true, width: 1200, height: 800 });
}

/* ============================ 端口 / 目录 / hosts ============================ */
/** HTTP 端口校验；顺带把 HTTPS 与它撞车的情况也标出来 */
async function checkPortTaken() {
    if (!instForm.port) { portError.value = ""; return; }
    try {
        const reason = await GoTools.NginxSrvPortTaken(instForm.port, instForm.id || "");
        if (!reason) { portError.value = ""; }
        else {
            const suggest = await GoTools.NginxSrvSuggestPort(instForm.port);
            portError.value = suggest && suggest !== instForm.port ? `${reason}，可用：${suggest}` : reason;
        }
    } catch (e) { /* ignore */ }
    if (instForm.sslPort && instForm.sslPort === instForm.port) sslPortError.value = "HTTPS 端口不能与 HTTP 端口相同";
    else if (sslPortError.value === "HTTPS 端口不能与 HTTP 端口相同") sslPortError.value = "";
}
async function checkSslPortTaken() {
    if (!instForm.sslPort) { sslPortError.value = ""; return; }
    if (instForm.sslPort === instForm.port) { sslPortError.value = "HTTPS 端口不能与 HTTP 端口相同"; return; }
    try {
        const reason = await GoTools.NginxSrvPortTaken(instForm.sslPort, instForm.id || "");
        if (!reason) { sslPortError.value = ""; return; }
        const suggest = await GoTools.NginxSrvSuggestPort(instForm.sslPort);
        sslPortError.value = suggest && suggest !== instForm.sslPort ? `${reason}，可用：${suggest}` : reason;
    } catch (e) { /* ignore */ }
}
async function chooseDocRoot() {
    try { const dir = await GoTools.NginxSrvChooseDir(); if (dir) instForm.docRoot = dir; }
    catch (e) { /* cancel */ }
}
/** 打开本机 hosts 文件：Windows 会弹 UAC 提权拉记事本；Linux 无图形环境会提示用 sudo */
async function openHosts() {
    try { await GoTools.NginxSrvOpenHosts(); }
    catch (e: any) { Message.warning("打开 hosts 失败: " + (e?.message || e)); }
}

/* ============================ 保存 ============================ */
async function saveInstance(): Promise<boolean> {
    if (!instForm.name) { Message.warning("请填写实例名称"); return false; }
    // 两种模式的必填项不一样：本地看本机版本库，容器看镜像（后端也会再校一次）
    if (instForm.mode === "docker") {
        if (!instForm.image) { Message.warning("Docker 模式请选择或输入镜像"); return false; }
    } else if (!instForm.version) { Message.warning("请选择 Nginx 版本"); return false; }
    if (!instForm.port) { Message.warning("请填写 HTTP 端口"); return false; }
    // PHP 后端：启用时必须选一个已安装版本（Docker 模式不参与关联）
    if (instForm.mode !== "docker" && instForm.phpMode && !instForm.phpVersion) {
        Message.warning("PHP 后端请选择已安装的 PHP 版本");
        return false;
    }
    // 端口占用先在本地拦一次，后端保存时还会再校验
    try {
        const reason = await GoTools.NginxSrvPortTaken(instForm.port, instForm.id || "");
        if (reason) {
            portError.value = reason;
            Message.warning("HTTP " + reason);
            return false;
        }
        if (instForm.sslPort) {
            if (instForm.sslPort === instForm.port) {
                sslPortError.value = "HTTPS 端口不能与 HTTP 端口相同";
                Message.warning(sslPortError.value);
                return false;
            }
            const sslReason = await GoTools.NginxSrvPortTaken(instForm.sslPort, instForm.id || "");
            if (sslReason) {
                sslPortError.value = sslReason;
                Message.warning("HTTPS " + sslReason);
                return false;
            }
        }
    } catch (e) { /* 探测失败不阻断，后端兜底 */ }
    try {
        // 编辑前记下原配置：切了版本/镜像且实例在跑，容器里还是旧版本，得重启才生效
        const old = props.instance;
        const switched = !!old && (old.mode !== instForm.mode || old.image !== instForm.image || old.version !== instForm.version);
        await GoTools.NginxSrvSaveInstance({ ...instForm });
        Message.success(instForm.mode === "docker"
            ? "已保存；容器里的 nginx.conf 默认监听 80/443，改这里只改宿主端口"
            : "已保存，首次启动会按表单生成 nginx.conf");
        emit("saved", { config: { ...instForm }, switched });
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

/* 绑定域名下方的 hosts 指引行：建议行等宽可复制，“打开”链接与路径同列 */
.hosts-hint {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    margin-top: 2px;
}

.hosts-line {
    font-family: Consolas, Menlo, monospace;
}

.hosts-path {
    font-family: Consolas, Menlo, monospace;
    color: var(--color-text-4);
    word-break: break-all;
}

/* PHP 关联提示行：未装 PHP 的警示与已关联端口提示 */
.php-warn {
    margin: -6px 0 12px;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}
</style>
