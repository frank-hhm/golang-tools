<template>
  <layout-body height="full" :isBg="false">
    <a-row :gutter="12" class="home-row">
      <a-col :span="16" class="left-col">
        <!-- ========== 左侧：系统信息 ========== -->

        <a-row class="left-inner">
          <a-col :span="24">
            <a-card class="card-head-tool">

              <template #title>
                <!-- 欢迎栏 -->
                <span>欢迎回来！</span>
              </template>
              <template #extra>
                <a-button size="small" :loading="sysLoading" @click="refreshSysInfo">
                  <template #icon><icon-refresh /></template>
                </a-button>
              </template>

              <!-- 统计卡片行：右上角大号数值 + 下方详情（核/线程、开机时间、占用明细等）；
                   栅格用 a-row/a-col，横纵间距由 gutter 提供 -->
              <a-row :gutter="[12, 12]" class="stat-cards">
                <a-col v-for="c in statCards" :key="c.label" :span="8">
                  <div class="stat-card">
                    <div class="stat-circle" :class="c.circle">
                      <common-stat-icon :name="c.icon" :value="c.p" :size="26" />
                    </div>
                    <div class="stat-info">
                      <div class="stat-head">
                        <div class="stat-label">{{ c.label }}</div>
                        <div class="stat-big">{{ c.big }}<span v-if="c.unit" class="stat-unit">{{ c.unit }}</span></div>
                      </div>
                      <div class="stat-detail">{{ c.detail }}</div>
                    </div>
                  </div>
                </a-col>
              </a-row>
            </a-card>
          </a-col>
          <a-col :span="24" class="svc-col">
            <a-card class="svc-card-fill card-head-tool">
              <!-- 本地服务实例卡片 -->
              <template #title>
                <span class="svc-title">本地服务实例</span>
              </template>
              <template #extra>
                <a-radio-group size="medium" v-model="svcFilter" type="button">
                  <a-radio value="all">全部</a-radio>
                  <a-radio value="redis">Redis</a-radio>
                  <a-radio value="mysql">MySQL</a-radio>
                  <a-radio value="php">PHP</a-radio>
                  <a-radio value="nginx">Nginx</a-radio>
                  <a-radio value="npm">Npm</a-radio>
                  <a-radio value="docker">Docker</a-radio>
                </a-radio-group>
              </template>
              <div class="svc-cards" v-if="filteredServices.length">
                <div v-for="(s, i) in filteredServices" :key="i" class="svc-card" @click="openServiceDetail(s)">
                  <div class="svc-card-top">
                    <div class="svc-logo" :class="'logo-' + s.type">
                      <span v-if="s.type === 'redis'" class="icon icon-icon-redis" />
                      <span v-else-if="s.type === 'php'" class="icon icon-php" />
                      <span v-else-if="s.type === 'mysql'" class="icon icon-MySQL" />
                      <span v-else-if="s.type === 'nginx'" class="icon icon-ngnix" />
                      <span v-else-if="s.type === 'npm'" class="icon icon-npm" />
                      <icon-computer v-else-if="s.type === 'docker'" />
                    </div>
                    <div class="svc-meta">
                      <a-tooltip :content="s.name">
                        <div class="svc-name">{{ s.name }}</div>
                      </a-tooltip>
                      <div class="svc-desc">{{ svcDesc(s) }}</div>
                    </div>
                  </div>
                  <div class="svc-card-bottom" @click.stop>
                    <!-- 状态圆点承担颜色语义（与 a-badge 一致），文案只做说明；运行中开启动效 -->
                    <div class="com-state" :class="s.running ? 'is-on' : 'is-dim'">
                      <com-badge :status="s.running ? 'success' : 'normal'" :animated="s.running"></com-badge>
                      <span>{{ s.running ? '已启用' : '已停止' }}</span>
                    </div>
                    <a-switch v-if="s.type !== 'docker'" :model-value="s.running" :loading="svcToggling === s.id" size="small"
                      @change="toggleService(s)" />
                    <a-tag v-else size="small" color="arcoblue" @click="openServiceDetail(s)">面板管理</a-tag>
                  </div>
                </div>
              </div>
              <a-empty v-else description="暂无服务实例" />
            </a-card>
          </a-col>
        </a-row>
      </a-col>

      <a-col :span="8" class="right-col">

        <!-- ========== 右侧：当前时间 ========== -->
        <div class="clock-body">
          <!-- Canvas 指针时钟：盘面已含日期/星期/年份/毫秒时间跑动信息 -->
          <common-canvas-clock :size="180" />
        </div>

        <!-- ========== 右侧：快捷操作 ========== -->
        <a-card class="tools-card">
          <template #title>
            <span>快捷操作</span>
          </template>
          <template #extra>
          </template>
          <div class="tools-grid">
            <a-tooltip v-for="(item, index) in toolsList" :key="item.id || index"
              :content="item.shortcut ? item.name + ' (' + item.shortcut + ')' : item.name">
              <div class="tool-cell" @click="onToolClick(item)">
                <div class="tool-icon-wrap">
                  <i v-if="isIconfontIcon(item.icon)" class="icon tool-icon" :class="item.icon" />
                  <component v-else :is="iconComp(item.icon) || 'IconApps'" class="tool-icon" />
                </div>
              </div>
            </a-tooltip>
          </div>
        </a-card>
      </a-col>
    </a-row>

  </layout-body>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onMounted, onUnmounted, reactive, ref } from "vue";
import { Events } from "@wailsio/runtime";
import { IconCheckCircleFill, IconExclamationCircleFill } from "@arco-design/web-vue/es/icon";
import { useToolEvents, type ToolItem } from "./events";
import { useToolWindow } from "./useToolWindow";
import { arcoIconComp as iconComp, isIconfontIcon, normalizeToolIcon } from "@/utils/tool-icon";

interface ToolRow extends ToolItem {
  id?: string;
  key?: string;
  icon?: string;
  shortcut?: string;
}

const toolEvents = reactive(useToolEvents());
const { openToolWindow } = useToolWindow();
const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

/* ==================== 工具列表 ==================== */
const toolsList = ref<ToolRow[]>([]);

/** 上次加载的工具列表签名：设置页自动保存会频繁广播事件，内容未变时跳过赋值 */
let toolsSig = "";

async function loadTools() {
  try {
    const cfg = await GoTools?.GetConfig();
    const list: any[] = cfg?.tools || [];
    const next = list.map((t) => ({
      id: t.id, key: t.key, name: t.name, path: t.path || undefined,
      event: t.event || undefined, single: !!t.single, fullscreen: !!t.fullscreen,
      disableFullscreen: !!t.disableFullscreen,
      width: t.width || undefined, height: t.height || undefined, icon: normalizeToolIcon(t.icon), shortcut: t.shortcut,
    }));
    const sig = JSON.stringify(next);
    if (sig === toolsSig) return;
    toolsSig = sig;
    toolsList.value = next;
  } catch { /* */ }
}

const onToolClick = (item: ToolRow) => {
  if (item.event) { toolEvents.runEvent(item.event as any); return; }
  if (item.path) {
    openToolWindow(item.path, item.name, { single: item.single, fullscreen: item.fullscreen, disableFullscreen: item.disableFullscreen, width: item.width, height: item.height });
  }
};

/** 设置入口（从顶部 header 移到快捷操作卡片右上角） */
function openSettings() {
  openToolWindow("/tools/settings", "设置", { single: true });
}
function activateById(id: string) {
  const item = toolsList.value.find((t) => t.id === id || t.key === id);
  if (item) onToolClick(item);
}

/* ==================== 系统信息 ==================== */
const sysInfo = ref<any>(null);

/** 图标名，对应 common-stat-icon 内置的六套图形 */
type StatIconName = "cpu" | "mem" | "uptime" | "go" | "disk" | "net";

/** 统计卡片数据源：模板只保留一份卡片结构，新增/调整卡片只改这里 */
interface StatCard {
  circle: string;
  label: string;
  big: string;
  unit?: string;
  detail: string;
  /** 图标名（见 common-stat-icon） */
  icon: StatIconName;
  /** 数据驱动进度 0-100：CPU 脉冲频率 / 内存液位 / 磁盘进度条 */
  p?: number;
}
const statCards = computed<StatCard[]>(() => {
  const s = sysInfo.value;
  return [
    {
      circle: "stat-circle-cpu", icon: "cpu", label: "CPU",
      big: s ? s.cpuUsage.toFixed(1) : "--", unit: "%",
      detail: s ? s.cpuCores + " 核 · " + s.cpuThreads + " 线程" : "",
      p: s?.cpuUsage,
    },
    {
      circle: "stat-circle-mem", icon: "mem", label: "内存",
      big: s ? s.memPercent.toFixed(1) : "--", unit: "%",
      detail: s ? fmtBytes(s.memUsed) + " / " + fmtBytes(s.memTotal) : "",
      p: s?.memPercent,
    },
    {
      circle: "stat-circle-up", icon: "uptime", label: "运行时间",
      big: s ? fmtUptime(s.uptime) : "--",
      detail: s ? "开机 " + fmtBootTime(s.bootTime) : "",
    },
    {
      circle: "stat-circle-go", icon: "go", label: "Go 运行时",
      big: s ? String(s.goroutines) : "--", unit: " 协程",
      detail: s ? "堆 " + fmtBytes(s.appMemAlloc) : "",
    },
    {
      circle: "stat-circle-disk", icon: "disk", label: "磁盘 C:",
      big: s ? s.diskPercent.toFixed(1) : "--", unit: "%",
      detail: s ? fmtBytes(s.diskUsed) + " / " + fmtBytes(s.diskTotal) : "",
      p: s?.diskPercent,
    },
    {
      circle: "stat-circle-net", icon: "net", label: "网络",
      big: s ? fmtBytes(s.netSent + s.netRecv) : "--", unit: " 累计",
      detail: s ? "↑ " + fmtBytes(s.netSent) + "  ↓ " + fmtBytes(s.netRecv) : "",
    },
  ];
});
const sysLoading = ref(false);
const svcFilter = ref<"all" | "redis" | "mysql" | "php" | "nginx" | "npm" | "docker">("all");
const svcToggling = ref<string | null>(null);

const filteredServices = computed(() => {
  const list = sysInfo.value?.services || [];
  if (svcFilter.value === "all") return list;
  return list.filter((s: any) => s.type === svcFilter.value);
});

const SVC_TYPE_LABELS: Record<string, string> = {
  redis: "Redis", mysql: "MySQL", php: "PHP", nginx: "Nginx", npm: "Npm", docker: "Docker",
};
function svcTypeLabel(t: string): string {
  return SVC_TYPE_LABELS[t] || (t ? t.toUpperCase() : "-");
}

/** 卡片副标题：Docker 引擎无端口/版本位仅展示引擎版本 */
function svcDesc(s: any): string {
  if (s.type === "docker") return `${svcTypeLabel(s.type)} 引擎 · v${s.version || "-"}`;
  return `${svcTypeLabel(s.type)} · v${s.version} · :${s.port}`;
}

/** 服务类型 → 对应管理工具路由 */
const SVC_TYPE_ROUTES: Record<string, string> = {
  redis: "/tools/redis", mysql: "/tools/mysql", php: "/tools/php", nginx: "/tools/nginx", npm: "/tools/npm",
  docker: "/tools/docker",
};

/** 点击服务卡片：打开对应工具窗口并带 ?inst= 定位实例；窗口已存在（单例复用）时靠事件通知其切换实例 */
function openServiceDetail(s: any) {
  const path = SVC_TYPE_ROUTES[s.type];
  if (!path) return;
  const item = toolsList.value.find((t) => t.key === s.type);
  openToolWindow(path + "?inst=" + encodeURIComponent(s.id), item?.name || svcTypeLabel(s.type) + "管理", {
    single: item?.single ?? true,
    fullscreen: item?.fullscreen,
    disableFullscreen: item?.disableFullscreen,
    width: item?.width,
    height: item?.height,
  });
  try { Events.Emit("tool:select-inst", { type: s.type, id: String(s.id) }); } catch { /* */ }
}

async function refreshSysInfo() {
  sysLoading.value = true;
  try { sysInfo.value = await GoTools?.GetSystemInfo(); } catch { /* */ }
  sysLoading.value = false;
}

async function toggleService(s: any) {
  svcToggling.value = s.id;
  try {
    const stopMap: Record<string, string> = {
      redis: "RedisSrvStop", mysql: "MySQLSrvStop", php: "PhpSrvStop", nginx: "NginxSrvStop", npm: "NpmSrvStop",
    };
    const startMap: Record<string, string> = {
      redis: "RedisSrvStart", mysql: "MySQLSrvStart", php: "PhpSrvStart", nginx: "NginxSrvStart", npm: "NpmSrvStart",
    };
    if (s.running) {
      const fn = (GoTools as any)?.[stopMap[s.type] || ""];
      if (fn) await fn(s.id);
    } else {
      const fn = (GoTools as any)?.[startMap[s.type] || ""];
      if (fn) await fn(s.id);
    }
    await refreshSysInfo();
  } catch (e) { /* */ }
  svcToggling.value = null;
}

function fmtBytes(b: number): string {
  if (!b) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"]; let i = 0; let v = b;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return v.toFixed(i > 0 ? 1 : 0) + " " + units[i];
}
function fmtUptime(sec: number): string {
  if (!sec) return "-";
  const d = Math.floor(sec / 86400); const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  if (d > 0) return `${d}天${h}时`; if (h > 0) return `${h}时${m}分`; return `${m}分`;
}
// 开机时间：bootTime 为 Unix 秒，格式化为本地 YYYY-MM-DD HH:mm
function fmtBootTime(sec: number): string {
  if (!sec) return "-";
  const d = new Date(sec * 1000);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

/* ==================== 当前时间 ==================== */
/* 时间展示已改用 common-canvas-clock 组件（盘面自带日期/星期/毫秒信息），此处不再维护定时器 */

/* ==================== 生命周期 ==================== */
const cancels: Array<() => void> = [];
function listen(name: string, cb: (id: string) => void) {
  try {
    const ret = Events.On(name, (ev: any) => cb(String(ev?.data ?? ev ?? "")));
    if (typeof ret === "function") cancels.push(ret as () => void);
  } catch { /* */ }
}

let sysTimer: ReturnType<typeof setInterval> | null = null;

onMounted(async () => {
  await loadTools();
  refreshSysInfo();
  sysTimer = setInterval(refreshSysInfo, 5000);
  listen("ball:action", activateById);
  listen("tool:activate", activateById);
  // 设置页保存工具列表后刷新快捷操作（新增/删除/改名称/改图标等）
  listen("config:saved", () => { void loadTools(); });
});
onUnmounted(() => {
  if (sysTimer) clearInterval(sysTimer);
  cancels.forEach((f) => { try { f(); } catch { /* */ } });
  cancels.length = 0;
});
</script>

<style scoped>
/* 统计卡片行：横纵间距由 a-row 的 gutter 提供，这里只留与下方内容块的间距 */
.stat-cards {
  margin-bottom: var(--base-padding);
}

.stat-card {
  display: flex;
  align-items: center;
  gap: var(--base-padding);
  padding: var(--base-padding);
  background: var(--color-bg-2);
  border-radius: var(--base-radius-default);
  border: 1px solid var(--color-border-1);
  transition: box-shadow 0.2s;
}

.stat-card:hover {
  box-shadow: var(--shadow-2-bottom);
}

.stat-circle {
  width: 44px;
  height: 44px;
  border-radius: var(--base-radius-default);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: var(--base-size-large-x);
  flex-shrink: 0;
}

.stat-circle-cpu {
  background: rgba(var(--arcoblue-6), 0.05);
  color: rgb(var(--arcoblue-6));
}

.stat-circle-mem {
  background: rgba(var(--cyan-6), 0.05);
  color: rgb(var(--cyan-6));
}

.stat-circle-up {
  background: rgba(var(--orange-6), 0.05);
  color: rgb(var(--orange-6));
}

.stat-circle-go {
  background: rgba(var(--purple-6), 0.05);
  color: rgb(var(--purple-6));
}

.stat-circle-disk {
  background: rgba(var(--green-6), 0.05);
  color: rgb(var(--green-6));
}

.stat-circle-net {
  background: rgba(var(--arcoblue-6), 0.05);
  color: rgb(var(--arcoblue-6));
}

.stat-info {
  flex: 1;
  min-width: 0;
}

.stat-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
}

.stat-label {
  font-size: var(--base-size-small);
  color: var(--color-text-3);
}

/* 大号数值：从下方主位移到标题行右侧 */
.stat-big {
  font-weight: bold;
  font-size: var(--base-size-default);
  line-height: 1.2;
  color: var(--color-text-1);
  white-space: nowrap;
}

.stat-unit {
  font-size: var(--base-size-small);
  font-weight: 400;
  color: var(--color-text-3);
  margin-left: var(--base-radius-small);
}

/* 详情小字：从右上角移到下方主位 */
.stat-detail {
  font-size: var(--base-size-small);
  color: var(--color-text-4);
  margin-top: var(--base-margin);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* 服务实例卡片 */
.svc-title {
  font-size: var(--base-size-default);
  font-weight: 600;
}

.svc-cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: var(--base-margin);
  /* 卡片按内容高度排列并顶部对齐；容器随左列铺满剩余高度（不再固定 240px） */
  flex: 1;
  min-height: 0;
  grid-auto-rows: max-content;
  align-content: start;
  overflow-y: auto;
  overflow-x: hidden;
  /* padding-right: 4px; */
  /* 隐藏滚动条但保留滚动能力（Chromium + Firefox） */
  /* scrollbar-width: none; */
}

.svc-cards::-webkit-scrollbar {
  display: none;
}

.svc-card {
  background: var(--color-bg-2);
  border: 1px solid var(--color-border-1);
  border-radius: var(--base-radius-default);
  padding: var(--base-padding);
  cursor: pointer;
}

.svc-card:hover {
  box-shadow: 0 4px 16px rgba(var(--primary-6), 0.08);
}


.svc-card-top {
  display: flex;
  align-items: center;
  gap: var(--base-margin);
  margin-bottom: var(--base-margin);
}

.svc-logo {
  width: 36px;
  height: 36px;
  border-radius: var(--base-radius-default);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: var(--base-size-large);
  flex-shrink: 0;
}

.logo-redis {
  background: rgba(var(--red-6), 0.05);
  color: rgb(var(--red-6));
}

.logo-mysql {
  background: rgba(var(--blue-6), 0.05);
  color: rgb(var(--blue-6));
}

.logo-php {
  background: rgba(var(--purple-6), 0.05);
  color: rgb(var(--purple-6));
}

.logo-nginx {
  background: rgba(var(--green-6), 0.05);
  color: rgb(var(--green-6));
}

.logo-npm {
  background: rgba(var(--red-6), 0.05);
  color: rgb(var(--red-6));
}

.logo-docker {
  background: rgba(var(--arcoblue-6), 0.05);
  color: rgb(var(--arcoblue-6));
}

.svc-meta {
  flex: 1;
  min-width: 0;
}

.svc-name {
  font-weight: 600;
  font-size: var(--base-size-default);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.svc-desc {
  font-size: var(--base-size-small);
  color: var(--color-text-3);
  margin-top: var(--base-radius-small);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.svc-card-bottom {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

/* ---- 右侧卡片 ---- */

/* 当前时间：无卡片裸时钟居中展示，与下方快捷操作卡片间距对齐 12px 栅格 */
.clock-body {
  display: flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 12px;
  height: 232px;
}

/* 行继承主窗口可视高度（100vh − 40px 自绘标题栏 − 内容区/页面内边距），
   使左右两列撞满整屏，右侧“快捷操作”卡片随之铺满到底，不留底部空白 */
.home-row {
  height: 100%;
}

/* 左列：纵向 flex，让“本地服务实例”卡片向下铺满剩余高度 */
.left-col {
  height: 100%;
}

.left-inner {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.left-inner :deep(.arco-col:first-child) {
  flex: 0 0 auto;
}

.svc-col {
  margin-top: 12px;
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
}

.svc-card-fill {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.svc-card-fill :deep(.arco-card-body) {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: auto;
}

/* 空态在铺满的卡片里垂直居中 */
.svc-card-fill :deep(.arco-empty) {
  margin: auto;
}

/* 右侧列：时间卡片固定高度，快捷操作卡片拉伸填满剩余空间 */
.right-col {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.tools-card {
  flex: 1;
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow-y: scroll;
    scrollbar-width: none;
}

.tools-card::-webkit-scrollbar {
  display: none;
}

.tools-card :deep(.arco-card-body) {
  flex: 1;
  display: flex;
  flex-direction: column;
}
.tools-grid {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  gap: var(--base-padding);
  flex: 1;
  align-content: start;
  overflow-y: scroll;
  scrollbar-width: none;
}

.tool-cell {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  /* 正方形单元格随列宽自适应铺满卡片宽度 */
  padding: var(--base-padding);
  border-radius: var(--base-radius-default);
  cursor: pointer;
  border: none;
  background: var(--color-fill-2);
  color: rgb(var(--color-text-1));
}

.tool-cell:hover {
  background: rgba(var(--primary-6), 0.12);
}

.tool-cell:active {
  transform: scale(0.95);
}


.tool-icon {
  font-size: 28px;
  color: inherit;
}
</style>
