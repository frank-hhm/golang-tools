<template>
    <a-row class="php-page" :gutter="6">
        <!-- 左侧：实例列表 -->
        <a-col :span="7" class="side-col">
            <a-card class="side-card" size="small" :body-style="{ padding: 0 }">
                <template #title>本地服务</template>
                <template #extra>
                    <div class="srv-head-actions">
                        <a-button size="small" title="新建实例" @click="openInstModal()">新建实例</a-button>
                        <a-button size="small" title="版本管理" @click="openVersionsModal()">版本管理</a-button>
                    </div>
                </template>
                <a-list :bordered="false" size="small">
                    <a-list-item v-for="inst in instances" :key="inst.id"
                        :class="{ 'inst-active': selectedId === inst.id }" @click="selectInst(inst.id)">
                        <a-space>
                            <a-badge :status="states[inst.id]?.running ? 'success' : 'normal'" />
                            <span class="conn-name" :title="inst.name + ':' + inst.port">{{ inst.name }}</span>
                            <a-tag color="arcoblue" size="small">v{{ inst.version }}</a-tag>
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
            <a-card v-if="!selectedId" class="main-panel empty-card">
                <a-empty description="从左侧选择一个 PHP 实例查看状态与日志" />
            </a-card>
            <a-card v-else class="main-panel" :body-style="{ padding: 0 }">
                <template #title>
                    <a-space>
                        <a-badge :status="selState?.running ? 'success' : 'normal'" />
                        <span>{{ selCfg?.name }}</span>
                        <a-tag color="arcoblue" size="small">php -S :{{ selCfg?.port }}</a-tag>
                    </a-space>
                </template>
                <template #extra>
                    <a-space>
                        <a-button size="small" type="primary" :disabled="selState?.running"
                            @click="doStart">启动</a-button>
                        <a-button size="small" status="danger" :disabled="!selState?.running"
                            @click="doStop">停止</a-button>
                        <a-button size="small" @click="doRestart">重启</a-button>
                    </a-space>
                </template>
                <div class="detail-grid">
                    <div class="ls-item"><span>PHP 版本</span><b>{{ selState?.version || selCfg?.version || '-' }}</b>
                    </div>
                    <div class="ls-item"><span>运行状态</span>
                        <b :class="selState?.running ? 'ok' : 'no'">{{ selState?.running ? '运行中' : '已停止' }}</b>
                    </div>
                    <div class="ls-item"><span>PID</span><b>{{ selState?.pid || '-' }}</b></div>
                    <div class="ls-item"><span>运行时长</span><b>{{ fmtUptimeS(selState?.uptimeSeconds) }}</b></div>
                    <div class="ls-item" title="站点根目录"><span>站点目录</span><b class="ellipsis">{{
                        selState?.docRoot || '-' }}</b></div>
                    <div class="ls-item" title="php.ini 路径"><span>php.ini</span><b class="ellipsis">{{
                        selState?.configPath || '-' }}</b></div>
                    <div class="ls-item"><span>已加载扩展</span><b>{{ selState?.extensionsLoaded ?? '-' }}</b></div>
                    <div class="ls-item" title="disable_functions"><span>禁用函数</span><b class="ellipsis">{{
                        selState?.disableFunctions || '（无）' }}</b></div>
                </div>
                <div class="log-head">
                    <span>访问/错误日志（实时）</span>
                    <a-space :size="6">
                        <a-button size="mini" :disabled="!selState?.running"
                            @click="openSite(selCfg?.port ?? 0)">
                            <template #icon><icon-launch /></template>打开站点
                        </a-button>
                        <a-button size="mini" @click="openIni(selectedId)">
                            <template #icon><icon-settings /></template>php.ini
                        </a-button>
                        <a-button size="mini" @click="openCommon(selectedId)">
                            <template #icon><icon-tool /></template>常用配置
                        </a-button>
                        <a-button size="mini" @click="openExt(selectedId)">
                            <template #icon><icon-list /></template>扩展管理
                        </a-button>
                        <a-button size="mini" @click="openDisFn(selectedId)">
                            <template #icon><icon-stop /></template>禁用函数
                        </a-button>
                        <a-button size="mini" @click="logLines = []">
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
    <a-modal v-model:visible="instVisible" :title="instForm.id ? '编辑实例' : '新建实例'" :width="520"
        @before-ok="saveInstance">
        <a-form :model="instForm" layout="vertical">
            <a-form-item field="name" label="实例名称" required>
                <a-input v-model="instForm.name" placeholder="如 my-site" />
            </a-form-item>
            <a-form-item field="version" label="PHP 版本" required>
                <a-select v-model="instForm.version" placeholder="选择已安装版本" :options="versionOptions" />
            </a-form-item>
            <a-form-item field="nts" label="构型">
                <a-radio-group v-model="instForm.nts" type="button" size="small">
                    <a-radio :value="true">NTS（非线程安全，常用）</a-radio>
                    <a-radio :value="false">ZTS（线程安全）</a-radio>
                </a-radio-group>
            </a-form-item>
            <a-form-item field="port" label="端口">
                <a-input-number v-model="instForm.port" :min="1" :max="65535" @blur="checkPortTaken" />
            </a-form-item>
            <a-form-item field="docRoot" label="站点根目录（留空 = 程序目录 data/www/实例名）">
                <a-input-search v-model="instForm.docRoot" placeholder="留空则使用默认站点目录" search-button
                    button-text="选择目录" @click-btn="chooseDocRoot" />
            </a-form-item>
            <a-form-item field="router" label="路由脚本（相对站点目录，如 index.php）">
                <a-input v-model="instForm.router" placeholder="可留空" />
            </a-form-item>
        </a-form>
    </a-modal>

    <!-- 版本管理 -->
    <a-modal v-model:visible="versionsVisible" title="PHP 版本管理" :width="880" :footer="false" unmount-on-close
        :body-style="{ padding: '12px 16px', maxHeight: '72vh', overflowY: 'auto' }">
        <!-- 顶部概览 -->
        <div class="vm-summary">
            <div class="vm-sum-item">
                <span class="vm-sum-num">{{ localVersions.length }}</span>
                <span class="vm-sum-label">本地版本</span>
            </div>
            <a-divider direction="vertical" style="margin: 0" />
            <div class="vm-sum-item">
                <span class="vm-sum-num">{{ remoteVersions.length }}</span>
                <span class="vm-sum-label">线上可安装</span>
            </div>
            <div class="vm-sum-item vm-sum-dir">
                <span class="vm-sum-label">插件目录</span>
                <a-tooltip :content="pluginsDirVal || '加载中…'">
                    <span class="vm-dir">{{ pluginsDirVal || '…' }}</span>
                </a-tooltip>
                <a-button size="mini" type="text" @click="copyPluginsDir">复制</a-button>
            </div>
        </div>

        <!-- 本地可用版本：固定高度滚动 -->
        <div class="vm-section">
            <div class="vm-head">
                <span class="vm-head-title"><icon-desktop /> 本地可用版本</span>
                <div class="vm-head-ops">
                    <span class="vm-head-tip">内置版本不可删除；运行中实例引用时拒绝删除</span>
                    <a-button size="mini" @click="loadLocalVersions">
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
                    <a-tag size="small" :color="v.source === 'bundled' ? 'purple' : v.source === 'downloaded' ? 'green' : 'orange'">
                        {{ srcLabel(v.source) }}
                    </a-tag>
                    <span class="vm-path" :title="v.path">{{ v.path }}</span>
                    <a-button size="mini" type="text" status="danger" :disabled="v.source === 'bundled'"
                        @click="removeVersion(v)">删除</a-button>
                </div>
            </div>
            <a-row class="add-local" :gutter="8">
                <a-col :span="16">
                    <a-input v-model="localDirInput" placeholder="注册本机已有 PHP：填写 php.exe 所在目录" allow-clear />
                </a-col>
                <a-col :span="4">
                    <a-button block @click="chooseLocalDir">浏览…</a-button>
                </a-col>
                <a-col :span="4">
                    <a-button type="primary" block @click="addLocalDir">添加</a-button>
                </a-col>
            </a-row>
        </div>

        <!-- 线上版本：固定高度滚动 -->
        <div class="vm-section">
            <div class="vm-head">
                <span class="vm-head-title"><icon-cloud /> 线上版本（windows.php.net）</span>
                <a-button size="mini" :loading="remoteLoading" @click="loadRemoteVersions">
                    <template #icon><icon-refresh /></template>刷新
                </a-button>
            </div>
            <a-spin :loading="remoteLoading" style="width:100%">
                <a-table :data="remoteVersions" size="small" :pagination="false" row-key="version"
                    :scroll="{ y: 200 }" :bordered="false"
                    :columns="[
                        { title: '版本', dataIndex: 'version', width: 100 },
                        { title: '产品线', dataIndex: 'release', width: 80 },
                        { title: '包大小', dataIndex: 'size', width: 90, slotName: 'size' },
                        { title: '状态', dataIndex: 'st', width: 190, slotName: 'st' },
                        { title: '安装', dataIndex: 'ops', width: 150, slotName: 'ops' },
                    ]">
                    <template #size="{ record }">{{ fmtBytes(record.size) }}</template>
                    <template #st="{ record }">
                        <a-tag v-if="installedSet.has(record.version)" color="green" size="small">已安装</a-tag>
                        <template v-else-if="installProgress[record.version]">
                            <a-tag v-if="installProgress[record.version].stage === 'error'" color="red" size="small">失败</a-tag>
                            <div v-else class="vm-progress">
                                <a-progress :percent="(installProgress[record.version].percent || 0) / 100" size="mini"
                                    :show-text="false" />
                                <span class="vm-progress-text">{{ stageText(installProgress[record.version]) }}</span>
                            </div>
                        </template>
                        <a-tag v-else size="small" color="gray">未安装</a-tag>
                    </template>
                    <template #ops="{ record }">
                        <a-button v-if="installedSet.has(record.version)" size="mini" @click="loadLocalVersions">已安装</a-button>
                        <a-button v-else-if="installProgress[record.version] && installProgress[record.version].stage !== 'error'"
                            size="mini" disabled>安装中…</a-button>
                        <template v-else>
                            <a-button size="mini" type="primary" title="非线程安全（推荐，php -S 场景）"
                                @click="installVersion(record, true)">NTS</a-button>
                            <a-button size="mini" style="margin-left:6px" title="线程安全（搭配 Apache/Nginx 模块时使用）"
                                @click="installVersion(record, false)">ZTS</a-button>
                        </template>
                    </template>
                </a-table>
            </a-spin>
            <div class="vm-tip">离线安装：将 php zip（如 php-8.3.36-nts-Win32-vs16-x64.zip）放入插件目录后刷新本地版本即可。</div>
        </div>

        <!-- 任务日志：固定高度滚动 -->
        <div class="vm-section" style="margin-bottom:0">
            <div class="vm-head">
                <span class="vm-head-title"><icon-file /> 任务日志</span>
                <a-button size="mini" type="text" :disabled="installLogs.length === 0" @click="installLogs = []">清空</a-button>
            </div>
            <div ref="installLogRef" class="vm-log">
                <div v-for="(l, i) in installLogs" :key="i" class="vm-log-line" :class="logClass(l)">{{ l }}</div>
                <div v-if="installLogs.length === 0" class="vm-empty small">暂无日志</div>
            </div>
        </div>
    </a-modal>

    <!-- php.ini 文本编辑 -->
    <a-modal v-model:visible="iniVisible" :title="`php.ini - ${cfgNameOf(iniInstId)}`" :width="760" :footer="false">
        <a-textarea v-model="iniText" :auto-size="{ minRows: 16, maxRows: 24 }" spellcheck="false" />
        <div class="ini-footer">
            <span class="ini-tip">保存前自动备份；配置加载失败将回滚。</span>
            <a-space>
                <a-button @click="iniVisible = false">取消</a-button>
                <a-button type="primary" @click="saveIniAndClose">保存</a-button>
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
            :columns="[
                { title: '扩展', dataIndex: 'name', width: 140 },
                { title: 'DLL', dataIndex: 'dll', ellipsis: true, tooltip: true },
                { title: '内置', dataIndex: 'system', width: 60, slotName: 'system' },
                { title: '有 DLL', dataIndex: 'available', width: 72, slotName: 'available' },
                { title: '已启用', dataIndex: 'enabled', width: 80, slotName: 'enabled' },
                { title: '已加载', dataIndex: 'loaded', width: 72, slotName: 'loaded' },
            ]">
            <template #system="{ record }"><a-tag v-if="record.system" size="small">核心</a-tag></template>
            <template #available="{ record }"><a-tag v-if="record.available" color="green" size="small">√</a-tag></template>
            <template #enabled="{ record }">
                <a-tag v-if="record.system" size="small" color="gray">核心内置</a-tag>
                <a-tag v-else-if="!record.available && !record.enabled" size="small" color="gray">无 DLL</a-tag>
                <a-switch v-else v-model="record.enabled" size="small"
                    @change="(v: any) => toggleExt(record, !!v)" />
            </template>
            <template #loaded="{ record }"><a-tag v-if="record.loaded" color="arcoblue" size="small">√</a-tag></template>
        </a-table>
        <div class="offline-tip">切换开关会写入 php.ini；重启实例后“已加载”才会刷新。</div>
    
        <a-divider style="margin: 12px 0" />
    
        <div class="ext-section-head">
            <span class="ext-section-title"><icon-cloud /> 在线可下载安装（PECL Windows）</span>
            <a-button size="mini" :loading="remoteExtsLoading" @click="loadRemoteExts(true)">
                <template #icon><icon-refresh /></template>强制刷新
            </a-button>
        </div>
        <a-spin :loading="remoteExtsLoading" style="width:100%">
            <a-table :data="remoteExts" size="small" :pagination="false" row-key="name" :scroll="{ y: 200 }"
                :columns="[
                    { title: '扩展', dataIndex: 'label', width: 110 },
                    { title: '说明', dataIndex: 'description', ellipsis: true, tooltip: true },
                    { title: '状态', dataIndex: 'st', width: 150, slotName: 'st' },
                    { title: '操作', dataIndex: 'ops', width: 110, slotName: 'ops' },
                ]">
                <template #st="{ record }">
                    <a-tag v-if="record.busy" color="arcoblue" size="small">安装中…</a-tag>
                    <a-tag v-else-if="record.installed" color="green" size="small">已安装{{ record.extVersion ? ' v'+record.extVersion : '' }}</a-tag>
                    <a-tag v-else-if="record.fetching" size="small" color="gray">获取目录中…</a-tag>
                    <a-tag v-else-if="record.matchable" color="orange" size="small">可下载{{ record.extVer ? ' v'+record.extVer : '' }}</a-tag>
                    <a-tag v-else-if="record.seriesSeen" color="gray" size="small">当前 PHP 版本无匹配</a-tag>
                    <a-tooltip v-else-if="record.reason" :content="record.reason">
                        <a-tag size="small" color="gray">不可用</a-tag>
                    </a-tooltip>
                    <a-tag v-else size="small" color="gray">暂无</a-tag>
                </template>
                <template #ops="{ record }">
                    <a-space :size="4">
                        <a-button v-if="record.matchable && !record.installed" size="mini" type="primary"
                            :disabled="record.busy" @click="installRemoteExt(record.name)">安装并启用</a-button>
                        <a-button v-if="record.downloaded && !record.busy" size="mini" status="danger"
                            @click="removeRemoteExt(record.name)">删除</a-button>
                    </a-space>
                </template>
            </a-table>
        </a-spin>
        <div class="offline-tip">DLL 将安装到 PHP 版本 ext 目录；删除仅针对在线获取版本，内置 DLL 不可删除。安装完成后请重启实例生效。</div>
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
                <a-button type="primary" @click="addDisFns(disfnInput.split(/[,，\s]+/))">添加</a-button>
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

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

interface InstConfig { id: string; name: string; version: string; port: number; nts: boolean; docRoot: string; router: string; enableLogs: boolean }

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
let offInstall: any = null;
let offSelInst: any = null;

const selCfg = computed(() => instances.value.find((i) => i.id === selectedId.value) || null);
const selState = computed(() => (selectedId.value ? states[selectedId.value] : null));

const instForm = reactive<InstConfig>({
    id: "", name: "", version: "", port: 8088, nts: true, docRoot: "", router: "", enableLogs: true,
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

/* ============================ 小工具 ============================ */
function srcLabel(s: string): string {
    return s === "bundled" ? "内置" : s === "downloaded" ? "下载" : "本机";
}
function stageText(p: any): string {
    if (!p) return "";
    if (p.stage === "downloading") return `下载中 ${p.percent || 0}%`;
    if (p.stage === "extracting") return "解压中";
    if (p.stage === "done") return "完成";
    if (p.stage === "info") return "准备中";
    return "失败";
}
function fmtBytes(b: number): string {
    if (!b) return "-";
    const units = ["B", "KB", "MB", "GB"]; let i = 0; let v = b;
    while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
    return v.toFixed(i > 0 ? 1 : 0) + " " + units[i];
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
async function loadInstances() {
    try { instances.value = (await GoTools.PhpSrvListInstances()) || []; }
    catch (e) { instances.value = []; }
    if (!selectedId.value && instances.value.length) selectInst(instances.value[0].id);
    refreshStates();
}
async function refreshStates() {
    for (const inst of instances.value) {
        try {
            states[inst.id] = await GoTools.PhpSrvStatus(inst.id);
            if (selectedId.value === inst.id && !logTimer) startLogPolling();
        } catch (e) { /* ignore */ }
    }
}
function selectInst(id: string) {
    selectedId.value = id;
    logLines.value = [];
    startLogPolling();
    refreshStates();
}

/* ============================ 日志轮询 ============================ */
async function pollLogs() {
    if (!selectedId.value) return;
    try {
        const lines = (await GoTools.PhpSrvLogs(selectedId.value, 300)) || [];
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

/* ============================ 实例操作 ============================ */
async function doStart() {
    const c = selCfg.value; if (!c) return;
    try { await GoTools.PhpSrvStart(c.id); Message.success(`实例 ${c.name} 已启动`); }
    catch (e: any) { Message.error("启动失败: " + (e?.message || e)); }
    refreshStates();
}
async function doStop() {
    const c = selCfg.value; if (!c) return;
    try { await GoTools.PhpSrvStop(c.id); Message.success(`实例 ${c.name} 已停止`); }
    catch (e: any) { Message.error("停止失败: " + (e?.message || e)); }
    refreshStates();
}
async function doRestart() {
    const c = selCfg.value; if (!c) return;
    try { await GoTools.PhpSrvRestart(c.id); Message.success(`实例 ${c.name} 已重启`); }
    catch (e: any) { Message.error("重启失败: " + (e?.message || e)); }
    refreshStates();
}
function openSite(port: number) {
    window.open(`http://127.0.0.1:${port}/`, "_blank");
}
async function onInstAction(v: string, inst: InstConfig) {
    if (v === "start") {
        try { await GoTools.PhpSrvStart(inst.id); Message.success(`实例 ${inst.name} 已启动`); }
        catch (e: any) { Message.error("启动失败: " + (e?.message || e)); }
        refreshStates();
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

function openInstModal(inst?: InstConfig) {
    if (inst) { Object.assign(instForm, { ...inst }); }
    else {
        Object.assign(instForm, { id: "", name: "", version: localVersions.value[0]?.version || "", port: 8088, nts: true, docRoot: "", router: "", enableLogs: true });
    }
    if (localVersions.value.length === 0) loadLocalVersions();
    instVisible.value = true;
}
async function checkPortTaken() {
    if (!instForm.port) return;
    try {
        const suggest = await GoTools.PhpSrvSuggestPort(instForm.port);
        if (suggest && suggest !== instForm.port) Message.warning(`端口 ${instForm.port} 已被占用，建议改为 ${suggest}`);
    } catch (e) { /* ignore */ }
}
async function chooseDocRoot() {
    try { const dir = await GoTools.PhpSrvChooseDir(); if (dir) instForm.docRoot = dir; }
    catch (e) { /* cancel */ }
}
async function saveInstance(): Promise<boolean> {
    if (!instForm.name) { Message.warning("请填写实例名称"); return false; }
    if (!instForm.version) { Message.warning("请选择 PHP 版本"); return false; }
    try {
        await GoTools.PhpSrvSaveInstance({ ...instForm });
        Message.success("已保存"); await loadInstances(); return true;
    } catch (e: any) { Message.error("保存失败: " + (e?.message || e)); return false; }
}

/* ============================ 版本管理 ============================ */
async function openVersionsModal() {
    versionsVisible.value = true;
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
            h("div", { style: "margin-top:8px;color:var(--color-text-3);font-size:12px" },
                "可稍后重试；或将对应 php zip 放入以下插件目录后刷新本地版本，离线安装。"),
            h("div", {
                style: "margin-top:6px;padding:6px 8px;border-radius:4px;background:var(--color-fill-2);" +
                    "font-family:Consolas,Menlo,monospace;font-size:12px;word-break:break-all;color:var(--color-text-2)",
            }, pluginsDirVal.value || "插件目录加载中…"),
        ]),
        onClose: () => { errModalOpen = false; },
    });
}
function removeVersion(v: any) {
    Modal.warning({
        title: "删除版本",
        content: `确定删除 PHP ${v.version}？若仍有运行中的实例引用该版本会被拒绝。`,
        hideCancel: false,
        onOk: async () => {
            try { await GoTools.PhpSrvRemoveVersion(v.version); Message.success("已删除"); loadLocalVersions(); }
            catch (e: any) { Message.error("删除失败: " + (e?.message || e)); }
        },
    });
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

/* ============================ 生命周期 ============================ */
onMounted(async () => {
    await loadInstances();
    // 主页点击服务实例进入（?inst=）：选中该实例展示详情
    const wantId = String(route.query.inst ?? "");
    if (wantId && instances.value.some((i) => i.id === wantId)) selectInst(wantId);
    loadLocalVersions();
    srvTimer = setInterval(refreshStates, 3000);
    // 单例窗口已打开时，主页再次点击其他实例 → 通过事件切换
    offSelInst = Events.On("tool:select-inst", (ev: any) => {
        const p = ev?.data ?? ev;
        if (p?.type !== "php" || !p?.id) return;
        if (instances.value.some((i) => i.id === String(p.id))) selectInst(String(p.id));
    });
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
    background: var(--color-fill-1);
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
