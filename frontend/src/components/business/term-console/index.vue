<template>
  <!-- 右键菜单：复制 / 粘贴 / 全选。终端里的 Ctrl+C 要留给远端（中断信号），
       所以复制走右键与 Ctrl+Shift+C，粘贴就是 Ctrl+V。
       a-dropdown 的 trigger 直接并到终端这层上，不多包一层元素。
       align-point 必须开：不然弹层会锚在终端元素上（跑到卡片边上），而不是跟着鼠标出现。 -->
  <a-dropdown trigger="contextMenu" :align-point="true" @select="onMenu">
    <!-- 铺满容器（留白在内部），不画圆角；FitAddon 量的是这层的尺寸 -->
    <div ref="boxEl" class="term-console"></div>
    <template #content>
      <a-doption value="copy" :disabled="!hasSelection"><icon-copy />复制</a-doption>
      <a-doption value="paste"><icon-paste />粘贴</a-doption>
      <a-doption value="selectAll"><icon-select-all />全选</a-doption>
    </template>
  </a-dropdown>
</template>

<script setup lang="ts">
import { getCurrentInstance, onBeforeUnmount, onMounted, ref } from 'vue';
import { Clipboard, Events } from '@wailsio/runtime';
import { Terminal, type ITheme } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';

/**
 * 通用终端视图（xterm 封装）。
 *
 * 只认「怎么打开会话」的回调，不关心对端是 SSH 主机还是容器：
 *   - SSH 页传 () => GoTools.TermOpenSSH(host.id, cols, rows)
 *   - 容器入口传 () => GoTools.TermOpenDocker(cid, cols, rows)
 * 输入 / 改尺寸 / 关闭都走通用的 TermWrite / TermResize / TermClose。
 * 因此同一个组件可以挂到任何“点一下进终端”的入口上。
 */
const props = withDefaults(
  defineProps<{
    /** 建立会话：入参为终端行列，返回 Go 侧会话句柄 */
    openSession: (cols: number, rows: number) => Promise<{ id: string }>;
    /** 字号（CSS 像素），默认 13 */
    fontSize?: number;
  }>(),
  { fontSize: 13 },
);

const emit = defineEmits<{
  (e: 'ready', sessionId: string): void;
  (e: 'exit', reason: string): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const boxEl = ref<HTMLDivElement>();
/** 当前是否有选中内容（右键菜单里「复制」的可用性） */
const hasSelection = ref(false);
let term: Terminal | null = null;
let fit: FitAddon | null = null;
let observer: ResizeObserver | null = null;
/** 监听 body[arco-theme]：切主题时只换配色，不重建终端 */
let themeWatcher: MutationObserver | null = null;
let offData: (() => void) | null = null;
let offExit: (() => void) | null = null;
/** Go 侧会话 id；拿到之前到达的输出先攒着，别把 banner 丢了 */
let sessionId = '';
let pending: { sessionId: string; data: string }[] = [];
let disposed = false;

/**
 * 终端配色：跟随应用主题（body[arco-theme]）。
 * 两套都把 ANSI 基础色补齐：不自己给 palette 时，xterm 默认色是给纯黑底调的，
 * 在暗黑下 ls 的蓝/绿几乎看不清；浅色底面上又偏霓虹。
 */
function themeOf(): ITheme {
  const dark = document.body.getAttribute('arco-theme') === 'dark';
  return dark
    ? {
        // 比卡片（bg-2）深一点的底色，铺满时能看出终端自己的区域
        background: '#17171a',
        foreground: '#e5e6eb',
        cursor: '#e5e6eb',
        cursorAccent: '#17171a',
        selectionBackground: '#3c7eff66',
        black: '#1f1f22', red: '#f76965', green: '#8edb6c', yellow: '#f7c95c',
        blue: '#4c8dff', magenta: '#c48cf5', cyan: '#4fd1c5', white: '#d5d6d9',
        brightBlack: '#6b6c72', brightRed: '#ff9a94', brightGreen: '#b3ed94', brightYellow: '#ffd67a',
        brightBlue: '#7aa9ff', brightMagenta: '#d9aef7', brightCyan: '#7fe3da', brightWhite: '#ffffff',
      }
    : {
        // 白卡片上纯白终端会和卡片糊在一起，用 fill-1 拉开一点层次
        background: '#f7f8fa',
        foreground: '#1d2129',
        cursor: '#1d2129',
        cursorAccent: '#f7f8fa',
        selectionBackground: '#165dff33',
        black: '#1d2129', red: '#cb2634', green: '#0f9d58', yellow: '#b7791f',
        blue: '#165dff', magenta: '#8e3fd1', cyan: '#0e8a86', white: '#c9cdd4',
        brightBlack: '#4e5969', brightRed: '#e5484d', brightGreen: '#22a06b', brightYellow: '#d48806',
        brightBlue: '#3c7eff', brightMagenta: '#a05cf0', brightCyan: '#13a8a8', brightWhite: '#ffffff',
      };
}

function toB64(text: string): string {
  const bytes = new TextEncoder().encode(text);
  let bin = '';
  bytes.forEach((b) => (bin += String.fromCharCode(b)));
  return btoa(bin);
}

function fromB64(b64: string): Uint8Array {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i += 1) out[i] = bin.charCodeAt(i);
  return out;
}

/* ==================== 复制 / 粘贴 ====================
 * 走 Wails 运行时剪贴板（Go 侧读写），不用 navigator.clipboard：
 * WebView2 里读剪贴板要授权，还会被“窗口没焦点”卡住。
 * 复制取 xterm 的选区文本；粘贴用 term.paste()，它会按 bracketed paste
 * 模式包上 \e[200~/\e[201~，vim/bash 里不会把多行当命令立即执行。 */
function copySelection() {
  const text = term?.getSelection() || '';
  if (text) void Clipboard.SetText(text);
}

function pasteClipboard() {
  void Clipboard.Text().then((text) => {
    if (!text) return;
    term?.paste(text);
    // 右键菜单点完焦点要还给终端，接着就能敲命令
    term?.focus();
  });
}

function onMenu(value: any) {
  if (value === 'copy') copySelection();
  else if (value === 'paste') pasteClipboard();
  else if (value === 'selectAll') term?.selectAll();
  // 菜单点完把焦点还给终端，不用再点一下才能继续敲命令
  term?.focus();
}

/** 主动断开会话（父组件关弹窗/关标签时调用，不必等 Vue 卸载时机） */
function closeSession() {
  const id = sessionId;
  sessionId = '';
  if (id) void GoTools?.TermClose(id);
}

defineExpose({ closeSession, focus: () => term?.focus() });

function flushPending() {
  // 会话 id 到手后只看自己那份：别的标签的输出自己不负责渲染，直接丢掉
  const queue = pending;
  pending = [];
  queue.forEach((p) => {
    if (p.sessionId === sessionId) term?.write(fromB64(p.data));
  });
}

onMounted(async () => {
  const el = boxEl.value;
  if (!el) return;

  term = new Terminal({
    fontSize: props.fontSize,
    fontFamily: 'Consolas, "Cascadia Mono", "Microsoft YaHei", monospace',
    cursorBlink: true,
    scrollback: 5000,
    theme: themeOf(),
  });
  fit = new FitAddon();
  term.loadAddon(fit);
  term.open(el);
  fit.fit();

  // 先挂事件再建会话：远端 banner / MOTD 可能在 TermOpenSSH 返回之前就推过来
  offData = Events.On('term:data', (raw: any) => {
    const p = raw?.data ?? raw;
    if (!p?.sessionId || !p?.data) return;
    if (!sessionId) {
      // 最多攒一小段：够护住 banner 即可，不无限吃内存
      if (pending.length < 200) pending.push({ sessionId: p.sessionId, data: p.data });
      return;
    }
    if (p.sessionId !== sessionId) return;
    term?.write(fromB64(p.data));
  });
  offExit = Events.On('term:exit', (raw: any) => {
    const p = raw?.data ?? raw;
    if (!p?.sessionId || p.sessionId !== sessionId) return;
    sessionId = '';
    emit('exit', p.reason || '连接已断开');
  });

  term.onData((data) => {
    if (sessionId) void GoTools?.TermWrite(sessionId, toB64(data));
  });
  term.onResize(({ cols, rows }) => {
    if (sessionId) void GoTools?.TermResize(sessionId, cols, rows);
  });
  term.onSelectionChange(() => {
    hasSelection.value = !!term?.getSelection();
  });

  // 终端快捷键：Ctrl+V / Ctrl+Shift+V 粘贴（Ctrl+V 是大家的下意识动作，
  // 代价是 vim 的 Ctrl+V 块选择会被占掉——Windows 下用 Ctrl+Q 同效）；
  // 复制靠 Ctrl+Shift+C、Ctrl+Insert，Ctrl+C 不劫持，还是给远端发 ^C。
  // 返回 false 表示这一下不交给 xterm，也就不会把 ^V 之类的控制字符发给远端。
  term.attachCustomKeyEventHandler((e) => {
    if (e.type !== 'keydown') return true;
    if (e.ctrlKey && e.shiftKey && e.code === 'KeyC') {
      copySelection();
      return false;
    }
    if (e.ctrlKey && e.code === 'KeyV') {
      pasteClipboard();
      return false;
    }
    if (e.ctrlKey && e.code === 'Insert') {
      copySelection();
      return false;
    }
    if (e.shiftKey && e.code === 'Insert') {
      pasteClipboard();
      return false;
    }
    return true;
  });

  // 容器尺寸变化（切会话、拖分隔条、窗口变化）时重算行列。
  // 隐藏中的会话（display:none）量出来是 0，此时 fit 会把行列压到最小，
  // 还会把远端的 PTY 也改小，所以没尺寸就跳过，等它重新可见再量。
  observer = new ResizeObserver(() => {
    const box = boxEl.value;
    if (!box || !box.clientWidth || !box.clientHeight) return;
    fit?.fit();
  });
  observer.observe(el);

  // 应用切深浅色时同步终端配色（body[arco-theme] 是 Arco 的开关）
  themeWatcher = new MutationObserver(() => {
    if (term) term.options.theme = themeOf();
  });
  themeWatcher.observe(document.body, { attributes: true, attributeFilter: ['arco-theme'] });

  try {
    const info = await props.openSession(term.cols, term.rows);
    if (disposed) {
      void GoTools?.TermClose(info.id);
      return;
    }
    sessionId = info.id;
    flushPending();
    emit('ready', info.id);
    term.focus();
  } catch (e: any) {
    emit('exit', String(e?.message || e || '连接失败'));
  }
});

onBeforeUnmount(() => {
  disposed = true;
  observer?.disconnect();
  themeWatcher?.disconnect();
  themeWatcher = null;
  offData?.();
  offExit?.();
  closeSession();
  term?.dispose();
  term = null;
});
</script>

<style scoped>
/* 铺满容器（留白留在内部），不画圆角 */
.term-console {
  box-sizing: border-box;
  height: 100%;
  overflow: hidden;
}

/* 内容内缩：padding 加在 .xterm 上，FitAddon 会把它减掉，行列不受影响 */
.term-console :deep(.xterm) {
  padding: 6px 10px 10px;
}
</style>
