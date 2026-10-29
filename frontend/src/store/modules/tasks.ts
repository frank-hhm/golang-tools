import { computed, ref } from "vue"
import { defineStore } from "pinia"
import { Events } from "@wailsio/runtime"
import { App } from "bindings/Tools/services/app/index"
import type { Task } from "bindings/Tools/services/tasks/index"
import { isNativeWindows, loadCaps } from "@/utils/caps"
import store from "@/store"

/**
 * 安装/下载任务台账（前端侧）。
 *
 * 为什么要有它：后端每个安装任务都是独立 goroutine，进度通过一次性事件推给前端，
 * 页面一卸载（点设置页、切工具、关窗口）进度就没了，用户再回来只能看到"什么也没发生"。
 * 这里做两件事：
 *  1. 启动时从 Go 拉一次快照（TaskList），之后只吃增量（task:update），
 *     所以任何页面都能问"现在还在装什么、装到哪了"；
 *  2. 终态（done/error）在面板模式下用浏览器 Notification 通知一次——
 *     桌面版由 Go 弹 Windows 系统原生通知（右下角 Toast，见 main.go），前端不再重复弹。
 */

/** 已通知过的终态任务（id@endedAt），跨刷新去重 */
const NOTIFIED_KEY = "taskHub.notified"
/** 上次打开任务抽屉的时间：早于它的结束态不再算未读 */
const SEEN_KEY = "taskHub.lastSeen"

function readSeen(): number {
    const v = Number(localStorage.getItem(SEEN_KEY) || 0)
    return Number.isFinite(v) ? v : 0
}

function readNotified(): string[] {
    try {
        const raw = JSON.parse(localStorage.getItem(NOTIFIED_KEY) || "[]")
        return Array.isArray(raw) ? raw.map(String) : []
    } catch {
        return []
    }
}

const notified = new Set<string>(readNotified())

function markNotified(key: string) {
    if (notified.has(key)) return
    notified.add(key)
    // 只保留最近 60 条：这不是业务数据，够去重就行
    if (notified.size > 60) {
        const keep = Array.from(notified).slice(-60)
        notified.clear()
        keep.forEach((k) => notified.add(k))
    }
    try {
        localStorage.setItem(NOTIFIED_KEY, JSON.stringify(Array.from(notified)))
    } catch { /* 隐私模式下写不进去，只是多弹一次 */ }
}

/** 让浏览器允许弹通知；只有用户点了按钮才会走到这里（需要手势） */
async function askPermission(): Promise<boolean> {
    if (typeof Notification === "undefined") return false
    if (Notification.permission === "granted") return true
    if (Notification.permission === "denied") return false
    try {
        return (await Notification.requestPermission()) === "granted"
    } catch {
        return false
    }
}

export const useTasksStore = defineStore("tasks", () => {
    /** 全量快照：进行中在前，其余按最近更新倒序 */
    const tasks = ref<Task[]>([])
    /** 最近一次事件时间，让"x 秒前"这类相对时间能刷新 */
    const tick = ref(0)
    /** 浏览器通知权限：granted / denied / default / unsupported */
    const notifyPerm = ref<string>(typeof Notification === "undefined" ? "unsupported" : Notification.permission)
    /** 抽屉上一次被打开的时间：早于它的结束态就算“已看过” */
    const lastSeenAt = ref<number>(readSeen())

    const running = computed(() => tasks.value.filter((t) => t.status === "running"))
    const failed = computed(() => tasks.value.filter((t) => t.status === "error"))
    const runningCount = computed(() => running.value.length)
    /**
     * 已经结束、但用户还没看过的任务。
     *
     * 浏览器标签页关着时任务照样跑完：面板模式下没有原生通知卡，也没窗口弹通知，
     * 回来时总得知道“它完成了”。这里用 lastSeenAt 做未读判定，抽屉一开就算看过。
     */
    const unseen = computed(() => {
        const seen = lastSeenAt.value
        return tasks.value.filter((t) => t.status !== "running" && (t.endedAt || t.updatedAt) > seen)
    })
    const unseenCount = computed(() => unseen.value.length)
    /** 浮标要不要露头：还在跑，或者有还没看过的结果（完成/失败都算） */
    const hasTasks = computed(() => runningCount.value > 0 || unseenCount.value > 0)

    function sortInPlace() {
        tasks.value.sort((a, b) => {
            const ra = a.status === "running" ? 0 : 1
            const rb = b.status === "running" ? 0 : 1
            if (ra !== rb) return ra - rb
            if (ra === 0) return a.startedAt - b.startedAt
            return b.updatedAt - a.updatedAt
        })
    }

    function upsert(t: Task) {
        if (!t?.id) return
        const i = tasks.value.findIndex((x) => x.id === t.id)
        if (i >= 0) {
            // 快照与增量可能乱序到达：结束态不允许被旧的运行态覆盖
            const cur = tasks.value[i]
            if (cur.status !== "running" && t.status === "running") return
            tasks.value[i] = t
        } else {
            tasks.value.push(t)
        }
        sortInPlace()
        tick.value = Date.now()
    }

    /** 终态落账：桌面版交给 Go 的系统原生通知，面板/浏览器用 Notification */
    function notifyTerminal(t: Task) {
        if (t.status !== "done" && t.status !== "error") return
        const key = `${t.id}@${t.endedAt || t.updatedAt}`
        if (notified.has(key)) return
        markNotified(key)
        if (isNativeWindows()) return
        if (typeof Notification === "undefined" || Notification.permission !== "granted") return
        const title = t.status === "done" ? "任务完成" : "任务失败"
        const body = t.status === "done" ? t.title : `${t.title}：${t.note || "未知错误"}`
        try {
            const n = new Notification(title, { body, tag: key, requireInteraction: false })
            n.onclick = () => { try { window.focus() } catch { /* */ } }
        } catch { /* 部分浏览器要求 ServiceWorker，静默跳过 */ }
    }

    /** 全量替换：启动/重新可见时与 Go 对齐，顺便把历史终态标成"已通知" */
    function replaceAll(list: Task[] | null) {
        const arr = (list || []).filter((t) => t && t.id)
        tasks.value = arr
        sortInPlace()
        tick.value = Date.now()
        for (const t of arr) {
            if (t.status !== "running") markNotified(`${t.id}@${t.endedAt || t.updatedAt}`)
        }
    }

    /** 抽屉打开：把当前所有结束态标成已看过 */
    function markSeen() {
        lastSeenAt.value = Date.now()
        try { localStorage.setItem(SEEN_KEY, String(lastSeenAt.value)) } catch { /* */ }
    }

    /** 用户主动开通知：必须在点击处理里调用，否则浏览器不给 */
    async function enableNotify() {
        const ok = await askPermission()
        notifyPerm.value = typeof Notification === "undefined" ? "unsupported" : Notification.permission
        return ok
    }

    /** 移除一条已结束的任务（进行中的 Go 侧会拒绝） */
    async function dismiss(id: string) {
        try { await App.TaskClear(id) } catch { /* 已不在台账里 */ }
        tasks.value = tasks.value.filter((t) => t.id !== id)
    }

    /** 一次清掉所有已结束的 */
    async function clearFinished() {
        try { await App.TaskClearFinished() } catch { /* */ }
        tasks.value = tasks.value.filter((t) => t.status === "running")
    }

    return {
        tasks, tick, notifyPerm, running, failed, unseen, runningCount, unseenCount, hasTasks,
        upsert, replaceAll, notifyTerminal, markSeen, enableNotify, dismiss, clearFinished,
    }
})

/** 在 setup 外使用 */
export function useTasksStoreHook() {
    return useTasksStore(store)
}

/** 任务初始化只在整页生命周期里做一次：快照播种 + 订阅增量 */
let booting: Promise<void> | null = null
let subscribed = false

async function initTasks(): Promise<void> {
    const st = useTasksStoreHook()
    // 先确认平台能力：桌面版不弹浏览器通知（Go 已经弹了原生卡）
    await loadCaps().catch(() => null)
    // 首次运行：历史任务不应该全被当成“未读”闪一个浮标
    if (!readSeen()) st.markSeen()
    // 面板模式：先探一次通知权限（没给也不影响后续，抽屉里有手动入口）
    if (!isNativeWindows() && st.notifyPerm === "default") void st.enableNotify()
    if (!subscribed) {
        try {
            Events.On("task:update", (ev: any) => {
                const t: Task = ev?.data ?? ev
                if (!t?.id) return
                st.upsert(t)
                st.notifyTerminal(t)
            })
            subscribed = true
        } catch { /* 运行时还没就绪，留待下次调用重试 */ }
    }
    // 先订阅后播种：两步之间到达的增量也不会丢
    try {
        st.replaceAll(await App.TaskList())
    } catch { /* 快照取不到不影响后续增量 */ }
    if (!subscribed) booting = null
}

export function ensureTasks(): Promise<void> {
    if (!booting) booting = initTasks()
    return booting
}

/**
 * 页面挂载时把"还在跑"的安装任务搬回页面自己的进度表。
 *
 * 六个安装入口（docker 引擎 / mysql / php / redis / nginx / npm）的进度都只存在页面
 * 组件里，切页即失。这里用台账快照把 installProgress[版本] 与日志补回去，
 * 用户切走再回来看到的还是同一笔任务，而不是一个可点的"安装"按钮。
 *
 * progress 是页面自己的 Record<string, any>（{stage, percent, note}），
 * pushLog 是页面自己的日志追加函数。
 */
export async function restoreInstalls(
    tool: string,
    kind: string,
    progress: Record<string, any>,
    pushLog?: (txt: string) => void,
): Promise<void> {
    await ensureTasks().catch(() => null)
    const st = useTasksStoreHook()
    for (const t of st.tasks) {
        if (t.tool !== tool || t.kind !== kind) continue
        if (t.status !== "running") continue
        if (!t.name) continue
        progress[t.name] = { version: t.name, stage: t.stage, percent: t.percent, note: t.note }
        if (pushLog) {
            pushLog(`接管未完成的任务：${t.title}（${t.percent}%）`)
            const tail = (t.logs || []).slice(-5)
            tail.forEach((l) => pushLog(l))
        }
    }
}

/**
 * 取某类还在跑的任务（快照可能还没播种，先 ensure 一次）。
 *
 * docker 引擎安装、npm 版本切换这类页面状态是自己的形状（不是 Record<版本, progress>），
 * 用这个拿到原始 Task 后自行映射。
 */
export async function runningTasks(tool: string, kind: string): Promise<Task[]> {
    await ensureTasks().catch(() => null)
    const st = useTasksStoreHook()
    return st.tasks.filter((t) => t.tool === tool && t.kind === kind && t.status === "running")
}

export default {
    useTasksStore,
    useTasksStoreHook,
    ensureTasks,
    restoreInstalls,
    runningTasks,
}
