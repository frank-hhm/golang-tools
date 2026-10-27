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
                <a-col v-for="c in orderedStatCards" :key="c.key" :span="8"
                  v-bind="statsReo.handlers(c.key)" :class="{ 'reorder-dragging': statsReo.isDragging(c.key), 'reorder-over': statsReo.isOver(c.key) }">
                  <common-stat-card :icon="c.icon" :icon-value="c.p" :icon-size="26" :tone="c.tone"
                    :label="c.label" :value="c.big" :unit="c.unit" :detail="c.detail" />
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
                <div v-for="s in orderedServices" :key="s.id" class="svc-card" @click="openServiceDetail(s)"
                                  @mouseenter="prefetchRoute(SVC_TYPE_ROUTES[s.type])"
                                  v-bind="servicesReo.handlers(s.id)"
                                  :class="{ 'reorder-dragging': servicesReo.isDragging(s.id), 'reorder-over': servicesReo.isOver(s.id) }">
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
            <!-- 更多入口收进卡片右侧插槽：与「欢迎回来」刷新按钮同一套方块图标钮 -->
            <a-tooltip content="全部工具">
              <a-button size="small" @click="moreVisible = true">
                <template #icon><icon-apps /></template>
              </a-button>
            </a-tooltip>
          </template>
          <div class="tools-grid">
            <a-tooltip v-for="(item, index) in orderedTools" :key="toolKeyOf(item, index)"
              :content="item.shortcut ? item.name + ' (' + item.shortcut + ')' : item.name">
              <div class="tool-cell" @click="onToolClick(item)" @mouseenter="prefetchRoute(item.path)"
                @pointerdown="prefetchRoute(item.path)" v-bind="toolsReo.handlers(toolKeyOf(item, index))"
                :class="{ 'reorder-dragging': toolsReo.isDragging(toolKeyOf(item, index)), 'reorder-over': toolsReo.isOver(toolKeyOf(item, index)) }">
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

    <!-- 全部工具：无背景蒙层（mask=false），不阻断后方页面交互 -->
    <a-modal v-model:visible="moreVisible" title="全部工具" :width="600" :mask="false" :mask-closable="false"
      :footer="false" draggable class="tools-more-modal">
      <div class="tools-grid tools-grid-modal">
        <div v-for="item in orderedAllTools" :key="item.id || item.name" class="tool-cell tool-cell-named"
          @click="onMoreToolClick(item)">
          <div class="tool-icon-wrap">
            <i v-if="isIconfontIcon(item.icon)" class="icon tool-icon" :class="item.icon" />
            <component v-else :is="iconComp(item.icon) || 'IconApps'" class="tool-icon" />
          </div>
          <span class="tool-name">{{ item.name }}</span>
        </div>
      </div>
    </a-modal>

  </layout-body>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onMounted, onUnmounted, reactive, ref } from "vue";
import { Events } from "@wailsio/runtime";
import { IconCheckCircleFill, IconExclamationCircleFill } from "@arco-design/web-vue/es/icon";
import { useToolEvents, type ToolItem } from "./events";
import { useToolWindow } from "./useToolWindow";
import { arcoIconComp as iconComp, isIconfontIcon, normalizeToolIcon } from "@/utils/tool-icon";
import { isToolAvailable } from "@/utils/caps";
import { prefetchRoute } from "@/router/prefetch";
import { useReorder, applyOrder, loadOrder } from "@/utils/reorder";

interface ToolRow extends ToolItem {
  id?: string;
  key?: string;
  icon?: string;
  shortcut?: string;
  home?: boolean;
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
    const next = list
      // 内嵌浏览器（WebView2）与截图（Win32 抓屏）在非桌面构建里没有对应绑定，直接不显示
      .filter((t) => isToolAvailable(t.path, t.event))
      .map((t) => ({
        id: t.id, key: t.key, name: t.name, path: t.path || undefined,
        event: t.event || undefined, single: !!t.single, fullscreen: !!t.fullscreen,
        disableFullscreen: !!t.disableFullscreen,
        width: t.width || undefined, height: t.height || undefined, icon: normalizeToolIcon(t.icon), shortcut: t.shortcut,
        home: t.home !== false,
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
  key: string;
  tone: string;
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
      tone: "blue", icon: "cpu", label: "CPU", key: "cpu",
      big: s ? s.cpuUsage.toFixed(1) : "--", unit: "%",
      detail: s ? s.cpuCores + " 核 · " + s.cpuThreads + " 线程" : "",
      p: s?.cpuUsage,
    },
    {
      tone: "cyan", icon: "mem", label: "内存", key: "mem",
      big: s ? s.memPercent.toFixed(1) : "--", unit: "%",
      detail: s ? fmtBytes(s.memUsed) + " / " + fmtBytes(s.memTotal) : "",
      p: s?.memPercent,
    },
    {
      tone: "orange", icon: "uptime", label: "运行时间", key: "uptime",
      big: s ? fmtUptime(s.uptime) : "--",
      detail: s ? "开机 " + fmtBootTime(s.bootTime) : "",
    },
    {
      tone: "purple", icon: "go", label: "Go 运行时", key: "go",
      big: s ? String(s.goroutines) : "--", unit: " 协程",
      detail: s ? "堆 " + fmtBytes(s.appMemAlloc) : "",
    },
    {
      tone: "green", icon: "disk", label: "磁盘 C:", key: "disk",
      big: s ? s.diskPercent.toFixed(1) : "--", unit: "%",
      detail: s ? fmtBytes(s.diskUsed) + " / " + fmtBytes(s.diskTotal) : "",
      p: s?.diskPercent,
    },
    {
      tone: "blue", icon: "net", label: "网络", key: "net",
      big: s ? fmtBytes(s.netSent + s.netRecv) : "--", unit: " 累计",
      detail: s ? "↑ " + fmtBytes(s.netSent) + "  ↓ " + fmtBytes(s.netRecv) : "",
    },
  ];
});
/** 只在用户主动点刷新（与首次进入）时亮：后台轮询不碰它，否则按钮每 5 秒转一下 */
const sysLoading = ref(false);
const svcFilter = ref<"all" | "redis" | "mysql" | "php" | "nginx" | "npm" | "docker">("all");
const svcToggling = ref<string | null>(null);

const filteredServices = computed(() => {
  const list = sysInfo.value?.services || [];
  if (svcFilter.value === "all") return list;
  return list.filter((s: any) => s.type === svcFilter.value);
});

/* ==================== 拖拽排序（主页三区，存入用户配置随导出搬运） ==================== */
const statsOrder = ref<string[]>([]);
const servicesOrder = ref<string[]>([]);

/** 把可见列表按新顺序重排，返回重排后的 id（供持久化写回） */
function reorderIds(items: any[], newVis: string[], idOf: (x: any, i: number) => string): string[] {
  const idx = new Map(newVis.map((id, i) => [id, i] as const));
  // 先把原始索引固定下来：兜底键（如工具无 id 时用 index）不随排序过程漂移
  return items
    .map((x, i) => ({ k: idOf(x, i), r: idx.get(idOf(x, i)) ?? 1e9 }))
    .sort((a, b) => a.r - b.r)
    .map((x) => x.k);
}

const statsReo = useReorder("home:stats", {
  getVisible: () => statCards.value.map((c) => c.key),
  onReordered: (nv) => { statsOrder.value = reorderIds(statCards.value, nv, (c) => c.key); },
});
const servicesReo = useReorder("home:services", {
  getVisible: () => filteredServices.value.map((s: any) => String(s.id)),
  getFull: () => (sysInfo.value?.services || []).map((s: any) => String(s.id)),
  onReordered: (nv) => { servicesOrder.value = reorderIds(filteredServices.value, nv, (s) => String(s.id)); },
});
/** 主页“快捷操作”网格只展示勾选了主页展示的工具；“更多”弹窗展示全部 */
const homeTools = computed(() => toolsList.value.filter((t) => t.home !== false));
/** 工具项的稳定标识：与 Go 侧 SetToolOrder 的匹配规则一致（id 优先，其次 key/path） */
function toolKeyOf(t: ToolRow, i: number | string): string {
  return String(t.id || t.key || t.path || i);
}
// 快捷操作的顺序不另存一份：直接回写配置里 tools 数组的顺序，
// 与设置页「工具」表同一个真源，一边拖动另一边跟着变
const toolsReo = useReorder("home:tools", {
  getVisible: () => homeTools.value.map(toolKeyOf),
  getFull: () => toolsList.value.map(toolKeyOf),
  save: (ids) => GoTools?.SetToolOrder(ids),
  onReordered: (_nv, full) => { toolsList.value = applyOrder(toolsList.value, full, toolKeyOf); },
});

const orderedStatCards = computed(() => applyOrder(statCards.value, statsOrder.value, (c) => c.key));
const orderedServices = computed(() => applyOrder(filteredServices.value, servicesOrder.value, (s: any) => String(s.id)));
// 工具区不再走 Order 表：配置里 tools 数组的顺序就是显示顺序
const orderedTools = homeTools;
const orderedAllTools = toolsList;

/** “更多”弹窗（无蒙层）：展示全部工具，点一个即打开并收起 */
const moreVisible = ref(false);
function onMoreToolClick(item: ToolRow) {
  moreVisible.value = false;
  onToolClick(item);
}

async function loadHomeOrders() {
  const [st, sv] = await Promise.all([loadOrder("home:stats"), loadOrder("home:services")]);
  statsOrder.value = st; servicesOrder.value = sv;
}

/** 旧版把快捷操作顺序单独存在 order["home:tools"]，与设置页各排一份：并进配置的工具顺序后清掉该键 */
async function migrateToolsOrder() {
  const legacy = await loadOrder("home:tools");
  if (!legacy.length) return;
  try {
    await GoTools?.SetToolOrder(legacy);
    await GoTools?.SetListOrder("home:tools", []);
    await loadTools();
  } catch { /* 迁移失败不影响使用，下次进入再试 */ }
}

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

// GetSystemInfo 要逐个问服务状态，慢起来可以超过 5s 轮询间隔，不允许上一轮还没回来就叠加下一轮
let sysInflight = false;

/**
 * 拉一次系统信息。
 * @param silent 后台轮询走静默路径：不动刷新按钮的 loading，也不在失败时抹掉已有数据
 */
async function loadSysInfo(silent = false) {
  if (sysInflight) return;
  sysInflight = true;
  if (!silent) sysLoading.value = true;
  try {
    sysInfo.value = await GoTools?.GetSystemInfo();
  } catch { /* 取不到就继续用上一次的数据，下一轮再试 */ }
  finally {
    sysInflight = false;
    sysLoading.value = false;
  }
}

/** 刷新按钮：用户主动动作，亮 loading 作为反馈 */
function refreshSysInfo() { void loadSysInfo(false); }

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
    await loadSysInfo(true);
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

/** 后台轮询：静默路径（不闪刷新按钮）；窗口隐藏/收进托盘时干脆不查，不白耗后端 */
function pollSysInfo() {
  if (!document.hidden) void loadSysInfo(true);
}

onMounted(async () => {
  await loadTools();
  void loadHomeOrders();
  void migrateToolsOrder();
  refreshSysInfo();
  sysTimer = setInterval(pollSysInfo, 5000);
  // 从隐藏态恢复时补一次，不让卡片停在隐藏前的数据
  document.addEventListener("visibilitychange", pollSysInfo);
  listen("ball:action", activateById);
  listen("tool:activate", activateById);
  // 设置页保存工具列表后刷新快捷操作（新增/删除/改名称/改图标等）
  listen("config:saved", () => { void loadTools(); });
});
onUnmounted(() => {
  if (sysTimer) clearInterval(sysTimer);
  document.removeEventListener("visibilitychange", pollSysInfo);
  cancels.forEach((f) => { try { f(); } catch { /* */ } });
  cancels.length = 0;
});
</script>

<style scoped>
/* 统计卡片行：横纵间距由 a-row 的 gutter 提供，这里只留与下方内容块的间距 */
.stat-cards {
  margin-bottom: var(--base-padding);
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

/* 拖拽排序反馈样式（.reorder-dragging/.reorder-over/[draggable] 光标）已下沉到全局 styles/index.scss，主页与各服务列表共用 */


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

/* a-row(.arco-row) 的 flex-flow 简写带 wrap：纵向排布下 wrap 会把第二块
   “本地服务实例”换到右侧新列，与右列时钟/快捷操作重叠。用更高优先级选择器强制不换行。 */
.home-row .left-inner {
  flex-wrap: nowrap;
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

/* “更多”弹窗内的网格：四列、不滚动（交给弹窗自身），格子带名称 */
.tools-grid-modal {
  grid-template-columns: repeat(4, 1fr);
  overflow-y: visible;
  flex: none;
}

.tool-cell-named {
  gap: 6px;
  padding: 12px 8px;
}

.tool-cell-named .tool-icon {
  font-size: 24px;
}

.tool-name {
  font-size: var(--base-size-small);
  color: var(--color-text-1);
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-align: center;
}
</style>
