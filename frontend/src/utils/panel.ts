/**
 * 面板自身管理接口的浏览器端封装。
 *
 * 这些接口由 Go 侧 services/panel 提供（登录页、会话、安全入口、端口），
 * 与 Wails 绑定方法走的是同一台服务，但这里是普通 JSON 接口：
 * 面板配置里有密码哈希，不能放进 AppConfig 走绑定下发，所以单独开一套。
 */

const CSRF_HEADERS = { "X-Requested-With": "Tools-panel" };

/** 面板信息（对应 GET /api/panel/info） */
export interface PanelInfo {
    enabled: boolean;
    username: string;
    host: string;
    port: number;
    entryPath: string;
    sessionHours: number;
    updatedAt: string;
    accessURL: string;
    listen: string;
    uptime: number;
    version: string;
}

/** 可提交的配置项（未提供的字段保持原值） */
export interface PanelSettingsPatch {
    host?: string;
    port?: number;
    entryPath?: string;
    username?: string;
    sessionHours?: number;
    enabled?: boolean;
}

export interface PanelSaveResult {
    ok: boolean;
    message: string;
    accessURL: string;
    moved: boolean;
    info?: Partial<PanelInfo>;
}

/** 跳到登录页，并带上当前地址以便登录后原路返回 */
export function redirectToLogin() {
    if (typeof window === "undefined") return;
    if (window.location.pathname === "/login") return;
    const next = window.location.pathname + window.location.search;
    window.location.href = "/login" + (next && next !== "/" ? "?next=" + encodeURIComponent(next) : "");
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const res = await fetch(path, {
        credentials: "same-origin",
        ...init,
        headers: {
            Accept: "application/json",
            ...(init.body ? { "Content-Type": "application/json" } : {}),
            ...(init.method && init.method !== "GET" ? CSRF_HEADERS : {}),
            ...(init.headers || {}),
        },
    });
    if (res.status === 401) {
        redirectToLogin();
        throw new Error("登录已过期，请重新登录");
    }
    const data: any = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data?.error || `请求失败（HTTP ${res.status}）`);
    return data as T;
}

/** 读取当前面板配置（含当前登录账号） */
export function getPanelInfo() {
    return request<PanelInfo>("/api/panel/info");
}

/** 保存端口 / 监听地址 / 安全入口 / 账号 / 会话时长；端口改动会即时重新监听 */
export function savePanelSettings(patch: PanelSettingsPatch) {
    return request<PanelSaveResult>("/api/panel/settings", {
        method: "POST",
        body: JSON.stringify(patch),
    });
}

/** 修改密码（需校验当前密码；成功后其它设备下线，当前浏览器保持登录） */
export function changePanelPassword(current: string, next: string) {
    return request<{ ok: boolean; message: string }>("/api/panel/password", {
        method: "POST",
        body: JSON.stringify({ current, next }),
    });
}

/** 退出登录 */
export async function logoutPanel() {
    try {
        await fetch("/logout", { method: "POST", credentials: "same-origin", headers: CSRF_HEADERS });
    } finally {
        window.location.href = "/login";
    }
}

/**
 * 给所有绑定调用装一层"登录墙"探测：
 * 会话过期后 Wails 的 /wails/runtime 会拿到 401（带 X-Panel-Auth 头），
 * 这里统一跳登录页，避免页面上一堆"未知错误"却不知道是掉线了。
 */
export function installPanelGuard() {
    if (typeof window === "undefined") return;
    const w = window as any;
    if (w.__gtPanelGuard) return;
    w.__gtPanelGuard = true;

    const raw = window.fetch.bind(window);
    window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
        const res = await raw(input as any, init);
        if (res.status === 401 && res.headers.get("X-Panel-Auth") === "required") {
            redirectToLogin();
        }
        return res;
    };
}
