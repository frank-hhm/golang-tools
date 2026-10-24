<template>
    <a-row class="npm-page" :gutter="6">
        <!-- 左侧：静态站点实例 + 功能入口 -->
        <a-col :span="7" class="side-col">
            <a-card class="side-card" size="small" :body-style="{ padding: 0 }">
                <template #title>静态站点服务</template>
                <template #extra>
                    <div class="srv-head-actions">
                        <a-button size="small" title="新建站点" @click="openInstModal()">新建站点</a-button>
                        <a-button size="small" title="Node 版本管理" @click="openVersionsModal()">版本管理</a-button>
                    </div>
                </template>
                <a-list :bordered="false" size="small">
                    <a-list-item v-for="inst in instances" :key="inst.id"
                        :class="{ 'inst-active': selectedId === inst.id }" @click="selectInst(inst.id)">
                        <a-space>
                            <a-badge :status="states[inst.id]?.running ? 'success' : 'normal'" />
                            <span class="conn-name" :title="inst.name + ':' + inst.port">{{ inst.name }}</span>
                            <a-tag color="arcoblue" size="small">node {{ inst.version }}</a-tag>
                        </a-space>
                        <template #actions>
                            <a-dropdown trigger="hover" @select="(v: any) => onInstAction(v, inst)">
                                <icon-more class="conn-more" @click.stop />
                                <template #content>
                                    <a-doption v-if="!states[inst.id]?.running" value="start"><icon-play-arrow />
                                        启动</a-doption>
                                    <a-doption v-else value="stop"><icon-pause /> 停止</a-doption>
                                    <a-doption value="restart"><icon-refresh /> 重启</a-doption>
                                    <a-doption value="open"><icon-launch /> 打开站点</a-doption>
                                    <a-doption value="logs"><icon-face-folder-open /> 日志</a-doption>
                                    <a-doption value="edit"><icon-edit /> 编辑</a-doption>
                                    <a-doption value="del"><icon-delete /> 删除</a-doption>
                                </template>
                            </a-dropdown>
                        </template>
                    </a-list-item>
                    <a-list-item v-if="instances.length === 0">
                        <div class="conn-empty">暂无站点，点击右上"新建站点"</div>
                    </a-list-item>
                </a-list>
            </a-card>
            <a-card class="side-card tools-card" size="small" :body-style="{ padding: '10px' }">
                <template #title>npm 工具箱</template>
                <div class="toolbox">
                    <a-button long @click="openNpmConfig()">
                        <template #icon><icon-settings /></template>npm 配置 / 镜像源
                    </a-button>
                    <a-button long @click="openCliChain()">
                        <template #icon><icon-swap /></template>npm 版本切换（命令行生效）
                    </a-button>
                    <a-button long @click="openPkgs()">
                        <template #icon><icon-storage /></template>全局包管理
                    </a-button>
                </div>
                <div class="reg-hint">当前 registry：{{ registryVal || '-' }}</div>
            </a-card>
        </a-col>

        <!-- 右侧：选中站点详情 + 日志 -->
        <a-col :span="17" class="main-col">
            <a-card v-if="!selectedId" class="main-panel empty-card">
                <a-empty description="从左侧选择一个静态站点查看状态与访问日志" />
            </a-card>
            <a-card v-else class="main-panel" :body-style="{ padding: 0 }">
                <template #title>
                    <a-space>
                        <a-badge :status="selState?.running ? 'success' : 'normal'" />
                        <span>{{ selCfg?.name }}</span>
                        <a-tag color="arcoblue" size="small">:{{ selCfg?.port }}</a-tag>
                        <a-tag v-if="selCfg?.spa" size="small">SPA</a-tag>
                        <a-tag v-if="selCfg?.directory" size="small">列目录</a-tag>
                    </a-space>
                </template>
                <template #extra>
                    <a-space>
                        <a-button size="small" type="primary" :disabled="selState?.running"
                            @click="doStart">启动</a-button>
                        <a-button size="small" status="danger" :disabled="!selState?.running"
                            @click="doStop">停止</a-button>
                        <a-button size="small" @click="doRestart">重启</a-button>
                        <a-button size="small" :disabled="!selState?.running" @click="openSite(selCfg?.port)">在新窗口打开</a-button>
                    </a-space>
                </template>
                <div class="detail-grid">
                    <div class="ls-item"><span>运行状态</span>
                        <b :class="selState?.running ? 'ok' : 'no'">{{ selState?.running ? '运行中' : '已停止' }}</b>
                    </div>
                    <div class="ls-item"><span>运行时长</span><b>{{ fmtUptimeS(selState?.uptimeSeconds) }}</b></div>
                    <div class="ls-item"><span>Node 版本</span><b>{{ selState?.version || selCfg?.version || '-' }}</b>
                    </div>
                    <div class="ls-item"><span>npm 版本</span><b>{{ selState?.npmVer || '-' }}</b></div>
                    <div class="ls-item"><span>累计请求</span><b>{{ selState?.requests ?? '-' }}</b></div>
                    <div class="ls-item"><span>req/s</span><b>{{ selState?.requestsPerSec ? selState.requestsPerSec.toFixed(2) : '-' }}</b></div>
                    <div class="ls-item"><span>累计发送</span><b>{{ fmtBytes(selState?.bytes) }}</b></div>
                    <div class="ls-item"><span>站点文件数</span><b>{{ selState?.files ?? '-' }}</b></div>
                    <div class="ls-item"><span>registry</span><b class="ellipsis">{{ selState?.registry || '-' }}</b></div>
                    <div class="ls-item" title="站点根目录"><span>站点目录</span><b class="ellipsis">{{
                        selState?.docRoot || '-' }}</b></div>
                    <div class="ls-item" title="node.exe 路径"><span>node.exe</span><b class="ellipsis">{{
                        selState?.nodePath || '-' }}</b></div>
                </div>
                <div class="log-head">
                    <span>访问日志（实时）</span>
                    <a-button size="mini" @click="logLines = []">
                        <template #icon><icon-delete /></template>清空
                    </a-button>
                </div>
                <div ref="logBoxRef" class="ls-log">
                    <div v-for="(l, i) in logLines" :key="i" class="ls-line">{{ l }}</div>
                    <div v-if="logLines.length === 0" class="conn-empty">暂无日志</div>
                </div>
            </a-card>
        </a-col>
    </a-row>

    <!-- 新建/编辑站点实例 -->
    <a-modal v-model:visible="instVisible" :title="instForm.id ? '编辑站点' : '新建站点'" :width="520"
        @before-ok="saveInstance">
        <a-form :model="instForm" layout="vertical">
            <a-form-item field="name" label="站点名称" required>
                <a-input v-model="instForm.name" placeholder="如 docs-site" />
            </a-form-item>
            <a-form-item field="version" label="Node.js 版本（用于展示与配套 npm）" required>
                <a-select v-model="instForm.version" placeholder="选择已安装版本" :options="versionOptions" />
            </a-form-item>
            <a-form-item field="port" label="端口">
                <a-input-number v-model="instForm.port" :min="1" :max="65535" @blur="checkPortTaken" />
            </a-form-item>
            <a-form-item field="docRoot" label="站点根目录（空 = 实例目录 www/）">
                <a-input-search v-model="instForm.docRoot" placeholder="如 D:\www\dist" search-button
                    button-text="选择目录" @click-btn="chooseDocRoot" />
            </a-form-item>
            <a-form-item field="spa" label="SPA 模式（找不到文件回退 index.html）">
                <a-switch v-model="instForm.spa" />
            </a-form-item>
            <a-form-item field="directory" label="允许列目录（无 index.html 时）">
                <a-switch v-model="instForm.directory" />
            </a-form-item>
            <a-form-item field="openBrowser" label="启动成功后自动打开浏览器">
                <a-switch v-model="instForm.openBrowser" />
            </a-form-item>
        </a-form>
    </a-modal>

    <!-- Node 版本管理 -->
    <a-modal v-model:visible="versionsVisible" title="Node.js 版本管理" :width="780" :footer="false"
        unmount-on-close>
        <div class="ver-block">
            <div class="ver-title">
                本地可用版本
                <a-button size="mini" :loading="scanning" @click="scanLocalNodes">扫描本机 Node</a-button>
                <a-button size="mini" @click="loadLocalVersions">刷新</a-button>
            </div>
            <a-table :data="localVersions" size="small" :pagination="false" row-key="version"
                :columns="[
                    { title: '版本', dataIndex: 'version', width: 110 },
                    { title: 'npm', dataIndex: 'npmVer', width: 90 },
                    { title: '来源', dataIndex: 'source', width: 90, slotName: 'source' },
                    { title: '路径', dataIndex: 'path', ellipsis: true, tooltip: true },
                    { title: '操作', dataIndex: 'ops', width: 90, slotName: 'ops' },
                ]">
                <template #source="{ record }">
                    <a-tag size="small" :color="record.source === 'bundled' ? 'purple' : record.source === 'downloaded' ? 'green' : 'orange'">{{ srcLabel(record.source) }}</a-tag>
                </template>
                <template #ops="{ record }">
                    <a-button size="mini" status="danger" :disabled="record.source === 'bundled'"
                        @click="removeVersion(record)">删除</a-button>
                </template>
            </a-table>
            <a-row class="add-local" :gutter="8">
                <a-col :span="16">
                    <a-input v-model="localDirInput" placeholder="注册本机已有 Node.js：填写 node.exe 所在目录" />
                </a-col>
                <a-col :span="4">
                    <a-button block @click="chooseLocalDir">浏览…</a-button>
                </a-col>
                <a-col :span="4">
                    <a-button type="primary" block @click="addLocalDir">添加</a-button>
                </a-col>
            </a-row>
        </div>
        <a-divider style="margin: 12px 0" />
        <div class="ver-block">
            <div class="ver-title">
                线上版本
                <a-button size="mini" :loading="remoteLoading" @click="loadRemoteVersions">刷新</a-button>
            </div>
            <a-spin :loading="remoteLoading" style="width:100%">
                <a-table :data="remoteVersions" size="small" :pagination="{ pageSize: 8 }" row-key="version"
                    :columns="[
                        { title: '版本', dataIndex: 'version', width: 110 },
                        { title: 'LTS', dataIndex: 'lts', width: 90, slotName: 'lts' },
                        { title: '包大小', dataIndex: 'size', width: 100, slotName: 'size' },
                        { title: '状态', dataIndex: 'st', width: 150, slotName: 'st' },
                        { title: '操作', dataIndex: 'ops', width: 100, slotName: 'ops' },
                    ]">
                    <template #lts="{ record }">
                        <a-tag v-if="record.lts" color="green" size="small">{{ record.lts }}</a-tag>
                    </template>
                    <template #size="{ record }">{{ fmtBytes(record.size) }}</template>
                    <template #st="{ record }">
                        <a-tag v-if="installedSet.has(record.version)" color="green" size="small">已安装</a-tag>
                        <a-tag v-else-if="installProgress[record.version]" :color="installProgress[record.version].stage === 'error' ? 'red' : 'arcoblue'" size="small">
                            {{ stageText(installProgress[record.version]) }}
                        </a-tag>
                        <a-tag v-else size="small" color="gray">未安装</a-tag>
                    </template>
                    <template #ops="{ record }">
                        <a-button v-if="!installedSet.has(record.version)" size="mini" type="primary"
                            :disabled="!!installProgress[record.version]" @click="installVersion(record)">安装</a-button>
                        <a-button v-else size="mini" @click="loadLocalVersions">已安装</a-button>
                    </template>
                </a-table>
            </a-spin>
            <div class="offline-tip">离线安装：将 node zip 放入 <a-link copyable :content="pluginsDirVal">{{ pluginsDirVal || '…' }}</a-link> 后刷新本地版本</div>
        </div>
        <div v-if="installLogs.length" class="ver-block install-log">
            <div class="ver-title">任务日志</div>
            <div class="ls-log small">
                <div v-for="(l, i) in installLogs" :key="i" class="ls-line">{{ l }}</div>
            </div>
        </div>
    </a-modal>

    <!-- npm 配置 -->
    <a-modal v-model:visible="cfgVisible" title="npm 配置（托管 .npmrc）" :width="720" :footer="false"
        unmount-on-close>
        <div class="cfg-registry">
            <div class="ver-title">镜像源快捷切换</div>
            <a-space wrap>
                <a-button v-for="r in commonRegistries" :key="r.value" size="small"
                    :type="registryVal === r.value ? 'primary' : 'outline'" @click="applyRegistry(r.value)">
                    {{ r.key }}
                </a-button>
            </a-space>
            <a-input-search v-model="registryInput" placeholder="自定义 registry URL" size="small"
                style="margin-top:8px;width:420px" search-button button-text="应用" @search="applyRegistry(registryInput)" />
        </div>
        <a-divider style="margin: 12px 0" />
        <div class="ver-title">
            配置项
            <a-button size="mini" @click="loadConfigItems">刷新</a-button>
        </div>
        <a-table :data="configItems" size="small" :pagination="false" row-key="key"
            :columns="[
                { title: 'Key', dataIndex: 'key', width: 220 },
                { title: 'Value', dataIndex: 'value', slotName: 'value' },
            ]">
            <template #value="{ record }">
                <a-input v-model="record.value" size="mini" @press-enter="setItem(record)"
                    @blur="setItem(record)" />
            </template>
        </a-table>
        <a-row class="add-local" :gutter="8">
            <a-col :span="8"><a-input v-model="newKey" size="small" placeholder="新增 Key" /></a-col>
            <a-col :span="12"><a-input v-model="newVal" size="small" placeholder="Value" /></a-col>
            <a-col :span="4"><a-button size="small" type="primary" block @click="addConfigItem">新增</a-button></a-col>
        </a-row>
        <a-divider style="margin: 12px 0" />
        <a-collapse>
            <a-collapse-item header=".npmrc 原文编辑" key="raw">
                <a-textarea v-model="npmrcText" :auto-size="{ minRows: 8, maxRows: 14 }" spellcheck="false" />
                <a-space style="margin-top:8px">
                    <a-button size="small" @click="loadNpmrcText">重新读取</a-button>
                    <a-button size="small" type="primary" @click="saveNpmrcText">保存 .npmrc</a-button>
                </a-space>
            </a-collapse-item>
        </a-collapse>
        <div class="user-npmrc">
            <div class="ver-title">用户全局 ~/.npmrc</div>
            <div class="offline-tip">{{ userNpmrcPath || '（未创建）' }}
                <a-button size="mini" style="margin-left:8px" @click="applyToUser">把托管配置同步过去（自动备份）</a-button>
            </div>
        </div>
    </a-modal>

    <!-- 全局包管理（多包管理器） -->
    <a-modal v-model:visible="pkgsVisible" title="全局包管理" :width="860" :footer="false" unmount-on-close>
        <a-row :gutter="8" align="center" style="margin-bottom:6px">
            <a-col :span="3" class="mgr-label">装/卸用</a-col>
            <a-col :span="7">
                <a-select v-model="pkgMgrId" :options="mgrOptions" size="small" placeholder="包管理器"
                    :loading="mgrLoading" allow-search />
            </a-col>
            <a-col :span="10">
                <a-input v-model="pkgInstallName" size="small" :placeholder="installHint" @press-enter="installPkg" />
            </a-col>
            <a-col :span="4">
                <a-button size="small" type="primary" block :disabled="!!pkgBusy" @click="installPkg">安装</a-button>
            </a-col>
            <a-col :span="4">
                <a-button size="small" block :loading="pkgsLoading" @click="scanPkgs">扫描</a-button>
            </a-col>
        </a-row>
        <div class="offline-tip" style="margin-top:0;margin-bottom:8px">{{ mgrHint }}</div>
        <a-input v-model="pkgFilter" size="small" placeholder="按包名过滤" allow-clear
            style="width:220px;margin-bottom:8px" />
        <a-spin :loading="pkgsLoading" style="width:100%">
            <a-table :data="filteredPackages" size="small" :pagination="false" row-key="path"
                :columns="[
                    { title: '包名', dataIndex: 'name', width: 200, ellipsis: true, tooltip: true },
                    { title: '版本', dataIndex: 'version', width: 96 },
                    { title: '管理器', dataIndex: 'manager', width: 84, slotName: 'manager' },
                    { title: 'CLI', dataIndex: 'hasBin', width: 56, slotName: 'bin' },
                    { title: '说明', dataIndex: 'description', ellipsis: true, tooltip: true },
                    { title: '操作', dataIndex: 'ops', width: 78, slotName: 'ops' },
                ]">
                <template #manager="{ record }">
                    <a-tag size="small" :color="mgrColor(record.manager)">{{ record.manager }}</a-tag>
                </template>
                <template #bin="{ record }">
                    <span v-if="record.hasBin" class="bin-dot">●</span>
                </template>
                <template #ops="{ record }">
                    <a-tooltip v-if="record.builtin" content="随 Node 发行，不可单独卸载">
                        <a-button size="mini" disabled>发行件</a-button>
                    </a-tooltip>
                    <a-button v-else size="mini" status="danger" :disabled="!!pkgBusy"
                        @click="uninstallPkg(record)">卸载</a-button>
                </template>
            </a-table>
            <div v-if="!pkgsLoading && filteredPackages.length === 0" class="conn-empty">
                {{ packages.length ? '没有匹配过滤的包' : '未扫到全局包（各管理器的全局安装根都是空的）' }}
            </div>
        </a-spin>
        <div v-if="pkgBusy" class="offline-tip">{{ pkgBusy }} …（进度见任务日志）</div>
        <div v-if="installLogs.length" class="ver-block install-log">
            <div class="ver-title">任务日志</div>
            <div class="ls-log small">
                <div v-for="(l, i) in installLogs" :key="i" class="ls-line">{{ l }}</div>
            </div>
        </div>
    </a-modal>

    <!-- npm 本体版本切换：改的是 cmd 里实际执行的那一份 -->
    <a-modal v-model:visible="cliVisible" title="npm 版本切换（命令行生效链）" :width="900" :footer="false"
        unmount-on-close>
        <a-spin :loading="chainLoading" style="width:100%">
            <div class="chain-hero">
                <div class="chain-hero-l">
                    <div class="chain-ver">
                        <span class="chain-cap">cmd 里 <b>npm -v</b></span>
                        <b class="chain-big">{{ chain?.activeVersion || '—' }}</b>
                        <a-tag size="small" :color="chain?.activeFrom === 'override' ? 'orange' : 'arcoblue'">{{
                            layerName(chain?.activeFrom) }}</a-tag>
                    </div>
                    <div class="chain-lines">
                        <div class="ls-item"><span>转发壳</span><b class="ellipsis" :title="chain?.activeShim">{{
                                chain?.activeShim || '-' }}</b></div>
                        <div class="ls-item"><span>node.exe</span><b class="ellipsis" :title="chain?.activeNode">{{
                                chain?.activeNode || '-' }}</b></div>
                        <div class="ls-item"><span>全局 prefix</span><b class="ellipsis" :title="chain?.activePrefix">{{
                                chain?.activePrefix || '-' }}</b></div>
                    </div>
                </div>
                <a-space direction="vertical" :size="6">
                    <a-button size="small" :loading="chainLoading" @click="loadChain()">
                        <template #icon><icon-refresh /></template>重新体检
                    </a-button>
                    <a-button size="small" :loading="relLoading" @click="ensureReleases(true)">
                        <template #icon><icon-history /></template>刷新版本清单
                    </a-button>
                </a-space>
            </div>
            <div v-if="chainHints.length" class="chain-hints">
                <div v-for="(h, i) in chainHints" :key="i" class="hint-line">• {{ h }}</div>
            </div>
            <div v-if="chainShadowed.length" class="offline-tip">PATH 里被压住的 npm 入口：{{
                chainShadowed.join('、') }}</div>

            <div class="ver-title" style="margin-top:14px">npm 入口（点一行选择）</div>
            <a-table :data="chainEntries" size="small" :pagination="false" row-key="dir" @row-click="pickEntryRow"
                :row-class="(r: any) => (r.dir === cliEntryDir ? 'row-active' : '')" :columns="[
                    { title: '目录', dataIndex: 'dir', ellipsis: true, tooltip: true },
                    { title: 'Node', dataIndex: 'version', width: 92 },
                    { title: 'PATH', dataIndex: 'pathIdx', width: 64, slotName: 'pidx' },
                    { title: '自带层', dataIndex: 'bundled', width: 90, slotName: 'bun' },
                    { title: '覆盖层', dataIndex: 'override', width: 90, slotName: 'ovr' },
                    { title: '实际生效', dataIndex: 'effective', width: 132, slotName: 'eff' },
                ]">
                <template #pidx="{ record }">
                    <span v-if="record.pathIdx >= 0">#{{ record.pathIdx }}</span>
                    <a-tag v-else size="small" color="gray">不在</a-tag>
                </template>
                <template #bun="{ record }">{{ record.bundled?.exists ? record.bundled.version : '-' }}</template>
                <template #ovr="{ record }">
                    <span v-if="record.override?.exists">{{ record.override.version }}</span>
                    <span v-else-if="record.override?.dir">无</span>
                    <span v-else>-</span>
                </template>
                <template #eff="{ record }">
                    <a-space :size="4">
                        <b>{{ record.effective || '-' }}</b>
                        <a-tag size="small" :color="record.effectiveFrom === 'override' ? 'orange' : 'arcoblue'">{{
                            layerName(record.effectiveFrom) }}</a-tag>
                    </a-space>
                </template>
            </a-table>
            <div v-if="!chainEntries.length && !chainLoading" class="conn-empty">
                没扫到带 npm 的目录，先到「Node.js 版本管理」扫描本机 Node
            </div>

            <template v-if="selEntry">
                <div class="ver-title" style="margin-top:14px">切换目标</div>
                <a-radio-group v-model="cliLayer" type="button" size="small">
                    <a-radio value="override">全局 prefix 覆盖层</a-radio>
                    <a-radio value="bundled">Node 自带层</a-radio>
                </a-radio-group>
                <div class="layer-note">{{ layerNote }}</div>
                <div class="detail-grid tight">
                    <div class="ls-item"><span>写入目录</span><b class="ellipsis" :title="selLayer?.dir">{{
                                selLayer?.dir || '-' }}</b></div>
                    <div class="ls-item"><span>该层现在</span><b>{{ selLayerNow }}</b></div>
                    <div class="ls-item"><span>该层出厂版本</span><b>{{ selLayerDefault || '-' }}
                            <a-link v-if="canRestoreDefault" size="mini"
                                @click="cliVersion = selLayerDefault">用它</a-link></b></div>
                    <div class="ls-item"><span>可撤销</span><b>{{ selLayer?.hasPrev ? '有（上一步）' : '无' }}</b></div>
                </div>
                <a-row :gutter="6" align="center" style="margin-top:10px">
                    <a-col :span="11">
                        <a-select v-model="cliVersion" size="small" allow-search :loading="relLoading"
                            placeholder="要切到的 npm 版本" :options="releaseOptions" />
                    </a-col>
                    <a-col :span="6">
                        <a-space :size="4">
                            <a-switch v-model="onlyCompat" size="small" @change="pickDefaultVersion" />
                            <span class="chain-cap">只列兼容本 Node</span>
                        </a-space>
                    </a-col>
                    <a-col :span="7">
                        <a-space :size="6">
                            <a-button size="small" type="primary" :disabled="!!cliBusy" @click="doSwitch()">切换</a-button>
                            <a-button size="small" :disabled="!selLayer?.hasPrev || !!cliBusy"
                                @click="doUndo()">撤销</a-button>
                            <a-button v-if="cliLayer === 'override' && selEntry.override?.exists" size="small"
                                status="danger" :disabled="!!cliBusy" @click="doRemoveOverride()">移除覆盖层</a-button>
                        </a-space>
                    </a-col>
                </a-row>
                <div v-if="relNote" class="offline-tip">{{ relNote }}</div>
                <div v-if="!relLoading && !releases.length" class="offline-tip">
                    版本清单为空：检查网络，或在「npm 配置」里换一个可达的 registry 后点「刷新版本清单」
                </div>
                <div v-else-if="onlyCompat && !releaseOptions.length" class="offline-tip">
                    当前 Node {{ selEntry.version || '?' }} 没有兼容的 npm 版本（关掉「只列兼容本 Node」可看全部）
                </div>
            </template>

            <div v-if="cliBusy" class="cli-prog">
                <span class="cli-name">{{ cliBusy }}</span>
                <a-progress :percent="(cliProgress?.percent || 0) / 100" size="small" style="flex:1"
                    :status="cliProgress?.stage === 'error' ? 'danger' : 'normal'" />
                <span class="cli-stage">{{ stageText(cliProgress) }}</span>
            </div>
            <div v-if="cliLogs.length" class="ver-block install-log">
                <div class="ver-title">切换日志
                    <a-button size="mini" @click="cliLogs = []">清空</a-button>
                </div>
                <div class="ls-log small">
                    <div v-for="(l, i) in cliLogs" :key="i" class="ls-line">{{ l }}</div>
                </div>
            </div>
        </a-spin>
    </a-modal>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, nextTick, onMounted, onUnmounted, reactive, ref } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import { useRoute } from "vue-router";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

interface InstConfig { id: string; name: string; version: string; port: number; docRoot: string; spa: boolean; directory: boolean; openBrowser: boolean }

/* ============================ 状态 ============================ */
const instances = ref<InstConfig[]>([]);
const states = reactive<Record<string, any>>({});
const selectedId = ref("");
const logLines = ref<string[]>([]);
const logBoxRef = ref<HTMLElement | null>(null);
let logTimer: any = null;
let srvTimer: any = null;
const route = useRoute();
let offInstall: any = null;
let offSelInst: any = null;

const selCfg = computed(() => instances.value.find((i) => i.id === selectedId.value) || null);
const selState = computed(() => (selectedId.value ? states[selectedId.value] : null));

const instForm = reactive<InstConfig>({
    id: "", name: "", version: "", port: 3000, docRoot: "", spa: true, directory: false, openBrowser: false,
});
const instVisible = ref(false);

const versionsVisible = ref(false);
const localVersions = ref<any[]>([]);
const remoteVersions = ref<any[]>([]);
const remoteLoading = ref(false);
const installProgress = reactive<Record<string, any>>({});
const installLogs = ref<string[]>([]);
const localDirInput = ref("");
const pluginsDirVal = ref("");
const scanning = ref(false);

const cfgVisible = ref(false);
const configItems = ref<any[]>([]);
const commonRegistries = ref<any[]>([]);
const registryVal = ref("");
const registryInput = ref("");
const newKey = ref("");
const newVal = ref("");
const npmrcText = ref("");
const userNpmrcPath = ref("");

const pkgsVisible = ref(false);
const pkgMgrId = ref("");
const managers = ref<any[]>([]);
const mgrLoading = ref(false);
const packages = ref<any[]>([]);
const pkgsLoading = ref(false);
const pkgInstallName = ref("");
const pkgBusy = ref("");
const pkgFilter = ref("");

const versionOptions = computed(() => localVersions.value.map((v: any) => ({ label: `${v.version}（${srcLabel(v.source)}）`, value: v.version })));
const installedSet = computed(() => new Set<string>(localVersions.value.map((v: any) => v.version)));

/* 包管理器：同一个名字可能有多份（每个 Node 自带一份 npm），靠 id 区分 */
const mgrOptions = computed(() => managers.value
    .filter((m: any) => m.name !== "corepack")
    .map((m: any) => ({
        label: `${m.name}${m.version ? " " + m.version : ""}${m.kind === "node" && m.nodeVer ? "（Node " + m.nodeVer + "）" : ""}`,
        value: m.id,
    })));
const curMgr = computed(() => managers.value.find((m: any) => m.id === pkgMgrId.value) || null);
const mgrHint = computed(() => {
    const m = curMgr.value;
    if (!m) return managers.value.length ? "" : "未发现本机包管理器，先到「Node.js 版本管理」扫描本机 Node";
    const roots = (m.roots || []).length ? (m.roots || []).join("、") : "（尚无全局安装目录，装过包后才有）";
    return `当前：${m.name} ${m.version || ""}　入口 ${m.path}　扫描目录 ${roots}`;
});
// 各家安装语法不同，把占位提示写成对应命令，避免用户以为都是 npm i -g
const installHint = computed(() => {
    const n = String(curMgr.value?.name || "npm").toLowerCase();
    const cmd = n === "yarn" ? "yarn global add" : (n === "pnpm" || n === "bun" || n === "cnpm") ? `${n} add -g` : "npm i -g";
    return `要全局安装的包名（${cmd}），如 axios`;
});
const filteredPackages = computed(() => {
    const q = pkgFilter.value.trim().toLowerCase();
    if (!q) return packages.value;
    return packages.value.filter((p: any) => String(p.name || "").toLowerCase().includes(q));
});

/* ============================ 小工具 ============================ */
function srcLabel(s: string): string {
    return s === "bundled" ? "内置" : s === "downloaded" ? "下载" : "本机";
}
function mgrColor(name: string): string {
    return ({ npm: "red", yarn: "blue", pnpm: "orange", bun: "purple", cnpm: "green", corepack: "gray" } as Record<string, string>)[name] || "gray";
}
function stageText(p: any): string {
    if (!p) return "";
    if (p.stage === "downloading") return `下载中 ${p.percent || 0}%`;
    if (p.stage === "extracting") return "解压中";
    if (p.stage === "done") return "完成";
    if (p.stage === "info") return "执行中";
    return "失败";
}
function fmtBytes(b?: number): string {
    if (!b) return "-";
    const units = ["B", "KB", "MB", "GB"]; let i = 0; let v = b;
    while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
    return v.toFixed(i > 0 ? 1 : 0) + " " + units[i];
}
function fmtUptimeS(sec?: number): string {
    if (!sec || sec <= 0) return "-";
    const d = Math.floor(sec / 86400); const h2 = Math.floor((sec % 86400) / 3600);
    const m = Math.floor((sec % 3600) / 60);
    if (d > 0) return `${d}d ${h2}h ${m}m`; if (h2 > 0) return `${h2}h ${m}m`; return `${m}m`;
}
function pushInstallLog(txt: string) {
    if (!txt) return;
    const t = new Date();
    const pad = (n: number) => n.toString().padStart(2, "0");
    installLogs.value.push(`[${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}] ${txt}`);
    if (installLogs.value.length > 200) installLogs.value.splice(0, installLogs.value.length - 200);
}
function openSite(port?: number) {
    if (!port) return;
    window.open(`http://127.0.0.1:${port}/`, "_blank");
}

/* ============================ 站点实例 ============================ */
async function loadLocalVersions() {
    try { localVersions.value = (await GoTools.NpmSrvListVersions()) || []; }
    catch (e) { localVersions.value = []; }
}
async function loadInstances() {
    try { instances.value = (await GoTools.NpmSrvListInstances()) || []; }
    catch (e) { instances.value = []; }
    if (!selectedId.value && instances.value.length) selectInst(instances.value[0].id);
    refreshStates();
}
async function refreshStates() {
    for (const inst of instances.value) {
        try {
            states[inst.id] = await GoTools.NpmSrvStatus(inst.id);
            if (states[inst.id]?.registry) registryVal.value = states[inst.id].registry;
        } catch (e) { /* ignore */ }
    }
}
function selectInst(id: string) {
    selectedId.value = id;
    logLines.value = [];
    startLogPolling();
    refreshStates();
}
async function pollLogs() {
    if (!selectedId.value) return;
    try {
        const lines = (await GoTools.NpmSrvLogs(selectedId.value, 300)) || [];
        if (lines.join("\n") !== logLines.value.join("\n")) {
            logLines.value = lines;
            nextTick(() => { if (logBoxRef.value) logBoxRef.value.scrollTop = logBoxRef.value.scrollHeight; });
        }
    } catch (e) { /* ignore */ }
}
function startLogPolling() {
    if (logTimer) clearInterval(logTimer);
    pollLogs();
    logTimer = setInterval(pollLogs, 2000);
}
async function doStart() {
    const c = selCfg.value; if (!c) return;
    try { await GoTools.NpmSrvStart(c.id); Message.success(`站点 ${c.name} 已启动`); }
    catch (e: any) { Message.error("启动失败: " + (e?.message || e)); }
    refreshStates();
}
async function doStop() {
    const c = selCfg.value; if (!c) return;
    try { await GoTools.NpmSrvStop(c.id); Message.success(`站点 ${c.name} 已停止`); }
    catch (e: any) { Message.error("停止失败: " + (e?.message || e)); }
    refreshStates();
}
async function doRestart() {
    const c = selCfg.value; if (!c) return;
    try { await GoTools.NpmSrvRestart(c.id); Message.success(`站点 ${c.name} 已重启`); }
    catch (e: any) { Message.error("重启失败: " + (e?.message || e)); }
    refreshStates();
}
async function onInstAction(v: string, inst: InstConfig) {
    if (v === "start") {
        try { await GoTools.NpmSrvStart(inst.id); Message.success(`站点 ${inst.name} 已启动`); }
        catch (e: any) { Message.error("启动失败: " + (e?.message || e)); }
        refreshStates();
    } else if (v === "stop") {
        try { await GoTools.NpmSrvStop(inst.id); Message.success(`站点 ${inst.name} 已停止`); }
        catch (e: any) { Message.error("停止失败: " + (e?.message || e)); }
        refreshStates();
    } else if (v === "restart") {
        try { await GoTools.NpmSrvRestart(inst.id); Message.success(`站点 ${inst.name} 已重启`); }
        catch (e: any) { Message.error("重启失败: " + (e?.message || e)); }
        refreshStates();
    } else if (v === "open") {
        openSite(inst.port);
    } else if (v === "logs") {
        selectInst(inst.id);
    } else if (v === "edit") {
        openInstModal(inst);
    } else if (v === "del") {
        Modal.warning({
            title: "删除站点", content: `确定删除站点「${inst.name}」？运行中会先停止；站点目录保留。`, hideCancel: false,
            onOk: async () => {
                try {
                    await GoTools.NpmSrvRemoveInstance(inst.id);
                    delete states[inst.id];
                    if (selectedId.value === inst.id) selectedId.value = "";
                    await loadInstances();
                } catch (e: any) { Message.error("删除失败: " + (e?.message || e)); }
            },
        });
    }
}
function openInstModal(inst?: InstConfig) {
    if (inst) { Object.assign(instForm, { ...inst }); }
    else {
        Object.assign(instForm, { id: "", name: "", version: localVersions.value[0]?.version || "", port: 3000, docRoot: "", spa: true, directory: false, openBrowser: false });
    }
    if (localVersions.value.length === 0) loadLocalVersions();
    instVisible.value = true;
}
async function checkPortTaken() {
    if (!instForm.port) return;
    try {
        const suggest = await GoTools.NpmSrvSuggestPort(instForm.port);
        if (suggest && suggest !== instForm.port) Message.warning(`端口 ${instForm.port} 已被占用，建议改为 ${suggest}`);
    } catch (e) { /* ignore */ }
}
async function chooseDocRoot() {
    try { const dir = await GoTools.NpmSrvChooseDir(); if (dir) instForm.docRoot = dir; }
    catch (e) { /* cancel */ }
}
async function saveInstance(): Promise<boolean> {
    if (!instForm.name) { Message.warning("请填写站点名称"); return false; }
    if (!instForm.version) { Message.warning("请选择 Node.js 版本"); return false; }
    try {
        await GoTools.NpmSrvSaveInstance({ ...instForm });
        Message.success("已保存"); await loadInstances(); return true;
    } catch (e: any) { Message.error("保存失败: " + (e?.message || e)); return false; }
}

/* ============================ Node 版本管理 ============================ */
async function openVersionsModal() {
    versionsVisible.value = true;
    loadLocalVersions();
    if (!pluginsDirVal.value) {
        try { pluginsDirVal.value = (await GoTools.NpmSrvPluginsDir()) || ""; } catch (e) { /* ignore */ }
    }
    if (remoteVersions.value.length === 0) loadRemoteVersions();
}
async function loadRemoteVersions() {
    remoteLoading.value = true;
    try { remoteVersions.value = (await GoTools.NpmSrvRemoteVersions()) || []; }
    catch (e: any) { Message.error("获取远程版本失败: " + (e?.message || e)); }
    remoteLoading.value = false;
}
async function installVersion(r: any) {
    try {
        pushInstallLog(`提交安装请求：Node.js ${r.version}`);
        await GoTools.NpmSrvInstall(r.version);
        installProgress[r.version] = { stage: "downloading", percent: 0 };
    } catch (e: any) {
        pushInstallLog(`安装请求被拒绝：${e?.message || e}`);
        Message.error("安装失败: " + (e?.message || e));
    }
}
function removeVersion(v: any) {
    Modal.warning({
        title: "删除版本", content: `确定删除 Node.js ${v.version}？`, hideCancel: false,
        onOk: async () => {
            try { await GoTools.NpmSrvRemoveVersion(v.version); Message.success("已删除"); loadLocalVersions(); }
            catch (e: any) { Message.error("删除失败: " + (e?.message || e)); }
        },
    });
}
async function chooseLocalDir() {
    try { const dir = await GoTools.NpmSrvChooseDir(); if (dir) localDirInput.value = dir; }
    catch (e) { /* cancel */ }
}
async function addLocalDir() {
    const dir = localDirInput.value.trim();
    if (!dir) { Message.warning("请先选择或填写目录"); return; }
    try { await GoTools.NpmSrvAddLocal(dir); Message.success("已添加本机目录"); localDirInput.value = ""; loadLocalVersions(); }
    catch (e: any) { Message.error("添加失败: " + (e?.message || e)); }
}

// 扫描本机已有 Node（PATH / 注册表 / nvm、fnm、volta 版本仓库）并登记为「本机」来源
async function scanLocalNodes() {
    scanning.value = true;
    try {
        const added = (await GoTools.NpmSrvScanLocal()) || [];
        const list = Array.isArray(added) ? added : (added?.result1 || []);
        await loadLocalVersions();
        if (list.length) {
            const detail = list.map((v: any) => `${v.version}（${v.path}）`).join("；");
            Message.success(`新登记 ${list.length} 个本机 Node：${list.map((v: any) => v.version).join("、")}`);
            pushInstallLog(`扫描本机 Node：新登记 ${list.length} 个 - ${detail}`);
        } else {
            Message.info("扫描完成，未发现新的本机 Node.js");
        }
    }
    catch (e: any) { Message.error("扫描失败: " + (e?.message || e)); }
    finally { scanning.value = false; }
}

/* ============================ npm 配置 ============================ */
async function openNpmConfig() {
    cfgVisible.value = true;
    loadConfigItems();
    loadNpmrcText();
    try { commonRegistries.value = (await GoTools.NpmCommonRegistries()) || []; } catch (e) { /* ignore */ }
    try { registryVal.value = (await GoTools.NpmGetRegistry()) || ""; } catch (e) { /* ignore */ }
    registryInput.value = registryVal.value;
    try {
        const res = await GoTools.NpmUserNpmrc();
        // bindings 多返回值：可能是数组或对象，兼容取第一个
        const p = Array.isArray(res) ? res[0] : (res?.path || res?.result1 || "");
        userNpmrcPath.value = p || "";
    } catch (e) { /* ignore */ }
}
async function loadConfigItems() {
    try { configItems.value = ((await GoTools.NpmConfigItems()) || []).map((x: any) => ({ ...x })); }
    catch (e: any) { Message.error("读取 npm 配置失败: " + (e?.message || e)); }
}
async function loadNpmrcText() {
    try { npmrcText.value = (await GoTools.NpmGetConfigText()) || ""; } catch (e) { npmrcText.value = ""; }
}
async function saveNpmrcText() {
    try { await GoTools.NpmSaveConfigText(npmrcText.value); Message.success("托管 .npmrc 已保存"); loadConfigItems(); }
    catch (e: any) { Message.error("保存失败: " + (e?.message || e)); }
}
async function setItem(rec: any) {
    if (!rec.key) return;
    try {
        await GoTools.NpmSetConfigItem(rec.key, rec.value);
        if (rec.key === "registry") registryVal.value = rec.value;
    } catch (e: any) { Message.error("设置失败: " + (e?.message || e)); }
}
async function addConfigItem() {
    const k = newKey.value.trim();
    if (!k) { Message.warning("请填写 Key"); return; }
    await setItem({ key: k, value: newVal.value.trim() });
    newKey.value = ""; newVal.value = "";
    loadConfigItems();
}
async function applyRegistry(url: string) {
    url = String(url || "").trim();
    if (!url) return;
    try {
        await GoTools.NpmSetRegistry(url);
        registryVal.value = url;
        Message.success("registry 已切换：" + url);
        loadConfigItems();
    } catch (e: any) { Message.error("切换失败: " + (e?.message || e)); }
}
async function applyToUser() {
    Modal.warning({
        title: "同步到用户全局", hideCancel: false,
        content: "将托管 .npmrc 的 registry 等配置写入用户全局 ~/.npmrc（写入前自动备份原文件）。确定继续？",
        onOk: async () => {
            try {
                const bak = await GoTools.NpmApplyToUserNpmrc();
                Message.success("已同步" + (bak ? "，原文件备份至 " + bak : ""));
            } catch (e: any) { Message.error("同步失败: " + (e?.message || e)); }
        },
    });
}

/* ============================ 全局包管理 ============================ */
async function openPkgs() {
    pkgsVisible.value = true;
    await loadLocalVersions();   // 管理器依附于 Node，先保证版本列表就绪
    await loadManagers();
    scanPkgs();
}
async function loadManagers() {
    mgrLoading.value = true;
    try { managers.value = (await GoTools.NpmPackageManagers()) || []; }
    catch (e) { managers.value = []; }
    if (!managers.value.some((m: any) => m.id === pkgMgrId.value)) {
        // 默认选 npm（多份 npm 时取版本最高的那份），没有 npm 就退而求其次
        pkgMgrId.value = managers.value.find((m: any) => m.name === "npm")?.id
            || managers.value[0]?.id || "";
    }
    mgrLoading.value = false;
}
// 一次扫完全部管理器的全局安装根（直读磁盘 package.json，不起子进程）
async function scanPkgs() {
    pkgsLoading.value = true;
    try { packages.value = (await GoTools.NpmScanGlobalPackages()) || []; }
    catch (e: any) {
        packages.value = [];
        Message.error("扫描全局包失败: " + String(e?.message || e).slice(0, 200));
    }
    pkgsLoading.value = false;
}
async function installPkg() {
    const pkg = pkgInstallName.value.trim();
    if (!pkg) { Message.warning("请填写包名"); return; }
    if (!pkgMgrId.value) { Message.warning("请选择包管理器"); return; }
    try {
        pkgBusy.value = `${curMgr.value?.name || ""} 全局安装 ${pkg}`;
        await GoTools.NpmInstallWith(pkgMgrId.value, pkg);
        pkgInstallName.value = "";
    } catch (e: any) {
        pkgBusy.value = "";
        Message.error("安装失败: " + (e?.message || e));
    }
}
function uninstallPkg(rec: any) {
    Modal.warning({
        title: "卸载全局包", hideCancel: false,
        content: `确定卸载 ${rec.name}@${rec.version}（由 ${rec.manager} 装在 ${rec.path}）？`,
        onOk: async () => {
            try {
                pkgBusy.value = `卸载 ${rec.name}`;
                // 传包的实际目录，后端据此反查当初装它的管理器，避免多份 npm 卸错
                await GoTools.NpmUninstallWith(pkgMgrId.value, rec.name, rec.path);
            } catch (e: any) {
                pkgBusy.value = "";
                Message.error("卸载失败: " + (e?.message || e));
            }
        },
    });
}

/* ============================ npm 本体版本切换（命令行生效链） ============================ */
const cliVisible = ref(false);
const chain = ref<any>(null);
const chainLoading = ref(false);
const cliEntryDir = ref("");
const cliLayer = ref("override");
const releases = ref<any[]>([]);
const relLoading = ref(false);
const relNote = ref("");
const relForNode = ref("");
const cliVersion = ref("");
const onlyCompat = ref(true);
const cliBusy = ref("");
const cliProgress = ref<any>(null);
const cliLogs = ref<string[]>([]);
let offNpmVer: any = null;

const chainEntries = computed<any[]>(() => chain.value?.entries || []);
const chainHints = computed<string[]>(() => chain.value?.hints || []);
const chainShadowed = computed<string[]>(() => chain.value?.shadowed || []);
const selEntry = computed(() => chainEntries.value.find((e: any) => e.dir === cliEntryDir.value) || null);
const selLayer = computed(() => (selEntry.value ? selEntry.value[cliLayer.value === "override" ? "override" : "bundled"] : null));
const selNodeVer = computed(() => selEntry.value?.version || chain.value?.activeVersion || "");
const selLayerNow = computed(() => {
    const l = selLayer.value;
    if (!l?.exists) return "该层还不存在，切到这里等于新装一份";
    return `${l.version}${l.engines ? "　engines " + l.engines : ""}`;
});
const selLayerDefault = computed(() => (cliLayer.value === "override" ? selEntry.value?.overrideDefault : selEntry.value?.bundledDefault) || "");
const canRestoreDefault = computed(() => !!selLayerDefault.value && selLayerDefault.value !== selLayer.value?.version);
const releaseOptions = computed(() => {
    const cur = selLayer.value?.version || "";
    const list = onlyCompat.value ? releases.value.filter((r: any) => r.compatible) : releases.value;
    return list.map((r: any) => {
        const marks: string[] = [];
        if (r.tag) marks.push(r.tag);
        if (r.version === cur) marks.push("当前");
        if (!r.compatible) marks.push("需 Node " + (r.engines || "?"));
        return { label: r.version + (marks.length ? `（${marks.join(" · ")}）` : ""), value: r.version };
    });
});
const layerNote = computed(() => cliLayer.value === "override"
    ? "写进 npm prefix -g 的目录：Node 的 npm.cmd 每次执行都先跳去这里，所以切它不需要管理员、不用改 PATH，已经开着的 cmd 下一条命令就生效。多个 Node 共用同一个 prefix 时会一起变。"
    : "改这个 Node 目录里的 node_modules\\npm：只有这个 Node 会变，但工具里用它的地方（含静态站点服务）会跟着变；文件被跑着的 npm/node 占用时会切不动。");

function layerName(k?: string): string {
    return k === "override" ? "覆盖层" : k === "bundled" ? "自带层" : "无 npm";
}
function pushCliLog(txt: string) {
    if (!txt) return;
    const t = new Date();
    const pad = (n: number) => n.toString().padStart(2, "0");
    cliLogs.value.push(`[${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}] ${txt}`);
    if (cliLogs.value.length > 200) cliLogs.value.splice(0, cliLogs.value.length - 200);
}

async function openCliChain() {
    cliVisible.value = true;
    await loadChain();
    ensureReleases();
}
// 每次打开都重新体检：PATH、prefix 都可能被外部改动，缓存在这里没意义
async function loadChain() {
    chainLoading.value = true;
    try { chain.value = await GoTools.NpmCliChain(); }
    catch (e: any) {
        chain.value = null;
        Message.error("生效链体检失败: " + String(e?.message || e).slice(0, 220));
    }
    chainLoading.value = false;
    const es = chainEntries.value;
    if (!es.some((e: any) => e.dir === cliEntryDir.value)) {
        cliEntryDir.value = es.find((e: any) => e.dir === chain.value?.activeDir)?.dir || es[0]?.dir || "";
        applyLayerDefault();
    }
}
// 默认落在覆盖层 —— 用户要的是「cmd 里的 npm 变」，而且它不碰 Node 本体
function applyLayerDefault() {
    const e = selEntry.value;
    if (!e) return;
    cliLayer.value = e.override?.dir ? "override" : "bundled";
    cliVersion.value = "";
    pickDefaultVersion();
}
function pickEntryRow(record: any) {
    if (!record?.dir || record.dir === cliEntryDir.value) return;
    cliEntryDir.value = record.dir;
    applyLayerDefault();
    ensureReleases();
}
async function ensureReleases(force = false) {
    const nv = selNodeVer.value;
    if (!force && relForNode.value === nv && releases.value.length) { pickDefaultVersion(); return; }
    relLoading.value = true;
    try {
        const r: any = force ? await GoTools.NpmCliRefreshReleases(nv) : await GoTools.NpmCliReleases(nv);
        releases.value = r?.releases || [];
        relNote.value = r?.note || "";
        relForNode.value = nv;
        pickDefaultVersion();
    } catch (e: any) {
        releases.value = [];
        Message.error("获取 npm 版本清单失败: " + String(e?.message || e).slice(0, 220));
    }
    relLoading.value = false;
}
// 预选最新可用版，但不选成当前这份了（不然一键就白做）
function pickDefaultVersion() {
    const opts = releaseOptions.value;
    if (cliVersion.value && opts.some((o: any) => o.value === cliVersion.value)) return;
    const cur = selLayer.value?.version || "";
    cliVersion.value = opts.find((o: any) => o.value !== cur)?.value || opts[0]?.value || "";
}
function doSwitch() {
    const e = selEntry.value, l = selLayer.value;
    if (!e || !l?.dir) { Message.warning("先选一个 npm 入口与目标层"); return; }
    const v = cliVersion.value.trim();
    if (!v) { Message.warning("选择要切到的 npm 版本"); return; }
    if (l.exists && l.version === v) { Message.info(`该层已经是 npm ${v}`); return; }
    const bad = releases.value.find((r: any) => r.version === v && !r.compatible);
    Modal.warning({
        title: cliLayer.value === "override" ? "往覆盖层装 npm" : "改这个 Node 自带的 npm",
        hideCancel: false,
        content: `${layerName(cliLayer.value)}：npm ${l.exists ? l.version : "（尚无）"} → ${v}　｜　目录 ${l.dir}`
            + (bad ? `　｜　注意：${bad.why}，装完会实测 npm -v，跑不通自动回滚` : ""),
        onOk: async () => {
            try {
                cliBusy.value = `npm@${v}`;
                cliProgress.value = { stage: "downloading", percent: 0 };
                pushCliLog(`提交切换：${layerName(cliLayer.value)} → npm ${v}`);
                await GoTools.NpmCliSwitch(cliLayer.value, cliEntryDir.value, v);
            } catch (err: any) {
                cliBusy.value = "";
                cliProgress.value = null;
                Message.error("切换失败: " + String(err?.message || err).slice(0, 220));
            }
        },
    });
}
function doUndo() {
    const l = selLayer.value;
    if (!l?.dir) return;
    Modal.warning({
        title: "撤销上一步切换", hideCancel: false,
        content: `把${layerName(cliLayer.value)}的 npm 换回切换前留存的旧版本（目录 ${l.dir}）。`,
        onOk: async () => {
            try {
                cliBusy.value = `npm@undo`;
                cliProgress.value = { stage: "info", percent: 0 };
                pushCliLog(`提交撤销：${layerName(cliLayer.value)}`);
                await GoTools.NpmCliUndoSwitch(cliLayer.value, cliEntryDir.value);
            } catch (e: any) {
                cliBusy.value = "";
                cliProgress.value = null;
                Message.error("撤销失败: " + String(e?.message || e).slice(0, 220));
            }
        },
    });
}
function doRemoveOverride() {
    const e = selEntry.value;
    if (!e?.override?.dir) return;
    Modal.warning({
        title: "移除覆盖层", hideCancel: false,
        content: `删除 ${e.override.dir} 里的 npm（连带清掉工具生成的 npm / npx 转发壳），cmd 回到该 Node 自带的 npm ${e.bundled?.exists ? e.bundled.version : "（也没有）"}。`,
        onOk: async () => {
            try {
                cliBusy.value = "npm@override";
                cliProgress.value = { stage: "info", percent: 0 };
                pushCliLog("提交移除覆盖层");
                await GoTools.NpmCliRemoveOverride(cliEntryDir.value);
            } catch (err: any) {
                cliBusy.value = "";
                cliProgress.value = null;
                Message.error("移除失败: " + String(err?.message || err).slice(0, 220));
            }
        },
    });
}

/* ============================ 生命周期 ============================ */
onMounted(async () => {
    await loadInstances();
    // 主页点击服务实例进入（?inst=）：选中该站点展示详情
    const wantId = String(route.query.inst ?? "");
    if (wantId && instances.value.some((i) => i.id === wantId)) selectInst(wantId);
    loadLocalVersions();
    srvTimer = setInterval(refreshStates, 3000);
    // 单例窗口已打开时，主页再次点击其他实例 → 通过事件切换
    offSelInst = Events.On("tool:select-inst", (ev: any) => {
        const p = ev?.data ?? ev;
        if (p?.type !== "npm" || !p?.id) return;
        if (instances.value.some((i) => i.id === String(p.id))) selectInst(String(p.id));
    });
    offInstall = Events.On("npm:install", (ev: any) => {
        const p = ev?.data ?? ev;
        if (!p || !p.version) return;
        if (p.stage === "info") {
            pushInstallLog(p.note || "");
        } else if (p.stage === "done") {
            delete installProgress[p.version];
            pkgBusy.value = "";
            pushInstallLog(`完成：${p.note || p.version}`);
            Message.success(`${p.version} 任务完成`);
            loadLocalVersions();
            if (pkgsVisible.value) scanPkgs();
        } else if (p.stage === "error") {
            if (installProgress[p.version]) installProgress[p.version] = p;
            pkgBusy.value = "";
            pushInstallLog(`失败 ${p.version}：${p.note || "未知错误"}`);
            Modal.error({ title: "任务失败", content: `${p.version}：${p.note || "未知错误"}` });
        } else {
            installProgress[p.version] = p;
        }
    });
    offNpmVer = Events.On("npm:npmver", async (ev: any) => {
        const p = ev?.data ?? ev;
        if (!p || !p.version) return;
        if (p.stage === "info") {
            pushCliLog(p.note || "");
        } else if (p.stage === "done") {
            cliBusy.value = "";
            cliProgress.value = null;
            pushCliLog(`完成：${p.note || p.version}`);
            Message.success(`npm 已切到 ${p.version.replace("npm@", "")}`);
            await loadChain();
            if (cliVersion.value === selLayer.value?.version) cliVersion.value = "";
            pickDefaultVersion();
            // 面板里的 npm 版本、包管理器列表、全局包归属全部跟着变
            loadLocalVersions();
            if (pkgsVisible.value) { loadManagers(); scanPkgs(); }
        } else if (p.stage === "error") {
            cliBusy.value = "";
            cliProgress.value = null;
            pushCliLog(`失败：${p.note || "未知错误"}`);
            // 失败可能只回了一半（自动回滚也试过），重新体检一次拿真实现场
            await loadChain();
            Modal.error({ title: "npm 切换失败", content: String(p.note || "未知错误").slice(0, 400) });
        } else {
            cliProgress.value = p;
        }
    });
});
onUnmounted(() => {
    if (offInstall) offInstall();
    if (offSelInst) offSelInst();
    if (offNpmVer) offNpmVer();
    if (srvTimer) clearInterval(srvTimer);
    if (logTimer) clearInterval(logTimer);
});
</script>

<style scoped>
.npm-page {
    height: 100%;
    min-height: 0;
    flex-wrap: nowrap;
    background: var(--color-fill-1);
}

.side-col,
.main-col {
    height: 100%;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
}

.side-card {
    display: flex;
    flex-direction: column;
}

.side-card:first-child {
    flex: 1;
    min-height: 0;
}

.side-card:first-child :deep(.arco-card-body) {
    flex: 1;
    overflow: auto;
}

.tools-card {
    flex-shrink: 0;
}

.toolbox {
    display: flex;
    flex-direction: column;
    gap: 8px;
}

.reg-hint {
    margin-top: 8px;
    font-size: 12px;
    color: var(--color-text-3);
    word-break: break-all;
}

.main-panel {
    height: 100%;
    display: flex;
    flex-direction: column;
}

.main-panel :deep(.arco-card-body) {
    flex: 1;
    overflow: auto;
    display: flex;
    flex-direction: column;
}

.empty-card :deep(.arco-card-body) {
    align-items: center;
    justify-content: center;
}

.srv-head-actions {
    display: flex;
    gap: 6px;
}

.conn-name {
    max-width: 130px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.conn-more {
    cursor: pointer;
    color: var(--color-text-3);
}

.conn-empty {
    color: var(--color-text-3);
    font-size: 12px;
    padding: 6px 0;
}

.inst-active {
    background: var(--color-primary-light-1);
    cursor: pointer;
}

:deep(.arco-list-item) {
    cursor: pointer;
}

.detail-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
    gap: 6px 16px;
    padding: 10px 14px;
    border-bottom: 1px solid var(--color-border-2);
}

.ls-item {
    display: flex;
    justify-content: space-between;
    font-size: 12px;
    gap: 8px;
}

.ls-item span {
    color: var(--color-text-3);
    flex-shrink: 0;
}

.ls-item b {
    font-weight: 500;
    text-align: right;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.ls-item b.ok {
    color: rgb(var(--green-6));
}

.ls-item b.no {
    color: var(--color-text-3);
}

.log-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 14px 4px;
    font-size: 12px;
    color: var(--color-text-3);
}

.ls-log {
    flex: 1;
    min-height: 160px;
    overflow: auto;
    background: var(--color-fill-2);
    margin: 0 14px 12px;
    padding: 8px;
    border-radius: 4px;
    font-family: Consolas, Menlo, monospace;
    font-size: 12px;
}

.ls-log.small {
    max-height: 140px;
    min-height: 60px;
    margin: 0;
}

.ls-line {
    white-space: pre-wrap;
    word-break: break-all;
    line-height: 1.6;
}

.ver-block .ver-title,
.ver-title {
    display: flex;
    align-items: center;
    gap: 8px;
    font-weight: 600;
    font-size: 13px;
    margin-bottom: 8px;
}

.add-local {
    margin-top: 10px;
}

.mgr-label {
    font-size: 12px;
    color: var(--color-text-3);
    text-align: right;
    padding-right: 2px;
}

.bin-dot {
    color: rgb(var(--green-6));
    font-size: 11px;
}

.offline-tip {
    margin-top: 8px;
    font-size: 12px;
    color: var(--color-text-3);
    word-break: break-all;
}

.install-log {
    margin-top: 12px;
}

.cfg-registry {
    margin-bottom: 4px;
}

.user-npmrc {
    margin-top: 12px;
}

.chain-hero {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: 12px;
    padding: 10px 12px;
    border: 1px solid var(--color-border-2);
    border-radius: 6px;
    background: var(--color-fill-1);
}

.chain-hero-l {
    min-width: 0;
    flex: 1;
}

.chain-ver {
    display: flex;
    align-items: baseline;
    gap: 8px;
    flex-wrap: wrap;
}

.chain-cap {
    font-size: 12px;
    color: var(--color-text-3);
}

.chain-big {
    font-size: 22px;
    font-weight: 600;
    font-family: Consolas, Menlo, monospace;
    color: rgb(var(--primary-6));
}

.chain-lines {
    margin-top: 6px;
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-width: 600px;
}

.chain-hints {
    margin-top: 8px;
    display: flex;
    flex-direction: column;
    gap: 4px;
}

.hint-line {
    font-size: 12px;
    color: var(--color-text-2);
    line-height: 1.6;
}

.layer-note {
    margin: 8px 0 4px;
    font-size: 12px;
    line-height: 1.6;
    color: var(--color-text-3);
}

.detail-grid.tight {
    border-bottom: none;
    padding: 4px 0;
    grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
}

:deep(.row-active) {
    background: var(--color-primary-light-1);
}

.cli-prog {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 12px;
}

.cli-name {
    font-size: 12px;
    font-family: Consolas, Menlo, monospace;
    min-width: 96px;
}

.cli-stage {
    font-size: 12px;
    color: var(--color-text-3);
    width: 92px;
    text-align: right;
}
</style>
