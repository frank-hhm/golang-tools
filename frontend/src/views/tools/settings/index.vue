<template>
    <!-- 布局参考 Redis 工具页：整页左右分栏，左侧分类导航 + 右侧主面板 -->
    <a-row class="settings-page" :gutter="6">
        <!-- 左侧：设置分类 -->
        <a-col :span="5" class="side-col">
            <a-card class="side-card card-head-tool" size="small" :body-style="{ padding: 0 }">
                
                <div class="side-search">
                    <a-input-search v-model="searchKw" placeholder="搜索设置项" allow-clear
                        @keydown.esc="searchKw = ''" />
                </div>
                <!-- 有关键词时左栏整体切换成结果列表（DeskBox 式「搜到任意设置」），清空后恢复分类导航 -->
                <div v-if="!searchKw.trim()" class="cat-list">
                    <div v-for="t in SETTING_TABS" :key="t.key" class="cat-item"
                        :class="{ active: activeTab === t.key }" @click="activeTab = t.key">
                        <component :is="t.icon" class="cat-icon" />
                        <span class="cat-name">{{ t.label }}</span>
                        <icon-right v-if="activeTab === t.key" class="cat-arrow" />
                    </div>
                </div>
                <div v-else class="cat-list search-list">
                    <div v-for="r in searchResults" :key="r.skey" class="cat-item s-item"
                        @click="jumpToSetting(r.skey)">
                        <div class="s-main">
                            <span class="s-label" v-html="hlHtml(r.label)"></span>
                            <span class="s-tab">{{ tabLabel(r.tab) }}</span>
                        </div>
                        <div v-if="r.hint" class="s-hint" v-html="hlHtml(r.hint)"></div>
                    </div>
                    <div v-if="!searchResults.length" class="s-empty">没有匹配的设置项</div>
                </div>
            </a-card>
        </a-col>

        <!-- 右侧：主区域，所有设置项修改后自动保存 -->
        <a-col :span="19" class="main-col">
            <div class="main-panel">
                <div class="tab-content">
                    <!-- 15 个分类共用一张卡片：标题=当前 tab 图标+heading，右侧=自动保存提示 + panel/tools/browser 三个特例的操作区；
                       每个分类的内容降级为普通 <div class="pane-body">，靠 v-show 互斥。tools 需要 body 不滚动+去内衬，故按 activeTab 动态挂 tools-card。 -->
                    <a-card class="pane-card card-head-tool" :class="{ 'tools-card': activeTab === 'tools' }"
                        :body-style="activeTab === 'tools' ? { padding: 0 } : {}">
                        <template #title>
                            <a-space>
                                <component :is="activeTabMeta.icon" />
                                <span>{{ activeTabMeta.heading }}</span>
                            </a-space>
                        </template>
                        <template #extra>
                            <a-space>
                                <span class="auto-save-tip">
                                    <span class="save-dot" :class="{ busy: saving }" />
                                    {{ saving ? "正在保存…" : "修改后自动保存" }}
                                </span>
                                <template v-if="activeTab === 'panel'">
                                    <a-tag v-if="panelInfo" size="small" :color="panelInfo.enabled ? 'green' : 'orangered'">
                                        {{ panelInfo.enabled ? "登录保护已开启" : "登录保护已关闭" }}
                                    </a-tag>
                                    <a-link v-if="panelInfo?.accessURL" @click="copyAccess">复制访问地址</a-link>
                                </template>
                                <template v-if="activeTab === 'tools'">
                                    <span class="hint hint-inline">快捷键为系统全局热键</span>
                                    <a-popconfirm content="恢复默认功能列表？自定义的增删与排序会被覆盖。" type="warning" position="lt"
                                        @ok="resetTools">
                                        <a-button size="small">恢复默认列表</a-button>
                                    </a-popconfirm>
                                    <a-button size="small" @click="openAdd">
                                        <template #icon><icon-plus /></template>新增功能
                                    </a-button>
                                </template>
                                <template v-if="activeTab === 'browser'">
                                    <span class="hint hint-inline">空间明细在浏览器页右上角齿轮里改</span>
                                    <a-button size="small" @click="openBrowserWindow">
                                        <template #icon><icon-compass /></template>打开浏览器
                                    </a-button>
                                </template>
                            </a-space>
                        </template>
                    <!-- ===================== 通用设置 ===================== -->
                    <div v-show="activeTab === 'general'" class="pane-body">
                        <a-form :model="form" :style="{ maxWidth: '720px' }" layout="vertical">
                            <a-form-item field="general.appName" data-skey="general.appName">
                                <template #label>应用名称</template>
                                <a-input v-model="form.general.appName" placeholder="Tools"
                                    :style="{ maxWidth: '280px' }" @blur="scheduleSave(0)"
                                    @press-enter="scheduleSave(0)" />
                                <span class="hint">窗口标题、托盘提示显示的名称，修改后立即生效。</span>
                            </a-form-item>
                            <a-form-item field="general.appLogo" data-skey="general.appLogo">
                                <template #label>应用 Logo</template>
                                <div class="logo-picker">
                                    <div class="logo-preview">
                                        <img v-if="logoPreview" :src="logoPreview" class="logo-img" />
                                        <span v-else class="logo-placeholder">无</span>
                                    </div>
                                    <a-space>
                                        <a-button size="small" @click="pickLogo">选择图片</a-button>
                                        <a-button size="small" status="danger" v-if="form.general.appLogo"
                                            @click="clearLogo">清除</a-button>
                                    </a-space>
                                </div>
                                <span class="hint">建议 PNG 正方形（如 128x128），用于浮动球中心图标。留空使用内置默认。</span>
                            </a-form-item>
                            <a-form-item field="autoStart" data-skey="general.autoStart">
                                <template #label>开机默认启动</template>
                                <a-switch type="round" v-model="form.general.autoStart" @change="scheduleSave(0)" />
                                <span class="hint">开启后 Windows 登录时自动启动应用。</span>
                            </a-form-item>
                        </a-form>
                    </div>

                    <!-- ===================== 主题设置 ===================== -->
                    <div v-show="activeTab === 'theme'" class="pane-body">
                        <a-form :model="form" :style="{ maxWidth: '720px' }" layout="vertical">
                            <a-form-item field="general.theme" data-skey="general.theme">
                                <template #label>主题模式</template>
                                <a-radio-group size="medium" v-model="form.general.theme.mode" type="button"
                                    @change="onThemeTokenChange">
                                    <a-radio value="light">浅色</a-radio>
                                    <a-radio value="dark">暗黑</a-radio>
                                    <a-radio value="auto">跟随系统</a-radio>
                                </a-radio-group>
                                <span class="hint">明暗模式，切换后立即生效，所有窗口同步。</span>
                            </a-form-item>
                            <a-form-item field="general.theme.radius" data-skey="general.theme">
                                <template #label>圆角</template>
                                <a-radio-group v-model="form.general.theme.radius" type="button" size="small"
                                    @change="onThemeTokenChange">
                                    <a-radio value="none">直角</a-radio>
                                    <a-radio value="small">小</a-radio>
                                    <a-radio value="default">标准</a-radio>
                                    <a-radio value="large">大</a-radio>
                                </a-radio-group>
                                <span class="hint">页面内圆角四档全量生效；主窗/工具窗的四角由系统接管，只有「直角（选直角/小）」与「系统默认圆角（标准/大）」两态。</span>
                            </a-form-item>
                            <a-form-item field="general.theme.density" data-skey="general.theme">
                                <template #label>显示密度</template>
                                <a-radio-group v-model="form.general.theme.density" type="button" size="small"
                                    @change="onThemeTokenChange">
                                    <a-radio value="compact">紧凑</a-radio>
                                    <a-radio value="standard">标准</a-radio>
                                    <a-radio value="loose">宽松</a-radio>
                                </a-radio-group>
                                <span class="hint">调整字号与留白；对第三方控件为尽力优化。</span>
                            </a-form-item>
                            <a-form-item field="general.theme.fontSize" data-skey="general.theme.fontSize">
                                <template #label>字体大小</template>
                                <a-radio-group v-model="form.general.theme.fontSize" type="button" size="small"
                                    @change="onThemeTokenChange">
                                    <a-radio value="small">小</a-radio>
                                    <a-radio value="default">标准</a-radio>
                                    <a-radio value="large">大</a-radio>
                                    <a-radio value="xlarge">特大</a-radio>
                                </a-radio-group>
                                <span class="hint">整体字号，独立于显示密度；两者可分别调节。</span>
                            </a-form-item>
                            <a-form-item field="general.theme.background" data-skey="general.theme">
                                <template #label>主题背景</template>
                                <a-radio-group v-model="form.general.theme.background.type" type="button" size="small"
                                    @change="onBackgroundTypeChange">
                                    <a-radio value="none">无</a-radio>
                                    <a-radio value="mesh">彩色网格</a-radio>
                                    <a-radio value="photo">风景照</a-radio>
                                    <a-radio value="dark">暗夜纹理</a-radio>
                                    <a-radio value="image">自定义图</a-radio>
                                    <a-radio v-if="caps.nativeWindows && caps.systemBackdrop" value="native">原生材质</a-radio>
                                </a-radio-group>
                                <div v-if="form.general.theme.background.type === 'image'" class="bg-image-row">
                                    <a-button size="small" @click="pickBackgroundImage">选择图片…</a-button>
                                    <span v-if="form.general.theme.background.image" class="hint">当前：{{ bgImageName }}</span>
                                    <a-button v-if="form.general.theme.background.image" size="small"
                                        @click="clearBackgroundImage">清除</a-button>
                                </div>
                                <input ref="bgFileInput" type="file" accept="image/*" class="bg-file-input"
                                    @change="onBgFileChosen" />
                                <span class="hint">全局背景主题，切换即时生效、所有窗口同步；「自定义图」可上传本地图片（≤8MB）。暗夜纹理随明暗主题给近白/暗两版。</span>
                                <span v-if="caps.nativeWindows && caps.systemBackdrop" class="hint">「原生材质」走 Windows 11 系统级桌面模糊（DWM Acrylic），关闭后需重启应用才能让主窗透明/模糊完全生效。</span>
                            </a-form-item>
                            <a-form-item field="general.theme.glass" data-skey="general.theme">
                                <template #label>表面玻璃</template>
                                <a-switch type="round" v-model="form.general.theme.glass.on"
                                    @change="onThemeTokenChange" />
                                <span class="hint">给卡片/弹窗/抽屉铺半透明磨砂（CSS backdrop-filter，跨平台）；配合「主题背景」才有可糊的底。</span>
                            </a-form-item>
                            <template v-if="form.general.theme.glass.on">
                                <a-form-item field="general.theme.glass.material" data-skey="general.theme">
                                    <template #label>玻璃材质</template>
                                    <a-radio-group v-model="form.general.theme.glass.material" type="button" size="small"
                                        @change="onThemeTokenChange">
                                        <a-radio value="frosted">普通磨砂</a-radio>
                                        <a-radio value="acrylic">Acrylic 仿</a-radio>
                                    </a-radio-group>
                                    <a-space class="glass-presets">
                                        <a-button size="mini" @click="applyGlassPreset('soft')">柔和</a-button>
                                        <a-button size="mini" @click="applyGlassPreset('default')">默认</a-button>
                                        <a-button size="mini" @click="applyGlassPreset('strong')">强磨砂</a-button>
                                    </a-space>
                                </a-form-item>
                                <a-form-item :label="`底透明度 ${form.general.theme.glass.alpha}%`">
                                    <a-slider v-model="form.general.theme.glass.alpha" :min="0" :max="100" :step="1"
                                        @change="onThemeTokenChange" />
                                </a-form-item>
                                <a-form-item :label="`模糊半径 ${form.general.theme.glass.blur}px`">
                                    <a-slider v-model="form.general.theme.glass.blur" :min="0" :max="40" :step="1"
                                        @change="onThemeTokenChange" />
                                </a-form-item>
                                <a-form-item :label="`描边强度 ${form.general.theme.glass.border}%`">
                                    <a-slider v-model="form.general.theme.glass.border" :min="0" :max="100" :step="1"
                                        @change="onThemeTokenChange" />
                                </a-form-item>
                                <a-form-item :label="`投影强度 ${form.general.theme.glass.shadow}%`">
                                    <a-slider v-model="form.general.theme.glass.shadow" :min="0" :max="50" :step="1"
                                        @change="onThemeTokenChange" />
                                </a-form-item>
                                <a-form-item v-if="form.general.theme.glass.material === 'acrylic'"
                                    :label="`噪点强度 ${form.general.theme.glass.noise}‰`">
                                    <a-slider v-model="form.general.theme.glass.noise" :min="0" :max="120" :step="1"
                                        @change="onThemeTokenChange" />
                                </a-form-item>
                            </template>
                            <a-form-item field="general.theme.flush" data-skey="general.theme">
                                <template #label>无边距模式</template>
                                <a-switch type="round" v-model="form.general.theme.flush"
                                    @change="onThemeTokenChange" />
                                <span class="hint">卡片去掉左右内边距、不铺背景与投影，描边也固定去掉（hover/选中态照常），内容贴边铺满、整页更紧凑。</span>
                            </a-form-item>
                        </a-form>
                    </div>

                    <!-- ===================== 目录设置 ===================== -->
                    <div v-show="activeTab === 'paths'" class="pane-body">
                        <a-form :model="form" :style="{ maxWidth: '720px' }" layout="vertical">
                            <a-form-item field="paths.plugins" data-skey="paths.plugins">
                                <template #label>插件目录（plugins）</template>
                                <div class="path-row">
                                    <a-input v-model="form.paths.plugins" placeholder="留空使用默认（应用目录/plugins）" allow-clear
                                        @blur="scheduleSave(0)" @press-enter="scheduleSave(0)"
                                        @clear="scheduleSave(0)" />
                                    <a-button size="small" @click="pickBaseDir('plugins', '选择插件目录')">选择目录</a-button>
                                </div>
                                <div class="hint">服务程序安装位置：PHP/Nginx/MySQL/Redis/npm
                                    等按服务名建子目录（plugins/服务名）。修改后新安装/下载的服务落到新目录。
                                </div>
                            </a-form-item>
                            <a-form-item field="paths.data" data-skey="paths.data">
                                <template #label>数据目录（data）</template>
                                <div class="path-row">
                                    <a-input v-model="form.paths.data" placeholder="留空使用默认（应用目录/data）" allow-clear
                                        @blur="scheduleSave(0)" @press-enter="scheduleSave(0)"
                                        @clear="scheduleSave(0)" />
                                    <a-button size="small" @click="pickBaseDir('data', '选择数据目录')">选择目录</a-button>
                                </div>
                                <div class="hint">
                                    服务配置缓存与实例工作目录（data/服务名）；截图另存、录像、站点根等文件的默认落点（data/screenshots、data/videos、data/www）。保存后各另存对话框默认目录随即使用新路径。
                                </div>
                            </a-form-item>
                            <a-form-item field="paths.downloads" data-skey="paths.downloads">
                                <template #label>下载目录（downloads）</template>
                                <div class="path-row">
                                    <a-input v-model="form.paths.downloads" placeholder="留空使用默认（应用目录/downloads）"
                                        allow-clear @blur="scheduleSave(0)" @press-enter="scheduleSave(0)"
                                        @clear="scheduleSave(0)" />
                                    <a-button size="small" @click="pickBaseDir('downloads', '选择下载目录')">选择目录</a-button>
                                </div>
                                <div class="hint">下载管理器新建任务时“保存目录”留空的默认落点。修改保存后，新建下载随即落到新目录（已存在的任务仍留在其原保存路径）。</div>
                            </a-form-item>
                            <a-form-item field="log.enabled" data-skey="log.enabled">
                                <template #label>启用文件日志</template>
                                <a-switch type="round" v-model="form.log.enabled" @change="scheduleSave(0)" />
                                <span class="hint">关闭后不再写入日志文件（仅开发控制台可见）。</span>
                            </a-form-item>
                            <a-form-item field="paths.logs" data-skey="paths.logs">
                                <template #label>日志目录（logs）</template>
                                <div class="path-row">
                                    <a-input v-model="form.paths.logs" placeholder="留空使用默认（应用目录/logs）" allow-clear
                                        @blur="scheduleSave(0)" @press-enter="scheduleSave(0)"
                                        @clear="scheduleSave(0)" />
                                    <a-button size="small" @click="pickBaseDir('logs', '选择日志目录')">选择目录</a-button>
                                    <a-button size="small" v-if="activePath" @click="openLogDir">打开日志目录</a-button>
                                </div>
                                <div class="hint">程序与各服务日志的根目录，按服务名建子目录：logs/app 程序日志、logs/mysql SQL 日志、logs/php
                                    实例服务日志等。开启文件日志后
                                    info / warn / error 全部记录，按“级别 + 当前日期”自动分文件（如
                                    Info20261022.log、Warn20261022.log、Error20261022.log）。当前程序日志目录：{{ activePath ||
                                    "（未加载）" }}
                                </div>
                            </a-form-item>
                        </a-form>
                    </div>

                    <!-- ===================== 访问与安全（仅 Web / 服务模式） ===================== -->
                    <div v-show="activeTab === 'panel'" class="pane-body">

                        <a-spin :loading="panelLoading" style="display: block">
                            <a-form :model="panelForm" :style="{ maxWidth: '720px' }" layout="vertical">
                                <a-row :gutter="12">
                                    <a-col :span="12">
                                        <a-form-item field="panelUsername" data-skey="panel.username">
                                            <template #label>登录账号</template>
                                            <a-input v-model="panelForm.username" placeholder="admin"
                                                :style="{ maxWidth: '260px' }" allow-clear />
                                        </a-form-item>
                                    </a-col>
                                    <a-col :span="12">
                                        <a-form-item field="panelSession" data-skey="panel.sessionHours">
                                            <template #label>登录有效期（小时）</template>
                                            <a-input-number v-model="panelForm.sessionHours" :min="1" :max="720"
                                                :style="{ width: '140px' }" />
                                        </a-form-item>
                                    </a-col>
                                </a-row>
                                <span class="hint">改账号或密码后，其它已登录的设备会立即失效，需要重新登录。</span>

                                <a-row :gutter="12" :style="{ marginTop: '16px' }">
                                    <a-col :span="14">
                                        <a-form-item field="panelHost" data-skey="panel.host">
                                            <template #label>监听地址</template>
                                            <a-select v-model="panelForm.host" allow-create allow-search>
                                                <a-option value="0.0.0.0">0.0.0.0（所有网卡）</a-option>
                                                <a-option value="127.0.0.1">127.0.0.1（仅本机，走 nginx 反代）</a-option>
                                            </a-select>
                                        </a-form-item>
                                    </a-col>
                                    <a-col :span="10">
                                        <a-form-item field="panelPort" data-skey="panel.port">
                                            <template #label>端口</template>
                                            <a-input-number v-model="panelForm.port" :min="1" :max="65535"
                                                :style="{ width: '140px' }" />
                                        </a-form-item>
                                    </a-col>
                                </a-row>

                                <a-form-item field="panelEntry" data-skey="panel.entryPath">
                                    <template #label>安全入口</template>
                                    <div class="path-row">
                                        <a-input v-model="panelForm.entryPath" placeholder="留空关闭，例如 /manage-9f3ad2"
                                            allow-clear />
                                        <a-button size="small" @click="genEntry">随机生成</a-button>
                                    </div>
                                    <span class="hint">
                                        设置后必须先访问该入口地址才能打开登录页；直接访问根地址一律返回 404，
                                        可挡掉大部分扫端口的机器人。改动立即生效，请记下来。
                                    </span>
                                </a-form-item>

                                <a-form-item field="panelEnabled" data-skey="panel.enabled">
                                    <template #label>登录保护</template>
                                    <a-switch v-model="panelForm.enabled" type="round" />
                                    <span class="hint">关闭后任何人访问面板地址都不需要登录，仅适合完全可信的内网临时排障。</span>
                                </a-form-item>

                                <a-form-item>
                                    <div class="panel-actions">
                                        <a-space>
                                            <a-button type="primary" :loading="panelSaving"
                                                @click="savePanel">保存并应用</a-button>
                                            <a-button @click="loadPanel">还原</a-button>
                                        </a-space>
                                        <div v-if="panelInfo?.accessURL" class="panel-access">
                                            <span class="hint">当前访问地址：</span>
                                            <code>{{ panelInfo.accessURL }}</code>
                                            <span v-if="panelInfo.listen" class="hint">（进程监听 {{ panelInfo.listen
                                                }}）</span>
                                        </div>
                                    </div>
                                </a-form-item>

                                <a-divider :margin="8" />

                                <a-form-item field="pwdCurrent" data-skey="panel.password">
                                    <template #label>修改登录密码</template>
                                    <a-input-password v-model="pwdForm.current" placeholder="当前密码"
                                        :style="{ maxWidth: '260px', marginBottom: '8px' }" />
                                    <a-input-password v-model="pwdForm.next" placeholder="新密码（至少 8 位）"
                                        :style="{ maxWidth: '260px', marginBottom: '8px' }" />
                                    <a-input-password v-model="pwdForm.confirm" placeholder="再输一次新密码"
                                        :style="{ maxWidth: '260px', marginBottom: '8px' }" />
                                    <a-button type="outline" :loading="pwdSaving" @click="savePassword">更新密码</a-button>
                                    <span class="hint">密码用 bcrypt 加盐存储；更新后当前浏览器保持登录，其它设备下线。</span>
                                </a-form-item>
                            </a-form>
                        </a-spin>
                    </div>

                    <!-- ===================== 浮动球设置 ===================== -->
                    <div v-show="activeTab === 'ball'" class="pane-body">
                        <a-form :model="form" :style="{ maxWidth: '720px' }" layout="vertical">
                            <a-form-item field="ballEnabled" data-skey="ball.enabled">
                                <template #label>显示桌面浮动球</template>
                                <a-switch v-model="form.ball.enabled" type="round" @change="scheduleSave(0)" />
                                <span class="hint">开启后浮动球常驻屏幕右下角（独立原生小窗，置顶、可拖动，点击展开快捷功能）。修改后立即显隐，并在下次启动时自动恢复。</span>
                            </a-form-item>
                            <a-form-item field="ballIcon" data-skey="ball.icon">
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
                                <span class="hint">浮动球中心图标（图标库或 Arco），颜色自动跟随主题正文色 var(--color-text-1)，球面底色跟随
                                    var(--color-bg-1)。留空使用应用 Logo/内置默认。</span>
                            </a-form-item>
                            <a-form-item field="ballHideAll" data-skey="ball.hideAllShortcut">
                                <template #label>显示/隐藏全部窗口热键</template>
                                <a-input :model-value="form.ball.hideAllShortcut" readonly allow-clear
                                    :placeholder="ballHaRecording ? '请按下组合键…（Esc 取消）' : '点击后直接按下组合键（如 Ctrl+Alt+H），清空即停用'"
                                    @click="ballHaStartRecord" @keydown="ballHaOnKeydown" @clear="ballHaClear">
                                    <template #suffix>
                                        <span v-if="ballHaRecording" class="rec-tip">录制中</span>
                                    </template>
                                </a-input>
                                <span class="hint">借鉴 DeskBox 的一键显隐：按下收起主窗口与所有工具窗口，再按恢复本次收起的那些；托盘与浮动球不受影响。</span>
                            </a-form-item>
                            <a-form-item field="ballDock" data-skey="ball.dockEdge">
                                <template #label>贴边自动收缩</template>
                                <a-switch type="round" v-model="form.ball.dockEdge" @change="scheduleSave(0)" />
                                <span class="hint">把浮动球拖到屏幕边缘时自动收成小胶囊 tab，指针碰到弹回完整球；拖离边缘即解除停靠。</span>
                            </a-form-item>
                        </a-form>
                    </div>

                    <!-- ===================== 快捷搜索设置 ===================== -->
                    <div v-show="activeTab === 'launcher'" class="pane-body">
                        <a-form :model="form" :style="{ maxWidth: '760px' }" layout="vertical">
                            <a-form-item field="lkEnabled" data-skey="launcher.enabled">
                                <template #label>启用快捷搜索</template>
                                <a-switch v-model="form.launcher.enabled" type="round" @change="onLauncherEnabled" />
                                <span class="hint">
                                    开启后才会挂载这套服务：注册全局热键、创建面板小窗、扫描目录建索引。
                                    关掉时三者一个都不留（面板当场收起、索引释放、热键注销），不会在后台留常驻开销。
                                </span>
                            </a-form-item>

                            <a-form-item field="lkShortcut" data-skey="launcher.shortcut">
                                <template #label>呼出热键</template>
                                <a-input :model-value="form.launcher.shortcut" readonly
                                    placeholder="点击后直接按下组合键（默认 Alt+Space）" @click="lkStartRecord"
                                    @keydown="lkOnShortcutKeydown">
                                    <template #suffix>
                                        <span v-if="lkRecording" class="rec-tip">录制中</span>
                                    </template>
                                </a-input>
                                <span class="hint">与工具热键撞车时以先注册者为准，注册失败会在日志里记一条告警。按 Esc 退出录制。</span>
                            </a-form-item>

                            <a-form-item field="lkScopes" data-skey="launcher.scopes">
                                <template #label>检索范围</template>
                                <a-space wrap>
                                    <a-checkbox v-model="form.launcher.scopes.apps"
                                        @change="scheduleSave(0)">已安装应用</a-checkbox>
                                    <a-checkbox v-model="form.launcher.scopes.tools"
                                        @change="scheduleSave(0)">本程序工具</a-checkbox>
                                    <a-checkbox v-model="form.launcher.scopes.windows"
                                        @change="scheduleSave(0)">运行中的窗口</a-checkbox>
                                    <a-checkbox v-model="form.launcher.scopes.files"
                                        @change="scheduleSave(0)">本地文件</a-checkbox>
                                    <a-checkbox v-model="form.launcher.scopes.commands" @change="scheduleSave(0)">命令 /
                                        算式 /
                                        网址</a-checkbox>
                                </a-space>
                            </a-form-item>

                            <a-form-item field="lkDirs" data-skey="launcher.dirs">
                                <template #label>额外索引目录</template>
                                <div class="lk-dirs">
                                    <div v-for="(d, i) in form.launcher.dirs" :key="i" class="lk-dir-row">
                                        <span class="lk-dir-path" :title="d">{{ d }}</span>
                                        <a-button size="mini" @click="removeLauncherDir(i)">移除</a-button>
                                    </div>
                                    <div v-if="!form.launcher.dirs.length" class="hint">未追加目录，仅索引下面列出的已知目录。</div>
                                    <a-button size="small" @click="addLauncherDir">添加目录</a-button>
                                </div>
                                <span class="hint">默认已纳入：{{ lkDefaultDirs.join('、') || '（没有可索引的已知目录）'
                                    }}；AppData、node_modules、回收站等一律跳过。</span>
                            </a-form-item>

                            <a-form-item field="lkMaxEntries" data-skey="launcher.maxEntries">
                                <template #label>索引条目上限</template>
                                <a-input-number v-model="form.launcher.maxEntries" :min="1000" :step="10000" hide-button
                                    @change="scheduleSave(600)" />
                                <span class="hint">撞顶时索引标记为“不全”，新文件可能搜不到；可提高上限，或把过深的目录从索引里移出去。</span>
                            </a-form-item>

                            <a-form-item field="lkIndex" data-skey="launcher.index">
                                <template #label>索引状态</template>
                                <a-space>
                                    <a-button size="small" :loading="lkRebuilding"
                                        @click="rebuildLauncherIndex">重建索引</a-button>
                                    <a-button size="small" @click="resetLauncherUsage">清空使用频次</a-button>
                                    <a-button size="small" :disabled="!form.launcher.enabled"
                                        @click="testLauncher">试一下面板</a-button>
                                </a-space>
                                <div class="lk-idx-line">
                                    条目 {{ lkIdx?.count || 0 }}
                                    <template v-if="lkIdx?.building"> · 正在建立…</template>
                                    <template v-else-if="lkIdx?.age"> · 上次构建 {{ lkIdx.age }} 前</template>
                                    <template v-else> · 从未构建</template>
                                    <template v-if="lkIdx?.partial"> · 已达上限（索引不全）</template>
                                </div>
                                <div v-if="lkIdx?.lastErr" class="lk-idx-err">{{ lkIdx.lastErr }}</div>
                                <span class="hint">未启用时不扫盘、不建索引，这里的条数会归零；“重建索引”会顺带刷新应用清单（刚装的程序搜不到就用它）。</span>
                            </a-form-item>

                            <a-form-item field="lkCommands" data-skey="launcher.commands">
                                <template #label>可搜的内置命令</template>
                                <div class="lk-cmds">
                                    <span v-for="c in lkCmds" :key="c.id" class="lk-cmd" :class="{ danger: c.danger }"
                                        :title="c.sub">{{ c.title }}</span>
                                </div>
                                <span class="hint">在搜索框里以 &gt; 开头可只搜命令，屏蔽应用与文件的干扰。</span>
                            </a-form-item>
                        </a-form>
                    </div>

                    <!-- ===================== 文件压缩设置 ===================== -->
                    <div v-show="activeTab === 'archive'" class="pane-body">
                        <a-form :model="form" :style="{ maxWidth: '760px' }" layout="vertical">
                            <a-form-item field="arEnabled" data-skey="archive.enabled">
                                <template #label>启用右键压缩</template>
                                <a-switch v-model="form.archive.enabled" type="round" @change="onArchiveEnabled" />
                                <span class="hint">
                                    开启后才会写注册表（HKCU，不需管理员）、生成「发送到」快捷方式、起本机转交监听。
                                    关掉时三者一个都不留（自家那几把键删干净、监听停掉），不占系统位置。
                                </span>
                            </a-form-item>

                            <a-form-item field="arShell" data-skey="archive.shell">
                                <template #label>当前挂载状态</template>
                                <div class="ar-shell-row">
                                    <span class="ar-slot">压缩菜单项：<b>{{ slotText(arShell?.compress) }}</b></span>
                                    <span class="ar-slot">解压菜单项：<b>{{ slotText(arShell?.extract) }}</b></span>
                                    <span class="ar-slot">发送到：<b>{{ slotText(arShell?.sendTo) }}</b></span>
                                </div>
                                <div class="ar-shell-btns">
                                    <a-button size="small" type="primary" status="success" :loading="arBusy"
                                        :disabled="!form.archive.enabled" @click="installArchiveShell">安装 /
                                        修复</a-button>
                                    <a-button size="small" :loading="arBusy"
                                        @click="uninstallArchiveShell">卸载</a-button>
                                </div>
                                <div v-if="arShell?.stale" class="hint">程序路径与注册表里记的不一致（换过目录），点「安装 / 修复」重写一次。</div>
                                <div v-else-if="arShell?.detail" class="hint">{{ arShell.detail }}</div>
                                <span class="hint">Win11 的静态右键菜单项落在「显示更多选项」二级菜单里；菜单文案带 (Tools) 以便与系统自带的「压缩为 ZIP
                                    文件」区分。解压冲突策略：目标已存在同名文件夹时自动让开为「名称 (1)」，只写入、绝不合并或覆盖原目录。</span>
                            </a-form-item>

                            <a-form-item field="arSendTo" data-skey="archive.sendTo">
                                <template #label>「发送到」快捷方式</template>
                                <a-switch v-model="form.archive.sendTo" type="round" @change="scheduleSave(0)" />
                                <span class="hint">多选文件后右键「发送到 → 压缩 (Tools)…」，会先弹一个选项小窗问格式与输出位置。</span>
                            </a-form-item>

                            <a-form-item field="arFormat" data-skey="archive.format">
                                <template #label>默认格式</template>
                                <a-radio-group v-model="form.archive.format" type="button" @change="scheduleSave(0)">
                                    <a-radio value="zip">zip</a-radio>
                                    <a-radio value="targz">tar.gz</a-radio>
                                </a-radio-group>
                                <span class="hint">右键直接压缩时没有界面，用的就是这一档；工具页与小窗里当场选过的不会被它盖掉。</span>
                            </a-form-item>

                            <a-form-item field="arLevel" data-skey="archive.level">
                                <template #label>默认压缩级别</template>
                                <a-select v-model="form.archive.level" style="width: 180px" @change="scheduleSave(0)">
                                    <a-option :value="1">快速（1）</a-option>
                                    <a-option :value="6">标准（6）</a-option>
                                    <a-option :value="9">最高（9）</a-option>
                                </a-select>
                                <span class="hint">级别只影响体积与耗时；tar.gz 在 Windows 上不带 unix 权限位，跨平台还原权限请用 tar
                                    类归档在类Unix系统上生成。</span>
                            </a-form-item>
                        </a-form>
                    </div>

                    <!-- ===================== Git 右键集成 ===================== -->
                    <div v-show="activeTab === 'git'" class="pane-body">
                        <a-form :model="form" :style="{ maxWidth: '760px' }" layout="vertical">
                            <a-form-item field="gitEnabled" data-skey="git.shellEnabled">
                                <template #label>启用 Git 右键集成</template>
                                <a-switch v-model="form.git.shellEnabled" type="round" @change="onGitEnabled" />
                                <span class="hint">
                                    开启后才会写注册表（HKCU，不需管理员）并起本机转交监听。关掉时一个都不留（自家 ToolsGit* 那几把键删干净、监听停），不占系统位置。
                                    Git 管理工具页本身不受此开关影响，一直可以在首页打开。
                                </span>
                            </a-form-item>

                            <a-form-item field="gitShell" data-skey="git.shell">
                                <template #label>当前挂载状态</template>
                                <div class="ar-shell-row">
                                    <span class="ar-slot">Git 菜单项：<b>{{ slotText(gitShell?.slot) }}</b></span>
                                </div>
                                <div class="ar-shell-btns">
                                    <a-button size="small" type="primary" status="success" :loading="gitBusy"
                                        :disabled="!form.git.shellEnabled" @click="installGitShell">安装 / 修复</a-button>
                                    <a-button size="small" :loading="gitBusy" @click="uninstallGitShell">卸载</a-button>
                                </div>
                                <div v-if="gitShell?.stale" class="hint">程序路径与注册表里记的不一致（换过目录），点「安装 / 修复」重写一次。</div>
                                <div v-else-if="gitShell?.detail" class="hint">{{ gitShell.detail }}</div>
                                <span class="hint">挂载「仓库状态 / 提交 / 推送 / 拉取 / 分支」五个菜单项于文件夹、文件与空白处三类位置；Win11 的静态右键项落在「显示更多选项」二级菜单里。
                                    菜单文案带 (Tools) 以便与 TortoiseGit 等别家区分。点某项时若该位置不在 Git 仓库内，会弹系统通知说明；命中仓库则在已运行的实例里打开 Git 工作台并定位到该仓库（推送/拉取/提交不盲跑，仍需你在工作台里确认）。</span>
                            </a-form-item>
                        </a-form>
                    </div>
                    <div v-show="activeTab === 'docspace'" class="pane-body">
                        <a-form :model="{}" :style="{ maxWidth: '720px' }" layout="vertical">
                            <a-alert type="normal" style="margin-bottom: 16px">
                                P1 已接入：分类树与标签、版本快照/还原、回收站（仅本机 local 源移实体）。加密/备份/分享/远程在后续期接入。
                            </a-alert>
                            <a-form-item label="启用文档中心" data-skey="docspace.enabled">
                                <a-switch v-model="docspaceForm.enabled" />
                                <span class="hint" style="margin-left: 10px">关闭后文件查看器左栏只保留“本机浏览”，不再展示“文档中心”态。</span>
                            </a-form-item>
                            <a-form-item label="每条目版本保留数" data-skey="docspace.versionKeep">
                                <a-input-number v-model="docspaceForm.versionKeep" :min="0" :max="1000" :step="1" style="width: 200px" />
                                <span class="hint" style="margin-left: 10px">存新快照后自动裁掉最旧的超额版本；0 表示不裁剪。</span>
                            </a-form-item>
                            <a-form-item label="回收站保留天数" data-skey="docspace.trashKeepDays">
                                <a-input-number v-model="docspaceForm.trashKeepDays" :min="0" :max="3650" :step="1" style="width: 200px" />
                                <span class="hint" style="margin-left: 10px">移入超过该天数的实体在打开回收站时自动彻底清理；0 表示不自动清理。</span>
                            </a-form-item>
                            <a-form-item>
                                <a-space>
                                    <a-button type="primary" :loading="docspaceSaving" @click="saveDocspace">保存设置</a-button>
                                    <a-button @click="loadDocspace">重载</a-button>
                                </a-space>
                            </a-form-item>
                            <a-divider style="margin: 4px 0 16px" />
                            <a-form-item label="库目录（元数据与实体快照落点）" data-skey="docspace.root">
                                <a-input :model-value="docspaceRoot" read-only />
                            </a-form-item>
                            <a-form-item label="概览" data-skey="docspace.counts">
                                <a-space wrap>
                                    <a-tag>分类 {{ docspaceCounts.categories }}</a-tag>
                                    <a-tag>条目 {{ docspaceCounts.items }}</a-tag>
                                    <a-tag>回收站 {{ docspaceCounts.trash }}</a-tag>
                                    <a-tag>加密 {{ docspaceCounts.secure }}</a-tag>
                                    <a-tag>分享 {{ docspaceCounts.shares }}</a-tag>
                                    <a-tag>备份计划 {{ docspaceCounts.backups }}</a-tag>
                                </a-space>
                            </a-form-item>
                        </a-form>
                    </div>

                    <!-- ===================== 云盘管理 ===================== -->
                    <div v-show="activeTab === 'cloud'" class="pane-body">
                        <a-form :model="{}" :style="{ maxWidth: '720px' }" layout="vertical">
                            <a-alert type="warning" style="margin-bottom: 16px">
                                本期（PR1）仅支持 <strong>Google Drive</strong>（官方 OAuth2）；阿里/百度与跨盘转存属后续。
                                授权令牌经 tools/secret 加密落盘（Windows DPAPI / Linux 0600 文件），回前端的账号列表不含任何密钥。
                                Google 的 client_id/secret 请自备并在下方填写，不内置。
                            </a-alert>
                            <a-form-item label="启用云盘管理" data-skey="cloud.enabled">
                                <a-switch v-model="cloudForm.enabled" />
                                <span class="hint" style="margin-left: 10px">关闭时云盘页不发起任何网络请求、不构造 provider。</span>
                            </a-form-item>
                            <a-form-item label="Google 客户端 ID" data-skey="cloud.clientId">
                                <a-input v-model="cloudForm.clientId" placeholder="xxxx.apps.googleusercontent.com" allow-clear />
                            </a-form-item>
                            <a-form-item label="Google 客户端密钥" data-skey="cloud.clientSecret">
                                <a-input-password v-model="cloudForm.clientSecret" placeholder="留空表示保持不变" allow-clear />
                                <span class="hint">为桌面应用性质，非真正的机密；留空则不覆盖已保存的值。</span>
                            </a-form-item>
                            <a-form-item label="OAuth 回调地址" data-skey="cloud.redirect">
                                <a-input v-model="cloudForm.redirect" placeholder="http://localhost:8458/callback" allow-clear />
                                <span class="hint">须与 Google 控制台登记的“已授权的重定向 URI”逐字一致。</span>
                            </a-form-item>
                            <a-form-item label="下载落地目录" data-skey="cloud.saveDir">
                                <a-input v-model="cloudForm.saveDir" placeholder="留空走数据目录下 clouddrive/downloads" allow-clear />
                            </a-form-item>
                            <a-form-item label="转存并发度" data-skey="cloud.concurrency">
                                <a-input-number v-model="cloudForm.concurrency" :min="1" :max="16" :step="1" style="width: 200px" />
                                <span class="hint" style="margin-left: 10px">本期为单连接，值留给后续跨盘转存。</span>
                            </a-form-item>
                            <a-form-item>
                                <a-space>
                                    <a-button type="primary" :loading="cloudSaving" @click="saveCloud">保存设置</a-button>
                                    <a-button @click="loadCloud">重载</a-button>
                                </a-space>
                            </a-form-item>
                        </a-form>
                    </div>

                    <!-- ===================== 下载管理 ===================== -->
                    <div v-show="activeTab === 'download'" class="pane-body">
                        <a-form :model="{}" :style="{ maxWidth: '760px' }" layout="vertical">
                            <a-alert type="info" style="margin-bottom: 16px">
                                分块/续传/限速/重试引擎全跨平台（Linux 面板下载的是服务器本机目录）。
                                这里的<strong>共享代理池</strong>供下载选源、仿站抓代理与浏览器 custom 代理（仅桌面）复用，减少重复录入。
                            </a-alert>
                            <a-form-item label="启用下载管理" data-skey="download.enabled">
                                <a-switch v-model="downloadForm.enabled" />
                                <span class="hint" style="margin-left: 10px">关闭时前端隐藏「下载管理」入口。</span>
                            </a-form-item>
                            <a-form-item label="默认下载目录" data-skey="download.saveDir">
                                <a-input v-model="downloadForm.saveDir" placeholder="留空走系统下载目录" allow-clear />
                                <span class="hint">新建任务未指定目录时落这里，保存后对新建任务即时生效。</span>
                            </a-form-item>
                            <a-form-item label="同时下载任务数" data-skey="download.maxRunningTasks">
                                <a-input-number v-model="downloadForm.maxRunningTasks" :min="1" :max="32" :step="1" style="width: 200px" />
                                <span class="hint" style="margin-left: 10px">超出的任务排队等额度。</span>
                            </a-form-item>
                            <a-form-item label="全局并发块数上限" data-skey="download.maxTotalParts">
                                <a-input-number v-model="downloadForm.maxTotalParts" :min="1" :max="256" :step="1" style="width: 200px" />
                                <span class="hint" style="margin-left: 10px">所有在跑任务合计的分块并发闸门。</span>
                            </a-form-item>
                            <a-form-item label="单任务默认分块数" data-skey="download.defaultConnections">
                                <a-input-number v-model="downloadForm.defaultConnections" :min="1" :max="32" :step="1" style="width: 200px" />
                                <span class="hint" style="margin-left: 10px">新建任务未指定连接数时用此值。</span>
                            </a-form-item>
                            <a-form-item label="全局下行限速（MB/s）" data-skey="download.speedLimitMB">
                                <a-input-number v-model="downloadForm.speedLimitMB" :min="0" :step="0.5" style="width: 200px" />
                                <span class="hint" style="margin-left: 10px">0 = 不限速。即时作用于所有在跑任务。</span>
                            </a-form-item>
                            <a-form-item label="共享代理池" data-skey="download.proxyPool">
                                <pool-editor :rows="proxyPool" name-placeholder="名称" url-placeholder="http://127.0.0.1:7890"
                                    add-label="添加代理" />
                                <span class="hint">http/https 代理（socks5 本期不保证）。浏览器 custom 代理可从这里下拉选。</span>
                            </a-form-item>
                            <a-form-item label="常用镜像模板" data-skey="download.mirrorPool">
                                <pool-editor :rows="mirrorPool" name-placeholder="名称" url-placeholder="https://mirror.example.com/file.zip"
                                    add-label="添加镜像" />
                                <span class="hint">新建下载任务时的多源候选，手动选 + 测速，不自动切换。</span>
                            </a-form-item>
                            <a-form-item>
                                <a-space>
                                    <a-button type="primary" :loading="downloadSaving" @click="saveDownload">保存设置</a-button>
                                    <a-button @click="loadDownload">重载</a-button>
                                </a-space>
                            </a-form-item>
                        </a-form>
                    </div>
                    <!-- ===================== 局域网互传 ===================== -->
                    <div v-show="activeTab === 'lanshare'" class="pane-body">
                        <a-form :model="{}" :style="{ maxWidth: '760px' }" layout="vertical">
                            <a-alert type="info" style="margin-bottom: 16px">
                                这里是互传服务的<strong>启动默认值</strong>：改完保存，下次在互传页点「启动」时预填生效；
                                已在跑的实例不受影响。接收类型黑名单在互传页改，即时对在跑服务生效。
                            </a-alert>
                            <a-form-item label="默认端口" data-skey="lanShare.port">
                                <a-input-number v-model="lanShareForm.port" :min="0" :max="65535" :step="1" :precision="0"
                                    style="width: 200px" placeholder="0 = 内置默认" />
                                <span class="hint" style="margin-left: 10px">0 取内置默认端口；被占用自动 +1 重试（最多 20 个）。</span>
                            </a-form-item>
                            <a-form-item label="监听范围" data-skey="lanShare.host">
                                <a-select v-model="lanShareForm.host" style="width: 260px">
                                    <a-option value="">全部网卡（局域网可访问）</a-option>
                                    <a-option value="127.0.0.1">仅本机回环</a-option>
                                </a-select>
                                <span class="hint" style="margin-left: 10px">局域网内任何设备都能往收件箱传文件，不需要时建议及时停服务。</span>
                            </a-form-item>
                            <a-form-item label="自动停止" data-skey="lanShare.autoStopMin">
                                <a-select v-model="lanShareForm.autoStopMin" style="width: 200px">
                                    <a-option :value="30">30 分钟后</a-option>
                                    <a-option :value="120">2 小时后</a-option>
                                    <a-option :value="0">不自动停</a-option>
                                </a-select>
                                <span class="hint" style="margin-left: 10px">到点自停并在界面提示，避免传完忘了关。</span>
                            </a-form-item>
                            <a-form-item label="访问 PIN（可选）" data-skey="lanShare.pin">
                                <a-input v-model="lanShareForm.pin" placeholder="留空 = 免密" allow-clear style="width: 260px" />
                                <span class="hint">仅挡无意访问，不是访问控制；明文存本机 config.json。</span>
                            </a-form-item>
                            <a-form-item>
                                <a-space>
                                    <a-button type="primary" :loading="lanShareSaving" @click="saveLanShare">保存设置</a-button>
                                    <a-button @click="loadLanShare">重载</a-button>
                                </a-space>
                            </a-form-item>
                        </a-form>
                    </div>
                    <div v-show="activeTab === 'tools'" class="pane-body tools-pane" data-skey="tools.list">
                            <div class="tools-table-wrap" ref="toolsTableRef" @dragover="onToolsWrapDragOver"
                                @dragleave="onToolsWrapDragLeave" @drop="onToolsWrapDrop">
                                <a-table :data="tools" :pagination="false" size="small" row-key="id"
                                    :column-resizable="true" :scroll="tableScroll">
                                    <template #columns>
                                        <a-table-column title="排序" :width="112">
                                            <template #cell="{ rowIndex }">
                                                <a-space :size="2">
                                                    <!-- 拖拽把手：按住它在目标行上松手即可换位；上下钮保留做微调 -->
                                                    <a-tooltip content="按住拖动排序">
                                                        <span class="row-drag-handle" draggable="true"
                                                            @dragstart="toolDragStart(rowIndex, $event)"
                                                            @dragend="toolDragEnd">
                                                            <icon-drag-dot-vertical />
                                                        </span>
                                                    </a-tooltip>
                                                    <a-button size="small" :disabled="rowIndex === 0"
                                                        @click="move(rowIndex, -1)">
                                                        <icon-up />
                                                    </a-button>
                                                    <a-button size="small" :disabled="rowIndex === tools.length - 1"
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
                                                    <component v-else :is="iconComp(record.icon)"
                                                        class="arco-preview" />
                                                    <a-tooltip :content="record.icon">
                                                        <span class="icon-text">{{ iconLabel(record.icon) }}</span>
                                                    </a-tooltip>
                                                </span>
                                                <span v-else class="off">—</span>
                                            </template>
                                        </a-table-column>
                                        <a-table-column title="主页" :width="60">
                                            <template #cell="{ record }">
                                                <icon-check-circle v-if="record.home !== false" class="on" />
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
                    </div>
                    <!-- ===================== 浏览器（全局默认） ===================== -->
                    <div v-show="activeTab === 'browser'" class="pane-body">
                        <a-form :model="browserForm" :style="{ maxWidth: '720px' }" layout="vertical">
                            <a-form-item data-skey="browser.maxSpaces">
                                <template #label>空间数量上限</template>
                                <a-input-number v-model="browserForm.maxSpaces" :min="spaceMin" :max="20"
                                    @change="scheduleBrowserSave(0)" />
                                <span class="hint">
                                    当前已有 {{ browserForm.spaces.length }} 个空间，最多可建 {{ browserForm.maxSpaces }} 个。
                                    一个空间 = 一套独立 profile（登录态/缓存）+ 一套独立代理，空间内多标签共享登录态。
                                </span>
                            </a-form-item>
                            <a-form-item data-skey="browser.homepage">
                                <template #label>默认首页</template>
                                <a-input v-model="browserForm.homepage" placeholder="about:blank" allow-clear
                                    @blur="scheduleBrowserSave(0)" @press-enter="scheduleBrowserSave(0)"
                                    @clear="scheduleBrowserSave(0)" />
                                <span class="hint">空间里留空首页时用这个；新标签与“关光标签自动补一个”都用它。</span>
                            </a-form-item>
                            <a-form-item data-skey="browser.searchURL">
                                <template #label>地址栏搜索模板</template>
                                <a-input v-model="browserForm.searchURL" placeholder="https://www.bing.com/search?q=%s"
                                    allow-clear @blur="scheduleBrowserSave(0)" @press-enter="scheduleBrowserSave(0)"
                                    @clear="scheduleBrowserSave(0)" />
                                <span class="hint">地址栏输入的不是网址时走这个地址，<code>%s</code> 会被替换成搜索词。</span>
                            </a-form-item>
                            <a-form-item data-skey="browser.proxyMode">
                                <template #label>默认代理</template>
                                <a-radio-group size="medium" v-model="browserForm.proxyMode" type="button"
                                    @change="scheduleBrowserSave(0)">
                                    <a-radio value="system">跟随系统</a-radio>
                                    <a-radio value="direct">直连</a-radio>
                                    <a-radio value="custom">自定义</a-radio>
                                </a-radio-group>
                                <span class="hint">空间里没单独设置代理时用这档；“跟随系统”就是系统代理设置，不做任何覆盖。</span>
                            </a-form-item>
                            <a-form-item v-if="browserForm.proxyMode === 'custom'" data-skey="browser.proxy">
                                <template #label>代理地址与绕过列表</template>
                                <div class="bs-pair">
                                    <a-input v-model="browserForm.proxy" allow-clear
                                        placeholder="http://127.0.0.1:7890 或 socks5://127.0.0.1:1080"
                                        @blur="scheduleBrowserSave(0)" @press-enter="scheduleBrowserSave(0)" />
                                    <a-input v-model="browserForm.proxyBypass" allow-clear
                                        placeholder="绕过：localhost,127.0.0.1" @blur="scheduleBrowserSave(0)"
                                        @press-enter="scheduleBrowserSave(0)" />
                                </div>
                                <span class="hint">代理与 UA 是环境级参数，改完需要重新打开浏览器页（或在齿轮里保存）才生效。</span>
                            </a-form-item>
                            <a-form-item data-skey="browser.userAgent">
                                <template #label>默认 User-Agent</template>
                                <a-textarea v-model="browserForm.userAgent" :auto-size="{ minRows: 1, maxRows: 3 }"
                                    placeholder="留空使用内置默认 UA" allow-clear @blur="scheduleBrowserSave(0)" />
                            </a-form-item>
                            <a-form-item data-skey="browser.defaultSpace">
                                <template #label>启动时打开的空间</template>
                                <a-select v-model="browserForm.defaultSpace" :style="{ maxWidth: '280px' }"
                                    placeholder="选择默认空间" @change="scheduleBrowserSave(0)">
                                    <a-option v-for="s in browserForm.spaces" :key="s.id" :value="s.id"
                                        :label="s.name ? `${s.name}（${s.id}）` : s.id" />
                                </a-select>
                            </a-form-item>
                        </a-form>

                        <a-divider :margin="8" />
                        <div class="space-head" data-skey="browser.spaces">
                            <span class="space-title">空间列表</span>
                            <a-tooltip content="增删空间、单独设置代理/UA/首页请到浏览器页右上角齿轮">
                                <icon-question-circle class="bs-help" />
                            </a-tooltip>
                        </div>
                        <a-table :data="spaceRows" :pagination="false" size="small" row-key="id" :scroll="{ x: 640 }"
                            class="space-table">
                            <template #columns>
                                <a-table-column title="名称" data-index="name" :width="130" ellipsis tooltip />
                                <a-table-column title="标识" data-index="id" :width="110" ellipsis tooltip />
                                <a-table-column title="首页" data-index="homepage" ellipsis tooltip />
                                <a-table-column title="代理" :width="150" ellipsis tooltip>
                                    <template #cell="{ record }">
                                        <a-tag size="small">{{ record.proxyText }}</a-tag>
                                    </template>
                                </a-table-column>
                                <a-table-column title="UA" :width="110" ellipsis tooltip>
                                    <template #cell="{ record }">
                                        <span v-if="record.uaText" class="on">{{ record.uaText }}</span>
                                        <span v-else class="off">跟随全局</span>
                                    </template>
                                </a-table-column>
                            </template>
                        </a-table>
                    </div>

                    <!-- ===================== 配置导出 / 导入 ===================== -->
                    <div v-show="activeTab === 'config'" class="pane-body">
                        <div class="cfg-sync" data-skey="config.sync">
                            <div class="cfg-sync-label">导出 / 导入配置</div>
                            <a-space>
                                <a-button size="small" @click="exportConfig">
                                    <template #icon><icon-export /></template>导出配置
                                </a-button>
                                <a-popconfirm content="导入会覆盖本机的工具列表、浏览器空间与代理、主题等设置（目录 / Logo / 浮动球位置保留本机值）。继续？"
                                    type="warning" position="lt" @ok="importConfig">
                                    <a-button size="small" type="primary">
                                        <template #icon><icon-import /></template>导入配置
                                    </a-button>
                                </a-popconfirm>
                                <a-button size="small" @click="openConfigDir">打开配置目录</a-button>
                            </a-space>
                            <div class="hint">
                                导出的是一份可搬运的 JSON：工具列表、工具设置、浏览器空间与代理、主题、开机自启。
                                机器绑定字段不会导出——浮动球位置、插件/数据/日志目录、Logo 与 SQL 日志的绝对路径。
                                把文件放到云盘或另一台机器上，用“导入配置”即可恢复同样的环境。
                            </div>
                        </div>
                    </div>
                    </a-card>
                </div>
            </div>
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
                        <a-button size="small"
                            @click="() => { proxy?.$refs['selectModalIconRef']?.open(); }">图标</a-button>
                        <select-icon-modal ref="selectModalIconRef"
                            :type="isIconfontIcon(editForm.icon) ? 'iconfont' : 'arco'"
                            @change="iconChange"></select-icon-modal>
                    </template>
                </a-input>
                <div class="hint">支持 Arco 图标名（如 camera）与图标库 icon-xxx（如 icon-lianjie），点击右侧“图标”选择。</div>
            </a-form-item>
            <a-form-item field="home" label="主页展示">
                <a-switch v-model="editForm.home" type="round" />
                <div class="hint">开启后在主页“快捷操作”区直接展示；关闭后仍可在该区“更多”弹窗里找到。</div>
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
                <a-input :model-value="editForm.shortcut" readonly
                    :placeholder="recording ? '请按下组合键…（Esc 取消）' : '点击后直接按下组合键（如 Ctrl+Alt+S），留空不设置'" @click="startRecord"
                    @keydown="onShortcutKeydown" allow-clear @clear="editForm.shortcut = ''">
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
import { Events } from "@wailsio/runtime";
import { useRoute } from "vue-router";
import { Message } from "@arco-design/web-vue";
import * as ArcoIconComps from "@arco-design/web-vue/es/icon";
import iconData from "@/components/common/icon/select-icon-modal/icon.json";
import {
    applyThemeConfig,
    normalizeConfig,
    DEFAULT_THEME,
    type ThemeConfig,
} from "@/utils/theme";
import { useCaps } from "@/utils/caps";
import { changePanelPassword, getPanelInfo, savePanelSettings, type PanelInfo } from "@/utils/panel";
import PoolEditor, { type PoolRow } from "./PoolEditor.vue";
import {
    arcoIconComp as iconComp,
    DEFAULT_TOOL_ICONS,
    iconLabel,
    isIconfontIcon,
    normalizeToolIcon,
} from "@/utils/tool-icon";

const { proxy } = getCurrentInstance() as any;
const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
/** 平台能力：决定哪些设置分类（浮动球、浏览器空间）给不给入口 */
const caps = useCaps();

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
    builtin("sniffer", "抓包工具", { path: "/tools/sniffer", single: true, fullscreen: true, quick: true }),
    builtin("websocket", "WebSocket调试", { path: "/tools/websocket", fullscreen: true }),
    builtin("usbserial", "USB调试", { path: "/tools/usbserial" }),
    builtin("redis", "Redis管理", { path: "/tools/redis", single: true, fullscreen: true }),
    builtin("mysql", "MySQL管理", { path: "/tools/mysql", single: true, fullscreen: true }),
    builtin("php", "PHP管理", { path: "/tools/php", single: true, width: 1200, height: 800 }),
    builtin("nginx", "Nginx管理", { path: "/tools/nginx", single: true, width: 1200, height: 800 }),
    builtin("docker", "Docker管理", { path: "/tools/docker", single: true, width: 1200, height: 800 }),
    builtin("npm", "Npm管理", { path: "/tools/npm", single: true, width: 1200, height: 800 }),
    builtin("settings", "设置", { path: "/tools/settings", single: true, quick: true }),
];

interface ToolRow {
    id: string; key: string; name: string; path: string; event: string; icon: string;
    single: boolean; fullscreen: boolean; disableFullscreen: boolean; width: number; height: number; shortcut: string; quick: boolean; home: boolean;
}

/** 完整配置（保存时整体回传） */
const fullConfig = ref<any>({ log: { enabled: true, level: "info", path: "" }, ball: { enabled: false, icon: "" }, general: { autoStart: false, appName: "Tools", appLogo: "", theme: { ...DEFAULT_THEME } }, paths: { plugins: "", data: "", logs: "", downloads: "" }, tools: [] });
const form = reactive<{
    log: { enabled: boolean; level: string; path: string };
    ball: { enabled: boolean; icon: string; hideAllShortcut: string; dockEdge: boolean };
    general: { autoStart: boolean; appName: string; appLogo: string; theme: ThemeConfig };
    paths: { plugins: string; data: string; logs: string; downloads: string };
    launcher: LauncherCfg;
    archive: ArchiveCfg;
    git: GitCfg;
}>({
    log: { enabled: true, level: "info", path: "" },
    ball: { enabled: false, icon: "", hideAllShortcut: "", dockEdge: true },
    general: { autoStart: false, appName: "Tools", appLogo: "", theme: { ...DEFAULT_THEME } },
    paths: { plugins: "", data: "", logs: "", downloads: "" },
    // 默认关闭：与后端 defaultLauncherConfig 一致，除开关外其余字段都是可直接编辑的初值
    launcher: {
        enabled: false,
        shortcut: "Alt+Space",
        scopes: { apps: true, tools: true, windows: true, files: true, commands: true },
        dirs: [],
        maxEntries: 60000,
    },
    // 默认关闭 + zip + 标准（6）：与后端 defaultArchiveConfig 逐项一致
    archive: {
        enabled: false,
        sendTo: false,
        format: "zip",
        level: 6,
    },
    // 右键集成默认关：与后端 defaultGitConfig 一致
    git: {
        shellEnabled: false,
    },
});
const tools = ref<ToolRow[]>([]);

const saving = ref(false);
let saveTimer = 0;
let saveChain: Promise<void> = Promise.resolve();

/**
 * 设置分类：左侧导航与右侧面板标题共用。
 * 浮动球与浏览器默认设置只在能开原生窗口的桌面版里有意义，Web 模式下直接不给入口。
 */
const SETTING_TABS = computed(() =>
    [
        { key: "general", label: "通用", heading: "应用与外观", icon: "IconSettings", desktopOnly: false },
        { key: "theme", label: "主题", heading: "主题与外观", icon: "IconPalette", desktopOnly: false },
        { key: "paths", label: "目录", heading: "全局目录布局", icon: "IconFolder", desktopOnly: false },
        { key: "panel", label: "访问与安全", heading: "面板访问", icon: "IconLock", desktopOnly: false, webOnly: true },
        { key: "ball", label: "浮动球", heading: "桌面浮动球", icon: "IconDesktop", desktopOnly: true },
        { key: "launcher", label: "快捷搜索", heading: "快捷搜索（Alt+Space 面板）", icon: "IconSearch", desktopOnly: true },
        { key: "archive", label: "文件压缩", heading: "文件压缩（系统右键挂载）", icon: "IconArchive", desktopOnly: true },
        { key: "git", label: "Git 右键", heading: "Git（系统右键挂载）", icon: "IconCode", desktopOnly: true },
        { key: "docspace", label: "文档中心", heading: "文档中心", icon: "IconFolder", desktopOnly: false },
        { key: "cloud", label: "云盘管理", heading: "云盘管理", icon: "IconCloud", desktopOnly: false },
        { key: "download", label: "下载管理", heading: "下载管理", icon: "IconSave", desktopOnly: false },
        { key: "lanshare", label: "局域网互传", heading: "局域网互传", icon: "IconSend", desktopOnly: false },
        { key: "browser", label: "浏览器", heading: "浏览器默认设置", icon: "IconCompass", desktopOnly: true },
        { key: "config", label: "配置", heading: "配置同步", icon: "IconSync", desktopOnly: false },
        { key: "tools", label: "工具", heading: "功能列表", icon: "IconApps", desktopOnly: false },
    ]
        .filter((t) => !t.desktopOnly || caps.nativeWindows)
        .filter((t) => !t.webOnly || !caps.nativeWindows),
);
const activeTab = ref("general");
const activeTabMeta = computed(() => SETTING_TABS.value.find((t) => t.key === activeTab.value) || SETTING_TABS.value[0]);

const route = useRoute();
/**
 * 定位到指定设置分类：托盘“切换主题”那条入口用的就是它。
 * 两条路互补：新开窗口（含原生单例窗）靠 URL 上的 ?tab=，挂载时落一次；
 * 窗口早已开着时单例复用不会重新挂载，靠后端广播的 settings:tab 事件切。
 * 只认 SETTING_TABS 里真实存在的 key（Web 模式下就没有浮动球等原生分类），
 * 认不出就当没这次请求，绝不把用户带到一个空白面板。
 */
function gotoTab(key: any) {
    const k = String(key ?? "");
    if (k && SETTING_TABS.value.some((t) => t.key === k)) activeTab.value = k;
}

/* ===================== 设置项全局搜索（借鉴 DeskBox「搜到任意设置」） =====================
 * 设置项分布在多个 v-show 面板里、没有统一元数据，采用手工维护的静态索引：
 * skey 与模板里各表单项的 data-skey 属性一一对应，新增设置项时同步补一条索引即可。 */
type SettingSearchEntry = { skey: string; label: string; hint: string; tab: string };
const SEARCH_INDEX: SettingSearchEntry[] = [
    { skey: "general.appName", label: "应用名称", hint: "窗口标题、托盘提示显示的名称", tab: "general" },
    { skey: "general.appLogo", label: "应用 Logo", hint: "用于浮动球中心图标，留空使用内置默认", tab: "general" },
    { skey: "general.theme", label: "主题搭配", hint: "明暗模式 / 种子色（预设或自定义取色）/ 色彩和谐搭配 / 圆角 / 字体大小 / 密度 / 高对比度 / 减少动效，切换后所有窗口同步", tab: "theme" },
    { skey: "general.theme.fontSize", label: "字体大小", hint: "整体字号，独立于显示密度，小/标准/大/特大", tab: "theme" },
    { skey: "general.autoStart", label: "开机默认启动", hint: "Windows 登录时自动启动应用", tab: "general" },
    { skey: "paths.plugins", label: "插件目录（plugins）", hint: "PHP/Nginx/MySQL/Redis/npm 服务程序安装位置", tab: "paths" },
    { skey: "paths.data", label: "数据目录（data）", hint: "服务配置缓存与实例工作目录；截图另存、录像、站点根默认落点", tab: "paths" },
    { skey: "paths.downloads", label: "下载目录（downloads）", hint: "下载管理器新建任务的默认落点", tab: "paths" },
    { skey: "log.enabled", label: "启用文件日志", hint: "关闭后不再写入日志文件", tab: "paths" },
    { skey: "paths.logs", label: "日志目录（logs）", hint: "程序与各服务日志的根目录", tab: "paths" },
    { skey: "panel.username", label: "登录账号", hint: "改账号后其它已登录设备立即失效", tab: "panel" },
    { skey: "panel.sessionHours", label: "登录有效期（小时）", hint: "会话过期后需要重新登录", tab: "panel" },
    { skey: "panel.host", label: "监听地址", hint: "0.0.0.0 所有网卡 / 127.0.0.1 仅本机", tab: "panel" },
    { skey: "panel.port", label: "端口", hint: "面板 HTTP 监听端口", tab: "panel" },
    { skey: "panel.entryPath", label: "安全入口", hint: "必须先访问入口地址才能打开登录页，挡扫描机器人", tab: "panel" },
    { skey: "panel.enabled", label: "登录保护", hint: "关闭后任何人访问面板地址都不需要登录", tab: "panel" },
    { skey: "panel.password", label: "修改登录密码", hint: "bcrypt 加盐存储；更新后其它设备下线", tab: "panel" },
    { skey: "ball.enabled", label: "显示桌面浮动球", hint: "常驻屏幕右下角的原生小窗，置顶、可拖动", tab: "ball" },
    { skey: "ball.icon", label: "浮动球图标", hint: "球中心图标，颜色自动跟随主题", tab: "ball" },
    { skey: "ball.hideAllShortcut", label: "显示/隐藏全部窗口热键", hint: "一键收起主窗口与所有工具窗口，再按恢复", tab: "ball" },
    { skey: "ball.dockEdge", label: "贴边自动收缩", hint: "浮动球拖到屏幕边缘时收成小胶囊，碰它弹回", tab: "ball" },
    { skey: "launcher.enabled", label: "启用快捷搜索", hint: "Alt+Space 呼出搜索面板", tab: "launcher" },
    { skey: "launcher.shortcut", label: "呼出热键", hint: "默认 Alt+Space，可与工具热键共存", tab: "launcher" },
    { skey: "launcher.scopes", label: "检索范围", hint: "应用 / 工具 / 窗口 / 文件 / 命令算式网址", tab: "launcher" },
    { skey: "launcher.dirs", label: "额外索引目录", hint: "默认已纳入已知目录，AppData 等跳过", tab: "launcher" },
    { skey: "launcher.maxEntries", label: "索引条目上限", hint: "撞顶时索引标记为不全", tab: "launcher" },
    { skey: "launcher.index", label: "索引状态", hint: "重建索引、清空使用频次", tab: "launcher" },
    { skey: "launcher.commands", label: "可搜的内置命令", hint: "搜索框以 > 开头只搜命令", tab: "launcher" },
    { skey: "archive.enabled", label: "启用右键压缩", hint: "写注册表挂载系统右键的压缩/解压", tab: "archive" },
    { skey: "archive.shell", label: "当前挂载状态", hint: "压缩/解压菜单项与注册表的一致性检查", tab: "archive" },
    { skey: "archive.sendTo", label: "「发送到」快捷方式", hint: "多选文件后右键发送到压缩", tab: "archive" },
    { skey: "archive.format", label: "默认格式", hint: "右键直接压缩时用的格式（zip / tar.gz）", tab: "archive" },
    { skey: "archive.level", label: "默认压缩级别", hint: "级别只影响体积与耗时", tab: "archive" },
    { skey: "docspace.enabled", label: "启用文档中心", hint: "关闭后文件查看器不再展示文档中心态", tab: "docspace" },
    { skey: "docspace.versionKeep", label: "每条目版本保留数", hint: "存新快照后自动裁掉最旧的超额版本", tab: "docspace" },
    { skey: "docspace.trashKeepDays", label: "回收站保留天数", hint: "超龄实体在打开回收站时自动彻底清理", tab: "docspace" },
    { skey: "docspace.root", label: "库目录", hint: "元数据与实体快照落点（只读）", tab: "docspace" },
    { skey: "docspace.counts", label: "概览", hint: "分类/条目/回收站/加密/分享/备份计数", tab: "docspace" },
    { skey: "cloud.enabled", label: "启用云盘管理", hint: "关闭时云盘页不发起任何网络请求", tab: "cloud" },
    { skey: "cloud.clientId", label: "Google 客户端 ID", hint: "OAuth 应用凭证，自备不内置", tab: "cloud" },
    { skey: "cloud.clientSecret", label: "Google 客户端密钥", hint: "留空则不覆盖已保存的值", tab: "cloud" },
    { skey: "cloud.redirect", label: "OAuth 回调地址", hint: "须与 Google 控制台登记逐字一致", tab: "cloud" },
    { skey: "cloud.saveDir", label: "下载落地目录", hint: "留空走数据目录下 clouddrive/downloads", tab: "cloud" },
    { skey: "cloud.concurrency", label: "转存并发度", hint: "本期为单连接，值留给后续", tab: "cloud" },
    { skey: "download.enabled", label: "启用下载管理", hint: "关闭时前端隐藏下载管理入口", tab: "download" },
    { skey: "lanShare.port", label: "互传默认端口", hint: "0 取内置默认，占用自动 +1", tab: "lanshare" },
    { skey: "lanShare.host", label: "互传监听范围", hint: "全部网卡或仅本机回环", tab: "lanshare" },
    { skey: "lanShare.autoStopMin", label: "互传自动停止", hint: "到点自停，避免传完忘了关", tab: "lanshare" },
    { skey: "lanShare.pin", label: "互传访问 PIN", hint: "仅挡无意访问，不是访问控制", tab: "lanshare" },
    { skey: "download.saveDir", label: "默认下载目录", hint: "新建任务未指定目录时落这里", tab: "download" },
    { skey: "download.maxRunningTasks", label: "同时下载任务数", hint: "超出的任务排队等额度", tab: "download" },
    { skey: "download.maxTotalParts", label: "全局并发块数上限", hint: "所有在跑任务合计的分块并发闸门", tab: "download" },
    { skey: "download.defaultConnections", label: "单任务默认分块数", hint: "新建任务未指定连接数时用此值", tab: "download" },
    { skey: "download.speedLimitMB", label: "全局下行限速（MB/s）", hint: "0 = 不限速，即时作用于所有在跑任务", tab: "download" },
    { skey: "download.proxyPool", label: "共享代理池", hint: "下载选源、仿站、浏览器 custom 代理复用", tab: "download" },
    { skey: "download.mirrorPool", label: "常用镜像模板", hint: "新建下载任务的多源候选", tab: "download" },
    { skey: "browser.maxSpaces", label: "空间数量上限", hint: "一个空间 = 一套独立 profile + 代理", tab: "browser" },
    { skey: "browser.homepage", label: "默认首页", hint: "空间留空首页时用这个", tab: "browser" },
    { skey: "browser.searchURL", label: "地址栏搜索模板", hint: "非网址输入走这个地址，%s 替换成搜索词", tab: "browser" },
    { skey: "browser.proxyMode", label: "默认代理", hint: "跟随系统 / 直连 / 自定义", tab: "browser" },
    { skey: "browser.proxy", label: "代理地址与绕过列表", hint: "环境级参数，改完需重开浏览器页", tab: "browser" },
    { skey: "browser.userAgent", label: "默认 User-Agent", hint: "留空使用内置默认 UA", tab: "browser" },
    { skey: "browser.defaultSpace", label: "启动时打开的空间", hint: "浏览器页启动默认选中的空间", tab: "browser" },
    { skey: "browser.spaces", label: "空间列表", hint: "增删空间去浏览器页右上角齿轮", tab: "browser" },
    { skey: "config.sync", label: "导出 / 导入配置", hint: "可搬运的 JSON；机器绑定字段不导出", tab: "config" },
    { skey: "tools.list", label: "功能列表", hint: "工具排序、快捷方式、隐藏与自定义功能", tab: "tools" },
];

const searchKw = ref("");
function tabLabel(key: string): string {
    return SETTING_TABS.value.find((t) => t.key === key)?.label || key;
}
const searchResults = computed(() => {
    const kw = searchKw.value.trim().toLowerCase();
    if (!kw) return [];
    return SEARCH_INDEX
        .filter((e) => (e.label + " " + e.hint + " " + tabLabel(e.tab)).toLowerCase().includes(kw))
        .slice(0, 30);
});
function escapeHtml(s: string): string {
    return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}
/** 命中词包 <mark>；文案全部来自本文件静态字符串，转义后再插入即可 */
function hlHtml(s: string): string {
    const esc = escapeHtml(s);
    const kw = searchKw.value.trim();
    if (!kw) return esc;
    const i = esc.toLowerCase().indexOf(kw.toLowerCase());
    if (i < 0) return esc;
    return esc.slice(0, i) + "<mark>" + esc.slice(i, i + kw.length) + "</mark>" + esc.slice(i + kw.length);
}
function jumpToSetting(skey: string) {
    const e = SEARCH_INDEX.find((x) => x.skey === skey);
    if (!e) return;
    searchKw.value = "";
    activeTab.value = e.tab;
    nextTick(() => {
        const el = document.querySelector<HTMLElement>(`.settings-page [data-skey="${skey}"]`);
        if (!el) return;
        el.scrollIntoView({ block: "center", behavior: "smooth" });
        el.classList.remove("s-flash");
        void el.offsetWidth; // 重启动画：同一个项连点两次也要再闪一次
        el.classList.add("s-flash");
        window.setTimeout(() => el.classList.remove("s-flash"), 1400);
    });
}

/* ===================== 文档中心（设置 + 概览） =====================
 * 库目录与计数只读展示；总开关 / 版本保留数 / 回收站保留天数可编辑保存
 * （跨平台：配置段与绑定均无 build tag，Linux 面板同样可读写）。 */
const docspaceRoot = ref("");
const docspaceCounts = ref({ categories: 0, items: 0, trash: 0, secure: 0, shares: 0, backups: 0 });
const docspaceSaving = ref(false);
const docspaceForm = reactive({ enabled: true, versionKeep: 20, trashKeepDays: 30 });
async function loadDocspace() {
    try {
        const [r, c, s] = await Promise.all([GoTools?.DocSpaceRoot(), GoTools?.DocSpaceCounts(), GoTools?.DocSpaceSettings()]);
        docspaceRoot.value = String(r ?? "");
        if (c) docspaceCounts.value = c as typeof docspaceCounts.value;
        if (s) {
            docspaceForm.enabled = !!s.enabled;
            docspaceForm.versionKeep = Number(s.versionKeep ?? 0);
            docspaceForm.trashKeepDays = Number(s.trashKeepDays ?? 0);
        }
    } catch {
        /* 静默：后端未连时不刷屏 */
    }
}
async function saveDocspace() {
    docspaceSaving.value = true;
    try {
        await GoTools?.SaveDocSpaceSettings(docspaceForm.enabled, docspaceForm.versionKeep, docspaceForm.trashKeepDays);
        Message.success("文档中心设置已保存");
        await loadDocspace();
    } catch (e: any) {
        Message.error(`保存失败：${String(e?.message ?? e ?? "操作失败")}`);
    } finally {
        docspaceSaving.value = false;
    }
}
watch(activeTab, (k) => { if (k === "docspace") void loadDocspace(); });

/* ===================== 云盘管理（设置） =====================
 * Google OAuth 客户端凭证与落地目录；clientSecret 不回显（留空=保持不变）。
 * 跨平台：配置段与绑定均无 build tag，Linux 面板同样可读写。 */
const cloudForm = reactive({ enabled: false, clientId: "", clientSecret: "", redirect: "", saveDir: "", concurrency: 3 });
const cloudSaving = ref(false);
async function loadCloud() {
    try {
        const s = await GoTools?.CloudSettings();
        if (s) {
            cloudForm.enabled = !!s.enabled;
            cloudForm.clientId = String(s.googleClientId ?? "");
            cloudForm.redirect = String(s.googleRedirectUri ?? "");
            cloudForm.saveDir = String(s.saveDir ?? "");
            cloudForm.concurrency = Number(s.concurrency ?? 3);
        }
        cloudForm.clientSecret = "";
    } catch {
        /* 静默：后端未连时不刷屏 */
    }
}
async function saveCloud() {
    cloudSaving.value = true;
    try {
        await GoTools?.SaveCloudSettings(
            cloudForm.enabled, cloudForm.clientId, cloudForm.clientSecret,
            cloudForm.redirect, cloudForm.saveDir, cloudForm.concurrency,
        );
        Message.success("云盘设置已保存");
        cloudForm.clientSecret = "";
        await loadCloud();
    } catch (e: any) {
        Message.error(`保存失败：${String(e?.message ?? e ?? "操作失败")}`);
    } finally {
        cloudSaving.value = false;
    }
}
watch(activeTab, (k) => { if (k === "cloud") void loadCloud(); });

/* ===================== 下载管理（设置） =====================
 * 引擎护栏参数（并发/默认分块/全局限速）+ 全局下载目录 + 共享代理池/镜像池。
 * 跨平台：配置段与绑定均无 build tag，Linux 面板同样可读写；限速按 MB/s 编辑、落 bps。
 * 保存后端会回灌引擎单例（ApplyConfig），改目录/并发无需重启即对新建任务生效。 */
const downloadSaving = ref(false);
const downloadForm = reactive({
    enabled: true,
    saveDir: "",
    maxRunningTasks: 3,
    maxTotalParts: 32,
    defaultConnections: 4,
    speedLimitMB: 0, // 0 = 不限速
});
const proxyPool = ref<PoolRow[]>([]);
const mirrorPool = ref<PoolRow[]>([]);
async function loadDownload() {
    try {
        const s = await GoTools?.DownloadSettings();
        if (!s) return;
        downloadForm.enabled = !!s.enabled;
        downloadForm.saveDir = String(s.saveDir ?? "");
        downloadForm.maxRunningTasks = Number(s.maxRunningTasks ?? 3);
        downloadForm.maxTotalParts = Number(s.maxTotalParts ?? 32);
        downloadForm.defaultConnections = Number(s.defaultConnections ?? 4);
        downloadForm.speedLimitMB = Number(s.globalSpeedLimitBps ?? 0) / 1048576;
        proxyPool.value = (s.proxyPool ?? []).map((p: any) => ({ id: String(p.id ?? ""), name: String(p.name ?? ""), url: String(p.url ?? "") }));
        mirrorPool.value = (s.mirrors ?? []).map((m: any) => ({ id: String(m.id ?? ""), name: String(m.name ?? ""), url: String(m.url ?? "") }));
    } catch {
        /* 静默：后端未连时不刷屏 */
    }
}
async function saveDownload() {
    downloadSaving.value = true;
    try {
        await GoTools?.SaveDownloadSettings({
            enabled: downloadForm.enabled,
            saveDir: downloadForm.saveDir,
            maxRunningTasks: downloadForm.maxRunningTasks,
            maxTotalParts: downloadForm.maxTotalParts,
            defaultConnections: downloadForm.defaultConnections,
            globalSpeedLimitBps: Math.round(downloadForm.speedLimitMB * 1048576),
            proxyPool: proxyPool.value.filter((p) => p.url.trim() !== ""),
            mirrors: mirrorPool.value.filter((m) => m.url.trim() !== ""),
        } as any);
        Message.success("下载设置已保存，目录/并发/限速已即时生效");
        await loadDownload();
    } catch (e: any) {
        Message.error(`保存失败：${String(e?.message ?? e ?? "操作失败")}`);
    } finally {
        downloadSaving.value = false;
    }
}
watch(activeTab, (k) => { if (k === "download") void loadDownload(); });

/* ===================== 局域网互传（设置） =====================
 * P2 收尾：互传服务的启动默认值（端口/监听/自停/PIN）从页内控件升到设置页落盘；
 * 运行期仍以互传页启动入参为准，这里只是预填来源。接收黑名单仍在互传页（即时生效）。 */
const lanShareSaving = ref(false);
const lanShareForm = reactive({ port: 0, host: "", autoStopMin: 120, pin: "" });
async function loadLanShare() {
    try {
        const s = await GoTools?.LanShareSettings();
        if (!s) return;
        lanShareForm.port = Number(s.port ?? 0);
        lanShareForm.host = String(s.host ?? "");
        lanShareForm.autoStopMin = Number(s.autoStopMin ?? 120);
        lanShareForm.pin = String(s.pin ?? "");
    } catch {
        /* 静默：后端未连时不刷屏 */
    }
}
async function saveLanShare() {
    lanShareSaving.value = true;
    try {
        // blockExt 透传当前值（黑名单落点在互传页，这里不碰）：先读回再原样带上
        const cur = await GoTools?.LanShareSettings();
        await GoTools?.SaveLanShareSettings({
            blockExt: (cur?.blockExt as string[]) ?? [],
            port: lanShareForm.port > 0 ? lanShareForm.port : 0,
            host: lanShareForm.host,
            autoStopMin: lanShareForm.autoStopMin,
            pin: lanShareForm.pin,
        } as any);
        Message.success("互传启动默认值已保存，下次启动时生效");
        await loadLanShare();
    } catch (e: any) {
        Message.error(`保存失败：${String(e?.message ?? e ?? "操作失败")}`);
    } finally {
        lanShareSaving.value = false;
    }
}
watch(activeTab, (k) => { if (k === "lanshare") void loadLanShare(); });

/* ===================== 面板访问（仅 Web / 服务模式） =====================
 * 这些配置存在服务端的 panel.json 里，桌面版没有这个文件，也没有登录概念，
 * 所以只在 !caps.nativeWindows 时加载与展示。端口、入口、账号改完即时生效，
 * 不需要重启进程（进程里还托管着 MySQL/Redis 实例，重启代价很大）。
 */
const panelLoading = ref(false);
const panelSaving = ref(false);
const pwdSaving = ref(false);
const panelInfo = ref<PanelInfo | null>(null);
const panelForm = reactive({
    username: "",
    host: "0.0.0.0",
    port: 8080,
    entryPath: "",
    sessionHours: 12,
    enabled: true,
});
const pwdForm = reactive({ current: "", next: "", confirm: "" });

async function loadPanel() {
    if (caps.nativeWindows) return;
    panelLoading.value = true;
    try {
        const info = await getPanelInfo();
        panelInfo.value = info;
        panelForm.username = info.username;
        panelForm.host = info.host;
        panelForm.port = info.port;
        panelForm.entryPath = info.entryPath || "";
        panelForm.sessionHours = info.sessionHours;
        panelForm.enabled = info.enabled;
    } catch (e: any) {
        Message.error(e?.message || "读取面板配置失败");
    } finally {
        panelLoading.value = false;
    }
}

/** 随机安全入口：用 crypto 生成不可预测的后缀，避免被人猜中 */
function genEntry() {
    const bytes = new Uint8Array(6);
    if (window.crypto?.getRandomValues) window.crypto.getRandomValues(bytes);
    else for (let i = 0; i < bytes.length; i++) bytes[i] = Math.floor(Math.random() * 256);
    panelForm.entryPath = "/manage-" + Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

async function savePanel() {
    const username = panelForm.username.trim();
    if (!username) return void Message.warning("登录账号不能为空");
    const port = Number(panelForm.port);
    if (!Number.isInteger(port) || port < 1 || port > 65535) return void Message.warning("端口需在 1~65535 之间");
    const entryPath = panelForm.entryPath.trim();
    // 与后端 panel.ValidateEntry 保持一致，先在前端挡一次省一次往返
    if (entryPath && !/^\/[A-Za-z0-9_-]{4,32}$/.test(entryPath)) {
        return void Message.warning("安全入口需以 / 开头，后接 4~32 位字母、数字、下划线或短横线");
    }
    panelSaving.value = true;
    try {
        const res = await savePanelSettings({
            username,
            host: panelForm.host.trim() || "0.0.0.0",
            port,
            entryPath,
            sessionHours: Number(panelForm.sessionHours) || 12,
            enabled: panelForm.enabled,
        });
        if (res.moved) {
            // 监听地址或端口变了，当前地址已经关停：只能提示换新地址，
            // 此时再去请求 /api/panel/info 会直接连不上。
            panelInfo.value = { ...(panelInfo.value as PanelInfo), ...(res.info as any) };
            Message.success({ content: `${res.message}，请改用新地址打开：${res.accessURL}`, duration: 10000, closable: true });
            return;
        }
        Message.success(res.message || "已保存");
        await loadPanel();
    } catch (e: any) {
        Message.error(e?.message || "保存失败");
    } finally {
        panelSaving.value = false;
    }
}

async function savePassword() {
    if (!pwdForm.current) return void Message.warning("请输入当前密码");
    if (pwdForm.next.length < 8) return void Message.warning("新密码至少 8 位");
    if (pwdForm.next !== pwdForm.confirm) return void Message.warning("两次输入的新密码不一致");
    pwdSaving.value = true;
    try {
        const res = await changePanelPassword(pwdForm.current, pwdForm.next);
        Message.success(res.message || "密码已更新");
        pwdForm.current = pwdForm.next = pwdForm.confirm = "";
    } catch (e: any) {
        Message.error(e?.message || "修改密码失败");
    } finally {
        pwdSaving.value = false;
    }
}

async function copyAccess() {
    const url = panelInfo.value?.accessURL;
    if (!url) return;
    try {
        await navigator.clipboard.writeText(url);
        Message.success("已复制：" + url);
    } catch {
        // 非安全上下文（http + 非 localhost）没有剪贴板权限，退化成展示
        Message.info(url);
    }
}

// 懒加载：只有真正切到该页签才请求，避免每次进设置页都打一次接口。
// 首次进页时面板若还没登录态，这里会 401，由 installPanelGuard 统一跳登录页。
watch(activeTab, (k) => {
    if (k === "panel" && !panelInfo.value && !panelLoading.value) void loadPanel();
    if (k === "launcher") void loadLauncherInfo();
    if (k === "archive") void loadArchiveInfo();
    if (k === "git") void loadGitInfo();
});

/** 主题令牌变更：立即应用到 DOM（localStorage 同步广播到其他窗口），并自动持久化到配置 */
function onThemeTokenChange() {
    applyThemeConfig(form.general.theme);
    scheduleSave(0);
}

/** 和谐模式候选与搭配总览矩阵已按用户要求删除：预设/和谐字段仍由 normalizeConfig 兜默认（default/analogous，纯 no-op） */

/* ===================== 主题背景图 / 玻璃材质 ===================== */
/** 背景图文件输入（仅 Web/无头构建用：无原生对话框，读成 dataURL 再交后端落盘） */
const bgFileInput = ref<HTMLInputElement | null>(null);
/** 当前背景图的文件名（从绝对路径末段取，跨平台分隔符都认） */
const bgImageName = computed(() => {
    const p = form.general.theme.background.image || "";
    return p.split(/[\\/]/).pop() || p;
});
/** 选背景图：桌面弹原生对话框；Web/无头触发隐藏文件输入 */
async function pickBackgroundImage() {
    if (caps.nativeWindows) {
        try {
            const path = await GoTools?.ImportThemeBackground();
            if (!path) return; // 取消
            form.general.theme.background.image = path;
            form.general.theme.background.type = "image";
            onThemeTokenChange();
        } catch (e: any) {
            Message.error(`导入背景图失败：${String(e?.message ?? e ?? "操作失败")}`);
        }
        return;
    }
    bgFileInput.value?.click();
}
/** 文件输入选定：校验体积 → dataURL → 后端落盘 → 写回路径 */
async function onBgFileChosen(ev: Event) {
    const input = ev.target as HTMLInputElement;
    const file = input.files?.[0];
    input.value = "";
    if (!file) return;
    if (file.size > 8 * 1024 * 1024) {
        Message.error("背景图过大（上限 8MB）");
        return;
    }
    try {
        const dataURI = await readFileAsDataURL(file);
        const path = await GoTools?.SaveThemeBackgroundDataURI(dataURI);
        if (!path) {
            Message.error("保存背景图失败");
            return;
        }
        form.general.theme.background.image = path;
        form.general.theme.background.type = "image";
        onThemeTokenChange();
    } catch (e: any) {
        Message.error(`上传背景图失败：${String(e?.message ?? e ?? "操作失败")}`);
    }
}
function readFileAsDataURL(file: File): Promise<string> {
    return new Promise((resolve, reject) => {
        const fr = new FileReader();
        fr.onload = () => resolve(String(fr.result || ""));
        fr.onerror = () => reject(fr.error || new Error("读取文件失败"));
        fr.readAsDataURL(file);
    });
}
/** 清除背景图：回到无背景档 */
function clearBackgroundImage() {
    form.general.theme.background.image = "";
    form.general.theme.background.type = "none";
    onThemeTokenChange();
}
/** 玻璃快速套用：三档观感预设（与实验台口径一致） */
function applyGlassPreset(name: "soft" | "default" | "strong") {
    const g = form.general.theme.glass;
    if (name === "soft") Object.assign(g, { material: "frosted", alpha: 72, blur: 10, border: 25, shadow: 18, noise: 0 });
    else if (name === "strong") Object.assign(g, { material: "frosted", alpha: 45, blur: 28, border: 50, shadow: 22, noise: 0 });
    else Object.assign(g, { material: "acrylic", alpha: 60, blur: 0, border: 0, shadow: 18, noise: 120 });
    onThemeTokenChange();
}
/** 主题背景档变更：常规即时应用+保存；选中原生材质时额外尝试即时下发 DWM 材质并如实提示需重启 */
async function onBackgroundTypeChange(v: string | number | boolean) {
    onThemeTokenChange();
    if (v !== "native") return;
    try {
        await GoTools?.SetWindowNativeBackdrop("acrylic");
    } catch (e: any) {
        // 运行期切换失败不致命（低版本 / 无主窗）：配置已保存，重启后由启动路径按 native 档建 translucent 窗
    }
    Message.info("原生材质已选中：重启应用后主窗透明/模糊完全生效");
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

/* ---------------- 浏览器（全局默认） + 配置导出/导入 ---------------- */

/** 浏览器设置：与浏览器页齿轮抽屉共用同一份 config.browser（后端归一化） */
const browserForm = ref<any>({
    enabled: true,
    maxSpaces: 5,
    homepage: "about:blank",
    searchURL: "https://www.bing.com/search?q=%s",
    proxyMode: "system",
    proxy: "",
    proxyBypass: "",
    userAgent: "",
    defaultSpace: "",
    spaces: [],
});

/** 上限不能小于已有空间数，否则保存必被后端拒 */
const spaceMin = computed(() => Math.max(1, browserForm.value.spaces?.length || 1));

/** 代理模式 → 人话（空间没单独设置时显式写出跟随的档位） */
function proxyModeText(mode: string, proxy: string): string {
    if (mode === "direct") return "直连";
    if (mode === "custom") return proxy ? `代理 ${proxy}` : "自定义（未填）";
    return "跟随系统";
}

/** 空间列表只读概览：明细编辑在浏览器页齿轮抽屉里 */
const spaceRows = computed(() =>
    (browserForm.value.spaces || []).map((s: any) => ({
        id: s.id,
        name: s.name || "未命名",
        homepage: s.homepage || `跟随全局（${browserForm.value.homepage || "about:blank"}）`,
        proxyText: s.proxyMode
            ? proxyModeText(s.proxyMode, s.proxy)
            : `跟随全局：${proxyModeText(browserForm.value.proxyMode, browserForm.value.proxy)}`,
        uaText: s.userAgent || "",
    })),
);

let browserSaveTimer = 0;
function scheduleBrowserSave(delay = 400) {
    clearTimeout(browserSaveTimer);
    browserSaveTimer = window.setTimeout(() => { void saveBrowser(); }, delay);
}

/** 浏览器设置单独落盘：走 BrowserSaveConfig，只换 browser 段，不会碰其他设置 */
async function saveBrowser() {
    saving.value = true;
    try {
        await GoTools?.BrowserSaveConfig({ ...browserForm.value });
    } catch (e: any) {
        Message.error(`浏览器设置保存失败：${String(e?.message || e)}`);
    } finally {
        saving.value = false;
    }
}

async function openBrowserWindow() {
    try {
        await GoTools?.BrowserOpenWindow();
    } catch (e: any) {
        Message.error(`打开浏览器失败：${String(e?.message || e)}`);
    }
}

/** 导出配置：Go 侧弹保存框，机器绑定字段已在导出时剔除 */
async function exportConfig() {
    try {
        const path = await GoTools?.ExportConfig();
        if (path) Message.success(`已导出到 ${path}`);
    } catch (e: any) {
        Message.error(`导出失败：${String(e?.message || e)}`);
    }
}

/** 导入配置：覆盖设置段，目录/Logo/球位保留本机值，导入后重新拉一遍配置 */
async function importConfig() {
    try {
        const path = await GoTools?.ImportConfig();
        if (!path) return;
        await load();
        applyThemeConfig(form.general.theme);
        Message.success("配置已导入");
    } catch (e: any) {
        Message.error(`导入失败：${String(e?.message || e)}`);
    }
}

async function openConfigDir() {
    try {
        await GoTools?.OpenConfigDir();
    } catch (e: any) {
        Message.error(`打开配置目录失败：${String(e?.message || e)}`);
    }
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
        home: t.home !== false,
    };
}

async function load() {
    try {
        const cfg = await GoTools?.GetConfig();
        if (cfg) {
            fullConfig.value = cfg;
            form.log = { enabled: !!cfg.log?.enabled, level: cfg.log?.level || "info", path: "" };
            form.ball = { enabled: !!cfg.ball?.enabled, icon: cfg.ball?.icon || "", hideAllShortcut: cfg.ball?.hideAllShortcut || "", dockEdge: cfg.ball?.dockEdge ?? true };
            form.general = { autoStart: !!cfg.general?.autoStart, appName: cfg.general?.appName || "Tools", appLogo: cfg.general?.appLogo || "", theme: normalizeConfig(cfg.general?.theme) };
            form.paths = { plugins: cfg.paths?.plugins || "", data: cfg.paths?.data || "", logs: cfg.paths?.logs || "", downloads: cfg.paths?.downloads || "" };
            form.launcher = normLauncher(cfg.launcher);
            form.archive = normArchive(cfg.archive);
            form.git = normGit(cfg.git);
            if (cfg.browser) {
                // 整段拷贝：页面上直接绑到 browserForm，保存时整段回传
                browserForm.value = { ...cfg.browser, spaces: cfg.browser.spaces || [] };
            }
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
    Object.assign(editForm, normTool({ id: "", key: "", name: "", path: "", event: "", icon: "", single: false, fullscreen: false, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false, home: true }));
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

/* ---------------- 工具表拖拽排序：「排序」列把手起拖，落点整行（表体由 a-table 渲染，事件在包裹容器上代理） ---------------- */
const dragFromIdx = ref(-1);
let dragOverTr: HTMLElement | null = null;
let dragSrcTr: HTMLElement | null = null;

function toolDragStart(i: number, e: DragEvent) {
    dragFromIdx.value = i;
    if (e.dataTransfer) {
        e.dataTransfer.effectAllowed = "move";
        try { e.dataTransfer.setData("text/plain", String(i)); } catch { /* 某些环境 setData 受限，忽略 */ }
    }
    // 被拖行半透明，和主页/实例列表的 reorder-dragging 反馈一致
    dragSrcTr = trOf(e.target);
    dragSrcTr?.classList.add("dragging-row");
}
function toolDragEnd() {
    dragFromIdx.value = -1;
    markOver(null);
    dragSrcTr?.classList.remove("dragging-row");
    dragSrcTr = null;
}
/** 事件落在哪一行：a-table 的 tr 在其 tbody 里的序号即数据下标（fixed 列的副本表体同序渲染，可直接用） */
function trOf(t: EventTarget | null): HTMLElement | null {
    return (t as HTMLElement | null)?.closest?.(".m-table-tr") ?? null;
}
function rowIdxOfTr(tr: HTMLElement | null): number {
    // 只认表体行：表头 tr 也叫 .m-table-tr，落在 thead 里不参与排序
    if (!tr || tr.parentElement?.tagName !== "TBODY") return -1;
    return Array.prototype.indexOf.call(tr.parentElement.children, tr);
}
function markOver(tr: HTMLElement | null) {
    if (dragOverTr === tr) return;
    dragOverTr?.classList.remove("drag-over-row");
    dragOverTr = tr;
    dragOverTr?.classList.add("drag-over-row");
}
function onToolsWrapDragOver(e: DragEvent) {
    if (dragFromIdx.value < 0) return;
    e.preventDefault(); // 不声明可落点，drop 不会触发
    if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
    const tr = trOf(e.target);
    const i = rowIdxOfTr(tr);
    markOver(tr && i >= 0 && i !== dragFromIdx.value ? tr : null);
}
function onToolsWrapDragLeave(e: DragEvent) {
    // 拖出表格区域才清高亮；在行间移动时 relatedTarget 仍在容器内
    if (dragOverTr && e.relatedTarget instanceof Node && toolsTableRef.value?.contains(e.relatedTarget)) return;
    markOver(null);
}
function onToolsWrapDrop(e: DragEvent) {
    const from = dragFromIdx.value;
    if (from < 0) return;
    e.preventDefault();
    const to = rowIdxOfTr(trOf(e.target));
    markOver(null);
    dragFromIdx.value = -1;
    dragSrcTr?.classList.remove("dragging-row");
    dragSrcTr = null;
    if (to < 0 || from === to) return;
    const arr = tools.value;
    const [moved] = arr.splice(from, 1);
    arr.splice(to, 0, moved);
    scheduleSave(0);
}

function resetTools() {
    tools.value = DEFAULT_TOOLS.map(normTool);
    scheduleSave(0);
}

/**
 * 跟随外部对工具列表的改动：主页「快捷操作」拖动会把新顺序直接写回配置里的 tools，
 * 本页不刷新就会和主页对不上。只同步工具表，不动其它表单（避免覆盖正在编辑的字段）；
 * 自己保存后的回声两边内容一致，比对后直接跳过。
 */
async function syncToolsFromConfig() {
    try {
        const cfg = await GoTools?.GetConfig();
        const next = (cfg?.tools || []).map(normTool);
        if (JSON.stringify(next) === JSON.stringify(tools.value.map(normTool))) return;
        tools.value = next;
    } catch { /* 读取失败保持现状 */ }
}

/* ---------------- 日志操作 ---------------- */
/** 选择全局基础目录（插件/数据/日志/下载）：保存后各服务配置、另存与下载默认目录随即落新路径 */
async function pickBaseDir(kind: "plugins" | "data" | "logs" | "downloads", title: string) {
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
        const payload = { log: { ...form.log }, ball: { ...form.ball }, general: { ...form.general }, paths: { ...form.paths }, launcher: { ...form.launcher, scopes: { ...form.launcher.scopes }, dirs: [...form.launcher.dirs] }, archive: { ...form.archive }, git: { ...form.git }, tools: tools.value.map(normTool) };
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

/** 默认工具列表：“恢复默认列表”还原到这份基线，与后端 config.DefaultConfig 的 Tools 逐项一致（含顺序）。
 * 这是当前保存配置（config.json）的快照（自定义图标/尺寸/开关/主页展示已烘入），不再从 BUILTINS 派生，避免默认基线漂移。
 * 不含 settings（设置仍可从浮动球/标题栏入口进入）。 */
const DEFAULT_TOOLS: ToolRow[] = [
    { id: "docker", key: "docker", name: "Docker管理", path: "/tools/docker", event: "", icon: "icon-docker", single: true, fullscreen: false, disableFullscreen: false, width: 1360, height: 860, shortcut: "", quick: false, home: true },
    { id: "browser", key: "browser", name: "浏览器", path: "/tools/browser", event: "", icon: "icon-liulanqi1", single: true, fullscreen: true, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false, home: true },
    { id: "speedtest", key: "speedtest", name: "网速测速", path: "/tools/speedtest", event: "", icon: "icon-cesu", single: true, fullscreen: false, disableFullscreen: false, width: 1080, height: 720, shortcut: "", quick: false, home: true },
    { id: "term", key: "term", name: "终端连接", path: "/tools/term", event: "", icon: "icon-terminal", single: true, fullscreen: false, disableFullscreen: false, width: 1200, height: 800, shortcut: "", quick: false, home: true },
    { id: "disk", key: "disk", name: "磁盘管理", path: "/tools/disk", event: "", icon: "icon-yingpan", single: true, fullscreen: false, disableFullscreen: false, width: 1360, height: 860, shortcut: "", quick: false, home: true },
    { id: "siteclone", key: "siteclone", name: "仿站工具", path: "/tools/siteclone", event: "", icon: "download", single: true, fullscreen: false, disableFullscreen: false, width: 1120, height: 780, shortcut: "", quick: false, home: false },
    { id: "guard", key: "guard", name: "进程守护", path: "/tools/guard", event: "", icon: "apps", single: true, fullscreen: false, disableFullscreen: false, width: 1120, height: 720, shortcut: "", quick: false, home: false },
    { id: "archive", key: "archive", name: "文件压缩", path: "/tools/archive", event: "", icon: "archive", single: true, fullscreen: false, disableFullscreen: false, width: 1120, height: 760, shortcut: "", quick: false, home: false },
    { id: "download", key: "download", name: "下载管理", path: "/tools/download", event: "", icon: "icon-xiazai", single: true, fullscreen: false, disableFullscreen: false, width: 1280, height: 820, shortcut: "", quick: false, home: false },
    { id: "docspace", key: "docspace", name: "文档中心", path: "/tools/docspace", event: "", icon: "folder", single: true, fullscreen: false, disableFullscreen: false, width: 1280, height: 820, shortcut: "", quick: false, home: false },
    { id: "lanshare", key: "lanshare", name: "局域网互传", path: "/tools/lanshare", event: "", icon: "icon-wangluo1", single: true, fullscreen: false, disableFullscreen: false, width: 1280, height: 820, shortcut: "", quick: false, home: false },
    { id: "clouddrive", key: "clouddrive", name: "云盘管理", path: "/tools/clouddrive", event: "", icon: "icon-yun", single: true, fullscreen: false, disableFullscreen: false, width: 1320, height: 860, shortcut: "", quick: false, home: false },
    { id: "git", key: "git", name: "Git管理", path: "/tools/git", event: "", icon: "code", single: true, fullscreen: false, disableFullscreen: false, width: 1360, height: 860, shortcut: "", quick: false, home: false },
    { id: "ftp", key: "ftp", name: "文件传输", path: "/tools/ftp", event: "", icon: "icon-ftp", single: true, fullscreen: false, disableFullscreen: false, width: 1360, height: 860, shortcut: "", quick: false, home: true },
    { id: "redis", key: "redis", name: "Redis管理", path: "/tools/redis", event: "", icon: "icon-icon-redis", single: true, fullscreen: false, disableFullscreen: false, width: 1360, height: 860, shortcut: "", quick: false, home: true },
    { id: "mysql", key: "mysql", name: "MySQL管理", path: "/tools/mysql", event: "", icon: "icon-MySQL", single: true, fullscreen: false, disableFullscreen: false, width: 1360, height: 860, shortcut: "", quick: false, home: true },
    { id: "php", key: "php", name: "PHP管理", path: "/tools/php", event: "", icon: "icon-php", single: true, fullscreen: false, disableFullscreen: false, width: 1360, height: 860, shortcut: "", quick: false, home: true },
    { id: "nginx", key: "nginx", name: "Nginx管理", path: "/tools/nginx", event: "", icon: "icon-ngnix", single: true, fullscreen: false, disableFullscreen: false, width: 1360, height: 860, shortcut: "", quick: false, home: true },
    { id: "npm", key: "npm", name: "Npm管理", path: "/tools/npm", event: "", icon: "icon-npm", single: true, fullscreen: false, disableFullscreen: false, width: 1360, height: 860, shortcut: "", quick: false, home: true },
    { id: "sniffer", key: "sniffer", name: "抓包工具", path: "/tools/sniffer", event: "", icon: "bug", single: true, fullscreen: true, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false, home: true },
    { id: "websocket", key: "websocket", name: "WebSocket调试", path: "/tools/websocket", event: "", icon: "icon-wangluo1", single: true, fullscreen: true, disableFullscreen: false, width: 1360, height: 860, shortcut: "", quick: false, home: true },
    { id: "timestamp", key: "timestamp", name: "时间戳转换", path: "/tools/timestamp", event: "", icon: "icon-riqishijianzhuanhuanmiaoshu", single: false, fullscreen: false, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false, home: true },
    { id: "usbserial", key: "usbserial", name: "USB调试", path: "/tools/usbserial", event: "", icon: "icon-usb1", single: true, fullscreen: false, disableFullscreen: false, width: 1360, height: 860, shortcut: "", quick: false, home: true },
    { id: "video", key: "video", name: "视频录制", path: "/tools/video", event: "", icon: "video-camera", single: true, fullscreen: true, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false, home: false },
    { id: "screenshot", key: "screenshot", name: "截图", path: "", event: "screenshot", icon: "icon-jietu", single: false, fullscreen: false, disableFullscreen: false, width: 0, height: 0, shortcut: "Ctrl+Alt+A", quick: true, home: true },
    { id: "colorpicker", key: "colorpicker", name: "颜色取色器", path: "/tools/colorpicker", event: "", icon: "icon-quseqi", single: true, fullscreen: false, disableFullscreen: true, width: 480, height: 276, shortcut: "", quick: true, home: true },
    { id: "encdec", key: "encdec", name: "文字加密解密", path: "/tools/encdec", event: "", icon: "lock", single: false, fullscreen: false, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false, home: false },
    { id: "strrandom", key: "strrandom", name: "随机字符串生成", path: "/tools/strrandom", event: "", icon: "icon-zifuchuan", single: false, fullscreen: false, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false, home: false },
    { id: "qrcode", key: "qrcode", name: "二维码生成", path: "/tools/qrcode", event: "", icon: "icon-erweima3", single: false, fullscreen: false, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false, home: false },
    { id: "hexconvert", key: "hexconvert", name: "进制转换", path: "/tools/hexconvert", event: "", icon: "swap", single: false, fullscreen: false, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false, home: false },
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
    const header = wrap.querySelector(".m-table-thead") as HTMLElement | null;
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

/* ===================== 快捷搜索（启动器） =====================
 * 这一页只是这份配置的编辑面。“开/关到底挂不挂服务”的判定在 Go 侧
 * （App.launcherMounted = 开关 && 原生窗口能力 && 应用已起来）：
 * 开着才注册热键、建面板窗、扫盘建索引；关掉时 SaveConfig 会顺带收窗、
 * 释放索引、并把热键从清单里抽出去。前端只负责把开关落盘 + 把后端回
 * 馈（索引条数 / 默认目录 / 命令清单）显示出来。
 */

/** 表单里的一份快捷搜索配置（与 config.LauncherConfig 同构） */
interface LauncherCfg {
    enabled: boolean;
    shortcut: string;
    scopes: { apps: boolean; tools: boolean; windows: boolean; files: boolean; commands: boolean };
    dirs: string[];
    maxEntries: number;
}

/** 索引状态（与 launcher.IndexInfo 同构） */
interface LkIndexInfo {
    count: number;
    building: boolean;
    partial: boolean;
    lastErr: string;
    roots: string[] | null;
    age: string;
}

/** 命令清单条目（与 launcher.Command 同构） */
interface LkCommand {
    id: string;
    title: string;
    sub: string;
    keywords: string[] | null;
    danger: boolean;
}

/** 把配置里的 launcher 段规整成表单结构：老配置没这一段（nil）时全走默认值 */
function normLauncher(c: any): LauncherCfg {
    return {
        enabled: !!c?.enabled,
        shortcut: String(c?.shortcut || "Alt+Space"),
        // 范围默认全开：指针缺失或字段省略都算“没说不”，与后端 normalize 保持一致
        scopes: {
            apps: c?.scopes?.apps !== false,
            tools: c?.scopes?.tools !== false,
            windows: c?.scopes?.windows !== false,
            files: c?.scopes?.files !== false,
            commands: c?.scopes?.commands !== false,
        },
        dirs: (c?.dirs || []).map((d: any) => String(d ?? "").trim()).filter((d: string) => d),
        maxEntries: c?.maxEntries > 0 ? Number(c.maxEntries) : 60000,
    };
}

const lkDefaultDirs = ref<string[]>([]);
const lkCmds = ref<LkCommand[]>([]);
const lkIdx = ref<LkIndexInfo | null>(null);
const lkRebuilding = ref(false);

/** 拉一次只有后端知道的信息：默认索引目录、可搜命令、索引状态 */
async function loadLauncherInfo() {
    if (!caps.nativeWindows) return;
    try {
        lkDefaultDirs.value = (await GoTools?.LauncherDefaultDirs()) || [];
        lkCmds.value = (await GoTools?.LauncherCommands()) || [];
        lkIdx.value = (await GoTools?.LauncherIndexStatus()) || null;
    } catch { /* 非 Wails 环境或绑定缺失：面板仍可读，只是状态空白 */ }
}

/**
 * 总开关：先落盘，SaveConfig 里面就会按新状态完成挂载/卸载，
 * 再把索引状态读回来显示——避免用户拨完开关看不出发生过什么。
 */
async function onLauncherEnabled() {
    await saveNow();
    await loadLauncherInfo();
    if (form.launcher.enabled) {
        Message.success(`已开启：按 ${form.launcher.shortcut || "（未设置热键）"} 呼出面板，索引正在后台建立`);
    } else {
        Message.info("已关闭：热键已注销，面板收起，索引已释放");
    }
}

/* ---- 呼出热键录制：与工具热键同一套规范化规则，只是写进 launcher 段 ---- */
const lkRecording = ref(false);

function lkStartRecord() {
    lkRecording.value = true;
}

function lkOnShortcutKeydown(e: KeyboardEvent) {
    e.preventDefault();
    if (e.key === "Escape") {
        lkRecording.value = false;
        return;
    }
    if (e.key === "Backspace" || e.key === "Delete") {
        form.launcher.shortcut = "";
        lkRecording.value = false;
        scheduleSave(0);
        return;
    }
    // 仅按下修饰键时忽略，等主键
    if (["Control", "Alt", "Shift", "Meta"].includes(e.key)) return;
    const parts: string[] = [];
    if (e.ctrlKey) parts.push("Ctrl");
    if (e.altKey) parts.push("Alt");
    if (e.shiftKey) parts.push("Shift");
    if (e.metaKey) parts.push("Win");
    parts.push(normalizeKey(e.key));
    form.launcher.shortcut = parts.join("+");
    lkRecording.value = false;
    // 热键变了要尽快重新注册，不跟普通输入一样走 300ms 防抖
    scheduleSave(0);
}

/* ---- 「显示/隐藏全部窗口」热键录制：与工具热键同一套规范化规则，只是写进 ball 段 ---- */
const ballHaRecording = ref(false);

function ballHaStartRecord() {
    ballHaRecording.value = true;
}

function ballHaOnKeydown(e: KeyboardEvent) {
    e.preventDefault();
    if (e.key === "Escape") {
        ballHaRecording.value = false;
        return;
    }
    if (e.key === "Backspace" || e.key === "Delete") {
        ballHaClear();
        return;
    }
    // 仅按下修饰键时忽略，等主键
    if (["Control", "Alt", "Shift", "Meta"].includes(e.key)) return;
    const parts: string[] = [];
    if (e.ctrlKey) parts.push("Ctrl");
    if (e.altKey) parts.push("Alt");
    if (e.shiftKey) parts.push("Shift");
    if (e.metaKey) parts.push("Win");
    parts.push(normalizeKey(e.key));
    form.ball.hideAllShortcut = parts.join("+");
    ballHaRecording.value = false;
    // 热键变了要尽快重新注册，不跟普通输入一样走 300ms 防抖
    scheduleSave(0);
}

function ballHaClear() {
    form.ball.hideAllShortcut = "";
    ballHaRecording.value = false;
    scheduleSave(0);
}

async function addLauncherDir() {
    try {
        const dir = String((await GoTools?.ChooseDir("选择要加入索引的目录")) || "");
        if (!dir) return;
        if (form.launcher.dirs.includes(dir)) {
            Message.info("该目录已在索引列表里");
            return;
        }
        if (!(await GoTools?.LauncherDirExists(dir))) {
            Message.error(`目录不存在或不可读：${dir}`);
            return;
        }
        form.launcher.dirs.push(dir);
        await saveNow();
        // 目录变了不重建就搜不到新内容，直接把重建拉起来
        await rebuildLauncherIndex();
    } catch { /* 用户取消 */ }
}

function removeLauncherDir(i: number) {
    form.launcher.dirs.splice(i, 1);
    scheduleSave(0);
}

/** 重建索引：Go 侧扫盘是异步的，按下后隔几档轮询一次，建完就止 */
async function rebuildLauncherIndex() {
    if (!form.launcher.enabled) {
        Message.warning("请先启用快捷搜索");
        return;
    }
    lkRebuilding.value = true;
    try {
        await GoTools?.LauncherRebuildIndex();
        for (const delay of [600, 2500, 6000]) {
            await new Promise((r) => setTimeout(r, delay));
            lkIdx.value = (await GoTools?.LauncherIndexStatus()) || null;
            if (lkIdx.value && !lkIdx.value.building) break;
        }
        Message.success(`索引完成，当前 ${lkIdx.value?.count || 0} 条`);
    } catch (e: any) {
        Message.error(`重建索引失败：${String(e?.message || e)}`);
    } finally {
        lkRebuilding.value = false;
    }
}

async function resetLauncherUsage() {
    try {
        await GoTools?.LauncherUsageReset();
        Message.success("已清空使用频次，排序回归字面匹配");
    } catch (e: any) {
        Message.error(`清空失败：${String(e?.message || e)}`);
    }
}

/** 从设置页直接呼出面板验一下，省得记热键；面板已显示时再次呼出即收起 */
async function testLauncher() {
    try {
        await GoTools?.LauncherToggle();
    } catch (e: any) {
        Message.error(`呼出失败：${String(e?.message || e)}`);
    }
}

/* ===================== 文件压缩（系统右键挂载） =====================
 * 同样只算这份配置的编辑面：“开关到底挂不挂”在 Go 侧（archive.Enabled 决定
 * 写不写注册表、起不起转交监听），而且只有桌面版有注册表可写。
 * 前端只负责落盘 + 把 ArchiveShellStatus 读回来的三态槽位显示出来。
 */

/** 表单里的一份压缩配置（与 config.ArchiveConfig 同构） */
interface ArchiveCfg {
    enabled: boolean;
    sendTo: boolean;
    format: string;
    level: number;
}

/** 挂载状态（与 archive.ShellInfo 同构，只用到展示得了的几个字段） */
interface ShellInfo {
    supported: boolean;
    exe: string;
    compress: number;
    extract: number;
    sendTo: number;
    stale: boolean;
    detail: string;
}

/** 把配置里的 archive 段规整成表单结构：老配置没这一段（nil）时全走默认值 */
function normArchive(c: any): ArchiveCfg {
    return {
        enabled: !!c?.enabled,
        sendTo: !!c?.sendTo,
        format: c?.format === "targz" ? "targz" : "zip",
        level: c?.level >= 1 && c?.level <= 9 ? Number(c.level) : 6,
    };
}

const arShell = ref<ShellInfo | null>(null);
const arBusy = ref(false);

/** 槽位态文案（与 Go 侧 archive.SlotText 同一口径，两头不能各编一套） */
function slotText(n: number | undefined): string {
    switch (n) {
        case 1: return "已挂载";
        case 2: return "指向旧路径";
        case 3: return "读取失败";
        default: return "未挂载";
    }
}

async function loadArchiveInfo() {
    if (!caps.nativeWindows) return;
    try {
        arShell.value = (await GoTools?.ArchiveShellStatus()) || null;
    } catch { /* 非 Wails 环境或绑定缺失：面板仍可读，只是状态空白 */ }
}

/**
 * 总开关：先落盘，SaveConfig 里就会按新状态完成挂载/卸载，
 * 再把挂载状态读回来显示——避免用户拨完开关看不出发生过什么。
 */
async function onArchiveEnabled() {
    await saveNow();
    await loadArchiveInfo();
    if (form.archive.enabled) {
        Message.success("已开启：右键与「发送到」已挂载，进度可在右下角任务气泡里回看");
    } else {
        Message.info("已关闭：注册表里的菜单项与快捷方式已删干净，转交监听已停")
    }
}

async function installArchiveShell() {
    arBusy.value = true;
    try {
        await saveNow(); // 装之前先把当前格式/SendTo 选项落盘，Go 侧读的是配置
        await GoTools?.ArchiveShellInstall();
        Message.success("已挂载到系统右键");
    } catch (e: any) {
        Message.error(`挂载失败：${String(e?.message || e)}`);
    } finally {
        arBusy.value = false;
        await loadArchiveInfo();
    }
}

async function uninstallArchiveShell() {
    arBusy.value = true;
    try {
        await GoTools?.ArchiveShellUninstall();
        Message.success("已卸载：只删了本程序自己写过的那几项");
    } catch (e: any) {
        Message.error(`卸载失败：${String(e?.message || e)}`);
    } finally {
        arBusy.value = false;
        await loadArchiveInfo();
    }
}

/* ===================== Git（系统右键挂载） =====================
 * 与文件压缩同构：“开关到底挂不挂”在 Go 侧（config.GitShellEnabled 决定
 * 写不写注册表、起不起转交监听），而且只有桌面版有注册表可写。
 * 前端只负责落盘 + 把 GitShellStatus 读回来的聚合槽位状态显示出来。
 */

/** 表单里的一份 Git 配置（与 config.GitConfig 同构） */
interface GitCfg {
    shellEnabled: boolean;
}

/** 挂载状态（与 gitcmd.GitShellInfo 同构，只用到展示得了的几个字段） */
interface GitShellInfo {
    supported: boolean;
    exe: string;
    slot: number;
    stale: boolean;
    detail: string;
}

/** 把配置里的 git 段规整成表单结构：老配置没这一段（nil）时全走默认值 */
function normGit(c: any): GitCfg {
    return { shellEnabled: !!c?.shellEnabled };
}

const gitShell = ref<GitShellInfo | null>(null);
const gitBusy = ref(false);

async function loadGitInfo() {
    if (!caps.nativeWindows) return;
    try {
        gitShell.value = (await GoTools?.GitShellStatus()) || null;
    } catch { /* 非 Wails 环境或绑定缺失：面板仍可读，只是状态空白 */ }
}

/**
 * 总开关：先落盘（SaveConfig 里会按新状态走 applyGitSwitch 完成挂载/卸载），
 * 再把挂载状态读回来显示。
 */
async function onGitEnabled() {
    await saveNow();
    await loadGitInfo();
    if (form.git.shellEnabled) {
        Message.success("已开启：Git 右键菜单已挂载，点菜单项会在当前实例里打开工作台并定位到该仓库");
    } else {
        Message.info("已关闭：注册表里的 ToolsGit* 菜单项已删干净，转交监听已停");
    }
}

async function installGitShell() {
    gitBusy.value = true;
    try {
        await saveNow(); // 装之前先把开关状态落盘
        await GoTools?.GitShellInstall();
        Message.success("已挂载到系统右键");
    } catch (e: any) {
        Message.error(`挂载失败：${String(e?.message || e)}`);
    } finally {
        gitBusy.value = false;
        await loadGitInfo();
    }
}

async function uninstallGitShell() {
    gitBusy.value = true;
    try {
        await GoTools?.GitShellUninstall();
        Message.success("已卸载：只删了本程序自己写的 ToolsGit* 那几项，不碰压缩与别家的键");
    } catch (e: any) {
        Message.error(`卸载失败：${String(e?.message || e)}`);
    } finally {
        gitBusy.value = false;
        await loadGitInfo();
    }
}

let offConfigSaved: (() => void) | null = null;
let offLauncherIndex: (() => void) | null = null;
let offSettingsTab: (() => void) | null = null;

onMounted(() => {
    load();
    // 托盘“切换主题”新开窗时带的 ?tab=：同为“挂载即落位”那一条路（与工具页 ?inst= 同构）
    gotoTab(route.query.tab);
    // 主页拖动快捷操作后同步工具表顺序（config:saved 由后端在配置落盘后广播）
    try {
        const ret = Events.On("config:saved", () => { void syncToolsFromConfig(); });
        if (typeof ret === "function") offConfigSaved = ret as () => void;
        // 索引后台建完会广播这里：“重建索引”按下去时扫描可能还远没收尾，
        // 靠这个事件把状态行上的条数补上，不必一直轮询。
        const ret2 = Events.On("launcher:index", () => {
            if (activeTab.value === "launcher") void loadLauncherInfo();
        });
        if (typeof ret2 === "function") offLauncherIndex = ret2 as () => void;
        // 设置页已开着时托盘再点“切换主题”：query 不会重新生效，靠这条事件切分类
        const ret3 = Events.On("settings:tab", (ev: any) => { gotoTab(ev?.data ?? ev); });
        if (typeof ret3 === "function") offSettingsTab = ret3 as () => void;
    } catch { /* 非 Wails 环境没有事件总线 */ }
    window.addEventListener("resize", onWinResize);
    if (typeof ResizeObserver !== "undefined") {
        ro = new ResizeObserver(() => calcTableScroll());
        if (toolsTableRef.value) ro.observe(toolsTableRef.value);
    }
});

onBeforeUnmount(() => {
    if (offConfigSaved) offConfigSaved();
    offConfigSaved = null;
    if (offLauncherIndex) offLauncherIndex();
    offLauncherIndex = null;
    if (offSettingsTab) offSettingsTab();
    offSettingsTab = null;
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
    /* 上下留一圈随密度的呼吸位：side-card body 是 padding:0，不给这里补的话
       首项紧贴搜索框、末项贴死卡片下缘（用户反馈「上下边距不对，要随主题」）。
       吃 --base-padding（紧凑8/标准12/宽松16），密度切换时上下边距同步长缩。 */
    padding: calc(var(--base-padding) / 2) 0;
    /* 菜单项纵向间距改由 flex gap 统一给：原 .cat-item 上下 margin 在普通流里会与相邻项
       折叠（标准档实际只剩 ~2px），既偏挤又不随密度明显变化。flex gap 不折叠，取
       --base-padding / 3（紧凑≈2.7/标准4/宽松≈5.3px），与 .side-search 的节奏一致，
       上下间距真正随「显示密度」联动（用户反馈「上下边距不对，要随主题里的边距配置」）。 */
    display: flex;
    flex-direction: column;
    gap: calc(var(--base-padding) / 3);
}

/* 搜索框：藏在左栏顶部，与分类导航同缩进节奏（间距随「显示密度」联动，标准档=原 8/8/4px）*/
.side-search {
    padding: calc(var(--base-padding) * 2 / 3) calc(var(--base-padding) * 2 / 3) calc(var(--base-padding) / 3);
    flex: none;
}

/* 搜索结果行：复用 cat-item 胶囊底座，改两行结构（标题+分类 / 摘要） */
.s-item {
    flex-direction: column;
    align-items: flex-start;
    gap: calc(var(--base-padding) / 6);
}

.s-main {
    display: flex;
    align-items: center;
    gap: calc(var(--base-padding) * 2 / 3);
    width: 100%;
}

.s-label {
    color: var(--color-text-1);
}

.s-tab {
    flex: none;
    font-size: var(--base-size-small);
    color: rgb(var(--gray-5));
}

.s-hint {
    width: 100%;
    font-size: var(--base-size-small);
    color: rgb(var(--gray-5));
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}

.search-list mark {
    color: rgb(var(--primary-6));
    background: transparent;
    font-weight: 600;
}

.s-empty {
    padding: calc(var(--base-padding) * 4 / 3) var(--base-padding);
    text-align: center;
    font-size: var(--base-size-small);
    color: rgb(var(--gray-5));
}

/* 跳转命中后的短暂主色底渐隐，帮眼睛在长表单里找到目标项 */
[data-skey].s-flash {
    animation: s-flash 1.2s ease-out 1;
}

@keyframes s-flash {
    0% {
        background: rgba(var(--primary-6), 0.12);
        border-radius: var(--base-radius-default);
    }

    100% {
        background: transparent;
        border-radius: var(--base-radius-default);
    }
}

/* 分类导航：胶囊项无分隔线；选中态按「短竖条+轻底」规范，不加深边框；字号对齐正文表单文字（default 档） */
.cat-item {
    position: relative;
    display: flex;
    align-items: center;
    gap: calc(var(--base-padding) * 5 / 6);
    /* 纵向不再靠 margin（会与相邻项折叠）：上下间距已交给 .cat-list 的 flex gap；
       这里只留左右 margin 把胶囊从列表两侧缩进，随密度联动。 */
    margin: 0 calc(var(--base-padding) * 2 / 3);
    padding: calc(var(--base-padding) * 3 / 4) var(--base-padding);
    font-size: var(--base-size-default);
    color: var(--color-text-2);
    cursor: pointer;
    border-radius: var(--base-radius-default);
    transition: background-color 0.15s, color 0.15s;
}

.cat-item:hover {
    background: var(--color-fill-2);
    color: var(--color-text-1);
}

/* 选中态：主题主色为黑白时大面积铺底对比不足，改用中性轻底 + 正文色 + 左侧短竖条 */
.cat-item.active {
    background: var(--color-fill-2);
    color: var(--color-text-1);
    font-weight: 600;
}

.cat-item::before {
    content: "";
    position: absolute;
    left: 0;
    top: 50%;
    transform: translateY(-50%);
    width: 3px;
    height: 55%;
    border-radius: 0 3px 3px 0;
    background: transparent;
    transition: background-color 0.15s;
}

.cat-item.active::before {
    background: rgb(var(--primary-6));
}

.cat-icon {
    font-size: var(--base-size-default);
    color: inherit;
}

.cat-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.cat-arrow {
    font-size: var(--base-size-small);
    color: rgb(var(--gray-5));
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
}

.panel-title {
    font-size: var(--base-size-default);
    font-weight: 600;
}

/* 内容区：同时只显示当前分类的子卡片。
   上下不铺 padding（避免与卡片 body 的上内边距叠成双层），但左右保留一圈 --base-padding：
   子卡片 pane-card 的 body 左右内边距会被「无边距模式」(body.card-flush) 归零，若这里不管左右，
   开了无边距或暗黑纹理下内容就贴死面板左右缘（用户反馈「这里还是不对，左右要有边距」）。
   这圈左右 padding 属普通 div、不在 .arco-card 内，card-flush 天然管不到它，故单独补一条归零规则：
   普通档给一个密度边距（随显示密度缩放），无边距档归零铺满——即「左右有边距且随主题」。 */
.tab-content {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    padding: 0 var(--base-padding);
}

body.card-flush .tab-content {
    padding-left: 0;
    padding-right: 0;
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
    gap: calc(var(--base-padding) * 2 / 3);
    width: 100%;
}

.path-row :deep(.arco-input) {
    flex: 1;
}

/* 配置同步：没有表单控件绑定，用普通块排版，标签/提示跟表单风格对齐 */
.cfg-sync {
    display: flex;
    flex-direction: column;
    gap: calc(var(--base-padding) * 2 / 3);
    max-width: 720px;
}

.cfg-sync-label {
    font-size: var(--base-size-small);
    color: var(--color-text-2);
}

/* 浏览器代理地址 + 绕过列表并排（地址要长，绕过列表短） */
.bs-pair {
    display: grid;
    grid-template-columns: 1.6fr 1fr;
    gap: calc(var(--base-padding) * 2 / 3);
    width: 100%;
}

/* 空间列表小标题：跟表单标签同字号，右侧带一个解释图标 */
.space-head {
    display: flex;
    align-items: center;
    gap: calc(var(--base-padding) / 2);
    margin-bottom: calc(var(--base-padding) / 2);
}

.space-title {
    font-size: var(--base-size-small);
    color: var(--color-text-2);
}

.bs-help {
    color: rgb(var(--gray-5));
    cursor: help;
}

/* 表单项内容默认横排（nowrap）：提示文案独占一行，避免与控件挤在同一行 */
.pane-card :deep(.arco-form-item-content-flex),
.edit-form :deep(.arco-form-item-content-flex) {
    flex-wrap: wrap;
    row-gap: 2px;
}

.pane-card :deep(.arco-form-item-content-flex)>.hint,
.edit-form :deep(.arco-form-item-content-flex)>.hint {
    flex: 0 0 100%;
    width: 100%;
}

.hint {
    display: block;
    margin-top: calc(var(--base-padding) / 3);
    color: rgb(var(--gray-5));
    font-size: var(--base-size-small);
    line-height: 1.5;
}

/* 面板当前访问地址：单独一行弱色描边块，避免长 URL 换行时和按钮缠在一起 */
.panel-actions {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: calc(var(--base-padding) * 5 / 6);
    width: 100%;
}

.panel-access {
    padding: calc(var(--base-padding) / 2) calc(var(--base-padding) * 5 / 6);
    border: 1px solid var(--color-border-1);
    border-radius: var(--base-radius-default);
    background: var(--color-bg-1);
    font-size: var(--base-size-small);
}

.panel-access code {
    color: rgb(var(--primary-6));
    word-break: break-all;
    user-select: all;
}

.hint-inline {
    display: inline;
    margin: 0;
    flex: none;
    width: auto;
}

/* 面板右上角自动保存状态：小圆点作反馈，保存中变主色呼吸 */
.auto-save-tip {
    display: inline-flex;
    align-items: center;
    gap: calc(var(--base-padding) / 2);
    font-size: var(--base-size-small);
    color: rgb(var(--gray-5));
}

.save-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: rgb(var(--gray-5));
    flex: none;
}

.save-dot.busy {
    background: rgb(var(--primary-6));
    animation: save-dot-pulse 0.9s ease-in-out infinite;
}

@keyframes save-dot-pulse {

    0%,
    100% {
        opacity: 1;
    }

    50% {
        opacity: 0.3;
    }
}

/* 工具页 pane-body：作为卡片 body（tools 档下 display:flex column / overflow:hidden）的唯一可见子项，
   填满剩余高度，把滚动交给内部 a-table 的 :scroll。 */
.tools-pane {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

.tools-table-wrap {
    flex: 1;
    min-height: 0;
    overflow: hidden;
}

/* 拖拽把手：常显淡灰，hover 提亮；按住起拖 */
.row-drag-handle {
    display: inline-flex;
    align-items: center;
    color: rgb(var(--gray-5));
    cursor: grab;
}

.row-drag-handle:hover {
    color: rgb(var(--primary-6));
}

.row-drag-handle:active {
    cursor: grabbing;
}

/* 拖拽反馈：落点行虚线描边（同全局 .reorder-over），被拖行半透明；tr 由 a-table 渲染，需 :deep 穿透 */
.tools-table-wrap :deep(.arco-table-tr.drag-over-row) {
    outline: 2px dashed rgb(var(--primary-6));
    outline-offset: -2px;
}

.tools-table-wrap :deep(.arco-table-tr.dragging-row) {
    opacity: 0.4;
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
    gap: calc(var(--base-padding) / 2);
}

.icon-cell .arco-preview,
.icon-cell .iconfont-preview {
    font-size: var(--base-size-default);
    color: var(--color-text-1);
}

.icon-cell .icon-text {
    color: rgb(var(--gray-5));
    font-size: var(--base-size-small);
}

/* 内置功能下拉：Arco 与图标库图标混排 */
.builtin-option {
    display: inline-flex;
    align-items: center;
    gap: calc(var(--base-padding) * 2 / 3);
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
    gap: calc(var(--base-padding) * 4 / 3);
}

/* 主题皮肤色板：一排圆点选预设主色，末尾一枚取色器作为自定义入口 */
.theme-palette {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: calc(var(--base-padding) * 5 / 6);
}

.swatch {
    width: 26px;
    height: 26px;
    border-radius: var(--base-radius-default);
    cursor: pointer;
    border: 2px solid transparent;
    box-shadow: 0 0 0 1px var(--color-border-2) inset;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: transform 0.12s, border-color 0.12s;
}

.swatch:hover {
    transform: scale(1.08);
}

.swatch.active {
    border-color: rgb(var(--primary-6));
}

.swatch.custom {
    background: var(--color-fill-2);
    color: var(--color-text-2);
    font-weight: 600;
    line-height: 1;
}

.swatch-plus {
    font-size: 16px;
}

/* 主题背景：自定义图行（选图/当前名/清除）与隐藏的文件输入 */
.bg-image-row {
    display: flex;
    align-items: center;
    gap: calc(var(--base-padding) * 5 / 6);
    margin-top: calc(var(--base-padding) * 2 / 3);
    flex-wrap: wrap;
}

.bg-file-input {
    display: none;
}

.glass-presets {
    margin-left: var(--base-padding);
}

.theme-preview {
    display: flex;
    gap: calc(var(--base-padding) * 4 / 3);
    flex-wrap: wrap;
}

.tp-item {
    display: flex;
    align-items: center;
    gap: calc(var(--base-padding) / 2);
}

.tp-chip {
    width: 22px;
    height: 22px;
    border-radius: var(--base-radius-default);
    box-shadow: inset 0 0 0 1px rgba(0, 0, 0, 0.08);
}

.tp-name {
    font-size: var(--base-size-small);
    color: var(--color-text-2);
}

.logo-preview {
    width: 48px;
    height: 48px;
    border-radius: var(--base-radius-large);
    border: 1px dashed var(--color-border-1);
    display: flex;
    align-items: center;
    justify-content: center;
    overflow: hidden;
    background: var(--color-bg-1);
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
    border: 1px dashed var(--color-border-1);
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

/* 快捷搜索：额外索引目录列表（每行一条路径 + 移除按钮，长路径省略号收尾） */
.lk-dirs {
    display: flex;
    flex-direction: column;
    gap: calc(var(--base-padding) / 2);
    width: 100%;
    align-items: flex-start;
}

.lk-dir-row {
    display: flex;
    align-items: center;
    gap: calc(var(--base-padding) * 2 / 3);
    width: 100%;
    padding: calc(var(--base-padding) / 6) calc(var(--base-padding) * 2 / 3);
    border: 1px solid var(--color-border-1);
    border-radius: var(--base-radius-default);
    background: var(--color-bg-1);
}

.lk-dir-path {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    font-size: var(--base-size-small);
    color: var(--color-text-2);
}

/* 索引状态行：弱色小字，与下面的 hint 区分开层次 */
.lk-idx-line {
    margin-top: calc(var(--base-padding) / 2);
    font-size: var(--base-size-small);
    color: var(--color-text-2);
}

.lk-idx-err {
    margin-top: calc(var(--base-padding) / 6);
    font-size: var(--base-size-small);
    color: rgb(var(--danger-6));
    word-break: break-all;
}

/* 可搜命令清单：只作展示，点不动，颜色跟面板结果里的 danger 语义一致 */
.lk-cmds {
    display: flex;
    flex-wrap: wrap;
    gap: calc(var(--base-padding) / 2);
}

.lk-cmd {
    padding: 1px calc(var(--base-padding) * 2 / 3);
    border: 1px solid var(--color-border-1);
    border-radius: 999px;
    font-size: var(--base-size-small);
    color: var(--color-text-2);
    background: var(--color-bg-1);
}

.lk-cmd.danger {
    border-color: rgba(var(--danger-6), 0.5);
    color: rgb(var(--danger-6));
}

/* ---- 文件压缩：挂载状态行 ---- */
.ar-shell-row {
    display: flex;
    flex-wrap: wrap;
    gap: calc(var(--base-padding) * 4 / 3);
    font-size: var(--base-size-small);
    color: var(--color-text-2);
}

.ar-slot b {
    color: var(--color-text-1);
    font-weight: 600;
    margin-left: calc(var(--base-padding) / 6);
}

.ar-shell-btns {
    display: flex;
    /* 「安装 / 修复」「卸载」推到本行最右侧对齐（用户要求）：与左侧的挂载状态文字拉开，
       不再紧贴状态文案；容器宽由外层表单 maxWidth 决定，flex-end 即贴到行尾。 */
    justify-content: flex-end;
    gap: calc(var(--base-padding) * 2 / 3);
    margin-top: calc(var(--base-padding) * 2 / 3);
}
</style>
