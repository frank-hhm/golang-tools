<template>
    <a-row class="npm-page" :gutter="6">
        <!-- 左侧：静态站点实例 + 功能入口 -->
        <a-col :span="6" class="side-col">
            <a-card class="side-card card-head-tool" size="small" :body-style="{ padding: 0 }">
                <template #title>静态站点服务</template>
                <template #extra>
                    <div class="srv-head-actions">
                        <a-button size="small" @click="openInstModal()">新建站点</a-button>
                    </div>
                </template>
                <!-- 引擎没起时先把话说明白：否则要等点「启动」撞一条管道错误才知道是 Docker 没跑 -->
                <common-docker-off class="srv-docker-off" :count="dockerInstCount" tool-label="静态站点" />
                <a-list :bordered="false" size="small">
                    <a-list-item v-for="inst in instances" :key="inst.id"
                        :class="{ 'inst-active': selectedId === inst.id }" @click="selectInst(inst.id)">
                        <a-space>
                            <com-badge :status="states[inst.id]?.running ? 'success' : 'normal'"
                                :animated="!!states[inst.id]?.running" />
                            <a-tooltip :content="inst.name + ':' + inst.port">
                                <span class="conn-name">{{ inst.name }}</span>
                            </a-tooltip>
                            <a-tag v-if="inst.mode === 'docker'" color="purple" size="small">{{ inst.image || 'Docker'
                            }}</a-tag>
                            <a-tag v-else color="arcoblue" size="small">node {{ inst.version }}</a-tag>
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
            <a-card class="side-card tools-card card-head-tool" size="small" :body-style="{ padding: '10px' }">
                <template #title>npm 工具箱</template>
                <div class="toolbox">
                    <a-button long class="tool-btn" @click="openNpmConfig()">
                        <template #icon><icon-settings /></template>
                        <span class="tool-btn-t">npm 配置 / 镜像源</span>
                        <icon-right class="tool-btn-r" />
                    </a-button>
                    <a-button long class="tool-btn" @click="openVersionsModal()">
                        <template #icon><icon-storage /></template>
                        <span class="tool-btn-t">Node / npm 版本管理</span>
                        <icon-right class="tool-btn-r" />
                    </a-button>
                    <a-button long class="tool-btn" @click="openPkgs()">
                        <template #icon><icon-storage /></template>
                        <span class="tool-btn-t">全局包管理</span>
                        <icon-right class="tool-btn-r" />
                    </a-button>
                </div>
                <div class="reg-hint">当前 registry：{{ registryVal || '-' }}</div>
            </a-card>
        </a-col>

        <!-- 右侧：环境总览 / 站点详情 -->
        <a-col :span="18" class="main-col">
            <a-card class="main-panel card-head-tool" :body-style="{ padding: 0 }">
                <template #title>
                    <a-radio-group size="medium" v-model="rightMode" type="button">
                        <a-radio value="overview">环境总览</a-radio>
                        <a-radio value="detail" :disabled="!selectedId">站点详情</a-radio>
                    </a-radio-group>
                </template>
                <template #extra>
                    <a-space v-if="rightMode === 'detail' && selCfg">
                        <com-badge :status="selState?.running ? 'success' : 'normal'" :animated="!!selState?.running" />
                        <span>{{ selCfg?.name }}</span>
                        <a-tag v-if="isDockerSel" color="purple" size="small">{{ selState?.image || selCfg?.image ||
                            'Docker' }}</a-tag>
                        <a-tag color="arcoblue" size="small">:{{ selCfg?.port }}</a-tag>
                        <a-tag v-if="selCfg?.spa" size="small">SPA</a-tag>
                        <a-tag v-if="selCfg?.directory" size="small">列目录</a-tag>
                        <a-button size="small" :disabled="selState?.running || stopBusy" @click="doStart">启动</a-button>
                        <!-- 停止会断开正在服务的请求，先气泡确认；按钮转 loading 显示「停止中」 -->
                        <a-popconfirm :content="`停止站点「${selCfg?.name}」？正在处理的请求会中断。`" type="warning"
                            position="lt" :disabled="!selState?.running || stopBusy" @ok="doStop">
                            <a-button size="small" status="danger" :disabled="!selState?.running || stopBusy"
                                :loading="stopBusy">{{ stopBusy ? '停止中' : '停止' }}</a-button>
                        </a-popconfirm>
                        <a-tooltip :content="isDockerSel ? '容器模式：重启 = 重启容器（会短暂中断），改过的 nginx.conf 随之生效' : '停掉内置服务器后重新拉起'">
                            <a-button size="small" :disabled="stopBusy" @click="doRestart">重启</a-button>
                        </a-tooltip>
                        <a-button size="small" @click="openLogModal(selCfg!.id)">
                            <template #icon><icon-face-folder-open /></template>日志
                        </a-button>
                        <a-button size="small" :disabled="!selState?.running"
                            @click="openSite(selCfg?.port)">在新窗口打开</a-button>
                    </a-space>
                    <a-button v-else size="small" :loading="ovLoading" @click="loadOverview()">刷新</a-button>
                </template>

                <!-- 环境总览 -->
                <div v-show="rightMode === 'overview'" class="ov-body">
                    <a-spin :loading="ovLoading" style="width:100%">
                        <a-alert v-if="pathStatus?.systemHasNode" type="warning" show-icon class="ov-alert">
                            检测到系统 PATH 里也有 Node 目录。Windows 组合顺序为「系统 PATH + 用户 PATH」，系统项优先——只绑「用户
                            PATH」会被系统里的 Node 盖过，需选「系统 PATH（管理员）」才能真正生效。
                        </a-alert>

                        <a-row :gutter="8" class="ov-top">
                            <!-- 绑定现状与绑定操作合成一张卡：状态用 descriptions，不再五行散排 -->
                            <a-col :span="12">
                                <a-card class="ov-card" size="small">
                                    <template #title>生效 npm / PATH 绑定</template>
                                    <template #extra>
                                        <!-- PATH 绑定是系统现状，用圆点+文案，与各页运行状态一致 -->
                                        <span class="com-state" :class="pathStatus?.bound ? 'is-on' : 'is-dim'">
                                            <com-badge :status="pathStatus?.bound ? 'success' : 'normal'" />
                                            {{ pathStatus?.bound ? '已绑定' : '未绑定' }}
                                        </span>
                                    </template>
                                    <a-descriptions :column="1" size="small" :data="bindDesc" class="ov-desc" />
                                    <div v-if="pathStatus?.otherNodeDirs?.length" class="ov-others">
                                        <div class="ov-others-t">PATH 中其它 Node 目录（绑定时将移除）</div>
                                        <a-tooltip v-for="(d, i) in pathStatus.otherNodeDirs" :key="i" :content="d">
                                            <div class="ov-other-dir">{{ d }}</div>
                                        </a-tooltip>
                                    </div>
                                    <a-divider :margin="12" />
                                    <div class="ov-bind">
                                        <a-select v-model="bindDir" :options="bindDirOptions"
                                            placeholder="选择要绑定的 Node 目录" allow-search />
                                        <div class="ov-bind-row">
                                            <a-radio-group size="medium" v-model="bindScope" type="button">
                                                <a-radio value="user">用户PATH</a-radio>
                                                <a-radio value="system">系统PATH[管理员]</a-radio>
                                            </a-radio-group>
                                            <a-button size="small" :loading="bindBusy" :disabled="!bindDir"
                                                @click="doBindPath">强制绑定</a-button>
                                            <a-button size="small" v-if="pathStatus?.canUndo" status="warning"
                                                :loading="bindBusy" @click="doUnbindPath">撤销绑定</a-button>
                                        </div>
                                    </div>
                                    <div v-if="pathStatus?.note" class="ov-note">{{ pathStatus.note }}</div>
                                </a-card>
                            </a-col>

                            <!-- 本机 Node 与包管理器：从整屏宽改成右栏上下两张表 -->
                            <a-col :span="12">
                                <a-card class="ov-card" size="small" :body-style="{ padding: 0 }">
                                    <template #title>本机 Node</template>
                                    <template #extra><a-tag size="small">{{ localVersions.length }}</a-tag></template>
                                    <a-table :data="localVersions" size="small" :pagination="false" row-key="path"
                                        :scroll="{ y: 150 }" :columns="[
                                            { title: '版本', dataIndex: 'version', width: 84 },
                                            { title: 'npm', dataIndex: 'npmVer', width: 70 },
                                            { title: '来源', dataIndex: 'source', width: 62, slotName: 'source' },
                                            { title: '路径', dataIndex: 'path', ellipsis: true, tooltip: true },
                                        ]" :bordered="false">
                                        <template #source="{ record }">
                                            <a-tag size="small"
                                                :color="record.source === 'bundled' ? 'purple' : record.source === 'downloaded' ? 'green' : 'orange'">{{
                                                    srcLabel(record.source) }}</a-tag>
                                        </template>
                                    </a-table>
                                </a-card>
                                <a-card class="ov-card mt8" size="small" :body-style="{ padding: 0 }">
                                    <template #title>包管理器</template>
                                    <template #extra><a-tag size="small">{{ managers.length }}</a-tag></template>
                                    <a-table :data="managers" size="small" :pagination="false" row-key="id"
                                        :scroll="{ y: 150 }" :columns="[
                                            { title: '名称', dataIndex: 'name', width: 76, slotName: 'name' },
                                            { title: '版本', dataIndex: 'version', width: 70 },
                                            { title: '类型', dataIndex: 'kind', width: 96, slotName: 'kind' },
                                            { title: '入口', dataIndex: 'path', ellipsis: true, tooltip: true },
                                        ]" :bordered="false">
                                        <template #name="{ record }">
                                            <a-tag size="small" :color="mgrColor(record.name)">{{ record.name }}</a-tag>
                                        </template>
                                        <template #kind="{ record }">
                                            <span>{{ record.kind === 'node' && record.nodeVer ? '随 Node ' +
                                                record.nodeVer : (record.kind === 'global' ? '全局安装' : record.kind ===
                                                    'native' ? '原生' : record.kind) }}</span>
                                        </template>
                                    </a-table>
                                </a-card>
                            </a-col>
                        </a-row>

                        <!-- 全局包：按管理器计数 + registry -->
                        <a-card class="ov-card" size="small" style="margin-top: 8px">
                            <template #title>全局包</template>
                            <template #extra>
                                <a-space :size="8">
                                    <a-tag size="small" color="arcoblue">{{ packages.length }} 个</a-tag>
                                    <a-link size="small" @click="openPkgs()">管理<template
                                            #icon><icon-right /></template></a-link>
                                </a-space>
                            </template>
                            <a-row>
                                <a-col v-for="g in pkgStats" :key="g.title" :span="4">
                                    <a-statistic :value="g.count" :title="g.title"
                                        :value-style="{ fontSize: '20px' }" />
                                </a-col>
                            </a-row>
                            <div class="ov-note">当前 registry：<a-link size="small" copyable :content="registryVal">{{
                                registryVal
                                || '-' }}</a-link></div>
                        </a-card>
                    </a-spin>
                </div>

                <!-- 站点详情 -->
                <div v-show="rightMode === 'detail'" class="detail-body">
                    <a-descriptions :column="2" bordered size="large" class="detail-desc">
                        <a-descriptions-item label="运行模式">
                            {{ selState?.mode === 'docker' ? 'Docker 容器' : '本地进程（内置 Go 服务器）' }}
                        </a-descriptions-item>
                        <a-descriptions-item label="站点名称">{{ selCfg?.name || '-' }}</a-descriptions-item>
                        <a-descriptions-item label="端口">{{ selCfg?.port ?? '-' }}</a-descriptions-item>
                        <a-descriptions-item label="运行状态">
                            <b :class="selState?.running ? 'ok' : 'no'">{{ selState?.running ? '运行中' : '已停止' }}</b>
                        </a-descriptions-item>
                        <!-- 容器里的 nginx 进程在容器 namespace，本机 PID 与 Node 字段都没意义：换成报容器与镜像 -->
                        <a-descriptions-item v-if="isDockerSel" label="容器">
                            <a-tooltip
                                :content="'容器 ID：' + (selState?.containerId || '-') + '（状态 ' + (selState?.containerState || '-') + '）'">
                                <span>{{ selState?.container || selCfg?.containerName || '-' }}</span>
                            </a-tooltip>
                        </a-descriptions-item>
                        <a-descriptions-item v-else label="进程 PID">{{ selState?.pid || '-' }}</a-descriptions-item>
                        <a-descriptions-item label="运行时长">{{ fmtUptimeS(selState?.uptimeSeconds)
                        }}</a-descriptions-item>
                        <a-descriptions-item :label="isDockerSel ? '镜像' : 'Node 版本'">
                            {{ isDockerSel ? (selCfg?.image || selState?.image || '-') : (selState?.version ||
                                selCfg?.version || '-') }}
                        </a-descriptions-item>
                        <a-descriptions-item v-if="!isDockerSel" label="npm 版本">{{ selState?.npmVer || '-' }}</a-descriptions-item>
                        <a-descriptions-item>
                            <template #label>
                                <span>累计请求<a-tooltip v-if="isDockerSel"
                                        content="容器里没有内置服务器的 /__gonfo__ 探针：请求数与流量按挂载到宿主机的 access.log 统计">
                                        <icon-question-circle class="desc-tip" /></a-tooltip></span>
                            </template>
                            {{ selState?.requests ?? '-' }}
                        </a-descriptions-item>
                        <a-descriptions-item label="req/s">{{ selState?.requestsPerSec ?
                            selState.requestsPerSec.toFixed(2) :
                            '-' }}</a-descriptions-item>
                        <a-descriptions-item label="累计发送">{{ fmtBytes(selState?.bytes) }}</a-descriptions-item>
                        <a-descriptions-item label="站点文件数">{{ selState?.files ?? '-' }}</a-descriptions-item>
                        <a-descriptions-item label="SPA 模式">{{ selCfg?.spa ? '开启' : '关闭' }}</a-descriptions-item>
                        <a-descriptions-item label="列目录">{{ selCfg?.directory ? '允许' : '禁止' }}</a-descriptions-item>
                        <a-descriptions-item label="自动开浏览器">{{ selCfg?.openBrowser ? '是' : '否' }}</a-descriptions-item>
                        <a-descriptions-item label="访问日志">
                            <a-link size="small" @click="openLogModal(selCfg!.id)">
                                <template #icon><icon-face-folder-open /></template>查看实时日志
                            </a-link>
                        </a-descriptions-item>
                        <a-descriptions-item v-if="!isDockerSel" label="registry" :span="2">{{ selState?.registry || '-'
                        }}</a-descriptions-item>
                        <a-descriptions-item label="站点目录" :span="2">{{ selState?.docRoot || selCfg?.docRoot || '-'
                        }}</a-descriptions-item>
                        <a-descriptions-item v-if="!isDockerSel" label="node.exe" :span="2">{{ selState?.nodePath || '-'
                        }}</a-descriptions-item>
                        <a-descriptions-item v-else label="容器内挂载" :span="2">
                            站点 → /usr/share/nginx/html · 配置 → /go-tools/conf/nginx.conf · 日志 → /var/log/nginx
                        </a-descriptions-item>
                    </a-descriptions>
                </div>
            </a-card>
        </a-col>
    </a-row>

    <!-- 新建/编辑站点实例 -->
    <a-modal v-model:visible="instVisible" :title="instForm.id ? '编辑站点' : '新建站点'" :width="520"
        :body-style="{ maxHeight: '72vh' }" @before-ok="saveInstance">
        <!-- 引擎没起时先把原因说清楚，否则要到启动那一步才报错；吸顶不占表单位 -->
        <common-modal-tip v-if="instForm.mode === 'docker' && dockerErr" :text="dockerErr">
          <a-button size="small" @click="openDockerWindow">打开 Docker 面板</a-button>
        </common-modal-tip>
        <a-form :model="instForm" layout="vertical" class="inst-form">
            <a-form-item field="name" label="站点名称" required>
                <a-input v-model="instForm.name" placeholder="如 docs-site" />
            </a-form-item>
            <a-form-item field="mode" label="运行模式">
                <a-radio-group size="medium" v-model="instForm.mode" type="button" @change="onModeChange">
                    <a-radio value="local"><icon-computer /> 本地</a-radio>
                    <a-radio value="docker"><icon-experiment /> Docker</a-radio>
                </a-radio-group>
                <template #extra>
                    <template v-if="instForm.mode === 'docker'">
                        容器里跑 nginx：站点目录挂到 /usr/share/nginx/html，conf 与 logs 一并挂载；改「SPA /
                        列目录」后点重启即生效（不重建容器），首次启动会自动拉镜像
                    </template>
                    <template v-else>内置 Go 静态服务器，随本程序一起启停；Node 版本用于展示与配套 npm</template>
                </template>
            </a-form-item>
            <!-- 说明文字全部移到 label 右侧的图标 tooltip 里，label 只留短名：表单不被长括号挤成两行 -->
            <!-- 两种模式的行数与列宽对齐（Docker 只把站点根目录分一半给容器名），切模式时弹窗高度不跳 -->
            <a-row :gutter="12">
                <a-col :span="12">
                    <a-form-item v-if="instForm.mode !== 'docker'" field="version" label="Node.js 版本" required
                        tooltip="用于展示与配套 npm；实际跑站点的是这个版本自带的 node.exe">
                        <a-select v-model="instForm.version" placeholder="选择已安装版本" :options="versionOptions" />
                    </a-form-item>
                    <a-form-item v-else class="img-item" field="image" label="镜像" required>
                        <div class="img-row">
                            <a-select v-model="instForm.image" class="img-select" placeholder="选择已拉取镜像" allow-create
                                filterable :loading="dockerImagesLoading">
                                <!-- 自闭合的 a-option 没有文字，下拉里会全是空白行 —— 显式写上文字 -->
                                <a-option v-for="im in imageOptions" :key="im" :value="im">{{ im }}</a-option>
                                <template #empty>
                                    <a-empty :image="false" description="无可用镜像" />
                                </template>
                            </a-select>
                            <!-- 配镜像时随手打开 Docker 管理窗口（拉取 / 看进度） -->
                            <a-tooltip content="打开 Docker 管理窗口：拉镜像、看拉取进度">
                                <a-button size="small" class="img-open" @click="openDockerWindow">
                                    <template #icon><icon-storage /></template>
                                </a-button>
                            </a-tooltip>
                        </div>
                        <template #help>
                            <span v-if="dockerImagesLoading">正在读取本地镜像…</span>
                            <span v-else-if="!imageOptions.length && !instForm.image" class="img-none">
                                本地无可用镜像，<a-link @click="openDockerWindow">点击去安装</a-link>
                            </span>
                        </template>
                    </a-form-item>
                </a-col>
                <a-col :span="12">
                    <a-form-item field="port" label="端口" :error="portError"
                        :tooltip="instForm.mode === 'docker' ? '宿主机发布端口（只绑 127.0.0.1）；容器内固定监听 80，改这里不动配置文件' : '内置服务器监听的端口'">
                        <a-input-number v-model="instForm.port" :min="1" :max="65535" @change="portError = ''"
                            @blur="checkPortTaken" />
                    </a-form-item>
                </a-col>
            </a-row>
            <a-row :gutter="12">
                <a-col :span="instForm.mode === 'docker' ? 12 : 24">
                    <a-form-item field="docRoot" label="站点根目录"
                        :tooltip="instForm.mode === 'docker' ? '留空 = 实例目录下的 www/；该目录挂载进容器，Docker Desktop 需能访问所在磁盘（Settings → Resources → File sharing）' : '留空 = 实例目录下的 www/'">
                        <a-input-search v-model="instForm.docRoot" placeholder="如 D:\www\dist" search-button
                            button-text="选择目录" @click-btn="chooseDocRoot" />
                    </a-form-item>
                </a-col>
                <a-col v-if="instForm.mode === 'docker'" :span="12">
                    <a-form-item field="containerName" label="容器名" tooltip="留空自动生成 go-tools-npm-实例ID">
                        <a-input v-model="instForm.containerName" placeholder="留空自动生成" />
                    </a-form-item>
                </a-col>
            </a-row>
            <a-row :gutter="12">
                <a-col :span="8">
                    <a-form-item field="spa" label="SPA 模式" tooltip="找不到文件时回退到 index.html">
                        <a-switch v-model="instForm.spa" />
                    </a-form-item>
                </a-col>
                <a-col :span="8">
                    <a-form-item field="directory" label="允许列目录" tooltip="无 index.html 时返回目录列表">
                        <a-switch v-model="instForm.directory" />
                    </a-form-item>
                </a-col>
                <a-col :span="8">
                    <a-form-item field="openBrowser" label="自动开浏览器" tooltip="启动成功后自动在浏览器打开站点">
                        <a-switch v-model="instForm.openBrowser" />
                    </a-form-item>
                </a-col>
            </a-row>
        </a-form>
    </a-modal>

    <!-- npm 配置 -->
    <a-modal v-model:visible="cfgVisible" title="npm 配置（托管 .npmrc）" :width="720" :footer="false" unmount-on-close>
        <div class="cfg-registry">
            <div class="ver-title">镜像源快捷切换</div>
            <a-space wrap>
                <a-button v-for="r in commonRegistries" :key="r.value" size="small"
                    :type="registryVal === r.value ? 'primary' : 'outline'" @click="applyRegistry(r.value)">
                    {{ r.key }}
                </a-button>
            </a-space>
            <a-input-search v-model="registryInput" placeholder="自定义 registry URL" style="margin-top:8px;width:420px"
                search-button button-text="应用" @search="applyRegistry(registryInput)" />
        </div>
        <a-divider style="margin: 12px 0" />
        <div class="ver-title">
            配置项
            <a-button size="small" @click="loadConfigItems">刷新</a-button>
        </div>
        <a-table :data="configItems" size="small" :pagination="false" row-key="key" :columns="[
            { title: 'Key', dataIndex: 'key', width: 220 },
            { title: 'Value', dataIndex: 'value', slotName: 'value' },
        ]">
            <template #value="{ record }">
                <a-input v-model="record.value" @press-enter="setItem(record)" @blur="setItem(record)" />
            </template>
        </a-table>
        <a-row class="add-local" :gutter="8">
            <a-col :span="8"><a-input v-model="newKey" placeholder="新增 Key" /></a-col>
            <a-col :span="12"><a-input v-model="newVal" placeholder="Value" /></a-col>
            <a-col :span="4"><a-button size="small" block @click="addConfigItem">新增</a-button></a-col>
        </a-row>
        <a-divider style="margin: 12px 0" />
        <a-collapse>
            <a-collapse-item header=".npmrc 原文编辑" key="raw">
                <a-textarea v-model="npmrcText" :auto-size="{ minRows: 8, maxRows: 14 }" spellcheck="false" />
                <a-space style="margin-top:8px">
                    <a-button size="small" @click="loadNpmrcText">重新读取</a-button>
                    <a-button size="small" @click="saveNpmrcText">保存 .npmrc</a-button>
                </a-space>
            </a-collapse-item>
        </a-collapse>
        <div class="user-npmrc">
            <div class="ver-title">用户全局 ~/.npmrc</div>
            <div class="offline-tip">{{ userNpmrcPath || '（未创建）' }}
                <a-button size="small" style="margin-left:8px" @click="applyToUser">把托管配置同步过去（自动备份）</a-button>
            </div>
        </div>
    </a-modal>

    <!-- 全局包管理（多包管理器）：固定高度避免切包管理器时弹窗跟着缩，但封顶到视口内 -->
    <a-modal v-model:visible="pkgsVisible" title="全局包管理" :width="860" :footer="false" unmount-on-close
        :body-style="{ height: 'min(600px, calc(100vh - 200px))', overflow: 'auto' }">
        <div class="mgr-hint">
            <template v-if="mgrHintLines">
                <div v-for="(h, i) in mgrHintLines" :key="i" class="mgr-hint-row">
                    <span class="mgr-hint-label">{{ h.label }}</span>
                    <a-tooltip :content="h.value">
                        <span class="mgr-hint-val">{{ h.value }}</span>
                    </a-tooltip>
                </div>
            </template>
            <div v-else class="mgr-hint-empty">{{ mgrHint }}</div>
        </div>
        <a-row :gutter="8" align="center" style="margin-bottom:6px">
            <a-col :span="10">
                <a-space>
                    <div class="mgr-label">装/卸用</div>
                    <a-select v-model="pkgMgrId" :options="mgrOptions" size="small" placeholder="包管理器"
                        :loading="mgrLoading" allow-search />
                </a-space>
            </a-col>
            <a-col :span="10">
                <a-input v-model="pkgInstallName" size="small" :placeholder="installHint" @press-enter="installPkg" />
            </a-col>
            <a-col :span="4">
                <a-space>
                    <a-button size="small" block :disabled="!!pkgBusy" @click="installPkg">安装</a-button>
                    <a-button size="small" block :loading="pkgsLoading" @click="scanPkgs">扫描</a-button>
                </a-space>
            </a-col>
        </a-row>
        <a-input v-model="pkgFilter" size="small" placeholder="按包名过滤" allow-clear
            style="width:220px;margin-bottom:8px" />
        <a-spin :loading="pkgsLoading" style="width:100%">
            <a-table :data="filteredPackages" size="small" :pagination="false" row-key="path" :scroll="{ x: '100%', y: 300 }"
                :columns="[
                    { title: '包名', dataIndex: 'name', width: 200, ellipsis: true, tooltip: true },
                    { title: '版本', dataIndex: 'version', width: 96 },
                    { title: '管理器', dataIndex: 'manager', width: 84, slotName: 'manager' },
                    { title: 'CLI', dataIndex: 'hasBin', width: 56, slotName: 'bin' },
                    { title: '说明', dataIndex: 'description', ellipsis: true, tooltip: true },
                    { title: '操作', dataIndex: 'ops', width: 100, slotName: 'ops', fixed: 'right', align: 'right' },
                ]">
                <template #manager="{ record }">
                    <a-tag size="small" :color="mgrColor(record.manager)">{{ record.manager }}</a-tag>
                </template>
                <template #bin="{ record }">
                    <com-badge v-if="record.hasBin" status="success" />
                </template>
                <template #ops="{ record }">
                    <a-tooltip v-if="record.builtin" content="随 Node 发行，不可单独卸载">
                        <a-button size="small" disabled>发行件</a-button>
                    </a-tooltip>
                    <a-popconfirm v-else :content="`确定卸载 ${record.name}@${record.version}？这个包由 ${record.manager} 安装。`"
                        type="warning" position="lt" :disabled="!!pkgBusy" @ok="uninstallPkg(record)">
                        <a-button size="small" status="danger" :disabled="!!pkgBusy"
                            :loading="pkgBusy === `卸载 ${record.name}`">卸载</a-button>
                    </a-popconfirm>
                </template>
                <template #empty>
                    <div v-if="!pkgsLoading && filteredPackages.length === 0" class="conn-empty">
                        {{ packages.length ? '没有匹配过滤的包' : '未扫到全局包（各管理器的全局安装根都是空的）' }}
                    </div>
                </template>
            </a-table>
        </a-spin>
        <div v-if="pkgBusy" class="offline-tip">{{ pkgBusy }} …（进度见任务日志）</div>
        <div v-if="installLogs.length" class="ver-block install-log">
            <div class="ver-title">任务日志</div>
            <div class="ls-log small">
                <div v-for="(l, i) in installLogs" :key="i" class="ls-line">{{ l }}</div>
            </div>
        </div>
    </a-modal>

    <!-- Node / npm 版本管理：原先工具箱里两个长得像“版本切换”的入口，合成一个弹窗的两个页签：
         「Node 版本」管 Node 本体的下载/注册/删除，「npm 生效链」管 cmd 里 npm -v 实际跑的那一份 -->
    <a-modal v-model:visible="versionsVisible" title="Node / Npm 版本管理" :width="900" :footer="false" unmount-on-close
        :body-style="{ height: 'min(620px, calc(100vh - 200px))', overflow: 'auto' }">
        <div class="ver-switch">
            <a-radio-group size="medium" v-model="verTab" type="button" @change="onVerTabChange">
                <a-radio value="node">Node 版本</a-radio>
                <a-radio value="chain">npm 生效链（命令行切换）</a-radio>
                <a-radio value="docker">Docker 镜像 / 容器</a-radio>
            </a-radio-group>
        </div>
        <div v-show="verTab === 'node'" class="ver-pane">
            <div class="ver-block">
                <div class="ver-title">
                    本地可用版本
                    <a-button size="small" :loading="scanning" @click="scanLocalNodes">扫描本机 Node</a-button>
                    <a-button size="small" @click="loadLocalVersions">刷新</a-button>
                </div>
                <a-table :data="localVersions" size="small" :pagination="false" row-key="version" :scroll="{ x: '100%' }"
                    :columns="[
                    { title: '版本', dataIndex: 'version', width: 110 },
                    { title: 'npm', dataIndex: 'npmVer', width: 90 },
                    { title: '来源', dataIndex: 'source', width: 90, slotName: 'source' },
                    { title: '路径', dataIndex: 'path', ellipsis: true, tooltip: true },
                    { title: '操作', dataIndex: 'ops', width: 90, slotName: 'ops', fixed: 'right', align: 'right' },
                ]">
                    <template #source="{ record }">
                        <a-tag size="small"
                            :color="record.source === 'bundled' ? 'purple' : record.source === 'downloaded' ? 'green' : 'orange'">{{
                                srcLabel(record.source) }}</a-tag>
                    </template>
                    <template #ops="{ record }">
                        <a-popconfirm :content="`确定删除 Node.js ${record.version}？若仍有站点引用该版本会被拒绝。`"
                            type="warning" position="lt" :disabled="record.source === 'bundled'"
                            @ok="removeVersion(record)">
                            <a-button size="small" status="danger" :disabled="record.source === 'bundled'">删除</a-button>
                        </a-popconfirm>
                    </template>
                </a-table>
                <a-row class="add-local" :gutter="8">
                    <a-col :span="16">
                        <a-input v-model="localDirInput" placeholder="注册本机已有 Node.js：填写 node.exe 所在目录" />
                    </a-col>
                    <a-col :span="4">
                        <a-button size="small" block @click="chooseLocalDir">浏览…</a-button>
                    </a-col>
                    <a-col :span="4">
                        <a-button size="small" block @click="addLocalDir">添加</a-button>
                    </a-col>
                </a-row>
            </div>
            <a-divider style="margin: 12px 0" />
            <div class="ver-block">
                <div class="ver-title">
                    线上版本
                    <a-button size="small" :loading="remoteLoading" @click="loadRemoteVersions">刷新</a-button>
                </div>
                <a-spin :loading="remoteLoading" style="width:100%">
                    <a-table :data="remoteVersions" size="small" :pagination="{ pageSize: 8 }" row-key="version"
                        :scroll="{ x: '100%' }"
                        :columns="[
                            { title: '版本', dataIndex: 'version', width: 110 },
                            { title: 'LTS', dataIndex: 'lts', width: 90, slotName: 'lts' },
                            { title: '包大小', dataIndex: 'size', width: 100, slotName: 'size' },
                            { title: '状态', dataIndex: 'st', width: 150, slotName: 'st' },
                            { title: '操作', dataIndex: 'ops', width: 100, slotName: 'ops', fixed: 'right', align: 'right' },
                        ]">
                        <template #lts="{ record }">
                            <a-tag v-if="record.lts" color="green" size="small">{{ record.lts }}</a-tag>
                        </template>
                        <template #size="{ record }">{{ fmtBytes(record.size) }}</template>
                        <template #st="{ record }">
                            <span v-if="installedSet.has(record.version)" class="com-state is-on">
                                <com-badge status="success" />已安装
                            </span>
                            <span v-else-if="installProgress[record.version]" class="com-state"
                                :class="installProgress[record.version].stage === 'error' ? 'is-off' : 'is-busy'">
                                <com-badge
                                    :status="installProgress[record.version].stage === 'error' ? 'danger' : 'processing'"
                                    :animated="installProgress[record.version].stage !== 'error'" />
                                {{ stageText(installProgress[record.version]) }}
                            </span>
                            <span v-else class="com-state is-dim">
                                <com-badge status="normal" />未安装
                            </span>
                        </template>
                        <template #ops="{ record }">
                            <a-button v-if="!installedSet.has(record.version)" size="small"
                                :disabled="!!installProgress[record.version]"
                                @click="installVersion(record)">安装</a-button>
                            <a-button v-else size="small" @click="loadLocalVersions">已安装</a-button>
                        </template>
                    </a-table>
                </a-spin>
                <div class="offline-tip">离线安装：将 node zip 放入 <a-link copyable :content="pluginsDirVal">{{
                    pluginsDirVal
                    || '…' }}</a-link> 后刷新本地版本</div>
            </div>
            <div v-if="installLogs.length" class="ver-block install-log">
                <div class="ver-title">任务日志</div>
                <div class="ls-log small">
                    <div v-for="(l, i) in installLogs" :key="i" class="ls-line">{{ l }}</div>
                </div>
            </div>
        </div>

        <div v-show="verTab === 'chain'" class="ver-pane">
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
                            <div class="ls-item"><span>转发壳</span>
                                <a-tooltip :content="chain?.activeShim || '-'" :disabled="!chain?.activeShim">
                                    <b class="ellipsis">{{ chain?.activeShim || '-' }}</b>
                                </a-tooltip>
                            </div>
                            <div class="ls-item"><span>node.exe</span>
                                <a-tooltip :content="chain?.activeNode || '-'" :disabled="!chain?.activeNode">
                                    <b class="ellipsis">{{ chain?.activeNode || '-' }}</b>
                                </a-tooltip>
                            </div>
                            <div class="ls-item"><span>全局 prefix</span>
                                <a-tooltip :content="chain?.activePrefix || '-'" :disabled="!chain?.activePrefix">
                                    <b class="ellipsis">{{ chain?.activePrefix || '-' }}</b>
                                </a-tooltip>
                            </div>
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
                    <template #bun="{ record }">{{ record.bundled?.exists ? record.bundled.version : '-'
                    }}</template>
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
                    没扫到带 npm 的目录，先到「Node 版本」页签扫描本机 Node
                </div>

                <template v-if="selEntry">
                    <div class="ver-title" style="margin-top:14px">切换目标</div>
                    <a-radio-group v-model="cliLayer" type="button" size="medium">
                        <a-radio value="override">全局 prefix 覆盖层</a-radio>
                        <a-radio value="bundled">Node 自带层</a-radio>
                    </a-radio-group>
                    <div class="layer-note">{{ layerNote }}</div>
                    <div class="detail-grid tight">
                        <div class="ls-item"><span>写入目录</span>
                            <a-tooltip :content="selLayer?.dir || '-'" :disabled="!selLayer?.dir">
                                <b class="ellipsis">{{ selLayer?.dir || '-' }}</b>
                            </a-tooltip>
                        </div>
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
                                <a-button size="small" :disabled="!!cliBusy" @click="doSwitch()">切换</a-button>
                                <a-button size="small" :disabled="!selLayer?.hasPrev || !!cliBusy"
                                    @click="doUndo()">撤销</a-button>
                                <a-popconfirm v-if="cliLayer === 'override' && selEntry.override?.exists"
                                    content="移除覆盖层并恢复该 Node 自带的 npm？工具生成的 npm/npx 转发壳会一并清掉。"
                                    type="warning" position="lt" :disabled="!!cliBusy" @ok="doRemoveOverride()">
                                    <a-button size="small" status="danger" :disabled="!!cliBusy"
                                        :loading="cliBusy === 'npm@override'">移除覆盖层</a-button>
                                </a-popconfirm>
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
                        <a-button size="small" @click="cliLogs = []">清空</a-button>
                    </div>
                    <div class="ls-log small">
                        <div v-for="(l, i) in cliLogs" :key="i" class="ls-line">{{ l }}</div>
                    </div>
                </div>
            </a-spin>
        </div>

        <!-- Docker 页签：静态站点的容器模式用 nginx 镜像承载，镜像 tag 就是它的「版本」 -->
        <div v-show="verTab === 'docker'" class="ver-pane">
            <docker-images keyword="nginx" tool-label="静态站点" repo="nginx" :presets="nginxImagePresets"
                container-tool="npm" :instances="instances" :states="states" @create="newInstanceFromImage"
                @use="editInstanceById" @changed="refreshStates" />
            <div class="layer-note">
                静态站点的 Docker 模式不跑 node.exe：站点目录挂到容器 /usr/share/nginx/html，SPA / 列目录开关写进容器
                nginx.conf，改完点「重启」即生效（不重建容器）。
            </div>
        </div>
    </a-modal>

    <!-- 站点访问日志（实时）：点击「日志」弹出，独立滚动区 -->
    <a-modal v-model:visible="logVisible" :title="`访问日志 · ${selCfg?.name || ''}`" :width="760" :footer="false"
        unmount-on-close @close="stopLogPolling">
        <div class="log-modal-head">
            <com-badge :status="selState?.running ? 'success' : 'normal'" :animated="!!selState?.running" />
            <span>{{ selState?.running ? '运行中' : '已停止' }}</span>
            <a-tag color="arcoblue" size="small">:{{ selCfg?.port }}</a-tag>
            <!-- 容器模式的日志来自挂载到宿主机的 access.log，来源与本地不一样，先标清楚 -->
            <a-tag v-if="isDockerSel" size="small" color="purple">容器 access.log</a-tag>
            <a-button size="small" style="margin-left:auto" @click="logLines = []">
                <template #icon><icon-delete /></template>清空
            </a-button>
        </div>
        <div ref="logBoxRef" class="ls-log modal">
            <div v-for="(l, i) in logLines" :key="i" class="ls-line">{{ l }}</div>
            <div v-if="logLines.length === 0" class="conn-empty">暂无日志</div>
        </div>
    </a-modal>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, nextTick, onMounted, onUnmounted, reactive, ref } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import { useRoute } from "vue-router";
import { useToolWindow } from "@/views/index/useToolWindow";
import { restoreInstalls, runningTasks } from "@/store/modules/tasks";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

interface InstConfig {
    id: string; name: string; version: string; port: number; docRoot: string; spa: boolean; directory: boolean; openBrowser: boolean;
    // mode: local | docker；容器模式不跑 node.exe，靠镜像里的 nginx 承载站点
    mode: string; image: string; containerName: string;
}

/* ============================ 状态 ============================ */
const instances = ref<InstConfig[]>([]);
const states = reactive<Record<string, any>>({});
const selectedId = ref("");
const logLines = ref<string[]>([]);
const logBoxRef = ref<HTMLElement | null>(null);
const logVisible = ref(false);
let logTimer: any = null;
let srvTimer: any = null;
const route = useRoute();
let offInstall: any = null;
let offSelInst: any = null;

const selCfg = computed(() => instances.value.find((i) => i.id === selectedId.value) || null);
const selState = computed(() => (selectedId.value ? states[selectedId.value] : null));
/** 当前站点是不是容器模式：配置优先（列表里就能看到刚改完还没启动的站点），状态兜底 */
const isDockerSel = computed(() => (selCfg.value?.mode || selState.value?.mode) === "docker");

const instForm = reactive<InstConfig>({
    id: "", name: "", version: "", port: 3000, docRoot: "", spa: true, directory: false, openBrowser: false,
    mode: "local", image: "", containerName: "",
});
const instVisible = ref(false);
const portError = ref("");

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

/* 环境总览 / 站点详情模式切换 + PATH 绑定 */
const rightMode = ref<"overview" | "detail">("overview");
const ovLoading = ref(false);
const pathStatus = ref<any>(null);
const bindDir = ref("");
const bindBusy = ref(false);
const bindScope = ref<"user" | "system">("user");

const versionOptions = computed(() => localVersions.value.map((v: any) => ({ label: `${v.version}（${srcLabel(v.source)}）`, value: v.version })));
const installedSet = computed(() => new Set<string>(localVersions.value.map((v: any) => v.version)));

/* Docker 模式表单：镜像候选只列本机已有的 nginx 系镜像（不猜预置 tag），为空就引导去拉 */
/* 名字不能回退成 dockerImages：script setup 下 kebab 标签 docker-images 会优先拿同名 setup 变量当组件，整个 Docker 区会渲染成空白 */
const dockerImageList = ref<string[]>([]);
const dockerImagesLoading = ref(false);
const dockerErr = ref("");
const imageOptions = computed(() => Array.from(new Set<string>(dockerImageList.value)));

const { openToolWindow } = useToolWindow();

/** 站点里 Docker 模式的实例数：大于 0 时页面才需要挂 Docker 引擎告示 */
const dockerInstCount = computed(() => instances.value.filter((i) => i.mode === "docker").length);

/* 包管理器：同一个名字可能有多份（每个 Node 自带一份 npm），靠 id 区分 */
const mgrOptions = computed(() => managers.value
    .filter((m: any) => m.name !== "corepack")
    .map((m: any) => ({
        label: `${m.name}${m.version ? " " + m.version : ""}${m.kind === "node" && m.nodeVer ? "（Node " + m.nodeVer + "）" : ""}`,
        value: m.id,
    })));
const curMgr = computed(() => managers.value.find((m: any) => m.id === pkgMgrId.value) || null);
const mgrHint = computed(() => managers.value.length ? "" : "未发现本机包管理器，先到「Node / npm 版本管理」扫描本机 Node");
// 把“当前管理器 / 入口 / 扫描目录”拆成多行结构化展示，避免 <br/> 在文本里被转义
const mgrHintLines = computed(() => {
    const m = curMgr.value;
    if (!m) return null;
    const roots = (m.roots || []) as string[];
    return [
        { label: "当前管理器", value: `${m.name} ${m.version || ""}`.trim() },
        { label: "入口", value: m.path || "-" },
        { label: "扫描目录", value: roots.length ? roots.join("、") : "（尚无全局安装目录，装过包后才有）" },
    ];
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
    // 并发问：实例串行 await 就是 N 次往返，Web 模式跑在远程服务器上时尤其明显
    await Promise.all(instances.value.map(async (inst) => {
        try {
            states[inst.id] = await GoTools.NpmSrvStatus(inst.id);
            if (states[inst.id]?.registry) registryVal.value = states[inst.id].registry;
        } catch (e) { /* 单个实例失败不影响其它实例 */ }
    }));
}
function selectInst(id: string) {
    selectedId.value = id;
    rightMode.value = "detail";
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
function stopLogPolling() {
    if (logTimer) { clearInterval(logTimer); logTimer = null; }
}
// 点击「日志」：确保选中该站点后打开日志弹窗并开启轮询（关闭时 stopLogPolling）
function openLogModal(id?: string) {
    const target = id || selectedId.value;
    if (!target) { Message.warning("请先选择站点"); return; }
    if (target !== selectedId.value) { selectedId.value = target; rightMode.value = "detail"; }
    logLines.value = [];
    logVisible.value = true;
    startLogPolling();
}
async function doStart() {
    const c = selCfg.value; if (!c) return;
    if (stopBusy.value) return;
    // 容器模式首次启动可能要拉镜像，先说一声否则像卡住了
    if (c.mode === "docker") Message.info("正在启动容器（首次会自动拉镜像，可能几十秒）");
    try { await GoTools.NpmSrvStart(c.id); Message.success(`站点 ${c.name} 已启动`); }
    catch (e: any) { Message.error("启动失败: " + (e?.message || e)); }
    refreshStates();
}
/** 停止中：停止要等内置服务器/容器退出，期间锁定按钮防连点 */
const stopBusy = ref(false);
async function doStop() {
    const c = selCfg.value; if (!c) return;
    if (stopBusy.value) return;
    stopBusy.value = true;
    try { await GoTools.NpmSrvStop(c.id); Message.success(`站点 ${c.name} 已停止`); }
    catch (e: any) { Message.error("停止失败: " + (e?.message || e)); }
    finally { stopBusy.value = false; }
    refreshStates();
}
async function doRestart() {
    const c = selCfg.value; if (!c) return;
    try {
        await GoTools.NpmSrvRestart(c.id);
        Message.success(c.mode === "docker" ? `站点 ${c.name} 容器已重启，改过的 nginx.conf 随之生效` : `站点 ${c.name} 已重启`);
    }
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
        try {
            await GoTools.NpmSrvRestart(inst.id);
            Message.success(inst.mode === "docker" ? `站点 ${inst.name} 容器已重启，改过的 nginx.conf 随之生效` : `站点 ${inst.name} 已重启`);
        }
        catch (e: any) { Message.error("重启失败: " + (e?.message || e)); }
        refreshStates();
    } else if (v === "open") {
        openSite(inst.port);
    } else if (v === "logs") {
        openLogModal(inst.id);
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
async function openInstModal(inst?: InstConfig) {
    portError.value = "";
    dockerErr.value = "";
    if (inst) {
        // 历史配置没 mode/image/containerName 字段，归一化后再进表单，避免 radio 两档都没选中
        Object.assign(instForm, {
            ...inst,
            mode: inst.mode === "docker" ? "docker" : "local",
            image: inst.image || "",
            containerName: inst.containerName || "",
        });
    }
    else {
        // 默认端口也要避开已被其它站点登记 / 已监听的端口
        let port = 3000;
        try { port = (await GoTools.NpmSrvSuggestPort(3000)) || 3000; } catch (e) { /* ignore */ }
        Object.assign(instForm, { id: "", name: "", version: localVersions.value[0]?.version || "", port, docRoot: "", spa: true, directory: false, openBrowser: false, mode: "local", image: "", containerName: "" });
    }
    if (localVersions.value.length === 0) loadLocalVersions();
    if (instForm.mode === "docker") onModeChange("docker");
    instVisible.value = true;
}

/** 切换运行模式：Docker 读本机镜像候选 + 引擎状态，不干预已有输入 */
async function onModeChange(v: any) {
    const mode = String(v ?? instForm.mode ?? "local");
    instForm.mode = mode;
    if (mode !== "docker") return;
    loadImageOptions();
    loadDockerEngine();
}

async function loadImageOptions() {
    dockerImagesLoading.value = true;
    try { dockerImageList.value = (await GoTools.NpmSrvDockerImages()) || []; }
    catch (e) { dockerImageList.value = []; } // 引擎不可用时当作没有镜像，走“去安装”引导
    dockerImagesLoading.value = false;
    // 还没选值且有候选时先填第一个（默认 nginx:alpine），省一步点击
    if (instForm.mode === "docker" && !instForm.image && dockerImageList.value.length) instForm.image = dockerImageList.value[0];
}

async function loadDockerEngine() {
    try {
        const [ok, msg] = await GoTools.NpmSrvDockerAvailable();
        dockerErr.value = ok ? "" : (msg || "Docker 引擎不可用");
    } catch (e: any) { dockerErr.value = e?.message || String(e); }
}

/** 打开 Docker 管理窗口（独立 webview，单例复用）：配镜像时直接过去拉，不用回主页找入口 */
function openDockerWindow() {
    openToolWindow("/tools/docker", "Docker管理", { single: true, width: 1200, height: 800 });
}

/** 版本管理 Docker 页签点「新建实例」：直接以该镜像开一个容器模式的站点 */
async function newInstanceFromImage(image: string) {
    versionsVisible.value = false;
    await openInstModal();
    instForm.mode = "docker";
    instForm.image = image;
    onModeChange("docker");
}
/** 版本管理里点实例名：关掉版本弹窗去编辑该实例（换镜像 = 换版本） */
function editInstanceById(id: string) {
    const inst = instances.value.find((i) => i.id === id);
    if (!inst) return;
    versionsVisible.value = false;
    openInstModal(inst);
}
async function checkPortTaken() {
    if (!instForm.port) { portError.value = ""; return; }
    try {
        const reason = await GoTools.NpmSrvPortTaken(instForm.port, instForm.id || "");
        if (reason) {
            const suggest = await GoTools.NpmSrvSuggestPort(instForm.port);
            portError.value = suggest && suggest !== instForm.port ? `${reason}，可用：${suggest}` : reason;
        } else {
            portError.value = "";
        }
    } catch (e) { /* ignore */ }
}
async function chooseDocRoot() {
    try { const dir = await GoTools.NpmSrvChooseDir(); if (dir) instForm.docRoot = dir; }
    catch (e) { /* cancel */ }
}
async function saveInstance(): Promise<boolean> {
    if (!instForm.name) { Message.warning("请填写站点名称"); return false; }
    // 两种模式的必填项不一样：本地看本机版本库，容器看镜像（后端也会再校一次）
    if (instForm.mode === "docker") {
        if (!instForm.image) { Message.warning("Docker 模式请选择或输入镜像，如 nginx:alpine"); return false; }
    } else if (!instForm.version) { Message.warning("请选择 Node.js 版本"); return false; }
    try {
        const reason = await GoTools.NpmSrvPortTaken(instForm.port, instForm.id || "");
        if (reason) { portError.value = reason; Message.error(reason); return false; }
        await GoTools.NpmSrvSaveInstance({ ...instForm });
        Message.success(instForm.mode === "docker"
            ? "已保存；容器里的 nginx 监听 80，改这里只改宿主端口；SPA / 列目录开关在下次重启时写回 nginx.conf"
            : "已保存");
        await loadInstances(); return true;
    } catch (e: any) { Message.error("保存失败: " + (e?.message || e)); return false; }
}

/* ============================ Node 版本管理 ============================ */
/* Node 本体与 npm 生效链合在一个弹窗里，工具箱只留一个入口 */
const verTab = ref<"node" | "chain" | "docker">("node");
/** 静态站点 Docker 模式的常用镜像：alpine 体积小，与 nginx 面板同一套候选 */
const nginxImagePresets = ["nginx:alpine", "nginx:stable-alpine", "nginx:1.27-alpine", "nginx:1.26-alpine", "nginx:alpine-slim", "nginx:1.27"];

async function openVersionsModal() {
    versionsVisible.value = true;
    verTab.value = "node";
    loadLocalVersions();
    if (!pluginsDirVal.value) {
        try { pluginsDirVal.value = (await GoTools.NpmSrvPluginsDir()) || ""; } catch (e) { /* ignore */ }
    }
    if (remoteVersions.value.length === 0) loadRemoteVersions();
}
/** 切到生效链页签才体检一次：PATH、prefix 都可能被外部改动，但开框不必抢这几毫秒 */
function onVerTabChange(k: string | number | boolean) {
    if (k !== "chain") return;
    // 总览页已经拉过一次生效链时不重拉；版本清单自身带缓存，直接调安全
    if (!chain.value) void loadChain();
    ensureReleases();
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
async function removeVersion(v: any) {
    try { await GoTools.NpmSrvRemoveVersion(v.version); Message.success("已删除"); loadLocalVersions(); }
    catch (e: any) { Message.error("删除失败: " + (e?.message || e)); }
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
/** 卸载全局包：气泡确认后直接下发，pkgBusy 用作行内 loading 文案 */
async function uninstallPkg(rec: any) {
    try {
        pkgBusy.value = `卸载 ${rec.name}`;
        // 传包的实际目录，后端据此反查当初装它的管理器，避免多份 npm 卸错
        await GoTools.NpmUninstallWith(pkgMgrId.value, rec.name, rec.path);
    } catch (e: any) {
        pkgBusy.value = "";
        Message.error("卸载失败: " + (e?.message || e));
    }
}

/* ============================ npm 本体版本切换（命令行生效链） ============================ */
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

/* 环境总览：绑定候选来自生效链里含 node.exe 的入口目录；全局包按管理器计数 */
const bindDirOptions = computed(() => chainEntries.value
    .filter((e: any) => e.hasNode)
    .map((e: any) => ({
        label: `node ${e.version || "?"}（npm ${e.effective || e.bundled?.version || "-"}）　${e.dir}`,
        value: e.dir,
    })));
const pkgByManager = computed(() => {
    const m: Record<string, number> = {};
    for (const p of packages.value) m[p.manager] = (m[p.manager] || 0) + 1;
    return Object.entries(m).map(([manager, count]) => ({ manager, count })).sort((a, b) => b.count - a.count);
});
/* 绑定现状：交给 a-descriptions 渲染，数组顺序即展示顺序 */
const bindDesc = computed(() => ([
    { label: '生效作用域', value: scopeLabel(pathStatus.value?.boundScope) },
    { label: '绑定 Node', value: pathStatus.value?.boundVersion || '-' },
    { label: '生效 npm', value: pathStatus.value?.boundNpm || '-' },
    { label: '绑定目录', value: pathStatus.value?.boundDir || '-' },
]));
/* 全局包计数：总数打头，后面按管理器分组 */
const pkgStats = computed(() => [
    { title: '总数', count: packages.value.length },
    ...pkgByManager.value.map((g: any) => ({ title: g.manager, count: g.count })),
]);
async function loadOverview() {
    ovLoading.value = true;
    try {
        const [ch, ps, reg] = await Promise.all([
            GoTools.NpmCliChain().catch(() => null),
            GoTools.NpmPathStatus().catch(() => null),
            GoTools.NpmGetRegistry().catch(() => ""),
        ]);
        chain.value = ch;
        pathStatus.value = ps;
        if (reg) registryVal.value = reg;
        await Promise.all([loadLocalVersions(), loadManagers(), scanPkgs()]);
    } finally {
        ovLoading.value = false;
    }
    if (!bindDirOptions.value.some((o: any) => o.value === bindDir.value)) bindDir.value = "";
}
function scopeLabel(s?: string): string {
    return s === "system" ? "系统 PATH" : s === "user" ? "用户 PATH" : "-";
}
function doBindPath() {
    const dir = bindDir.value;
    if (!dir) { Message.warning("请选择要绑定的 Node 目录"); return; }
    const sys = bindScope.value === "system";
    Modal.warning({
        title: sys ? "强制绑定到系统 PATH（需管理员）" : "强制绑定到用户 PATH", hideCancel: false, okText: "绑定",
        content: sys
            ? "将把选定 Node 目录前置到「系统 PATH」并移除系统 PATH 中其它 Node 目录，使所有用户的 cmd 里 npm 都命中它。会弹出 UAC 管理员授权；首次操作自动备份，可一键撤销。新开终端生效。确定继续？"
            : "将把选定 Node 目录前置到「当前用户 PATH」并移除用户 PATH 中其它 Node 目录（免管理员）。注意：若系统 PATH 里也有 Node，系统项会盖过用户绑定——那种情况请改选「系统 PATH（管理员）」。首次操作自动备份，可撤销。新开终端生效。确定继续？",
        onOk: async () => {
            bindBusy.value = true;
            try { await GoTools.NpmPathBind(dir, sys); Message.success("已绑定，新开终端生效"); }
            catch (e: any) { Message.error("绑定失败: " + (e?.message || e)); }
            bindBusy.value = false;
            await loadOverview();
        },
    });
}
function doUnbindPath() {
    Modal.warning({
        title: "撤销 PATH 绑定", hideCancel: false,
        content: "将恢复首次绑定前备份的用户 PATH 并删除备份。新开终端生效。确定继续？",
        onOk: async () => {
            bindBusy.value = true;
            try { const msg = await GoTools.NpmPathUnbind(); Message.success(msg || "已撤销"); }
            catch (e: any) { Message.error("撤销失败: " + (e?.message || e)); }
            bindBusy.value = false;
            await loadOverview();
        },
    });
}
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

// 每次切到生效链页签都重新体检：PATH、prefix 都可能被外部改动，缓存在这里没意义
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
async function doRemoveOverride() {
    const e = selEntry.value;
    if (!e?.override?.dir) return;
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
}

/* ============================ 生命周期 ============================ */
onMounted(async () => {
    await loadInstances();
    // 主页点击服务实例进入（?inst=）：选中该站点展示详情
    const wantId = String(route.query.inst ?? "");
    if (wantId && instances.value.some((i) => i.id === wantId)) selectInst(wantId);
    loadLocalVersions();
    loadOverview();
    // 离开页面时还在装/还在切版本：从任务台账把页面自己的进度补回来
    restoreInstalls("npm", "install", installProgress, pushInstallLog);
    runningTasks("npm", "npmver").then((list) => {
        const t = list[0];
        if (!t) return;
        cliBusy.value = t.name;
        cliProgress.value = { stage: t.stage, percent: t.percent, note: t.note };
        pushCliLog(`接管未完成的切换：${t.title}（${t.percent}%）`);
    });
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
            if (rightMode.value === "overview") loadOverview();
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

.toolbox :deep(.tool-btn) {
    justify-content: flex-start;
}

.tool-btn-t {
    text-align: left;
}

.tool-btn-r {
    margin-left: auto;
    color: var(--color-text-3);
    font-size: 12px;
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

/* 版本管理弹窗：a-radio-group 按钮式页签切换替代 a-tabs */
.ver-switch {
    margin-bottom: 12px;
}

.ver-pane {
    min-height: 0;
}

.mgr-label {
    font-size: 12px;
    color: var(--color-text-3);
    text-align: right;
    padding-right: 2px;
}

.offline-tip {
    margin-top: 8px;
    font-size: 12px;
    color: var(--color-text-3);
    word-break: break-all;
}

/* 全局包管理：当前管理器提示（多行结构化） */
.mgr-hint {
    background: var(--color-fill-2);
    border: 1px solid var(--color-border-2);
    border-radius: 4px;
    padding: 8px 10px;
    margin-bottom: 10px;
    font-size: 12px;
}

.mgr-hint-row {
    display: flex;
    align-items: baseline;
    gap: 8px;
    line-height: 1.9;
}

.mgr-hint-label {
    flex-shrink: 0;
    width: 68px;
    color: var(--color-text-3);
}

.mgr-hint-val {
    flex: 1;
    min-width: 0;
    color: var(--color-text-1);
    font-family: Consolas, Menlo, monospace;
    word-break: break-all;
}

.mgr-hint-empty {
    color: var(--color-text-3);
}

/* 日志弹窗内的实时日志区：固定高度、内部滚动（短窗口下随视口收缩） */
.ls-log.modal {
    height: min(420px, calc(100vh - 260px));
    min-height: 0;
    margin: 0;
}

.log-modal-head {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 8px;
    font-size: 13px;
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

/* 环境总览 */
.ov-body {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 12px 14px;
}

.detail-body {
    flex: 1;
    min-height: 0;
    overflow: auto;
    display: flex;
    flex-direction: column;
}

.detail-desc {
    margin: 12px 14px 4px;
    flex-shrink: 0;
}

.detail-desc :deep(.arco-descriptions-item-label) {
    color: var(--color-text-3);
    width: 92px;
    white-space: nowrap;
}

.detail-desc :deep(.arco-descriptions-item-value) {
    word-break: break-all;
}

.detail-desc b.ok {
    color: rgb(var(--green-6));
    font-weight: 600;
}

.detail-desc b.no {
    color: var(--color-text-3);
    font-weight: 600;
}

/* 环境总览：改成卡片分区，卡内自己分层，不再靠分隔线 + 裸标题堆出一条长页 */
.ov-alert {
    margin-bottom: 8px;
}

.ov-top {
    align-items: stretch;
}

.ov-card {
    border-radius: 6px;
}

.ov-card :deep(.arco-card-header-title) {
    font-size: 13px;
    font-weight: 600;
}

.ov-card :deep(.arco-card-body) {
    padding: 10px 12px;
}

.ov-desc :deep(.arco-descriptions-item-label) {
    color: var(--color-text-3);
    width: 88px;
    white-space: nowrap;
}

.ov-desc :deep(.arco-descriptions-item-value) {
    word-break: break-all;
}

.ov-others {
    margin-top: 8px;
    font-size: 12px;
    color: var(--color-text-3);
}

.ov-others-t {
    margin-bottom: 4px;
}

.ov-other-dir {
    font-family: Consolas, Menlo, monospace;
    word-break: break-all;
    padding-left: 8px;
}

.ov-bind {
    display: flex;
    flex-direction: column;
    gap: 8px;
}

.ov-bind-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
}

.ov-note {
    margin-top: 8px;
    font-size: 12px;
    line-height: 1.6;
    color: var(--color-text-3);
    word-break: break-all;
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

/* descriptions 标签里的小问号：跟文字同形，靠一点间距不贴在一起 */
.desc-tip {
    margin-left: 4px;
    color: var(--color-text-3);
    vertical-align: -1px;
}

/* Docker 镜像行：下拉占满剩余宽度，右侧一个方形按钮打开 Docker 管理窗口
 * Arco 的竖排表单项内容区默认按内容宽度收缩（align-items: flex-start），空下拉会被挤成一个小方块，
 * 这里显式把内容区拉成整栏宽 */
.inst-form :deep(.img-item .arco-form-item-wrapper-col) {
    align-items: stretch;
}

.inst-form :deep(.img-item .arco-form-item-content) {
    width: 100%;
}

.img-row {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
}

.img-select {
    flex: 1;
    min-width: 0;
}

.img-open {
    flex: none;
    width: 32px;
    padding: 0;
}

/* 本地没有镜像时的提示：橙色一点，提醒可以点进去装 */
.img-none {
    color: rgb(var(--orange-6));
}

/* #help 里的“去安装”链接：跟提示文字同字号，不把行高撑开 */
.img-none :deep(.arco-link) {
    padding: 0;
    font-size: inherit;
    line-height: inherit;
}
</style>
