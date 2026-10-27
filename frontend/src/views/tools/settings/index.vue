<template>
    <!-- 布局参考 Redis 工具页：整页左右分栏，左侧分类导航 + 右侧主面板 -->
    <a-row class="settings-page" :gutter="6">
        <!-- 左侧：设置分类 -->
        <a-col :span="6" class="side-col">
            <a-card class="side-card card-head-tool" size="small" :body-style="{ padding: 0 }">
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
            <a-card class="main-panel card-head-tool" :body-style="{ padding: 0 }">
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
                    <a-card v-show="activeTab === 'general'" class="pane-card card-head-tool">
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
                                <a-radio-group size="medium" v-model="form.general.theme" type="button" @change="onThemeChange">
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
                    <a-card v-show="activeTab === 'paths'" class="pane-card card-head-tool">
                        <template #title>全局目录布局</template>
                        <a-form :model="form" :style="{ maxWidth: '720px' }" layout="vertical">
                            <a-form-item field="paths.plugins">
                                <template #label>插件目录（plugins）</template>
                                <div class="path-row">
                                    <a-input v-model="form.paths.plugins" placeholder="留空使用默认（应用目录/plugins）"
                                        allow-clear @blur="scheduleSave(0)" @press-enter="scheduleSave(0)"
                                        @clear="scheduleSave(0)" />
                                    <a-button size="small" @click="pickBaseDir('plugins', '选择插件目录')">选择目录</a-button>
                                </div>
                                <div class="hint">服务程序安装位置：PHP/Nginx/MySQL/Redis/npm 等按服务名建子目录（plugins/服务名）。修改后新安装/下载的服务落到新目录。</div>
                            </a-form-item>
                            <a-form-item field="paths.data">
                                <template #label>数据目录（data）</template>
                                <div class="path-row">
                                    <a-input v-model="form.paths.data" placeholder="留空使用默认（应用目录/data）"
                                        allow-clear @blur="scheduleSave(0)" @press-enter="scheduleSave(0)"
                                        @clear="scheduleSave(0)" />
                                    <a-button size="small" @click="pickBaseDir('data', '选择数据目录')">选择目录</a-button>
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
                                    <a-button size="small" @click="pickBaseDir('logs', '选择日志目录')">选择目录</a-button>
                                    <a-button size="small" v-if="activePath" @click="openLogDir">打开日志目录</a-button>
                                </div>
                                <div class="hint">程序与各服务日志的根目录，按服务名建子目录：logs/app 程序日志、logs/mysql SQL 日志、logs/php 实例服务日志等。开启文件日志后 info / warn / error 全部记录，按“级别 + 当前日期”自动分文件（如 Info20261022.log、Warn20261022.log、Error20261022.log）。当前程序日志目录：{{ activePath || "（未加载）" }}</div>
                            </a-form-item>
                        </a-form>
                    </a-card>

                    <!-- ===================== 访问与安全（仅 Web / 服务模式） ===================== -->
                    <a-card v-show="activeTab === 'panel'" class="pane-card card-head-tool">
                        <template #title>面板访问</template>
                        <template #extra>
                            <a-space>
                                <a-tag v-if="panelInfo" size="small"
                                    :color="panelInfo.enabled ? 'green' : 'orangered'">
                                    {{ panelInfo.enabled ? "登录保护已开启" : "登录保护已关闭" }}
                                </a-tag>
                                <a-link v-if="panelInfo?.accessURL" @click="copyAccess">复制访问地址</a-link>
                            </a-space>
                        </template>

                        <a-spin :loading="panelLoading" style="display: block">
                            <a-form :model="panelForm" :style="{ maxWidth: '720px' }" layout="vertical">
                                <a-row :gutter="12">
                                    <a-col :span="12">
                                        <a-form-item field="panelUsername">
                                            <template #label>登录账号</template>
                                            <a-input v-model="panelForm.username" placeholder="admin"
                                                :style="{ maxWidth: '260px' }" allow-clear />
                                        </a-form-item>
                                    </a-col>
                                    <a-col :span="12">
                                        <a-form-item field="panelSession">
                                            <template #label>登录有效期（小时）</template>
                                            <a-input-number v-model="panelForm.sessionHours" :min="1" :max="720"
                                                :style="{ width: '140px' }" />
                                        </a-form-item>
                                    </a-col>
                                </a-row>
                                <span class="hint">改账号或密码后，其它已登录的设备会立即失效，需要重新登录。</span>

                                <a-row :gutter="12" :style="{ marginTop: '16px' }">
                                    <a-col :span="14">
                                        <a-form-item field="panelHost">
                                            <template #label>监听地址</template>
                                            <a-select v-model="panelForm.host" allow-create allow-search>
                                                <a-option value="0.0.0.0">0.0.0.0（所有网卡）</a-option>
                                                <a-option value="127.0.0.1">127.0.0.1（仅本机，走 nginx 反代）</a-option>
                                            </a-select>
                                        </a-form-item>
                                    </a-col>
                                    <a-col :span="10">
                                        <a-form-item field="panelPort">
                                            <template #label>端口</template>
                                            <a-input-number v-model="panelForm.port" :min="1" :max="65535"
                                                :style="{ width: '140px' }" />
                                        </a-form-item>
                                    </a-col>
                                </a-row>

                                <a-form-item field="panelEntry">
                                    <template #label>安全入口</template>
                                    <div class="path-row">
                                        <a-input v-model="panelForm.entryPath"
                                            placeholder="留空关闭，例如 /manage-9f3ad2" allow-clear />
                                        <a-button size="small" @click="genEntry">随机生成</a-button>
                                    </div>
                                    <span class="hint">
                                        设置后必须先访问该入口地址才能打开登录页；直接访问根地址一律返回 404，
                                        可挡掉大部分扫端口的机器人。改动立即生效，请记下来。
                                    </span>
                                </a-form-item>

                                <a-form-item field="panelEnabled">
                                    <template #label>登录保护</template>
                                    <a-switch v-model="panelForm.enabled" type="round" />
                                    <span class="hint">关闭后任何人访问面板地址都不需要登录，仅适合完全可信的内网临时排障。</span>
                                </a-form-item>

                                <a-form-item>
                                    <div class="panel-actions">
                                        <a-space>
                                            <a-button type="primary" :loading="panelSaving" @click="savePanel">保存并应用</a-button>
                                            <a-button @click="loadPanel">还原</a-button>
                                        </a-space>
                                        <div v-if="panelInfo?.accessURL" class="panel-access">
                                            <span class="hint">当前访问地址：</span>
                                            <code>{{ panelInfo.accessURL }}</code>
                                            <span v-if="panelInfo.listen" class="hint">（进程监听 {{ panelInfo.listen }}）</span>
                                        </div>
                                    </div>
                                </a-form-item>

                                <a-divider :margin="8" />

                                <a-form-item field="pwdCurrent">
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
                    </a-card>

                    <!-- ===================== 浮动球设置 ===================== -->
                    <a-card v-show="activeTab === 'ball'" class="pane-card card-head-tool">
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
                        <a-card class="pane-card tools-card card-head-tool" :body-style="{ padding: 0 }">
                            <template #title>功能列表</template>
                            <template #extra>
                                <a-space>
                                    <span class="hint hint-inline">快捷键为系统全局热键</span>
                                    <a-popconfirm content="恢复默认功能列表？自定义的增删与排序会被覆盖。" type="warning"
                                        position="lt" @ok="resetTools">
                                        <a-button size="small">恢复默认列表</a-button>
                                    </a-popconfirm>
                                    <a-button size="small" @click="openAdd">
                                        <template #icon><icon-plus /></template>新增功能
                                    </a-button>
                                </a-space>
                            </template>
                            <div class="tools-table-wrap" ref="toolsTableRef" @dragover="onToolsWrapDragOver"
                                @dragleave="onToolsWrapDragLeave" @drop="onToolsWrapDrop">
                                <a-table :data="tools" :pagination="false" size="small" row-key="id" :column-resizable="true"
                                    :scroll="tableScroll">
                                    <template #columns>
                                        <a-table-column title="排序" :width="112">
                                            <template #cell="{ rowIndex }">
                                                <a-space :size="2">
                                                    <!-- 拖拽把手：按住它在目标行上松手即可换位；上下钮保留做微调 -->
                                                    <a-tooltip content="按住拖动排序">
                                                        <span class="row-drag-handle" draggable="true"
                                                            @dragstart="toolDragStart(rowIndex, $event)" @dragend="toolDragEnd">
                                                            <icon-drag-dot-vertical />
                                                        </span>
                                                    </a-tooltip>
                                                    <a-button size="small" :disabled="rowIndex === 0" @click="move(rowIndex, -1)">
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
                                                    <component v-else :is="iconComp(record.icon)" class="arco-preview" />
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
                        </a-card>
                    </div>
                    <!-- ===================== 浏览器（全局默认） ===================== -->
                    <a-card v-show="activeTab === 'browser'" class="pane-card card-head-tool">
                        <template #title>浏览器默认设置</template>
                        <template #extra>
                            <a-space>
                                <span class="hint hint-inline">空间明细在浏览器页右上角齿轮里改</span>
                                <a-button size="small" @click="openBrowserWindow">
                                    <template #icon><icon-compass /></template>打开浏览器
                                </a-button>
                            </a-space>
                        </template>
                        <a-form :model="browserForm" :style="{ maxWidth: '720px' }" layout="vertical">
                            <a-form-item>
                                <template #label>空间数量上限</template>
                                <a-input-number v-model="browserForm.maxSpaces" :min="spaceMin" :max="20"
                                    @change="scheduleBrowserSave(0)" />
                                <span class="hint">
                                    当前已有 {{ browserForm.spaces.length }} 个空间，最多可建 {{ browserForm.maxSpaces }} 个。
                                    一个空间 = 一套独立 profile（登录态/缓存）+ 一套独立代理，空间内多标签共享登录态。
                                </span>
                            </a-form-item>
                            <a-form-item>
                                <template #label>默认首页</template>
                                <a-input v-model="browserForm.homepage" placeholder="about:blank" allow-clear
                                    @blur="scheduleBrowserSave(0)" @press-enter="scheduleBrowserSave(0)"
                                    @clear="scheduleBrowserSave(0)" />
                                <span class="hint">空间里留空首页时用这个；新标签与“关光标签自动补一个”都用它。</span>
                            </a-form-item>
                            <a-form-item>
                                <template #label>地址栏搜索模板</template>
                                <a-input v-model="browserForm.searchURL" placeholder="https://www.bing.com/search?q=%s"
                                    allow-clear @blur="scheduleBrowserSave(0)" @press-enter="scheduleBrowserSave(0)"
                                    @clear="scheduleBrowserSave(0)" />
                                <span class="hint">地址栏输入的不是网址时走这个地址，<code>%s</code> 会被替换成搜索词。</span>
                            </a-form-item>
                            <a-form-item>
                                <template #label>默认代理</template>
                                <a-radio-group size="medium" v-model="browserForm.proxyMode" type="button"
                                    @change="scheduleBrowserSave(0)">
                                    <a-radio value="system">跟随系统</a-radio>
                                    <a-radio value="direct">直连</a-radio>
                                    <a-radio value="custom">自定义</a-radio>
                                </a-radio-group>
                                <span class="hint">空间里没单独设置代理时用这档；“跟随系统”就是系统代理设置，不做任何覆盖。</span>
                            </a-form-item>
                            <a-form-item v-if="browserForm.proxyMode === 'custom'">
                                <template #label>代理地址与绕过列表</template>
                                <div class="bs-pair">
                                    <a-input v-model="browserForm.proxy" allow-clear
                                        placeholder="http://127.0.0.1:7890 或 socks5://127.0.0.1:1080"
                                        @blur="scheduleBrowserSave(0)" @press-enter="scheduleBrowserSave(0)" />
                                    <a-input v-model="browserForm.proxyBypass" allow-clear placeholder="绕过：localhost,127.0.0.1"
                                        @blur="scheduleBrowserSave(0)" @press-enter="scheduleBrowserSave(0)" />
                                </div>
                                <span class="hint">代理与 UA 是环境级参数，改完需要重新打开浏览器页（或在齿轮里保存）才生效。</span>
                            </a-form-item>
                            <a-form-item>
                                <template #label>默认 User-Agent</template>
                                <a-textarea v-model="browserForm.userAgent" :auto-size="{ minRows: 1, maxRows: 3 }"
                                    placeholder="留空使用内置默认 UA" allow-clear @blur="scheduleBrowserSave(0)" />
                            </a-form-item>
                            <a-form-item>
                                <template #label>启动时打开的空间</template>
                                <a-select v-model="browserForm.defaultSpace" :style="{ maxWidth: '280px' }"
                                    placeholder="选择默认空间" @change="scheduleBrowserSave(0)">
                                    <a-option v-for="s in browserForm.spaces" :key="s.id" :value="s.id"
                                        :label="s.name ? `${s.name}（${s.id}）` : s.id" />
                                </a-select>
                            </a-form-item>
                        </a-form>

                        <a-divider :margin="8" />
                        <div class="space-head">
                            <span class="space-title">空间列表</span>
                            <a-tooltip content="增删空间、单独设置代理/UA/首页请到浏览器页右上角齿轮">
                                <icon-question-circle class="bs-help" />
                            </a-tooltip>
                        </div>
                        <a-table :data="spaceRows" :pagination="false" size="small" row-key="id"
                            :scroll="{ x: 640 }" class="space-table">
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
                    </a-card>

                    <!-- ===================== 配置导出 / 导入 ===================== -->
                    <a-card v-show="activeTab === 'config'" class="pane-card card-head-tool">
                        <template #title>配置同步</template>
                        <div class="cfg-sync">
                            <div class="cfg-sync-label">导出 / 导入配置</div>
                            <a-space>
                                <a-button size="small" @click="exportConfig">
                                    <template #icon><icon-export /></template>导出配置
                                </a-button>
                                <a-popconfirm
                                    content="导入会覆盖本机的工具列表、浏览器空间与代理、主题等设置（目录 / Logo / 浮动球位置保留本机值）。继续？"
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
                    </a-card>
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
                            <a-button size="small" @click="() => { proxy?.$refs['selectModalIconRef']?.open(); }">图标</a-button>
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
import { Events } from "@wailsio/runtime";
import { Message } from "@arco-design/web-vue";
import * as ArcoIconComps from "@arco-design/web-vue/es/icon";
import iconData from "@/components/common/select-icon-modal/icon.json";
import { applyTheme, type ThemeMode } from "@/utils/theme";
import { useCaps } from "@/utils/caps";
import { changePanelPassword, getPanelInfo, savePanelSettings, type PanelInfo } from "@/utils/panel";
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
    builtin("imageBase64", "图片base64", { path: "/tools/imageBase64" }),
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

/**
 * 设置分类：左侧导航与右侧面板标题共用。
 * 浮动球与浏览器默认设置只在能开原生窗口的桌面版里有意义，Web 模式下直接不给入口。
 */
const SETTING_TABS = computed(() =>
    [
        { key: "general", label: "通用", icon: "IconSettings", desktopOnly: false },
        { key: "paths", label: "目录", icon: "IconFolder", desktopOnly: false },
        { key: "panel", label: "访问与安全", icon: "IconLock", desktopOnly: false, webOnly: true },
        { key: "ball", label: "浮动球", icon: "IconDesktop", desktopOnly: true },
        { key: "browser", label: "浏览器", icon: "IconCompass", desktopOnly: true },
        { key: "config", label: "配置", icon: "IconSync", desktopOnly: false },
        { key: "tools", label: "工具", icon: "IconApps", desktopOnly: false },
    ]
        .filter((t) => !t.desktopOnly || caps.nativeWindows)
        .filter((t) => !t.webOnly || !caps.nativeWindows),
);
const activeTab = ref("general");
const activeTabMeta = computed(() => SETTING_TABS.value.find((t) => t.key === activeTab.value) || SETTING_TABS.value[0]);

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
});

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
        applyTheme(String(form.general.theme || "light") as ThemeMode);
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
            form.ball = { enabled: !!cfg.ball?.enabled, icon: cfg.ball?.icon || "" };
            form.general = { autoStart: !!cfg.general?.autoStart, appName: cfg.general?.appName || "Tools", appLogo: cfg.general?.appLogo || "", theme: cfg.general?.theme || "light" };
            form.paths = { plugins: cfg.paths?.plugins || "", data: cfg.paths?.data || "", logs: cfg.paths?.logs || "" };
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
    return (t as HTMLElement | null)?.closest?.(".arco-table-tr") ?? null;
}
function rowIdxOfTr(tr: HTMLElement | null): number {
    // 只认表体行：表头 tr 也叫 .arco-table-tr，落在 thead 里不参与排序
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

/** 默认工具列表：“恢复默认列表”还原到这份基线，与后端 config.DefaultConfig 的 Tools 逐项一致（含顺序）。
 * 这是当前保存配置（config.json）的快照（自定义图标/尺寸/开关/主页展示已烘入），不再从 BUILTINS 派生，避免默认基线漂移。
 * 不含 settings（设置仍可从浮动球/标题栏入口进入）。 */
const DEFAULT_TOOLS: ToolRow[] = [
    { id: "docker", key: "docker", name: "Docker管理", path: "/tools/docker", event: "", icon: "icon-docker", single: true, fullscreen: false, disableFullscreen: false, width: 1360, height: 860, shortcut: "", quick: false, home: true },
    { id: "browser", key: "browser", name: "浏览器", path: "/tools/browser", event: "", icon: "icon-liulanqi1", single: true, fullscreen: true, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false, home: true },
    { id: "speedtest", key: "speedtest", name: "网速测速", path: "/tools/speedtest", event: "", icon: "icon-cesu", single: true, fullscreen: false, disableFullscreen: false, width: 1080, height: 720, shortcut: "", quick: false, home: true },
    { id: "term", key: "term", name: "终端连接", path: "/tools/term", event: "", icon: "icon-terminal", single: true, fullscreen: false, disableFullscreen: false, width: 1200, height: 800, shortcut: "", quick: false, home: true },
    { id: "disk", key: "disk", name: "磁盘管理", path: "/tools/disk", event: "", icon: "icon-yingpan", single: true, fullscreen: false, disableFullscreen: false, width: 1360, height: 860, shortcut: "", quick: false, home: true },
    { id: "ftp", key: "ftp", name: "文件传输", path: "/tools/ftp", event: "", icon: "icon-ftp", single: true, fullscreen: false, disableFullscreen: false, width: 1360, height: 860, shortcut: "", quick: false, home: true },
    { id: "redis", key: "redis", name: "Redis管理", path: "/tools/redis", event: "", icon: "icon-icon-redis", single: true, fullscreen: false, disableFullscreen: false, width: 1360, height: 860, shortcut: "", quick: false, home: true },
    { id: "mysql", key: "mysql", name: "MySQL管理", path: "/tools/mysql", event: "", icon: "icon-MySQL", single: true, fullscreen: false, disableFullscreen: false, width: 1360, height: 860, shortcut: "", quick: false, home: true },
    { id: "php", key: "php", name: "PHP管理", path: "/tools/php", event: "", icon: "icon-php", single: true, fullscreen: false, disableFullscreen: false, width: 1360, height: 860, shortcut: "", quick: false, home: true },
    { id: "nginx", key: "nginx", name: "Nginx管理", path: "/tools/nginx", event: "", icon: "icon-ngnix", single: true, fullscreen: false, disableFullscreen: false, width: 1360, height: 860, shortcut: "", quick: false, home: true },
    { id: "npm", key: "npm", name: "Npm管理", path: "/tools/npm", event: "", icon: "icon-npm", single: true, fullscreen: false, disableFullscreen: false, width: 1360, height: 860, shortcut: "", quick: false, home: true },
    { id: "sniffer", key: "sniffer", name: "抓包工具", path: "/tools/sniffer", event: "", icon: "bug", single: true, fullscreen: true, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: true, home: true },
    { id: "websocket", key: "websocket", name: "WebSocket调试", path: "/tools/websocket", event: "", icon: "icon-wangluo1", single: true, fullscreen: true, disableFullscreen: false, width: 1360, height: 860, shortcut: "", quick: false, home: true },
    { id: "timestamp", key: "timestamp", name: "时间戳转换", path: "/tools/timestamp", event: "", icon: "icon-riqishijianzhuanhuanmiaoshu", single: false, fullscreen: false, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false, home: true },
    { id: "usbserial", key: "usbserial", name: "USB调试", path: "/tools/usbserial", event: "", icon: "icon-usb1", single: true, fullscreen: false, disableFullscreen: false, width: 1360, height: 860, shortcut: "", quick: false, home: true },
    { id: "video", key: "video", name: "视频录制", path: "/tools/video", event: "", icon: "video-camera", single: true, fullscreen: true, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: true, home: false },
    { id: "screenshot", key: "screenshot", name: "截图", path: "", event: "screenshot", icon: "icon-jietu", single: false, fullscreen: false, disableFullscreen: false, width: 0, height: 0, shortcut: "Ctrl+Alt+A", quick: true, home: true },
    { id: "colorpicker", key: "colorpicker", name: "颜色取色器", path: "/tools/colorpicker", event: "", icon: "icon-quseqi", single: true, fullscreen: false, disableFullscreen: true, width: 480, height: 560, shortcut: "", quick: true, home: true },
    { id: "encdec", key: "encdec", name: "文字加密解密", path: "/tools/encdec", event: "", icon: "lock", single: false, fullscreen: false, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false, home: false },
    { id: "strrandom", key: "strrandom", name: "随机字符串生成", path: "/tools/strrandom", event: "", icon: "icon-zifuchuan", single: false, fullscreen: false, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false, home: false },
    { id: "qrcode", key: "qrcode", name: "二维码生成", path: "/tools/qrcode", event: "", icon: "icon-erweima3", single: false, fullscreen: false, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false, home: false },
    { id: "imageBase64", key: "imageBase64", name: "图片base64", path: "/tools/imageBase64", event: "", icon: "icon-img", single: false, fullscreen: false, disableFullscreen: false, width: 0, height: 0, shortcut: "", quick: false, home: false },
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

let offConfigSaved: (() => void) | null = null;

onMounted(() => {
    load();
    // 主页拖动快捷操作后同步工具表顺序（config:saved 由后端在配置落盘后广播）
    try {
        const ret = Events.On("config:saved", () => { void syncToolsFromConfig(); });
        if (typeof ret === "function") offConfigSaved = ret as () => void;
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

/* 配置同步：没有表单控件绑定，用普通块排版，标签/提示跟表单风格对齐 */
.cfg-sync {
    display: flex;
    flex-direction: column;
    gap: 8px;
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
    gap: 8px;
    width: 100%;
}

/* 空间列表小标题：跟表单标签同字号，右侧带一个解释图标 */
.space-head {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-bottom: 6px;
}

.space-title {
    font-size: var(--base-size-small);
    color: var(--color-text-2);
}

.bs-help {
    color: var(--color-text-3);
    cursor: help;
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

/* 面板当前访问地址：单独一行弱色描边块，避免长 URL 换行时和按钮缠在一起 */
.panel-actions {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 10px;
    width: 100%;
}

.panel-access {
    padding: 6px 10px;
    border-radius: var(--border-radius-medium);
    background: var(--color-fill-1);
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

/* 拖拽把手：常显淡灰，hover 提亮；按住起拖 */
.row-drag-handle {
    display: inline-flex;
    align-items: center;
    color: var(--color-text-3);
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
