<template>
    <a-row class="php-page" :gutter="6">
        <!-- 左侧：实例列表 -->
        <a-col :span="7" class="side-col">
            <a-card class="side-card card-head-tool" size="small" :body-style="{ padding: 0 }">
                <template #title>本地服务</template>
                <template #extra>
                    <div class="srv-head-actions">
                        <a-button size="small" @click="openInstModal()">新建实例</a-button>
                        <a-button size="small" @click="openVersionsModal()">版本管理</a-button>
                    </div>
                </template>
                <!-- 引擎没起时先把话说明白：否则要等点「启动」撞一条管道错误才知道是 Docker 没跑 -->
                <common-docker-off class="srv-docker-off" :count="dockerInstCount" tool-label="PHP" />
                <a-list :bordered="false" size="small">
                    <a-list-item v-for="inst in instances" :key="inst.id"
                        :class="{ 'inst-active': selectedId === inst.id }" @click="selectInst(inst.id)">
                        <a-space>
                            <com-badge :status="states[inst.id]?.running ? 'success' : 'normal'"
                                :animated="!!states[inst.id]?.running" />
                            <a-tooltip :content="inst.name + ':' + inst.port">
                                <span class="conn-name">{{ inst.name }}</span>
                            </a-tooltip>
                            <a-tooltip v-if="inst.mode === 'docker'"
                                :content="imgMissing(inst.image)
                                    ? `镜像 ${inst.image} 本机还没有，启动时会先自动拉取（可能几分钟）`
                                    : `镜像 ${inst.image}：这就是该实例的 PHP 版本`">
                                <a-tag :color="imgMissing(inst.image) ? 'orange' : 'purple'" size="small">Docker
                                    {{ imgTag(inst.image) }}</a-tag>
                            </a-tooltip>
                            <a-tag v-else color="arcoblue" size="small">v{{ inst.version }}</a-tag>
                            <a-tag v-if="inst.mode !== 'docker' && inst.runMode === 'cgi'" color="green" size="small">FastCGI</a-tag>
                            <a-tooltip v-if="inst.remark" :content="inst.remark">
                                <a-tag color="gray" size="small">Nginx 站点</a-tag>
                            </a-tooltip>
                        </a-space>
                        <template #actions>
                            <a-dropdown trigger="hover" @select="(v: any) => onInstAction(v, inst)">
                                <icon-more class="conn-more" @click.stop />
                                <template #content>
                                    <a-doption v-if="!states[inst.id]?.running" value="start"><icon-play-arrow />
                                        启动</a-doption>
                                    <a-doption v-else value="stop"><icon-pause /> 停止</a-doption>
                                    <a-doption value="restart"><icon-refresh /> 重启</a-doption>
                                    <a-doption value="logs"><icon-file /> 日志</a-doption>
                                    <a-doption value="edit"><icon-edit /> 编辑</a-doption>
                                    <a-doption value="del"><icon-delete /> 删除</a-doption>
                                </template>
                            </a-dropdown>
                        </template>
                    </a-list-item>
                    <a-list-item v-if="instances.length === 0">
                        <div class="conn-empty">暂无实例，点击右上"新建实例"</div>
                    </a-list-item>
                </a-list>
            </a-card>
        </a-col>

        <!-- 右侧：选中实例详情 + 日志 -->
        <a-col :span="17" class="main-col">
            <a-card v-if="!selectedId" class="main-panel empty-card card-head-tool">
                <a-empty description="从左侧选择一个 PHP 实例查看状态与日志" />
            </a-card>
            <a-card v-else class="main-panel card-head-tool" :body-style="{ padding: 0 }">
                <template #title>
                    <a-space>
                        <com-badge :status="selState?.running ? 'success' : 'normal'" :animated="!!selState?.running" />
                        <span>{{ selCfg?.name }}</span>
                        <a-tooltip v-if="isDockerSel" :content="selCfg?.image || ''">
                            <a-tag color="purple" size="small">Docker {{ imgTag(selCfg?.image || '') }} · :{{ selCfg?.port }}</a-tag>
                        </a-tooltip>
                        <a-tag v-else color="arcoblue" size="small">{{ selCfg?.runMode === 'cgi' ? 'php-cgi' : 'php -S' }} :{{ selCfg?.port }}</a-tag>
                        <a-tooltip v-if="selCfg?.remark" :content="selCfg?.remark">
                            <a-tag color="gray" size="small">Nginx 站点</a-tag>
                        </a-tooltip>
                    </a-space>
                </template>
                <template #extra>
                    <a-space>
                        <a-button size="small" :disabled="selState?.running || startBusy || stopBusy"
                            :loading="startBusy" @click="doStart">启动</a-button>
                        <!-- 停止会断开正在服务的请求，先气泡确认；按钮转 loading 显示「停止中」 -->
                        <a-popconfirm :content="`停止实例「${selCfg?.name}」？正在处理的请求会中断。`" type="warning"
                            position="lt" :disabled="!selState?.running || stopBusy" @ok="doStop">
                            <a-button size="small" status="danger"
                                :disabled="!selState?.running || startBusy || stopBusy" :loading="stopBusy">
                                {{ stopBusy ? '停止中' : '停止' }}</a-button>
                        </a-popconfirm>
                        <a-button size="small" :disabled="stopBusy" :loading="startBusy" @click="doRestart">
                            {{ startBusy ? '重启中' : '重启' }}</a-button>
                    </a-space>
                </template>
                <div class="detail-grid">
                    <div class="ls-item"><span>PHP 版本</span><b>{{ selState?.version || selCfg?.version || '-' }}</b>
                    </div>
                    <div class="ls-item"><span>运行模式</span>
                        <b>{{ isDockerSel ? 'Docker 容器' : '本地 php -S' }}</b>
                    </div>
                    <div class="ls-item"><span>运行状态</span>
                        <b class="com-state" :class="selState?.running ? 'ok' : 'no'">
                            <com-badge :status="selState?.running ? 'success' : 'normal'"
                                :animated="!!selState?.running" />
                            {{ selState?.running ? '运行中' : '已停止' }}
                        </b>
                    </div>
                    <div class="ls-item" v-if="isDockerSel"><span>容器</span>
                        <!-- 容器名带实例后缀很长，单元格会省略号收尾，悬停拿完整名 -->
                        <a-tooltip :content="selState?.container || '-'">
                            <b class="ellipsis">{{ selState?.container || '-' }}{{ selState?.containerState ? '（' + selState.containerState + '）' : '' }}</b>
                        </a-tooltip>
                    </div>
                    <div class="ls-item" v-else><span>PID</span><b>{{ selState?.pid || '-' }}</b></div>
                    <div class="ls-item"><span>运行时长</span><b>{{ fmtUptimeS(selState?.uptimeSeconds) }}</b></div>
                    <div class="ls-item">
                        <span>站点目录<a-tooltip content="Docker 模式为宿主目录，会挂载进容器 /var/www/html">
                                <icon-question-circle class="ls-label-tip" />
                            </a-tooltip></span>
                        <a-tooltip :content="selState?.docRoot || '-'">
                            <b class="ellipsis">{{ selState?.docRoot || '-' }}</b>
                        </a-tooltip>
                    </div>
                    <div class="ls-item">
                        <span>php.ini<a-tooltip
                                :content="isDockerSel ? '挂载进容器 conf.d 的覆盖配置' : '实例启动时 -c 指向的 php.ini'">
                                <icon-question-circle class="ls-label-tip" />
                            </a-tooltip></span>
                        <a-tooltip :content="selState?.configPath || '-'">
                            <b class="ellipsis">{{ selState?.configPath || '-' }}</b>
                        </a-tooltip>
                    </div>
                    <div class="ls-item">
                        <span>已加载扩展</span>
                        <a-tooltip v-if="isDockerSel" content="容器内的扩展由镜像提供，本面板的扩展管理只作用于本机 PHP">
                            <b>不适用（容器内）</b>
                        </a-tooltip>
                        <b v-else>{{ selState?.extensionsLoaded ?? '-' }}</b>
                    </div>
                    <div class="ls-item"><span>禁用函数</span>
                        <a-tooltip :content="selState?.disableFunctions || '（无）'">
                            <b class="ellipsis">{{ selState?.disableFunctions || '（无）' }}</b>
                        </a-tooltip>
                    </div>
                </div>
                <div v-if="pullNote" class="pull-tip"><icon-loading spin />{{ pullNote }}</div>
                <div class="log-head">
                    <span>访问/错误日志（实时）</span>
                    <a-space :size="6">
                        <a-button size="small" :disabled="!selState?.running"
                            @click="openSite(selCfg?.port ?? 0)">
                            <template #icon><icon-launch /></template>打开站点
                        </a-button>
                        <a-button size="small" @click="openIni(selectedId)">
                            <template #icon><icon-settings /></template>php.ini
                        </a-button>
                        <a-button size="small" @click="openCommon(selectedId)">
                            <template #icon><icon-tool /></template>常用配置
                        </a-button>
                        <a-tooltip v-if="isDockerSel" content="Docker 实例的 PHP 扩展由镜像提供，扩展管理只作用于本机 PHP">
                            <span class="btn-wrap"><a-button size="small" disabled>
                                    <template #icon><icon-list /></template>扩展管理
                                </a-button></span>
                        </a-tooltip>
                        <a-button v-else size="small" @click="openExt(selectedId)">
                            <template #icon><icon-list /></template>扩展管理
                        </a-button>
                        <a-button size="small" @click="openDisFn(selectedId)">
                            <template #icon><icon-stop /></template>禁用函数
                        </a-button>
                        <a-button size="small" @click="logLines = []">
                            <template #icon><icon-delete /></template>清空
                        </a-button>
                    </a-space>
                </div>
                <div ref="logBoxRef" class="ls-log">
                    <div v-for="(l, i) in logLines" :key="i" class="ls-line">{{ l }}</div>
                    <div v-if="logLines.length === 0" class="conn-empty">暂无日志</div>
                </div>
            </a-card>
        </a-col>
    </a-row>

    <!-- 新建/编辑实例 -->
    <a-modal v-model:visible="instVisible" :title="instForm.id ? '编辑实例' : '新建实例'" :width="560"
        :body-style="{ maxHeight: '72vh' }" @before-ok="saveInstance">
        <!-- 引擎没起：吸顶提示只占一行，不随错误串长度把弹窗撑高，也不推动表单字段 -->
        <common-modal-tip v-if="instForm.mode === 'docker' && dockerErr" :text="dockerErr">
          <a-button size="small" @click="openDockerWindow">打开 Docker 面板</a-button>
        </common-modal-tip>
        <a-form :model="instForm" layout="vertical" class="inst-form">
            <a-row :gutter="12">
                <a-col :span="24">
                    <a-form-item field="name" label="实例名称" required>
                        <a-input v-model="instForm.name" placeholder="如 my-site" />
                    </a-form-item>
                </a-col>

                <a-col :span="24">
                    <a-form-item field="mode" label="运行模式">
                        <a-radio-group size="medium" v-model="instForm.mode" type="button" @change="onModeChange">
                            <a-radio value="local"><icon-computer /> 本地</a-radio>
                            <a-radio value="docker"><icon-experiment /> Docker</a-radio>
                        </a-radio-group>
                        <template #extra>
                            <template v-if="instForm.mode === 'docker'">
                                容器内跑 php -S，站点目录挂载到 /var/www/html；首次启动会自动拉取镜像（可能几分钟）
                            </template>
                            <template v-if="sysPkg">使用系统安装的 PHP（php -S 内置服务器，版本在「版本管理」里选）</template>
                            <template v-else>使用插件目录下的本机 PHP（php -S 内置服务器）</template>
                        </template>
                    </a-form-item>
                </a-col>

                <!-- 本地：版本 / 构型 / 端口 同一行 -->
                <template v-if="instForm.mode !== 'docker'">
                    <a-col :span="sysPkg ? 17 : 10">
                        <a-form-item field="version" label="PHP 版本" required>
                            <a-select v-model="instForm.version" placeholder="选择已安装版本" :options="versionOptions" />
                        </a-form-item>
                    </a-col>
                    <!-- Linux 只有一种 php-cli 构建，NTS/ZTS 是 Windows 绿色版才有的区分 -->
                    <a-col v-if="!sysPkg" :span="7">
                        <a-form-item field="nts" label="构型"
                            tooltip="NTS 非线程安全，php -S 内置服务器用这个；ZTS 线程安全，供 Apache 等多线程环境">
                            <a-radio-group v-model="instForm.nts" type="button" size="medium">
                                <a-radio :value="true">NTS</a-radio>
                                <a-radio :value="false">ZTS</a-radio>
                            </a-radio-group>
                        </a-form-item>
                    </a-col>
                </template>

                <!-- Docker：镜像 / 容器名 / 端口 同一行，与本地模式的版本/构型/端口行数一致，切模式时弹窗高度不跳 -->
                <template v-else>
                    <a-col :span="10">
                        <a-form-item class="img-item" field="image" label="镜像" required>
                            <div class="img-row">
                                <a-select v-model="instForm.image" class="img-select" placeholder="选择已安装镜像"
                                    allow-create filterable :loading="dockerImagesLoading">
                                    <!-- 自闭合的 a-option 没有文字，label 会是空串，下拉里全是空白行 —— 显式写上文字 -->
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
                    <a-col :span="8">
                        <a-form-item field="containerName" label="容器名" tooltip="留空自动生成 go-tools-php-实例ID">
                            <a-input v-model="instForm.containerName" placeholder="留空自动生成" />
                        </a-form-item>
                    </a-col>
                </template>

                <!-- 端口：两种模式都与上一项并排，位置一致 -->
                <a-col :span="instForm.mode === 'docker' ? 6 : 7">
                    <a-form-item field="port" label="端口" :error="portError"
                        tooltip="实例只监听 127.0.0.1，Docker 与本地模式一样不对外暴露">
                        <a-input-number v-model="instForm.port" :min="1" :max="65535"
                            @change="portError = ''" @blur="checkPortTaken" />
                    </a-form-item>
                </a-col>

                <a-col :span="24">
                    <a-form-item field="docRoot" label="站点根目录"
                        tooltip="留空则使用程序目录 data/www/实例名；Docker 模式会挂载进容器 /var/www/html">
                        <a-input-search v-model="instForm.docRoot" placeholder="留空则使用默认站点目录" search-button
                            button-text="选择目录" @search="chooseDocRoot" />
                    </a-form-item>
                </a-col>
                <a-col :span="24">
                    <a-form-item field="router" label="路由脚本">
                        <a-input v-model="instForm.router" placeholder="相对站点目录，如 index.php；可留空" />
                        <template #extra v-if="instForm.mode === 'docker'">Docker 模式只能使用站点目录内的文件</template>
                    </a-form-item>
                </a-col>
            </a-row>
        </a-form>
    </a-modal>

    <!-- 版本管理 -->
    <!-- 固定高度：三个页签内容高矮不一，不钉住的话切一下弹窗就跳一次高度 -->
    <a-modal v-model:visible="versionsVisible" title="PHP 版本管理" :width="880" :footer="false" unmount-on-close
        :body-style="{ padding: '12px 16px', height: 'min(600px, calc(100vh - 220px))', overflowY: 'auto' }">
        <!-- 顶部：类型切换（原本只是三个计数，现在点哪类就只看哪类，弹窗不用来回滚） -->
        <div class="vm-summary">
            <a-radio-group size="medium" v-model="verTab" type="button">
                <a-radio value="local">本地版本 {{ localVersions.length }}</a-radio>
                <a-radio value="remote">{{ sysPkg ? '仓库可安装' : '线上可安装' }} {{ remoteVersions.length }}</a-radio>
                <a-radio value="docker">Docker 实例 {{ dockerInstCount }}</a-radio>
            </a-radio-group>
            <!-- 插件目录只在绿色版有意义：Linux 的版本都在系统路径里，没有可复制的存放目录 -->
            <div v-if="!sysPkg" class="vm-sum-item vm-sum-dir">
                <span class="vm-sum-label">插件目录</span>
                <a-tooltip :content="pluginsDirVal || '加载中…'">
                    <span class="vm-dir">{{ pluginsDirVal || '…' }}</span>
                </a-tooltip>
                <a-button size="small" @click="copyPluginsDir">复制</a-button>
            </div>
        </div>

        <!-- 本地可用版本：固定高度滚动 -->
        <div v-show="verTab === 'local'" class="vm-section">
            <div class="vm-head">
                <span class="vm-head-title"><icon-desktop /> 本地可用版本</span>
                <div class="vm-head-ops">
                    <span class="vm-head-tip">内置版本与系统包版本不可删除；运行中实例引用时拒绝删除</span>
                    <a-button size="small" @click="loadLocalVersions">
                        <template #icon><icon-refresh /></template>刷新
                    </a-button>
                </div>
            </div>
            <div class="vm-local-list">
                <div v-if="localVersions.length === 0" class="vm-empty">
                    <icon-empty /> 暂无本地版本，可从下方安装，或注册本机已有目录
                </div>
                <div v-for="v in localVersions" :key="v.version" class="vm-local-row">
                    <span class="vm-ver">v{{ v.version }}</span>
                    <a-tag size="small" :color="v.source === 'bundled' ? 'purple' : v.source === 'downloaded' ? 'green' : v.source === 'system' ? 'gray' : 'orange'">
                        {{ srcLabel(v.source) }}
                    </a-tag>
                    <a-tooltip :content="v.path">
                        <span class="vm-path">{{ v.path }}</span>
                    </a-tooltip>
                    <a-popconfirm :content="`确定删除 PHP ${v.version}？若仍有运行中的实例引用该版本会被拒绝。`"
                        type="warning" position="lt" :disabled="v.source === 'bundled' || v.source === 'system'" @ok="removeVersion(v)">
                        <a-button size="small" status="danger" :disabled="v.source === 'bundled' || v.source === 'system'">删除</a-button>
                    </a-popconfirm>
                </div>
            </div>
            <a-row class="add-local" :gutter="8">
                <a-col :span="16">
                    <a-input v-model="localDirInput" placeholder="注册本机已有 PHP：填写 php 所在目录" allow-clear />
                </a-col>
                <a-col :span="4">
                    <a-button size="small" block @click="chooseLocalDir">浏览…</a-button>
                </a-col>
                <a-col :span="4">
                    <a-button size="small" block @click="addLocalDir">添加</a-button>
                </a-col>
            </a-row>
        </div>

        <!-- Docker 镜像与容器：Docker 实例的「版本」就是镜像 tag（php:8.3-cli），这里直接当一个版本源来管 -->
        <div v-show="verTab === 'docker'">
            <docker-images class="vm-docker" keyword="php" tool-label="PHP" repo="php" :presets="phpImagePresets"
                :instances="instances" :states="states" @create="newInstanceFromImage" @use="editInstanceById"
                @changed="refreshStates" />
        </div>

        <!-- 线上版本：固定高度滚动 -->
        <div v-show="verTab === 'remote'" class="vm-section">
            <div class="vm-head">
                <span class="vm-head-title"><icon-cloud /> {{ sysPkg ? '仓库可安装版本（发行版包，装到系统标准路径）' : '线上版本（windows.php.net）' }}</span>
                <a-button size="small" :loading="remoteLoading" @click="loadRemoteVersions">
                    <template #icon><icon-refresh /></template>刷新
                </a-button>
            </div>
            <a-spin :loading="remoteLoading" style="width:100%">
                <a-table :data="remoteVersions" size="small" :pagination="false" row-key="version"
                    :scroll="{ x: '100%', y: 200 }" :bordered="false"
                    :columns="[
                        { title: '版本', dataIndex: 'version', width: 100 },
                        { title: '产品线', dataIndex: 'release', width: 80 },
                        { title: sysPkg ? '包名' : '包大小', dataIndex: 'size', width: 90, slotName: 'size' },
                        { title: '状态', dataIndex: 'st', width: 190, slotName: 'st' },
                        { title: '安装', dataIndex: 'ops', width: 150, slotName: 'ops', fixed: 'right', align: 'right' },
                    ]">
                    <template #size="{ record }">{{ remoteVerText(record) }}</template>
                    <template #st="{ record }">
                        <span v-if="installedSet.has(record.version)" class="com-state is-on">
                            <com-badge status="success" />已安装
                        </span>
                        <template v-else-if="installProgress[record.version]">
                            <span v-if="installProgress[record.version].stage === 'error'" class="com-state is-off">
                                <com-badge status="danger" />失败
                            </span>
                            <div v-else class="vm-progress">
                                <a-progress :percent="(installProgress[record.version].percent || 0) / 100" size="mini"
                                    :show-text="false" />
                                <span class="vm-progress-text">{{ stageText(installProgress[record.version]) }}</span>
                            </div>
                        </template>
                        <span v-else class="com-state is-dim">
                            <com-badge status="normal" />未安装
                        </span>
                    </template>
                    <template #ops="{ record }">
                        <a-button v-if="installedSet.has(record.version)" size="small" @click="loadLocalVersions">已安装</a-button>
                        <a-button v-else-if="installProgress[record.version] && installProgress[record.version].stage !== 'error'"
                            size="small" disabled>安装中…</a-button>
                        <template v-else-if="sysPkg">
                            <a-tooltip content="Linux 只有一条 php-cli 包可用，不区分 NTS/ZTS">
                                <a-button size="small" @click="installVersion(record, true)">安装</a-button>
                            </a-tooltip>
                        </template>
                        <template v-else>
                            <a-tooltip content="非线程安全（推荐，php -S 场景）">
                                <a-button size="small" @click="installVersion(record, true)">NTS</a-button>
                            </a-tooltip>
                            <a-tooltip content="线程安全（搭配 Apache/Nginx 模块时使用）">
                                <a-button size="small" style="margin-left:6px"
                                    @click="installVersion(record, false)">ZTS</a-button>
                            </a-tooltip>
                        </template>
                    </template>
                </a-table>
            </a-spin>
            <div v-if="sysPkg" class="vm-tip">版本由发行版仓库提供：点「安装」即由包管理器装好 php-cli（含标准扩展），装完刷新本地版本即可选。</div>
            <div v-else class="vm-tip">离线安装：将 php zip（如 php-8.3.36-nts-Win32-vs16-x64.zip）放入插件目录后刷新本地版本即可。</div>
        </div>

        <!-- 任务日志：固定高度滚动；Docker 页签不涉及下载/安装，不占这一块地方 -->
        <div v-show="verTab !== 'docker'" class="vm-section" style="margin-bottom:0">
            <div class="vm-head">
                <span class="vm-head-title"><icon-file /> 任务日志</span>
                <a-button size="small" :disabled="installLogs.length === 0" @click="installLogs = []">清空</a-button>
            </div>
            <div ref="installLogRef" class="vm-log">
                <div v-for="(l, i) in installLogs" :key="i" class="vm-log-line" :class="logClass(l)">{{ l }}</div>
                <div v-if="installLogs.length === 0" class="vm-empty small">暂无日志</div>
            </div>
        </div>
    </a-modal>

    <!-- php.ini 文本编辑 -->
    <a-modal v-model:visible="iniVisible" :title="`php.ini - ${cfgNameOf(iniInstId)}`" :width="760" :footer="false">
        <a-alert v-if="isDockerOf(iniInstId)" type="info" style="margin-bottom: 8px">
            Docker 实例：这一份是挂载进容器 /usr/local/etc/php/conf.d/zz-go-tools.ini 的覆盖配置，只写需要覆盖镜像默认值的指令；
            重启实例后生效，extension= 指向的是镜像内的扩展，与本面板「扩展管理」无关。
        </a-alert>
        <a-textarea v-model="iniText" :auto-size="{ minRows: 16, maxRows: 24 }" spellcheck="false" />
        <div class="ini-footer">
            <span class="ini-tip">保存前自动备份；配置加载失败将回滚。</span>
            <a-space>
                <a-button size="small" @click="iniVisible = false">取消</a-button>
                <a-button size="small" @click="saveIniAndClose">保存</a-button>
            </a-space>
        </div>
    </a-modal>

    <!-- 常用配置 -->
    <a-modal v-model:visible="commonVisible" title="常用配置" :width="520" @before-ok="saveCommon">
        <a-form v-if="commonForm" :model="commonForm" layout="vertical">
            <a-row :gutter="12">
                <a-col :span="12">
                    <a-form-item field="memoryLimit" label="memory_limit（如 128M）">
                        <a-input v-model="commonForm.memoryLimit" />
                    </a-form-item>
                </a-col>
                <a-col :span="12">
                    <a-form-item field="maxExecutionTime" label="max_execution_time（秒）">
                        <a-input v-model="commonForm.maxExecutionTime" />
                    </a-form-item>
                </a-col>
                <a-col :span="12">
                    <a-form-item field="uploadMaxFilesize" label="upload_max_filesize（如 64M）">
                        <a-input v-model="commonForm.uploadMaxFilesize" />
                    </a-form-item>
                </a-col>
                <a-col :span="12">
                    <a-form-item field="postMaxSize" label="post_max_size（如 64M）">
                        <a-input v-model="commonForm.postMaxSize" />
                    </a-form-item>
                </a-col>
                <a-col :span="12">
                    <a-form-item field="displayErrors" label="display_errors">
                        <a-select v-model="commonForm.displayErrors"
                            :options="[{ label: 'On', value: 'On' }, { label: 'Off', value: 'Off' }]" />
                    </a-form-item>
                </a-col>
                <a-col :span="12">
                    <a-form-item field="exposePhp" label="expose_php">
                        <a-select v-model="commonForm.exposePhp"
                            :options="[{ label: 'On', value: 'On' }, { label: 'Off', value: 'Off' }]" />
                    </a-form-item>
                </a-col>
                <a-col :span="12">
                    <a-form-item field="errorReporting" label="error_reporting">
                        <a-input v-model="commonForm.errorReporting" placeholder="E_ALL" />
                    </a-form-item>
                </a-col>
                <a-col :span="12">
                    <a-form-item field="timezone" label="date.timezone">
                        <a-input v-model="commonForm.timezone" placeholder="Asia/Shanghai" />
                    </a-form-item>
                </a-col>
            </a-row>
        </a-form>
    </a-modal>

    <!-- 扩展管理 -->
    <a-modal v-model:visible="extVisible" :title="`扩展管理 - ${cfgNameOf(extInstId)}`" :width="780" :footer="false"
        unmount-on-close :body-style="{ padding: '12px 16px', maxHeight: '82vh', overflowY: 'auto' }">
        <div class="ext-section-head">
            <span class="ext-section-title">本地扩展（php.ini 配置）</span>
            <a-input-search v-model="extFilter" placeholder="搜索扩展名" size="small" style="width:200px" allow-clear />
        </div>
        <a-table :data="filteredExtensions" size="small" :pagination="false" row-key="name" :scroll="{ y: 260 }"
            :columns="extColumns">
            <!-- 绿色版：按 ext 目录里的 dll 判断能不能开；Linux：扩展由发行版包提供，没有 dll 这一层。
                 已随系统加载的模块（Core、date 这类）没有对应包，不能拿扩展名硬拼包名。 -->
            <template #dll="{ record }">
                <template v-if="sysPkg">{{ record.system ? '—' : 'php-' + record.name }}</template>
                <template v-else>{{ record.dll }}</template>
            </template>
            <template #system="{ record }"><a-tag v-if="record.system" size="small">{{ sysPkg ? '系统' : '核心' }}</a-tag></template>
            <template #available="{ record }"><a-tag v-if="record.available" color="green" size="small">√</a-tag></template>
            <template #enabled="{ record }">
                <a-tag v-if="record.system" size="small" color="gray">{{ sysPkg ? '随系统加载' : '核心内置' }}</a-tag>
                <a-tag v-else-if="!record.available && !record.enabled" size="small" color="gray">无 DLL</a-tag>
                <a-switch v-else v-model="record.enabled" size="small"
                    @change="(v: any) => toggleExt(record, !!v)" />
            </template>
            <template #loaded="{ record }"><a-tag v-if="record.loaded" color="arcoblue" size="small">√</a-tag></template>
        </a-table>
        <div v-if="sysPkg" class="offline-tip">这里的扩展由发行版包提供（如 php-curl），随系统 php.ini / scan 目录加载；
            增删请用下方「扩展安装」（包管理器），卸载也只能用包管理器。</div>
        <div v-else class="offline-tip">切换开关会写入 php.ini；重启实例后“已加载”才会刷新。</div>
    
        <a-divider style="margin: 12px 0" />
    
        <div class="ext-section-head">
            <span class="ext-section-title"><icon-cloud /> {{ sysPkg ? '扩展安装（发行版包 php-<ext>，需 root）' : '在线可下载安装（PECL Windows）' }}</span>
            <a-button size="small" :loading="remoteExtsLoading" @click="loadRemoteExts(true)">
                <template #icon><icon-refresh /></template>强制刷新
            </a-button>
        </div>
        <a-spin :loading="remoteExtsLoading" style="width:100%">
            <a-table :data="remoteExts" size="small" :pagination="false" row-key="name" :scroll="{ x: '100%', y: 200 }"
                :columns="[
                    { title: '扩展', dataIndex: 'label', width: 110 },
                    { title: '说明', dataIndex: 'description', ellipsis: true, tooltip: true },
                    { title: '状态', dataIndex: 'st', width: 150, slotName: 'st' },
                    { title: '操作', dataIndex: 'ops', width: 190, slotName: 'ops', fixed: 'right', align: 'right' },
                ]">
                <template #st="{ record }">
                    <span v-if="record.busy" class="com-state is-busy">
                        <com-badge status="processing" :animated="true" />安装中…
                    </span>
                    <span v-else-if="record.installed" class="com-state is-on">
                        <com-badge status="success" />已安装{{ record.extVersion ? ' v'+record.extVersion : '' }}
                    </span>
                    <span v-else-if="record.fetching" class="com-state is-dim">
                        <com-badge status="normal" :animated="true" />获取目录中…
                    </span>
                    <span v-else-if="record.matchable" class="com-state is-warn">
                        <com-badge status="warning" />可下载{{ record.extVer ? ' v'+record.extVer : '' }}
                    </span>
                    <span v-else-if="record.seriesSeen" class="com-state is-dim">
                        <com-badge status="normal" />当前版本无匹配
                    </span>
                    <a-tooltip v-else-if="record.reason" :content="record.reason">
                        <span class="com-state is-dim"><com-badge status="normal" />不可用</span>
                    </a-tooltip>
                    <span v-else class="com-state is-dim"><com-badge status="normal" />暂无</span>
                </template>
                <template #ops="{ record }">
                    <a-space :size="4">
                        <a-button v-if="record.matchable && !record.installed" size="small"
                            :disabled="record.busy" @click="installRemoteExt(record.name)">安装并启用</a-button>
                        <a-popconfirm v-if="record.downloaded && !record.busy"
                            :content="sysPkg ? `卸载 ${record.name}？Linux 上扩展由包管理器管理，这里只会提示对应命令` : `删除已下载的扩展 ${record.name}？DLL 文件会被移除，内置 DLL 不受影响。`"
                            type="warning" position="lt" @ok="removeRemoteExt(record.name)">
                            <a-button size="small" status="danger" :loading="record.busy">删除</a-button>
                        </a-popconfirm>
                    </a-space>
                </template>
            </a-table>
        </a-spin>
        <div v-if="sysPkg" class="offline-tip">安装交给发行版包管理器（php-&lt;扩展名&gt;），需要 root；仓库里没有的扩展会提示改用 pecl 编译。安装完成后请重启实例生效。</div>
        <div v-else class="offline-tip">DLL 将安装到 PHP 版本 ext 目录；删除仅针对在线获取版本，内置 DLL 不可删除。安装完成后请重启实例生效。</div>
    </a-modal>

    <!-- 禁用函数 -->
    <a-modal v-model:visible="disfnVisible" :title="`禁用函数 - ${cfgNameOf(disfnInstId)}`" :width="640" :footer="false"
        unmount-on-close>
        <a-spin :loading="disfnLoading" style="width:100%">
            <div class="disfn-cur">
                <a-tag v-for="f in disableFns" :key="f" closable color="red" @click="() => { }"
                    @close="removeDisFn(f)">{{ f }}</a-tag>
                <span v-if="disableFns.length === 0" class="conn-empty">当前未禁用任何函数</span>
            </div>
            <a-space style="margin: 10px 0">
                <a-input v-model="disfnInput" placeholder="添加禁用函数，多个用逗号分隔" style="width:300px"
                    @press-enter="addDisFns(disfnInput.split(/[,，\s]+/))" />
                <a-button size="small" @click="addDisFns(disfnInput.split(/[,，\s]+/))">添加</a-button>
            </a-space>
            <div class="ver-title">常见建议禁用（点击追加）</div>
            <div class="disfn-common">
                <a-tag v-for="f in commonlyDisabled" :key="f" :color="disableFns.includes(f) ? 'gray' : 'orangered'"
                    style="cursor:pointer" @click="!disableFns.includes(f) && addDisFns([f])">{{ f }}</a-tag>
            </div>
        </a-spin>
    </a-modal>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, h, nextTick, onMounted, onUnmounted, reactive, ref } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import { useRoute } from "vue-router";
import { useToolWindow } from "@/views/index/useToolWindow";
import { remoteVerText, useSysPkg } from "@/utils/caps";
import { restoreInstalls } from "@/store/modules/tasks";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

/** 系统包模式（Linux）：版本与扩展都来自发行版仓库，没有 Windows 绿色版 zip/dll */
const sysPkg = useSysPkg();

interface InstConfig { id: string; name: string; version: string; port: number; nts: boolean; docRoot: string; router: string; enableLogs: boolean; mode: string; image: string; containerName: string; runMode?: string; remark?: string }

/** Docker 实例的版本就是镜像 tag：列表里只展示 tag（8.3-cli），完整镜像名走 tooltip */
function imgTag(image: string): string {
    const s = String(image || "");
    const i = s.indexOf(":");
    return i >= 0 ? s.slice(i + 1) : (s || "Docker");
}

/* ============================ 状态 ============================ */
const instances = ref<InstConfig[]>([]);
const states = reactive<Record<string, any>>({});
const selectedId = ref("");
const logLines = ref<string[]>([]);
const logBoxRef = ref<HTMLElement | null>(null);
const installLogRef = ref<HTMLElement | null>(null);
let logTimer: any = null;
let srvTimer: any = null;
const route = useRoute();
const { openToolWindow } = useToolWindow();
let offInstall: any = null;
let offSelInst: any = null;
let offPull: any = null;

const selCfg = computed(() => instances.value.find((i) => i.id === selectedId.value) || null);
const selState = computed(() => (selectedId.value ? states[selectedId.value] : null));
const isDockerSel = computed(() => selCfg.value?.mode === "docker");

/* Docker 模式表单：镜像候选只列本机已安装的 php 系镜像（不猜预置 tag），列表为空就引导去安装 */
/* 名字不能回退成 dockerImages：script setup 下 kebab 标签 docker-images 会优先拿同名 setup 变量当组件，整个 Docker 区会渲染成空白 */
const dockerImageList = ref<string[]>([]);
const dockerImagesLoading = ref(false);
const dockerErr = ref("");
const pullNote = ref("");
const imageOptions = computed(() => Array.from(new Set<string>(dockerImageList.value)));

/** 只刷新镜像清单（不写表单）：用来标记实例引用的镜像本机是否还在 */
async function probeDockerImages() {
    try { dockerImageList.value = (await GoTools.PhpSrvDockerImages()) || []; }
    catch (e) { /* 引擎未就绪就不做校验，免得误报 */ }
}
/** 镜像清单非空且不含它 → 悬空引用（镜像被删了，实例还指着） */
function imgMissing(image: string): boolean {
    return !!image && dockerImageList.value.length > 0 && !dockerImageList.value.includes(image);
}

const instForm = reactive<InstConfig>({
    id: "", name: "", version: "", port: 8088, nts: true, docRoot: "", router: "", enableLogs: true,
    mode: "local", image: "", containerName: "",
});
/** 端口占用提示：贴表单项底下（红字），保存前也用同一份结果拦一次 */
const portError = ref("");
const instVisible = ref(false);

/** 版本管理概览：有多少实例在用容器（它们的版本由镜像 tag 决定，不占本机版本库） */
const dockerInstCount = computed(() => instances.value.filter((i) => i.mode === "docker").length);
/** 版本管理弹窗的类型切换：本地 / 线上 / Docker 只看一类 */
const verTab = ref<"local" | "remote" | "docker">("local");
const versionsVisible = ref(false);
/** 版本管理里 Docker 镜像的常用 tag：容器内跑 php -S 只认 cli 变体，所以 cli 排前面 */
const phpImagePresets = ["php:8.4-cli", "php:8.3-cli", "php:8.2-cli", "php:8.1-cli", "php:8.4-apache", "php:8.3-zts-cli"];
const localVersions = ref<any[]>([]);
const remoteVersions = ref<any[]>([]);
const remoteLoading = ref(false);
const installProgress = reactive<Record<string, any>>({});
const installLogs = ref<string[]>([]);
const localDirInput = ref("");
const pluginsDirVal = ref("");

const iniVisible = ref(false);
const iniInstId = ref("");
const iniText = ref("");

const commonVisible = ref(false);
const commonInstId = ref("");
const commonForm = ref<any>(null);

const extVisible = ref(false);
const extInstId = ref("");
const extensions = ref<any[]>([]);
const extFilter = ref("");
const remoteExts = ref<any[]>([]);
const remoteExtsLoading = ref(false);
let extPollTimer: any = null;
let offExtInstall: any = null;

const disfnVisible = ref(false);
const disfnInstId = ref("");
const disfnLoading = ref(false);
const disableFns = ref<string[]>([]);
const disfnInput = ref("");
const commonlyDisabled = ref<string[]>([]);

const versionOptions = computed(() => localVersions.value.map((v: any) => ({ label: `${v.version}（${srcLabel(v.source)}）`, value: v.version })));
const installedSet = computed(() => new Set<string>(localVersions.value.map((v: any) => v.version)));
const filteredExtensions = computed(() => {
    const q = extFilter.value.trim().toLowerCase();
    return q ? extensions.value.filter((e: any) => e.name.toLowerCase().includes(q) || (e.dll || "").toLowerCase().includes(q)) : extensions.value;
});
/**
 * 本地扩展表头：绿色版按 ext 目录里的 dll 判断能不能开；
 * Linux 上扩展由发行版包提供（没有 dll），改成展示对应的包名。
 */
const extColumns = computed(() => sysPkg.value
    ? [
        { title: "扩展", dataIndex: "name", width: 140 },
        { title: "发行版包", dataIndex: "dll", slotName: "dll", ellipsis: true },
        { title: "内置", dataIndex: "system", width: 60, slotName: "system" },
        { title: "已启用", dataIndex: "enabled", width: 110, slotName: "enabled" },
        { title: "已加载", dataIndex: "loaded", width: 72, slotName: "loaded" },
    ]
    : [
        { title: "扩展", dataIndex: "name", width: 140 },
        { title: "DLL", dataIndex: "dll", ellipsis: true, tooltip: true },
        { title: "内置", dataIndex: "system", width: 60, slotName: "system" },
        { title: "有 DLL", dataIndex: "available", width: 72, slotName: "available" },
        { title: "已启用", dataIndex: "enabled", width: 80, slotName: "enabled" },
        { title: "已加载", dataIndex: "loaded", width: 72, slotName: "loaded" },
    ]);

/* ============================ 小工具 ============================ */
function srcLabel(s: string): string {
    return s === "bundled" ? "内置" : s === "downloaded" ? "下载" : s === "system" ? "系统包" : "本机";
}
function stageText(p: any): string {
    if (!p) return "";
    if (p.stage === "downloading") return `下载中 ${p.percent || 0}%`;
    if (p.stage === "extracting") return "解压中";
    if (p.stage === "done") return "完成";
    if (p.stage === "info") return "准备中";
    return "失败";
}
function fmtUptimeS(sec?: number): string {
    if (!sec || sec <= 0) return "-";
    const d = Math.floor(sec / 86400); const h2 = Math.floor((sec % 86400) / 3600);
    const m = Math.floor((sec % 3600) / 60); const s = sec % 60;
    if (d > 0) return `${d}d ${h2}h ${m}m`; if (h2 > 0) return `${h2}h ${m}m`; return `${m}m ${s}s`;
}
function cfgNameOf(id: string): string {
    return instances.value.find((i) => i.id === id)?.name || id;
}
function pushInstallLog(txt: string) {
    if (!txt) return;
    const t = new Date();
    const pad = (n: number) => n.toString().padStart(2, "0");
    installLogs.value.push(`[${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}] ${txt}`);
    if (installLogs.value.length > 200) installLogs.value.splice(0, installLogs.value.length - 200);
    // 固定高度日志框自动滚到底部
    nextTick(() => {
        const el = installLogRef.value;
        if (el) el.scrollTop = el.scrollHeight;
    });
}
/** 日志行着色：失败红、完成绿、进行中蓝 */
function logClass(line: string): string {
    if (line.includes("失败") || line.includes("错误") || line.includes("HTTP") || line.includes("拒绝")) return "err";
    if (line.includes("完成") || line.includes("成功")) return "ok";
    if (line.includes("下载") || line.includes("安装") || line.includes("提交") || line.includes("解压")) return "info";
    return "";
}
async function copyPluginsDir() {
    if (!pluginsDirVal.value) return;
    try {
        await navigator.clipboard.writeText(pluginsDirVal.value);
        Message.success("已复制插件目录");
    } catch (e) {
        Message.info(pluginsDirVal.value);
    }
}

/* ============================ 数据加载 ============================ */
async function loadLocalVersions() {
    try { localVersions.value = (await GoTools.PhpSrvListVersions()) || []; }
    catch (e) { localVersions.value = []; }
}
/** 某个实例是否 Docker 模式（以配置为准，状态未加载也能判断） */
function isDockerOf(id: string): boolean {
    return instances.value.find((i) => i.id === id)?.mode === "docker";
}
async function loadImageOptions() {
    dockerImagesLoading.value = true;
    try { dockerImageList.value = (await GoTools.PhpSrvDockerImages()) || []; }
    catch (e) { dockerImageList.value = []; } // 引擎不可用时当作没有镜像，走“去安装”引导
    dockerImagesLoading.value = false;
    // 还没选值且有候选时先填第一个，省一步点击
    if (instForm.mode === "docker" && !instForm.image && dockerImageList.value.length) instForm.image = dockerImageList.value[0];
}
async function loadDockerEngine() {
    try {
        const [ok, msg] = await GoTools.PhpSrvDockerAvailable();
        dockerErr.value = ok ? "" : (msg || "Docker 引擎不可用");
    } catch (e: any) { dockerErr.value = e?.message || String(e); }
}
/** 打开 Docker 管理窗口（独立 webview，单例复用）：配镜像时直接过去拉，不用回主页找入口 */
function openDockerWindow() {
    openToolWindow("/tools/docker", "Docker管理", { single: true, width: 1200, height: 800 });
}
async function loadInstances() {
    try { instances.value = (await GoTools.PhpSrvListInstances()) || []; }
    catch (e) { instances.value = []; }
    if (!selectedId.value && instances.value.length) selectInst(instances.value[0].id);
    refreshStates();
}
/**
 * 状态轮询是单飞的：上一轮没回来就跳过本轮。
 * Docker 实例的状态要走 Engine API，引擎卡住时一次要挂十几秒，定时器照旧触发就会
 * 把请求叠成雪崩（终端满屏 Binding 调用失败 + Docker Desktop 被拖到 CPU 100%）。
 */
let stateInflight = false;
/** 一轮全失败后的静默期：引擎离线时不要每 3 秒追问一次 */
let stateQuietUntil = 0;

async function refreshStates(silent = false) {
    if (stateInflight) return;
    if (silent && Date.now() < stateQuietUntil) return;
    if (silent && startBusy.value) return; // 启动/重启期间先让后端专心建容器
    stateInflight = true;
    let ok = 0;
    try {
        // 并发问每个实例：串行 await 会变成 N 次往返，Web 模式在远程服务器上尤其明显
        await Promise.all(instances.value.map(async (inst) => {
            try {
                states[inst.id] = await GoTools.PhpSrvStatus(inst.id);
                ok++;
                if (selectedId.value === inst.id && !logTimer) startLogPolling();
            } catch (e) { /* 单个实例失败不影响其它实例 */ }
        }));
    } finally {
        stateQuietUntil = ok === 0 && instances.value.length > 0 ? Date.now() + 15000 : 0;
        stateInflight = false;
    }
}
function selectInst(id: string) {
    selectedId.value = id;
    logLines.value = [];
    lastLogFetch = 0;
    startLogPolling();
    refreshStates();
}

/* ============================ 日志轮询 ============================ */
let logInflight = false;
/** 日志接口连续失败后的静默期 */
let logQuietUntil = 0;
let lastLogFetch = 0;

async function pollLogs() {
    if (!selectedId.value || logInflight) return;
    if (startBusy.value) return; // 建容器期间不去戳日志接口
    const now = Date.now();
    if (now < logQuietUntil) return;
    // Docker 实例没在跑时日志不再增长：降到 10 秒一次，只为捎带最后几行
    if (isDockerSel.value && !selState.value?.running && now - lastLogFetch < 10000) return;
    logInflight = true;
    try {
        const lines = (await GoTools.PhpSrvLogs(selectedId.value, 300)) || [];
        logQuietUntil = 0;
        if (lines.join("\n") !== logLines.value.join("\n")) {
            logLines.value = lines;
            nextTick(() => { if (logBoxRef.value) logBoxRef.value.scrollTop = logBoxRef.value.scrollHeight; });
        }
    } catch (e) {
        // 引擎离线/日志读不到：退避 15 秒，不要每 2 秒往终端刷一条错误
        logQuietUntil = Date.now() + 15000;
    } finally {
        lastLogFetch = Date.now();
        logInflight = false;
    }
}
function startLogPolling() {
    if (logTimer) clearInterval(logTimer);
    pollLogs();
    logTimer = setInterval(pollLogs, 2000);
}

/* ============================ 实例操作 ============================ */
const startBusy = ref(false);
/** 停止中：停止走的是守护进程退出，Docker 模式可能等几秒，期间禁止重入 */
const stopBusy = ref(false);
/** 启动（本地拉进程，Docker 建容器；首次可能需拉镜像数分钟，进度靠 docker:pull 事件） */
async function doStartId(id: string, name: string) {
    if (!id || startBusy.value) return;
    startBusy.value = true;
    pullNote.value = "";
    try {
        await GoTools.PhpSrvStart(id);
        Message.success(`实例 ${name} 已启动`);
    } catch (e: any) {
        Message.error("启动失败: " + (e?.message || e));
    } finally {
        startBusy.value = false;
        refreshStates();
        setTimeout(() => { pullNote.value = ""; }, 4000);
    }
}
async function doStart() {
    const c = selCfg.value; if (!c) return;
    await doStartId(c.id, c.name);
}
async function doStop() {
    const c = selCfg.value; if (!c) return;
    if (startBusy.value || stopBusy.value) return;
    stopBusy.value = true;
    try { await GoTools.PhpSrvStop(c.id); Message.success(`实例 ${c.name} 已停止`); }
    catch (e: any) { Message.error("停止失败: " + (e?.message || e)); }
    finally { stopBusy.value = false; }
    refreshStates();
}
async function doRestart() {
    const c = selCfg.value; if (!c) return;
    startBusy.value = true;
    try { await GoTools.PhpSrvRestart(c.id); Message.success(`实例 ${c.name} 已重启`); }
    catch (e: any) { Message.error("重启失败: " + (e?.message || e)); }
    finally { startBusy.value = false; refreshStates(); }
}
function openSite(port: number) {
    window.open(`http://127.0.0.1:${port}/`, "_blank");
}
async function onInstAction(v: string, inst: InstConfig) {
    if (v === "start") {
        await doStartId(inst.id, inst.name);
    } else if (v === "stop") {
        try { await GoTools.PhpSrvStop(inst.id); Message.success(`实例 ${inst.name} 已停止`); }
        catch (e: any) { Message.error("停止失败: " + (e?.message || e)); }
        refreshStates();
    } else if (v === "restart") {
        try { await GoTools.PhpSrvRestart(inst.id); Message.success(`实例 ${inst.name} 已重启`); }
        catch (e: any) { Message.error("重启失败: " + (e?.message || e)); }
        refreshStates();
    } else if (v === "open") {
        openSite(inst.port);
    } else if (v === "ini") {
        openIni(inst.id);
    } else if (v === "common") {
        openCommon(inst.id);
    } else if (v === "ext") {
        openExt(inst.id);
    } else if (v === "disfn") {
        openDisFn(inst.id);
    } else if (v === "logs") {
        selectInst(inst.id);
    } else if (v === "edit") {
        openInstModal(inst);
    } else if (v === "del") {
        Modal.warning({
            title: "删除实例", content: `确定删除实例「${inst.name}」？运行中会先停止；站点目录保留。`, hideCancel: false,
            onOk: async () => {
                try {
                    await GoTools.PhpSrvRemoveInstance(inst.id);
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
    if (inst) {
        // 历史配置可能没有 mode/image 字段，归一后再填表（空值即本地模式）
        Object.assign(instForm, {
            ...inst,
            mode: inst.mode === "docker" ? "docker" : "local",
            image: inst.image || "",
            containerName: inst.containerName || "",
        });
    } else {
        // 默认端口也要能用：跳过其它实例已登记的端口与系统里已监听的端口
        let port = 8088;
        try { port = (await GoTools.PhpSrvSuggestPort(8088)) || port; } catch (e) { /* ignore */ }
        Object.assign(instForm, {
            id: "", name: "", version: localVersions.value[0]?.version || "", port, nts: true,
            docRoot: "", router: "", enableLogs: true, mode: "local", image: "", containerName: "",
        });
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

/** 版本管理里点「新建实例」：直接以该镜像开一个 Docker 实例（端口仍走可用端口探测） */
async function newInstanceFromImage(image: string) {
    versionsVisible.value = false;
    await openInstModal();
    instForm.mode = "docker";
    instForm.image = image;
    await onModeChange("docker");
}

/** 版本管理里点「使用实例」：从镜像视角转去改该实例的版本（镜像） */
async function editInstanceById(id: string) {
    const inst = instances.value.find((i) => i.id === id);
    if (!inst) return;
    versionsVisible.value = false;
    selectInst(id);
    await openInstModal(inst);
}
async function checkPortTaken() {
    if (!instForm.port) { portError.value = ""; return; }
    try {
        // 后端会把“其它工具实例已登记的端口”和“系统已监听”一起算进来；编辑时排除自身
        const reason = await GoTools.PhpSrvPortTaken(instForm.port, instForm.id || "");
        if (!reason) { portError.value = ""; return; }
        const suggest = await GoTools.PhpSrvSuggestPort(instForm.port);
        portError.value = suggest && suggest !== instForm.port ? `${reason}，可用：${suggest}` : reason;
    } catch (e) { /* ignore */ }
}
async function chooseDocRoot() {
    try { const dir = await GoTools.PhpSrvChooseDir(); if (dir) instForm.docRoot = dir; }
    catch (e) { /* cancel */ }
}
async function saveInstance(): Promise<boolean> {
    if (!instForm.name) { Message.warning("请填写实例名称"); return false; }
    if (instForm.mode === "docker") {
        if (!instForm.image) { Message.warning("请选择镜像：本地没有镜像时点下方“点击去安装”"); return false; }
    } else if (!instForm.version) { Message.warning("请选择 PHP 版本"); return false; }
    if (!instForm.port) { Message.warning("请填写端口"); return false; }
    // 端口占用先在本地拦一次，省得提交后才报错（后端保存时还会再校验）
    try {
        const reason = await GoTools.PhpSrvPortTaken(instForm.port, instForm.id || "");
        if (reason) {
            const suggest = await GoTools.PhpSrvSuggestPort(instForm.port);
            const tip = suggest && suggest !== instForm.port ? `${reason}，可用：${suggest}` : reason;
            portError.value = tip;
            Message.warning(tip);
            return false;
        }
    } catch (e) { /* 探测失败不阻断，后端保存时兜底 */ }
    try {
        // 编辑前先记下原配置：切了版本/镜像且实例在跑，容器里还是旧版本，得重启才生效
        const old = instForm.id ? instances.value.find((i) => i.id === instForm.id) : null;
        const switched = !!old && (old.mode !== instForm.mode || old.image !== instForm.image || old.version !== instForm.version);
        const wasRunning = !!old && !!states[old.id]?.running;
        await GoTools.PhpSrvSaveInstance({ ...instForm });
        Message.success("已保存"); await loadInstances();
        if (switched && wasRunning) setTimeout(() => askRestartAfterSwitch({ ...instForm }), 240);
        return true;
    } catch (e: any) { Message.error("保存失败: " + (e?.message || e)); return false; }
}

/** 切了版本/镜像又没重启：跑着的还是旧版本，问一句要不要现在就换过去 */
function askRestartAfterSwitch(inst: InstConfig) {
    const what = inst.mode === "docker" ? `镜像 ${inst.image}` : `PHP ${inst.version}`;
    Modal.confirm({
        title: "版本已切换",
        content: `${what} 已写入配置，但实例「${inst.name}」还在跑旧版本；重启后才会生效（期间会短暂中断服务）。`,
        okText: "立即重启",
        cancelText: "稍后手动重启",
        onOk: async () => {
            try {
                await GoTools.PhpSrvRestart(inst.id);
                Message.success(`实例 ${inst.name} 已重启，新版本已生效`);
            } catch (e: any) { Message.error("重启失败: " + (e?.message || e)); }
            refreshStates();
        },
    });
}

/* ============================ 版本管理 ============================ */
async function openVersionsModal() {
    versionsVisible.value = true;
    verTab.value = "local";
    loadLocalVersions();
    if (!pluginsDirVal.value) {
        try { pluginsDirVal.value = (await GoTools.PhpSrvPluginsDir()) || ""; } catch (e) { /* ignore */ }
    }
    if (remoteVersions.value.length === 0) loadRemoteVersions();
}
async function loadRemoteVersions() {
    remoteLoading.value = true;
    try { remoteVersions.value = (await GoTools.PhpSrvRemoteVersions()) || []; }
    catch (e: any) { Message.error("获取远程版本失败: " + (e?.message || e)); }
    remoteLoading.value = false;
}
async function installVersion(r: any, nts: boolean) {
    try {
        pushInstallLog(`提交安装请求：PHP ${r.version}（${nts ? "NTS" : "ZTS"}）`);
        await GoTools.PhpSrvInstall(r.version, nts);
        installProgress[r.version] = { stage: "downloading", percent: 0 };
    } catch (e: any) {
        pushInstallLog(`安装请求被拒绝：${e?.message || e}`);
        Message.error("安装失败: " + (e?.message || e));
    }
}

/** 失败原因截断：去掉后端附带的版本号前缀，过长内容省略 */
function shortErr(note: string): string {
    let s = (note || "未知错误").trim().replace(/^[0-9][0-9.]*\s*[：:]\s*/, "");
    if (s.length > 160) s = s.slice(0, 160) + "…";
    return s;
}

let errModalOpen = false;
/** 紧凑卡片式失败提示（替代默认大段白底弹窗） */
function showInstallError(version: string, note: string) {
    if (errModalOpen) return;
    errModalOpen = true;
    Modal.error({
        title: `PHP ${version} 安装失败`,
        width: 460,
        hideCancel: true,
        okText: "知道了",
        content: () => h("div", { style: "font-size:13px;line-height:1.7" }, [
            h("div", { style: "color:var(--color-text-1);word-break:break-all" }, shortErr(note)),
            // Linux 没有“放个 zip 进去”这条路：失败只可能是包管理器/权限/网络
            h("div", { style: "margin-top:8px;color:var(--color-text-3);font-size:12px" },
                sysPkg.value
                    ? "可稍后重试；若提示需要 root、仓库无此版本或网络不通，请按提示在服务器上执行对应命令后刷新。"
                    : "可稍后重试；或将对应 php zip 放入以下插件目录后刷新本地版本，离线安装。"),
            h("div", {
                style: "margin-top:6px;padding:6px 8px;border-radius:4px;background:var(--color-fill-2);" +
                    "font-family:Consolas,Menlo,monospace;font-size:12px;word-break:break-all;color:var(--color-text-2)",
            }, sysPkg.value ? "由发行版包管理器提供（apt / dnf）" : (pluginsDirVal.value || "插件目录加载中…")),
        ]),
        onClose: () => { errModalOpen = false; },
    });
}
async function removeVersion(v: any) {
    try { await GoTools.PhpSrvRemoveVersion(v.version); Message.success("已删除"); loadLocalVersions(); }
    catch (e: any) { Message.error("删除失败: " + (e?.message || e)); }
}
async function chooseLocalDir() {
    try { const dir = await GoTools.PhpSrvChooseDir(); if (dir) localDirInput.value = dir; }
    catch (e) { /* cancel */ }
}
async function addLocalDir() {
    const dir = localDirInput.value.trim();
    if (!dir) { Message.warning("请先选择或填写目录"); return; }
    try { await GoTools.PhpSrvAddLocal(dir); Message.success("已添加本机目录"); localDirInput.value = ""; loadLocalVersions(); }
    catch (e: any) { Message.error("添加失败: " + (e?.message || e)); }
}

/* ============================ php.ini ============================ */
async function openIni(id: string) {
    iniInstId.value = id;
    iniText.value = "";
    iniVisible.value = true;
    try { iniText.value = (await GoTools.PhpSrvGetConfigText(id)) || ""; }
    catch (e: any) { Message.error("读取 php.ini 失败: " + (e?.message || e)); iniVisible.value = false; }
}
async function saveIniText(): Promise<boolean> {
    try {
        await GoTools.PhpSrvSaveConfigText(iniInstId.value, iniText.value);
        Message.success("php.ini 已保存"); refreshStates(); return true;
    } catch (e: any) { Message.error("保存失败: " + (e?.message || e)); return false; }
}
async function saveIniAndClose() {
    if (await saveIniText()) iniVisible.value = false;
}

/* ============================ 常用配置 ============================ */
async function openCommon(id: string) {
    commonInstId.value = id;
    commonForm.value = null;
    commonVisible.value = true;
    try { commonForm.value = await GoTools.PhpSrvGetCommon(id); }
    catch (e: any) { Message.error("读取配置失败: " + (e?.message || e)); commonVisible.value = false; }
}
async function saveCommon(): Promise<boolean> {
    if (!commonForm.value) return false;
    try {
        await GoTools.PhpSrvSetCommon(commonInstId.value, { ...commonForm.value });
        Message.success("常用配置已保存，重启实例后生效"); refreshStates(); return true;
    } catch (e: any) { Message.error("保存失败: " + (e?.message || e)); return false; }
}

/* ============================ 扩展管理 ============================ */
async function openExt(id: string) {
    extInstId.value = id;
    extensions.value = [];
    remoteExts.value = [];
    extVisible.value = true;
    try { extensions.value = (await GoTools.PhpSrvExtensions(id)) || []; }
    catch (e: any) { Message.error("读取扩展列表失败: " + (e?.message || e)); extVisible.value = false; }
    loadRemoteExts(false);
}
async function toggleExt(rec: any, enable: boolean) {
    try {
        const msg = await GoTools.PhpSrvSetExtension(extInstId.value, rec.name, enable);
        Message.success(msg || `扩展 ${rec.name} 已${enable ? "启用" : "禁用"}，重启实例后生效`);
        extensions.value = (await GoTools.PhpSrvExtensions(extInstId.value)) || [];
    } catch (e: any) {
        rec.enabled = !enable;
        Message.error("操作失败: " + (e?.message || e));
    }
}

async function loadRemoteExts(force: boolean) {
    if (!extInstId.value) return;
    remoteExtsLoading.value = true;
    try {
        remoteExts.value = (await GoTools.PhpSrvExtCatalog(extInstId.value, force)) || [];
        // 后台摸描未就绪时轮询
        if (extPollTimer) { clearTimeout(extPollTimer); extPollTimer = null; }
        if (remoteExts.value.some((e: any) => e.fetching)) {
            extPollTimer = setTimeout(() => { loadRemoteExts(false); }, 2500);
        }
    } catch (e) { /* ignore */ }
    remoteExtsLoading.value = false;
}

async function installRemoteExt(ext: string) {
    const r = remoteExts.value.find((e: any) => e.name === ext);
    if (r) r.busy = true;
    try {
        await GoTools.PhpSrvExtInstall(extInstId.value, ext, true);
    } catch (e: any) {
        if (r) r.busy = false;
        Message.error("安装失败: " + (e?.message || e));
    }
}

async function removeRemoteExt(ext: string) {
    const r = remoteExts.value.find((e: any) => e.name === ext);
    if (r) r.busy = true;
    try {
        await GoTools.PhpSrvExtRemove(extInstId.value, ext);
        if (r) { r.installed = false; r.downloaded = false; r.enabled = false; }
        if (r) r.busy = false;
        Message.success(`扩展 ${ext} 已删除`);
        // 刷新本地扩展列表
        extensions.value = (await GoTools.PhpSrvExtensions(extInstId.value)) || [];
    } catch (e: any) {
        if (r) r.busy = false;
        Message.error("删除失败: " + (e?.message || e));
    }
}

/* ============================ 禁用函数 ============================ */
async function openDisFn(id: string) {
    disfnInstId.value = id;
    disableFns.value = [];
    disfnVisible.value = true;
    disfnLoading.value = true;
    try {
        disableFns.value = (await GoTools.PhpSrvDisableFunctions(id)) || [];
        if (commonlyDisabled.value.length === 0) {
            commonlyDisabled.value = (await GoTools.PhpSrvCommonlyDisabled()) || [];
        }
    } catch (e: any) { Message.error("读取禁用函数失败: " + (e?.message || e)); }
    disfnLoading.value = false;
}
async function addDisFns(names: string[]) {
    const list = (names || []).map((s) => s.trim()).filter((s) => s && !disableFns.value.includes(s));
    if (list.length === 0) return;
    try {
        await GoTools.PhpSrvAddDisableFunctions(disfnInstId.value, list);
        disableFns.value = Array.from(new Set([...disableFns.value, ...list]));
        disfnInput.value = "";
        Message.success("已添加，重启实例后生效");
        refreshStates();
    } catch (e: any) { Message.error("添加失败: " + (e?.message || e)); }
}
function removeDisFn(f: string) {
    Modal.warning({
        title: "移除禁用", content: `确定从 disable_functions 中移除「${f}」？`, hideCancel: false,
        onOk: async () => {
            try {
                await GoTools.PhpSrvRemoveDisableFunctions(disfnInstId.value, [f]);
                disableFns.value = disableFns.value.filter((x) => x !== f);
                refreshStates();
            } catch (e: any) { Message.error("移除失败: " + (e?.message || e)); }
        },
    });
}

/** docker:pull 事件 → 容器实例启动时的拉镜像进度（负载 PullProgress） */
function onPullEvent(raw: any) {
    const p = raw?.data ?? raw;
    if (!p || !p.image) return;
    if (p.stage === "done") { pullNote.value = `镜像 ${p.image} 已就绪`; return; }
    if (p.stage === "error") { pullNote.value = `镜像 ${p.image} 拉取失败：${p.note || ""}`; return; }
    const bits = [p.status, p.percent ? `${p.percent}%` : ""].filter((s) => !!s).join(" ");
    pullNote.value = `拉取镜像 ${p.image}${p.layer ? ` · ${String(p.layer).slice(0, 12)}` : ""}${bits ? ` · ${bits}` : ""}`;
}

/* ============================ 生命周期 ============================ */
onMounted(async () => {
    await loadInstances();
    // 镜像清单不进表单，只用来标记「实例引用的镜像本机已没有」
    probeDockerImages();
    // 主页点击服务实例进入（?inst=）：选中该实例展示详情
    const wantId = String(route.query.inst ?? "");
    if (wantId && instances.value.some((i) => i.id === wantId)) selectInst(wantId);
    loadLocalVersions();
    // 离开页面时安装还在跑：从任务台账把进度表与日志补回来
    // （扩展安装进度挂在远程扩展行上，那个靠全局任务浮标看，这里只补版本安装）
    restoreInstalls("php", "install", installProgress, pushInstallLog);
    srvTimer = setInterval(() => { refreshStates(true); }, 3000);
    // 单例窗口已打开时，主页再次点击其他实例 → 通过事件切换
    offSelInst = Events.On("tool:select-inst", (ev: any) => {
        const p = ev?.data ?? ev;
        if (p?.type !== "php" || !p?.id) return;
        if (instances.value.some((i) => i.id === String(p.id))) selectInst(String(p.id));
    });
    offPull = Events.On("docker:pull", onPullEvent);
    offInstall = Events.On("php:install", (ev: any) => {
        const p = ev?.data ?? ev;
        if (!p || !p.version) return;
        if (p.stage === "info") {
            pushInstallLog(p.note || "");
        } else if (p.stage === "done") {
            delete installProgress[p.version];
            pushInstallLog(`安装完成 PHP ${p.version}`);
            Message.success(`PHP ${p.version} 安装完成`);
            loadLocalVersions();
        } else if (p.stage === "error") {
            installProgress[p.version] = p;
            pushInstallLog(`安装失败 ${p.version}：${p.note || "未知错误"}`);
            showInstallError(p.version, p.note || "未知错误");
        } else {
            installProgress[p.version] = p;
        }
    });
    offExtInstall = Events.On("php:ext-install", (ev: any) => {
        const p = ev?.data ?? ev;
        if (!p || !p.version) return;
        const ext = p.version;
        const r = remoteExts.value.find((e: any) => e.name === ext);
        if (p.stage === "done") {
            if (r) { r.installed = true; r.downloaded = true; r.enabled = true; r.busy = false; }
            pushInstallLog(`扩展 ${ext} 安装完成`);
            // 刷新本地扩展列表
            if (extVisible.value && extInstId.value) {
                GoTools.PhpSrvExtensions(extInstId.value).then((list: any) => { extensions.value = list || []; }).catch(() => {});
            }
        } else if (p.stage === "error") {
            if (r) r.busy = false;
            pushInstallLog(`扩展 ${ext} 安装失败：${p.note || ""}`);
            Message.error(`扩展 ${ext} 安装失败：${p.note || "未知错误"}`);
        } else {
            if (r) r.busy = true;
        }
    });
});
onUnmounted(() => {
    if (offInstall) offInstall();
    if (offSelInst) offSelInst();
    if (offExtInstall) offExtInstall();
    if (offPull) offPull();
    if (extPollTimer) clearTimeout(extPollTimer);
    if (srvTimer) clearInterval(srvTimer);
    if (logTimer) clearInterval(logTimer);
});
</script>

<style scoped>
.php-page {
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
}

.side-card {
    height: 100%;
    display: flex;
    flex-direction: column;
}

.side-card :deep(.arco-card-body) {
    flex: 1;
    overflow: auto;
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
    max-width: 140px;
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

/* 这个 b 原本为省略号设了 overflow:hidden，会把圆点光晕切掉一圈 */
.ls-item b.com-state {
    overflow: visible;
    justify-content: flex-end;
}

/* 字段说明：图标 + a-tooltip，不用原生 title */
.ls-label-tip {
    margin-left: 4px;
    font-size: 12px;
    color: var(--color-text-3);
    vertical-align: -1px;
    cursor: help;
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

.ver-block .ver-title {
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

.offline-tip {
    margin-top: 8px;
    font-size: 12px;
    color: var(--color-text-3);
    word-break: break-all;
}

/* 表单固定高度：两种模式字段行数一样，切模式时弹窗不再跟着跳 */
.inst-form {
    min-height: 490px;
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

/* #help 里的“去安装”链接：跟 12px 提示文字同字号，不把行高撑开 */
.img-none :deep(.arco-link) {
    padding: 0;
    font-size: inherit;
    line-height: inherit;
}

.btn-wrap {
    display: inline-flex;
}

/* Docker 启动时的拉镜像进度行 */
.pull-tip {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 0 14px 8px;
    padding: 6px 8px;
    border-radius: 4px;
    background: var(--color-fill-2);
    font-size: 12px;
    color: var(--color-text-2);
}

.install-log {
    margin-top: 12px;
}

/* ============================ 版本管理弹窗 ============================ */
.vm-summary {
    display: flex;
    align-items: center;
    gap: 14px;
    background: var(--color-fill-2);
    border-radius: 6px;
    padding: 8px 14px;
    margin-bottom: 10px;
}

.vm-sum-item {
    display: flex;
    align-items: baseline;
    gap: 6px;
}

.vm-sum-num {
    font-size: 20px;
    font-weight: 600;
    line-height: 1;
    color: rgb(var(--primary-6));
}

.vm-sum-label {
    font-size: 12px;
    color: var(--color-text-3);
}

.vm-sum-dir {
    flex: 1;
    min-width: 0;
    justify-content: flex-end;
}

.vm-dir {
    max-width: 380px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 12px;
    color: var(--color-text-2);
}

.vm-section {
    border: 1px solid var(--color-border-2);
    border-radius: 6px;
    padding: 10px 12px;
    margin-bottom: 10px;
}

.vm-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 8px;
    min-height: 24px;
}

.vm-head-title {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 13px;
    font-weight: 600;
}

.vm-head-tip {
    font-size: 12px;
    color: var(--color-text-3);
}

.vm-head-ops {
    display: flex;
    align-items: center;
    gap: 10px;
}

/* 本地版本列表：固定高度滚动 */
.vm-local-list {
    height: 132px;
    overflow: auto;
    background: var(--color-fill-1);
    border-radius: 4px;
    padding: 2px 4px;
}

.vm-local-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 5px 8px;
    border-radius: 4px;
    font-size: 12px;
}

.vm-local-row:hover {
    background: var(--color-fill-2);
}

.vm-ver {
    min-width: 64px;
    font-weight: 600;
}

.vm-path {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--color-text-3);
}

.vm-empty {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    height: 100%;
    font-size: 12px;
    color: var(--color-text-3);
}

.vm-empty.small {
    height: 60px;
}

.vm-tip {
    margin-top: 8px;
    font-size: 12px;
    color: var(--color-text-3);
    word-break: break-all;
}

.vm-progress {
    display: flex;
    align-items: center;
    gap: 6px;
}

.vm-progress :deep(.arco-progress) {
    width: 70px;
}

.vm-progress-text {
    font-size: 12px;
    color: var(--color-text-3);
}

/* 任务日志：固定高度滚动 */
.vm-log {
    height: 140px;
    overflow: auto;
    background: var(--color-fill-1);
    border-radius: 4px;
    padding: 6px 8px;
    font-family: Consolas, Menlo, monospace;
    font-size: 12px;
}

.vm-log-line {
    line-height: 1.7;
    white-space: pre-wrap;
    word-break: break-all;
    color: var(--color-text-2);
}

.vm-log-line.err {
    color: rgb(var(--red-6));
}

.vm-log-line.ok {
    color: rgb(var(--green-6));
}

.vm-log-line.info {
    color: rgb(var(--arcoblue-6));
}

.ini-footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-top: 10px;
}

.ini-tip {
    font-size: 12px;
    color: var(--color-text-3);
}

.disfn-cur {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    min-height: 40px;
    padding: 8px;
    background: var(--color-fill-2);
    border-radius: 4px;
}

.disfn-common {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 8px;
}

/* 扩展管理分节头 */
.ext-section-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    margin-bottom: 8px;
}

.ext-section-title {
    display: flex;
    align-items: center;
    gap: 6px;
    font-weight: 600;
    font-size: 13px;
    color: var(--color-text-1);
}
</style>
