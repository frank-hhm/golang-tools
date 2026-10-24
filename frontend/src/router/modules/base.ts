import BasicLayout from "@/components/layouts/index.vue";
import BlankLayout from "@/components/layouts/BlankLayout.vue";

/** 工具路由定义（相对路径，供多种父布局复用） */
const TOOLS = [
    { key: "Sniffer", menu_name: "抓包工具", path: "tools/sniffer", component: () => import('@/views/tools/sniffer/index.vue') },
    { key: "Usbserial", menu_name: "USB调试", path: "tools/usbserial", component: () => import('@/views/tools/usbserial/index.vue') },
    { key: "Redis", menu_name: "Redis管理", path: "tools/redis", component: () => import('@/views/tools/redis/index.vue') },
    { key: "Mysql", menu_name: "MySQL管理", path: "tools/mysql", component: () => import('@/views/tools/mysql/index.vue') },
    { key: "Php", menu_name: "PHP管理", path: "tools/php", component: () => import('@/views/tools/php/index.vue') },
    { key: "Nginx", menu_name: "Nginx管理", path: "tools/nginx", component: () => import('@/views/tools/nginx/index.vue') },
    { key: "Npm", menu_name: "npm管理", path: "tools/npm", component: () => import('@/views/tools/npm/index.vue') },
    { key: "Timestamp", menu_name: "时间戳转换", path: "tools/timestamp", component: () => import('@/views/tools/timestamp/index.vue') },
    { key: "Strrandom", menu_name: "随机字符串生成", path: "tools/strrandom", component: () => import('@/views/tools/strrandom/index.vue') },
    { key: "Encdec", menu_name: "文字加密解密", path: "tools/encdec", component: () => import('@/views/tools/encdec/index.vue') },
    { key: "Hexconvert", menu_name: "进制转换", path: "tools/hexconvert", component: () => import('@/views/tools/hexconvert/index.vue') },
    { key: "ImageBase64", menu_name: "图片base64编码", path: "tools/imageBase64", component: () => import('@/views/tools/imageBase64/index.vue') },
    { key: "Qrcode", menu_name: "二维码生成", path: "tools/qrcode", component: () => import('@/views/tools/qrcode/index.vue') },
    { key: "Websocket", menu_name: "WebSocket调试", path: "tools/websocket", component: () => import('@/views/tools/websocket/index.vue') },
    { key: "Screenshot", menu_name: "截图预览", path: "tools/screenshot", component: () => import('@/views/tools/screenshot/index.vue') },
    { key: "Video", menu_name: "视频录制", path: "tools/video", component: () => import('@/views/tools/video/index.vue') },
    { key: "Colorpicker", menu_name: "颜色取色器", path: "tools/colorpicker", component: () => import('@/views/tools/colorpicker/index.vue') },
    { key: "Settings", menu_name: "设置", path: "tools/settings", component: () => import('@/views/tools/settings/index.vue') },

];

/**
 * 生成子路由。
 * @param namePrefix 路由 name 前缀（保证全局唯一）
 * @param extraMeta 追加 meta（如 blank：空白/全屏布局下 LayoutBody 裸渲染）
 */
const buildChildren = (namePrefix: string, extraMeta: Record<string, unknown> = {}) =>
    TOOLS.map((t) => ({
        path: t.path,
        name: namePrefix + "tools" + t.key,
        meta: {
            auth: false,
            menu_name: t.menu_name,
            ...extraMeta,
        },
        component: t.component,
    }));

export default [{
    path: '/',
    name: '/',
    header: '/',
    meta: {
        menu_name: "首页",
    },
    redirect: {
        path: `/index`
    },
    component: BasicLayout,
    children: [
        {
            path: '/index',
            name: '/index',
            meta: {
                auth: false,
                menu_name: "首页",
            },
            component: () => import('@/views/index/index.vue')
        },
        {
            // Go → Arco Message 消息桥接 demo（未全局推广，仅供验证）
            path: '/demo/gomessage',
            name: '/demo/gomessage',
            meta: {
                auth: false,
                menu_name: "Go消息Demo",
            },
            component: () => import('@/views/demo/gomessage/index.vue')
        },
        {
            // common-canvas-clock 时钟组件 demo（由 test/clock-by-canvas 插件封装）
            path: '/demo/clock',
            name: '/demo/clock',
            meta: {
                auth: false,
                menu_name: "Canvas时钟Demo",
            },
            component: () => import('@/views/demo/clock/index.vue')
        },
        ...buildChildren('/')
    ]
}, {
    // 原生新窗口（非全屏）：空白布局，无面包屑与菜单
    path: '/win',
    name: '/win',
    meta: {
        blank: true,
    },
    component: BlankLayout,
    children: [
        ...buildChildren('/win/', { blank: true }),
        {
            // Go 消息桥接 demo 的新窗口路由（首页 test 卡片经 OpenToolWindow 拼 /win 前缀）
            path: 'demo/gomessage',
            name: '/win/demo/gomessage',
            meta: {
                auth: false,
                blank: true,
                menu_name: "Go消息Demo",
            },
            component: () => import('@/views/demo/gomessage/index.vue')
        },
        {
            // Canvas 时钟 demo 的新窗口路由
            path: 'demo/clock',
            name: '/win/demo/clock',
            meta: {
                auth: false,
                blank: true,
                menu_name: "Canvas时钟Demo",
            },
            component: () => import('@/views/demo/clock/index.vue')
        }
    ]
}, {
    // 录制悬浮小窗：独立轻量页面（无布局/面包屑），由后端 VideoOpenWidget 开窗加载
    path: '/widget/recorder',
    name: '/widget/recorder',
    meta: {
        auth: false,
        blank: true,
        menu_name: "屏幕录制",
    },
    component: () => import('@/views/widget/recorder.vue')
}, {
    // 原生消息通知小窗：由后端 ShowMessage 开窗加载（屏幕右上角，自动消失）
    path: '/widget/toast',
    name: '/widget/toast',
    meta: {
        auth: false,
        blank: true,
        menu_name: "通知",
    },
    component: () => import('@/views/widget/toast.vue')
}, {
    // 系统风格通知卡片小窗：由后端 ShowNotification 开窗加载（屏幕右下角，驻留至手动关闭）
    path: '/widget/notify',
    name: '/widget/notify',
    meta: {
        auth: false,
        blank: true,
        menu_name: "通知",
    },
    component: () => import('@/views/widget/notify.vue')
}, {
    // 截图选区编辑浮层：全屏无边框置顶，由后端 OpenCaptureOverlay 开窗加载。
    // 背景是 Go 冻住的那一帧屏幕，选区/控制点/工具栏/标注全在这里画。
    path: '/widget/capture',
    name: '/widget/capture',
    meta: {
        auth: false,
        blank: true,
        menu_name: "截图",
    },
    component: () => import('@/views/widget/capture.vue')
}, {
    // 图钉置顶：把一张截图钉成无边框置顶小窗，由后端 OpenPinnedImage 开窗，URL 带 ?id= 拉图。
    path: '/widget/pin',
    name: '/widget/pin',
    meta: {
        auth: false,
        blank: true,
        menu_name: "图钉",
    },
    component: () => import('@/views/widget/pin.vue')
}]
