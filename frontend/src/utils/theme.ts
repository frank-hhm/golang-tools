/**
 * 应用主题（浅色 / 暗黑 / 跟随系统）。
 * 暗黑依赖 Arco 内置的 body[arco-theme='dark'] 变量集，切换该属性即全局生效。
 * 持久化双层：
 *  - localStorage：窗口启动时同步读取（无闪烁），storage 事件实现多窗口即时同步
 *  - config.general.theme：随应用配置保存（后端持久化，作为首次运行的回退值）
 */
export type ThemeMode = "light" | "dark" | "auto";

const STORAGE_KEY = "app-theme-mode";

function prefersDark(): boolean {
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

function isDark(mode: ThemeMode): boolean {
  return mode === "dark" || (mode === "auto" && prefersDark());
}

/** 当前主题模式（localStorage 优先） */
export function currentTheme(): ThemeMode {
  return (localStorage.getItem(STORAGE_KEY) as ThemeMode) || "light";
}

/** 立即应用主题：切换 body 的 arco-theme 属性并记忆选择 */
export function applyTheme(mode: ThemeMode) {
  localStorage.setItem(STORAGE_KEY, mode);
  const dark = isDark(mode);
  document.body.setAttribute("arco-theme", dark ? "dark" : "light");
  document.documentElement.style.colorScheme = dark ? "dark" : "light";
}

let inited = false;

/** 启动初始化：同步应用已记忆的主题，并监听系统主题变化与跨窗口同步 */
export function initTheme() {
  if (inited) return;
  inited = true;
  applyTheme(currentTheme());
  // “跟随系统”模式下，系统主题切换时实时响应
  window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
    applyTheme(currentTheme());
  });
  // 其他窗口（如设置页独立窗口）修改主题时，storage 事件自动广播同步
  window.addEventListener("storage", (e) => {
    if (e.key === STORAGE_KEY && e.newValue) {
      applyTheme(e.newValue as ThemeMode);
    }
  });
}

/** 从后端配置回退应用主题（仅本地从未选择过主题时生效） */
export function syncThemeFromConfig(theme?: string) {
  if (theme && !localStorage.getItem(STORAGE_KEY)) {
    applyTheme(theme as ThemeMode);
  }
}
