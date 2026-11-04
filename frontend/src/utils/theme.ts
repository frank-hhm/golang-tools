/**
 * 应用主题：在原有 light/dark/auto 之上，叠加一层与之正交的设计令牌（主色 / 圆角 / 密度 /
 * 高对比度 / 减少动效 / 玻璃占位）。
 *
 * 生效方式（对齐全站已在读的 CSS 变量，零页面改动）：
 *  - mode：切 body[arco-theme] 与 colorScheme（沿用旧行为，浮动球/窗口首帧据此定深浅）；
 *  - 数值型令牌：把结果写进 documentElement.style（inline 覆盖 :root 与 arco.css）——
 *    --primary-1..10 由主色生成十档 ramp，--base-radius-*--border-radius-* 管圆角，
 *    --base-size-*--base-margin/--base-padding 管密度；组件自动跟随。
 *  - 开关型：靠 body 上的 reduce-motion / a11y-contrast / density-* 类，由 styles/theme/tokens.scss 消费。
 *
 * 持久化双层：
 *  - localStorage（app-theme-cfg，整套 JSON）：窗口启动同步读取无闪烁，storage 事件跨窗口即时同步；
 *  - config.general.theme（后端）：随应用配置保存，作为首次运行/新窗口的回退值。
 * 兼容旧值：早期 localStorage 只存模式串（app-theme-mode）、后端 theme 只存字符串，读取时归一为整套配置。
 */

import type { ThemeScheme } from "./palette";
import { buildTheme, normalizeHarmony, type HarmonyMode } from "./palette";
import { resolveTheme } from "./presets";

export type ThemeMode = "light" | "dark" | "auto";

/** 前端整套主题令牌配置，与后端 config.general.theme（bindings 的 ThemeConfig）同构 */
export interface ThemeConfig {
  mode: ThemeMode;
  preset: string;
  primary?: string;
  harmony?: string;
  radius: string;
  density: string;
  fontSize: string;
  contrast: string;
  motion: string;
  glass: string;
}

/** 旧：仅模式的 localStorage 键（保留以兼容读取与 theme-switch 的 storage 同步） */
export const THEME_STORAGE_KEY = "app-theme-mode";
/** 新：整套令牌配置的 localStorage 键 */
export const THEME_CFG_STORAGE_KEY = "app-theme-cfg";

export const DEFAULT_THEME: ThemeConfig = {
  mode: "light",
  preset: "default",
  primary: "",
  harmony: "analogous",
  radius: "default",
  density: "standard",
  fontSize: "default",
  contrast: "normal",
  motion: "on",
  glass: "off",
};

// 圆角映射：同时投射到项目自用的 --base-radius-* 与 Arco 的 --border-radius-*。
// default 档与 index.scss 现状 / arco.css 默认严格相等，保证「默认主题」是纯 no-op。
const RADIUS_MAP: Record<string, { base: [number, number, number, number]; arco: [number, number, number] }> = {
  none: { base: [0, 0, 0, 0], arco: [0, 0, 0] },
  small: { base: [2, 4, 6, 12], arco: [2, 3, 4] },
  default: { base: [2, 6, 10, 20], arco: [2, 4, 8] },
  large: { base: [4, 10, 14, 24], arco: [4, 8, 12] },
};

// 密度映射：--base-size-small/default/large/large-x/large-xx + --base-margin/padding，
// 以及一个 body 密度类（供 tokens.scss 尽力微调 .arco-* 控件纵向留白）。standard 与现状一致。
const DENSITY_MAP: Record<string, { sizes: number[]; gap: number; cls: string }> = {
  compact: { sizes: [11, 11, 13, 15, 18], gap: 8, cls: "density-compact" },
  standard: { sizes: [12, 12, 14, 16, 20], gap: 12, cls: "" },
  loose: { sizes: [13, 13, 15, 18, 22], gap: 16, cls: "density-loose" },
};

// 字号映射：独立于密度的字体缩放系数，乘在密度给出的 --base-size-* 之上。
// default=1.0 与现状一致（纯 no-op）；只放大字号，间距/留白仍由密度独立决定。
const FONT_SCALE_MAP: Record<string, number> = {
  small: 0.9,
  default: 1,
  large: 1.12,
  xlarge: 1.25,
};

function prefersDark(): boolean {
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

/** 主题模式最终呈现是否为暗黑（auto 按系统实际偏好判定） */
export function isDark(mode: ThemeMode): boolean {
  return mode === "dark" || (mode === "auto" && prefersDark());
}

/** 明暗档：主色 ramp 的浅/深生成用哪一套（auto 解析成实际偏好） */
export function effectiveScheme(mode: ThemeMode): ThemeScheme {
  return isDark(mode) ? "dark" : "light";
}

/** 把任意来源（旧模式串 / 不完整对象）归一成一整套合法 ThemeConfig，缺项回落默认 */
export function normalizeConfig(input: unknown): ThemeConfig {
  // 旧配置：裸字符串只表达 mode
  if (typeof input === "string") {
    const mode = input === "dark" || input === "auto" ? input : "light";
    return { ...DEFAULT_THEME, mode };
  }
  const src = (input || {}) as Partial<ThemeConfig>;
  const mode: ThemeMode = src.mode === "dark" || src.mode === "auto" ? src.mode : "light";
  return {
    mode,
    preset: src.preset || DEFAULT_THEME.preset,
    primary: src.primary || "",
    harmony: normalizeHarmony(src.harmony || "") as HarmonyMode,
    radius: RADIUS_MAP[src.radius || ""] ? src.radius! : DEFAULT_THEME.radius,
    density: DENSITY_MAP[src.density || ""] ? src.density! : DEFAULT_THEME.density,
    fontSize: FONT_SCALE_MAP[src.fontSize || ""] ? src.fontSize! : DEFAULT_THEME.fontSize,
    contrast: src.contrast === "high" ? "high" : "normal",
    motion: src.motion === "reduced" ? "reduced" : "on",
    glass: src.glass === "surface" ? "surface" : "off",
  };
}

/** 当前整套主题配置（localStorage 优先，无则默认） */
export function currentThemeConfig(): ThemeConfig {
  const raw = localStorage.getItem(THEME_CFG_STORAGE_KEY);
  if (raw) {
    try {
      return normalizeConfig(JSON.parse(raw));
    } catch {
      /* 落井下：解析失败按默认 */
    }
  }
  // 迁移：旧版只存了模式串
  const legacy = localStorage.getItem(THEME_STORAGE_KEY);
  return normalizeConfig(legacy ? legacy : DEFAULT_THEME);
}

/** 当前主题模式（localStorage 优先）——保留旧 API，theme-switch 等仍在用 */
export function currentTheme(): ThemeMode {
  return currentThemeConfig().mode;
}

function setVars(el: HTMLElement, entries: Record<string, string>) {
  for (const [k, v] of Object.entries(entries)) el.style.setProperty(k, v);
}

function removeVars(el: HTMLElement, names: string[]) {
  for (const k of names) el.style.removeProperty(k);
}

/** 投射整套令牌到 DOM（不写 localStorage；由调用方负责持久化） */
function renderTokens(cfg: ThemeConfig) {
  const root = document.documentElement;
  const body = document.body;

  // ① 明暗：切 arco-theme 与 colorScheme
  const dark = isDark(cfg.mode);
  body.setAttribute("arco-theme", dark ? "dark" : "light");
  root.style.colorScheme = dark ? "dark" : "light";

  // ② 配色：种子 + 和谐模式 → OKLCH 整套调色板。先写真源 --app-*（新 UI 消费），
  //    再把 brand 投给 Arco --primary-1..10（三元组）作兼容出口，存量页/组件零改动跟随。
  const { seed, harmony } = resolveTheme(cfg.preset, cfg.primary || "", cfg.harmony);
  const theme = buildTheme(seed, harmony, effectiveScheme(cfg.mode));
  const scaleKeys = ["brand", "accent", "secondary", "neutral"];
  const roleKeys = ["success", "warning", "danger", "info", "brand", "accent", "secondary"];
  if (theme) {
    const vars: Record<string, string> = {};
    for (const key of scaleKeys) {
      theme[key as "brand" | "accent" | "secondary" | "neutral"].forEach((hex, i) => {
        vars[`--app-${key}-${i + 1}`] = hex;
      });
    }
    Object.entries(theme.semantic).forEach(([k, hex]) => (vars[`--app-${k}`] = hex));
    vars["--app-brand"] = theme.seeds.brand;
    vars["--app-accent"] = theme.seeds.accent;
    vars["--app-secondary"] = theme.seeds.secondary;
    theme.arcoPrimary.forEach((triplet, i) => (vars[`--primary-${i + 1}`] = triplet));
    setVars(root, vars);
  } else {
    const sink: string[] = [];
    for (let i = 1; i <= 10; i++) {
      sink.push(`--primary-${i}`);
      for (const k of scaleKeys) sink.push(`--app-${k}-${i}`);
    }
    for (const k of roleKeys) sink.push(`--app-${k}`);
    removeVars(root, sink);
  }

  // ③ 圆角
  const r = RADIUS_MAP[cfg.radius] || RADIUS_MAP.default;
  setVars(root, {
    "--base-radius-small": `${r.base[0]}px`,
    "--base-radius-default": `${r.base[1]}px`,
    "--base-radius-large": `${r.base[2]}px`,
    "--base-radius-large-x": `${r.base[3]}px`,
    "--border-radius-small": `${r.arco[0]}px`,
    "--border-radius-medium": `${r.arco[1]}px`,
    "--border-radius-large": `${r.arco[2]}px`,
  });

  // ④ 密度 + 字号：密度定间距（--base-margin/padding），字号按独立系数缩放 --base-size-*。
  //    两者正交：紧凑密度也能配大字号，字号不跟随密度。
  const d = DENSITY_MAP[cfg.density] || DENSITY_MAP.standard;
  const fs = FONT_SCALE_MAP[cfg.fontSize] || 1;
  setVars(root, {
    "--base-size-small": `${Math.round(d.sizes[0] * fs)}px`,
    "--base-size-default": `${Math.round(d.sizes[1] * fs)}px`,
    "--base-size-large": `${Math.round(d.sizes[2] * fs)}px`,
    "--base-size-large-x": `${Math.round(d.sizes[3] * fs)}px`,
    "--base-size-large-xx": `${Math.round(d.sizes[4] * fs)}px`,
    "--base-margin": `${d.gap}px`,
    "--base-padding": `${d.gap}px`,
  });
  body.classList.toggle("density-compact", cfg.density === "compact");
  body.classList.toggle("density-loose", cfg.density === "loose");

  // ⑤ 高对比度 / 减少动效：body 类开关
  body.classList.toggle("a11y-contrast", cfg.contrast === "high");
  body.classList.toggle("reduce-motion", cfg.motion === "reduced");

  // ⑥ 玻璃（阶段一占位，不生效）
  body.classList.toggle("app-glass", cfg.glass === "surface");
}

/** 应用并持久化整套主题配置 */
export function applyThemeConfig(cfg: ThemeConfig) {
  const norm = normalizeConfig(cfg);
  renderTokens(norm);
  localStorage.setItem(THEME_CFG_STORAGE_KEY, JSON.stringify(norm));
  localStorage.setItem(THEME_STORAGE_KEY, norm.mode); // 双写模式键，兼容旧 theme-switch 跨窗口同步
}

/** 立即应用主题模式（旧 API，保留）：把模式并入当前配置后整套重渲染并持久化 */
export function applyTheme(mode: ThemeMode) {
  applyThemeConfig({ ...currentThemeConfig(), mode });
}

let inited = false;

/** 启动初始化：同步应用已记忆的主题，监听系统偏好变化与跨窗口同步 */
export function initTheme() {
  if (inited) return;
  inited = true;
  applyThemeConfig(currentThemeConfig());
  // auto 模式下系统深浅切换时重算（主色 ramp 明暗档也随之变）
  window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
    applyThemeConfig(currentThemeConfig());
  });
  // 其他窗口改主题时，storage 事件广播同步
  window.addEventListener("storage", (e) => {
    if (e.key === THEME_CFG_STORAGE_KEY && e.newValue) {
      try {
        renderTokens(normalizeConfig(JSON.parse(e.newValue)));
      } catch {
        /* 忽略非法同步值 */
      }
    } else if (e.key === THEME_STORAGE_KEY && e.newValue) {
      // 兼容仅同步模式的旧窗口（如 theme-switch）
      applyTheme(e.newValue as ThemeMode);
    }
  });
}

/** 从后端配置回退应用主题（仅本地从未选择过主题时生效）。兼容旧的字符串入参。 */
export function syncThemeFromConfig(theme?: ThemeConfig | string | null) {
  if (!theme) return;
  if (localStorage.getItem(THEME_CFG_STORAGE_KEY) || localStorage.getItem(THEME_STORAGE_KEY)) {
    return; // 本地已有选择，不覆盖
  }
  applyThemeConfig(normalizeConfig(theme));
}
