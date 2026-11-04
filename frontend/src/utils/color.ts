/**
 * 色彩空间与调色基础：sRGB ↔ OKLCH 互转 + 色域内 gamut-map。
 *
 * 为什么用 OKLCH 而不是 Arco 的线性混白/混黑：OKLab/OKLCH 是感知均匀色空间
 * （Björn Ottosson），等量 L 变化在人眼里等亮，色相 H 与饱和 C 解耦，因此由单个种子色
 * 生成的明度阶不发灰、不串色，多色和谐（按 H 旋转）也更可控。这是我们自己的搭配方式的底座。
 *
 * 约定：
 *  - Rgb 通道 0..255；linear 通道 0..1；Oklch { L:0..1, C:0..≈0.4, H:0..360(度) }。
 *  - 全是纯函数，不碰 DOM，可脱离浏览器用 node:test 直接验证。
 *  - oklchToRgb 默认做 gamut-map：保 L/H 不变，二分降 C 直到落进 sRGB 色域，绝不溢出/串色。
 */

export type Rgb = { r: number; g: number; b: number };
export type Oklch = { L: number; C: number; H: number };

/** #rgb / #rrggbb → Rgb；带不带前导 # 均可，非法输入返回 null（取色器实时调用，绝不抛异常） */
export function hexToRgb(hex: string): Rgb | null {
  const s = String(hex ?? "").trim().replace(/^#/, "");
  let full = s;
  if (s.length === 3) full = s.split("").map((c) => c + c).join("");
  if (full.length !== 6 || /[^0-9a-fA-F]/.test(full)) return null;
  const n = parseInt(full, 16);
  return { r: (n >> 16) & 255, g: (n >> 8) & 255, b: n & 255 };
}

/** Rgb → #rrggbb（小写带 #） */
export function rgbToHex({ r, g, b }: Rgb): string {
  const h = (v: number) => Math.max(0, Math.min(255, Math.round(v))).toString(16).padStart(2, "0");
  return `#${h(r)}${h(g)}${h(b)}`;
}

/** Rgb → Arco 的 CSS 三元组字符串 "r,g,b"（--primary-N 落点格式） */
export function rgbToTriplet(c: Rgb): string {
  return `${c.r},${c.g},${c.b}`;
}

const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, v));

// sRGB 分量 gamma（0..255 → 0..1 线性）
function srgbToLinear(c8: number): number {
  const c = c8 / 255;
  return c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
}

// 线性（0..1）→ sRGB 分量（0..255，四舍五入并夹取）
function linearToSrgb8(c: number): number {
  const v = c <= 0.0031308 ? c * 12.92 : 1.055 * Math.pow(c, 1 / 2.4) - 0.055;
  return Math.round(clamp(v, 0, 1) * 255);
}

/** sRGB → OKLCH */
export function rgbToOklch({ r, g, b }: Rgb): Oklch {
  const lr = srgbToLinear(r);
  const lg = srgbToLinear(g);
  const lb = srgbToLinear(b);
  // 线性 RGB → 锥状 LMS（Ottosson 矩阵）
  const l = Math.cbrt(0.4122214708 * lr + 0.5363325363 * lg + 0.0514459929 * lb);
  const m = Math.cbrt(0.2119034982 * lr + 0.6806995451 * lg + 0.1073969566 * lb);
  const s = Math.cbrt(0.0883024619 * lr + 0.2817188376 * lg + 0.6299787005 * lb);
  const L = 0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s;
  const A = 1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s;
  const B = 0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s;
  const C = Math.hypot(A, B);
  let H = (Math.atan2(B, A) * 180) / Math.PI;
  if (H < 0) H += 360;
  return { L, C, H };
}

// OKLCH → 线性 RGB（可能越界，交由调用方判色域）
function oklchToLinear(L: number, C: number, H: number): [number, number, number] {
  const hr = (H * Math.PI) / 180;
  const A = C * Math.cos(hr);
  const B = C * Math.sin(hr);
  const l = Math.pow(L + 0.3963377774 * A + 0.2158037573 * B, 3);
  const m = Math.pow(L - 0.1055613458 * A - 0.0638541728 * B, 3);
  const s = Math.pow(L - 0.0894841775 * A - 1.291485548 * B, 3);
  return [
    4.076741639 * l - 3.307711591 * m + 0.2309699287 * s, // r
    -1.268438004 * l + 2.609757401 * m - 0.3413193965 * s, // g
    -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s, // b
  ];
}

function inGamut(lin: [number, number, number], eps: number): boolean {
  return lin.every((v) => v >= -eps && v <= 1 + eps);
}

/**
 * OKLCH → Rgb（0..255）。默认 gamutMap=true：保 L/H 不变，二分降 C 直到落进 sRGB，
 * 最终把线性值夹到 [0,1] 再编码。非法/极端输入一律返回安全夹取值，绝不 NaN。
 */
export function oklchToRgb(c: Oklch, gamutMap = true): Rgb {
  const L = clamp(c.L, 0, 1);
  const H = ((c.H % 360) + 360) % 360;
  let C = Math.max(0, c.C);
  if (gamutMap) {
    const eps = 1e-3;
    if (!inGamut(oklchToLinear(L, C, H), eps)) {
      let lo = 0;
      let hi = C;
      for (let i = 0; i < 24; i++) {
        const mid = (lo + hi) / 2;
        if (inGamut(oklchToLinear(L, mid, H), eps)) lo = mid;
        else hi = mid;
      }
      C = lo;
    }
  }
  const [lr, lg, lb] = oklchToLinear(L, C, H);
  return { r: linearToSrgb8(lr), g: linearToSrgb8(lg), b: linearToSrgb8(lb) };
}

/** hex → OKLCH；非法返回 null */
export function hexToOklch(hex: string): Oklch | null {
  const rgb = hexToRgb(hex);
  return rgb ? rgbToOklch(rgb) : null;
}

/** OKLCH → hex（含 gamut-map） */
export function oklchToHex(c: Oklch, gamutMap = true): string {
  return rgbToHex(oklchToRgb(c, gamutMap));
}
