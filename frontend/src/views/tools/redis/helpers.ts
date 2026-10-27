/**
 * Redis 页面的共享类型与纯函数：从 index.vue 拆出，页面与各弹窗组件（ConnModal / InstanceModal /
 * VersionsModal / LogsModal）共用同一套定义与格式化，行为与原单文件实现一致。
 */

/** 已保存的外部连接配置 */
export interface ConnConfig { id: string; name: string; host: string; port: number; password: string; db: number }

/** 本地服务实例配置：mode = local | docker；容器模式靠 image 定位镜像，不占本机版本库 */
export interface SrvInstanceConfig {
    id: string; name: string; version: string; port: number;
    password: string; appendOnly: boolean; maxMemoryMB: number; dataDir: string;
    mode: string; image: string; containerName: string;
}

/** 左栏统一列表项：外部连接与本地实例合并成一个列表，点击都能连上并在右侧展开总览 */
export type RedisListItem =
    | { kind: "instance"; key: string; name: string; sub: string; live: boolean; inst: SrvInstanceConfig }
    | { kind: "external"; key: string; name: string; sub: string; live: boolean; conn: ConnConfig };

/** 版本管理里 Docker 镜像区的常用 tag：alpine 体积小，官方长期维护 */
export const REDIS_IMAGE_PRESETS = ["redis:7.4-alpine", "redis:7.4", "redis:7.2-alpine", "redis:7.2", "redis:alpine", "redis:7.0-alpine"];

/** Docker 实例的版本就是镜像 tag：列表里只展示 tag（7.4-alpine），完整镜像名走 tooltip */
export function imgTag(image: string): string {
    const s = String(image || "");
    const i = s.indexOf(":");
    return i >= 0 ? s.slice(i + 1) : (s || "Docker");
}

export function srcLabel(s: string): string {
    return s === "bundled" ? "内置" : s === "downloaded" ? "下载" : s === "system" ? "系统包" : "本机";
}

export function stageText(p: any): string {
    if (!p) return "";
    // 百分比由行内 a-progress 自带文本显示，这里只报阶段，别再重复带一次数字
    if (p.stage === "downloading") return "下载中";
    if (p.stage === "extracting") return "解压中";
    if (p.stage === "done") return "完成";
    return "失败";
}

/** TTL 展示：负数 = 持久，否则 hh:mm:ss */
export function formatTTL(ttl: number): string {
    if (ttl < 0) return "持久";
    const h = Math.floor(ttl / 3600);
    const m = Math.floor((ttl % 3600) / 60);
    const s = ttl % 60;
    const p = (n: number) => n.toString().padStart(2, "0");
    return `${p(h)}:${p(m)}:${p(s)}`;
}

/** 状态页运行时长（天/小时/分钟） */
export function formatUptime(sec: number): string {
    const d = Math.floor(sec / 86400);
    const h = Math.floor((sec % 86400) / 3600);
    if (d > 0) return `${d} 天 ${h} 小时`;
    if (h > 0) return `${h} 小时`;
    return `${Math.floor(sec / 60)} 分钟`;
}

/** 实例日志/状态里的运行时长：天/时/分/秒 */
export function fmtUptimeS(sec: number): string {
    if (!sec || sec <= 0) return "-";
    const d = Math.floor(sec / 86400); const h2 = Math.floor((sec % 86400) / 3600);
    const m = Math.floor((sec % 3600) / 60); const s = sec % 60;
    if (d > 0) return `${d}d ${h2}h ${m}m`; if (h2 > 0) return `${h2}h ${m}m`; return `${m}m ${s}s`;
}

/** keyspace 命中率：服务端 INFO 若有 hits/misses 就算，没给则回 “—” */
export function hitRateText(s: any): string {
    const hits = Number(s?.keyspaceHits) || 0;
    const misses = Number(s?.keyspaceMisses) || 0;
    const total = hits + misses;
    if (total <= 0) return "—";
    return ((hits / total) * 100).toFixed(1) + "%";
}

/**
 * 状态页指标卡：版式对齐 nginx，把运行态/内存/客户端/键数/版本/命中率收成一排卡片。
 * 传入 RedisStatus 返回的对象；为空返回空数组（页面未加载时不渲染卡片）。
 */
export function buildRedisStatCards(s: any): any[] {
    if (!s) return [];
    const memPct = s.maxMemory && s.usedMemory ? Math.round((s.usedMemory / s.maxMemory) * 100) : 0;
    return [
        {
            key: "uptime", title: "运行时长", value: formatUptime(s.uptimeSeconds || 0),
            sub: "自本次启动累计", icon: "uptime", tone: "orange", ready: true,
            tip: "服务已运行时长；重启后归零",
        },
        {
            key: "mem", title: "内存使用", value: s.usedMemoryHuman || "0",
            sub: s.maxMemoryHuman ? `上限 ${s.maxMemoryHuman}` : "未设置上限", icon: "mem", tone: "blue", ready: true,
            tip: s.maxMemory ? `已用占配置最大内存约 ${memPct}%` : "redis-server 未配置 maxmemory",
        },
        {
            key: "clients", title: "已连客户端", value: s.connectedClients ?? 0,
            sub: "当前活动连接数", icon: "net", tone: "cyan", ready: true,
            tip: "实时连接到该实例的客户端数量",
        },
        {
            key: "keys", title: "键总数", value: s.totalKeys ?? 0,
            sub: "所有库键空间合计", icon: "container", tone: "purple", ready: true,
            tip: "各数据库键数量之和",
        },
        {
            key: "ver", title: "Redis 版本", value: s.version || "—",
            sub: `${s.mode || "standalone"} · ${s.role || "master"}`, icon: "server", tone: "red", ready: !!s.version,
            tip: "服务端版本与运行模式/角色",
        },
        {
            key: "hit", title: "命中率", value: hitRateText(s),
            sub: "keyspace 命中/未命中", icon: "gauge", tone: "green", ready: !!s.keyspaceHits || !!s.keyspaceMisses,
            tip: "按 keyspace_hits / misses 估算，非精确实时监控",
        },
    ];
}

/**
 * 本地实例总览指标卡：数据源是 RedisSrvStatus（网络探测聚合），实例没起来也能出全量字段，
 * 支撑「单击未启动实例只看总览、不自动启动」的交互（对齐 mysql 实例总览）。
 */
export function buildRedisSrvStatCards(st: any, c: SrvInstanceConfig): any[] {
    const running = !!st?.running;
    const docker = (c.mode || st?.mode) === "docker";
    return [
        {
            key: "state", title: "运行状态", value: running ? "运行中" : "已停止",
            sub: docker ? (st?.container || c.containerName || "容器") : (st?.pid ? "PID " + st.pid : "未启动"),
            icon: "server", tone: running ? "green" : "gray", ready: running,
            tip: running ? "端口已监听，点「打开总览」连上看数据" : "实例未运行：双击左栏行或点右上「启动」拉起",
        },
        {
            key: "ver", title: docker ? "镜像" : "Redis 版本", value: docker ? imgTag(st?.image || c.image || "") : (st?.version || c.version || "—"),
            sub: docker ? "容器镜像 tag" : "本机 redis-server", icon: "tag", tone: "red", ready: true,
            tip: docker ? "容器模式的版本由镜像 tag 决定" : "配置文件里登记的本机版本，启动后以实际探测为准",
        },
        {
            key: "port", title: "端口", value: c.port ? ":" + c.port : "—",
            sub: docker ? "映射到宿主 127.0.0.1" : "仅监听 127.0.0.1", icon: "plug", tone: "purple", ready: true,
            tip: "127.0.0.1:" + c.port + "，只对本机开放",
        },
        {
            key: "uptime", title: "运行时长", value: fmtUptimeS(st?.uptimeSeconds),
            sub: running ? "自本次启动累计" : "实例未运行", icon: "uptime", tone: "blue", ready: running,
            tip: "进程存活时长；重启后归零",
        },
        {
            key: "clients", title: "已连客户端", value: running ? (st?.connectedClients ?? 0) : "—",
            sub: "当前活动连接数", icon: "net", tone: "cyan", ready: running,
            tip: "实时连接到该实例的客户端数量",
        },
        {
            key: "keys", title: "键总数", value: running ? (st?.totalKeys ?? 0) : "—",
            sub: "所有库键空间合计", icon: "container", tone: "orange", ready: running,
            tip: "各数据库键数量之和",
        },
    ];
}
