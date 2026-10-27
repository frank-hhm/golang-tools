// npm（静态站点服务）页面的纯函数与共享类型：与 index.vue 及 components/*.vue 共用。
// 只放无副作用的类型与格式化逻辑，涉及 GoTools / 响应式状态的一律留在组件里。

export interface InstConfig {
    id: string; name: string; version: string; port: number; docRoot: string; spa: boolean; directory: boolean; openBrowser: boolean;
    // mode: local | docker；容器模式不跑 node.exe，靠镜像里的 nginx 承载站点
    mode: string; image: string; containerName: string;
}

/** 静态站点 Docker 模式的常用镜像：alpine 体积小，与 nginx 面板同一套候选 */
export const NGINX_IMAGE_PRESETS = ["nginx:alpine", "nginx:stable-alpine", "nginx:1.27-alpine", "nginx:1.26-alpine", "nginx:alpine-slim", "nginx:1.27"];

/** Docker 镜像 tag（nginx:alpine → alpine）：卡里只放短标，完整镜像名走 tooltip */
export function imageTag(image: string): string {
    const s = String(image || "");
    const i = s.indexOf(":");
    return i >= 0 ? s.slice(i + 1) : (s || "Docker");
}

/** 版本来源标签：内置 / 下载 / 本机 */
export function srcLabel(s: string): string {
    return s === "bundled" ? "内置" : s === "downloaded" ? "下载" : "本机";
}

/** 包管理器配色的 tag 颜色 */
export function mgrColor(name: string): string {
    return ({ npm: "red", yarn: "blue", pnpm: "orange", bun: "purple", cnpm: "green", corepack: "gray" } as Record<string, string>)[name] || "gray";
}

/** 安装 / 切换进度阶段文案 */
export function stageText(p: any): string {
    if (!p) return "";
    // 百分比由行内 a-progress 自带文本显示，这里只报阶段，别再重复带一次数字
    if (p.stage === "downloading") return "下载中";
    if (p.stage === "extracting") return "解压中";
    if (p.stage === "done") return "完成";
    if (p.stage === "info") return "执行中";
    return "失败";
}

/** 字节数格式化：B / KB / MB / GB */
export function fmtBytes(b?: number): string {
    if (!b) return "-";
    const units = ["B", "KB", "MB", "GB"]; let i = 0; let v = b;
    while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
    return v.toFixed(i > 0 ? 1 : 0) + " " + units[i];
}

/** 运行时长格式化：秒 → 1d 2h 3m */
export function fmtUptimeS(sec?: number): string {
    if (!sec || sec <= 0) return "-";
    const d = Math.floor(sec / 86400); const h2 = Math.floor((sec % 86400) / 3600);
    const m = Math.floor((sec % 3600) / 60);
    if (d > 0) return `${d}d ${h2}h ${m}m`; if (h2 > 0) return `${h2}h ${m}m`; return `${m}m`;
}

/** PATH 绑定作用域文案 */
export function scopeLabel(s?: string): string {
    return s === "system" ? "系统 PATH" : s === "user" ? "用户 PATH" : "-";
}

/** npm 生效链层级名：覆盖层 / 自带层 / 无 npm */
export function layerName(k?: string): string {
    return k === "override" ? "覆盖层" : k === "bundled" ? "自带层" : "无 npm";
}

/** 站点详情指标卡：版式对齐 nginx，把运行态/时长/端口/请求/流量/版本收成一排卡片 */
export function buildNpmStatCards(st: any, c: any): any[] {
    if (!c) return [];
    const running = !!st?.running;
    const docker = (c.mode || st?.mode) === "docker";
    return [
        {
            key: "state", title: "运行状态",
            value: running ? "运行中" : "已停止",
            sub: docker ? (st?.container || "容器") : (st?.pid ? "PID " + st.pid : "未启动"),
            icon: "server", tone: running ? "green" : "gray", ready: running,
            tip: running ? "端口已监听，站点在跑" : "站点当前没有对外服务",
        },
        {
            key: "uptime", title: "运行时长", value: fmtUptimeS(st?.uptimeSeconds),
            sub: running ? "自本次启动累计" : "站点未运行", icon: "uptime", tone: "blue", ready: running,
            tip: "进程存活时长；重启后归零",
        },
        {
            key: "port", title: "HTTP 端口", value: c.port ? ":" + c.port : "—",
            sub: docker ? "映射到容器 :80" : "127.0.0.1 仅本机", icon: "plug", tone: "purple", ready: !!c.port,
            tip: docker ? "宿主端口映射到容器 80" : "内置服务器只监听 127.0.0.1",
        },
        {
            key: "req", title: "累计请求", value: st?.requests ?? "—",
            sub: st?.requestsPerSec ? st.requestsPerSec.toFixed(2) + " req/s" : "访问日志估算", icon: "gauge", tone: "orange", ready: running,
            tip: docker ? "容器里没有内置探针：按挂载到宿主机的 access.log 统计" : "内置服务器 /__gonfo__ 探针累计计数",
        },
        {
            key: "bytes", title: "累计发送", value: fmtBytes(st?.bytes),
            sub: st?.files ? "站点文件 " + st.files + " 个" : "静态资源出流量", icon: "net", tone: "cyan", ready: running,
            tip: "自本次启动以来发送的字节总量",
        },
        {
            key: "ver", title: docker ? "镜像" : "Node 版本", value: docker ? imageTag(st?.image || c.image || "") : (st?.version || c.version || "—"),
            sub: docker ? "容器镜像 tag" : (st?.npmVer ? "npm " + st.npmVer : "本机 node"), icon: docker ? "image" : "tag", tone: "red",
            ready: docker ? !!(st?.image || c.image) : !!(st?.version || c.version),
            tip: docker ? "容器模式的版本由镜像 tag 决定" : "实际跑站点的 node.exe 版本",
        },
    ];
}
