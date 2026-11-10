<template>
  <layout-body height="full" :isBg="false">
    <div class="home-layout">
    <!-- 行1：内容自适应高度（欢迎栏 + 时钟） -->
    <a-row :gutter="[12,12]" class="home-row home-row-top">
      <!-- ===== 行1：欢迎栏（左）+ 指针时钟（右，上下左右居中） ===== -->
      <a-col :span="16" class="welcome-col">
        <a-card class="welcome-card">
          <template #title>
            <!-- 欢迎栏 -->
            <span>欢迎回来！</span>
          </template>
          <template #extra>
            <a-space :size="8">
              <!-- 测试入口：只在 dev 跑的前端服务里显示，打包产物隐藏入口但页面与路由保留（叠加不删） -->
              <a-tooltip v-if="showTestHub" content="组件测试（仅开发环境）">
                <a-button size="small" @click="openTestHub">
                  <template #icon><icon-experiment /></template>
                </a-button>
              </a-tooltip>
              <a-button size="small" :loading="sysLoading" @click="refreshSysInfo">
                <template #icon><icon-refresh /></template>
              </a-button>
            </a-space>
          </template>

          <!-- 统计卡片行：右上角大号数值 + 下方详情（核/线程、开机时间、占用明细等）；
               栅格用 a-row/a-col，横纵间距由 gutter 提供；顺序固定，不做拖拽排序；
               带 link 的卡片可点，跳到对应的监控面板（都不进工具列表） -->
          <a-row :gutter="[12, 12]" class="stat-cards">
            <a-col v-for="c in statCards" :key="c.key" :span="8" :class="{ 'stat-clickable': !!c.link }"
              :title="c.link ? '点击查看' + c.link : undefined" @click="onStatCardClick(c.key)">
              <m-stat-card :icon="c.icon" :icon-value="c.p" :icon-size="26" :tone="c.tone" :label="c.label"
                :value="c.big" :unit="c.unit" :detail="c.detail" />
            </a-col>
          </a-row>
        </a-card>
      </a-col>

      <!-- 指针时钟：行1 右侧独立格，时钟上下左右居中 -->
      <a-col :span="8" class="clock-col">
        <div class="clock-body">
          <!-- Canvas 指针时钟：盘面已含日期/星期/年份/毫秒时间跑动信息 -->
          <common-canvas-clock :size="180" />
        </div>
      </a-col>
    </a-row>

    <!-- 行2：铺满剩余高度到底（本地服务实例 + 快捷操作） -->
    <a-row :gutter="[12,12]" class="home-row home-row-bottom">
      <!-- ===== 行2：本地服务实例（左）+ 快捷操作（右） ===== -->
      <a-col :span="16" class="svc-col">
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
          <!-- 本地服务实例卡片：顺序跟随后端返回的实例列表，不做拖拽排序；
               卡片外壳已封成公共组件 m-svc-card（logo/名称/描述/状态行/操作区），这里只填内容 -->
          <div class="svc-cards" v-if="filteredServices.length">
            <m-svc-card v-for="s in filteredServices" :key="s.id" :name="s.name" :desc="svcDesc(s)"
              :tone="svcTone(s.type)" :running="s.running" @click="openServiceDetail(s)"
              @mouseenter="prefetchRoute(SVC_TYPE_ROUTES[s.type])">
              <template #logo>
                <span v-if="s.type === 'redis'" class="icon icon-icon-redis" />
                <span v-else-if="s.type === 'php'" class="icon icon-php" />
                <span v-else-if="s.type === 'mysql'" class="icon icon-MySQL" />
                <span v-else-if="s.type === 'nginx'" class="icon icon-ngnix" />
                <span v-else-if="s.type === 'npm'" class="icon icon-npm" />
                <icon-computer v-else-if="s.type === 'docker'" />
              </template>
              <template #extra>
                <a-switch v-if="s.type !== 'docker'" :model-value="s.running" :loading="svcToggling === s.id"
                  size="small" type="round" @change="toggleService(s)" />
                <a-tag v-else size="small" color="arcoblue" @click="openServiceDetail(s)">面板管理</a-tag>
              </template>
            </m-svc-card>
          </div>
          <a-empty v-else description="暂无服务实例" />
        </a-card>
      </a-col>

      <a-col :span="8" class="tools-col">
        <a-card class="tools-card">
          <template #title>
            <span>快捷操作</span>
          </template>
          <template #extra>
            <!-- 更多入口收进卡片右侧插槽：与「欢迎回来」刷新按钮同一套方块图标钮 -->
            <a-tooltip content="全部工具">
              <a-button size="small" @click="openMore">
                <template #icon><icon-apps /></template>
              </a-button>
            </a-tooltip>
          </template>
          <div class="tools-grid">
            <a-tooltip v-for="(item, index) in orderedTools" :key="toolKeyOf(item, index)"
              :content="item.shortcut ? item.name + ' (' + item.shortcut + ')' : item.name">
              <m-tool-cell :icon="item.icon" @click="onToolClick(item)" @mouseenter="prefetchRoute(item.path)"
                @pointerdown="prefetchRoute(item.path)" v-bind="toolsReo.handlers(toolKeyOf(item, index))"
                :class="{ 'reorder-dragging': toolsReo.isDragging(toolKeyOf(item, index)), 'reorder-over': toolsReo.isOver(toolKeyOf(item, index)) }" />
            </a-tooltip>
          </div>
        </a-card>
      </a-col>
    </a-row>
    </div>

    <!-- 全部工具：带半透明蒙层压住首页内容，焦点回到弹窗本身；点蒙层即收起；标题右侧带搜索框实时过滤网格。
         弹窗本体背景交回 Arco/主题默认（bg-3 基色或纹理/玻璃），比格子的 bg-2 略亮，格子才能从面板上拉开 -->
    <a-modal v-model:visible="moreVisible" :width="600" :footer="false" draggable :mask="true">
      <template #title>
        <div class="more-head">
          <span class="more-title">全部工具</span>
          <!-- 标题栏整体就是拖拽把手，Arco 对它的 mousedown 无条件 preventDefault，而 preventDefault
               在 Chromium 里连带把聚焦一起吞掉——搜索框落在标题插槽里就点不动（用户反馈的「不可用」）。
               这里 .stop 住 mousedown 不让它冒到 header：输入框正常聚焦输入，标题文字那边照常能拖窗口。 -->
          <div class="more-search-wrap" @mousedown.stop>
            <a-input ref="moreSearchRef" v-model="moreQuery" size="small" placeholder="搜索工具（名称 / git / term…）"
              allow-clear class="more-search" @keyup.enter="openFirstMatch" />
            <span v-if="moreQuery.trim()" class="more-count">{{ filteredAllTools.length }}</span>
          </div>
        </div>
      </template>
      <!-- 固定高度面板：命中 34 个还是 3 个都占同一块高度，边打字弹窗不再上下跳；超出走内部滚动 -->
      <div class="more-panel">
        <div class="tools-grid tools-grid-modal" v-if="filteredAllTools.length">
          <m-tool-cell v-for="item in filteredAllTools" :key="item.id || item.name" :icon="item.icon"
            :name="item.name" @click="onMoreToolClick(item)" />
        </div>
        <a-empty v-else :description="moreQuery.trim() ? '没有匹配的工具：' + moreQuery.trim() : '暂无工具'" />
      </div>
    </a-modal>

  </layout-body>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, nextTick, onMounted, onUnmounted, reactive, ref } from "vue";
import { Events } from "@wailsio/runtime";
import { IconCheckCircleFill, IconExclamationCircleFill } from "@arco-design/web-vue/es/icon";
import { useToolEvents, type ToolItem } from "./events";
import { useToolWindow } from "./useToolWindow";
import { normalizeToolIcon } from "@/utils/tool-icon";
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

/** 进程管理面板：不进工具项/设置页，首页 CPU 与内存卡片都唤起它（同一窗口，单例） */
function openProcManager() {
  openToolWindow("/tools/procman", "进程管理", { single: true, width: 1180, height: 780 });
}
/** 端口管理面板：同样不进工具项，首页「网络」卡片唤起 */
function openPortManager() {
  openToolWindow("/tools/portman", "端口管理", { single: true, width: 1180, height: 760 });
}
/** 磁盘面板是既有工具项：优先走它自己的配置（尺寸/单例跟着用户设置走），配置里被删了才兜底 */
function openDiskPanel() {
  const item = toolsList.value.find((t) => t.id === "disk" || t.key === "disk");
  if (item) { onToolClick(item); return; }
  openToolWindow("/tools/disk", "磁盘管理", { single: true, width: 1360, height: 860 });
}
function onStatCardClick(key: string) {
  if (key === "cpu" || key === "mem") openProcManager();
  else if (key === "disk") openDiskPanel();
  else if (key === "net") openPortManager();
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
  /** 点击卡片唤起的面板名；留空表示这张卡不可点 */
  link?: string;
}
const statCards = computed<StatCard[]>(() => {
  const s = sysInfo.value;
  return [
    {
      tone: "blue", icon: "cpu", label: "CPU", key: "cpu", link: "进程管理",
      big: s ? s.cpuUsage.toFixed(1) : "--", unit: "%",
      detail: s ? s.cpuCores + " 核 · " + s.cpuThreads + " 线程" : "",
      p: s?.cpuUsage,
    },
    {
      tone: "cyan", icon: "mem", label: "内存", key: "mem", link: "进程管理",
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
      tone: "green", icon: "disk", label: "磁盘 C:", key: "disk", link: "磁盘面板",
      big: s ? s.diskPercent.toFixed(1) : "--", unit: "%",
      detail: s ? fmtBytes(s.diskUsed) + " / " + fmtBytes(s.diskTotal) : "",
      p: s?.diskPercent,
    },
    {
      tone: "blue", icon: "net", label: "网络", key: "net", link: "端口管理",
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

/* ==================== 拖拽排序（仅主页快捷操作，顺序回写配置 tools 数组） ==================== */
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

// 工具区不走 Order 表：配置里 tools 数组的顺序就是显示顺序
const orderedTools = homeTools;
const orderedAllTools = toolsList;

/** “更多”弹窗（带蒙层）：展示全部工具，点一个即打开并收起；标题旁搜索框实时检索 */
const moreVisible = ref(false);
const moreQuery = ref("");
const moreSearchRef = ref<{ focus?: () => void } | null>(null);

/** 打开时清掉上次的关键词并直接聚焦输入框：点「全部工具」就是要搜，省一次点击 */
function openMore() {
  moreQuery.value = "";
  moreVisible.value = true;
  void nextTick(() => moreSearchRef.value?.focus?.());
}

/**
 * 检索口径：除显示名以外还比 id / key / path，所以敲 git、term、ftp、mysql
 * 这类英文（用户实际就这么搜）也能命中对应工具，而不只是中文名称。
 */
function matchTool(t: ToolRow, q: string): boolean {
  return [t.name, t.id, t.key, t.path].some((f) => String(f || "").toLowerCase().includes(q));
}

const filteredAllTools = computed(() => {
  const q = moreQuery.value.trim().toLowerCase();
  if (!q) return orderedAllTools.value;
  return orderedAllTools.value.filter((t) => matchTool(t, q));
});

/** 回车直达首个匹配项：键盘就能把工具开出来，不用回到鼠标 */
function openFirstMatch() {
  const first = filteredAllTools.value[0];
  if (first) onMoreToolClick(first);
}

/**
 * 测试入口只在开发环境亮：MODE 判定而非只看 DEV，因为 `npm run build:dev` 是
 * `vite build --mode development`（DEV 也为 true），那样打包出来入口就关不掉了。
 * 隐藏的是入口，/demo/test 与 /win/demo/test 路由照旧打进产物，需要时手输地址能开。
 */
const showTestHub = import.meta.env.DEV && import.meta.env.MODE === "development";

/** 组件测试台：走统一工具窗入口，桌面开原生小窗（single），Web 模式回退页面内路由 */
function openTestHub() {
  openToolWindow("/demo/test", "组件测试", { single: true, width: 1180, height: 820 });
}

function onMoreToolClick(item: ToolRow) {
  moreVisible.value = false;
  onToolClick(item);
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

/** 服务类型 → m-svc-card 的 logo 底色语义（沿用旧 logo-* 配色） */
const SVC_TYPE_TONES: Record<string, string> = {
  redis: "red", mysql: "blue", php: "purple", nginx: "green", npm: "red", docker: "arcoblue",
};
function svcTone(t: string): string {
  return SVC_TYPE_TONES[t] || "gray";
}

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
}

/* 首页统计卡(m-stat-card)与服务实例卡(m-svc-card)嵌在 welcome-card / svc-card-fill 两个 a-card 里，
   组件默认自铺 --color-bg-2 实底，与父卡叠成「卡中卡」双层底、显脏。这里只归零这层背景、透出父卡，
   描边与 hover 投影照常保留（可点卡片的交互反馈不丢，比 inherit-bg 更保守——后者会连投影一起抹掉）。
   :deep 编译成 .stat-cards[data-v] .m-stat-card (0,3,0)，稳过组件 scoped .m-stat-card[data-v] (0,2,0)。 */
.stat-cards :deep(.m-stat-card) {
  background: transparent;
}

.svc-cards :deep(.m-svc-card) {
  background: transparent;
}

/* 可点击唤起面板的统计卡片（CPU / 内存 → 进程管理，磁盘 → 磁盘面板，网络 → 端口管理） */
.stat-clickable {
  cursor: pointer;
}

/* 服务实例卡片标题：字号不单独指定，继承全局 .arco-card-header-title 的 --base-size-large-x（16px），
   与「欢迎回来！」「快捷操作」等卡片标题同大小、同随「字号×密度」缩放；这里只保留加粗。
   （旧写法 font-size: var(--base-size-default) 在运行时 standard 档 = 12px，比其它标题小一截 → 已删） */
.svc-title {
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

/* 单卡外壳样式（铺底/描边/logo 底色/名称描述/状态行）已封进公共组件 m-svc-card，这里只剩网格布局 */

/* 拖拽排序反馈样式（.reorder-dragging/.reorder-over/[draggable] 光标）已下沉到全局 styles/index.scss，主页与各服务列表共用 */

/* ---- 右侧卡片 ---- */

/* 指针时钟：填满 clock-col 并上下左右居中（clock-col 与左侧欢迎卡同高，align-items 拉伸） */
.clock-body {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  height: 100%;
}

/* 主页整体：纵向 flex 两行——行1 内容自适应高度，行2 铺满剩余高度到底。
   （原单个 m-row 折行 + height:100% 只能靠 align-content 均分两行高度，无法表达 auto+fill 分配，故拆成两个 m-row） */
.home-layout {
  display: flex;
  flex-direction: column;
  height: 100%;
  gap: 12px;
}

/* 行1：欢迎栏 + 时钟，高度由内容（欢迎卡）决定；align-items 默认 stretch 使时钟在欢迎栏高度内上下居中 */
.home-row-top {
  flex: none;
}

/* 行2：本地服务实例 + 快捷操作，flex:1 铺满剩余高度到底；min-height:0 让内部卡片可滚动不溢出 */
.home-row-bottom {
  flex: 1;
  min-height: 0;
}

/* 行1 左：欢迎栏（含统计卡片），高度由内容决定 */
.welcome-col {
  display: flex;
}

.welcome-card {
  width: 100%;
}

/* 行1 右：时钟格，撑满行高供 clock-body 居中 */
.clock-col {
  display: flex;
}

/* 行2 左：本地服务实例，铺满行高（align-self:stretch 覆盖 m-col 默认 flex-basis:0 行为） */
.svc-col {
  display: flex;
  align-self: stretch;
  min-height: 0;
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

/* 行2 右：快捷操作格，撑满行高供 tools-card 铺满 */
.tools-col {
  display: flex;
  align-self: stretch;
  min-height: 0;
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

/* 工具格本体（图标/背景/hover/名称排版）已封成 m-tool-cell 组件，这里只留网格布局 */

/* 「全部工具」面板：定高≈ 5 行格子，命中数多少只影响内部滚动，不影响弹窗尺寸 */
.more-panel {
  height: 420px;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* 弹窗本体背景不在此处：交给 Arco/主题默认（bg-3 基色或纹理/玻璃），与格子的 bg-2 拉开对比 */

/* 空态在定高面板里垂直居中，而不是顶在左上角 */
.more-panel :deep(.arco-empty) {
  margin: auto;
}

/* “更多”弹窗内的网格：五列、在定高面板内滚动，格子带名称 */
.tools-grid-modal {
  grid-template-columns: repeat(5, 1fr);
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  scrollbar-width: none;
}

.tools-grid-modal::-webkit-scrollbar {
  display: none;
}

/* 弹窗标题行：标题 + 搜索框横向排布（搜索框落在用户标记的标题右侧位置） */
.more-head {
  display: flex;
  align-items: center;
  gap: var(--base-padding);
  width: 100%;
  min-width: 0;
}

.more-title {
  flex: none;
}

/* 搜索框包一层：给 mousedown.stop 当挂点，并把输入框与命中数排成一行 */
.more-search-wrap {
  flex: 1 1 auto;
  min-width: 0;
  max-width: 300px;
  display: flex;
  align-items: center;
  gap: 6px;
}

.more-search {
  flex: 1 1 auto;
  min-width: 0;
  /*  arco 标题是加粗字重，不压回来搜索文字会跟着变粗 */
  font-weight: normal;
}

/* 命中数：排在输入框后面（不用绝对定位，否则跟 allow-clear 的清除图标撞位） */
.more-count {
  flex: none;
  font-size: var(--base-size-small);
  font-weight: normal;
  color: rgb(var(--gray-5));
}
</style>
