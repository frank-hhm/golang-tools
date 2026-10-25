/**
 * Canvas 时钟 Demo 的展示数据与文案
 *
 * 页面只负责摆放，配色 / 说明集中在这里改：新增一个预设就是往 CLOCK_PRESETS 里加一项。
 */

/** 与 common-canvas-clock 的六个颜色 props 同名，可直接 v-bind 过去 */
export interface ClockColors {
    ringColor: string;
    faceColor: string;
    numColor: string;
    hourColor: string;
    minuteColor: string;
    secondColor: string;
}

export interface ClockPreset {
    key: string;
    /** 格子下方的说明 */
    caption: string;
    /** 相对工具栏尺寸的比例：小尺寸演示格用它等比缩小 */
    scale: number;
    /** 空对象 = 完全跟随 Arco 主题，暗黑模式自动换肤 */
    colors: Partial<ClockColors>;
}

/** 前三个固定演示格 */
export const CLOCK_PRESETS: ClockPreset[] = [
    {
        key: "theme",
        caption: "跟随主题（浅色/暗黑自动换肤）",
        scale: 1,
        colors: {},
    },
    {
        key: "ocean",
        caption: "暗黑青绿表盘",
        scale: 1,
        colors: {
            ringColor: "#0b3d4d",
            faceColor: "#0f172a",
            numColor: "#7dd3fc",
            hourColor: "#94d8ea",
            minuteColor: "#38bdf8",
            secondColor: "#fbbf24",
        },
    },
    {
        key: "small",
        caption: "小尺寸（全部元素等比缩放）",
        scale: 0.5,
        colors: {
            ringColor: "#33291b",
            numColor: "#8d6e63",
            hourColor: "#4e342e",
            minuteColor: "#8d6e63",
            secondColor: "#ef5350",
        },
    },
];

/** 自定义配色格的初始值：暖紫，和上面三个预设都不撞色 */
export const CUSTOM_DEFAULT: ClockColors = {
    ringColor: "#3b2f63",
    faceColor: "#1b1533",
    numColor: "#c4b5fd",
    hourColor: "#ddd6fe",
    minuteColor: "#a78bfa",
    secondColor: "#f472b6",
};

/** 自定义配色弹窗里逐项调节的六个颜色 */
export const COLOR_FIELDS: { key: keyof ClockColors; label: string; hint: string }[] = [
    { key: "ringColor", label: "外圈", hint: "最外粗圆环，内阴影随它着色" },
    { key: "faceColor", label: "表盘底", hint: "缺省跟随卡片底色" },
    { key: "numColor", label: "刻度 / 文字", hint: "数字、日期与时间" },
    { key: "hourColor", label: "时针", hint: "粗短针" },
    { key: "minuteColor", label: "分针", hint: "长细针" },
    { key: "secondColor", label: "秒针", hint: "带光晕与中心轴点" },
];

/** 用法弹窗里的示例代码（模板里用插值渲染，不必手写转义） */
export const USAGE_SNIPPET = `<common-canvas-clock
  :size="320"
  :show-info="true"
  ring-color / face-color / num-color
  hour-color / minute-color / second-color
/>`;

/** 用法弹窗里的要点 */
export const USAGE_LINES: string[] = [
    "颜色缺省时读 Arco 主题变量（主色 / 卡片底 / 文本色），切换暗黑自动整套换肤；显式传值则固定配色。",
    "size 控制边长，刻度、文字、指针按比例等比缩放，并按 devicePixelRatio 渲染，高分屏不发虚。",
    "show-info 控制盘面上的日期、星期、年份水印与百分秒；关掉后只留指针。",
    "秒针连续平滑走动并带光晕，每帧 rAF 重绘；组件卸载会自动停掉动画与主题监听。",
];
