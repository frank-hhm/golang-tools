import { App, Plugin } from 'vue';
import LayoutBody from '@/components/layouts/LayoutBody.vue'
import selectIconModal from '@/components/common/icon/select-icon-modal/index.vue'
import CommonPager from '@/components/common/pager/index.vue'
import CommonThemeSwitch from '@/components/common/theme-switch/index.vue'
import CommonCanvasClock from '@/components/common/canvas-clock/index.vue'
import CommonBadge from '@/components/common/badge/index.vue'
import CommonModalTip from '@/components/common/modal-tip/index.vue'
import CommonDockerOff from '@/components/common/docker-off/index.vue'
import CommonStatIcon from '@/components/common/icon/stat-icon/index.vue'
import CommonStatCard from '@/components/common/stat-card/index.vue'
import CommonItemCard from '@/components/common/item-card/index.vue'
import CommonInfoRow from '@/components/common/info-row/index.vue'
import CommonLogModal from '@/components/common/log-modal/index.vue'
import CommonTitleBar from '@/components/common/title-bar/index.vue'
import CommonToolIcon from '@/components/common/icon/tool-icon/index.vue'
import CommonFileIcon from '@/components/common/icon/file-icon/index.vue'
import CommonTaskProgress from '@/components/common/task-progress/index.vue'
import CommonUploadPanel from '@/components/common/upload-panel/index.vue'
import MCard from '@/components/common/theme/m-card/index.vue'
import BodyViewer from '@/components/business/body-viewer/index.vue'
import DockerImages from '@/components/business/docker-images/index.vue'
import AnnotateCanvas from '@/components/business/annotate-canvas/AnnotateCanvas.vue'
import SshGenModal from '@/components/business/ssh-gen/SshGenModal.vue'

/**
 * 组件目录约定：
 *  - layouts/    布局组件：主窗口布局（index.vue）、工具窗空白布局（BlankLayout）、
 *                公共自绘标题栏（WinTitleBar）、内容区容器（LayoutBody）
 *  - common/     全局通用组件（纯 UI、跨 webview 复用，无后端依赖）：统一注册并加 `common-` 前缀，
 *                如 common-pager 分页、common-theme-switch 主题开关、select-icon-modal 图标选择弹层
 *                图标族集中在 common/icon/ 子目录：tool-icon 功能图标 / file-icon 文件图标 /
 *                stat-icon 指标图标 / select-icon-modal 图标选择弹层
 *  - business/   webview 业务组件（特定工具场景）：如 body-viewer 报文查看器（sniffer 专用）、
 *                annotate-canvas 标注编辑画布（截图页 / 文件查看器图片编辑共用）
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
        // common-stat-card：指标卡（彩色圆角图标 + 标签/数值同排 + 下方明细），首页/Nginx状态/Docker总览/磁盘总览共用
        app.component('common-stat-card', CommonStatCard);
        // common-item-card：列表实体卡外壳（选中竖条 + 状态/标题/操作头部 + 正文），实例列表/任务抽屉条目共用
        app.component('common-item-card', CommonItemCard);
        // common-info-row：通用信息行（标签 | 内容 | 右侧操作链接 + 分隔线），站点信息等详情面板复用
        app.component('common-info-row', CommonInfoRow);
        // common-log-modal：通用日志弹窗（标题 + 可滚动日志区 + 自动贴底 + 底部左提示/右操作）
        //  数据与轮询留在调用方：传 lines，用 #alert/#head/#tip/#operations 插槽填自己的横幅、状态、提示、按钮
        app.component('common-log-modal', CommonLogModal);
        // common-title-bar：页面往全局标题栏注入内容，#title 靠左（紧跟标题）、#extra 靠右（全局按钮前）
        app.component('common-title-bar', CommonTitleBar);
        // common-tool-icon：渲染功能配置里的图标值（icon-xxx 字体图标 / Arco 图标名两种写法都认）
        app.component('common-tool-icon', CommonToolIcon);
        // common-file-icon：Windows 11 风格的文件/目录图标（内联 SVG，按扩展名上色），磁盘管理与 FTP 文件列表共用
        app.component('common-file-icon', CommonFileIcon);
        // common-task-progress：任务/下载进度行（名称↔速度 / 整宽进度条 / 底部左ETA右操作），下载管理与任务抽屉共用
        app.component('common-task-progress', CommonTaskProgress);
        // common-upload-panel：公用上传面板（拖拽区+文件队列+切片续传），传输经 transport 注入与 UI 解耦，局域网互传等上传场景复用
        app.component('common-upload-panel', CommonUploadPanel);
        // m-card：项目自绘卡片外壳（统一封装 a-card），带 inheritBg 控制「是否继承上级背景」，供主题背景系统统一穿透
        app.component('m-card', MCard);
        // select-icon-modal：图标选择弹层，通过 type 属性切换图标源
        //  - type="iconfont"（默认）：本地字体图标，emit `icon-xxx`
        //  - type="arco"：Arco Design 图标库，emit 图标名（如 camera / clock-circle）
        app.component('select-icon-modal', selectIconModal);
        // body-viewer：报文查看器，支持 美化 / 原生 / 预览 / 可视化 四种视图
        app.component('body-viewer', BodyViewer);
        // docker-images：Docker 镜像当版本管（php/mysql/nginx/redis/npm 版本管理弹窗里用）：镜像列表、快速拉取、建实例、删除，另带本工具容器页签
        app.component('docker-images', DockerImages);
        // annotate-canvas：标注编辑画布（矩形/椭圆/箭头/画笔/马赛克/文字 + 撤销/旋转/裁剪/缩放），
        //  截图页预览区与文件查看器的图片编辑模式共用；宿主用 load()/exportDataURL() 收发图片
        app.component('annotate-canvas', AnnotateCanvas);
        // ssh-gen-modal：SSH 生成助手（生成密钥对 / 连接片段 / 部署公钥），FTP 连接表单与终端主机表单共用；
        //  宿主经 context 传主机上下文，生成密钥后 emit useKey 回填 keyPath
        app.component('ssh-gen-modal', SshGenModal);
    },
};
export { LayoutBody, CommonPager, CommonThemeSwitch, CommonCanvasClock, CommonBadge, CommonModalTip, CommonDockerOff, CommonStatIcon, CommonStatCard, CommonItemCard, CommonInfoRow, CommonLogModal, CommonTitleBar, CommonToolIcon, CommonFileIcon, CommonTaskProgress, CommonUploadPanel, MCard, selectIconModal, BodyViewer, DockerImages, AnnotateCanvas, SshGenModal };
