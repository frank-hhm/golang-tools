/**
 * 镜像长任务（拉取 / 推送 / 导入 / 构建 / 导出 / 清理）的共享类型与小工具。
 *
 * 页面负责发起与控制，TaskCenterModal 只负责展示；两边共用这里的类型与格式化，
 * 字段名与 Go 侧 PullProgress / ImageTaskProgress 的 json tag 一一对应。
 */

import dayjs from "dayjs";

export type TaskOp = "pull" | "push" | "import" | "build" | "export" | "prune";

/** 清单里每一项前面的短词 */
export const OP_TEXT: Record<TaskOp, string> = {
    pull: "拉取",
    push: "推送",
    import: "导入",
    build: "构建",
    export: "导出",
    prune: "清理",
};

/** 创建容器弹窗校验完回传给页面的表单 */
export interface RunForm {
    image: string;
    name: string;
    containerPort: number;
    hostPort: number;
    env: string;
    autoStart: boolean;
}

/** 分层表格的一行（只有拉取有）；key 是层的完整 ID，表格用它做 row-key */
export interface LayerRow {
    key: string;
    layer: string;
    percent: number;
    status: "running" | "done";
    text: string;
}

/** 一笔长任务的完整状态：页面持有并更新，组件只读 */
export interface ImageTask {
    id: string;
    op: TaskOp;
    /** 展示名：镜像引用 / 镜像标签 / 文件名 */
    name: string;
    /** 后端 beginOp 用的任务名，取消时按它精确匹配 */
    cancelKey: string;
    busy: boolean;
    failed: boolean;
    canceled: boolean;
    percent: number;
    stage: string;
    stageText: string;
    note: string;
    bytes: number;
    total: number;
    layers: number;
    doneLayers: number;
    logs: string[];
    speed: string;
    startedAt: number;
    endedAt: number;
    layerMap: Record<string, LayerRow>;
    /** 失败后点「重试」要做什么，由发起方各自塞进来 */
    retry?: () => void;
    /** 后端报「长时间没新进度」：任务还在跑，但界面提前给提示并开放重试 */
    stalled?: boolean;
    /** 每启动一次自增：重试时用来丢掉上一轮调用晚到的收尾 */
    runId?: number;
    /** 速率采样点，不参与展示 */
    rate?: { t: number; b: number };
}

export function taskStatusText(t: ImageTask): string {
    if (t.busy) return t.stalled ? "疑似卡住" : "进行中";
    if (t.failed) return "失败";
    if (t.canceled) return "已取消";
    return t.percent >= 100 ? "已完成" : "已结束";
}

export function taskStatusBadge(t: ImageTask): "processing" | "warning" | "danger" | "success" | "normal" {
    if (t.busy) return t.stalled ? "warning" : "processing";
    if (t.failed) return "danger";
    if (t.canceled) return "warning";
    return t.percent >= 100 ? "success" : "normal";
}

/** 圆点与文字共用的语义色类，挂在 .com-state 上 */
const BADGE_CLASS: Record<string, string> = {
    processing: "is-busy",
    warning: "is-warn",
    danger: "is-off",
    success: "is-on",
    normal: "is-dim",
};

export function taskStatusClass(t: ImageTask): string {
    return BADGE_CLASS[taskStatusBadge(t)];
}

export function taskProgressStatus(t: ImageTask): "normal" | "success" | "danger" | "warning" {
    if (t.failed) return "danger";
    if (t.busy) return t.stalled ? "warning" : "normal";
    if (t.canceled) return "warning";
    return "success";
}

export function fmtBytes(n: number): string {
    if (!n || n <= 0) return "-";
    const u = ["B", "KB", "MB", "GB", "TB"];
    let i = 0;
    let v = n;
    while (v >= 1024 && i < u.length - 1) {
        v /= 1024;
        i++;
    }
    return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${u[i]}`;
}

/** daemon 经常只报几十字节的层元数据（尤其走加速站时），
 *  这种量级的“已下 / 总量”拿去展示只会让人以为快下完了 */
export function bytesTrustworthy(n: number): boolean {
    return (Number(n) || 0) >= 1024 * 1024;
}

/** daemon 回的是英文阶段名，表格里翻成中文更好扫 */
const LAYER_STATUS_ZH: Record<string, string> = {
    "Pulling fs layer": "拉取元数据",
    Waiting: "等待中",
    Downloading: "下载中",
    "Verifying Checksum": "校验中",
    "Download complete": "下载完成",
    Extracting: "解压中",
    "Pull complete": "已完成",
    "Already exists": "已存在",
};

export function layerStatusText(s: string): string {
    return LAYER_STATUS_ZH[s] || s || "处理中";
}

/** 虚拟化后端的短名：引擎只回键名，界面上要给人看的写法 */
export function beLabel(name: string): string {
    return name === "wsl2" ? "WSL2" : name === "hyperv" ? "Hyper-V" : "-";
}

/** 仓库地址去掉协议与 /v1|v2 尾巴，只留主机名 */
export function hostOnly(u: string): string {
    return String(u || "")
        .replace(/^https?:\/\//i, "")
        .replace(/\/v[12]\/?$/, "")
        .replace(/\/+$/, "");
}

/** 镜像名的首段像主机名时，看它是不是已登记的私有仓库（本地表打标签 / 推送默认目标用） */
export function localRegistryOf(repo: string, registries: any[]): string {
    const head = String(repo || "").split("/")[0];
    if (!head) return "";
    const isHost = head.includes(".") || head.includes(":") || head === "localhost";
    if (!isHost) return "";
    const hit = (registries || []).find((r: any) => {
        const raw = hostOnly(String(r.url)).split("/")[0];
        return raw === head;
    });
    return hit ? (hit.name || hit.url) : head;
}

/**
 * 网络 / 存储卷 / 编排列表里用到的两段时间格式：
 * daemon 的网络与卷给的是 RFC3339 字符串，容器与 compose 项目给的是 unix 秒，空值统一给 "-"
 */
export function fmtRFC(s: string): string {
    if (!s) return "-";
    const t = dayjs(s);
    return t.isValid() ? t.format("YYYY-MM-DD HH:mm") : "-";
}

export function fmtUnix(sec: number): string {
    if (!sec) return "-";
    const t = dayjs(sec * 1000);
    return t.isValid() ? t.format("YYYY-MM-DD HH:mm") : "-";
}
