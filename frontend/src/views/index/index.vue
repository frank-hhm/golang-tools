<template>
  <layout-body height="full" :isBg="false">
    <a-row :gutter="12" class="home-row">
      <a-col :span="16" class="left-col">
        <!-- ========== 左侧：系统信息 ========== -->

        <a-row :gutter="[12, 12]" class="left-inner">
          <a-col :span="24">
            <a-card>

              <template #title>
                <!-- 欢迎栏 -->
                <span>欢迎回来！</span>
              </template>
              <template #extra>
                <a-button :loading="sysLoading" @click="refreshSysInfo">
                  <template #icon><icon-refresh /></template>
                </a-button>
              </template>

              <!-- 统计卡片行：右上角大号数值 + 下方详情（核/线程、开机时间、占用明细等） -->
              <div class="stat-cards">
                <div class="stat-card">
                  <div class="stat-circle stat-circle-cpu"><span class="icon icon-cpu-1"></span></div>
                  <div class="stat-info">
                    <div class="stat-head">
                      <div class="stat-label">CPU</div>
                      <div class="stat-big">{{ sysInfo ? sysInfo.cpuUsage.toFixed(1) : '--' }}<span
                          class="stat-unit">%</span>
                      </div>
                    </div>
                    <div class="stat-detail">{{ sysInfo ? sysInfo.cpuCores + ' 核 · ' + sysInfo.cpuThreads + ' 线程' : ''
                      }}
                    </div>
                  </div>
                </div>
                <div class="stat-card">
                  <div class="stat-circle stat-circle-mem"><icon-storage size="24"/></div>
                  <div class="stat-info">
                    <div class="stat-head">
                      <div class="stat-label">内存</div>
                      <div class="stat-big">{{ sysInfo ? sysInfo.memPercent.toFixed(1) : '--' }}<span
                          class="stat-unit">%</span>
                      </div>
                    </div>
                    <div class="stat-detail">{{ sysInfo ? fmtBytes(sysInfo.memUsed) + ' / ' + fmtBytes(sysInfo.memTotal)
                      :
                      ''
                    }}
                    </div>
                  </div>
                </div>
                <div class="stat-card">
                  <div class="stat-circle stat-circle-up"><span class="icon icon-yunhangjianshi"></span></div>
                  <div class="stat-info">
                    <div class="stat-head">
                      <div class="stat-label">运行时间</div>
                      <div class="stat-big">{{ sysInfo ? fmtUptime(sysInfo.uptime) : '--' }}</div>
                    </div>
                    <div class="stat-detail">{{ sysInfo ? '开机 ' + fmtBootTime(sysInfo.bootTime) : '' }}</div>
                  </div>
                </div>
                <div class="stat-card">
                  <div class="stat-circle stat-circle-go"><span class="icon icon-golang"></span></div>
                  <div class="stat-info">
                    <div class="stat-head">
                      <div class="stat-label">Go 运行时</div>
                      <div class="stat-big">{{ sysInfo ? sysInfo.goroutines : '--' }}<span class="stat-unit"> 协程</span>
                      </div>
                    </div>
                    <div class="stat-detail">{{ sysInfo ? '堆 ' + fmtBytes(sysInfo.appMemAlloc) : '' }}</div>
                  </div>
                </div>
                <div class="stat-card">
                  <div class="stat-circle stat-circle-disk"><span class="icon icon-yingpan"></span></div>
                  <div class="stat-info">
                    <div class="stat-head">
                      <div class="stat-label">磁盘 C:</div>
                      <div class="stat-big">{{ sysInfo ? sysInfo.diskPercent.toFixed(1) : '--' }}<span
                          class="stat-unit">%</span>
                      </div>
                    </div>
                    <div class="stat-detail">{{ sysInfo ? fmtBytes(sysInfo.diskUsed) + ' / ' +
                      fmtBytes(sysInfo.diskTotal) : '' }}
                    </div>
                  </div>
                </div>
                <div class="stat-card">
                  <div class="stat-circle stat-circle-net"><span class="icon icon-wangluo1"></span></div>
                  <div class="stat-info">
                    <div class="stat-head">
                      <div class="stat-label">网络</div>
                      <div class="stat-big">{{ sysInfo ? fmtBytes(sysInfo.netSent + sysInfo.netRecv) : '--' }}<span
                          class="stat-unit"> 累计</span>
                      </div>
                    </div>
                    <div class="stat-detail">{{ sysInfo ? '↑ ' + fmtBytes(sysInfo.netSent) + '  ↓ ' +
                      fmtBytes(sysInfo.netRecv) : '' }}
                    </div>
                  </div>
                </div>
              </div>
            </a-card>
          </a-col>
          <a-col :span="24" class="svc-col">
            <a-card class="svc-card-fill">
              <!-- 本地服务实例卡片 -->
              <template #title>
                <span class="svc-title">本地服务实例</span>
              </template>
              <template #extra>
                <a-radio-group v-model="svcFilter" type="button">
                  <a-radio value="all">全部</a-radio>
                  <a-radio value="redis">Redis</a-radio>
                  <a-radio value="mysql">MySQL</a-radio>
                  <a-radio value="php">PHP</a-radio>
                  <a-radio value="nginx">Nginx</a-radio>
                  <a-radio value="npm">Npm</a-radio>
                </a-radio-group>
              </template>
              <div class="svc-cards" v-if="filteredServices.length">
                <div v-for="(s, i) in filteredServices" :key="i" class="svc-card" title="点击查看实例详情"
                  @click="openServiceDetail(s)">
                  <div class="svc-card-top">
                    <div class="svc-logo" :class="'logo-' + s.type">
                      <span v-if="s.type === 'redis'" class="icon icon-icon-redis" />
                      <span v-else-if="s.type === 'php'" class="icon icon-php" />
                      <span v-else-if="s.type === 'mysql'" class="icon icon-MySQL" />
                      <span v-else-if="s.type === 'nginx'" class="icon icon-ngnix" />
                      <span v-else-if="s.type === 'npm'" class="icon icon-npm" />
                    </div>
                    <div class="svc-meta">
                      <div class="svc-name" :title="s.name">{{ s.name }}</div>
                      <div class="svc-desc">{{ svcTypeLabel(s.type) }} · v{{ s.version }} · :{{ s.port }}</div>
                    </div>
                  </div>
                  <div class="svc-card-bottom" @click.stop>
                    <a-tag :color="s.running ? 'green' : 'gray'" size="small">
                      <template #icon>
                        <icon-check-circle-fill v-if="s.running" />
                        <icon-exclamation-circle-fill v-else />
                      </template>
                      {{ s.running ? '已启用' : '已停止' }}
                    </a-tag>
                    <a-switch :model-value="s.running" :loading="svcToggling === s.id" size="small"
                      @change="toggleService(s)" />
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
        <a-card class="clock-card">
          <template #title>
            <span>当前时间</span>
          </template>
          <div class="clock-body">
            <div class="clock-circle"><icon-clock-circle /></div>
            <div class="clock-info">
              <div class="clock-time">{{ clockTime }}</div>
              <div class="clock-date">{{ clockDate }}</div>
            </div>
          </div>
        </a-card>

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
const sysLoading = ref(false);
const svcFilter = ref<"all" | "redis" | "mysql" | "php" | "nginx" | "npm">("all");
const svcToggling = ref<string | null>(null);

const filteredServices = computed(() => {
  const list = sysInfo.value?.services || [];
  if (svcFilter.value === "all") return list;
  return list.filter((s: any) => s.type === svcFilter.value);
});

const SVC_TYPE_LABELS: Record<string, string> = {
  redis: "Redis", mysql: "MySQL", php: "PHP", nginx: "Nginx", npm: "Npm",
};
function svcTypeLabel(t: string): string {
  return SVC_TYPE_LABELS[t] || (t ? t.toUpperCase() : "-");
}

/** 服务类型 → 对应管理工具路由 */
const SVC_TYPE_ROUTES: Record<string, string> = {
  redis: "/tools/redis", mysql: "/tools/mysql", php: "/tools/php", nginx: "/tools/nginx", npm: "/tools/npm",
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
const now = ref(new Date());
const WEEK_NAMES = ["周日", "周一", "周二", "周三", "周四", "周五", "周六"];
const pad2 = (n: number) => String(n).padStart(2, "0");

const clockTime = computed(() => {
  const d = now.value;
  return `${pad2(d.getHours())}:${pad2(d.getMinutes())}:${pad2(d.getSeconds())}`;
});
const clockDate = computed(() => {
  const d = now.value;
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())} ${WEEK_NAMES[d.getDay()]}`;
});

/* ==================== 生命周期 ==================== */
const cancels: Array<() => void> = [];
function listen(name: string, cb: (id: string) => void) {
  try {
    const ret = Events.On(name, (ev: any) => cb(String(ev?.data ?? ev ?? "")));
    if (typeof ret === "function") cancels.push(ret as () => void);
  } catch { /* */ }
}

let sysTimer: ReturnType<typeof setInterval> | null = null;
let clockTimer: ReturnType<typeof setInterval> | null = null;

onMounted(async () => {
  await loadTools();
  refreshSysInfo();
  sysTimer = setInterval(refreshSysInfo, 5000);
  clockTimer = setInterval(() => { now.value = new Date(); }, 1000);
  listen("ball:action", activateById);
  listen("tool:activate", activateById);
  // 设置页保存工具列表后刷新快捷操作（新增/删除/改名称/改图标等）
  listen("config:saved", () => { void loadTools(); });
});
onUnmounted(() => {
  if (sysTimer) clearInterval(sysTimer);
  if (clockTimer) clearInterval(clockTimer);
  cancels.forEach((f) => { try { f(); } catch { /* */ } });
  cancels.length = 0;
});
</script>

<style scoped>
/* 统计卡片 - 浅色圆形图标风格 */
.stat-cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: var(--base-padding);
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
  .icon {
    font-size:30px;
  }
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
  padding-right: 4px;
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

/* 当前时间卡片 */
.clock-card {
  margin-bottom: var(--base-margin);
}

.clock-body {
  display: flex;
  align-items: center;
  gap: var(--base-padding);
}

.clock-circle {
  width: 44px;
  height: 44px;
  border-radius: var(--base-radius-default);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: var(--base-size-large-x);
  flex-shrink: 0;
  background: rgba(var(--arcoblue-6), 0.05);
  color: rgb(var(--arcoblue-6));
}

.clock-info {
  flex: 1;
  min-width: 0;
}

.clock-time {
  font-weight: bold;
  font-size: 28px;
  line-height: 1.2;
  color: var(--color-text-1);
  font-variant-numeric: tabular-nums;
  letter-spacing: 1px;
}

.clock-date {
  margin-top: var(--base-radius-small);
  font-size: var(--base-size-small);
  color: var(--color-text-3);
}

/* 行继承主窗口可视高度（100vh − 40px 自绘标题栏 − 内容区/页面内边距），
   使左右两列撞满整屏，右侧“快捷操作”卡片随之铺满到底，不留底部空白 */
.home-row {
  height: calc(100vh - 2 * var(--base-padding));
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
}

.tools-card :deep(.arco-card-body) {
  flex: 1;
  display: flex;
  flex-direction: column;
}

.tools-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: var(--base-padding);
  flex: 1;
  align-content: start;
}

.tool-cell {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  /* 正方形单元格随列宽自适应铺满卡片宽度 */
  aspect-ratio: 1;
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
