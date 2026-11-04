/**
 * 内置预设主题：每套 = {种子色 seed + 色彩和谐模式 harmony}。palette.buildTheme 据此在 OKLCH 上
 * 自动派生 brand/accent/secondary/neutral/semantic 整套协调色，无需手配十档，也不必为明暗各配一份。
 * 落进 config.general.theme.preset 的是这里的 name（稳定键，不随语言变）；自定义走 preset="custom" + primary。
 */

import { normalizeHarmony, type HarmonyMode } from "./palette.ts";

export interface ThemePreset {
  name: string; // 唯一键，落 config.general.theme.preset
  label: string; // 中文名，设置页展示
  seed: string; // 种子主色 hex（对应 brand 基准第 6 档）
  harmony: HarmonyMode; // 该套默认和谐模式（用户可在设置页覆盖）
}

/** CUSTOM：用户用取色器自选种子色，seed 走 ThemeConfig.primary */
export const CUSTOM_PRESET = "custom";

export const COLOR_PRESETS: ThemePreset[] = [
  { name: "default", label: "墨蓝", seed: "#165dff", harmony: "analogous" },
  { name: "graphite", label: "石墨", seed: "#3a3f4b", harmony: "analogous" },
  { name: "ocean", label: "海洋", seed: "#0e7490", harmony: "split" },
  { name: "forest", label: "森野", seed: "#00b42a", harmony: "analogous" },
  { name: "sunset", label: "落日", seed: "#f77234", harmony: "complementary" },
  { name: "purple", label: "紫韵", seed: "#722ed1", harmony: "triadic" },
  { name: "rose", label: "玫红", seed: "#f53f8b", harmony: "analogous" },
];

/** 按 name 取预设；找不到（含 custom）返回 undefined */
export function findPreset(name: string): ThemePreset | undefined {
  return COLOR_PRESETS.find((p) => p.name === name);
}

/**
 * 解析出「种子色 + 生效和谐模式」：
 *  - 命名预设：用预设 seed；harmony 取用户覆盖值，无则预设默认。
 *  - custom / 未知：seed 用 primary（非法则由调用方 buildTheme 返回 null 回落）。
 */
export function resolveTheme(preset: string, primary: string, harmony?: string): { seed: string; harmony: HarmonyMode } {
  const p = findPreset(preset);
  if (p) {
    return { seed: p.seed, harmony: harmony ? normalizeHarmony(harmony) : p.harmony };
  }
  return { seed: primary || "", harmony: normalizeHarmony(harmony) };
}
