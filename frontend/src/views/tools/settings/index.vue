<template>
    <!-- 布局参考 Redis 工具页：整页左右分栏，左侧分类导航 + 右侧主面板 -->
    <a-row class="settings-page" :gutter="6">
        <!-- 左侧：设置分类 -->
        <a-col :span="6" class="side-col">
            <a-card class="side-card" size="small" :body-style="{ padding: 0 }">
                <template #title>设置</template>
                <div class="cat-list">
                    <div v-for="t in SETTING_TABS" :key="t.key" class="cat-item"
                        :class="{ active: activeTab === t.key }" @click="activeTab = t.key">
                        <component :is="t.icon" class="cat-icon" />
                        <span class="cat-name">{{ t.label }}</span>
                        <icon-right v-if="activeTab === t.key" class="cat-arrow" />
                    </div>
                </div>
            </a-card>
        </a-col>

        <!-- 右侧：主区域，所有设置项修改后自动保存 -->
        <a-col :span="18" class="main-col">
            <a-card class="main-panel" :body-style="{ padding: 0 }">
                <template #title>
                    <a-space>
                        <component :is="activeTabMeta.icon" />
                        <span class="panel-title">{{ activeTabMeta.label }}</span>
                    </a-space>
                </template>
                <template #extra>
                    <span class="auto-save-tip">{{ saving ? "正在保存…" : "修改后自动保存" }}</span>
                </template>

                <div class="tab-content">
                    <!-- ===================== 通用设置 ===================== -->
                    <a-card v-show="activeTab === 'general'" class="pane-card">
                        <template #title>应用与外观</template>
                        <a-form :model="form" :style="{ maxWidth: '720px' }" layout="vertical">
                            <a-form-item field="general.appName">
                                <template #label>应用名称</template>
                                <a-input v-model="form.general.appName" placeholder="Tools" :style="{ maxWidth: '280px' }"
                                    @blur="scheduleSave(0)" @press-enter="scheduleSave(0)" />
                                <span class="hint">窗口标题、托盘提示显示的名称，修改后立即生效。</span>
                            </a-form-item>
                            <a-form-item field="general.appLogo">
                                <template #label>应用 Logo</template>
                                <div class="logo-picker">
                                    <div class="logo-preview">
                                        <img v-if="logoPreview" :src="logoPreview" class="logo-img" />
                                        <span v-else class="logo-placeholder">无</span>
                                    </div>
                                    <a-space>
                                        <a-button size="small" @click="pickLogo">选择图片</a-button>
                                        <a-button size="small" status="danger" v-if="form.general.appLogo" @click="clearLogo">清除</a-button>
                                    </a-space>
                                </div>
                                <span class="hint">建议 PNG 正方形（如 128x128），用于浮动球中心图标。留空使用内置默认。</span>
                            </a-form-item>
                            <a-form-item field="general.theme">
                                <template #label>主题模式</template>
                                <a-radio-group v-model="form.general.theme" type="button" @change="onThemeChange">
                                    <a-radio value="light">浅色</a-radio>
                                    <a-radio value="dark">暗黑</a-radio>
                                    <a-radio value="auto">跟随系统</a-radio>
                                </a-radio-group>
                                <span class="hint">切换后立即生效，所有窗口同步。</span>
                            </a-form-item>
                            <a-form-item field="autoStart">
                                <template #label>开机默认启动</template>
                                <a-switch  type="round" v-model="form.general.autoStart" @change="scheduleSave(0)" />
                                <span class="hint">开启后 Windows 登录时自动启动应用。</span>
                            </a-form-item>
                        </a-form>
                    </a-card>

                    <!-- ===================== 目录设置 ===================== -->
                    <a-card v-show="activeTab === 'paths'" class="pane-card">
                        <template #title>全局目录布局</template>
                        <a-form :model="form" :style="{ maxWidth: '720px' }" layout="vertical">
                            <a-form-item field="paths.plugins">
                                <template #label>插件目录（plugins）</template>
                                <div class="path-row">
                                    <a-input v-model="form.paths.plugins" placeholder="留空使用默认（应用目录/plugins）"
                                        allow-clear @blur="scheduleSave(0)" @press-enter="scheduleSave(0)"
                                        @clear="scheduleSave(0)" />
                                    <a-button @click="pickBaseDir('plugins', '选择插件目录')">选择目录</a-button>
                                </div>
                                <div class="hint">服务程序安装位置：PHP/Nginx/MySQL/Redis/npm 等按服务名建子目录（plugins/服务名）。修改后新安装/下载的服务落到新目录。</div>
                            </a-form-item>
                            <a-form-item field="paths.data">
                                <template #label>数据目录（data）</template>
                                <div class="path-row">
                                    <a-input v-model="form.paths.data" placeholder="留空使用默认（应用目录/data）"
                                        allow-clear @blur="scheduleSave(0)" @press-enter="scheduleSave(0)"
                                        @clear="scheduleSave(0)" />
                                    <a-button @click="pickBaseDir('data', '选择数据目录')">选择目录</a-button>
                                </div>
                                <div class="hint">服务配置缓存与实例工作目录（data/服务名）；截图另存、录像、站点根等文件的默认落点（data/screenshots、data/videos、data/www）。保存后各另存对话框默认目录随即使用新路径。</div>
                            </a-form-item>
                            <a-form-item field="log.enabled">
                                <template #label>启用文件日志</template>
                                <a-switch type="round" v-model="form.log.enabled" @change="scheduleSave(0)" />
                                <span class="hint">关闭后不再写入日志文件（仅开发控制台可见）。</span>
                            </a-form-item>
                            <a-form-item field="paths.logs">
                                <template #label>日志目录（logs）</template>
                                <div class="path-row">
                                    <a-input v-model="form.paths.logs" placeholder="留空使用默认（应用目录/logs）"
                                        allow-clear @blur="scheduleSave(0)" @press-enter="scheduleSave(0)"
                                        @clear="scheduleSave(0)" />
                                    <a-button @click="pickBaseDir('logs', '选择日志目录')">选择目录</a-button>
                                    <a-button v-if="activePath" @click="openLogDir">打开日志目录</a-button>
                                </div>
                                <div class="hint">程序与各服务日志的根目录，按服务名建子目录：logs/app 程序日志、logs/mysql SQL 日志、logs/php 实例服务日志等。开启文件日志后 info / warn / error 全部记录，按“级别 + 当前日期”自动分文件（如 Info20261022.log、Warn20261022.log、Error20261022.log）。当前程序日志目录：{{ activePath || "（未加载）" }}</div>
                            </a-form-item>
                        </a-form>
                    </a-card>

                    <!-- ===================== 浮动球设置 ===================== -->
                    <a-card v-show="activeTab === 'ball'" class="pane-card">
                        <template #title>桌面浮动球</template>
                        <a-form :model="form" :style="{ maxWidth: '720px' }" layout="vertical">
                            <a-form-item field="ballEnabled">
                                <template #label>显示桌面浮动球</template>
                                <a-switch v-model="form.ball.enabled" type="round" @change="scheduleSave(0)" />
                                <span class="hint">开启后浮动球常驻屏幕右下角（独立原生小窗，置顶、可拖动，点击展开快捷功能）。修改后立即显隐，并在下次启动时自动恢复。</span>
                            </a-form-item>
                            <a-form-item field="ballIcon">
                                <template #label>浮动球图标</template>
                                <div class="logo-picker">
                                    <div class="ball-icon-preview">
                                        <i v-if="isIconfontIcon(form.ball.icon)" class="icon ball-icon-glyph"
                                            :class="form.ball.icon"></i>
                                        <component v-else-if="form.ball.icon" :is="iconComp(form.ball.icon)"
                                            class="ball-icon-glyph" />
                                        <span v-else class="logo-placeholder">默认</span>
                                    </div>
                                    <a-space>
                                        <a-button size="small" @click="openBallIconModal">选择图标</a-button>
                                        <a-button size="small" status="danger" v-if="form.ball.icon"
                                            @click="clearBallIcon">清除</a-button>
                                    </a-space>
                                    <select-icon-modal ref="ballIconModalRef"
                                        :type="isIconfontIcon(form.ball.icon) ? 'iconfont' : 'arco'"
                                        @change="onBallIconChange"></select-icon-modal>
                                </div>
                                <span class="hint">浮动球中心图标（图标库或 Arco），颜色自动跟随主题正文色 var(--color-text-1)，球面底色跟随 var(--color-bg-1)。留空使用应用 Logo/内置默认。</span>
                            </a-form-item>
                        </a-form>
                    </a-card>

                    <!-- ===================== 工具列表编辑 ===================== -->
                    <div v-show="activeTab === 'tools'" class="tools-pane">
                        <a-card class="pane-card tools-card" :body-style="{ padding: 0 }">
                            <template #title>功能列表</template>
                            <template #extra>
                                <a-space>
                                    <span class="hint hint-inline">快捷键为系统全局热键</span>
                                    <a-button size="small" @click="resetTools">恢复默认列表</a-button>
                                    <a-button type="primary" size="small" @click="openAdd">
                                        <template #icon><icon-plus /></template>新增功能
                                    </a-button>
                                </a-space>
                            </template>
                            <div class="tools-table-wrap" ref="toolsTableRef">
                                <a-table :data="tools" :pagination="false" size="small" row-key="id" :column-resizable="true"
                                    :scroll="tableScroll">
                                    <template #columns>
                                        <a-table-column title="排序" :width="88">
                                            <template #cell="{ rowIndex }">
                                                <a-space :size="2">
                                                    <a-button size="mini" :disabled="rowIndex === 0" @click="move(rowIndex, -1)">
                                                        <icon-up />
                                                    </a-button>
                                                    <a-button size="mini" :disabled="rowIndex === tools.length - 1"
                                                        @click="move(rowIndex, 1)">
                                                        <icon-down />
                                                    </a-button>
                                                </a-space>
                                            </template>
                                        </a-table-column>
                                        <a-table-column title="标题" data-index="name" :width="150" ellipsis tooltip />
                                        <a-table-column title="内置功能" data-index="key" :width="130" ellipsis tooltip />
                                        <a-table-column title="图标" data-index="icon" :width="140">
                                            <template #cell="{ record }">
                                                <span v-if="record.icon" class="icon-cell">
                                                    <i v-if="isIconfontIcon(record.icon)" class="icon iconfont-preview"
                                                        :class="record.icon" />
                                                    <component v-else :is="iconComp(record.icon)" class="arco-preview" />
                                                    <span class="icon-text" :title="record.icon">{{ iconLabel(record.icon) }}</span>
                                                </span>
                                                <span v-else class="off">—</span>
                                            </template>
                                        </a-table-column>
                                        <a-table-column title="快捷" :width="60">
                                            <template #cell="{ record }">
                                                <icon-check-circle v-if="record.quick" class="on" />
                                                <span v-else class="off">—</span>
                                            </template>
                                        </a-table-column>
                                        <a-table-column title="单例" :width="60">
                                            <template #cell="{ record }">
                                                <icon-check-circle v-if="record.single" class="on" />
                                                <span v-else class="off">—</span>
                                            </template>
                                        </a-table-column>
                                        <a-table-column title="全屏" :width="60">
                                            <template #cell="{ record }">
                                                <icon-check-circle v-if="record.fullscreen" class="on" />
                                                <span v-else class="off">—</span>
                                            </template>
                                        </a-table-column>
                                        <a-table-column title="快捷键" data-index="shortcut" :width="120" ellipsis tooltip>
                                            <template #cell="{ record }">
                                                <a-tag v-if="record.shortcut" size="small">{{ record.shortcut }}</a-tag>
                                                <span v-else class="off">—</span>
                                            </template>
                                        </a-table-column>
                                        <a-table-column title="操作" :width="120" fixed="right">
                                            <template #cell="{ record, rowIndex }">
                                                <a-space :size="6">
                                                    <a-link @click="openEdit(rowIndex)">编辑</a-link>
                                                    <a-popconfirm content="确定删除该功能？" @ok="del(rowIndex)">
                                                        <a-link status="danger">删除</a-link>
                                                    </a-popconfirm>
                                                </a-space>
                                            </template>
                                        </a-table-column>
                                    </template>
                                </a-table>
                            </div>
                        </a-card>
                    </div>
                </div>
            </a-card>
        </a-col>
    </a-row>

        <!-- ===================== 工具编辑弹窗 ===================== -->
        <a-modal v-model:visible="editVisible" :title="editIndex < 0 ? '新增功能' : '编辑功能'" :width="520" @ok="confirmEdit"
            @cancel="editVisible = false" ok-text="确定" cancel-text="取消">
            <a-form :model="editForm" layout="vertical" class="edit-form">
                <a-form-item field="key" label="内置功能">
                    <a-select v-model="editForm.key" placeholder="选择要绑定的内置功能" allow-search @change="onBuiltinChange">
                        <a-option v-for="b in BUILTINS" :key="b.key" :value="b.key" :label="b.name">
                            <span class="builtin-option">
                                <i v-if="isIconfontIcon(b.icon)" class="icon builtin-icon" :class="b.icon" />
                                <component v-else :is="iconComp(b.icon)" class="builtin-icon" />
                                <span>{{ b.name }}</span>
                            </span>
                        </a-option>
                    </a-select>
                </a-form-item>
                <a-form-item field="name" label="标题">
                    <a-input v-model="editForm.name" placeholder="显示名称" />
                </a-form-item>
                <a-form-item field="icon" label="图标">
                    <a-input v-model="editForm.icon" placeholder="选择图标" allow-clear>
                        <template #prefix>
                            <i v-if="isIconfontIcon(editForm.icon)" class="icon form-icon" :class="editForm.icon" />
                            <component v-else-if="editForm.icon" :is="iconComp(editForm.icon)" class="form-icon" />
                        </template>
                        <template #append>
                            <a-button @click="() => { proxy?.$refs['selectModalIconRef']?.open(); }">图标</a-button>
                            <select-icon-modal ref="selectModalIconRef"
                                :type="isIconfontIcon(editForm.icon) ? 'iconfont' : 'arco'"
                                @change="iconChange"></select-icon-modal>
                        </template>
                    </a-input>
                    <div class="hint">支持 Arco 图标名（如 camera）与图标库 icon-xxx（如 icon-lianjie），点击右侧“图标”选择。</div>
                </a-form-item>
                <a-row :gutter="12">
                    <a-col :span="6">
                        <a-form-item field="quick" label="浮动球快捷">
                            <a-switch v-model="editForm.quick" />
                        </a-form-item>
                    </a-col>
                    <a-col :span="6">
                        <a-form-item field="single" label="单例窗口">
                            <a-switch v-model="editForm.single" />
                        </a-form-item>
                    </a-col>
                    <a-col :span="6">
                        <a-form-item field="fullscreen" label="全屏启动">
                            <a-switch v-model="editForm.fullscreen" />
                        </a-form-item>
                    </a-col>
                    <a-col :span="6">
                        <a-form-item field="disableFullscreen" label="禁用全屏">
                            <a-switch v-model="editForm.disableFullscreen" />
                        </a-form-item>
                    </a-col>
                </a-row>
                <a-row :gutter="12">
                    <a-col :span="12">
                        <a-form-item field="width" label="视口宽（0=默认）">
                            <a-input-number v-model="editForm.width" :min="0" :step="10" hide-button />
                        </a-form-item>
                    </a-col>
                    <a-col :span="12">
                        <a-form-item field="height" label="视口高（0=默认）">
                            <a-input-number v-model="editForm.height" :min="0" :step="10" hide-button />
                        </a-form-item>
                    </a-col>
                </a-row>
                <a-form-item field="shortcut" label="全局快捷键">
                    <a-input :model-value="editForm.shortcut" readonly :placeholder="recording ? '请按下组合键…（Esc 取消）' : '点击后直接按下组合键（如 Ctrl+Alt+S），留空不设置'"
                        @click="startRecord" @keydown="onShortcutKeydown" allow-clear @clear="editForm.shortcut = ''">
                        <template #suffix>
                            <span v-if="recording" class="rec-tip">录制中</span>
                        </template>
                    </a-input>
                    <div class="hint">点击输入框后直接按下你想要的组合键即可自动录入；按 Esc 退出录制。</div>
                </a-form-item>
            </a-form>
        </a-modal>
</template>

<script setup lang="ts">
import { computed, createVNode, getCurrentInstance, nextTick, onBeforeUnmount, onMounted, reactive, ref, render as renderVNode, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import * as ArcoIconComps from "@arco-design/web-vue/es/icon";
import iconData from "@/components/common/select-icon-modal/icon.json";
import { applyTheme, type ThemeMode } from "@/utils/theme";
import {
    arcoIconComp as iconComp,
    DEFAULT_TOOL_ICONS,
    iconLabel,
    isIconfontIcon,
    normalizeToolIcon,
} from "@/utils/tool-icon";

const { proxy } = getCurrentInstance() as any;
const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

interface BuiltinDef {
    key: string;
    name: string;
    path?: string;
    event?: string;
    icon: string;
    single?: boolean;
    fullscreen?: boolean;
    disableFullscreen?: boolean;
    width?: number;
    height?: number;
    quick?: boolean;
    shortcut?: string;
}

type BuiltinExtra = Omit<BuiltinDef, "key" | "name" | "icon">;

/** 内置功能目录：图标统一从 tool-icon 的默认映射获取（Arco + icon.json） */
function builtin(key: string, name: string, extra: BuiltinExtra = {}): BuiltinDef {
    return { key, name, icon: DEFAULT_TOOL_ICONS[key] || "apps", ...extra };
}

/** 内置功能目录：新增下拉与“恢复默认列表”共用，避免默认数据分叉 */
const BUILTINS: BuiltinDef[] = [
    builtin("screenshot", "截图", { event: "screenshot", quick: true, shortcut: "Ctrl+Alt+A", disableFullscreen: true }),
    builtin("colorpicker", "颜色取色器", { path: "/tools/colorpicker", single: true, width: 480, height: 560, quick: true }),
    builtin("video", "视频录制", { path: "/tools/video", single: true, fullscreen: true, quick: true }),
    builtin("qrcode", "二维码生成", { path: "/tools/qrcode" }),
    builtin("timestamp", "时间戳转换", { path: "/tools/timestamp" }),
    builtin("strrandom", "随机字符串生成", { path: "/tools/strrandom" }),
    builtin("encdec", "文字加密解密", { path: "/tools/encdec" }),
    builtin("hexconvert", "进制转换", { path: "/tools/hexconvert" }),
    builtin("imageBase64", "图片base64", { path: "/tools/imageBase64" }),
    builtin("sniffer", "抓包工具", { path: "/tools/sniffer", single: true, fullscreen: true, quick: true }),
    builtin("websocket", "WebSocket调试", { path: "/tools/websocket", fullscreen: true }),
    builtin("usbserial", "USB调试", { path: "/tools/usbserial" }),
    builtin("redis", "Redis管理", { path: "/tools/redis", single: true, fullscreen: true }),
    builtin("mysql", "MySQL管理", { path: "/tools/mysql", single: true, fullscreen: true }),
    builtin("php", "PHP管理", { path: "/tools/php", single: true, width: 1200, height: 800 }),
    builtin("nginx", "Nginx管理", { path: "/tools/nginx", single: true, width: 1200, height: 800 }),
    builtin("npm", "Npm管理", { path: "/tools/npm", single: true, width: 1200, height: 800 }),
    builtin("settings", "设置", { path: "/tools/settings", single: true, quick: true }),
];

interface ToolRow {
    id: string; key: string; name: string; path: string; event: string; icon: string;
    single: boolean; fullscreen: boolean; disableFullscreen: boolean; width: number; height: number; shortcut: string; quick: boolean;
}

/** 完整配置（保存时整体回传） */
const fullConfig = ref<any>({ log: { enabled: true, level: "info", path: "" }, ball: { enabled: false, icon: "" }, general: { autoStart: false, appName: "Tools", appLogo: "", theme: "light" }, paths: { plugins: "", data: "", logs: "" }, tools: [] });
const form = reactive<{
    log: { enabled: boolean; level: string; path: string };
    ball: { enabled: boolean; icon: string };
    general: { autoStart: boolean; appName: string; appLogo: string; theme: string };
    paths: { plugins: string; data: string; logs: string };
}>({
    log: { enabled: true, level: "info", path: "" },
    ball: { enabled: false, icon: "" },
    general: { autoStart: false, appName: "Tools", appLogo: "", theme: "light" },
    paths: { plugins: "", data: "", logs: "" },
});
const tools = ref<ToolRow[]>([]);

const saving = ref(false);
let saveTimer = 0;
let saveChain: Promise<void> = Promise.resolve();

/** 设置分类：左侧导航与右侧面板标题共用 */
const SETTING_TABS = [
    { key: "general", label: "通用", icon: "IconSettings" },
    { key: "paths", label: "目录", icon: "IconFolder" },
    { key: "ball", label: "浮动球", icon: "IconDesktop" },
    { key: "tools", label: "工具", icon: "IconApps" },
];
const activeTab = ref("general");
const activeTabMeta = computed(() => SETTING_TABS.find((t) => t.key === activeTab.value) || SETTING_TABS[0]);

/** 主题切换：立即应用到 DOM（localStorage 同步广播到其他窗口），并自动持久化到配置 */
function onThemeChange(val: any) {
    applyTheme(String(val) as ThemeMode);
    scheduleSave(0);
}
const activePath = ref("");
const logoPreview = ref("");

async function pickLogo() {
    try {
        const path = await GoTools?.ChooseAppLogo();
        if (path) {
            form.general.appLogo = path;
            // 先持久化，再按已保存的路径生成预览（base64）
            await saveNow();
            const dataUrl = await GoTools?.GetAppLogo();
            logoPreview.value = dataUrl || "";
        }
    } catch { /* cancelled */ }
}

/** 清除自定义 Logo：保存后回退到内置默认图并刷新预览 */
async function clearLogo() {
    form.general.appLogo = "";
    await saveNow();
    try { logoPreview.value = (await GoTools?.GetAppLogo()) || ""; } catch { logoPreview.value = ""; }
}

/* ---------------- 浮动球球心图标 ---------------- */
const ballIconModalRef = ref<any>(null);
function openBallIconModal() {
    ballIconModalRef.value?.open?.();
}

/**
 * 图标值（icon-xxx / Arco 名）→ 128px 黑色字形 alpha 遮罩 PNG base64。
 * Go 侧浮动球只取遮罩形状，按主题文字色（text-1）着色，因此这里固定用黑色绘制。
 */
async function iconToMaskPng(icon: string, size = 128): Promise<string> {
    const val = String(icon || "").trim();
    if (!val) return "";
    const canvas = document.createElement("canvas");
    canvas.width = size;
    canvas.height = size;
    const ctx = canvas.getContext("2d");
    if (!ctx) return "";
    ctx.fillStyle = "#000";
    if (isIconfontIcon(val)) {
        // 图标库：查 icon.json 的 unicode，用页面已加载的 "icon" 字体画字形
        const hit = (iconData as any[]).find((i) => "icon-" + i.font_class === val);
        if (!hit?.unicode) return "";
        const ch = String.fromCharCode(parseInt(String(hit.unicode), 16));
        try { await (document as any).fonts?.load(`100px "icon"`, ch); } catch { /* 字体已由样式全局加载，失败时直接绘制 */ }
        ctx.font = `${Math.round(size * 0.82)}px "icon"`;
        ctx.textAlign = "center";
        ctx.textBaseline = "middle";
        ctx.fillText(ch, size / 2, size / 2 + size * 0.02);
    } else {
        // Arco：离屏渲染图标组件 → 序列化 SVG（currentColor 独立解析为黑色）→ 画入 canvas
        const compName = iconComp(val);
        const compObj = compName ? (ArcoIconComps as any)[compName] : null;
        if (!compObj) return "";
        const host = document.createElement("div");
        host.style.cssText = `font-size:${size}px;color:#000;position:absolute;left:-9999px;`;
        document.body.appendChild(host);
        try {
            renderVNode(createVNode(compObj, null), host);
            const svgEl = host.querySelector("svg");
            if (!svgEl) return "";
            svgEl.setAttribute("width", String(size));
            svgEl.setAttribute("height", String(size));
            const src = new XMLSerializer().serializeToString(svgEl);
            const img = new Image();
            const ok = await new Promise<boolean>((res) => {
                img.onload = () => res(true);
                img.onerror = () => res(false);
                img.src = "data:image/svg+xml;charset=utf-8," + encodeURIComponent(src);
            });
            if (!ok) return "";
            ctx.drawImage(img, 0, 0, size, size);
        } finally {
            renderVNode(null, host);
            host.remove();
        }
    }
    const url = canvas.toDataURL("image/png");
    return url.includes(",") ? url.split(",")[1] : "";
}

/** 图标选择回填：生成遮罩上传 Go 侧 → 配置记录图标值 → 自动保存 */
async function onBallIconChange(val: any) {
    const icon = String(val ?? "");
    let b64 = "";
    try { b64 = await iconToMaskPng(icon); } catch { /* 渲染异常按失败处理 */ }
    if (!b64) {
        Message.error("该图标无法渲染，请换一个试试");
        return;
    }
    try {
        await GoTools?.SaveBallIcon(b64);
    } catch {
        Message.error("保存球图标失败");
        return;
    }
    form.ball.icon = icon;
    scheduleSave(0);
}

/** 清除自定义球图标：回退应用 Logo/内置默认（图标值随 ball.icon 一并清空保存） */
async function clearBallIcon() {
    try { await GoTools?.ClearBallIcon(); } catch { /* 忽略 */ }
    form.ball.icon = "";
    scheduleSave(0);
}

function normTool(t: any): ToolRow {
    return {
        id: t.id || t.key || "",
        key: t.key || "",
        name: t.name || "",
        path: t.path || "",
        event: t.event || "",
        icon: normalizeToolIcon(t.icon),
        single: !!t.single,
        fullscreen: !!t.fullscreen,
        disableFullscreen: !!t.disableFullscreen,
        width: t.width || 0,
        height: t.height || 0,
        shortcut: t.shortcut || "",
        quick: !!t.quick,
    };
}

async function load() {
    try {
        const cfg = await GoTools?.GetConfig();
        if (cfg) {
            fullConfig.value = cfg;
            form.log = { enabled: !!cfg.log?.enabled, level: cfg.log?.level || "info", path: "" };
            form.ball = { enabled: !!cfg.ball?.enabled, icon: cfg.ball?.icon || "" };
            form.general = { autoStart: !!cfg.general?.autoStart, appName: cfg.general?.appName || "Tools", appLogo: cfg.general?.appLogo || "", theme: cfg.general?.theme || "light" };
            form.paths = { plugins: cfg.paths?.plugins || "", data: cfg.paths?.data || "", logs: cfg.paths?.logs || "" };
            tools.value = (cfg.tools || []).map(normTool);
        }
        activePath.value = (await GoTools?.GetLogDir()) || "";
        // 加载 logo 预览
        try { const lp = await GoTools?.GetAppLogo(); if (lp) logoPreview.value = lp; } catch { /* */ }
    } catch {
        Message.error("读取配置失败");
    }
}

/* ---------------- 工具编辑 ---------------- */
const editVisible = ref(false);
const editIndex = ref(-1);
const editForm = reactive<ToolRow>(normTool({}));

function openAdd() {
    editIndex.value = -1;
    Object.assign(editForm, normTool({ id: "", key: "", name: "", path: "", event: "", icon: "", single: false, fullscreen: false, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false }));
    editVisible.value = true;
}

function openEdit(i: number) {
    editIndex.value = i;
    Object.assign(editForm, normTool(tools.value[i]));
    editVisible.value = true;
}

/** 图标选择弹层回填（Arco 模式 emit 图标名，图标库模式 emit icon-xxx） */
function iconChange(val: any) {
    editForm.icon = String(val ?? "");
}

function onBuiltinChange(val: any) {
    const key = String(val ?? "");
    const b = BUILTINS.find((x) => x.key === key);
    if (!b) return;
    editForm.key = b.key;
    if (!editForm.name || editIndex.value < 0) editForm.name = b.name;
    editForm.path = b.path || "";
    editForm.event = b.event || "";
    editForm.icon = b.icon || editForm.icon;
    if (editIndex.value < 0) {
        editForm.single = !!b.single;
        editForm.fullscreen = !!b.fullscreen;
        editForm.disableFullscreen = !!b.disableFullscreen;
        editForm.width = b.width || 0;
        editForm.height = b.height || 0;
        editForm.quick = !!b.quick;
    }
}

function genId() {
    return "t" + Date.now().toString(36) + Math.random().toString(36).slice(2, 6);
}

function confirmEdit() {
    if (!editForm.key && !editForm.path && !editForm.event) {
        Message.warning("请选择内置功能或填写路由/事件");
        return;
    }
    const row = normTool({ ...editForm, id: editForm.id || genId() });
    if (editIndex.value < 0) {
        tools.value.push(row);
    } else {
        tools.value[editIndex.value] = row;
    }
    editVisible.value = false;
    scheduleSave(0);
}

function del(i: number) {
    tools.value.splice(i, 1);
    scheduleSave(0);
}

function move(i: number, dir: number) {
    const j = i + dir;
    if (j < 0 || j >= tools.value.length) return;
    const arr = tools.value;
    [arr[i], arr[j]] = [arr[j], arr[i]];
    scheduleSave(0);
}

function resetTools() {
    tools.value = DEFAULT_TOOLS.map(normTool);
    scheduleSave(0);
}

/* ---------------- 日志操作 ---------------- */
/** 选择全局基础目录（插件/数据/日志）：保存后各服务配置与另存默认目录随即落新路径 */
async function pickBaseDir(kind: "plugins" | "data" | "logs", title: string) {
    try {
        const dir = await GoTools?.ChooseDir(title);
        if (dir) {
            form.paths[kind] = dir;
            scheduleSave(0);
        }
    } catch {
        /* 用户取消 */
    }
}

/** 合并短时间内的连续变更（如主题按钮 + 其它字段），避免重复写配置 */
function scheduleSave(delay = 300) {
    clearTimeout(saveTimer);
    saveTimer = window.setTimeout(() => { void autoSave(); }, delay);
}

/** 立即持久化当前表单（用于选择目录/Logo 等需要等待保存结果的场景） */
async function saveNow() {
    clearTimeout(saveTimer);
    await autoSave();
}

/** 自动保存：按调用顺序串行执行，避免并发写配置时旧数据覆盖新数据 */
function autoSave(): Promise<void> {
    saveChain = saveChain.catch(() => undefined).then(runSave);
    return saveChain;
}

/** 开关/下拉/输入框变更即写入配置，不再需要手动点“保存设置” */
async function runSave() {
    saving.value = true;
    try {
        const payload = { log: { ...form.log }, ball: { ...form.ball }, general: { ...form.general }, paths: { ...form.paths }, tools: tools.value.map(normTool) };
        const path = await GoTools?.SaveConfig(payload);
        fullConfig.value = payload;
        activePath.value = path || "";
        // 应用名称/Logo 变更立即生效到窗口标题与托盘
        try { await GoTools?.ApplyAppName(); } catch { /* */ }
    } catch {
        Message.error("自动保存失败");
    } finally {
        saving.value = false;
    }
}

async function openLogDir() {
    try {
        await GoTools?.OpenLogDir();
    } catch {
        if (activePath.value) Message.info(activePath.value);
    }
}

/** 默认工具列表：“恢复默认列表”还原到这份基线，与后端 DefaultConfig 一致。
 * 这是当前保存配置的快照（自定义图标/开关已烘入），不再从 BUILTINS 派生，避免默认基线漂移。
 * 不含 settings（设置仍可从浮动球/标题栏入口进入）。 */
const DEFAULT_TOOLS: ToolRow[] = [
    { id: "screenshot", key: "screenshot", name: "截图", path: "", event: "screenshot", icon: "icon-jietu", single: false, fullscreen: false, disableFullscreen: false, width: 0, height: 0, shortcut: "Ctrl+Alt+A", quick: true },
    { id: "colorpicker", key: "colorpicker", name: "颜色取色器", path: "/tools/colorpicker", event: "", icon: "icon-quseqi", single: true, fullscreen: false, disableFullscreen: false, width: 480, height: 560, shortcut: "", quick: true },
    { id: "video", key: "video", name: "视频录制", path: "/tools/video", event: "", icon: "video-camera", single: true, fullscreen: true, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: true },
    { id: "qrcode", key: "qrcode", name: "二维码生成", path: "/tools/qrcode", event: "", icon: "icon-erweima3", single: false, fullscreen: false, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false },
    { id: "timestamp", key: "timestamp", name: "时间戳转换", path: "/tools/timestamp", event: "", icon: "icon-riqishijianzhuanhuanmiaoshu", single: false, fullscreen: false, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false },
    { id: "strrandom", key: "strrandom", name: "随机字符串生成", path: "/tools/strrandom", event: "", icon: "icon-zifuchuan", single: false, fullscreen: false, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false },
    { id: "encdec", key: "encdec", name: "文字加密解密", path: "/tools/encdec", event: "", icon: "lock", single: false, fullscreen: false, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false },
    { id: "hexconvert", key: "hexconvert", name: "进制转换", path: "/tools/hexconvert", event: "", icon: "swap", single: false, fullscreen: false, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false },
    { id: "imageBase64", key: "imageBase64", name: "图片base64", path: "/tools/imageBase64", event: "", icon: "icon-img", single: false, fullscreen: false, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false },
    { id: "sniffer", key: "sniffer", name: "抓包工具", path: "/tools/sniffer", event: "", icon: "bug", single: true, fullscreen: true, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: true },
    { id: "websocket", key: "websocket", name: "WebSocket调试", path: "/tools/websocket", event: "", icon: "icon-wangluo1", single: false, fullscreen: true, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false },
    { id: "usbserial", key: "usbserial", name: "USB调试", path: "/tools/usbserial", event: "", icon: "icon-usb1", single: false, fullscreen: false, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false },
    { id: "redis", key: "redis", name: "Redis管理", path: "/tools/redis", event: "", icon: "icon-icon-redis", single: true, fullscreen: true, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false },
    { id: "mysql", key: "mysql", name: "MySQL管理", path: "/tools/mysql", event: "", icon: "icon-MySQL", single: true, fullscreen: true, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false },
    { id: "php", key: "php", name: "PHP管理", path: "/tools/php", event: "", icon: "icon-php", single: true, fullscreen: false, disableFullscreen: false, width: 1200, height: 800, shortcut: "", quick: false },
    { id: "nginx", key: "nginx", name: "Nginx管理", path: "/tools/nginx", event: "", icon: "icon-ngnix", single: true, fullscreen: false, disableFullscreen: false, width: 1200, height: 800, shortcut: "", quick: false },
    { id: "npm", key: "npm", name: "Npm管理", path: "/tools/npm", event: "", icon: "icon-npm", single: true, fullscreen: false, disableFullscreen: false, width: 1200, height: 800, shortcut: "", quick: false },
];

/* ---------------- 表格高度自适应（继承窗口高度，表体内部滚动） ---------------- */
const toolsTableRef = ref<HTMLElement>();
const tableScrollY = ref<number>(0);
const tableScroll = computed(() =>
    tableScrollY.value > 0
        ? { x: "100%", y: tableScrollY.value }
        : { x: "100%" }
);

/** 测量表格包裹容器可用高度，扣除表头，得到可滚动的表体 y */
function calcTableScroll() {
    if (activeTab.value !== "tools") return;
    const wrap = toolsTableRef.value;
    if (!wrap) return;
    const header = wrap.querySelector(".arco-table-header") as HTMLElement | null;
    const headerH = header?.offsetHeight ?? 34;
    // 容器由 flex 撑出高度；隐藏（display:none）时高度为 0，跳过本次计算
    const avail = wrap.clientHeight;
    if (!avail) return;
    tableScrollY.value = Math.max(160, avail - headerH);
}

let resizeTimer = 0;
const onWinResize = () => {
    clearTimeout(resizeTimer);
    resizeTimer = window.setTimeout(calcTableScroll, 100);
};

let ro: ResizeObserver | null = null;
watch(tools, () => nextTick(calcTableScroll));

//  切换到“工具”页时重新观测包裹容器并计算高度
watch(activeTab, (k) => {
    nextTick(() => {
        if (ro) {
            ro.disconnect();
            if (toolsTableRef.value) ro.observe(toolsTableRef.value);
        }
        calcTableScroll();
    });
});

/* ---------------- 快捷键录制（按下组合键直接赋值） ---------------- */
const recording = ref(false);

function startRecord() {
    recording.value = true;
}

/** 将按键名规范化为热键文本（与后端全局热键解析一致） */
function normalizeKey(k: string): string {
    if (k === " ") return "Space";
    if (k.length === 1) return k.toUpperCase();
    const map: Record<string, string> = {
        ArrowUp: "Up", ArrowDown: "Down", ArrowLeft: "Left", ArrowRight: "Right",
        Escape: "Esc", Delete: "Del", Insert: "Ins", PageUp: "PgUp", PageDown: "PgDn",
    };
    return map[k] || k;
}

function onShortcutKeydown(e: KeyboardEvent) {
    e.preventDefault();
    if (e.key === "Escape") {
        recording.value = false;
        return;
    }
    if (e.key === "Backspace" || e.key === "Delete") {
        editForm.shortcut = "";
        return;
    }
    // 仅按下修饰键时忽略，等待主键
    if (["Control", "Alt", "Shift", "Meta"].includes(e.key)) return;
    const parts: string[] = [];
    if (e.ctrlKey) parts.push("Ctrl");
    if (e.altKey) parts.push("Alt");
    if (e.shiftKey) parts.push("Shift");
    if (e.metaKey) parts.push("Win");
    parts.push(normalizeKey(e.key));
    editForm.shortcut = parts.join("+");
    recording.value = false;
}

onMounted(() => {
    load();
    window.addEventListener("resize", onWinResize);
    if (typeof ResizeObserver !== "undefined") {
        ro = new ResizeObserver(() => calcTableScroll());
        if (toolsTableRef.value) ro.observe(toolsTableRef.value);
    }
});

onBeforeUnmount(() => {
    window.removeEventListener("resize", onWinResize);
    ro?.disconnect();
    ro = null;
    clearTimeout(resizeTimer);
    clearTimeout(saveTimer);
});
</script>

<style scoped>
/* 布局参考 Redis 工具页：整页铺满、左右分栏 */
.settings-page {
    height: 100%;
    min-height: 0;
    flex-wrap: nowrap;
    background: var(--color-fill-1);
}

/* 左侧分类列 */
.side-col {
    height: 100%;
    min-width: 0;
    display: flex;
    flex-direction: column;
}

/* 左侧分类卡片：body 弹性填充，列表内部滚动 */
.side-card {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

.side-card :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

.cat-list {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
}

.cat-item {
    display: flex;
    align-items: center;
    gap: var(--base-padding);
    padding: var(--base-padding);
    font-size: var(--base-size-small);
    cursor: pointer;
    border-bottom: 1px solid var(--color-fill-1);
}

.cat-item:hover {
    background: var(--color-fill-2);
}

/* 选中态：主题主色为黑白时默认样式在暗色下对比不足，改用中性底色 + 正文色 */
.cat-item.active {
    background: var(--color-fill-3);
    color: var(--color-text-1);
    font-weight: 600;
}

.cat-icon {
    font-size: var(--base-size-default);
}

.cat-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.cat-arrow {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}

/* 右侧主列 */
.main-col {
    height: 100%;
    min-width: 0;
    display: flex;
    flex-direction: column;
}

.main-panel {
    width: 100%;
    height: 100%;
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    background: var(--color-bg-2);
}

.main-panel :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

.panel-title {
    font-size: var(--base-size-default);
    font-weight: 600;
}

/* 内容区：同时只显示当前分类的子卡片 */
.tab-content {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: var(--base-padding);
    overflow: hidden;
}

.pane-card {
    height: 100%;
    display: flex;
    flex-direction: column;
}

.pane-card :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    overflow: auto;
}

/* 工具页：表体内部滚动，卡片 body 不滚动 */
.pane-card.tools-card :deep(.arco-card-body) {
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

.path-row {
    display: flex;
    gap: 8px;
    width: 100%;
}

.path-row :deep(.arco-input-wrapper) {
    flex: 1;
}

/* 表单项内容默认横排（nowrap）：提示文案独占一行，避免与控件挤在同一行 */
.pane-card :deep(.arco-form-item-content-flex),
.edit-form :deep(.arco-form-item-content-flex) {
    flex-wrap: wrap;
    row-gap: 2px;
}

.pane-card :deep(.arco-form-item-content-flex) > .hint,
.edit-form :deep(.arco-form-item-content-flex) > .hint {
    flex: 0 0 100%;
    width: 100%;
}

.hint {
    display: block;
    margin-top: 4px;
    color: var(--color-text-3);
    font-size: var(--base-size-small);
    line-height: 1.5;
}

.hint-inline {
    display: inline;
    margin: 0;
    flex: none;
    width: auto;
}

/* 面板右上角自动保存状态 */
.auto-save-tip {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}

.tools-pane {
    height: 100%;
}

.tools-table-wrap {
    flex: 1;
    min-height: 0;
    overflow: hidden;
}

.on {
    color: rgb(var(--green-6));
}

.off {
    color: var(--color-text-4);
}

.icon-cell {
    display: inline-flex;
    align-items: center;
    gap: 6px;
}

.icon-cell .arco-preview,
.icon-cell .iconfont-preview {
    font-size: var(--base-size-default);
    color: var(--color-text-1);
}

.icon-cell .icon-text {
    color: var(--color-text-3);
    font-size: var(--base-size-small);
}

/* 内置功能下拉：Arco 与图标库图标混排 */
.builtin-option {
    display: inline-flex;
    align-items: center;
    gap: 8px;
}

.builtin-icon,
.form-icon {
    font-size: var(--base-size-default);
    color: var(--color-text-2);
}

/* 快捷键录制提示 */
.rec-tip {
    font-size: var(--base-size-small);
    color: var(--color-text-2);
}

/* 主题按钮选中态：黑白主题下默认文字色（primary-6）在暗色下不可读 */
.pane-card :deep(.arco-radio-button.arco-radio-checked),
.pane-card :deep(.arco-radio-button.arco-radio-checked:hover) {
    color: var(--color-text-1);
    background-color: var(--color-fill-3);
    font-weight: 600;
}

/* Logo 选择器 */
.logo-picker {
    display: flex;
    align-items: center;
    gap: 16px;
}
.logo-preview {
    width: 48px;
    height: 48px;
    border-radius: var(--base-radius-large);
    border: 1px dashed var(--color-border-3);
    display: flex;
    align-items: center;
    justify-content: center;
    overflow: hidden;
    background: var(--color-fill-1);
}
.logo-img {
    width: 100%;
    height: 100%;
    object-fit: contain;
}
.logo-placeholder {
    font-size: var(--base-size-small);
    color: var(--color-text-4);
}

/* 浮动球图标选择器：圆形预览模拟球面，颜色语义与 Go 侧原生绘制一致（bg-1 底 / text-1 图标） */
.ball-icon-preview {
    width: 48px;
    height: 48px;
    border-radius: 50%;
    border: 1px dashed var(--color-border-3);
    background: var(--color-bg-1);
    color: var(--color-text-1);
    display: flex;
    align-items: center;
    justify-content: center;
    overflow: hidden;
}
.ball-icon-glyph {
    font-size: 26px;
}
</style>
