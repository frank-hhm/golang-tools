<template>
    <!-- Windows 11 风格的文件/目录图标：纯内联 SVG，无外部图片资源，列表(小)与网格(大)都清晰。
         目录 = 金色双色文件夹（后板 + 前袋）；文件 = 折角白纸 + 按扩展名上色的类型色条（条上写扩展名）。
         色条取 Windows「按类型着色」的语义（Word 蓝 / Excel 绿 / PDF 红 / 压缩 琥珀…），比原来的单色线性图标更直观。 -->
    <svg :width="px" :height="px" viewBox="0 0 48 48" class="w11-icon" role="img" :aria-label="ariaLabel">
        <template v-if="isDir">
            <!-- 后板（含左上标签凸出），深一档金，压出层次 -->
            <path d="M4 12c0-1.66 1.34-3 3-3h8.2c.9 0 1.74.45 2.24 1.2L19.4 12H41c1.66 0 3 1.34 3 3v3H4v-6z"
                fill="#E0A22E" />
            <!-- 前袋（主体），亮一档金，顶边留出与后板的分界 -->
            <path d="M4 16.5h40V36c0 1.66-1.34 3-3 3H7c-1.66 0-3-1.34-3-3V16.5z" fill="#F6C453" />
        </template>

        <template v-else>
            <!-- 纸张：左上到右上，右上角折角 -->
            <path d="M11 6h16l8 8v23a2 2 0 0 1-2 2H11a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2z" fill="#FCFCFD"
                stroke="#C4C8CF" stroke-width="1" />
            <!-- 折角三角 -->
            <path d="M27 6l8 8h-6a2 2 0 0 1-2-2V6z" fill="#DFE3E8" stroke="#C4C8CF" stroke-width="1" />
            <!-- 有扩展名才画色条 + 文字；无扩展名就是一张空白纸 -->
            <template v-if="extLabel">
                <rect x="9" y="24.5" width="26" height="12.5" rx="2" :fill="accent" />
                <text x="22" y="33.4" text-anchor="middle" font-size="8" font-weight="700" fill="#FFFFFF"
                    font-family="'Segoe UI', system-ui, -apple-system, sans-serif">{{ extLabel }}</text>
            </template>
        </template>
    </svg>
</template>

<script lang="ts" setup>
/**
 * Windows 11 风格文件/目录图标。
 * 只做「按类型上色 + 扩展名文字」的静态呈现，不依赖任何图片资源，方便深浅色主题与任意尺寸复用。
 */
import { computed } from 'vue';

const props = withDefaults(defineProps<{
    /** 是否目录（画文件夹） */
    isDir?: boolean;
    /** 文件扩展名（不带点），目录可省略 */
    ext?: string;
    /** 像素尺寸：列表里 18，网格视图放大到 40 左右 */
    size?: number | string;
}>(), {
    isDir: false,
    ext: '',
    size: 18,
});

const px = computed(() => {
    const n = typeof props.size === 'number' ? props.size : parseFloat(String(props.size));
    return Number.isFinite(n) && n > 0 ? n : 18;
});

// 扩展名 → 类型色（对齐 Windows 常见应用的品牌色）。同一类别多个扩展名共用一色。
const KIND_COLORS: Record<string, string> = {
    image: '#2AA96B',
    audio: '#8A5CF6',
    video: '#E5484D',
    pdf: '#D93A2B',
    doc: '#2B5CE6',
    sheet: '#1A8A4C',
    slides: '#D9642B',
    archive: '#B7791F',
    text: '#5B6472',
    app: '#4B5563',
    code: '#2B7DE9',
};
const KIND_OF_EXT: Record<string, string> = {};
const KIND_GROUPS: Record<string, string[]> = {
    image: ['png', 'jpg', 'jpeg', 'gif', 'bmp', 'ico', 'webp', 'svg', 'tiff', 'heic'],
    audio: ['mp3', 'wav', 'flac', 'aac', 'ogg', 'm4a', 'mid'],
    video: ['mp4', 'mkv', 'avi', 'mov', 'flv', 'webm', 'wmv', 'm4v'],
    pdf: ['pdf'],
    doc: ['doc', 'docx', 'rtf', 'odt', 'wps'],
    sheet: ['xls', 'xlsx', 'csv', 'ods', 'et'],
    slides: ['ppt', 'pptx', 'pps', 'odp', 'dps'],
    archive: ['zip', 'rar', '7z', 'gz', 'tar', 'bz2', 'xz', 'tgz'],
    text: ['txt', 'md', 'log', 'ini', 'conf', 'properties', 'env'],
    app: ['exe', 'msi', 'apk', 'dmg', 'app', 'jar'],
    code: ['js', 'ts', 'jsx', 'tsx', 'vue', 'go', 'py', 'java', 'c', 'cpp', 'h', 'rs', 'sh', 'ps1', 'bat',
        'json', 'yml', 'yaml', 'xml', 'html', 'htm', 'css', 'scss', 'less', 'sql', 'php'],
};
for (const [kind, list] of Object.entries(KIND_GROUPS)) {
    for (const e of list) KIND_OF_EXT[e] = kind;
}

const normExt = computed(() => (props.ext || '').toLowerCase().replace(/^\./, ''));

const accent = computed(() => {
    const kind = KIND_OF_EXT[normExt.value];
    return kind ? KIND_COLORS[kind] : '#8A9099';
});

// 色条最多放 4 个字符的扩展名（够覆盖 JPEG/DOCX/PPTX 等常见项）
const extLabel = computed(() => normExt.value.slice(0, 4).toUpperCase());

const ariaLabel = computed(() => (props.isDir ? '文件夹' : extLabel.value ? extLabel.value + ' 文件' : '文件'));
</script>

<style scoped>
.w11-icon {
    display: block;
    flex: none;
    /* 折角色条之外都是矢量描边，缩到 18px 仍利落 */
    shape-rendering: geometricPrecision;
}
</style>
