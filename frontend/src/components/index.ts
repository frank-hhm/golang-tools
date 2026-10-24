import { App, Plugin } from 'vue';
import LayoutBody from '@/components/layouts/LayoutBody.vue'
import selectIconModal from '@/components/common/select-icon-modal/index.vue'
import CommonPager from '@/components/common/pager/index.vue'
import CommonThemeSwitch from '@/components/common/theme-switch/index.vue'
import BodyViewer from '@/components/business/body-viewer/index.vue'

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
        // select-icon-modal：图标选择弹层，通过 type 属性切换图标源
        //  - type="iconfont"（默认）：本地字体图标，emit `icon-xxx`
        //  - type="arco"：Arco Design 图标库，emit 图标名（如 camera / clock-circle）
        app.component('select-icon-modal', selectIconModal);
        // body-viewer：报文查看器，支持 美化 / 原生 / 预览 / 可视化 四种视图
        app.component('body-viewer', BodyViewer);
    },
};
export { LayoutBody, CommonPager, CommonThemeSwitch, selectIconModal, BodyViewer };
