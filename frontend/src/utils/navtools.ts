import { isToolAvailable } from "./caps";
import { normalizeToolIcon } from "./tool-icon";

/** 侧边导航用的工具项：只有路由型（path）能进导航，event 型是首页的快捷触发 */
export interface NavTool {
    id?: string;
    key?: string;
    name: string;
    path: string;
    icon?: string;
}

/**
 * 从配置里挑出可导航的工具。
 *
 * 与首页快捷操作共用同一份 config.tools（用户自己增删与排序），
 * 区别是这里必须带路由：截图那类 event 型入口在导航栏里点了没有落点。
 * 同时按平台能力过滤，避免把内嵌浏览器这类当前构建用不了的入口摆出来。
 */
export function navToolsFromConfig(cfg: any): NavTool[] {
    const list: any[] = cfg?.tools || [];
    const out: NavTool[] = [];
    for (const t of list) {
        const path = String(t?.path || "").trim();
        if (!path) continue;
        if (!isToolAvailable(path, t?.event)) continue;
        out.push({
            id: t.id,
            key: t.key,
            name: String(t.name || path),
            path,
            icon: normalizeToolIcon(t.icon),
        });
    }
    return out;
}
