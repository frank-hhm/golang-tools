<template>
  <div class="lk-wrap" @click.self="dismiss">
    <div class="lk-card">
      <!-- 输入行：一个搜索字形 + 输入框 + 索引状态点 -->
      <div class="lk-input-row">
        <span class="lk-ic">
          <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
            <circle cx="10.5" cy="10.5" r="6.5" fill="none" stroke="currentColor" stroke-width="2" />
            <path d="M15.5 15.5L21 21" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
          </svg>
        </span>
        <input
          ref="inputRef"
          v-model="query"
          class="lk-input"
          type="text"
          spellcheck="false"
          placeholder="搜索应用、工具、文件、窗口，或输入算式 / > 命令"
          @keydown="onKeydown"
        />
        <span v-if="!indexReady && query" class="lk-tip-dot" :title="`索引建立中（已 ${indexCount} 条）`" />
      </div>

      <!-- 结果列表 -->
      <div v-if="items.length" class="lk-list">
        <div
          v-for="(it, i) in items"
          :key="it.type + '|' + it.id"
          class="lk-item"
          :class="{ active: i === active, danger: it.danger }"
          @mouseenter="active = i"
          @click="run(i)"
        >
          <span class="lk-item-ic">
            <img v-if="it.icon" :src="it.icon" alt="" />
            <b v-else>{{ typeGlyph(it.type) }}</b>
          </span>
          <span class="lk-item-main">
            <span class="lk-item-title">{{ it.title }}</span>
            <span class="lk-item-sub">{{ it.sub }}</span>
          </span>
          <span class="lk-item-tag">{{ typeLabel(it.type) }}</span>
          <span v-if="it.danger && armed === i" class="lk-item-confirm">再按一次回车确认</span>
        </div>
      </div>

      <!-- 空态：没输入时给用法提示，输入了没结果时如实说没有 -->
      <div v-else class="lk-empty">
        <template v-if="!query">
          <div class="lk-empty-title">一条输入，同时找应用、工具、窗口和文件</div>
          <ul class="lk-hints">
            <li><b>chrome</b> 启动已安装的应用</li>
            <li><b>redis</b> 打开本程序的 Redis 管理</li>
            <li><b>3+4*2</b> 直接算，回车即复制结果</li>
            <li><b>&gt;锁屏</b> 只用 &gt; 前缀搜内置命令</li>
            <li><b>example.com</b> 用默认浏览器打开网址</li>
          </ul>
        </template>
        <template v-else>
          <div class="lk-empty-title">没有匹配「{{ rawQuery }}」的结果</div>
          <div class="lk-empty-sub">
            {{ busy ? "正在检索…" : "换个词试试；文件只覆盖设置里纳入的目录" }}
          </div>
        </template>
      </div>

      <div class="lk-foot">
        <span>↑↓ 选择</span><span>Enter 执行</span><span>Alt+Enter 打开位置</span><span>Esc 关闭</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * 快捷搜索面板（Alt+Space）。
 *
 * 这是一个无边框、透明背景、置顶的小窗：Go 侧只负责开窗与点亮（services/app/launcher.go），
 * 所有检索都走绑定方法 LauncherSearch，本页面自己不含任何匹配逻辑——打分与排序的口径
 * 必须只有一个来源，否则同一个词在两台机器上排序不同就没法解释。
 *
 * 窗口是复用的：点亮时 Go 发 "launcher:show"，这里清空上次输入并重新聚焦；
 * 索引建完时 Go 广播 "launcher:index"，这里对当前输入重查一次，免得结果停在半份索引上。
 */
import { getCurrentInstance, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { Events } from "@wailsio/runtime";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const inputRef = ref<HTMLInputElement | null>(null);
const query = ref("");
const rawQuery = ref("");
const items = ref<any[]>([]);
const active = ref(0);
const armed = ref(-1); // 已"预备"的危险条目：再按一次回车才真执行
const busy = ref(false);
const indexReady = ref(true);
const indexCount = ref(0);

const TYPE_LABEL: Record<string, string> = {
  app: "应用", tool: "工具", window: "窗口", file: "文件",
  calc: "计算", command: "命令", url: "网址",
};
const TYPE_GLYPH: Record<string, string> = {
  app: "应", tool: "工", window: "窗", file: "文",
  calc: "=", command: "❯", url: "↗",
};
const typeLabel = (t: string) => TYPE_LABEL[t] ?? "";
const typeGlyph = (t: string) => TYPE_GLYPH[t] ?? "·";

let offShow: (() => void) | undefined;
let offIndex: (() => void) | undefined;

// 每次检索都自增一代：慢机器上后一次输入的结果可能比前一次先到，
// 靠序号丢掉过期回包，否则列表会跳回用户早已删掉的关键词上。
let seq = 0;

async function search() {
  const q = query.value;
  rawQuery.value = q;
  const gen = ++seq;
  armed.value = -1;
  active.value = 0;
  if (!q.trim()) {
    items.value = [];
    indexReady.value = true;
    return;
  }
  busy.value = true;
  try {
    const res: any = await GoTools?.LauncherSearch(q);
    if (gen !== seq) return;
    items.value = Array.isArray(res?.items) ? res.items : [];
    indexReady.value = !!res?.indexReady;
    indexCount.value = Number(res?.indexCount ?? 0);
  } catch {
    if (gen === seq) items.value = [];
  } finally {
    if (gen === seq) busy.value = false;
  }
}

// 输入即时出结果，但仍要一小段防抖：每敲一个字就扫一遍索引 + 枚举窗口，
// 在机械盘上足以让输入本身变得发涩。
let timer = 0;
watch(query, () => {
  if (timer) clearTimeout(timer);
  timer = window.setTimeout(() => void search(), 120);
});

function onKeydown(e: KeyboardEvent) {
  const n = items.value.length;
  switch (e.key) {
    case "Escape":
      e.preventDefault();
      dismiss();
      return;
    case "ArrowDown":
      e.preventDefault();
      if (n) active.value = (active.value + 1) % n;
      scrollActiveIntoView();
      return;
    case "ArrowUp":
      e.preventDefault();
      if (n) active.value = (active.value - 1 + n) % n;
      scrollActiveIntoView();
      return;
    case "Enter":
      e.preventDefault();
      if (e.altKey) {
        openLocation(active.value);
        return;
      }
      run(active.value);
      return;
    default:
      return;
  }
}

function cur(i: number) {
  return i >= 0 && i < items.value.length ? items.value[i] : null;
}

async function run(i: number) {
  const it = cur(i);
  if (!it) return;
  // 危险命令（退出程序）要两次回车：一次输入失误就把整个程序连同后台服务一起关掉，
  // 代价太大，宁可让用户多点一下。
  if (it.danger && armed.value !== i) {
    armed.value = i;
    return;
  }
  if (it.type === "calc") {
    await copyCalc(it.id);
  }
  try {
    await GoTools?.LauncherExecute(it.type, it.id);
  } catch (err: any) {
    // 执行失败绝不能静默：面板已经收起来的话，用户只会以为"点了没反应"
    showFail(err?.message ?? String(err));
    return;
  }
  dismiss();
}

async function openLocation(i: number) {
  const it = cur(i);
  if (!it) return;
  try {
    await GoTools?.LauncherOpenLocation(it.type, it.id, it.sub ?? "");
  } catch (err: any) {
    showFail(err?.message ?? String(err));
    return;
  }
  dismiss();
}

// 算式结果的复制由浏览器完成：本仓库没有 Go 侧文本剪贴板 helper，
// 为这一条再引一个依赖不值；面板本来就是前台窗口，clipboard API 的授权也拿得到。
async function copyCalc(text: string) {
  try {
    await navigator.clipboard.writeText(String(text));
  } catch {
    /* 剪贴板被系统拒绝时不拦执行：Go 侧照样记一次使用频次 */
  }
}

function showFail(msg: string) {
  try {
    GoTools?.ShowMessage("warning", msg);
  } catch {
    console.warn("launcher execute failed:", msg);
  }
}

function dismiss() {
  try {
    GoTools?.LauncherHide();
  } catch {
    /* 没有原生窗口能力时（浏览器里直接打开这条路由）收起就是收起不了，不管 */
  }
}

function scrollActiveIntoView() {
  nextTick(() => {
    const el = document.querySelector(".lk-item.active");
    el?.scrollIntoView({ block: "nearest" });
  });
}

onMounted(() => {
  inputRef.value?.focus();
  offShow = Events.On("launcher:show", () => {
    // 复用窗口时页面不重新 mount，靠这个事件把上一轮的输入和选中态抹干净
    query.value = "";
    items.value = [];
    armed.value = -1;
    active.value = 0;
    nextTick(() => inputRef.value?.focus());
  });
  offIndex = Events.On("launcher:index", () => {
    if (query.value.trim()) void search();
  });
  // 冷建时页面刚 mount 就是新的一次点亮，直接抢焦点；复用窗口靠 launcher:show 事件
  nextTick(() => inputRef.value?.focus());
});

onBeforeUnmount(() => {
  if (timer) clearTimeout(timer);
  const off = [offShow, offIndex];
  for (const f of off) {
    try {
      f?.();
    } catch {
      /* 事件句柄本来就不一定存在 */
    }
  }
});
</script>

<style scoped>
/* 窗口本身完全透明（Go 侧已关掉无边框装饰，不留 DWM 玻璃底板）：
   四周留白给卡片投影腾地方（窗口的原生投影已被关掉），同时点击空白处即收起 */
.lk-wrap {
  width: 100vw;
  height: 100vh;
  box-sizing: border-box;
  padding: 12px 12px 26px;
  display: flex;
  justify-content: center;
  align-items: flex-start;
  background: transparent;
  font-family: "Microsoft YaHei", "Segoe UI", sans-serif;
}
.lk-card {
  width: 100%;
  max-height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--color-bg-1, #fff);
  border: 1px solid var(--color-border-2, #e5e6eb);
  border-radius: var(--base-radius-default);
  box-shadow: 0 6px 22px rgba(0, 0, 0, 0.22);
  overflow: hidden;
  user-select: none;
}
.lk-input-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 14px;
  /* 输入行不再靠固定高撑开：18px 的图标加上下一点呼吸就够，行高再大只剩空白 */
  height: 42px;
  flex: none;
  border-bottom: 1px solid var(--color-border-1, #f2f2f5);
  color: var(--color-text-2, #4e5969);
}
.lk-ic {
  display: flex;
  align-items: center;
  flex: none;
}
.lk-input {
  flex: 1;
  border: none;
  outline: none;
  background: transparent;
  font-size: var(--base-size-large);
  color: var(--color-text-1, #1d2129);
}
.lk-input::placeholder {
  color: var(--color-text-3, #86909c);
  font-size: var(--base-size-default);
}
.lk-tip-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: rgb(var(--orange-6, 255, 125, 0));
  flex: none;
}
.lk-list {
  flex: 1 1 auto;
  overflow-y: auto;
  padding: 4px 6px;
}
.lk-item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 5px 12px;
  border-radius: var(--base-radius-default);
  cursor: pointer;
}
/* 选中态不铺灰底（整块灰色压在透明窗上很脏）：只点亮左侧一根短竖条，
   再加一点投影把它抬起来——与 nginx 实例列表的选中口径一致。 */
.lk-item.active {
  box-shadow: 0 1px 6px rgba(0, 0, 0, 0.14);
}
.lk-item.active::before {
  content: "";
  position: absolute;
  left: 0;
  top: 50%;
  transform: translateY(-50%);
  width: 3px;
  height: 56%;
  border-radius: 0 var(--base-radius-small) var(--base-radius-small) 0;
  background: rgb(var(--primary-6));
}
/* 图标位不再垫灰色方块：应用图标本身是彩色 PNG，背后铺一层灰只会看着脏 */
.lk-item-ic {
  width: 28px;
  height: 28px;
  flex: none;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--color-text-3, #86909c);
  overflow: hidden;
}
.lk-item-ic img {
  width: 24px;
  height: 24px;
  border-radius: var(--base-radius-small);
}
/* 取不到真图标时用一个细描边字符占位，不填底色 */
.lk-item-ic b {
  width: 22px;
  height: 22px;
  line-height: 20px;
  text-align: center;
  font-size: var(--base-size-small);
  font-weight: 600;
  border: 1px solid var(--color-border-2, #e5e6eb);
  border-radius: var(--base-radius-small);
}
.lk-item-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.lk-item-title {
  font-size: var(--base-size-default);
  color: var(--color-text-1, #1d2129);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.lk-item-sub {
  font-size: var(--base-size-small);
  color: var(--color-text-3, #86909c);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
/* 类型标签：去掉灰色药丸，只留一个靠右的弱色短词，选中行才点亮成主题色 */
.lk-item-tag {
  flex: none;
  font-size: var(--base-size-small);
  color: var(--color-text-4, #c9cdd4);
}
.lk-item.active .lk-item-tag {
  color: rgb(var(--primary-6));
}
.lk-item.danger .lk-item-title {
  color: rgb(var(--red-6, 245, 63, 63));
}
.lk-item.danger .lk-item-tag {
  color: rgb(var(--red-6, 245, 63, 63));
}
.lk-item-confirm {
  flex: none;
  font-size: var(--base-size-small);
  color: rgb(var(--red-6, 245, 63, 63));
}
.lk-empty {
  flex: 1 1 auto;
  padding: 10px 16px;
  color: var(--color-text-2, #4e5969);
  overflow-y: auto;
}
.lk-empty-title {
  font-size: var(--base-size-default);
  margin-bottom: 6px;
}
.lk-empty-sub {
  font-size: var(--base-size-small);
  color: var(--color-text-3, #86909c);
}
.lk-hints {
  margin: 0;
  padding-left: 0;
  list-style: none;
  font-size: var(--base-size-small);
  color: var(--color-text-3, #86909c);
}
.lk-hints li {
  line-height: 20px;
}
.lk-hints b {
  display: inline-block;
  min-width: 88px;
  color: var(--color-text-2, #4e5969);
  font-weight: 600;
}
.lk-foot {
  flex: none;
  display: flex;
  gap: 18px;
  padding: 5px 16px;
  font-size: var(--base-size-small);
  color: var(--color-text-4, #c9cdd4);
  border-top: 1px solid var(--color-border-1, #f2f2f5);
}
</style>
