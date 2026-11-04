<template>
    <a-row class="ls-page" :gutter="6">
        <!-- 左侧：服务状态与控制 -->
        <a-col :span="7" class="side-col">
            <a-row class="side-pages">
                <a-col class="side-cols" :span="24">
                    <a-card class="side-card card-head-tool" size="small" :body-style="{ padding: 0 }">
                        <template #title>互传服务</template>
                        <template #extra>
                            <span class="com-state" :class="srv.running ? 'is-on' : 'is-dim'">
                                <com-badge :status="srv.running ? 'success' : 'normal'" :animated="srv.running" />
                                {{ srv.running ? "运行中" : "已停止" }}
                            </span>
                        </template>
                        <div class="side-body">
                            <div class="field">
                                <div class="field-label">监听范围</div>
                                <a-select v-model="ctl.host" size="small" :disabled="srv.running">
                                    <a-option value="">全部网卡（局域网可访问）</a-option>
                                    <a-option value="127.0.0.1">仅本机回环</a-option>
                                </a-select>
                            </div>
                            <div class="field">
                                <div class="field-label">端口</div>
                                <a-input-number v-model="ctl.port" size="small" :min="0" :max="65535" :step="1"
                                    :precision="0" :disabled="srv.running" placeholder="0 = 默认（占用自动 +1）" />
                            </div>
                            <div class="field">
                                <div class="field-label">自动停止</div>
                                <a-select v-model="ctl.stopMin" size="small" :disabled="srv.running">
                                    <a-option :value="30">30 分钟后</a-option>
                                    <a-option :value="120">2 小时后</a-option>
                                    <a-option :value="0">不自动停</a-option>
                                </a-select>
                            </div>
                            <div class="field">
                                <div class="field-label">访问 PIN（可选）</div>
                                <a-input v-model="ctl.pin" size="small" :disabled="srv.running" allow-clear
                                    placeholder="留空 = 免密；仅挡无意访问，不是访问控制" />
                            </div>
                            <div class="field">
                                <div class="field-label">接收类型黑名单（留空 = 全类型收）</div>
                                <a-select v-model="ctl.blockExt" size="small" mode="tags" allow-clear
                                    :token-separators="[',', ' ', ';']" placeholder="如 exe、dll、ps1；回车添加"
                                    :disabled="cfgSaving" />
                                <a-space :size="6" class="blk-actions">
                                    <a-button size="mini" :disabled="cfgSaving" @click="fillDangerTypes">一键填危险类型</a-button>
                                    <a-button size="mini" :disabled="cfgSaving" @click="ctl.blockExt = []">清空（全收）</a-button>
                                    <a-button size="mini" type="primary" :loading="cfgSaving" @click="saveReceivePolicy">保存</a-button>
                                </a-space>
                                <span class="hint">命中黑名单的扩展名在接收时直接拒收；对已在跑的服务即时生效，无需重启。危险类型仅挡随手双击运行，不是访问控制。</span>
                            </div>

                            <a-space :size="8">
                                <a-button v-if="!srv.running" size="small" type="primary" :loading="busy"
                                    @click="start">
                                    <template #icon><icon-play-circle /></template>启动
                                </a-button>
                                <a-button v-else size="small" status="danger" :loading="busy" @click="stop">
                                    <template #icon><icon-pause-circle /></template>停止
                                </a-button>
                                <a-button size="small" @click="refreshAll">
                                    <template #icon><icon-refresh /></template>刷新
                                </a-button>
                                <a-button size="small" :disabled="!srv.running || !srv.urls.length"
                                    @click="openQr(srv.urls[0])">
                                    <template #icon><icon-qrcode /></template>二维码
                                </a-button>
                            </a-space>

                            <div v-if="srv.running" class="urls">
                                <div class="field-label">可访问地址（发给同一网络设备）</div>
                                <div v-for="u in srv.urls" :key="u" class="url-row">
                                    <span class="mono break">{{ u }}</span>
                                    <a-button size="mini" type="text" @click="copyText(u)">
                                        <template #icon><icon-copy /></template>
                                    </a-button>
                                </div>
                            </div>

                            <div class="kv">
                                <span class="k">端口</span><span class="v mono">{{ srv.running ? srv.port : "--" }}</span>
                            </div>
                            <div class="kv">
                                <span class="k">累计请求</span><span class="v">{{ srv.reqCount }}</span>
                            </div>
                            <div class="kv">
                                <span class="k">已收文件</span><span class="v">{{ srv.uploadCount }}</span>
                            </div>
                            <div class="kv" v-if="srv.running && srv.autoStopAt > 0">
                                <span class="k">将在</span><span class="v">{{ autoStopIn }}</span>
                            </div>
                            <div class="kv inbox">
                                <span class="k">收件箱</span>
                                <span class="v mono break" :title="srv.inbox">{{ srv.inbox || "--" }}</span>
                                <a-button v-if="caps.nativeWindows" size="mini" type="text" class="mini-btn"
                                    :disabled="!srv.inbox" title="在资源管理器中打开收件箱目录" @click="revealInbox()">
                                    <template #icon><icon-folder /></template>
                                </a-button>
                            </div>
                            <a-alert v-if="srv.lastErr" type="error" class="err">{{ srv.lastErr }}</a-alert>
                        </div>
                    </a-card>
                </a-col>
                <a-col class="side-cols mt6" :span="24">
                    <a-card class="side-card card-head-tool" size="small" :body-style="{ padding: 0 }">
                        <template #title>说明</template>
                        <div class="side-body">
                            <ul class="tips">
                                <li>本页是局域网互传与分享链接的总控：文档中心/磁盘页「分享」登记的链接、局域网设备传来的文件都汇集于此。</li>
                                <li>上传免密：同一局域网内任何设备打开上面的地址即可往收件箱传文件，靠大小上限、类型黑名单与限频兜底，<strong>不靠认证</strong>。</li>
                                <li>不要直接双击运行收件箱里的文件。</li>
                                <li>收件箱的文件都在本机目录里：点「收件箱」行的文件夹按钮直接打开那个目录，列表里双击一行用文件查看器预览——只读本地文件，不走网络也不触发下载。</li>
                                <li>该端口对局域网开放、<strong>不经面板登录墙</strong>；停止服务后所有分享链接立即失效。</li>
                            </ul>
                        </div>
                    </a-card>
                </a-col>
            </a-row>
        </a-col>

        <!-- 右侧：分享链接 / 收件箱 -->
        <a-col :span="17" class="main-col">
            <a-card class="main-panel card-head-tool" :body-style="{ padding: 0 }">
                <template #title>
                    <a-space :size="8">
                        <span>局域网互传</span>
                        <a-tag size="small">{{ tab === "links" ? `分享链接 ${links.length}` : tab === "grants" ? `授权下载 ${grants.length}` : tab === "upload" ? `上传会话 ${sessions.length}` : `收件箱 ${inbox.length}` }}</a-tag>
                    </a-space>
                </template>
                <template #extra>
                    <a-space :size="8">
                        <a-button v-if="tab === 'grants'" size="small" type="primary" @click="openAddGrant">
                            <template #icon><icon-plus /></template>新增授权
                        </a-button>
                        <a-radio-group v-model="tab" type="button" size="medium">
                            <a-radio value="links">分享链接</a-radio>
                            <a-radio value="grants">授权下载</a-radio>
                            <a-radio value="upload">上传会话</a-radio>
                            <a-radio value="inbox">收件箱</a-radio>
                        </a-radio-group>
                    </a-space>
                </template>
                <div class="main-body">
                    <a-table v-show="tab === 'links'" :data="links" :loading="loading" :pagination="false"
                        size="small" row-key="token" class="fill-table">
                        <template #empty>
                            <a-empty description="还没有分享链接，去文档中心或磁盘页点「分享」" />
                        </template>
                        <template #columns>
                            <a-table-column title="文件" data-index="name" :ellipsis="true" :tooltip="true" />
                            <a-table-column title="创建" :width="160">
                                <template #cell="{ record }">{{ fmtTime(record.createdAt) }}</template>
                            </a-table-column>
                            <a-table-column title="到期" :width="160">
                                <template #cell="{ record }">
                                    {{ record.expiryAt > 0 ? fmtTime(record.expiryAt) : "不过期" }}
                                </template>
                            </a-table-column>
                            <a-table-column title="下载" :width="90">
                                <template #cell="{ record }">
                                    {{ record.downloads }} / {{ record.maxDownloads > 0 ? record.maxDownloads : "不限" }}
                                </template>
                            </a-table-column>
                            <a-table-column title="状态" :width="90">
                                <template #cell="{ record }">
                                    <a-tag :color="stateColor(record)" size="small">{{ stateText(record) }}</a-tag>
                                </template>
                            </a-table-column>
                            <a-table-column title="操作" :width="150">
                                <template #cell="{ record }">
                                    <a-space :size="4">
                                        <a-button size="mini" type="text" :disabled="!srv.running"
                                            @click="copyLink(record)">
                                            <template #icon><icon-copy /></template>链接
                                        </a-button>
                                        <a-popconfirm content="撤销后该链接立即失效（不动原文件），确定？" type="warning"
                                            @ok="revoke(record)">
                                            <a-button size="mini" type="text" status="danger">撤销</a-button>
                                        </a-popconfirm>
                                    </a-space>
                                </template>
                            </a-table-column>
                        </template>
                    </a-table>

                    <a-table v-show="tab === 'inbox'" :data="inbox" :loading="loading" :pagination="false"
                        size="small" row-key="name" class="fill-table inbox-table"
                        :row-class="(r: any) => (r.name === selectedInbox ? 'row-selected' : '')"
                        @row-click="(r: any) => (selectedInbox = r?.name ?? '')"
                        @row-dblclick="(r: any) => r && previewInbox(r)">
                        <template #empty>
                            <a-empty description="收件箱为空" />
                        </template>
                        <template #columns>
                            <a-table-column title="文件" data-index="name" :ellipsis="true" :tooltip="true" />
                            <a-table-column title="大小" :width="110">
                                <template #cell="{ record }">{{ fmtBytes(record.size) }}</template>
                            </a-table-column>
                            <a-table-column title="收到时间" :width="180">
                                <template #cell="{ record }">{{ fmtTime(record.modAt) }}</template>
                            </a-table-column>
                            <a-table-column title="操作" :width="150">
                                <template #cell="{ record }">
                                    <a-space :size="4">
                                        <a-button size="mini" type="text" title="用文件查看器预览（双击行同效）"
                                            @click.stop="previewInbox(record)">
                                            <template #icon><icon-eye /></template>预览
                                        </a-button>
                                        <a-button v-if="caps.nativeWindows" size="mini" type="text"
                                            title="在资源管理器中定位到该文件" @click.stop="revealInbox(record)">
                                            <template #icon><icon-folder /></template>定位
                                        </a-button>
                                    </a-space>
                                </template>
                            </a-table-column>
                        </template>
                    </a-table>

                    <a-table v-show="tab === 'grants'" :data="grants" :loading="loading" :pagination="false"
                        size="small" row-key="id" class="fill-table">
                        <template #empty>
                            <a-empty description="还没有授权下载，点右上「新增授权」选本机文件或目录" />
                        </template>
                        <template #columns>
                            <a-table-column title="名称" data-index="name" :ellipsis="true" :tooltip="true" />
                            <a-table-column title="类型" :width="90">
                                <template #cell="{ record }">
                                    <a-tag size="small" :color="record.dir ? 'blue' : 'gray'">{{ record.dir ? (record.recursive ? "目录·可浏览" : "目录·整包") : "文件" }}</a-tag>
                                </template>
                            </a-table-column>
                            <a-table-column title="到期" :width="150">
                                <template #cell="{ record }">{{ record.expiryAt > 0 ? fmtTime(record.expiryAt) : "长期" }}</template>
                            </a-table-column>
                            <a-table-column title="下载" :width="80">
                                <template #cell="{ record }">{{ record.downloads }}</template>
                            </a-table-column>
                            <a-table-column title="状态" :width="90">
                                <template #cell="{ record }">
                                    <a-tag :color="grantState(record).c" size="small">{{ grantState(record).t }}</a-tag>
                                </template>
                            </a-table-column>
                            <a-table-column title="操作" :width="210">
                                <template #cell="{ record }">
                                    <a-space :size="4">
                                        <a-button size="mini" type="text" :disabled="!srv.running" @click="copyGrant(record)">
                                            <template #icon><icon-copy /></template>链接
                                        </a-button>
                                        <a-button size="mini" type="text" :disabled="!srv.running"
                                            @click="copyGrant(record, true)">
                                            <template #icon><icon-qrcode /></template>二维码
                                        </a-button>
                                        <a-popconfirm content="撤销后该授权立即失效（不动原文件），确定？" type="warning"
                                            position="lt" @ok="revokeGrant(record)">
                                            <a-button size="mini" type="text" status="danger">撤销</a-button>
                                        </a-popconfirm>
                                    </a-space>
                                </template>
                            </a-table-column>
                            <a-table-column title="路径" :ellipsis="true" :tooltip="true">
                                <template #cell="{ record }"><span class="mono break">{{ record.path }}</span></template>
                            </a-table-column>
                        </template>
                    </a-table>

                    <div v-show="tab === 'upload'" class="up-wrap">
                        <common-upload-panel v-if="srv.running && srv.urls.length" :transport="transport"
                            :threshold-bytes="8 * 1024 * 1024" :concurrency="2" hint="把本机文件拖进来，直接投递到收件箱"
                            @done="onLocalUploaded" />
                        <a-alert v-if="srv.running && srv.urls.length" type="info" class="up-tip">
                            上方走的是本机互传服务（{{ srv.urls[0] }}）同一套 init/part/complete 协议；下方是局域网设备正在上传／可续传的接收会话，卡住的可就地强踢。</a-alert>
                        <a-empty v-else description="先启动互传服务，本机拖拽上传与接收会话才可用" />
                        <a-table v-if="sessions.length" :data="sessions" :loading="sessLoading" :pagination="false"
                            size="small" row-key="id" class="fill-table sess-table">
                            <template #columns>
                                <a-table-column title="文件" data-index="name" :ellipsis="true" :tooltip="true" />
                                <a-table-column title="已收 / 总大小" :width="170">
                                    <template #cell="{ record }">{{ fmtBytes(record.received) }} / {{ fmtBytes(record.size) }}</template>
                                </a-table-column>
                                <a-table-column title="进度" :width="160">
                                    <template #cell="{ record }"><a-progress :percent="record.percent / 100" size="small" /></template>
                                </a-table-column>
                                <a-table-column title="更新" :width="160">
                                    <template #cell="{ record }">{{ fmtTime(record.updatedAt) }}</template>
                                </a-table-column>
                                <a-table-column title="操作" :width="100" align="right">
                                    <template #cell="{ record }">
                                        <a-popconfirm content="强踢会删除该会话已收的半成品，确定？" type="warning" @ok="kickSession(record)">
                                            <a-button size="mini" type="text" status="danger">强踢</a-button>
                                        </a-popconfirm>
                                    </template>
                                </a-table-column>
                            </template>
                        </a-table>
                    </div>
                </div>
            </a-card>
        </a-col>
    </a-row>

    <!-- 二维码弹层：手机扫码直接打开门户/授权下载 -->
    <a-modal v-model:visible="qrVisible" :width="320" :footer="false" title="扫码访问">
        <div class="qr-box">
            <img v-if="qrDataUrl" :src="qrDataUrl" alt="二维码" class="qr-img" />
            <div class="mono break qr-url">{{ qrValue }}</div>
            <a-button size="mini" type="text" @click="copyText(qrValue)">
                <template #icon><icon-copy /></template>复制地址
            </a-button>
        </div>
    </a-modal>

    <!-- 新增授权弹层：选本机文件/目录（桌面走原生选择框，Web 可手输路径） -->
    <a-modal v-model:visible="grantVisible" :width="520" :ok-loading="grantBusy" @ok="submitGrant"
        @cancel="grantVisible = false" title="新增授权下载">
        <a-form :model="grantForm" layout="vertical" :style="{ maxWidth: '460px' }">
            <a-form-item field="path" label="本机路径">
                <a-input-group>
                    <a-input v-model="grantForm.path" placeholder="绝对路径，如 E:\\share 或 /home/me/share" allow-clear />
                    <a-button v-if="caps.nativeWindows" @click="pickPath">
                        <template #icon><icon-folder /></template>选择
                    </a-button>
                </a-input-group>
                <span class="hint">目录默认整包（zip）下载；勾选可浏览后允许逐项下载子文件。</span>
            </a-form-item>
            <a-form-item field="recursive" label="目录允许逐项浏览">
                <a-switch v-model="grantForm.recursive" />
                <span class="hint" style="margin-left: 10px">不勾选只能整包下 zip，无法浏览目录内部。</span>
            </a-form-item>
            <a-form-item field="hours" label="有效期（小时）">
                <a-input-number v-model="grantForm.hours" :min="0" :step="1" style="width: 180px" />
                <span class="hint" style="margin-left: 10px">0 = 长期有效（直到服务停止或手动撤销）。</span>
            </a-form-item>
        </a-form>
    </a-modal>
</template>

<script setup lang="ts">
/**
 * 局域网互传 / 分享链接总控页（计划-SSH管理与局域网互传 §四）。
 * 一个 net/http 监听同时提供收件箱上传与临时分享链接；本页只做“看与撤”，
 * 生成链接的入口在文档中心/磁盘页的「分享」。绑定层 services/app/lanshare.go。
 */
import { computed, getCurrentInstance, onMounted, onUnmounted, reactive, ref } from "vue";
import { Events } from "@wailsio/runtime";
import { Message } from "@arco-design/web-vue";
import qrcode from "qrcode-generator";
import { useCaps } from "@/utils/caps";
import { useToolWindow } from "@/views/index/useToolWindow";
import type { UploadTransport } from "@/components/common/upload-panel/upload-panel";

// 中文/UTF-8 支持：默认按 latin1 截断，需覆盖为 UTF-8（与二维码生成页同一口径）
qrcode.stringToBytes = (s: string): number[] => {
    const bytes: number[] = [];
    new TextEncoder().encode(s).forEach((b) => bytes.push(b));
    return bytes;
};

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const caps = useCaps();
const { openToolWindow } = useToolWindow();

interface LsStatus {
    running: boolean; host: string; port: number; urls: string[]; inbox: string;
    startedAt: number; autoStopAt: number; reqCount: number; uploadCount: number;
    pin: string; grantPath: string; lastErr: string;
}
interface LsLink {
    token: string; path: string; name: string; maxDownloads: number; expiryAt: number;
    createdAt: number; downloads: number; lastAt?: number; bytes: number;
}
interface LsInbox { name: string; size: number; modAt: number; }
interface LsGrant {
    id: string; path: string; name: string; recursive: boolean; expiryAt: number;
    createdAt: number; downloads: number; lastAt?: number; dir: boolean;
}
interface LsSession {
    id: string; name: string; size: number; received: number; percent: number; updatedAt: number;
}
/** lanshare:progress 事件负载（与后端 tools/lanshare Progress 对齐） */
interface LsProgress {
    op: string; name: string; stage: string; percent: number; note?: string; id?: string; status?: string;
}

const srv = reactive<LsStatus>({
    running: false, host: "", port: 0, urls: [], inbox: "",
    startedAt: 0, autoStopAt: 0, reqCount: 0, uploadCount: 0, pin: "", grantPath: "/g/", lastErr: "",
});
const ctl = reactive({ host: "", stopMin: 120, pin: "", port: 0, blockExt: [] as string[] });
/** 常见危险类型（一键填入）：与旧版写死的后端黑名单对齐，供用户随时找回安全默认 */
const DANGER_TYPES = ["exe", "dll", "bat", "cmd", "com", "msi", "scr", "vbs", "js", "jse", "wsf", "wsh", "ps1", "psm1", "lnk", "sh", "desktop", "jar", "reg"];
/** 接收类型黑名单保存中标记 */
const cfgSaving = ref(false);
const links = ref<LsLink[]>([]);
const inbox = ref<LsInbox[]>([]);
const grants = ref<LsGrant[]>([]);
const sessions = ref<LsSession[]>([]);
const sessLoading = ref(false);
const tab = ref<"links" | "grants" | "upload" | "inbox">("links");
const busy = ref(false);
const loading = ref(false);
/** 收件箱选中行（只存文件名）：收件箱不会重名，row-key 也是它 */
const selectedInbox = ref("");

/* 二维码弹层 */
const qrVisible = ref(false);
const qrValue = ref("");
const qrDataUrl = ref("");

/* 新增授权弹层 */
const grantVisible = ref(false);
const grantBusy = ref(false);
const grantForm = reactive({ path: "", recursive: false, hours: 0 });

/* 传输进度（订阅 lanshare:progress，页内轻提示；任务中心靠 task:update 跨页可见） */
let offLsProgress: (() => void) | null = null;

let tick: ReturnType<typeof setInterval> | null = null;
const now = ref(Date.now());

const autoStopIn = computed(() => {
    if (!srv.running || srv.autoStopAt <= 0) return "--";
    const left = srv.autoStopAt - now.value;
    if (left <= 0) return "即将停止";
    const m = Math.floor(left / 60000);
    const s = Math.floor((left % 60000) / 1000);
    return `${m} 分 ${s} 秒后自动停止`;
});

async function refreshStatus() {
    try {
        const st = await GoTools?.LanShareStatus();
        if (st) Object.assign(srv, st);
        srv.urls = (st?.urls as string[]) ?? [];
    } catch { /* 绑定未就绪时保持原状 */ }
}

async function refreshLinks() {
    loading.value = true;
    try {
        links.value = (await GoTools?.LanShareListLinks()) ?? [];
    } catch (e: any) {
        Message.error(`读取链接失败：${errText(e)}`);
    } finally {
        loading.value = false;
    }
}

async function refreshInbox() {
    try {
        inbox.value = (await GoTools?.LanShareInboxFiles()) ?? [];
    } catch { /* 收件箱读取失败不弹窗，页面留空即可 */ }
}

async function refreshGrants() {
    try {
        grants.value = (await GoTools?.LanShareListGrants()) ?? [];
    } catch { /* 授权列表读取失败不弹窗 */ }
}

async function refreshSessions() {
    sessLoading.value = true;
    try {
        sessions.value = (await GoTools?.LanShareUploadSessions()) ?? [];
    } catch { /* 会话列表读取失败不弹窗 */ } finally { sessLoading.value = false; }
}

/* ===================== 互传设置（黑名单即时生效；port/host/pin/自停是启动默认值） ===================== */
// loadReceivePolicy 回填当前设置：黑名单后端已归一为小写带前导点（如 ".exe"）；
// 启动默认值（host/pin/自停分钟）仅在服务未运行时覆盖控件，不碰正在跑的实例。
async function loadReceivePolicy() {
    try {
        const s = await GoTools?.LanShareSettings();
        ctl.blockExt = ((s?.blockExt as string[]) ?? []).slice();
        if (!srv.running) {
            ctl.host = (s?.host as string) ?? "";
            ctl.stopMin = Number(s?.autoStopMin ?? 120);
            ctl.pin = (s?.pin as string) ?? "";
            ctl.port = Number(s?.port ?? 0);
        }
    } catch { /* 设置未就绪时保持默认（全收 / 免密 / 120 分钟自停） */ }
}
function fillDangerTypes() {
    ctl.blockExt = DANGER_TYPES.slice();
}
// saveReceivePolicy 保存整段设置：落盘后 Go 侧回灌单例，黑名单对已在跑的服务
// 即时生效；启动默认值下次启动时用。页面控件当前值即默认值，不留双标。
async function saveReceivePolicy() {
    cfgSaving.value = true;
    try {
        await GoTools?.SaveLanShareSettings({
            blockExt: ctl.blockExt.map((s) => String(s).trim()).filter(Boolean),
            port: ctl.port > 0 ? ctl.port : 0,
            host: ctl.host,
            autoStopMin: ctl.stopMin,
            pin: ctl.pin.trim(),
        } as any);
        Message.success("互传设置已保存（黑名单即时生效，启动默认值下次启动用）");
        await loadReceivePolicy();
    } catch (e: any) {
        Message.error(`保存失败：${errText(e)}`);
    } finally {
        cfgSaving.value = false;
    }
}

// transport：把公用上传面板的 init/part/complete/abort/once 指到本机互传服务。
// 每次调用实时取 srv.urls[0]（服务重启会变 URL）；路径前缀与门户页一致。
const lsBase = () => (srv.urls[0] || "").replace(/[\/]+$/, "");
const transport: UploadTransport = {
    async init(file) {
        const r = await fetch(lsBase() + "/upload/init", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name: file.name, size: file.size, mtime: file.lastModified || 0 }) });
        if (!r.ok) throw new Error(await r.text());
        return await r.json();
    },
    async part(file, sid, idx, start, end) {
        const r = await fetch(lsBase() + `/upload/part?sid=${encodeURIComponent(sid)}&idx=${idx}`, { method: "PUT", body: file.slice(start, end) });
        if (!r.ok) throw new Error(await r.text());
    },
    async complete(_file, sid) {
        const r = await fetch(lsBase() + `/upload/complete?sid=${encodeURIComponent(sid)}`, { method: "POST" });
        if (!r.ok) throw new Error(await r.text());
    },
    async abort(sid) {
        if (!sid) return;
        try { await fetch(lsBase() + `/upload/abort?sid=${encodeURIComponent(sid)}`, { method: "DELETE" }); } catch { /* 强踢失败不阻断 UI */ }
    },
    async once(file) {
        const fd = new FormData(); fd.append("file", file, file.name);
        const r = await fetch(lsBase() + "/upload", { method: "POST", body: fd });
        if (!r.ok) throw new Error(await r.text());
    },
};

async function onLocalUploaded() {
    await Promise.all([refreshInbox(), refreshSessions(), refreshStatus()]);
}

async function kickSession(s: LsSession) {
    try {
        await GoTools?.LanShareAbortUpload(s.id);
        Message.success("已强踢该上传会话");
        await refreshSessions();
    } catch (e: any) {
        Message.error(`强踢失败：${errText(e)}`);
    }
}

async function refreshAll() {
    await Promise.all([refreshStatus(), refreshLinks(), refreshGrants(), refreshInbox(), refreshSessions()]);
}

/* ===================== 收件箱：开目录与就地预览 ===================== */

// inboxPath 拼收件箱条目的本机绝对路径。后端只回名字（不暴露路径拼接给前端），
// 目录用 srv.inbox —— 服务停着时 StatusNow 也照填，所以未启动一样能开目录。
function inboxPath(name: string): string {
    const dir = (srv.inbox || "").replace(/[\\/]+$/, "");
    if (!dir) return "";
    return dir + (dir.includes("\\") ? "\\" : "/") + name;
}

// previewInbox 把收件箱文件交给文件查看器：它按本机路径读，走的是和磁盘页
// 「打开」完全同一条链路（单例窗口靠 query 定位，已开着时靠事件导航）。
// 不新写一套预览：收件箱里常有 csv/日志/压缩包，正是查看器分发的类型。
async function previewInbox(r: LsInbox) {
    const p = inboxPath(r.name);
    if (!p) {
        Message.warning("还没拿到收件箱目录，点「刷新」重试");
        return;
    }
    await openToolWindow(`/tools/viewer?path=${encodeURIComponent(p)}`, "文件查看器", { single: true });
    try { Events.Emit("viewer:open-path", { path: p }); } catch { /* 非 Wails 环境忽略 */ }
}

// revealInbox 在资源管理器里打开收件箱目录；传了条目就顺带选中那个文件。
// 复用磁盘页的 DiskOpenInExplorer（不新增绑定）；非 Windows 面板下它直接报
// 不支持，所以入口按 caps.nativeWindows 隐藏。
async function revealInbox(r?: LsInbox) {
    const p = r ? inboxPath(r.name) : (srv.inbox || "");
    if (!p) {
        Message.warning("还没拿到收件箱目录，点「刷新」重试");
        return;
    }
    try {
        await GoTools?.DiskOpenInExplorer(p);
    } catch (e: any) {
        Message.error(`打开目录失败：${errText(e)}`);
    }
}

async function start() {
    busy.value = true;
    try {
        const st = await GoTools?.LanShareStart(ctl.port > 0 ? ctl.port : 0, ctl.host, ctl.stopMin, ctl.pin.trim());
        if (st) Object.assign(srv, st);
        Message.success(srv.running ? "互传服务已启动" : "服务未能启动");
        await refreshAll();
    } catch (e: any) {
        Message.error(`启动失败：${errText(e)}`);
    } finally {
        busy.value = false;
    }
}

async function stop() {
    busy.value = true;
    try {
        await GoTools?.LanShareStop();
        Message.success("互传服务已停止，所有链接即刻失效");
        await refreshAll();
    } catch (e: any) {
        Message.error(`停止失败：${errText(e)}`);
    } finally {
        busy.value = false;
    }
}

async function revoke(l: LsLink) {
    try {
        await GoTools?.LanShareRevokeLink(l.token);
        Message.success("已撤销");
        await refreshLinks();
    } catch (e: any) {
        Message.error(`撤销失败：${errText(e)}`);
    }
}

async function copyLink(l: LsLink) {
    try {
        const url = await GoTools?.LanShareURL(l.token);
        await copyText(url);
    } catch (e: any) {
        Message.error(`获取链接失败：${errText(e)}`);
    }
}

/* ===================== 授权下载 ===================== */
function openAddGrant() {
    grantForm.path = "";
    grantForm.recursive = false;
    grantForm.hours = 0;
    grantVisible.value = true;
}

async function pickPath() {
    try {
        const p = await GoTools?.LanShareChoosePath("选择要授权下载的文件或目录");
        if (p) grantForm.path = String(p);
    } catch (e: any) {
        Message.warning(`原生选择不可用，请手输路径：${errText(e)}`);
    }
}

async function submitGrant() {
    const p = grantForm.path.trim();
    if (!p) {
        Message.warning("请填写或选择本机路径");
        return;
    }
    grantBusy.value = true;
    try {
        await GoTools?.LanShareAddGrant(p, grantForm.recursive, grantForm.hours);
        Message.success("已授权");
        grantVisible.value = false;
        await refreshGrants();
    } catch (e: any) {
        Message.error(`授权失败：${errText(e)}`);
    } finally {
        grantBusy.value = false;
    }
}

async function revokeGrant(g: LsGrant) {
    try {
        await GoTools?.LanShareRevokeGrant(g.id);
        Message.success("已撤销");
        await refreshGrants();
    } catch (e: any) {
        Message.error(`撤销失败：${errText(e)}`);
    }
}

async function copyGrant(g: LsGrant, asQr = false) {
    try {
        const url = await GoTools?.LanShareGrantURL(g.id);
        if (asQr) openQr(String(url));
        else await copyText(url);
    } catch (e: any) {
        Message.error(`获取链接失败：${errText(e)}`);
    }
}

function grantState(g: LsGrant): { t: string; c: string } {
    if (g.expiryAt > 0 && now.value > g.expiryAt) return { t: "已过期", c: "red" };
    return { t: "有效", c: "green" };
}

/* ===================== 二维码 ===================== */
function openQr(value: string) {
    if (!value) return;
    try {
        const qr = qrcode(0, "M");
        qr.addData(String(value), "Byte");
        qr.make();
        qrDataUrl.value = qr.createDataURL(4, 8);
        qrValue.value = String(value);
        qrVisible.value = true;
    } catch {
        Message.error("内容过长，无法生成二维码");
    }
}

function stateText(l: LsLink): string {
    if (l.expiryAt > 0 && now.value > l.expiryAt) return "已过期";
    if (l.maxDownloads > 0 && l.downloads >= l.maxDownloads) return "已用尽";
    return "有效";
}
function stateColor(l: LsLink): string {
    const s = stateText(l);
    return s === "有效" ? "green" : s === "已用尽" ? "orange" : "red";
}

/* ---------- 工具函数 ---------- */
function errText(e: any): string { return String(e?.message ?? e ?? "操作失败"); }
function fmtTime(ms: number): string {
    if (!ms) return "--";
    try { return new Date(ms).toLocaleString(); } catch { return String(ms); }
}
function fmtBytes(n: number): string {
    if (!n || n < 0) return "0 B";
    const u = ["B", "KB", "MB", "GB", "TB"];
    let i = 0, v = n;
    while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
    return `${v.toFixed(i === 0 ? 0 : 1)} ${u[i]}`;
}
async function copyText(text: string) {
    if (!text) return;
    try {
        await navigator.clipboard.writeText(text);
        Message.success("已复制");
    } catch {
        const ta = document.createElement("textarea");
        ta.value = text;
        document.body.appendChild(ta);
        ta.select();
        document.execCommand("copy");
        document.body.removeChild(ta);
        Message.success("已复制");
    }
}

onMounted(() => {
    void refreshAll();
    void loadReceivePolicy();
    tick = setInterval(() => { now.value = Date.now(); }, 1000);
    // 订阅传输进度：改为按 payload 驱动。上传进行中用事件里的百分比就地刷行（不扫盘），
    // 新会话才扫一次拉准确行；终态与下载才对齐磁盘刷新。跨页可见性由全局任务中心兼顾。
    try {
        const off = Events.On("lanshare:progress", (ev: any) => {
            const p: LsProgress | undefined = ev?.data ?? ev;
            if (!p || !p.op) return;
            const terminal = p.stage === "done" || p.stage === "error" || p.stage === "canceled";
            if (p.op === "upload") {
                if (!terminal) {
                    const id = p.id || "";
                    const i = id ? sessions.value.findIndex((s) => s.id === id) : -1;
                    if (i >= 0) {
                        sessions.value[i] = { ...sessions.value[i], percent: p.percent ?? sessions.value[i].percent, updatedAt: Date.now() };
                    } else {
                        void refreshSessions(); // 新会话：扫一次拉含大小/已收的准确行
                    }
                    return;
                }
                void Promise.all([refreshSessions(), refreshInbox(), refreshStatus()]);
                return;
            }
            if (p.op === "download") {
                // 下载只改计数（授权/链接的下载次数），实体不动
                void Promise.all([refreshGrants(), refreshLinks()]);
            }
        });
        offLsProgress = () => { try { (off as any)?.(); } catch { /* 忽略卸载失败 */ } };
    } catch { /* 事件层未就绪时静默 */ }
});
onUnmounted(() => {
    if (tick) clearInterval(tick);
    if (offLsProgress) offLsProgress();
});
</script>

<style scoped>
.ls-page {
    height: 100%;
    min-height: 0;
    flex-wrap: nowrap;
}

.side-col {
    height: 100%;
    min-width: 0;
    display: flex;
    flex-direction: column;
}

.side-pages {
    height: calc(100% - 6px);
}

.side-cols {
    height: 62%;
    width: 100%;
    display: flex;
    flex-direction: column;
}

.side-cols + .side-cols {
    height: 38%;
}

.mt6 {
    margin-top: 6px;
}

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

.side-body {
    flex: 1;
    min-height: 0;
    padding: var(--base-padding);
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 10px;
}

.field {
    display: flex;
    flex-direction: column;
    gap: 4px;
}

.field-label {
    color: var(--color-text-2);
    font-size: var(--base-size-small);
}

.urls {
    display: flex;
    flex-direction: column;
    gap: 2px;
}

.url-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 6px;
}

.kv {
    display: flex;
    gap: 8px;
    font-size: var(--base-size-small);
}

.kv .k {
    color: var(--color-text-3);
    flex: none;
    width: 72px;
}

.kv .v {
    color: var(--color-text-1);
}

.kv.inbox .v {
    word-break: break-all;
}

/* 收件箱行尾的文件夹按钮：路径要能换行，按钮不能被压扁 */
.kv.inbox .mini-btn {
    flex: none;
    align-self: flex-start;
}

/* 收件箱行可双击预览：选中高亮必须盖到 td（Arco 的 td 自带背景会盖住 tr），
   与抓包页同一写法 */
.inbox-table :deep(.arco-table-tr) {
    cursor: pointer;
}

.inbox-table :deep(.arco-table-tr.row-selected .arco-table-td) {
    background-color: var(--color-fill-2) !important;
}

.err {
    margin-top: 4px;
}

/* 接收类型黑名单操作行：靠左上排开，与输入框留点间距 */
.blk-actions {
    margin-top: 2px;
}

.tips {
    margin: 0;
    padding-left: 16px;
    color: var(--color-text-2);
    font-size: var(--base-size-small);
    line-height: 1.8;
}

.mono {
    font-family: Consolas, Menlo, monospace;
    font-size: var(--base-size-small);
}

.break {
    word-break: break-all;
}

.com-state {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: var(--base-size-small);
}

.com-state.is-on {
    color: rgb(var(--green-6));
}

.com-state.is-dim {
    color: var(--color-text-3);
}

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

.main-body {
    flex: 1;
    min-height: 0;
    padding: var(--base-padding);
    overflow-y: auto;
}

.fill-table {
    width: 100%;
}

.hint {
    display: block;
    margin-top: 4px;
    color: var(--color-text-3);
    font-size: var(--base-size-small);
    line-height: 1.6;
}

.hint-inline {
    display: inline;
}

.qr-box {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
    padding: 8px 0;
}

.qr-img {
    image-rendering: pixelated;
    width: 200px;
    height: 200px;
}

.qr-url {
    max-width: 260px;
    text-align: center;
    color: var(--color-text-2);
}
</style>
