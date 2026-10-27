<template>
    <!-- 磁盘总览：版式对齐 Docker 总览（区块用 a-card，指标用首页同款 stat-card，
         下方卡片墙逐盘展示用量），数据全部来自父页面一次 DiskDrives 的结果 -->
    <a-card class="card-head-tool ov-card" size="small" :body-style="collapsed ? { display: 'none' } : undefined">
        <template #title>
            <span class="ov-title">磁盘总览</span>
            <span v-if="drives.length" class="ov-count">{{ drives.length }}</span>
        </template>
        <template #extra>
            <a-space :size="6">
                <a-tooltip content="重新读取各盘符容量">
                    <a-button size="small" :loading="loading" @click="emit('refresh')">
                        <template #icon><icon-refresh /></template>
                    </a-button>
                </a-tooltip>
                <a-tooltip :content="collapsed ? '展开磁盘总览' : '收起磁盘总览（给文件列表让高度）'">
                    <a-button size="small" @click="emit('toggle')">
                        <template #icon>
                            <icon-up v-if="!collapsed" />
                            <icon-down v-else />
                        </template>
                    </a-button>
                </a-tooltip>
            </a-space>
        </template>

        <a-row :gutter="[12, 12]">
            <a-col v-for="c in cards" :key="c.key" :xs="24" :sm="12" :md="8" :xl="4">
                <common-stat-card :icon="c.icon" :icon-value="c.p" :tone="c.tone" :label="c.title" :value="c.value"
                    :detail="c.sub" :tip="c.tip" :ready="c.ready" />
            </a-col>
        </a-row>

        <!-- 盘符墙：点卡片直接进该盘根目录 -->
        <div v-if="drives.length" class="drive-grid">
            <div v-for="d in drives" :key="d.root" class="drive"
                :class="{ active: activeRoot === d.root, 'is-off': !d.total }" @click="emit('jump', d.root)">
                <div class="drive-head">
                    <span class="drive-icon" :class="'tone-' + usageTone(d.usedPct)">
                        <common-stat-icon name="disk" :value="d.usedPct" :size="20" />
                    </span>
                    <div class="drive-title">
                        <div class="drive-name">{{ d.root }}</div>
                        <a-tooltip :content="`${d.label || '无卷标'} · ${d.fsType || '未知文件系统'} · ${kindText(d.kind)}`">
                            <div class="drive-sub">{{ kindText(d.kind) }}{{ d.label ? " · " + d.label : "" }}</div>
                        </a-tooltip>
                    </div>
                    <span class="drive-pct" :class="'text-' + usageTone(d.usedPct)">{{ pct(d.usedPct) }}%</span>
                </div>
                <div class="drive-bar">
                    <i :class="'tone-bg-' + usageTone(d.usedPct)" :style="{ width: pct(d.usedPct) + '%' }"></i>
                </div>
                <div class="drive-foot">
                    <span>已用 {{ fmtSize(d.used) }}</span>
                    <span>剩余 {{ fmtSize(d.free) }}</span>
                    <span>共 {{ fmtSize(d.total) }}</span>
                </div>
            </div>
        </div>
        <div v-else class="ov-empty">
            <a-empty :image="false" description="没有读到任何盘符" />
        </div>
    </a-card>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { DRIVE_KIND, fmtSize, usageTone } from "../helpers";

const props = withDefaults(defineProps<{
    /** DiskDrives 的结果：root/label/fsType/kind/total/free/used/usedPct */
    drives: any[];
    loading?: boolean;
    /** 当前正在浏览的路径，用来给所属盘符描边 */
    activeRoot?: string;
    /** 收起来时只留卡片头，文件列表能多占一屏 */
    collapsed?: boolean;
}>(), { loading: false, activeRoot: "", collapsed: false });

const emit = defineEmits<{
    (e: "refresh"): void;
    (e: "toggle"): void;
    (e: "jump", root: string): void;
}>();

const kindText = (k: string) => DRIVE_KIND[k] || k || "未知类型";
const pct = (v: number) => Math.min(100, Math.max(0, Number(v) || 0)).toFixed(0);

/** 五个指标：数量 / 总容量 / 已用 / 可用 / 最紧张 */
const cards = computed(() => {
    const list = props.drives || [];
    const total = list.reduce((n, d) => n + (d.total || 0), 0);
    const free = list.reduce((n, d) => n + (d.free || 0), 0);
    const used = Math.max(0, total - free);
    const p = total ? (used / total) * 100 : 0;
    const fixed = list.filter((d) => d.kind === "fixed").length;
    const removable = list.filter((d) => d.kind === "removable").length;
    const remote = list.filter((d) => d.kind === "network").length;
    // 最紧张：挑已用比例最高的盘，容量为 0（空光驱）的先把它们排除掉
    const tight = [...list].filter((d) => d.total > 0).sort((a, b) => (b.usedPct || 0) - (a.usedPct || 0))[0];
    return [
        {
            key: "count",
            title: "磁盘数量",
            value: String(list.length),
            sub: `本地 ${fixed} · 可移动 ${removable}${remote ? ` · 网络 ${remote}` : ""}`,
            ready: list.length > 0,
            icon: "disk",
            p: 0,
            tone: "blue",
            tip: "系统返回的逻辑盘符数量（含可移动盘与网络映射盘）",
        },
        {
            key: "total",
            title: "总容量",
            value: fmtSize(total),
            sub: `${list.length} 个盘符合计`,
            ready: total > 0,
            icon: "volume",
            p,
            tone: "cyan",
            tip: "所有可访问盘符的容量之和",
        },
        {
            key: "used",
            title: "已用空间",
            value: fmtSize(used),
            sub: `整体占用 ${p.toFixed(1)}%`,
            ready: total > 0,
            icon: "disk",
            p,
            tone: "orange",
            tip: "容量减去可用空间；回收站里的文件仍算已用",
        },
        {
            key: "free",
            title: "可用空间",
            value: fmtSize(free),
            sub: `剩余 ${(total ? 100 - p : 0).toFixed(1)}%`,
            ready: total > 0,
            icon: "mem",
            p: total ? 100 - p : 0,
            tone: "green",
            tip: "可直接写入的空间；空间告急时先清空回收站往往见效最快",
        },
        {
            key: "tight",
            title: "最紧张盘符",
            value: tight ? `${tight.root.replace(/\\$/, "")} ${pct(tight.usedPct)}%` : "—",
            sub: tight ? `${tight.label || tight.root} 剩余 ${fmtSize(tight.free)}` : "没有可用的盘符",
            ready: !!tight,
            icon: "cpu",
            p: tight ? tight.usedPct : 0,
            tone: "purple",
            tip: "已用比例最高的盘；到 100% 之前先把大文件挪到别的盘",
        },
    ];
});
</script>

<style scoped>
.ov-card {
    flex: none;
}

.ov-count {
    margin-left: 6px;
    padding: 0 6px;
    border-radius: 8px;
    background: var(--color-fill-2);
    color: var(--color-text-3);
    font-size: 11px;
    font-weight: normal;
    line-height: 16px;
    vertical-align: 1px;
}

.ov-title {
    font-size: var(--base-size-default);
    font-weight: 600;
}

/* 指标卡版式已收进 common-stat-card；以下 tone-* 仍供盘符墙 drive-icon 复用 */
.tone-blue {
    background: rgba(var(--arcoblue-6), 0.08);
    color: rgb(var(--arcoblue-6));
}

.tone-cyan {
    background: rgba(var(--cyan-6), 0.08);
    color: rgb(var(--cyan-6));
}

.tone-purple {
    background: rgba(var(--purple-6), 0.08);
    color: rgb(var(--purple-6));
}

.tone-orange {
    background: rgba(var(--orange-6), 0.08);
    color: rgb(var(--orange-6));
}

.tone-green {
    background: rgba(var(--green-6), 0.08);
    color: rgb(var(--green-6));
}

.tone-red {
    background: rgba(var(--red-6), 0.08);
    color: rgb(var(--red-6));
}

/* 盘符墙：同宽自适应，卡片少时靠左不拉伸 */
.drive-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
    gap: var(--base-margin);
    margin-top: var(--base-margin);
}

.drive {
    padding: var(--base-padding);
    border: 1px solid var(--color-border-1);
    border-radius: var(--base-radius-default);
    cursor: pointer;
    transition: box-shadow 0.2s, border-color 0.2s;
}

.drive:hover {
    box-shadow: var(--shadow-2-bottom);
    border-color: rgb(var(--primary-5));
}

/* 当前浏览的盘：主色描边，和列表里看到的内容对得上 */
.drive.active {
    border-color: rgb(var(--primary-6));
    box-shadow: 0 0 0 1px rgba(var(--primary-6), 0.16);
}

.drive.is-off {
    opacity: 0.62;
    border-style: dashed;
}

.drive-head {
    display: flex;
    align-items: center;
    gap: 8px;
}

.drive-icon {
    width: 34px;
    height: 34px;
    border-radius: var(--base-radius-small);
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
}

.drive-title {
    flex: 1;
    min-width: 0;
}

.drive-name {
    font-weight: 600;
    font-size: var(--base-size-default);
    line-height: 1.3;
    color: var(--color-text-1);
}

.drive-sub {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}

.drive-pct {
    font-size: var(--base-size-small);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    flex-shrink: 0;
}

.text-green {
    color: rgb(var(--green-6));
}

.text-orange {
    color: rgb(var(--orange-6));
}

.text-red {
    color: rgb(var(--red-6));
}

.drive-bar {
    height: 4px;
    margin: 8px 0 6px;
    border-radius: 2px;
    background: var(--color-fill-2);
    overflow: hidden;
}

.drive-bar > i {
    display: block;
    height: 100%;
    border-radius: 2px;
    transition: width 0.4s ease;
}

.tone-bg-green {
    background: rgb(var(--green-6));
}

.tone-bg-orange {
    background: rgb(var(--orange-6));
}

.tone-bg-red {
    background: rgb(var(--red-6));
}

.drive-foot {
    display: flex;
    justify-content: space-between;
    gap: 8px;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    font-variant-numeric: tabular-nums;
}

.ov-empty {
    margin-top: var(--base-margin);
    padding: 16px 0;
    display: flex;
    justify-content: center;
}
</style>
