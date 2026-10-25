import { App, Plugin } from 'vue';
import LayoutBody from '@/components/layouts/LayoutBody.vue'
import selectIconModal from '@/components/common/select-icon-modal/index.vue'
import CommonPager from '@/components/common/pager/index.vue'
import CommonThemeSwitch from '@/components/common/theme-switch/index.vue'
import CommonCanvasClock from '@/components/common/canvas-clock/index.vue'
import CommonBadge from '@/components/common/badge/index.vue'
import CommonModalTip from '@/components/common/modal-tip/index.vue'
import CommonDockerOff from '@/components/common/docker-off/index.vue'
import CommonStatIcon from '@/components/common/stat-icon/index.vue'
import CommonTitleBar from '@/components/common/title-bar/index.vue'
import CommonToolIcon from '@/components/common/tool-icon/index.vue'
import BodyViewer from '@/components/business/body-viewer/index.vue'
import DockerImages from '@/components/business/docker-images/index.vue'

/**
 * 组件目录约定：
 *  - layouts/    布局组件：主窗口布局（index.vue）、工具窗空白布局（BlankLayout）、
 *                公共自绘标题栏（WinTitleBar）、内容区容器（LayoutBody）
 *  - common/     全局通用组件（纯 UI、跨 webview 复用，无后端依赖）：统一注册并加 `common-` 前缀，
 *                如 common-pager 分页、common-theme-switch 主题开关、select-icon-modal 图标选择弹层
 *  - business/   webview 业务组件（特定工具场景）：如 body-viewer 报文查看器（sniffer 专用）
 */
export const ComponentsPlugin: Plugin = {
    install(app: App) {
        app.component('layout-body', LayoutBody);
        // common-pager：分页组件（每页条数记忆到全局设置）
        app.component('common-pager', CommonPager);
        // common-theme-switch：浅色/暗黑切换开关，主窗口与工具窗标题栏共用，跨窗口经 storage 同步
        app.component('common-theme-switch', CommonThemeSwitch);
        // common-canvas-clock：Canvas 指针时钟（size/配色/平滑走动可配），由 test/clock-by-canvas 插件封装
        app.component('common-canvas-clock', CommonCanvasClock);
        // com-badge：状态圆点，颜色同 a-badge 的 status 语义色；animated 开启模糊光晕扩张
        app.component('com-badge', CommonBadge);
        // common-modal-tip：弹窗内的吸顶提示条（单行截断 + tooltip 看全文），不随错误串长度撑开弹窗
        app.component('common-modal-tip', CommonModalTip);
        // common-docker-off：页面级「有 Docker 实例但引擎没起」告示，自带面板入口与重试
        app.component('common-docker-off', CommonDockerOff);
        // common-stat-icon：指标卡自绘 SVG 图标（cpu/mem/uptime/go/disk/net），
        // value 传 0-100 时按数据驱动形态：CPU 脉冲频率、内存液位、磁盘进度条
        app.component('common-stat-icon', CommonStatIcon);
        // common-title-bar：页面往全局标题栏注入内容，#title 靠左（紧跟标题）、#extra 靠右（全局按钮前）
        app.component('common-title-bar', CommonTitleBar);
        // common-tool-icon：渲染功能配置里的图标值（icon-xxx 字体图标 / Arco 图标名两种写法都认）
        app.component('common-tool-icon', CommonToolIcon);
        // select-icon-modal：图标选择弹层，通过 type 属性切换图标源
        //  - type="iconfont"（默认）：本地字体图标，emit `icon-xxx`
        //  - type="arco"：Arco Design 图标库，emit 图标名（如 camera / clock-circle）
        app.component('select-icon-modal', selectIconModal);
        // body-viewer：报文查看器，支持 美化 / 原生 / 预览 / 可视化 四种视图
        app.component('body-viewer', BodyViewer);
        // docker-images：Docker 镜像当版本管（php/mysql/nginx/redis/npm 版本管理弹窗里用）：镜像列表、快速拉取、建实例、删除，另带本工具容器页签
        app.component('docker-images', DockerImages);
    },
};
export { LayoutBody, CommonPager, CommonThemeSwitch, CommonCanvasClock, CommonBadge, CommonModalTip, CommonDockerOff, CommonStatIcon, CommonTitleBar, CommonToolIcon, selectIconModal, BodyViewer, DockerImages };
