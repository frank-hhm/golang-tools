/**
 * 磁盘管理页共用的小工具：容量格式化、盘符类型文案、用量色阶。
 *
 * 总览卡与文件列表都要用，放这里各写一份的话两边迟早对不上。
 */

/** 字节数 → "12.3 GB"；0 显示 "0 B"（空目录确实是 0，不该显示成 - ） */
export function fmtSize(n: number): string {
    if (!Number.isFinite(n) || n <= 0) return "0 B";
    const unit = ["B", "KB", "MB", "GB", "TB", "PB"];
    let v = n;
    let i = 0;
    while (v >= 1024 && i < unit.length - 1) {
        v /= 1024;
        i++;
    }
    return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${unit[i]}`;
}

/** 盘符类型（GetDriveTypeW 的取值）→ 中文 */
export const DRIVE_KIND: Record<string, string> = {
    fixed: "本地磁盘",
    removable: "可移动磁盘",
    network: "网络驱动器",
    cdrom: "光驱",
    ram: "内存盘",
    unknown: "未知类型",
};

/** 用量色阶：<70 绿、<90 橙、其余红，与首页磁盘指标同一套语义色 */
export function usageTone(pct: number): "green" | "orange" | "red" {
    if (pct >= 90) return "red";
    if (pct >= 70) return "orange";
    return "green";
}

/** 路径末段名（"C:\\a\\b" → "b"），提示文案里用短名更省地方 */
export function baseName(p: string): string {
    const s = String(p || "").replace(/[\\/]+$/, "");
    const i = Math.max(s.lastIndexOf("\\"), s.lastIndexOf("/"));
    return i >= 0 ? s.slice(i + 1) : s;
}

/** 路径所属盘符根（"C:\\a\\b" → "C:\\"）；无盘符时返回空串 */
export function driveRootOf(p: string): string {
    const m = /^([A-Za-z]:)[\\/]/.exec(String(p || ""));
    return m ? m[1].toUpperCase() + "\\" : "";
}

/** 文件扩展名 → 表格里的小图标名（Arco 图标组件名，模板里 withIcon 拼出来） */
export function extIcon(ext: string): string {
    const e = (ext || "").toLowerCase();
    if (["png", "jpg", "jpeg", "gif", "bmp", "ico", "webp", "svg"].includes(e)) return "file-image";
    if (["mp3", "wav", "flac", "aac", "ogg"].includes(e)) return "file-audio";
    if (["mp4", "mkv", "avi", "mov", "flv", "webm"].includes(e)) return "file-video";
    if (e === "pdf") return "file-pdf";
    if (["zip", "rar", "7z", "gz", "tar", "bz2"].includes(e)) return "archive";
    if (["go", "ts", "js", "vue", "py", "java", "c", "cpp", "h", "rs", "sh", "ps1", "json", "yml", "yaml", "xml", "html", "css", "scss"].includes(e))
        return "code";
    return "file";
}
