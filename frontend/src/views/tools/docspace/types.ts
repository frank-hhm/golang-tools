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
