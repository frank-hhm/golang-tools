<template>
    <!-- 容器表：状态点 + 端口/资源占用 + 行内操作，动作全部回传页面。
         scroll.x 必须给（列宽合计 1268），否则 fixed="right" 的操作列不会吸边，窄窗口下直接被切掉；
         给了之后内容横向滚动，操作按钮始终贴在右侧 -->
    <a-table :data="data" :loading="loading" :pagination="false" size="small" row-key="id"
        :scroll="{ x: 1268, y: '100%' }">
        <template #columns>
            <a-table-column title="名称" data-index="name" :width="170" ellipsis tooltip>
                <template #cell="{ record }">
                    <!-- 不用 a-space：它会每项包一层 .arco-space-item，名称的 flex/min-width 控不住，
                         长名会把列撑开（横向滚动条就是这么冒出来的）。这里自己排一行：点 / 名（可缩） / 标签 -->
                    <span class="cname-cell">
                        <com-badge :status="record.state === 'running' ? 'success' : 'normal'"
                            :animated="record.state === 'running'" />
                        <!-- 列宽固定，名字过长会省略号收尾，悬停拿完整名 -->
                        <a-tooltip :content="record.name || '(未命名)'">
                            <span class="cname">{{ record.name || '(未命名)' }}</span>
                        </a-tooltip>
                        <a-tag v-if="record.ownerTool" size="small" color="purple" class="cname-tag">{{ record.ownerTool }}</a-tag>
                    </span>
                </template>
            </a-table-column>
            <a-table-column title="镜像" data-index="image" :width="180" ellipsis tooltip />
            <a-table-column title="状态" data-index="status" :width="150" ellipsis tooltip>
                <template #cell="{ record }">
                    <!-- 只用文字颜色表达状态：圆点已经在名称列，一行里不再堆第二个点 -->
                    <span :style="{ color: stateColor(record.state) }">{{ record.status || record.state }}</span>
                </template>
            </a-table-column>
            <a-table-column title="端口映射" :width="190">
                <template #cell="{ record }">
                    <div class="ports">
                        <span v-for="(p, i) in (record.ports || []).slice(0, 3)" :key="i" class="port">
                            {{ p.ip || '0.0.0.0' }}:{{ p.publicPort || '-' }}→{{ p.privatePort }}/{{ p.type }}
                        </span>
                        <span v-if="(record.ports || []).length > 3" class="port more">
                            +{{ record.ports.length - 3 }}
                        </span>
                        <span v-if="!(record.ports || []).length" class="port none">-</span>
                    </div>
                </template>
            </a-table-column>
            <a-table-column title="CPU" :width="80">
                <template #cell="{ record }">{{ record.state === 'running' ? record.cpuPct.toFixed(1) + '%' : '-' }}
                </template>
            </a-table-column>
            <a-table-column title="内存" :width="120">
                <template #cell="{ record }">
                    <a-tooltip :content="record.memUsage" :disabled="!record.memUsage">
                        <span>
                            {{ record.state === 'running' ? record.memPct.toFixed(1) + '%' : '-' }}
                        </span>
                    </a-tooltip>
                </template>
            </a-table-column>
            <a-table-column title="创建" :width="100">
                <template #cell="{ record }">{{ fmtAgo(record.created) }}</template>
            </a-table-column>
            <!-- 固定列宽度按实际按钮排布给足（启停/重启/删除/… ≈ 300px，动作中的文案再多 14px），
                 否则固定列会裁掉尾巴；靠右对齐，让按钮贴着表格右边。
                 高频主动作留按钮直给，重命名/终端/日志/监控收进「…」下拉；
                 行内动作在等待 docker 时置 loading 并锁住整行按钮，避免连点重复下发 -->
            <a-table-column title="操作" :width="300" fixed="right" align="right">
                <template #cell="{ record }">
                    <a-space :size="2">
                        <!-- 停止/重启会短暂中断服务，先气泡确认再下发 -->
                        <a-button v-if="record.state !== 'running'" size="mini" status="success"
                            :loading="opOf(record) === 'start'" :disabled="locked(record)"
                            @click="emit('start', record)"><template #icon><icon-play-arrow /></template>{{ opOf(record) === 'start' ? '启动中' : '启动' }}</a-button>
                        <a-popconfirm v-else :content="`停止容器「${record.name}」？正在处理的请求会中断。`"
                            type="warning" position="lt" :disabled="locked(record)" @ok="emit('stop', record)">
                            <a-button size="mini" status="warning" :loading="opOf(record) === 'stop'"
                                :disabled="locked(record)"><template #icon><icon-pause /></template>{{ opOf(record) === 'stop' ? '停止中' : '停止' }}</a-button>
                        </a-popconfirm>
                        <a-popconfirm :content="`重启容器「${record.name}」？会先停再起，期间不可访问。`"
                            type="warning" position="lt" :disabled="locked(record)"
                            @ok="emit('restart', record)">
                            <a-button size="mini" :loading="opOf(record) === 'restart'" :disabled="locked(record)"><template
                                    #icon><icon-refresh /></template>{{ opOf(record) === 'restart' ? '重启中' : '重启' }}</a-button>
                        </a-popconfirm>
                        <!-- 次要动作收进「…」下拉：终端/监控只对运行中的容器有意义，置灰不隐藏，保持菜单项稳定 -->
                        <a-dropdown trigger="click" @select="(v: any) => onMore(String(v), record)">
                            <a-button size="mini" :disabled="locked(record)"><template
                                    #icon><icon-more /></template></a-button>
                            <template #content>
                                <a-doption value="rename"><template #icon><icon-edit /></template>重命名</a-doption>
                                <a-doption value="term" :disabled="record.state !== 'running'"><template
                                        #icon><icon-code /></template>终端</a-doption>
                                <a-doption value="logs"><template #icon><icon-list /></template>日志</a-doption>
                                <a-doption value="monitor" :disabled="record.state !== 'running'"><template
                                        #icon><icon-dashboard /></template>监控</a-doption>
                            </template>
                        </a-dropdown>
                        <a-popconfirm :content="`确定删除容器「${record.name}」？运行中会强制删除。`" type="warning"
                            position="lt" :disabled="locked(record)" @ok="emit('remove', record)">
                            <a-button size="mini" status="danger" :loading="opOf(record) === 'remove'"
                                :disabled="locked(record)"><template #icon><icon-delete /></template>{{ opOf(record) === 'remove' ? '删除中' : '删除' }}
                            </a-button>
                        </a-popconfirm>
                    </a-space>
                </template>
            </a-table-column>
        </template>
    </a-table>
</template>

<script setup lang="ts">
const props = defineProps<{
    /** 页面持有的容器清单 */
    data: any[];
    loading: boolean;
    /** 行内动作进行中：容器 id → 动作（start/stop/restart/remove），用于按钮 loading 与整行锁定 */
    busy?: Record<string, string>;
}>();

/** 该容器正在执行的动作，空串表示空闲 */
function opOf(record: any): string {
    return props.busy?.[record.id] || "";
}

/** 行内有动作在跑：其余按钮一律禁用，防止并发操作同一个容器 */
function locked(record: any): boolean {
    return !!opOf(record);
}

/** 「…」下拉的选择回传：与原来各按钮的 emit 口径一致 */
function onMore(v: string, record: any) {
    if (v === "rename") emit("rename", record);
    else if (v === "term") emit("term", record);
    else if (v === "logs") emit("logs", record);
    else if (v === "monitor") emit("monitor", record);
}

const emit = defineEmits<{
    (e: "start", c: any): void;
    (e: "stop", c: any): void;
    (e: "restart", c: any): void;
    (e: "rename", c: any): void;
    (e: "logs", c: any): void;
    (e: "term", c: any): void;
    (e: "monitor", c: any): void;
    (e: "remove", c: any): void;
}>();

/** 容器状态 → 文字色（与 com-badge 的语义色同一套） */
function stateColor(s: string): string {
    if (s === "running") return "rgb(var(--success-6))";
    if (s === "paused") return "rgb(var(--warning-6))";
    if (s === "restarting") return "rgb(var(--primary-6))";
    return "var(--color-text-3)";
}

function fmtAgo(unixSec: number): string {
    if (!unixSec) return "-";
    const s = Math.max(0, Math.floor(Date.now() / 1000 - unixSec));
    if (s < 60) return `${s} 秒前`;
    if (s < 3600) return `${Math.floor(s / 60)} 分钟前`;
    if (s < 86400) return `${Math.floor(s / 3600)} 小时前`;
    return `${Math.floor(s / 86400)} 天前`;
}
</script>

<style scoped>
/* 表格撑满父级 flex 列：滚动区跟着卡片高度走 */
:deep(.arco-table-container) {
    flex: 1;
}

.cname-cell {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
}

.cname-tag {
    flex: none;
}

/* 名称占剩下的宽度，超出就省略号（宽度由列宽控制，不再写死 max-width） */
.cname {
    flex: 1 1 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 500;
}

.ports {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
}

.port {
    font-family: var(--code-font-family);
    font-size: var(--base-size-small);
    background: var(--color-fill-2);
    border-radius: var(--base-radius-small);
    padding: 0 var(--base-radius-small);
    color: var(--color-text-2);
}

.port.none,
.port.more {
    background: transparent;
    color: var(--color-text-3);
}
</style>
