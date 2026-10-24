import iconData from "@/components/select-modal/icon/icon.json";

interface IconfontItem {
    name?: string;
    font_class?: string;
}

const ICONFONT_ITEMS = (iconData as IconfontItem[]).filter((i) => !!i.font_class);
const ICONFONT_CLASSES = new Set(ICONFONT_ITEMS.map((i) => String(i.font_class)));

/** 从 icon.json 取字体图标值（统一为 icon-xxx）；不存在时返回空串 */
export function iconfontIcon(fontClass: string): string {
    const cls = String(fontClass || "").trim().replace(/^icon-/, "");
    return cls && ICONFONT_CLASSES.has(cls) ? `icon-${cls}` : "";
}

/** 图标值是否来自图标库（icon.json 的 font_class） */
export function isIconfontIcon(icon?: string): boolean {
    return !!icon && String(icon).startsWith("icon-");
}

/** 图标值 → 可读名称：图标库取 icon.json 的中文名，Arco 直接返回图标名 */
export function iconLabel(icon?: string): string {
    const val = String(icon || "");
    if (!val) return "";
    if (!isIconfontIcon(val)) return val;
    const cls = val.slice(5);
    const hit = ICONFONT_ITEMS.find((i) => i.font_class === cls);
    return hit?.name ? String(hit.name) : cls;
}

/** Arco 图标名（camera / clock-circle）→ 全局组件名（IconCamera / IconClockCircle） */
export function arcoIconComp(icon?: string): string | null {
    if (!icon || isIconfontIcon(icon)) return null;
    const val = String(icon);
    if (/^Icon[A-Z]/.test(val)) return val;
    const pascal = val
        .split(/[-_\s]+/)
        .filter(Boolean)
        .map((s) => s[0].toUpperCase() + s.slice(1))
        .join("");
    return pascal ? "Icon" + pascal : null;
}

/**
 * 功能列表默认图标：结合 Arco 图标与 icon.json 图标库。
 * Arco 直接用图标名（如 camera），图标库用 iconfontIcon 取值（如 icon-lianjie）。
 */
export const DEFAULT_TOOL_ICONS: Record<string, string> = {
    screenshot: "camera",
    colorpicker: "icon-mofa",
    video: "video-camera",
    qrcode: iconfontIcon("erweima3") || "qrcode",
    timestamp: "clock-circle",
    strrandom: "icon-yanfa",
    encdec: "lock",
    hexconvert: "swap",
    imageBase64: iconfontIcon("img") || "image",
    sniffer: "wifi",
    websocket: iconfontIcon("lianjie") || "link",
    usbserial: iconfontIcon("xinpian") || "drive-file",
    redis: "icon-icon-redis",
    mysql: "icon-MySQL",
    php: "icon-php",
    nginx: "icon-ngnix",
    npm: "icon-npm",
    settings: "settings",
};

/** 旧版默认数据里 Arco 不存在的图标 → 当前默认图标（避免历史配置显示空白） */
const LEGACY_TOOL_ICONS: Record<string, string> = {
    "color-picker": DEFAULT_TOOL_ICONS.colorpicker,
    random: DEFAULT_TOOL_ICONS.strrandom,
    connection: DEFAULT_TOOL_ICONS.websocket,
    usb: DEFAULT_TOOL_ICONS.usbserial,
};

/** 规范工具图标：修复旧默认值，兼容 Arco 名称与 icon-xxx 两种格式 */
export function normalizeToolIcon(icon?: string): string {
    const val = String(icon || "").trim();
    if (!val) return "";
    return LEGACY_TOOL_ICONS[val] || val;
}
