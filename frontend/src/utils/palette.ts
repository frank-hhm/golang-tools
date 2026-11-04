/**
 * 调色板生成（我们的搭配方式）：由「一个种子色 + 一种色彩和谐模式」产出一整套协调调色板，
 * 全部基于 OKLCH（见 color.ts），不再走 Arco 的线性混白/混黑 1-10。
 *
 * 产物（每套 10 档，index 5 = 基准/种子，对应 Arco --primary-6）：
 *  - brand / accent / secondary：种子色相 + 和谐偏移各自的明度标度；
 *  - neutral：种子色相 + 极低饱和的「染色中性」（背景/文字/边框带主题色温，更成套）；
 *  - semantic：success/warning/danger/info 固定色相区间、同一明度曲线，随明暗微调；
 *  - arcoPrimary：brand 标度转成逗号 RGB 三元组，作投给 Arco --primary-1..10 的兼容出口。
 *
 * 明暗（scheme）：light 档 1 最浅→10 最深、dark 档整体反转（与 Arco 暗黑同一规律，
 * 但明度/饱和由 OKLCH 感知曲线决定，不发灰、不串色）。非法种子返回 null，调用方跳过投射。
 */

import {
  hexToRgb,
  oklchToRgb,
  rgbToTriplet,
  rgbToHex,
  hexToOklch,
  type Oklch,
  type Rgb,
} from "./color.ts";

export type ThemeScheme = "light" | "dark";
export type HarmonyMode = "analogous" | "complementary" | "split" | "triadic";

export { hexToRgb, rgbToHex, rgbToTriplet };

/** 和谐模式 → [brand, accent, secondary] 相对种子的色相偏移（度） */
export const HARMONY_OFFSETS: Record<HarmonyMode, [number, number, number]> = {
  analogous: [0, 30, -30],
  complementary: [0, 180, 210],
  split: [0, 150, 210],
  triadic: [0, 120, 240],
};

export const DEFAULT_HARMONY: HarmonyMode = "analogous";

function isHarmony(v: string): v is HarmonyMode {
  return v === "analogous" || v === "complementary" || v === "split" || v === "triadic";
}

export function normalizeHarmony(v: string | undefined | null): HarmonyMode {
  return v && isHarmony(v) ? v : DEFAULT_HARMONY;
}

// 明度曲线的两端锚点（OKLCH L：0.985 近白、0.16 近黑）
const L_TOP = 0.985;
const L_BOTTOM = 0.16;

/**
 * 生成某色相hue上、以 baseL 为第 6 档基准的 10 档明度曲线（index 0..9）。
 * light：0 最浅→9 最深；dark：0 最深→9 最浅。饱和按到基准的距离高斯收敛，两端不发灰也不刺眼。
 */
function scaleL(hue: number, baseL: number, baseC: number, scheme: ThemeScheme, chromaScale = 1): Oklch[] {
  const out: Oklch[] = [];
  for (let i = 0; i < 10; i++) {
    let L: number;
    if (i === 5) {
      L = baseL;
    } else if (scheme === "light") {
      if (i < 5) {
        const d = (5 - i) / 5; // 0..1，越接近 0 档越浅
        L = baseL + (L_TOP - baseL) * Math.pow(d, 1.3);
      } else {
        const d = (i - 5) / 4;
        L = baseL - (baseL - L_BOTTOM) * Math.pow(d, 1.25);
      }
    } else {
      // dark：1 最深→10 最浅，与 light 镜像
      if (i < 5) {
        const d = (5 - i) / 5;
        L = baseL - (baseL - L_BOTTOM) * Math.pow(d, 1.3);
      } else {
        const d = (i - 5) / 4;
        L = baseL + (L_TOP - baseL) * Math.pow(d, 1.25);
      }
    }
    const gauss = 1 - 0.02 * (i - 5) * (i - 5); // 基准最饱，两端收敛
    const C = baseC * chromaScale * Math.max(0.4, Math.min(1, gauss));
    out.push({ L, C, H: hue });
  }
  return out;
}

function scaleHex(hue: number, baseL: number, baseC: number, scheme: ThemeScheme, chromaScale = 1): string[] {
  return scaleL(hue, baseL, baseC, scheme, chromaScale).map((c) => rgbToHex(oklchToRgb(c)));
}

/** 一整套协调调色板 */
export interface BuiltTheme {
  brand: string[];
  accent: string[];
  secondary: string[];
  neutral: string[];
  arcoPrimary: string[]; // 10 × "r,g,b"，投给 --primary-1..10
  semantic: { success: string; warning: string; danger: string; info: string };
  seeds: { brand: string; accent: string; secondary: string };
}

// 语义色相区间（OKLCH H，度）：绿/黄橙/红/蓝，明度走同一曲线、饱和适中
const SEMANTIC_HUE = { success: 145, warning: 85, danger: 25, info: 255 };

/**
 * 由种子色 + 和谐模式 + 明暗档产出整套调色板。seedHex 非法返回 null。
 * 染色中性用种子色相、极低饱和；accent/secondary 用和谐偏移色相。
 */
export function buildTheme(seedHex: string, harmony: HarmonyMode, scheme: ThemeScheme): BuiltTheme | null {
  const seed = hexToOklch(seedHex);
  if (!seed) return null;
  const [dBrand, dAccent, dSecondary] = HARMONY_OFFSETS[harmony];

  const brand = scaleHex(seed.H, seed.L, seed.C, scheme);
  const accent = scaleHex((seed.H + dAccent + 360) % 360, seed.L, seed.C, scheme);
  const secondary = scaleHex((seed.H + dSecondary + 360) % 360, seed.L, seed.C, scheme);
  // 染色中性：色相取种子、饱和压到极低（0.012），带一点主题色温；与 brand 同一明度曲线
  const neutral = scaleL(seed.H, seed.L, 0.012, scheme).map((c) => rgbToHex(oklchToRgb(c)));

  const baseLight = scheme === "light";
  const sem = (h: number): string =>
    rgbToHex(oklchToRgb({ L: baseLight ? 0.6 : 0.72, C: 0.155, H: h }));

  return {
    brand,
    accent,
    secondary,
    neutral,
    arcoPrimary: brand.map((hex) => {
      const rgb = hexToRgb(hex) as Rgb;
      return rgbToTriplet(rgb);
    }),
    semantic: {
      success: sem(SEMANTIC_HUE.success),
      warning: sem(SEMANTIC_HUE.warning),
      danger: sem(SEMANTIC_HUE.danger),
      info: sem(SEMANTIC_HUE.info),
    },
    seeds: {
      brand: brand[5],
      accent: accent[5],
      secondary: secondary[5],
    },
  };
}

/** 向后兼容/单测：生成 10 档 brand 标度的 RGB 三元组（等价 arcoPrimary）。非法种子返回空数组。 */
export function generateRamp(baseHex: string, scheme: ThemeScheme = "light"): string[] {
  const t = buildTheme(baseHex, DEFAULT_HARMONY, scheme);
  return t ? t.arcoPrimary : [];
}
