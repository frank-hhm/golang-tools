/**
 * 路由块预热。
 *
 * Web 模式下每个工具页都是独立 chunk（docker 1.4MB、终端 285KB、mysql 88KB…），
 * 点进去才发请求，在远程服务器上就是"点一下愣一秒"。这里从 router 的路由表里
 * 取出各工具页的 loader，两种时机提前拉：
 *
 *  1. 交互时（hover / 触摸按下 / 聚焦）：命中率高、几乎零成本；
 *  2. 挂载后空闲时：分批把剩下的也拉完，点哪儿都是命中缓存。
 *
 * 空闲预热做了三道约束，避免把"点击卡顿"换成"开机卡顿"：
 *   - 首屏稳定后才开始（延迟 600ms，不抢首屏带宽）；
 *   - 每块之间隔 180ms 串行排队（不和当前页正在发的绑定请求抢连接）；
 *   - 客户端开启"节省流量"时整体跳过。
 * 资源本身走 immutable 缓存，二次进入不再产生请求。
 *
 * 注意：本文件不 import 路由表（modules/base），而是由 main.ts 注入 router 实例后
 * 从路由表反查 loader。modules/base → layouts → SideNav → 本文件本身就是一个环，
 * 顶层读路由表会撞上 TDZ / 生产构建里的 undefined。
 */
import type { Router } from "vue-router";

type Loader = () => Promise<unknown>;

let router: Router | null = null;

/** 已经发起过的路径：loader 自身会去重，这里只是省掉重复排任务 */
const warmed = new Set<string>();

/** 空闲预热是否已经排过（只排一次） */
let allQueued = false;

/** 由 main.ts 在创建 router 后注入 */
export function bindPrefetchRouter(r: Router) {
    router = r;
}

function normalize(path: string): string {
    const p = "/" + String(path).split("?")[0].split("#")[0].replace(/^\/+/, "");
    // 原生新窗口路由 /win/tools/xxx 与页面内路由是同一个组件
    return p.startsWith("/win/") ? p.slice(4) : p;
}

/** 从路由记录里取懒加载 loader：命中链尾那个函数才是页面本身（前面是布局） */
function loaderFor(path: string): Loader | undefined {
    if (!router) return undefined;
    const matched = router.resolve(path).matched;
    for (let i = matched.length - 1; i >= 0; i--) {
        const c: any = (matched[i] as any).components?.default;
        if (typeof c === "function") return c as Loader;
    }
    return undefined;
}

/** 提前拉某个工具页面的 chunk；路径不认识时安静返回 */
export function prefetchRoute(path?: string) {
    if (!path) return;
    const key = normalize(path);
    if (warmed.has(key)) return;
    const loader = loaderFor(key);
    if (!loader) return;
    warmed.add(key);
    // 失败就把标记撤回：下次 hover 还能再试，不把错误吞成"永远不预热"
    void loader().catch(() => { warmed.delete(key); });
}

function idle(cb: () => void) {
    const ric = (window as any).requestIdleCallback;
    if (typeof ric === "function") {
        ric(cb, { timeout: 1500 });
        return;
    }
    window.setTimeout(cb, 120);
}

/** 路由表里所有还在懒加载的工具页路径 */
function lazyToolPaths(): string[] {
    if (!router) return [];
    const out: string[] = [];
    const seen = new Set<string>();
    for (const rec of router.getRoutes()) {
        if (!rec.path.startsWith("/tools/") || seen.has(rec.path)) continue;
        const c: any = (rec as any).components?.default;
        if (typeof c === "function") {
            seen.add(rec.path);
            out.push(rec.path);
        }
    }
    return out;
}

/**
 * 空闲时把所有工具页 chunk 分批预热一遍。
 * 只在 Web 模式调用（桌面版每个工具是独立窗口，预热没有意义）。
 */
export function prefetchAllRoutes() {
    if (typeof window === "undefined" || allQueued) return;
    allQueued = true;

    const conn = (navigator as any).connection;
    if (conn?.saveData) return;

    const queue = lazyToolPaths();
    let i = 0;
    const pump = () => {
        if (i >= queue.length) return;
        prefetchRoute(queue[i++]);
        idle(() => window.setTimeout(pump, 180));
    };
    // 让首屏（首页卡片 + caps 请求）先跑完再开始
    idle(() => window.setTimeout(pump, 600));
}
