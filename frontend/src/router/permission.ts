import { ref } from "vue"
import type { Router } from "vue-router"

/**
 * 路由是否在解析中（含目标页 chunk 的下载）。
 * 布局层据此显示顶部进度条：远程服务器上切换页面时，用户至少知道"点了、在加载"，
 * 而不是以为没点上又点一次。
 */
export const routeLoading = ref(false)

/**
 * 安装进度条守卫。
 *
 * 有意做成函数、由 main.ts 显式传入 router，而不是在本模块 import router 后直接
 * 调用 router.beforeEach：本文件被 layouts/index.vue 引用，而 layouts 又在
 * router → modules/base 的导入链上，构成循环。dev 模式下原生 ESM 能容忍，
 * 但生产构建把整条链打进同一个 chunk 后，本模块可能在 router 赋值前就执行，
 * 结果是 `Cannot read properties of undefined (reading 'beforeEach')`，整个应用不挂载。
 */
export function installRouterProgress(router: Router) {
    router.beforeEach((to: any, from: any, next: any) => {
        if (to.path !== from.path) routeLoading.value = true
        next()
    })

    router.afterEach(() => {
        // 等一帧再收起，避免进度条在极快切换时闪一下就消失
        window.setTimeout(() => { routeLoading.value = false }, 160)
    })
}
