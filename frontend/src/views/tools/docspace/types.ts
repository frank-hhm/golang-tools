/** 文档中心前端共享类型（与 Go tools/docspace 的 JSON tag 对齐） */

export interface Category {
  id: string;
  name: string;
  parent?: string;
  sort: number;
}

export interface DocItem {
  id: string;
  src: string;
  sid?: string;
  path: string;
  name: string;
  category?: string;
  tags?: string[];
  version: number;
  createdAt: number;
  updatedAt: number;
}

export interface Version {
  n: number;
  file: string;
  size: number;
  sha256: string;
  note: string;
  srcMtime: number;
  createdAt: number;
}

export interface TrashEntry {
  id: string;
  name: string;
  origin: string;
  deletedAt: number;
  src?: string;
  relPath?: string;
  sid?: string;
  category?: string;
}

export interface Counts {
  categories: number;
  items: number;
  trash: number;
  secure: number;
  shares: number;
  backups: number;
}

/**
 * 条目派生元数据（与 Go docspace.ItemMeta 对齐）。
 * 不落盘、每次列表现取：kind/size/mtime 是实体文件的派生事实，
 * 写进条目 JSON 就会与磁盘脱节。unreachable 为真表示取不到实体（已删除/会话已关），
 * 此时 size/modTime 都是 0，前端显「实体缺失」而不是 0 字节。
 */
export interface ItemMeta {
  id: string;
  kind: string;
  mime: string;
  ext: string;
  size: number;
  modTime: number;
  unreachable: boolean;
  thumbable: boolean;
  /** 缩略图端点完整 URL，由绑定层补齐；非图片/取不到引用时为空 */
  thumb?: string;
}

/** 类型计数（与 Go docspace.KindCount 对齐）；label 由后端给，前后端只一份名称表 */
export interface KindCount {
  kind: string;
  label: string;
  num: number;
  size: number;
}

/** 分类计数；inclusive 含子级，用于左树徽标 */
export interface CategoryCount {
  id: string;
  name: string;
  num: number;
  inclusive: number;
  size: number;
}

export interface SourceCount {
  src: string;
  num: number;
  size: number;
}

export interface ExtCount {
  ext: string;
  num: number;
}

/** 总览数字（与 Go docspace.Stats 对齐） */
export interface Stats {
  total: number;
  totalSize: number;
  byKind: Record<string, number>;
  kindSize: Record<string, number>;
  missing: number;
  updated7d: number;
  withTags: number;
  withVersions: number;
}

/** 一次总览：统计卡 + 类型 chips + 分类徽标 + 源分布都来自这里 */
export interface Overview {
  stats: Stats;
  kinds?: KindCount[];
  categories?: CategoryCount[];
  sources?: SourceCount[];
  topExts?: ExtCount[];
}

/** 批量动作回执（与 Go docspace.BatchResult 对齐） */
export interface BatchResult {
  done: number;
  failed?: string[];
  reason?: string;
}

/**
 * 列表行 = 条目 + 派生元数据摊平。
 * 摊平而不是嵌套，是为了让表格列直接 data-index 取值，同时又能整行传给
 * 只认 DocItem 的既有弹窗（标签/版本/分享）。
 */
export interface DocRow extends DocItem {
  kind: string;
  kindLabel: string;
  ext: string;
  mime: string;
  size: number;
  modTime: number;
  missing: boolean;
  thumbable: boolean;
  thumb: string;
  /** 元数据是否已取到；false 时体积/时间显占位而不是 0 */
  hasMeta: boolean;
}

/** 分享弹窗所需最小字段：磁盘页与文档中心都能直接传，不绑定 DocItem */
export interface ShareItem {
  src?: string;
  path: string;
  name?: string;
}

/** 智能搜索条件（与 Go docspace.SearchFilter 对齐）。零值 = 该条件不启用。 */
export interface SearchFilter {
  keyword: string;
  category: string;
  excludeSubCats: boolean;
  tags: string[];
  hasVersions: boolean;
  updatedWithinDays: number;
  minSizeBytes: number;
  maxSizeBytes: number;
  limit: number;
}

/** 磁盘命中（与 tools/disk Entry 对齐，只取展示用到的字段） */
export interface DiskEntry {
  name: string;
  path: string;
  isDir: boolean;
  size: number;
  modTime: string;
  ext?: string;
}

/** 磁盘内容命中（与 tools/disk Match 对齐） */
export interface DiskMatch {
  path: string;
  name: string;
  line: number;
  text: string;
  size: number;
}

/** 备份计划（与 Go docspace.BackupPlan 对齐）。本期仅手动「立即备份」，intervalSec/enabled 为调度预留。 */
export interface BackupEntry {
  id: string;
  name: string;
  src: string;
  dst: string;
  intervalSec: number;
  enabled: boolean;
  lastRunAt: number;
  lastStatus: string;
  copied: number;
  skipped: number;
}

/** 加密保险库条目（与 Go docspace.SecureEntry 对齐）。salt/check 是校验用派生值，非口令。 */
export interface SecureEntry {
  id: string;
  path: string;
  salt?: string;
  check?: string;
  createdAt: number;
}

/** 从路径取文件名（兼容 \ 与 /），仅展示用 */
export const baseName = (p: string): string => {
  const seg = String(p ?? "").split(/[\\/]+/).filter(Boolean);
  return seg.length ? seg[seg.length - 1] : p || "--";
};

/** 统一从 $GoTools 代理错误里取文案 */
export const errText = (e: any): string => String(e?.message ?? e ?? "操作失败");

/** 毫秒时间戳格式化；空/0 显示占位 */
export const fmtTime = (ms: number): string => {
  if (!ms) return "--";
  try {
    return new Date(ms).toLocaleString();
  } catch {
    return String(ms);
  }
};

/** 字节数 → "12.3 MB"；<=0 显 "0 B"（空文件确实是 0，不该显 - ） */
export function fmtSize(n?: number): string {
  if (!Number.isFinite(n) || (n as number) <= 0) return "0 B";
  const unit = ["B", "KB", "MB", "GB", "TB", "PB"];
  let v = n as number;
  let i = 0;
  while (v >= 1024 && i < unit.length - 1) {
    v /= 1024;
    i++;
  }
  return `${i === 0 ? v : v.toFixed(1)} ${unit[i]}`;
}

/** 相对时间：卡片上「3 天前」比完整时间戳更好扫读 */
export function fmtAgo(ms?: number): string {
  if (!ms) return "--";
  const diff = Date.now() - ms;
  if (diff < 0) return "刚刚";
  const min = Math.floor(diff / 60000);
  if (min < 1) return "刚刚";
  if (min < 60) return `${min} 分钟前`;
  const hour = Math.floor(min / 60);
  if (hour < 24) return `${hour} 小时前`;
  const day = Math.floor(hour / 24);
  if (day < 30) return `${day} 天前`;
  const mon = Math.floor(day / 30);
  if (mon < 12) return `${mon} 个月前`;
  return `${Math.floor(mon / 12)} 年前`;
}
