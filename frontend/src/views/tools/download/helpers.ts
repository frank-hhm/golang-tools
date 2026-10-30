/**
 * 下载管理页专用格式化与展示口径：字节 / 速度 / 状态文案 / 分类与状态分组。
 *
 * 纯前端、无后端调用；数据来自 tools/downloader 引擎经绑定层发来的 downloader.Task。
 * 与 common-task-progress 的 status 对齐靠 @/utils/taskProgress 的 normalizeStatus，
 * 这里只补下载特有的展示细节（速度带单位、分块区间、分类树与状态筛选）。
 */
import type { Task } from "bindings/Tools/tools/downloader/index";
import { etaFromBytes, statusLabel, statusTagColor } from "@/utils/taskProgress";

// 状态文案 / 标签色已下沉到 @/utils/taskProgress，与主页下载抽屉同一口径；
// 本页按原名再导出，既有 import（index.vue）零改动。
export { statusLabel, statusTagColor };

/** 分类树固定项（与引擎 categoryByExt 的取值一一对应，多一个"全部"聚合）。
 * "安装"为安装类下载专用桶：mysql/redis/php/nginx/npm 版本包、php 扩展包、docker 安装器
 * 经 downloader.Fetch 落盘时统一传 Category="安装"，故单列一类便于与用户手动下载区分。 */
export const CATEGORIES = ["全部", "安装", "压缩", "文档", "音乐", "程序", "视频", "图片", "其他"] as const;

/**
 * 分类 → 图标：统一交给公共组件 common-file-icon 渲染（Windows 11 风格按类型上色的纸片/文件夹）。
 * 全部=文件夹；其余取一个代表扩展名，色条即该类型的 Windows 语义色；其他=空白纸。
 */
export const CATEGORY_ICON: Record<string, { dir?: boolean; ext?: string }> = {
  全部: { dir: true },
  安装: { ext: "exe" },
  压缩: { ext: "zip" },
  文档: { ext: "doc" },
  音乐: { ext: "mp3" },
  程序: { ext: "exe" },
  视频: { ext: "mp4" },
  图片: { ext: "png" },
  其他: { ext: "" },
};

/** 状态分组（横向切面，与分类正交）：key 用于筛选，label 用于展示 */
export const STATE_GROUPS = [
  { key: "active", label: "未完成" },
  { key: "queue", label: "队列中" },
  { key: "done", label: "已完成" },
  { key: "failed", label: "失败 / 已取消" },
] as const;

/** 字节数 → "12.3 MB"；0 / 负 / 非有限值统一显 "0 B"（与引擎 size 缺省口径一致） */
export function fmtBytes(n?: number): string {
  if (!Number.isFinite(n) || (n as number) <= 0) return "0 B";
  const u = ["B", "KB", "MB", "GB", "TB", "PB"];
  let v = n as number;
  let i = 0;
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
  return (i === 0 ? v.toFixed(0) : v.toFixed(1)) + " " + u[i];
}

/** 字节/秒 → "1.2 MB/s"；≤0 或未知显 "—"（限速档位展示也复用它） */
export function fmtSpeed(bytesPerSec?: number): string {
  if (!Number.isFinite(bytesPerSec) || (bytesPerSec as number) <= 0) return "—";
  return fmtBytes(bytesPerSec) + "/s";
}

/** 任务百分比：done 恒 100，其余取引擎 percent 并钳 0–100 */
export function pctOf(t?: Task): number {
  if (!t) return 0;
  if (t.status === "done") return 100;
  return Math.max(0, Math.min(100, Math.round(t.percent || 0)));
}

/** "已下 x / 共 y"：总长未知（size<=0）时只说已下量，不编一个假总数 */
export function progressText(t?: Task): string {
  if (!t) return "";
  const dl = fmtBytes(t.downloaded);
  if (!t.size || t.size <= 0) return `已下 ${dl}`;
  return `已下 ${dl} / 共 ${fmtBytes(t.size)}`;
}

/** 单任务底部 ETA：仅下载中给（其余相位不猜剩余时间） */
export function etaOf(t?: Task): string {
  if (!t || t.status !== "running") return "";
  return etaFromBytes(t.downloaded, t.size, t.speed);
}

/** 限速档位（bps）：与工具条下拉共用；0=不限 */
export const SPEED_PRESETS = [
  { label: "不限速", bps: 0 },
  { label: "512 KB/s", bps: 512 * 1024 },
  { label: "1 MB/s", bps: 1024 * 1024 },
  { label: "5 MB/s", bps: 5 * 1024 * 1024 },
  { label: "10 MB/s", bps: 10 * 1024 * 1024 },
];

/** 分块闭区间起末 → "12.3 MB ~ 45.6 MB"（end<0 表示未定长尾块） */
export function partRange(start: number, end: number): string {
  if (end < 0) return `${fmtBytes(start)} ~ 未定`;
  return `${fmtBytes(start)} ~ ${fmtBytes(end)}`;
}

/** 单个分块的百分比：按该块 [start,end] 已下量算；未定长块返回 0 */
export function partPct(start: number, end: number, done: number): number {
  if (end < 0) return 0;
  const span = end - start + 1;
  if (span <= 0) return 0;
  return Math.max(0, Math.min(100, Math.round((done / span) * 100)));
}

/**
 * 状态分组筛选：给定分类树选中的分类 + 状态分组 key，判断任务是否入列。
 * cat 为"全部"不筛分类；group 为空不筛状态。
 */
export function matchFilter(t: Task, cat: string, group: string): boolean {
  if (cat && cat !== "全部" && t.category !== cat) return false;
  switch (group) {
    case "active":
      return t.status !== "done" && t.status !== "canceled";
    case "queue":
      return t.status === "queued" || t.status === "pending";
    case "done":
      return t.status === "done";
    case "failed":
      return t.status === "error" || t.status === "canceled";
    default:
      return true;
  }
}
