/**
 * 色彩引擎单测（color.ts + palette.ts 纯函数）。用 Node 20+/24 内置的 node:test + node:assert 直接跑，
 * 不引入 vitest 等测试框架（本仓库前端此前无任何测试运行器，避免为纯函数拖进整套工具链）。
 * 运行： cd frontend && node --test "tests/*.test.ts"   （Node 原生剥离类型，import 用完整 .ts 路径）
 */
import { test } from "node:test";
import assert from "node:assert/strict";

import { hexToRgb, rgbToHex, rgbToTriplet, hexToOklch, oklchToHex } from "../src/utils/color.ts";
import {
  generateRamp,
  buildTheme,
  normalizeHarmony,
  HARMONY_OFFSETS,
} from "../src/utils/palette.ts";

const near = (a: number, b: number, tol: number) => Math.abs(a - b) <= tol;

test("hexToRgb：6 位 / 3 位 / 无 # / 非法", () => {
  assert.deepEqual(hexToRgb("#165dff"), { r: 22, g: 93, b: 255 });
  assert.deepEqual(hexToRgb("f80"), { r: 255, g: 136, b: 0 });
  assert.equal(hexToRgb("#12g456"), null);
  assert.equal(hexToRgb(""), null);
});

test("sRGB ↔ OKLCH 往返稳定（合法色回得到自身，容差内）", () => {
  const back = oklchToHex(hexToOklch("#165dff")!);
  const a = hexToRgb("#165dff")!;
  const b = hexToRgb(back)!;
  assert.ok(near(a.r, b.r, 2) && near(a.g, b.g, 2) && near(a.b, b.b, 2), `${back} 偏离种子`);
});

test("generateRamp：十档、第 6 档≈种子基准、非法返回空", () => {
  const light = generateRamp("#165dff", "light");
  assert.equal(light.length, 10);
  // arcoPrimary 是 "r,g,b" 三元组
  const [br, bg, bb] = light[5].split(",").map(Number);
  assert.ok(near(br, 22, 2) && near(bg, 93, 2) && near(bb, 255, 2), `基准 ${light[5]}`);
  assert.deepEqual(generateRamp("nope", "light"), []);
});

test("明度单调：light 递减、dark 递增（按 OKLCH L）", () => {
  const t = buildTheme("#165dff", "analogous", "light")!;
  const Ls = t.brand.map((h) => hexToOklch(h)!.L);
  for (let i = 1; i < 10; i++) assert.ok(Ls[i] <= Ls[i - 1] + 1e-3, `light 非单调 @${i}`);
  const td = buildTheme("#165dff", "analogous", "dark")!;
  const Ld = td.brand.map((h) => hexToOklch(h)!.L);
  for (let i = 1; i < 10; i++) assert.ok(Ld[i] >= Ld[i - 1] - 1e-3, `dark 非单调 @${i}`);
});

test("和谐色相偏移：accent/secondary 相对 brand 按模式旋转", () => {
  for (const mode of ["analogous", "complementary", "split", "triadic"] as const) {
    const t = buildTheme("#165dff", mode, "light")!;
    const hB = hexToOklch(t.seeds.brand)!.H;
    const hA = hexToOklch(t.seeds.accent)!.H;
    const [offA, offS] = [HARMONY_OFFSETS[mode][1], HARMONY_OFFSETS[mode][2]];
    const delta = (x: number) => (((x + 540) % 360) - 180); // 归一到 [-180,180)
    assert.ok(Math.abs(delta(hA - hB) - delta(offA)) <= 6, `${mode} accent 色相偏 ${hA - hB}≠${offA}`);
    const hS = hexToOklch(t.seeds.secondary)!.H;
    assert.ok(Math.abs(delta(hS - hB) - delta(offS)) <= 6, `${mode} secondary 色相偏 ${hS - hB}≠${offS}`);
  }
});

test("染色中性：色相≈种子、饱和极低", () => {
  const t = buildTheme("#165dff", "analogous", "light")!;
  const mid = hexToOklch(t.neutral[5])!;
  const seed = hexToOklch("#165dff")!;
  assert.ok(mid.C < 0.05, `中性饱和过高 ${mid.C}`);
  const dd = (((mid.H - seed.H + 540) % 360) - 180);
  assert.ok(Math.abs(dd) < 8, `中性色相串了 ${dd}`);
});

test("normalizeHarmony：非法/空回落 analogous", () => {
  assert.equal(normalizeHarmony("triadic"), "triadic");
  assert.equal(normalizeHarmony("bogus"), "analogous");
  assert.equal(normalizeHarmony(""), "analogous");
  assert.equal(normalizeHarmony(undefined), "analogous");
});

test("rgbToTriplet / rgbToHex 格式", () => {
  assert.equal(rgbToTriplet({ r: 22, g: 93, b: 255 }), "22,93,255");
  assert.equal(rgbToHex({ r: 255, g: 136, b: 0 }), "#ff8800");
});
