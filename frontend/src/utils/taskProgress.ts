/**
 * 全局任务/下载进度行的归一工具：把各处任务模型（后端 tasks.Task、docker DockerTask 等）
 * 映射成 common-task-progress 的 props 片段，避免各页模板里重复写 /100、状态三元、ETA 估算。
 *
 * 纯前端、无后端依赖；只处理"展示口径"，不发起任何调用。
 */

/** 与 common-task-progress 的 status 取值一致 */
export type ProgressStatus = "idle" | "running" | "done" | "error" | "canceled" | "stalled" | "paused";

/**
 * 后端 / 各模块五花八门的状态词表 → 统一 ProgressStatus。
 * 认：running/downloading/extracting/installing/starting、pending/queued/idle、
 * done/success、error/failed、canceled/cancelled、paused（用户暂停）；未知按 running 处理。
 */
export function normalizeStatus(status?: string): ProgressStatus {
  switch (String(status || "").toLowerCase()) {
    case "pending":
    case "queued":
    case "idle":
      return "idle";
    case "paused":
      return "paused";
    case "done":
    case "success":
      return "done";
    case "error":
    case "failed":
      return "error";
    case "canceled":
    case "cancelled":
      return "canceled";
    case "stalled":
      return "stalled";
    case "running":
    case "downloading":
    case "extracting":
    case "installing":
    case "starting":
    default:
      return "running";
  }
}

/** 百分比钳到 0–100；undefined 透传（表示不定进度） */
export function clampPercent(p?: number): number | undefined {
  if (p == null || Number.isNaN(p)) return undefined;
  return Math.max(0, Math.min(100, Math.round(p)));
}

/**
 * 把秒数说成"大约还需 x"：不足 1 分钟显秒，1 小时内显分，再长显时；0/负/非有限值返回空串。
 * 用于进度行底部左侧的 ETA。
 */
export function humanEta(seconds?: number): string {
  if (seconds == null || !Number.isFinite(seconds) || seconds <= 0) return "";
  const s = Math.floor(seconds);
  if (s < 60) return `大约还需 ${s} 秒`;
  const m = Math.floor(s / 60);
  if (m < 60) return `大约还需 ${m} 分钟`;
  const h = Math.floor(m / 60);
  return `大约还需 ${h} 小时${m % 60 ? " " + (m % 60) + " 分" : ""}`;
}

/**
 * 由"已下字节 / 总字节 / 当前速率（字节每秒）"估算剩余时间文案。
 * 数据不全（缺 total、缺 downloaded、速率 ≤0）时返回空串——宁可不显 ETA，也不猜一个离谱数字。
 */
export function etaFromBytes(downloaded?: number, total?: number, bytesPerSec?: number): string {
  if (!total || total <= 0 || downloaded == null || downloaded < 0) return "";
  if (!bytesPerSec || bytesPerSec <= 0) return "";
  const remainBytes = Math.max(0, total - downloaded);
  return humanEta(remainBytes / bytesPerSec);
}

/** common-task-progress 需要的 props 片段（各消费方按需再补 name/speed/meta/actions） */
export interface TaskProgressView {
  status: ProgressStatus;
  percent?: number;
  eta?: string;
}

/**
 * 引擎 / 台账状态词 → 中文短标签。下载页与主页抽屉共用同一口径，避免两套文案漂移。
 * 认：pending/queued/running/paused/done/error/canceled；未知显"—"。
 */
const STATUS_LABEL: Record<string, string> = {
  pending: "待开始",
  queued: "排队中",
  running: "下载中",
  paused: "已暂停",
  done: "已完成",
  error: "失败",
  canceled: "已取消",
};

export function statusLabel(status?: string): string {
  return STATUS_LABEL[String(status || "")] || "—";
}

/**
 * 状态标签色（a-tag color）：common-task-progress 语义色取向，下载页状态标签与抽屉状态标签共用。
 */
export function statusTagColor(status?: string): string {
  switch (String(status || "")) {
    case "done": return "green";
    case "error": return "red";
    case "canceled": return "gray";
    case "paused": return "orange";
    case "queued":
    case "pending": return "gray";
    default: return "arcoblue";
  }
}
