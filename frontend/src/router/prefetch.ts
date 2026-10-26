/**
 * 路由块预热。
 *
 * Web 模式下每个工具页都是独立 chunk（docker 635KB、终端 285KB、mysql 88KB…），
 * 点进去才发请求，在远程服务器上就是"点一下愣一秒"。这里把 TOOLS 里的 loader
 * 按路径暴露出来，交给导航在 hover / 触摸按下 / 聚焦时提前拉，点下去时通常已经
 * 命中浏览器缓存（资源走 immutable 缓存，命中后不再有网络往返）。
 *
 * 不做"挂载后一次性预热全部路由"：23 个工具合计约 4MB，那样只是把卡顿从点击时
 * 挪到开机时，对按流量计费的服务器也不友好。
 */
import { TOOLS } from "./modules/base";

type Loader = () => Promise<unknown>;

/**
 * 路径 → loader 的映射，首次真正预热时才建。
 *
 * 不能放在模块顶层建：base.ts → layouts/index.vue → SideNav.vue → 本文件 构成环，
 * 顶层就读 TOOLS 会撞上"模块尚未初始化"的 TDZ 错误。hover 时 TOOLS 早已就绪。
 */
let loaders: Map<string, Loader> | null = null;

/** 已经发起过的路径：loader 自身会去重，这里只是省掉重复排任务 */
const warmed = new Set<string>();

function normalize(path: string): string {
    const p = "/" + String(path).split("?")[0].split("#")[0].replace(/^\/+/, "");
    // 原生新窗口路由 /win/tools/xxx 与页面内路由是同一个组件
    return p.startsWith("/win/") ? p.slice(4) : p;
}

function loaderFor(path: string): Loader | undefined {
    if (!loaders) {
        loaders = new Map();
        for (const t of TOOLS) {
            loaders.set("/" + String(t.path).replace(/^\/+/, ""), t.component as Loader);
        }
    }
    return loaders.get(path);
}

/** 提前拉某个工具页面的 chunk；路径不认识时安静返回 */
export function prefetchRoute(path?: string) {
    if (!path) return;
    const key = normalize(path);
    const loader = loaderFor(key);
    if (!loader || warmed.has(key)) return;
    warmed.add(key);
    // 失败就把标记撤回：下次 hover 还能再试，不把错误吞成"永远不预热"
    void loader().catch(() => { warmed.delete(key); });
}
