<template>
    <a-row class="nginx-page" :gutter="6">
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
                <common-docker-off class="srv-docker-off" :count="dockerInstCount" tool-label="Nginx" />
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
                                    : `镜像 ${inst.image}：这就是该实例的 Nginx 版本`">
                                <a-tag :color="imgMissing(inst.image) ? 'orange' : 'purple'" size="small">Docker
                                    {{ imgTag(inst.image) }}</a-tag>
                            </a-tooltip>
                            <a-tag v-else color="arcoblue" size="small">v{{ inst.version }}</a-tag>
                        </a-space>
                        <template #actions>
                            <a-dropdown trigger="hover" @select="(v: any) => onInstAction(v, inst)">
                                <icon-more class="conn-more" @click.stop />
                                <template #content>
                                    <a-doption v-if="!states[inst.id]?.running" value="start"><icon-play-arrow />
                                        启动</a-doption>
                                    <a-doption v-else value="stop"><icon-pause /> 停止</a-doption>
                                    <a-doption value="restart"><icon-refresh /> 重启</a-doption>
                                    <a-doption value="reload"><icon-sync /> 重载配置</a-doption>
                                    <a-doption value="check"><icon-safe /> 配置检查</a-doption>
                                    <a-doption value="conf"><icon-settings /> nginx.conf</a-doption>
                                    <a-doption value="open"><icon-launch /> 打开站点</a-doption>
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
                <a-empty description="从左侧选择一个 Nginx 实例查看状态与日志" />
            </a-card>
            <a-card v-else class="main-panel card-head-tool" :body-style="{ padding: 0 }">
                <template #title>
                    <a-space>
                        <com-badge :status="selState?.running ? 'success' : 'normal'" :animated="!!selState?.running" />
                        <span>{{ selCfg?.name }}</span>
                        <a-tooltip v-if="isDockerSel" :content="selState?.image || selCfg?.image || ''">
                            <a-tag color="purple" size="small">Docker {{ imgTag(selState?.image || selCfg?.image || '') }}</a-tag>
                        </a-tooltip>
                        <a-tag v-else color="arcoblue" size="small">v{{ selState?.version || selCfg?.version }}</a-tag>
                        <a-tag color="arcoblue" size="small">:{{ selCfg?.port }}</a-tag>
                        <a-tag v-if="selCfg?.sslPort" color="orange" size="small">https :{{ selCfg.sslPort }}</a-tag>
                    </a-space>
                </template>
                <template #extra>
                    <a-space>
                        <a-button size="small" :disabled="selState?.running || stopBusy"
                            @click="doStart">启动</a-button>
                        <!-- 停止会断开正在服务的请求，先气泡确认；按钮转 loading 显示「停止中」 -->
                        <a-popconfirm :content="`停止实例「${selCfg?.name}」？正在处理的请求会中断。`" type="warning"
                            position="lt" :disabled="!selState?.running || stopBusy" @ok="doStop">
                            <a-button size="small" status="danger" :disabled="!selState?.running || stopBusy"
                                :loading="stopBusy">{{ stopBusy ? '停止中' : '停止' }}</a-button>
                        </a-popconfirm>
                        <a-button size="small" :disabled="stopBusy" @click="doRestart">重启</a-button>
                        <a-tooltip
                            :content="isDockerSel ? '容器模式没有 nginx -s reload：重载 = 重启容器（会短暂中断），新配置随之生效' : 'nginx -s reload 平滑重载配置'">
                            <a-button size="small" :disabled="!selState?.running || stopBusy" @click="doReload">重载</a-button>
                        </a-tooltip>
                    </a-space>
                </template>
                <div class="detail-grid">
                    <div class="ls-item"><span>运行模式</span>
                        <b>{{ selState?.mode === 'docker' ? 'Docker 容器' : '本地进程' }}</b>
                    </div>
                    <div class="ls-item"><span>{{ isDockerSel ? '镜像' : 'Nginx 版本' }}</span><b>{{ selState?.version || selCfg?.version || '-' }}</b>
                    </div>
                    <div class="ls-item"><span>运行状态</span>
                        <b class="com-state" :class="selState?.running ? 'ok' : 'no'">
                            <com-badge :status="selState?.running ? 'success' : 'normal'"
                                :animated="!!selState?.running" />
                            {{ selState?.running ? '运行中' : '已停止' }}
                        </b>
                    </div>
                    <!-- 容器里的 nginx 进程在容器 namespace，本机看不到 master PID 与 worker 数：改成报容器状态 -->
                    <div class="ls-item" v-if="isDockerSel"><span>容器</span>
                        <a-tooltip :content="'容器 ID：' + (selState?.containerId || '-') + '（状态 ' + (selState?.containerState || '-') + '）'">
                            <b class="ellipsis">{{ selState?.container || '-' }}</b>
                        </a-tooltip>
                    </div>
                    <div class="ls-item" v-else><span>Master PID</span><b>{{ selState?.pid || '-' }}</b></div>
                    <div class="ls-item" v-if="!isDockerSel"><span>Worker 数</span><b>{{ selState?.workers ?? '-' }}</b></div>
                    <div class="ls-item" v-else><span>Worker 数<a-tooltip
                                content="容器内的 nginx 进程不在本机进程列表里，从容器外统不到">
                                <icon-question-circle class="ls-label-tip" />
                            </a-tooltip></span><b>-</b>
                    </div>
                    <div class="ls-item"><span>运行时长</span><b>{{ fmtUptimeS(selState?.uptimeSeconds) }}</b></div>
                    <div class="ls-item"><span>req/s（估算）</span><b>{{ selState?.requestsPerSec ? selState.requestsPerSec.toFixed(2) : '-' }}</b></div>
                    <div class="ls-item"><span>站点目录</span>
                        <a-tooltip :content="selState?.docRoot || '-'">
                            <b class="ellipsis">{{ selState?.docRoot || '-' }}</b>
                        </a-tooltip>
                    </div>
                    <div class="ls-item"><span>nginx.conf</span>
                        <a-tooltip :content="selState?.configPath || '-'">
                            <b class="ellipsis">{{ selState?.configPath || '-' }}</b>
                        </a-tooltip>
                    </div>
                    <div class="ls-item">
                        <span>运行目录<a-tooltip content="启动时 -p prefix 指定的目录：其中 conf/ 与 logs/ 与之相对">
                                <icon-question-circle class="ls-label-tip" />
                            </a-tooltip></span>
                        <a-tooltip :content="selState?.prefix || '-'">
                            <b class="ellipsis">{{ selState?.prefix || '-' }}</b>
                        </a-tooltip>
                    </div>
                </div>
                <div class="log-head">
                    <span>错误/访问日志（实时）</span>
                    <a-space :size="6">
                        <a-button size="small" :disabled="!selState?.running" @click="openSite(selCfg?.port ?? 0)">
                            <template #icon><icon-launch /></template>打开站点
                        </a-button>
                        <a-tooltip
                            :content="isDockerSel ? '容器模式：重载 = 重启容器（新配置随之生效）' : 'nginx -s reload 平滑重载配置'">
                            <a-button size="small" :disabled="!selState?.running" @click="doReload">
                                <template #icon><icon-sync /></template>重载
                            </a-button>
                        </a-tooltip>
                        <a-button size="small" @click="openConf(selectedId)">
                            <template #icon><icon-settings /></template>nginx.conf
                        </a-button>
                        <a-button size="small" @click="doCheck(selectedId)">
                            <template #icon><icon-safe /></template>配置检查
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
    <a-modal v-model:visible="instVisible" :title="instForm.id ? '编辑实例' : '新建实例'" :width="520"
        :body-style="{ maxHeight: '72vh' }" @before-ok="saveInstance">
        <!-- 引擎没起时先把原因说清楚，否则要到启动那一步才报错；吸顶不占表单位 -->
        <common-modal-tip v-if="instForm.mode === 'docker' && dockerErr" :text="dockerErr">
          <a-button size="small" @click="openDockerWindow">打开 Docker 面板</a-button>
        </common-modal-tip>
        <a-form :model="instForm" layout="vertical" class="inst-form">
            <a-form-item field="name" label="实例名称" required>
                <a-input v-model="instForm.name" placeholder="如 site-dev" />
            </a-form-item>
            <a-form-item field="mode" label="运行模式">
                <a-radio-group size="medium" v-model="instForm.mode" type="button" @change="onModeChange">
                    <a-radio value="local"><icon-computer /> 本地</a-radio>
                    <a-radio value="docker"><icon-experiment /> Docker</a-radio>
                </a-radio-group>
                <template #extra>
                    <template v-if="instForm.mode === 'docker'">
                        容器里跑 nginx：实例的 conf/ 与 logs/ 以及站点目录都挂载进容器，改配置后点「重载」（重启容器）生效；首次启动会自动拉镜像
                    </template>
                    <template v-else>使用本机插件目录下的 nginx.exe，可在「版本管理」获取</template>
                </template>
            </a-form-item>
            <!-- 两种模式的行数和列宽对齐（Docker 只多一个容器名），切模式时弹窗高度不跳 -->
            <a-row :gutter="12">
                <a-col :span="14">
                    <a-form-item v-if="instForm.mode !== 'docker'" field="version" label="Nginx 版本" required
                        tooltip="需已安装，可在「版本管理」获取">
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
                <a-col :span="10">
                    <a-form-item field="port" label="HTTP 端口" :error="portError"
                        :tooltip="instForm.mode === 'docker' ? '宿主机发布端口（只绑 127.0.0.1）；容器内固定监听 80，改这里不动配置文件' : 'nginx.conf 里的 listen 端口'">
                        <a-input-number v-model="instForm.port" :min="1" :max="65535" @change="portError = ''"
                            @blur="checkPortTaken" />
                    </a-form-item>
                </a-col>
            </a-row>
            <a-row :gutter="12">
                <a-col v-if="instForm.mode === 'docker'" :span="8">
                    <a-form-item field="containerName" label="容器名" tooltip="留空自动生成 go-tools-nginx-实例ID">
                        <a-input v-model="instForm.containerName" placeholder="留空自动生成" />
                    </a-form-item>
                </a-col>
                <a-col :span="instForm.mode === 'docker' ? 8 : 12">
                    <a-form-item field="sslPort" label="HTTPS 端口" :tooltip="instForm.mode === 'docker' ? '0 = 不启用；>0 时容器内监听 443 并发布到该端口，证书放到实例 conf 目录的 server.crt/server.key' : '0 = 不启用 HTTPS'"
                        :error="sslPortError">
                        <a-input-number v-model="instForm.sslPort" :min="0" :max="65535"
                            @change="sslPortError = ''" @blur="checkSslPortTaken" />
                    </a-form-item>
                </a-col>
                <a-col :span="instForm.mode === 'docker' ? 8 : 12">
                    <a-form-item field="workers" label="worker_processes" tooltip="0 = auto（按 CPU 核数自动）">
                        <a-input-number v-model="instForm.workers" :min="0" :max="64" />
                    </a-form-item>
                </a-col>
            </a-row>
            <a-form-item field="docRoot" label="站点根目录"
                :tooltip="instForm.mode === 'docker' ? '留空 = 程序目录 data/www/实例名；该目录会挂载到容器 /usr/share/nginx/html' : '留空 = 程序目录 data/www/实例名'">
                <a-input-search v-model="instForm.docRoot" placeholder="如 D:\www\site" search-button
                    button-text="选择目录" @click-btn="chooseDocRoot" />
            </a-form-item>
            <a-form-item field="stubStatus" label="开放 /stub_status 状态页" tooltip="开启后可用工具面板读取 nginx 连接数/请求数">
                <a-switch v-model="instForm.stubStatus" />
            </a-form-item>
        </a-form>
    </a-modal>

    <!-- 版本管理 -->
    <!-- 固定高度：三个页签内容高矮不一，不钉住的话切一下弹窗就跳一次高度 -->
    <a-modal v-model:visible="versionsVisible" title="Nginx 版本管理" :width="780" :footer="false" unmount-on-close
        :body-style="{ padding: '12px 16px', height: 'min(600px, calc(100vh - 220px))', overflowY: 'auto' }">
        <!-- 类型切换：本地 / 线上 / Docker 只看一类，不用在长弹窗里找 -->
        <a-radio-group class="ver-tab" size="medium" v-model="verTab" type="button">
            <a-radio value="local">本地版本 {{ localVersions.length }}</a-radio>
            <a-radio value="remote">线上可安装 {{ remoteVersions.length }}</a-radio>
            <a-radio value="docker">Docker 实例 {{ dockerInstCount }}</a-radio>
        </a-radio-group>
        <div v-show="verTab === 'local'" class="ver-block">
            <div class="ver-title">本地可用版本</div>
            <a-table :data="localVersions" size="small" :pagination="false" row-key="version" :scroll="{ x: '100%', y: 200 }"
                :columns="[
                    { title: '版本', dataIndex: 'version' },
                    { title: '来源', dataIndex: 'source', width: 90, slotName: 'source' },
                    { title: '路径', dataIndex: 'path', ellipsis: true, tooltip: true },
                    { title: '操作', dataIndex: 'ops', width: 90, slotName: 'ops', fixed: 'right', align: 'right' },
                ]">
                <template #source="{ record }">
                    <a-tag size="small" :color="record.source === 'bundled' ? 'purple' : record.source === 'downloaded' ? 'green' : record.source === 'system' ? 'gray' : 'orange'">{{ srcLabel(record.source) }}</a-tag>
                </template>
                <template #ops="{ record }">
                    <a-popconfirm :content="`确定删除 Nginx ${record.version}？若仍有实例引用该版本会被拒绝。`"
                        type="warning" position="lt" :disabled="record.source === 'bundled' || record.source === 'system'"
                        @ok="removeVersion(record)">
                        <a-button size="small" status="danger" :disabled="record.source === 'bundled' || record.source === 'system'">删除</a-button>
                    </a-popconfirm>
                </template>
            </a-table>
            <a-row class="add-local" :gutter="8">
                <a-col :span="16">
                    <a-input v-model="localDirInput" placeholder="注册本机已有 Nginx：填写 nginx 所在目录" />
                </a-col>
                <a-col :span="4">
                    <a-button size="small" block @click="chooseLocalDir">浏览…</a-button>
                </a-col>
                <a-col :span="4">
                    <a-button size="small" block @click="addLocalDir">添加</a-button>
                </a-col>
            </a-row>
        </div>
        <!-- Docker 镜像与容器：Docker 实例的「版本」就是镜像 tag（nginx:1.27-alpine） -->
        <div v-show="verTab === 'docker'">
            <docker-images keyword="nginx" tool-label="Nginx" repo="nginx" :presets="nginxImagePresets"
                :instances="instances" :states="states" @create="newInstanceFromImage" @use="editInstanceById"
                @changed="refreshStates" />
        </div>
        <div v-show="verTab === 'remote'" class="ver-block">
            <div class="ver-title">
                线上版本
                <a-button size="small" :loading="remoteLoading" @click="loadRemoteVersions">刷新</a-button>
            </div>
            <a-spin :loading="remoteLoading" style="width:100%">
                <a-table :data="remoteVersions" size="small" :pagination="false" row-key="version" :scroll="{ x: '100%', y: 220 }"
                    :columns="[
                        { title: '版本', dataIndex: 'version', width: 110 },
                        { title: '产品线', dataIndex: 'release', width: 90 },
                        { title: '包大小', dataIndex: 'size', width: 100, slotName: 'size' },
                        { title: '状态', dataIndex: 'st', width: 150, slotName: 'st' },
                        { title: '操作', dataIndex: 'ops', width: 100, slotName: 'ops', fixed: 'right', align: 'right' },
                    ]">
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
                            :disabled="!!installProgress[record.version]" @click="installVersion(record)">安装</a-button>
                        <a-button v-else size="small" @click="loadLocalVersions">已安装</a-button>
                    </template>
                </a-table>
            </a-spin>
            <div class="offline-tip">离线安装：将 nginx zip 放入 <a-link copyable :content="pluginsDirVal">{{ pluginsDirVal || '…' }}</a-link> 后刷新本地版本</div>
        </div>
        <div v-if="installLogs.length" class="ver-block install-log">
            <div class="ver-title">安装日志</div>
            <div class="ls-log small">
                <div v-for="(l, i) in installLogs" :key="i" class="ls-line">{{ l }}</div>
            </div>
        </div>
    </a-modal>

    <!-- nginx.conf 文本编辑 -->
    <a-modal v-model:visible="confVisible" :title="`nginx.conf - ${cfgNameOf(confInstId)}`" :width="760">
        <a-textarea v-model="confText" :auto-size="{ minRows: 16, maxRows: 24 }" spellcheck="false" />
        <template #footer>
            <div class="ini-tip">{{ confInstDocker ? '容器模式跑不了 nginx -t：保存后点「重载」（重启容器）生效，起不来时看日志。' : '保存前自动备份并执行配置检查，检查失败将回滚。' }}</div>
            <a-space>
                <a-button size="small" @click="confVisible = false">取消</a-button>
                <a-button size="small" @click="saveConfText">保存</a-button>
            </a-space>
        </template>
    </a-modal>

    <!-- 配置检查结果 -->
    <a-modal v-model:visible="checkVisible" title="nginx -t 配置检查" :width="600" :footer="false">
        <pre class="check-out">{{ checkOutput }}</pre>
    </a-modal>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, nextTick, onMounted, onUnmounted, reactive, ref } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import { useRoute } from "vue-router";
import { useToolWindow } from "@/views/index/useToolWindow";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

interface InstConfig {
    id: string; name: string; version: string; port: number; sslPort: number; docRoot: string; workers: number; stubStatus: boolean;
    // mode: local | docker；容器模式不占本机版本库，靠 image 定位镜像
    mode: string; image: string; containerName: string;
}

/** Docker 实例的版本就是镜像 tag：列表里只展示 tag（1.27-alpine），完整镜像名走 tooltip */
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
let logTimer: any = null;
let srvTimer: any = null;
const route = useRoute();
let offInstall: any = null;
let offSelInst: any = null;

const selCfg = computed(() => instances.value.find((i) => i.id === selectedId.value) || null);
const selState = computed(() => (selectedId.value ? states[selectedId.value] : null));
/** 当前实例是不是容器模式：配置优先（列表里就能看到刚改完还没启动的实例），状态兜底 */
const isDockerSel = computed(() => (selCfg.value?.mode || selState.value?.mode) === "docker");

const instForm = reactive<InstConfig>({
    id: "", name: "", version: "", port: 8090, sslPort: 0, docRoot: "", workers: 0, stubStatus: true,
    mode: "local", image: "", containerName: "",
});
/** 端口占用提示：HTTP/HTTPS 各一份，贴表单项底下（红字） */
const portError = ref("");
const sslPortError = ref("");
const instVisible = ref(false);

const versionsVisible = ref(false);
/** 版本管理弹窗的类型切换：本地 / 线上 / Docker 只看一类 */
const verTab = ref<"local" | "remote" | "docker">("local");
/** 有多少实例跑在容器里（它们的版本由镜像 tag 决定，不占本机版本库） */
const dockerInstCount = computed(() => instances.value.filter((i) => i.mode === "docker").length);
/** 版本管理里 Docker 镜像的常用 tag：alpine 体积小、且镜像内目录约定完整 */
const nginxImagePresets = ["nginx:alpine", "nginx:stable-alpine", "nginx:1.27-alpine", "nginx:1.26-alpine", "nginx:alpine-slim", "nginx:1.27"];
const localVersions = ref<any[]>([]);
const remoteVersions = ref<any[]>([]);
const remoteLoading = ref(false);
const installProgress = reactive<Record<string, any>>({});
const installLogs = ref<string[]>([]);
const localDirInput = ref("");
const pluginsDirVal = ref("");

const confVisible = ref(false);
const confInstId = ref("");
const confText = ref("");
/** 配置弹窗里的提示按模式分流：容器模式既不能 nginx -t 也不会回滚 */
const confInstDocker = computed(() => instances.value.find((i) => i.id === confInstId.value)?.mode === "docker");

/* Docker 模式表单：镜像候选只列本机已有的 nginx 系镜像（不猜预置 tag），为空就引导去拉 */
/* 名字不能回退成 dockerImages：script setup 下 kebab 标签 docker-images 会优先拿同名 setup 变量当组件，整个 Docker 区会渲染成空白 */
const dockerImageList = ref<string[]>([]);
const dockerImagesLoading = ref(false);
const dockerErr = ref("");
const imageOptions = computed(() => Array.from(new Set<string>(dockerImageList.value)));

/** 只刷新镜像清单（不写表单）：用来标记实例引用的镜像本机是否还在 */
async function probeDockerImages() {
    try { dockerImageList.value = (await GoTools.NginxSrvDockerImages()) || []; }
    catch (e) { /* 引擎未就绪就不做校验，免得误报 */ }
}
/** 镜像清单非空且不含它 → 悬空引用（镜像被删了，实例还指着） */
function imgMissing(image: string): boolean {
    return !!image && dockerImageList.value.length > 0 && !dockerImageList.value.includes(image);
}

const { openToolWindow } = useToolWindow();

const checkVisible = ref(false);
const checkOutput = ref("");

const versionOptions = computed(() => localVersions.value.map((v: any) => ({ label: `${v.version}（${srcLabel(v.source)}）`, value: v.version })));
const installedSet = computed(() => new Set<string>(localVersions.value.map((v: any) => v.version)));

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
function fmtBytes(b: number): string {
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
function cfgNameOf(id: string): string {
    return instances.value.find((i) => i.id === id)?.name || id;
}
function pushInstallLog(txt: string) {
    if (!txt) return;
    const t = new Date();
    const pad = (n: number) => n.toString().padStart(2, "0");
    installLogs.value.push(`[${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}] ${txt}`);
    if (installLogs.value.length > 200) installLogs.value.splice(0, installLogs.value.length - 200);
}

/* ============================ 数据加载 ============================ */
async function loadLocalVersions() {
    try { localVersions.value = (await GoTools.NginxSrvListVersions()) || []; }
    catch (e) { localVersions.value = []; }
}
async function loadInstances() {
    try { instances.value = (await GoTools.NginxSrvListInstances()) || []; }
    catch (e) { instances.value = []; }
    if (!selectedId.value && instances.value.length) selectInst(instances.value[0].id);
    refreshStates();
}
async function refreshStates() {
    // 并发问：实例串行 await 就是 N 次往返，Web 模式跑在远程服务器上时尤其明显
    await Promise.all(instances.value.map(async (inst) => {
        try {
            states[inst.id] = await GoTools.NginxSrvStatus(inst.id);
            if (selectedId.value === inst.id && !logTimer) startLogPolling();
        } catch (e) { /* 单个实例失败不影响其它实例 */ }
    }));
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
        const lines = (await GoTools.NginxSrvLogs(selectedId.value, 300)) || [];
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
/** 停止中：停止要等守护进程退出（容器模式更慢），期间锁定按钮防连点 */
const stopBusy = ref(false);
async function doStart() {
    const c = selCfg.value; if (!c) return;
    try { await GoTools.NginxSrvStart(c.id); Message.success(`实例 ${c.name} 已启动`); }
    catch (e: any) { Message.error("启动失败: " + (e?.message || e)); }
    refreshStates();
}
async function doStop() {
    const c = selCfg.value; if (!c) return;
    if (stopBusy.value) return;
    stopBusy.value = true;
    try { await GoTools.NginxSrvStop(c.id); Message.success(`实例 ${c.name} 已停止`); }
    catch (e: any) { Message.error("停止失败: " + (e?.message || e)); }
    finally { stopBusy.value = false; }
    refreshStates();
}
async function doRestart() {
    const c = selCfg.value; if (!c) return;
    try { await GoTools.NginxSrvRestart(c.id); Message.success(`实例 ${c.name} 已重启`); }
    catch (e: any) { Message.error("重启失败: " + (e?.message || e)); }
    refreshStates();
}
async function doReload() {
    const c = selCfg.value; if (!c) return;
    const docker = (states[c.id]?.mode || c.mode) === "docker";
    try { await GoTools.NginxSrvReload(c.id); Message.success(docker ? "容器已重启，新配置已生效" : "配置已平滑重载"); }
    catch (e: any) { Message.error("重载失败: " + (e?.message || e)); }
    refreshStates();
}
async function doCheck(id: string) {
    try {
        checkOutput.value = await GoTools.NginxSrvCheckConfig(id);
    } catch (e: any) {
        checkOutput.value = String(e?.message || e);
    }
    checkVisible.value = true;
}
function openSite(port: number) {
    window.open(`http://127.0.0.1:${port}/`, "_blank");
}
async function onInstAction(v: string, inst: InstConfig) {
    if (v === "start") {
        try { await GoTools.NginxSrvStart(inst.id); Message.success(`实例 ${inst.name} 已启动`); }
        catch (e: any) { Message.error("启动失败: " + (e?.message || e)); }
        refreshStates();
    } else if (v === "stop") {
        try { await GoTools.NginxSrvStop(inst.id); Message.success(`实例 ${inst.name} 已停止`); }
        catch (e: any) { Message.error("停止失败: " + (e?.message || e)); }
        refreshStates();
    } else if (v === "restart") {
        try { await GoTools.NginxSrvRestart(inst.id); Message.success(`实例 ${inst.name} 已重启`); }
        catch (e: any) { Message.error("重启失败: " + (e?.message || e)); }
        refreshStates();
    } else if (v === "reload") {
        doReload();
    } else if (v === "check") {
        doCheck(inst.id);
    } else if (v === "conf") {
        openConf(inst.id);
    } else if (v === "open") {
        openSite(inst.port);
    } else if (v === "logs") {
        selectInst(inst.id);
    } else if (v === "edit") {
        openInstModal(inst);
    } else if (v === "del") {
        Modal.warning({
            title: "删除实例", content: `确定删除实例「${inst.name}」？运行中会先停止；站点目录保留。`, hideCancel: false,
            onOk: async () => {
                try {
                    await GoTools.NginxSrvRemoveInstance(inst.id);
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
    sslPortError.value = "";
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
        // 默认端口也要能用：避开其它实例已登记 / 系统已监听的
        let port = 8090;
        try { port = (await GoTools.NginxSrvSuggestPort(8090)) || port; } catch (e) { /* ignore */ }
        Object.assign(instForm, { id: "", name: "", version: localVersions.value[0]?.version || "", port, sslPort: 0, docRoot: "", workers: 0, stubStatus: true, mode: "local", image: "", containerName: "" });
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

/** 版本管理里点「新建实例」：直接以该镜像开一个 Docker 实例 */
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

async function loadImageOptions() {
    dockerImagesLoading.value = true;
    try { dockerImageList.value = (await GoTools.NginxSrvDockerImages()) || []; }
    catch (e) { dockerImageList.value = []; } // 引擎不可用时当作没有镜像，走“去安装”引导
    dockerImagesLoading.value = false;
    // 还没选值且有候选时先填第一个，省一步点击
    if (instForm.mode === "docker" && !instForm.image && dockerImageList.value.length) instForm.image = dockerImageList.value[0];
}

async function loadDockerEngine() {
    try {
        const [ok, msg] = await GoTools.NginxSrvDockerAvailable();
        dockerErr.value = ok ? "" : (msg || "Docker 引擎不可用");
    } catch (e: any) { dockerErr.value = e?.message || String(e); }
}

/** 打开 Docker 管理窗口（独立 webview，单例复用）：配镜像时直接过去拉，不用回主页找入口 */
function openDockerWindow() {
    openToolWindow("/tools/docker", "Docker管理", { single: true, width: 1200, height: 800 });
}
/** HTTP 端口校验；顺带把 HTTPS 与它撞车的情况也标出来 */
async function checkPortTaken() {
    if (!instForm.port) { portError.value = ""; return; }
    try {
        const reason = await GoTools.NginxSrvPortTaken(instForm.port, instForm.id || "");
        if (!reason) { portError.value = ""; }
        else {
            const suggest = await GoTools.NginxSrvSuggestPort(instForm.port);
            portError.value = suggest && suggest !== instForm.port ? `${reason}，可用：${suggest}` : reason;
        }
    } catch (e) { /* ignore */ }
    if (instForm.sslPort && instForm.sslPort === instForm.port) sslPortError.value = "HTTPS 端口不能与 HTTP 端口相同";
    else if (sslPortError.value === "HTTPS 端口不能与 HTTP 端口相同") sslPortError.value = "";
}
async function checkSslPortTaken() {
    if (!instForm.sslPort) { sslPortError.value = ""; return; }
    if (instForm.sslPort === instForm.port) { sslPortError.value = "HTTPS 端口不能与 HTTP 端口相同"; return; }
    try {
        const reason = await GoTools.NginxSrvPortTaken(instForm.sslPort, instForm.id || "");
        if (!reason) { sslPortError.value = ""; return; }
        const suggest = await GoTools.NginxSrvSuggestPort(instForm.sslPort);
        sslPortError.value = suggest && suggest !== instForm.sslPort ? `${reason}，可用：${suggest}` : reason;
    } catch (e) { /* ignore */ }
}
async function chooseDocRoot() {
    try { const dir = await GoTools.NginxSrvChooseDir(); if (dir) instForm.docRoot = dir; }
    catch (e) { /* cancel */ }
}
async function saveInstance(): Promise<boolean> {
    if (!instForm.name) { Message.warning("请填写实例名称"); return false; }
    // 两种模式的必填项不一样：本地看本机版本库，容器看镜像（后端也会再校一次）
    if (instForm.mode === "docker") {
        if (!instForm.image) { Message.warning("Docker 模式请选择或输入镜像"); return false; }
    } else if (!instForm.version) { Message.warning("请选择 Nginx 版本"); return false; }
    if (!instForm.port) { Message.warning("请填写 HTTP 端口"); return false; }
    // 端口占用先在本地拦一次，后端保存时还会再校验
    try {
        const reason = await GoTools.NginxSrvPortTaken(instForm.port, instForm.id || "");
        if (reason) {
            portError.value = reason;
            Message.warning("HTTP " + reason);
            return false;
        }
        if (instForm.sslPort) {
            if (instForm.sslPort === instForm.port) {
                sslPortError.value = "HTTPS 端口不能与 HTTP 端口相同";
                Message.warning(sslPortError.value);
                return false;
            }
            const sslReason = await GoTools.NginxSrvPortTaken(instForm.sslPort, instForm.id || "");
            if (sslReason) {
                sslPortError.value = sslReason;
                Message.warning("HTTPS " + sslReason);
                return false;
            }
        }
    } catch (e) { /* 探测失败不阻断，后端兜底 */ }
    try {
        // 编辑前先记下原配置：切了版本/镜像且实例在跑，容器里还是旧版本，得重启才生效
        const old = instForm.id ? instances.value.find((i) => i.id === instForm.id) : null;
        const switched = !!old && (old.mode !== instForm.mode || old.image !== instForm.image || old.version !== instForm.version);
        const wasRunning = !!old && !!states[old.id]?.running;
        await GoTools.NginxSrvSaveInstance({ ...instForm });
        Message.success(instForm.mode === "docker"
            ? "已保存；容器里的 nginx.conf 默认监听 80/443，改这里只改宿主端口"
            : "已保存，首次启动会按表单生成 nginx.conf");
        await loadInstances();
        if (switched && wasRunning) setTimeout(() => askRestartAfterSwitch({ ...instForm }), 240);
        return true;
    } catch (e: any) { Message.error("保存失败: " + (e?.message || e)); return false; }
}

/** 切了版本/镜像又没重启：跑着的还是旧版本，问一句要不要现在就换过去 */
function askRestartAfterSwitch(inst: InstConfig) {
    const what = inst.mode === "docker" ? `镜像 ${inst.image}` : `Nginx ${inst.version}`;
    Modal.confirm({
        title: "版本已切换",
        content: `${what} 已写入配置，但实例「${inst.name}」还在跑旧版本；重启后才会生效（期间会短暂中断服务）。`,
        okText: "立即重启",
        cancelText: "稍后手动重启",
        onOk: async () => {
            try {
                await GoTools.NginxSrvRestart(inst.id);
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
        try { pluginsDirVal.value = (await GoTools.NginxSrvPluginsDir()) || ""; } catch (e) { /* ignore */ }
    }
    if (remoteVersions.value.length === 0) loadRemoteVersions();
}
async function loadRemoteVersions() {
    remoteLoading.value = true;
    try { remoteVersions.value = (await GoTools.NginxSrvRemoteVersions()) || []; }
    catch (e: any) { Message.error("获取远程版本失败: " + (e?.message || e)); }
    remoteLoading.value = false;
}
async function installVersion(r: any) {
    try {
        pushInstallLog(`提交安装请求：Nginx ${r.version}`);
        await GoTools.NginxSrvInstall(r.version);
        installProgress[r.version] = { stage: "downloading", percent: 0 };
    } catch (e: any) {
        pushInstallLog(`安装请求被拒绝：${e?.message || e}`);
        Message.error("安装失败: " + (e?.message || e));
    }
}
async function removeVersion(v: any) {
    try { await GoTools.NginxSrvRemoveVersion(v.version); Message.success("已删除"); loadLocalVersions(); }
    catch (e: any) { Message.error("删除失败: " + (e?.message || e)); }
}
async function chooseLocalDir() {
    try { const dir = await GoTools.NginxSrvChooseDir(); if (dir) localDirInput.value = dir; }
    catch (e) { /* cancel */ }
}
async function addLocalDir() {
    const dir = localDirInput.value.trim();
    if (!dir) { Message.warning("请先选择或填写目录"); return; }
    try { await GoTools.NginxSrvAddLocal(dir); Message.success("已添加本机目录"); localDirInput.value = ""; loadLocalVersions(); }
    catch (e: any) { Message.error("添加失败: " + (e?.message || e)); }
}

/* ============================ nginx.conf ============================ */
async function openConf(id: string) {
    confInstId.value = id;
    confText.value = "";
    confVisible.value = true;
    try { confText.value = (await GoTools.NginxSrvGetConfigText(id)) || ""; }
    catch (e: any) { Message.error("读取 nginx.conf 失败: " + (e?.message || e)); confVisible.value = false; }
}
async function saveConfText() {
    try {
        await GoTools.NginxSrvSaveConfigText(confInstId.value, confText.value);
        Message.success(confInstDocker.value
            ? "nginx.conf 已保存；容器模式点「重载」（重启容器）后生效"
            : "nginx.conf 已保存；运行中实例建议执行一次重载");
        confVisible.value = false;
    } catch (e: any) { Message.error("保存失败: " + (e?.message || e)); }
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
    srvTimer = setInterval(refreshStates, 3000);
    // 单例窗口已打开时，主页再次点击其他实例 → 通过事件切换
    offSelInst = Events.On("tool:select-inst", (ev: any) => {
        const p = ev?.data ?? ev;
        if (p?.type !== "nginx" || !p?.id) return;
        if (instances.value.some((i) => i.id === String(p.id))) selectInst(String(p.id));
    });
    offInstall = Events.On("nginx:install", (ev: any) => {
        const p = ev?.data ?? ev;
        if (!p || !p.version) return;
        if (p.stage === "info") {
            pushInstallLog(p.note || "");
        } else if (p.stage === "done") {
            delete installProgress[p.version];
            pushInstallLog(`安装完成 Nginx ${p.version}`);
            Message.success(`Nginx ${p.version} 安装完成`);
            loadLocalVersions();
        } else if (p.stage === "error") {
            installProgress[p.version] = p;
            pushInstallLog(`安装失败 ${p.version}：${p.note || "未知错误"}（可将 nginx zip 放入插件目录后离线安装）`);
            Modal.error({
                title: "安装失败",
                content: `${p.version}：${p.note || "未知错误"}。可将 nginx zip 放入插件目录后重试（离线安装）。`,
            });
        } else {
            installProgress[p.version] = p;
        }
    });
});
onUnmounted(() => {
    if (offInstall) offInstall();
    if (offSelInst) offSelInst();
    if (srvTimer) clearInterval(srvTimer);
    if (logTimer) clearInterval(logTimer);
});
</script>

<style scoped>
.nginx-page {
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

/* 版本管理弹窗的类型切换：与下方内容留一行间距（原本靠分隔线撑开的节奏） */
.ver-tab {
    display: flex;
    margin-bottom: 12px;
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

.ini-tip {
    float: left;
    font-size: 12px;
    color: var(--color-text-3);
    line-height: 32px;
}

.check-out {
    background: var(--color-fill-2);
    padding: 10px;
    border-radius: 4px;
    font-size: 12px;
    white-space: pre-wrap;
    word-break: break-all;
    margin: 0;
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
