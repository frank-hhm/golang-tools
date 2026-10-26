<template>
  <div class="local-service">
    <a-card class="side-card card-head-tool" size="small" :body-style="{ padding: 0 }">
      <template #title>本地服务</template>
      <template #extra>
        <div class="srv-head-actions">
          <a-button size="small" @click="openInstModal()">新建实例</a-button>
          <a-button size="small" @click="openVersionsModal()">版本管理</a-button>
        </div>
      </template>
      <!-- 引擎没起时先把话说明白：否则要等点「启动」撞一条管道错误才知道是 Docker 没跑 -->
      <common-docker-off class="srv-docker-off" :count="dockerInstCount" tool-label="MySQL" />
      <a-list :bordered="false" size="small">
        <a-list-item v-for="inst in srvInstances" :key="inst.id">
          <a-space>
            <com-badge :status="srvStates[inst.id]?.running ? 'success' : 'normal'"
              :animated="!!srvStates[inst.id]?.running" />
            <a-tooltip :content="inst.name + ':' + inst.port">
              <span class="conn-name">{{ inst.name }}</span>
            </a-tooltip>
            <a-tooltip v-if="inst.mode === 'docker'"
              :content="imgMissing(inst.image)
                ? `镜像 ${inst.image} 本机还没有，启动时会先自动拉取（可能几分钟）`
                : `镜像 ${inst.image}：这就是该实例的 MySQL 版本`">
              <a-tag :color="imgMissing(inst.image) ? 'orange' : 'purple'">Docker {{ imgTag(inst.image) }}</a-tag>
            </a-tooltip>
            <a-tag v-else color="arcoblue">v{{ inst.version }}</a-tag>
          </a-space>
          <template #actions>
            <a-space :size="2">
              <a-dropdown trigger="hover" @select="(v: any) => onSrvAction(v, inst)">
                <icon-more class="conn-more" @click.stop />
                <template #content>
                  <a-doption v-if="!srvStates[inst.id]?.running" value="start"><icon-play-arrow /> 启动</a-doption>
                  <a-doption v-else value="stop"><icon-pause /> 停止</a-doption>
                  <a-doption value="logs"><icon-list /> 日志/状态</a-doption>
                  <a-doption value="connect"><icon-link /> 连接</a-doption>
                  <a-doption value="edit"><icon-edit /> 编辑</a-doption>
                </template>
              </a-dropdown>
              <a-popconfirm :content="`确定删除实例「${inst.name}」？运行中会先停止；数据目录保留。`" type="warning" position="lt"
                @ok="removeInstance(inst)">
                <a-button size="small" status="danger"><icon-delete /></a-button>
              </a-popconfirm>
            </a-space>
          </template>
        </a-list-item>
        <a-list-item v-if="srvInstances.length === 0">
          <a-empty class="srv-empty" description="暂无实例，点 + 新建" />
        </a-list-item>
      </a-list>
    </a-card>

    <!-- 版本管理（对齐 Redis：a-space vertical fill + 三张 a-card，标题带图标、操作移 #extra、空态用 a-empty） -->
    <!-- 宽度 820：Docker 镜像表带上「使用实例」列后 680 会横向溢出 -->
    <!-- 固定高度：三个页签内容高矮不一，不钉住的话切一下弹窗就跳一次高度（Docker 页里还有两张表） -->
    <a-modal v-model:visible="versionsVisible" title="MySQL 版本管理" :width="820" :footer="false"
        :body-style="{ height: 'min(600px, calc(100vh - 220px))', overflowY: 'auto' }">
      <a-space direction="vertical" fill :size="12">
        <!-- 类型切换：本地 / 线上 / Docker 只看一类，不用在长弹窗里找 -->
        <a-radio-group size="medium" v-model="verTab" type="button">
          <a-radio value="local">已安装版本 {{ localVersions.length }}</a-radio>
          <a-radio value="remote">在线可安装 {{ remoteVersions.length }}</a-radio>
          <a-radio value="docker">Docker 实例 {{ dockerInstCount }}</a-radio>
        </a-radio-group>
        <!-- 已安装版本 -->
        <div v-show="verTab === 'local'">
        <a-card size="small" :body-style="{ padding: '8px 12px' }">
          <template #title><span class="ver-sec-title"><icon-storage /> 已安装版本</span></template>
          <div class="ver-scroll">
            <div v-for="v in localVersions" :key="v.version + v.source" class="ver-item">
              <a-tag size="small"
                :color="v.source === 'bundled' ? 'green' : (v.source === 'downloaded' ? 'arcoblue' : (v.source === 'system' ? 'gray' : 'orange'))">{{
                  srcLabel(v.source) }}</a-tag>
              <span class="ver-name">MySQL {{ v.version }}</span>
              <a-tooltip :content="v.path">
                <span class="ver-path">{{ v.path }}</span>
              </a-tooltip>
              <a-popconfirm :content="`确定删除 MySQL ${v.version}？若仍有实例引用该版本会被拒绝。`" type="warning" position="lt"
                :disabled="v.source === 'bundled' || v.source === 'system'"
                @ok="removeVersion(v)">
                <a-button size="small" status="danger" :disabled="v.source === 'bundled' || v.source === 'system'">删除</a-button>
              </a-popconfirm>
            </div>
            <a-empty v-if="localVersions.length === 0" :image="false" description="尚未安装任何版本，可从下方在线安装或添加本机目录" />
          </div>
          <div class="ver-actions">
            <a-input v-model="localDirInput" size="small" placeholder="本机 mysqld 所在目录" allow-clear />
            <a-button size="small" @click="chooseLocalDir"><template #icon><icon-folder /></template>选目录</a-button>
            <a-button size="small" @click="addLocalDir">添加</a-button>
          </div>
          <div class="ver-hint">离线安装：将 mysql-[ver]-winx64.zip 放入 <span class="mono">{{ srvPluginsDir }}</span> 后点「安装」即可
          </div>
        </a-card>
        </div>

        <!-- Docker 镜像与容器：Docker 实例的「版本」就是镜像 tag（mysql:8.4） -->
        <div v-show="verTab === 'docker'">
          <docker-images keyword="mysql" tool-label="MySQL" repo="mysql" :presets="IMAGE_PRESETS"
            :instances="srvInstances" :states="srvStates" :reload-key="verReload" @create="newInstanceFromImage"
            @use="editSrvById" @changed="refreshSrvStates" />
        </div>

        <!-- 在线版本 -->
        <div v-show="verTab === 'remote'">
        <a-card size="small" :body-style="{ padding: '8px 12px' }">
          <template #title>
            <span class="ver-sec-title"><icon-cloud /> 在线版本（cdn.mysql.com 官方 zip，包体约 230MB / 解压约 1.5GB）</span>
          </template>
          <template #extra>
            <a-button size="small" :loading="remoteLoading" @click="loadRemoteVersions">
              <template #icon><icon-refresh /></template>刷新
            </a-button>
          </template>
          <div class="ver-scroll">
            <div v-for="r in remoteVersions" :key="r.version" class="ver-item">
              <span class="ver-name">MySQL {{ r.version }}</span>
              <span class="ver-path">{{ r.size ? (r.size / 1024 / 1024).toFixed(0) + ' MB' : '' }}</span>
              <a-button size="small"
                :disabled="!!installProgress[r.version] || installedSet.has(r.version)" @click="installVersion(r)">{{
                  installedSet.has(r.version) ? '已安装' : '安装' }}</a-button>
            </div>
            <a-empty v-if="remoteVersions.length === 0" :image="false"
              :description="remoteLoading ? '加载中…' : '点「刷新」获取版本列表'" />
          </div>
          <div v-for="(p, ver) in installProgress" :key="'p' + ver" class="inst-prog">
            <div class="inst-prog-row">
              <span class="ver-name">{{ ver }}</span>
              <a-progress :percent="(p.percent || 0) / 100" size="small"
                :status="p.stage === 'error' ? 'danger' : 'normal'" style="flex: 1" />
              <span class="inst-stage">{{ stageText(p) }}</span>
              <a-button v-if="p.stage === 'error'" size="small" status="danger" @click="retryInstall(ver as string)">
                <template #icon><icon-refresh /></template>重试
              </a-button>
            </div>
            <div v-if="p.stage === 'error' && p.note" class="inst-err">
              <a-tooltip :content="p.note" position="tl">
                <a-alert type="error" :show-icon="false">{{ p.note }}</a-alert>
              </a-tooltip>
            </div>
          </div>
        </a-card>

        <!-- 安装日志 -->
        <a-card size="small" :body-style="{ padding: '8px 12px' }">
          <template #title><span class="ver-sec-title"><icon-history /> 安装日志</span></template>
          <template #extra>
            <a-button size="small" @click="installLogs = []"><template
                #icon><icon-delete /></template>清空</a-button>
          </template>
          <div class="ver-scroll inst-log">
            <div v-for="(l, i) in installLogs" :key="i" class="inst-log-line">{{ l }}</div>
            <a-empty v-if="installLogs.length === 0" :image="false" description="暂无安装记录" />
          </div>
        </a-card>
        </div>
      </a-space>
    </a-modal>

    <!-- 实例编辑 -->
    <a-modal v-model:visible="instVisible" :title="instForm.id ? '编辑实例' : '新建实例'" :width="460"
      :body-style="{ maxHeight: '72vh' }" @ok="saveInstance">
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
        </a-form-item>
        <!-- 两种模式行数一致（本地：版本 + 端口行；Docker：镜像/容器名 + 端口行），切模式时弹窗高度不跳 -->
        <a-row v-if="instForm.mode !== 'docker'" :gutter="12">
          <a-col :span="12">
            <a-form-item field="version" label="MySQL 版本" tooltip="需已安装，可在「版本管理」获取">
              <a-select v-model="instForm.version" placeholder="选择已安装版本">
                <a-option v-for="v in localVersions" :key="v.version" :value="v.version">
                  {{ v.version }}（{{ srcLabel(v.source) }}）
                </a-option>
              </a-select>
            </a-form-item>
          </a-col>
        </a-row>
        <a-row v-else :gutter="12">
          <a-col :span="24">
            <a-form-item class="img-item" field="image" label="镜像" required>
              <div class="img-row">
                <a-select v-model="instForm.image" class="img-select" placeholder="选择已拉取镜像" allow-create filterable
                  :loading="dockerImagesLoading">
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
                <span v-else>本地没有该镜像时，启动会自动拉取</span>
              </template>
            </a-form-item>
          </a-col>
          <a-col :span="12">
            <a-form-item field="containerName" label="容器名" tooltip="留空自动生成 go-tools-mysql-实例ID">
              <a-input v-model="instForm.containerName" placeholder="留空自动生成" />
            </a-form-item>
          </a-col>
        </a-row>
        <a-row :gutter="12">
          <a-col :span="12">
            <a-form-item field="port" label="端口" :error="portError"
              :tooltip="instForm.mode === 'docker' ? '容器内 3306 发布到 127.0.0.1 的该端口' : '实例只监听 127.0.0.1'">
              <a-input-number v-model="instForm.port" :min="1" :max="65535"
                @change="portError = ''" @blur="checkPortTaken" />
            </a-form-item>
          </a-col>
          <a-col :span="12">
            <a-form-item field="maxConnections" label="最大连接数" tooltip="0 = 使用 MySQL 默认值 151">
              <a-input-number v-model="instForm.maxConnections" :min="0" />
            </a-form-item>
          </a-col>
        </a-row>
        <a-form-item field="rootPassword" label="root 密码"
          :tooltip="instForm.mode === 'docker' ? '首次启动容器时写入；留空 = 空密码' : '初始化数据目录时写入；留空 = 空密码'">
          <a-input-password v-model="instForm.rootPassword" placeholder="留空则不设置" />
        </a-form-item>
      </a-form>
    </a-modal>

    <!-- 日志/状态 -->
    <a-modal v-model:visible="logsVisible" :title="logsInst ? `实例 ${logsInst.name}（:${logsInst.port}）` : '实例日志'"
      :width="660" :footer="false">
      <div v-if="logsStatus" class="ls-grid">
        <div class="ls-item"><span>模式</span><b>{{ logsStatus.mode === 'docker' ? 'Docker' : '本地' }}</b></div>
        <div class="ls-item"><span>状态</span><b :class="{ ok: logsStatus.running }">{{ logsStatus.running ? '运行中' : '已停止'
          }}</b></div>
        <div class="ls-item"><span>版本</span><b>{{ logsStatus.mode === 'docker' ? (logsStatus.image || '-') : (logsStatus.version
          || '-') }}</b></div>
        <div class="ls-item"><span>PID</span><b>{{ logsStatus.mode === 'docker' ? '容器内' : (logsStatus.pid || '-') }}</b></div>
        <template v-if="logsStatus.mode === 'docker'">
          <div class="ls-item"><span>容器</span>
            <a-tooltip :content="logsStatus.container || '-'" :disabled="!logsStatus.container">
              <b class="ellipsis">{{ logsStatus.container || '-' }}</b>
            </a-tooltip>
          </div>
          <div class="ls-item"><span>容器状态</span><b>{{ logsStatus.containerState || '-' }}</b></div>
        </template>
        <div class="ls-item"><span>运行时长</span><b>{{ fmtUptimeS(logsStatus.uptimeSeconds) }}</b></div>
        <div class="ls-item"><span>线程连接</span><b>{{ logsStatus.threadsConnected ?? '-' }}</b></div>
        <div class="ls-item"><span>线程活跃</span><b>{{ logsStatus.threadsRunning ?? '-' }}</b></div>
        <div class="ls-item"><span>最大连接</span><b>{{ logsStatus.maxConnections ?? '-' }}</b></div>
      </div>
      <a-tooltip v-if="logsStatus?.mode === 'docker' && logsStatus.dataDir" :content="logsStatus.dataDir">
        <div class="ls-dir">
          数据目录映射：<span class="mono">{{ logsStatus.dataDir }}</span> → <span class="mono">/var/lib/mysql</span>
        </div>
      </a-tooltip>
      <div ref="logBoxRef" class="ls-log">
        <div v-for="(l, i) in logsLines" :key="i" class="ls-line">{{ l }}</div>
        <div v-if="logsLines.length === 0" class="empty-tip">暂无日志</div>
      </div>
      <div class="ls-actions">
        <a-button size="small" @click="logsLines = []"><template #icon><icon-delete /></template>清空</a-button>
        <a-button size="small" type="outline" :disabled="!logsStatus?.running" @click="connectInstance(logsInst)">
          <template #icon><icon-link /></template>连接此实例
        </a-button>
      </div>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, nextTick, onMounted, onUnmounted, reactive, ref } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import { useRoute } from "vue-router";
import { useToolWindow } from "@/views/index/useToolWindow";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

interface SrvInstanceConfig {
  id: string; name: string; version: string; port: number;
  rootPassword: string; maxConnections: number;
  // Docker 运行模式：mode 空/local 为本地拉起 mysqld，docker 走 Engine API 管容器
  mode: string; image: string; containerName: string
}

interface SrvConnConfig {
  id: string; name: string; host: string; port: number;
  user: string; password: string; database: string
}

/** Docker 实例的版本就是镜像 tag：列表里只展示 tag（8.4），完整镜像名走 tooltip */
function imgTag(image: string): string {
  const s = String(image || "");
  const i = s.indexOf(":");
  return i >= 0 ? s.slice(i + 1) : (s || "Docker");
}

const emit = defineEmits<{ (e: "connect", cfg: SrvConnConfig): void }>();

const srvInstances = ref<SrvInstanceConfig[]>([]);
const srvStates = reactive<Record<string, any>>({});
const localVersions = ref<any[]>([]);
const remoteVersions = ref<any[]>([]);
const remoteLoading = ref(false);
const installProgress = reactive<Record<string, any>>({});
const installLogs = ref<string[]>([]);
const versionsVisible = ref(false);
/** 版本管理弹窗的类型切换：本地 / 线上 / Docker 只看一类 */
const verTab = ref<"local" | "remote" | "docker">("local");
/** 有多少实例跑在容器里（它们的版本由镜像 tag 决定，不占本机版本库） */
const dockerInstCount = computed(() => srvInstances.value.filter((i: any) => i.mode === "docker").length);
/** 版本管理里 Docker 镜像区的刷新信号：弹窗不销毁，每次打开自增一次触发重拉 */
const verReload = ref(0);
const instVisible = ref(false);
const logsVisible = ref(false);
const localDirInput = ref("");
const srvPluginsDir = ref("");
const logsInst = ref<SrvInstanceConfig | null>(null);
const logsStatus = ref<any>(null);
const logsLines = ref<string[]>([]);
const logBoxRef = ref<HTMLElement | null>(null);

const instForm = reactive<SrvInstanceConfig>({
  id: "", name: "", version: "", port: 3306, rootPassword: "", maxConnections: 0,
  mode: "local", image: "", containerName: "",
});
/** 端口占用提示：贴表单项底下（红字），保存前也用同一份结果拦一次 */
const portError = ref("");

/* Docker 模式的镜像候选：本地已有镜像 tag（需引擎在线）+ 官方常用 tag 兑底 */
const IMAGE_PRESETS = ["mysql:8.4", "mysql:8.0", "mysql:5.7", "mariadb:11", "percona:8.0"];
/* 名字不能回退成 dockerImages：script setup 下 kebab 标签 docker-images 会优先拿同名 setup 变量当组件，整个 Docker 区会渲染成空白 */
const dockerImageList = ref<string[]>([]);
const defaultImage = ref(IMAGE_PRESETS[0]);
const imageOptions = computed(() => Array.from(new Set<string>([...dockerImageList.value, ...IMAGE_PRESETS])));
const dockerImagesLoading = ref(false);
/** 引擎不可用原因：填 Docker 模式表单时先报出来，别等启动那一步 */
const dockerErr = ref("");
const { openToolWindow } = useToolWindow();

/** 只刷新镜像清单（不写表单）：用来标记实例引用的镜像本机是否已拉取 */
async function probeDockerImages() {
  try { dockerImageList.value = (await GoTools.MySQLSrvDockerImages()) || []; }
  catch (e) { /* 引擎未就绪就不做校验，免得误报 */ }
}
/** 清单非空且不含它 → 本机还没有；启动时会自动拉 */
function imgMissing(image: string): boolean {
  return !!image && dockerImageList.value.length > 0 && !dockerImageList.value.includes(image);
}

async function loadImageOptions() {
  dockerImagesLoading.value = true;
  try { dockerImageList.value = (await GoTools.MySQLSrvDockerImages()) || []; }
  catch (e) { dockerImageList.value = []; } // 引擎不可用时仅展示预置 tag
  dockerImagesLoading.value = false;
}

/** 引擎就绪与否：复用 Docker 服务的状态接口（不可达也返回，带可读 error） */
async function loadDockerEngine() {
  try {
    const info = await GoTools.DockerSrvEngineStatus();
    dockerErr.value = info?.available ? "" : (info?.error || "Docker 引擎不可用");
  } catch (e: any) { dockerErr.value = e?.message || String(e); }
}

/** 打开 Docker 管理窗口（独立 webview，单例复用）：拉镜像、看拉取进度、管容器 */
function openDockerWindow() {
  openToolWindow("/tools/docker", "Docker管理", { single: true, width: 1200, height: 800 });
}

function onModeChange(v: string | number | boolean) {
  if (v !== "docker") return;
  if (!instForm.image) instForm.image = defaultImage.value;
  loadImageOptions();
  loadDockerEngine();
}

/** 版本管理里点「新建实例」：直接以该镜像开一个 Docker 实例 */
function newInstanceFromImage(image: string) {
  versionsVisible.value = false;
  openInstModal();
  instForm.mode = "docker";
  instForm.image = image;
}

/** 版本管理里点「使用实例」：从镜像视角转去改该实例的版本（镜像） */
function editSrvById(id: string) {
  const inst = srvInstances.value.find((i: any) => i.id === id);
  if (!inst) return;
  versionsVisible.value = false;
  openInstModal(inst);
}

const installedSet = computed(() => new Set<string>(localVersions.value.map((v: any) => v.version)));

let offInstall: any = null;
const route = useRoute();
let offSelInst: any = null;
let srvTimer: any = null;

function pushInstallLog(txt: string) {
  if (!txt) return;
  const t = new Date();
  const pad = (n: number) => n.toString().padStart(2, "0");
  installLogs.value.push(`[${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}] ${txt}`);
  if (installLogs.value.length > 200) installLogs.value.splice(0, installLogs.value.length - 200);
}

function srcLabel(s: string): string {
  return s === "bundled" ? "内置" : s === "downloaded" ? "下载" : s === "system" ? "系统包" : "本机";
}

function stageText(p: any): string {
  if (!p) return "";
  if (p.stage === "downloading") return `下载中 ${p.percent || 0}%`;
  if (p.stage === "extracting") return `解压中 ${p.percent || 0}%`;
  if (p.stage === "done") return "完成";
  return "失败";
}

function fmtUptimeS(sec: number): string {
  if (!sec || sec <= 0) return "-";
  const d = Math.floor(sec / 86400); const h2 = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60); const s = sec % 60;
  if (d > 0) return `${d}d ${h2}h ${m}m`; if (h2 > 0) return `${h2}h ${m}m`; return `${m}m ${s}s`;
}

async function loadLocalVersions() {
  try { localVersions.value = (await GoTools.MySQLSrvListVersions()) || []; }
  catch (e) { localVersions.value = []; }
}

async function loadSrvInstances() {
  try { srvInstances.value = (await GoTools.MySQLSrvListInstances()) || []; }
  catch (e) { srvInstances.value = []; }
  refreshSrvStates();
}

/**
 * 状态轮询单飞：上一轮没回来就跳过本轮。
 * Docker 实例的状态要走 Engine API，引擎卡住时一次能挂十几秒，定时器照旧触发就会把
 * 请求叠成雪崩（终端满屏 Binding 调用失败 + Docker Desktop 被拖到 CPU 100%）。
 */
let srvStatesInflight = false;
let srvStatesQuietUntil = 0;

async function refreshSrvStates(silent = false) {
  if (srvStatesInflight) return;
  if (silent && Date.now() < srvStatesQuietUntil) return;
  srvStatesInflight = true;
  let ok = 0;
  try {
    // 并发问每个实例：串行 await 会变成 N 次往返，Web 模式在远程服务器上尤其明显
    await Promise.all(srvInstances.value.map(async (inst) => {
      try {
        const status = await GoTools.MySQLSrvStatus(inst.id);
        srvStates[inst.id] = status;
        ok++;
        if (logsVisible.value && logsInst.value?.id === inst.id) logsStatus.value = status;
      } catch (e) { /* 单个实例失败不影响其它实例 */ }
    }));
  } finally {
    // 一轮全失败：多半是引擎离线，退避 15 秒再来问
    srvStatesQuietUntil = ok === 0 && srvInstances.value.length > 0 ? Date.now() + 15000 : 0;
    srvStatesInflight = false;
  }
}

async function openVersionsModal() {
  versionsVisible.value = true;
  verTab.value = "local";
  verReload.value++;
  loadLocalVersions();
  if (!srvPluginsDir.value) {
    try { srvPluginsDir.value = (await GoTools.MySQLSrvPluginsDir()) || ""; } catch (e) { /* ignore */ }
  }
}

async function loadRemoteVersions() {
  remoteLoading.value = true;
  try { remoteVersions.value = (await GoTools.MySQLSrvRemoteVersions()) || []; }
  catch (e: any) { Message.error("获取远程版本失败: " + (e?.message || e)); }
  remoteLoading.value = false;
}

async function installVersion(r: any) {
  try {
    pushInstallLog(`提交安装请求：MySQL ${r.version}`);
    await GoTools.MySQLSrvInstall(r.version);
    installProgress[r.version] = { stage: "downloading", percent: 0 };
  } catch (e: any) {
    pushInstallLog(`安装请求被拒绝：${e?.message || e}`);
    Message.error("安装失败: " + (e?.message || e));
  }
}

function retryInstall(ver: string) {
  delete installProgress[ver];
  installVersion({ version: ver });
}

async function removeVersion(v: any) {
  try { await GoTools.MySQLSrvRemoveVersion(v.version); Message.success("已删除"); loadLocalVersions(); }
  catch (e: any) { Message.error("删除失败: " + (e?.message || e)); }
}

async function chooseLocalDir() {
  try { const dir = await GoTools.MySQLSrvChooseDir(); if (dir) localDirInput.value = dir; }
  catch (e) { /* cancel */ }
}

async function addLocalDir() {
  const dir = localDirInput.value.trim();
  if (!dir) { Message.warning("请先选择或填写目录"); return; }
  try { await GoTools.MySQLSrvAddLocal(dir); Message.success("已添加本机目录"); localDirInput.value = ""; loadLocalVersions(); }
  catch (e: any) { Message.error("添加失败: " + (e?.message || e)); }
}

function openInstModal(inst?: SrvInstanceConfig) {
  portError.value = "";
  dockerErr.value = "";
  if (inst) {
    // 历史配置无 mode/image/containerName 字段，先给默认值再覆盖
    Object.assign(instForm, { mode: "local", image: "", containerName: "" }, { ...inst });
  } else {
    // 默认端口也要能用：避开其它实例已登记 / 系统已监听的
    GoTools.MySQLSrvSuggestPort(3306)
      .then((p: number) => { instForm.port = p || 3306; })
      .catch(() => { /* 探测失败就留默认值 */ });
    Object.assign(instForm, { id: "", name: "", version: localVersions.value[0]?.version || "", port: 3306, rootPassword: "", maxConnections: 0, mode: "local", image: "", containerName: "" });
  }
  if (localVersions.value.length === 0) loadLocalVersions();
  loadImageOptions();
  if (instForm.mode === "docker") loadDockerEngine();
  instVisible.value = true;
}

async function checkPortTaken() {
  if (!instForm.port) { portError.value = ""; return; }
  try {
    const reason = await GoTools.MySQLSrvPortTaken(instForm.port, instForm.id || "");
    if (!reason) { portError.value = ""; return; }
    const suggest = await GoTools.MySQLSrvSuggestPort(instForm.port);
    portError.value = suggest && suggest !== instForm.port ? `${reason}，可用：${suggest}` : reason;
  } catch (e) { /* ignore */ }
}

async function saveInstance() {
  if (!instForm.name) { Message.warning("请填写实例名称"); return; }
  if (instForm.mode === "docker") {
    if (!String(instForm.image || "").trim()) { Message.warning("Docker 模式需指定镜像，如 " + defaultImage.value); return; }
  } else if (!instForm.version) { Message.warning("请选择 MySQL 版本"); return; }
  if (!instForm.port) { Message.warning("请填写端口"); return; }
  // 端口占用先在本地拦一次，后端保存时还会再校验
  try {
    const reason = await GoTools.MySQLSrvPortTaken(instForm.port, instForm.id || "");
    if (reason) {
      const suggest = await GoTools.MySQLSrvSuggestPort(instForm.port);
      portError.value = suggest && suggest !== instForm.port ? `${reason}，可用：${suggest}` : reason;
      Message.warning(portError.value);
      return;
    }
  } catch (e) { /* 探测失败不阻断，后端兜底 */ }
  try {
    // 编辑前先记下原配置：切了版本/镜像且实例在跑，容器里还是旧版本，得重启才生效
    const old = instForm.id ? srvInstances.value.find((i) => i.id === instForm.id) : null;
    const switched = !!old && (old.mode !== instForm.mode || old.image !== instForm.image || old.version !== instForm.version);
    const wasRunning = !!old && !!srvStates[old.id]?.running;
    await GoTools.MySQLSrvSaveInstance({ ...instForm });
    instVisible.value = false; Message.success("已保存"); await loadSrvInstances();
    if (switched && wasRunning) setTimeout(() => askRestartAfterSwitch({ ...instForm }), 240);
  } catch (e: any) { Message.error("保存失败: " + (e?.message || e)); }
}

/** 切了版本/镜像又没重启：跑着的还是旧版本，问一句要不要现在就换过去 */
function askRestartAfterSwitch(inst: SrvInstanceConfig) {
  const what = inst.mode === "docker" ? `镜像 ${inst.image}` : `MySQL ${inst.version}`;
  Modal.confirm({
    title: "版本已切换",
    content: `${what} 已写入配置，但实例「${inst.name}」还在跑旧版本；重启后才会生效（期间会短暂中断服务）。`,
    okText: "立即重启",
    cancelText: "稍后手动重启",
    onOk: async () => {
      try {
        // MySQL 没有单独的 restart，停完再起；停止失败就不硬起，免得端口冲突
        await GoTools.MySQLSrvStop(inst.id);
        await GoTools.MySQLSrvStart(inst.id);
        Message.success(`实例 ${inst.name} 已重启，新版本已生效`);
      } catch (e: any) { Message.error("重启失败: " + (e?.message || e)); }
      refreshSrvStates();
    },
  });
}

async function onSrvAction(v: string, inst: SrvInstanceConfig) {
  if (v === "start") {
    try { await GoTools.MySQLSrvStart(inst.id); Message.success(`实例 ${inst.name} 已启动`); }
    catch (e: any) { Message.error("启动失败: " + (e?.message || e)); }
    refreshSrvStates();
  } else if (v === "stop") {
    try { await GoTools.MySQLSrvStop(inst.id); Message.success(`实例 ${inst.name} 已停止`); }
    catch (e: any) { Message.error("停止失败: " + (e?.message || e)); }
    refreshSrvStates();
  } else if (v === "logs") {
    openLogs(inst);
  } else if (v === "connect") {
    connectInstance(inst);
  } else if (v === "edit") {
    openInstModal(inst);
  }
}

/** 删除实例（确认由 Popconfirm 承担） */
async function removeInstance(inst: SrvInstanceConfig) {
  try {
    await GoTools.MySQLSrvRemoveInstance(inst.id);
    delete srvStates[inst.id];
    await loadSrvInstances();
    Message.success("实例已删除");
  } catch (e: any) { Message.error("删除失败: " + (e?.message || e)); }
}

/** 实例连接交给父页面打开（需要合并已保存连接） */
function connectInstance(inst: SrvInstanceConfig | null) {
  if (!inst) return;
  if (!srvStates[inst.id]?.running) { Message.warning("请先启动实例"); return; }
  emit("connect", {
    id: "srv-" + inst.id, name: `${inst.name}(${inst.mode === "docker" ? "容器" : "本地"})`,
    host: "127.0.0.1", port: inst.port, user: "root", password: inst.rootPassword, database: "",
  });
}

let logsTimer: any = null;
/** 日志轮询单飞 + 失败退避：引擎卡住时不要每 2 秒叠一个新请求上去 */
let logsInflight = false;
let logsQuietUntil = 0;
async function openLogs(inst: SrvInstanceConfig) {
  logsInst.value = inst;
  logsLines.value = [];
  logsStatus.value = srvStates[inst.id] || null;
  logsVisible.value = true;
  logsQuietUntil = 0;
  try { logsLines.value = (await GoTools.MySQLSrvLogs(inst.id, 300)) || []; } catch (e) { /* ignore */ }
  if (logsTimer) clearInterval(logsTimer);
  logsTimer = setInterval(async () => {
    if (!logsVisible.value) { clearInterval(logsTimer); logsTimer = null; return; }
    if (logsInflight || Date.now() < logsQuietUntil) return;
    logsInflight = true;
    try {
      logsLines.value = (await GoTools.MySQLSrvLogs(inst.id, 300)) || [];
      logsQuietUntil = 0;
      nextTick(() => { if (logBoxRef.value) logBoxRef.value.scrollTop = logBoxRef.value.scrollHeight; });
    } catch (e) {
      logsQuietUntil = Date.now() + 15000;
    } finally {
      logsInflight = false;
    }
  }, 2000);
}

onMounted(async () => {
  await loadSrvInstances();
  // 镜像清单不进表单，只用来标记「实例引用的镜像本机还没拉」
  probeDockerImages();
  try { defaultImage.value = (await GoTools.MySQLSrvDefaultImage()) || IMAGE_PRESETS[0]; } catch (e) { /* ignore */ }
  // 主页点击服务实例进入（?inst=）：直接打开该实例的日志/状态详情
  const wantId = String(route.query.inst ?? "");
  if (wantId) {
    const target = srvInstances.value.find((i) => i.id === wantId);
    if (target) openLogs(target);
  }
  loadLocalVersions();
  srvTimer = setInterval(() => { refreshSrvStates(true); }, 3000);
  // 单例窗口已打开时，主页再次点击其他实例 → 通过事件切换
  offSelInst = Events.On("tool:select-inst", (ev: any) => {
    const p = ev?.data ?? ev;
    if (p?.type !== "mysql" || !p?.id) return;
    const target = srvInstances.value.find((i) => i.id === String(p.id));
    if (target) openLogs(target);
  });
  offInstall = Events.On("mysql:install", (ev: any) => {
    const p = ev?.data ?? ev;
    if (!p || !p.version) return;
    if (p.stage === "info") {
      pushInstallLog(p.note || "");
    } else if (p.stage === "done") {
      delete installProgress[p.version];
      pushInstallLog(`安装完成 MySQL ${p.version}` + (p.note ? "：" + p.note : ""));
      Message.success(`MySQL ${p.version} 安装完成`);
      loadLocalVersions();
    } else if (p.stage === "error") {
      installProgress[p.version] = p;
      pushInstallLog(`安装失败 ${p.version}：${p.note || "未知错误"}（可将 mysql zip 放入插件目录后离线安装）`);
      Modal.error({
        title: "安装失败",
        content: `${p.version}：${p.note || "未知错误"}。可将 mysql zip 放入插件目录后重试（离线安装）。`,
      });
    } else {
      installProgress[p.version] = p;
    }
  });
});

onUnmounted(() => {
  if (srvTimer) clearInterval(srvTimer);
  if (logsTimer) clearInterval(logsTimer);
  if (offInstall) offInstall();
  if (offSelInst) offSelInst();
});
</script>

<style scoped>
.local-service {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

/* 卡片撑满左栏，body 弹性填充供列表滚动 */
.side-card {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.side-card :deep(.arco-card-body) {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.side-card :deep(.arco-list) {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
}

.srv-head-actions {
  display: flex;
  align-items: center;
  gap: 2px;
}

.conn-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.conn-more {
  color: var(--color-text-3);
}

/* 空状态：覆盖 a-list 内 .arco-empty 的横排居中，改为图标在上、文字在下 */
.srv-empty,
.side-card :deep(.arco-list-content .arco-empty) {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  height: auto;
  padding: 20px 8px;
}

.srv-empty :deep(.arco-empty-image),
.side-card :deep(.arco-list-content .arco-empty .arco-empty-image) {
  display: block;
  margin-bottom: 0;
  font-size: 40px;
  line-height: 1;
}

.srv-empty :deep(.arco-empty-description),
.side-card :deep(.arco-list-content .arco-empty .arco-empty-description) {
  display: block;
  font-size: var(--base-size-small);
  color: var(--color-text-3);
  line-height: 1.6;
  text-align: center;
  white-space: normal;
}

.empty-tip {
  color: var(--color-text-3);
  text-align: center;
  font-size: var(--base-size-small);
  padding: var(--base-padding);
}

/* ---- 版本管理弹窗 ---- */
.ver-sec-title {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
  font-size: var(--base-size-default);
}

.ver-scroll {
  max-height: 170px;
  overflow-y: auto;
}

.ver-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: var(--base-radius-default) var(--base-padding);
  border-radius: var(--base-radius-default);
  font-size: var(--base-size-small);
}

.ver-item:hover {
  background: var(--color-fill-2);
}

.ver-name {
  font-weight: 500;
  min-width: 100px;
}

.ver-path {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--color-text-3);
  font-size: var(--base-size-small);
}

.ver-actions {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-top: var(--base-padding);
}

.ver-actions :deep(.arco-input-wrapper) {
  flex: 1;
}

.ver-hint {
  font-size: var(--base-size-small);
  color: var(--color-text-3);
  margin-top: var(--base-radius-default);
}

/* 日志弹窗：docker 实例的数据目录映射行 */
.ls-dir {
  font-size: var(--base-size-small);
  color: var(--color-text-3);
  margin-bottom: var(--base-radius-default);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ls-item b.ellipsis {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: bottom;
}

.mono {
  font-family: Consolas, monospace;
  background: var(--color-fill-2);
  padding: var(--base-radius-small) var(--base-radius-default);
  border-radius: var(--base-radius-small);
}

/* 安装进度 */
.inst-prog {
  margin-top: var(--base-padding);
}

.inst-prog-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.inst-stage {
  font-size: var(--base-size-small);
  color: var(--color-text-2);
  min-width: 80px;
}

/* 安装失败提示：内容是 a-alert，这里只管间距，颜色交给 alert 的类型色 */
.inst-err {
  padding: var(--base-radius-default) 0 0 0;
  word-break: break-all;
}

/* 安装失败原因常带着下载地址：两行截断，不把版本弹窗撑高；完整内容悬停看 */
.inst-err :deep(.arco-alert-content) {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

/* 安装日志：随主题自适应的控制台风格面板 */
.inst-log {
  max-height: 130px;
  background: var(--color-fill-1);
  border: 1px solid var(--color-border-2);
  border-radius: var(--base-radius-default);
  padding: var(--base-radius-default) var(--base-radius-large);
}

.inst-log-line {
  font-family: Consolas, monospace;
  font-size: var(--base-size-small);
  color: var(--color-text-2);
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
}

/* 日志/状态弹窗 */
.ls-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  gap: 8px;
  margin-bottom: var(--base-margin);
}

.ls-item {
  background: var(--color-fill-1);
  border-radius: var(--base-radius-default);
  padding: var(--base-radius-default) var(--base-radius-large);
  font-size: var(--base-size-small);
}

.ls-item span {
  color: var(--color-text-3);
  display: block;
}

.ls-item b {
  font-size: var(--base-size-small);
}

.ls-item b.ok {
  color: rgb(var(--green-6));
}

.ls-log {
  background: var(--color-fill-1);
  border: 1px solid var(--color-border-2);
  border-radius: var(--base-radius-default);
  padding: var(--base-padding) var(--base-radius-large);
  max-height: 320px;
  overflow-y: auto;
  font-family: Consolas, monospace;
  font-size: var(--base-size-small);
  color: var(--color-text-1);
  line-height: 1.5;
}

.ls-line {
  white-space: pre-wrap;
  word-break: break-all;
}

.ls-actions {
  display: flex;
  gap: 8px;
  margin-top: var(--base-radius-large);
}

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
