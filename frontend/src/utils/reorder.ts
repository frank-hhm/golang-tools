import { ref } from "vue";
import { App } from "bindings/Tools/services/app/index";

/**
 * 列表拖拽重排（原生 HTML5 DnD，不引第三方库）。
 *
 * 设计：
 *  - 顺序默认持久化到用户配置 AppConfig.Order[key]（随“导出/导入配置”一起搬运）；主页与各服务列表各用独立 key，互不共用。
 *    顺序本身就是某数组的属性时（如主页快捷操作 = 配置 tools 数组顺序），给 save 自己接管落盘。
 *  - 模块级 dragKey/dragId 保证同一时刻只有一个拖拽在飞，且只有同面的元素能互相排序。
 *  - 落点重排后：以 getVisible() 的当前可见顺序为准做移位；若另给 getFull()（完整未过滤 ID），
 *    按“可见重排 + 隐藏项保持相对位置”合并出完整顺序再持久化，带过滤的列表拖动也不会丢掉未显示项的顺序。
 */

/** 同一时刻只允许一个拖拽在飞；记录来源面 key 与被拖条目 id */
let dragKey = "";
let dragId = "";

export interface ReorderOpts {
    /** 当前可见条目 id 顺序（拖拽移位在此列表上进行）；必填 */
    getVisible: () => string[];
    /** 完整（未过滤）id 列表；提供时按它合并出持久化顺序，避免过滤态丢序 */
    getFull?: () => string[];
    /**
     * 顺序落定后的回调（列表尚未接线的面用它把新顺序应用到本地数据）：
     *  - newVisibleIds：可见项的新顺序
     *  - fullIds：合并隐藏项后的完整顺序（未给 getFull 时与前者一致）
     */
    onReordered?: (newVisibleIds: string[], fullIds: string[]) => void;
    /**
     * 覆盖默认持久化（写 Order[key]）：顺序得存成某个数据数组自身的真实顺序时用，
     * 比如主页快捷操作直接回写配置里的 tools 顺序，与设置页「工具」表共用一份
     */
    save?: (fullIds: string[]) => unknown;
}

/** 读取某面的已存顺序（来自 GetConfig 的 order 段） */
export async function loadOrder(key: string): Promise<string[]> {
    try {
        const cfg = await App.GetConfig();
        return (cfg?.order?.[key] as string[]) || [];
    } catch {
        return [];
    }
}

/**
 * 把已存顺序套用到列表：按 order 里的 id 排名排序，未在 order 中的项保持自然顺序追加在末尾。
 * 新增项（从没排过序）自然落在最后，不打乱用户已排好的部分。稳定排序。
 */
export function applyOrder<T>(list: T[], order: string[], idOf: (t: T, i: number) => string | number): T[] {
    if (!order?.length) return [...list];
    const idx = new Map<string, number>();
    order.forEach((id, i) => idx.set(id, i));
    const rank = (t: T, i: number) => {
        const k = String(idOf(t, i));
        return idx.has(k) ? (idx.get(k) as number) : Number.MAX_SAFE_INTEGER;
    };
    return list
        .map((t, i) => ({ t, i, r: rank(t, i) }))
        .sort((a, b) => a.r - b.r || a.i - b.i)
        .map((x) => x.t);
}

export function useReorder(key: string, opts: ReorderOpts) {
    const draggingId = ref("");
    const dragOverId = ref("");

    function handlers(id: string | number) {
        const sid = String(id);
        return {
            draggable: true,
            onDragstart: (e: DragEvent) => {
                dragKey = key;
                dragId = sid;
                draggingId.value = sid;
                if (e.dataTransfer) {
                    e.dataTransfer.effectAllowed = "move";
                    try { e.dataTransfer.setData("text/plain", sid); } catch { /* 某些环境 setData 受限，忽略 */ }
                }
            },
            onDragenter: (e: DragEvent) => {
                if (dragKey !== key || !dragId) return;
                e.preventDefault();
                if (dragId !== sid) dragOverId.value = sid;
            },
            onDragover: (e: DragEvent) => {
                if (dragKey !== key || !dragId) return;
                e.preventDefault();
                if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
            },
            onDragleave: (e: DragEvent) => {
                // 仍在同一元素内移动时不清高亮
                if (dragOverId.value === sid && e.relatedTarget && (e.currentTarget as Node)?.contains?.(e.relatedTarget as Node)) return;
                if (dragOverId.value === sid) dragOverId.value = "";
            },
            onDrop: (e: DragEvent) => {
                if (dragKey !== key || !dragId || dragId === sid) { reset(); return; }
                e.preventDefault();
                const from = dragId, to = sid;
                reset();
                commit(from, to);
            },
            onDragend: () => reset(),
        };
    }

    function reset() {
        dragKey = "";
        dragId = "";
        draggingId.value = "";
        dragOverId.value = "";
    }

    /** 当前可见顺序里把 from 移到 to 位置 → 合并完整顺序 → 回调 → 持久化 */
    function commit(from: string, to: string) {
        const vis = opts.getVisible().map(String);
        const fi = vis.indexOf(from), ti = vis.indexOf(to);
        if (fi < 0 || ti < 0 || fi === ti) return;
        const nextVis = [...vis];
        const [moved] = nextVis.splice(fi, 1);
        nextVis.splice(ti, 0, moved);
        const full = mergeFull(nextVis);
        opts.onReordered?.(nextVis, full);
        void persist(full);
    }

    /** 合并“可见重排 + 隐藏项保持相对位置”，得到要落盘的完整顺序 */
    function mergeFull(newVis: string[]): string[] {
        if (!opts.getFull) return newVis;
        const all = opts.getFull().map(String);
        const visSet = new Set(newVis);
        const hidden = all.filter((id) => !visSet.has(id));
        const full = [...newVis];
        for (const id of hidden) {
            const oldIdx = all.indexOf(id);
            let prevVisId = "";
            for (let k = oldIdx - 1; k >= 0; k--) {
                if (visSet.has(all[k])) { prevVisId = all[k]; break; }
            }
            const pos = prevVisId ? full.indexOf(prevVisId) + 1 : 0;
            full.splice(pos, 0, id);
        }
        return full;
    }

    /** 落盘：默认写 Order[key]；给了 save 就由调用方存到自己的地方 */
    async function persist(full: string[]) {
        try {
            if (opts.save) await opts.save(full);
            else await App.SetListOrder(key, full);
        } catch { /* 持久化失败静默，下次进入以本地为准 */ }
    }

    const isDragging = (id: string | number) => draggingId.value === String(id);
    const isOver = (id: string | number) => dragOverId.value === String(id);

    return { draggingId, dragOverId, handlers, isDragging, isOver, load: () => loadOrder(key) };
}
