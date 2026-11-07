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
import { App } from "bindings/Tools/services/app/index";
import { isNativeWindows, loadCaps } from "./caps";

export type ThemeMode = "light" | "dark" | "auto";

/** 表面玻璃材质参数（与后端 config.general.theme 的 ThemeGlass 同构）。数值存整数，前端各自除数还原 */
export interface ThemeGlass {
  on: boolean;
  material: "frosted" | "acrylic";
  alpha: number; // 0-100（÷100）
  blur: number; // 0-40 px
  border: number; // 0-100（÷100）
  shadow: number; // 0-50（÷100）
  noise: number; // 0-120（÷1000），仅 acrylic 消费
}

/** 全局背景主题档（与后端 ThemeBackground 同构） */
export type BackgroundType = "none" | "mesh" | "photo" | "dark" | "image" | "native";
export interface ThemeBackground {
  type: BackgroundType;
  image: string; // 绝对路径，仅 type=image
}

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
  /** 无边距模式：卡片去左右内边距、不铺背景，描边固定归无（见 tokens.scss card-flush 段） */
  flush: boolean;
  glass: ThemeGlass;
  background: ThemeBackground;
}

/** 旧：仅模式的 localStorage 键（保留以兼容读取与 theme-switch 的 storage 同步） */
export const THEME_STORAGE_KEY = "app-theme-mode";
/** 新：整套令牌配置的 localStorage 键 */
export const THEME_CFG_STORAGE_KEY = "app-theme-cfg";

/** 默认玻璃：关闭（纯 no-op），参数取实验台定稿那组观感，开关一开即所见 */
export const DEFAULT_GLASS: ThemeGlass = {
  on: false,
  material: "acrylic",
  alpha: 60,
  blur: 0,
  border: 0,
  shadow: 18,
  noise: 120,
};

/** 默认背景：none（与改动前像素级一致，不铺任何全局背景） */
export const DEFAULT_BACKGROUND: ThemeBackground = { type: "none", image: "" };

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
  flush: false,
  glass: { ...DEFAULT_GLASS },
  background: { ...DEFAULT_BACKGROUND },
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
    return {
      ...DEFAULT_THEME,
      mode,
      glass: { ...DEFAULT_GLASS },
      background: { ...DEFAULT_BACKGROUND },
    };
  }
  const src = (input || {}) as Partial<ThemeConfig> & { glass?: unknown; background?: unknown };
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
    flush: !!src.flush,
    glass: normalizeGlass(src.glass),
    background: normalizeBackground(src.background),
  };
}

function clampInt(v: unknown, lo: number, hi: number, dflt: number): number {
  const n = typeof v === "number" && Number.isFinite(v) ? Math.round(v) : dflt;
  return Math.min(hi, Math.max(lo, n));
}

/** 归一玻璃参数：兼容旧字符串 off|surface（surface 映射成阶段一那组观感），对象走常规夹区间 */
export function normalizeGlass(input: unknown): ThemeGlass {
  if (typeof input === "string") {
    if (input === "surface") {
      return { on: true, material: "frosted", alpha: 72, blur: 16, border: 30, shadow: 18, noise: 0 };
    }
    return { ...DEFAULT_GLASS, on: false };
  }
  const s = (input || {}) as Partial<ThemeGlass>;
  return {
    on: !!s.on,
    material: s.material === "acrylic" ? "acrylic" : "frosted",
    alpha: clampInt(s.alpha, 0, 100, DEFAULT_GLASS.alpha),
    blur: clampInt(s.blur, 0, 40, DEFAULT_GLASS.blur),
    border: clampInt(s.border, 0, 100, DEFAULT_GLASS.border),
    shadow: clampInt(s.shadow, 0, 50, DEFAULT_GLASS.shadow),
    noise: clampInt(s.noise, 0, 120, DEFAULT_GLASS.noise),
  };
}

const BG_TYPES: BackgroundType[] = ["none", "mesh", "photo", "dark", "image", "native"];

/** 归一背景档：非法 type 兜 none；非 image 档清空残留路径 */
export function normalizeBackground(input: unknown): ThemeBackground {
  const s = (input || {}) as Partial<ThemeBackground>;
  const type: BackgroundType = BG_TYPES.includes((s.type || "none") as BackgroundType)
    ? (s.type as BackgroundType)
    : "none";
  return { type, image: type === "image" ? String(s.image || "") : "" };
}

/**
 * 把玻璃参数换算成 CSS 变量表（纯函数、无 DOM 依赖，便于单测与实验台复用）。
 * alpha/border/shadow 除 100、noise 除 1000 还原成 0..1 单位数；blur 直接 px；material 透传给 tokens.scss 选择噪点层。
 */
export function glassVars(g: ThemeGlass): Record<string, string> {
  return {
    "--app-glass-alpha": (g.alpha / 100).toFixed(3),
    "--app-glass-blur": `${g.blur}px`,
    "--app-glass-border": (g.border / 100).toFixed(3),
    "--app-glass-shadow": (g.shadow / 100).toFixed(3),
    "--app-glass-noise": (g.noise / 1000).toFixed(3),
    "--app-glass-material": g.material,
  };
}

/** 玻璃变量名（关闭时按此清单从 documentElement 撤除，回落 tokens.scss :root 占位） */
const GLASS_VAR_NAMES = Object.keys(glassVars(DEFAULT_GLASS));

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

  // ③ 圆角：页面内令牌（--base-radius-* / --border-radius-*）+ 原生窗口四角两处。
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
  applyWindowCorners(cfg.radius);

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

  // ⑤ 高对比度 / 减少动效 / 无边距模式：body 类开关
  body.classList.toggle("a11y-contrast", cfg.contrast === "high");
  body.classList.toggle("reduce-motion", cfg.motion === "reduced");
  body.classList.toggle("card-flush", cfg.flush);

  // ⑥ 玻璃：body.app-glass 开关 + 材质类；开则把参数投进 --app-glass-*，关则撤除回落 :root 占位
  const g = cfg.glass;
  body.classList.toggle("app-glass", g.on);
  body.classList.toggle("mat-acrylic", g.on && g.material === "acrylic");
  body.classList.toggle("mat-frosted", g.on && g.material === "frosted");
  if (g.on) {
    setVars(root, glassVars(g));
  } else {
    removeVars(root, GLASS_VAR_NAMES);
  }

  // ⑦ 背景：body.app-bg-<type> 类驱动背景层与底色让路；image 档异步读 dataURL 写 --app-bg-image
  const bg = cfg.background;
  for (const t of BG_TYPES) body.classList.toggle(`app-bg-${t}`, bg.type === t);
  if (bg.type === "image" && bg.image) {
    void applyBackgroundImage(bg.image);
  } else {
    bgReqSeq++;
    root.style.removeProperty("--app-bg-image");
  }
}

/**
 * 原生窗口四角跟随圆角档：主窗与工具窗的窗口角是 DWM 在合成层裁的，CSS 变量够不着，
 * 只能经 SetWindowCorner 下发到 Go 侧（Win11 只有「系统默认圆角 / 直角」两态：
 * none/small 直角，default/large 系统圆角）。
 * Web/Linux 面板返回不支持、启动早期窗口未就绪等失败都静默忽略（页面内令牌照常生效，
 * 新开窗由 Go 侧按记忆档位补应用）。
 */
function applyWindowCorners(radius: string) {
  try {
    App.SetWindowCorner(radius).catch(() => {
      /* Web 模式返回不支持、启动早期窗口未就绪等：静默，新开窗由 Go 侧按记忆档位补应用 */
    });
  } catch {
    /* 非桌面运行时没有该绑定，忽略 */
  }
}

// 背景图 dataURL 异步加载序号：快速切换背景时丢弃过期回包，避免迟到的图片盖掉当前档。
let bgReqSeq = 0;
function applyBackgroundImage(path: string) {
  const seq = ++bgReqSeq;
  App.ReadFileDataURI(path)
    .then((uri) => {
      if (seq !== bgReqSeq) return;
      if (uri) document.documentElement.style.setProperty("--app-bg-image", `url("${uri}")`);
      else document.documentElement.style.removeProperty("--app-bg-image");
    })
    .catch(() => {
      if (seq === bgReqSeq) document.documentElement.style.removeProperty("--app-bg-image");
    });
}

/**
 * 主题切换淡入开关：首屏那一次应用（initTheme 里）不淡入，避免开机看到「默认浅色→记忆主题」的
 * 整屏渐变；启动完成后置 true，之后用户在设置里切明暗/玻璃/纹理/圆角/密度才淡入。
 */
let allowThemeAnim = false;
let themeAnimTimer = 0;

/** 给 <body> 临时挂 theme-anim 类，让本次令牌变化走颜色过渡（见 tokens.scss），约 360ms 后撤。 */
function beginThemeAnim() {
  if (!allowThemeAnim) return;
  const body = document.body;
  // 尊重「减少动效」偏好；即便漏判，tokens.scss 的近零时长也会兜底压平。
  if (body.classList.contains("reduce-motion")) return;
  body.classList.add("theme-anim");
  if (themeAnimTimer) clearTimeout(themeAnimTimer);
  themeAnimTimer = window.setTimeout(() => body.classList.remove("theme-anim"), 360);
}

/** 应用并持久化整套主题配置 */
export function applyThemeConfig(cfg: ThemeConfig) {
  const norm = normalizeConfig(cfg);
  beginThemeAnim(); // 必须在改令牌之前挂类，浏览器才会把「旧值→新值」当成过渡起点
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
  // 首屏那一次不淡入；等应用与首帧绘制完成（两帧 rAF）后再放开，之后切主题才走过渡。
  requestAnimationFrame(() => requestAnimationFrame(() => { allowThemeAnim = true; }));
  // 启动时窗口角下发可能早于 Go 侧窗口就绪（失败被静默吞掉），能力探测落定后
  // 按当前档再补一次；Web 模式 isNativeWindows 恒 false，不会白打这一枪。
  void loadCaps().then(() => {
    if (isNativeWindows()) applyWindowCorners(currentThemeConfig().radius);
  });
  // auto 模式下系统深浅切换时重算（主色 ramp 明暗档也随之变）
  window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
    applyThemeConfig(currentThemeConfig());
  });
  // 其他窗口改主题时，storage 事件广播同步
  window.addEventListener("storage", (e) => {
    if (e.key === THEME_CFG_STORAGE_KEY && e.newValue) {
      try {
        beginThemeAnim(); // 跨窗口同步也是一次主题变化，同样淡入
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
