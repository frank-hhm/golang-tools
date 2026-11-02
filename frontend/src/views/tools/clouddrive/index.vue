<template>
    <a-row class="cd-page" :gutter="6">
        <!-- 左栏：账号 -->
        <a-col :span="6" class="side-col">
            <a-card class="side-card card-head-tool" size="small" :body-style="{ padding: 0 }">
                <template #title>云盘账号</template>
                <template #extra>
                    <a-button size="mini" :disabled="!ready" @click="openAdd">
                        <template #icon><icon-plus /></template>添加
                    </a-button>
                </template>
                <div class="side-body">
                    <a-alert v-if="!cfg.enabled" type="warning" class="cd-warn">
                        云盘功能未启用，请到 设置 → 云盘管理 打开。
                    </a-alert>
                    <a-alert v-else-if="!cfg.clientReady" type="warning" class="cd-warn">
                        尚未填写 Google OAuth 客户端 ID/密钥，请到 设置 → 云盘管理 配置。
                    </a-alert>

                    <div v-for="a in accounts" :key="a.id" class="acct" :class="{ active: a.id === cur?.id }"
                        @click="pick(a)">
                        <div class="acct-main">
                            <div class="acct-name">{{ a.label }}</div>
                            <div class="acct-sub">
                                <a-tag size="small">{{ kindName(a.kind) }}</a-tag>
                                <a-tag size="small" :color="a.hasToken ? 'green' : 'orange'">
                                    {{ a.hasToken ? '已授权' : '未授权' }}
                                </a-tag>
                            </div>
                        </div>
                        <a-space :size="2">
                            <a-tooltip content="授权">
                                <a-button size="mini" type="text" :disabled="!ready" @click.stop="openAuth(a)">
                                    <icon-safe />
                                </a-button>
                            </a-tooltip>
                            <a-popconfirm content="删除本地账号与令牌（不影响云端文件），确定？" type="warning"
                                @ok="remove(a)">
                                <a-button size="mini" type="text" status="danger" @click.stop>
                                    <icon-delete />
                                </a-button>
                            </a-popconfirm>
                        </a-space>
                    </div>
                    <a-empty v-if="!accounts.length" description="还没有账号，点上方「添加」" />

                    <div v-if="quota && cur" class="quota">
                        <div class="quota-txt">
                            {{ fmtBytes(quota.used) }} / {{ quota.total > 0 ? fmtBytes(quota.total) : '未知' }}
                        </div>
                        <a-progress :percent="quotaPct" size="small" :show-text="false"
                            :status="quotaPct > 0.9 ? 'warning' : 'normal'" />
                    </div>
                </div>
            </a-card>
        </a-col>

        <!-- 右栏：文件浏览器 -->
        <a-col :span="18" class="main-col">
            <a-card class="main-panel card-head-tool" :body-style="{ padding: 0 }">
                <template #title>
                    <a-space :size="8">
                        <span>{{ cur ? cur.label : '云盘浏览' }}</span>
                        <a-tag v-if="cur" size="small">{{ entries.length }} 项</a-tag>
                    </a-space>
                </template>
                <template #extra>
                    <a-space :size="8">
                        <a-button size="small" :disabled="!canBrowse || !crumbs.length" @click="goUp">
                            <template #icon><icon-arrow-left /></template>上级
                        </a-button>
                        <a-button size="small" :disabled="!canBrowse" @click="openMkdir">
                            <template #icon><icon-folder-add /></template>新建目录
                        </a-button>
                        <a-button size="small" :disabled="!canBrowse" @click="openUpload">
                            <template #icon><icon-upload /></template>上传
                        </a-button>
                        <a-button size="small" :disabled="!canBrowse" @click="load()">
                            <template #icon><icon-refresh /></template>刷新
                        </a-button>
                        <a-badge :count="trRunning">
                            <a-button size="small" @click="trDrawer = true; loadTransfers()">
                                <template #icon><icon-history /></template>转存任务
                            </a-button>
                        </a-badge>
                    </a-space>
                </template>
                <div class="main-body">
                    <div v-if="!cur" class="cd-hint">
                        <a-empty description="从左侧选择一个账号开始浏览" />
                    </div>
                    <template v-else>
                        <div class="crumbs">
                            <span class="crumb" :class="{ cur: !crumbs.length }" @click="goRoot">全部文件</span>
                            <template v-for="(c, i) in crumbs" :key="c.id">
                                <span class="crumb-sep">/</span>
                                <span class="crumb" :class="{ cur: i === crumbs.length - 1 }" @click="goCrumb(i)">{{
                                    c.name }}</span>
                            </template>
                        </div>

                        <a-table :data="entries" :loading="loading" :pagination="false" size="small" row-key="id"
                            class="cd-table">
                            <template #empty>
                                <a-empty :description="cur.hasToken ? '此目录为空' : '该账号未授权，请先点授权'" />
                            </template>
                            <template #columns>
                                <a-table-column title="名称" :ellipsis="true" :tooltip="true">
                                    <template #cell="{ record }">
                                        <span class="fname" :class="{ dir: record.isDir }" @click="onOpen(record)">
                                            <icon-folder v-if="record.isDir" />
                                            <icon-file v-else />
                                            {{ record.name }}
                                        </span>
                                    </template>
                                </a-table-column>
                                <a-table-column title="大小" :width="110">
                                    <template #cell="{ record }">{{ record.isDir ? '--' : fmtBytes(record.size) }}
                                    </template>
                                </a-table-column>
                                <a-table-column title="修改时间" :width="170">
                                    <template #cell="{ record }">{{ fmtTime(record.mtime) }}</template>
                                </a-table-column>
                                <a-table-column title="操作" :width="160">
                                    <template #cell="{ record }">
                                        <a-space :size="2">
                                            <a-button size="mini" type="text" :disabled="record.isDir"
                                                @click="download(record)">下载</a-button>
                                            <a-dropdown trigger="hover" @select="(k: any) => onRowMore(String(k), record)">
                                                <a-button size="mini" type="text">
                                                    更多<icon-down />
                                                </a-button>
                                                <template #content>
                                                    <a-doption v-if="!record.isDir" value="preview"><template #icon><icon-eye /></template>预览</a-doption>
                                                    <a-doption v-if="!record.isDir" value="transfer"><template #icon><icon-swap /></template>转存到…</a-doption>
                                                    <a-doption value="rename"><template #icon><icon-edit /></template>重命名</a-doption>
                                                    <a-doption v-if="!record.isDir" value="move"><template #icon><icon-folder /></template>移动</a-doption>
                                                    <a-doption v-if="!record.isDir" value="share"><template #icon><icon-link /></template>分享链接</a-doption>
                                                    <a-doption value="del" class="danger"><template #icon><icon-delete /></template>删除</a-doption>
                                                </template>
                                            </a-dropdown>
                                        </a-space>
                                    </template>
                                </a-table-column>
                            </template>
                        </a-table>
                    </template>
                </div>
            </a-card>
        </a-col>

        <!-- 添加账号 -->
        <a-modal v-model:visible="addModal.open" title="添加云盘账号" :ok-loading="addModal.saving" @ok="doAdd">
            <a-form :model="addModal" layout="vertical">
                <a-form-item label="类型">
                    <a-select v-model="addModal.kind">
                        <a-option value="google">Google Drive</a-option>
                        <a-option value="aliyun">阿里云盘</a-option>
                        <a-option value="baidu">百度网盘</a-option>
                    </a-select>
                </a-form-item>
                <a-form-item label="显示名">
                    <a-input v-model="addModal.label" placeholder="例如：我的谷歌盘" />
                </a-form-item>
            </a-form>
        </a-modal>

        <!-- 授权：Google 走粘贴回跳，阿里/百度走粘贴长期凭证 -->
        <a-modal v-model:visible="authModal.open" :title="authTitle" :width="560" :ok-loading="authModal.saving"
            :ok-text="authModal.kind === 'google' ? '提交授权码' : '保存凭证'" @ok="onAuthOk">
            <div class="auth-step">
                <template v-if="authModal.kind === 'google'">
                    <div class="auth-h">第 1 步：点下方链接，在浏览器里完成 Google 授权（选账号、允许）。</div>
                    <a-input-group class="auth-url">
                        <a-input :model-value="authModal.url" readonly class="mono" placeholder="生成授权链接…" />
                        <a-button @click="genAuthUrl"><template #icon><icon-link /></template>生成</a-button>
                        <a-button :disabled="!authModal.url" @click="copy(authModal.url)"><template
                                #icon><icon-copy /></template></a-button>
                    </a-input-group>
                    <div class="auth-h">第 2 步：授权后浏览器会跳到一个「无法访问」的本机地址，把<strong>地址栏整串</strong>（或
                        code= 后面的值）粘贴到下面。</div>
                    <a-textarea v-model="authModal.code" :auto-size="{ minRows: 2, maxRows: 4 }"
                        placeholder="http://localhost:8458/callback?code=4/xxxx&scope=..." />
                    <div class="auth-tip">回调地址须与设置里登记的完全一致。令牌加密存在本地，不会回前端。</div>
                </template>
                <template v-else>
                    <div class="auth-h">从浏览器登录 {{ kindName(authModal.kind) }} 后，抓取长期凭证粘贴到下方（仅需一次，加密存本地）。</div>
                    <a-textarea v-model="authModal.code" :auto-size="{ minRows: 2, maxRows: 4 }"
                        :placeholder="credPlaceholder" />
                    <div class="auth-tip">{{ credHint }} 凭证不会回传前端。</div>
                </template>
            </div>
        </a-modal>

        <!-- 新建/重命名/上传：统一文本输入弹窗 -->
        <a-modal v-model:visible="prompt.open" :title="prompt.title" :ok-loading="prompt.saving" @ok="doPrompt">
            <a-form :model="prompt" layout="vertical">
                <a-form-item v-if="prompt.kind === 'upload'" label="本地文件路径">
                    <a-input-group>
                        <a-input v-model="prompt.localPath" placeholder="可点右侧选择，或直接输入绝对路径" />
                        <a-button @click="chooseFile"><template #icon><icon-folder /></template></a-button>
                    </a-input-group>
                </a-form-item>
                <a-form-item :label="prompt.fieldLabel">
                    <a-input v-model="prompt.value" :placeholder="prompt.placeholder" />
                </a-form-item>
            </a-form>
        </a-modal>

        <!-- 移动：目录选择 -->
        <a-modal v-model:visible="moveModal.open" title="移动到…" :ok-loading="moveModal.saving" ok-text="移动到当前目录"
            @ok="doMove">
            <div class="crumbs">
                <span class="crumb" :class="{ cur: !moveModal.crumbs.length }" @click="moveRoot">全部文件</span>
                <template v-for="(c, i) in moveModal.crumbs" :key="c.id">
                    <span class="crumb-sep">/</span>
                    <span class="crumb" :class="{ cur: i === moveModal.crumbs.length - 1 }" @click="moveGo(i)">{{ c.name
                        }}</span>
                </template>
            </div>
            <div class="move-list">
                <div v-for="d in moveModal.dirs" :key="d.id" class="move-dir" @click="moveEnter(d)">
                    <icon-folder /> {{ d.name }}
                </div>
                <a-empty v-if="!moveModal.dirs.length" description="此目录下没有子目录" />
            </div>
        </a-modal>

        <!-- 分享结果 -->
        <a-modal v-model:visible="shareModal.open" title="分享链接" :footer="false" :width="520">
            <a-input-group class="sh-url">
                <a-input :model-value="shareModal.url" readonly class="mono" />
                <a-button @click="copy(shareModal.url)"><template #icon><icon-copy /></template>复制</a-button>
            </a-input-group>
            <div class="auth-tip">这是 Google 生成的对外链接，任何拿到链接的人都可看（按 Google 权限设置）。</div>
        </a-modal>

        <!-- 跨盘转存：选目标账号 + 目标目录（同账号内可浏览） -->
        <a-modal v-model:visible="transferModal.open" title="跨盘转存到…" :width="560" :ok-loading="transferModal.saving"
            ok-text="开始转存" @ok="doTransfer">
            <a-form :model="transferModal" layout="vertical">
                <a-form-item label="源文件">
                    <a-input :model-value="transferModal.src?.name" readonly />
                </a-form-item>
                <a-form-item label="目标账号">
                    <a-select :model-value="transferModal.dstAcct" @change="(v: any) => transferPickAcct(String(v))">
                        <a-option v-for="o in dstOptions" :key="o.id" :value="o.id">{{ o.label }}（{{ kindName(o.kind) }}）</a-option>
                    </a-select>
                </a-form-item>
                <a-form-item label="目标文件名（可改）">
                    <a-input v-model="transferModal.name" placeholder="留空则沿用源名" />
                </a-form-item>
                <a-form-item label="目标目录">
                    <div class="crumbs">
                        <span class="crumb" :class="{ cur: !transferModal.crumbs.length }" @click="transferRoot">根目录</span>
                        <template v-for="(c, i) in transferModal.crumbs" :key="c.id">
                            <span class="crumb-sep">/</span>
                            <span class="crumb" :class="{ cur: i === transferModal.crumbs.length - 1 }" @click="transferGo(i)">{{ c.name }}</span>
                        </template>
                    </div>
                    <div class="move-list">
                        <div v-for="d in transferModal.dirs" :key="d.id" class="move-dir" @click="transferEnter(d)">
                            <icon-folder /> {{ d.name }}
                        </div>
                        <a-empty v-if="!transferModal.dirs.length" description="此目录下没有子目录（将转入当前目录）" />
                    </div>
                </a-form-item>
                <a-alert type="info">转存在后台异步进行：源端流式读取→目标端上传，超大文件会整段缓冲，可关页后在任务中心回看进度。</a-alert>
            </a-form>
        </a-modal>

        <!-- 转存任务抽屉：实时进度 + 取消 -->
        <a-drawer v-model:visible="trDrawer" title="转存任务" :width="440" :footer="false" @open="loadTransfers">
            <div class="tr-list">
                <div v-for="t in transfers" :key="t.id" class="tr-item">
                    <div class="tr-head">
                        <span class="tr-name" :title="t.name">{{ t.name }}</span>
                        <a-tag size="small" :color="stageTag(t.stage).c">{{ stageTag(t.stage).t }}</a-tag>
                    </div>
                    <div class="tr-sub">{{ acctName(t.srcAcct) }} → {{ acctName(t.dstAcct) }}</div>
                    <a-progress :percent="trPercent(t) / 100" size="small" :show-text="false"
                        :status="t.stage === 'error' ? 'warning' : t.stage === 'done' ? 'success' : 'normal'" />
                    <div class="tr-note">{{ t.note }}<span v-if="t.size > 0">・{{ fmtBytes(t.trans) }} / {{ fmtBytes(t.size) }}</span></div>
                    <div v-if="t.stage === 'running'" class="tr-ops">
                        <a-popconfirm content="确定取消这笔转存？" type="warning" @ok="cancelTransfer(t)">
                            <a-button size="mini" type="text" status="danger">取消</a-button>
                        </a-popconfirm>
                    </div>
                </div>
                <a-empty v-if="!transfers.length" description="暂无转存记录" />
            </div>
            <div class="tr-tip">只存最近 50 条（内存态，重启即清）；下载/上传进度请看右下角任务中心。</div>
        </a-drawer>
    </a-row>
</template>

<script setup lang="ts">
/**
 * 云盘管理页（计划-云盘管理工具 PR1，仅 Google Drive）。
 * 左栏账号 + 授权，右栏浏览与管理。绑定层 services/app/clouddrive.go（转调 tools/clouddrive）。
 * token 只在 Go 侧解密使用，本页永不接触密钥；未启用/未配客户端时只给引导不请求网络。
 */
import { computed, getCurrentInstance, onBeforeUnmount, onMounted, reactive, ref } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import { useToolWindow } from "@/views/index/useToolWindow";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const { openToolWindow } = useToolWindow();

interface AccountView { id: string; kind: string; label: string; createdAt: number; updatedAt: number; hasToken: boolean; }
interface Entry { id: string; name: string; path: string; isDir: boolean; size: number; mtime: number; mime: string; thumbUrl?: string; }
interface Quota { used: number; total: number; }
interface Transfer { id: string; op: string; srcAcct: string; srcPath: string; dstAcct: string; dstPath: string; name: string; size: number; trans: number; stage: string; note: string; createdAt: number; updatedAt: number; }
interface CloudProgress { op: string; id: string; name: string; stage: string; percent: number; note: string; }

const cfg = reactive({ enabled: false, clientReady: false });
const accounts = ref<AccountView[]>([]);
const cur = ref<AccountView | null>(null);
const entries = ref<Entry[]>([]);
const quota = ref<Quota | null>(null);
const crumbs = ref<{ id: string; name: string }[]>([]);
const loading = ref(false);

const ready = computed(() => cfg.enabled);
const canBrowse = computed(() => !!cur.value?.hasToken && cfg.enabled);
const quotaPct = computed(() => (quota.value && quota.value.total > 0 ? Math.min(1, quota.value.used / quota.value.total) : 0));

const errText = (e: any) => String(e?.message ?? e ?? "操作失败");
const kindName = (k: string) =>
    (k === "google" ? "Google Drive" : k === "aliyun" ? "阿里云盘" : k === "baidu" ? "百度网盘" : k);
const curDirID = () => (crumbs.value.length ? crumbs.value[crumbs.value.length - 1].id : "");

function fmtBytes(n: number): string {
    if (!n || n < 0) return "0 B";
    const u = ["B", "KB", "MB", "GB", "TB"];
    let i = 0, v = n;
    while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
    return `${v.toFixed(i === 0 ? 0 : 1)} ${u[i]}`;
}
function fmtTime(ms: number): string {
    if (!ms) return "--";
    try { return new Date(ms).toLocaleString(); } catch { return String(ms); }
}
async function copy(t: string) {
    if (!t) return;
    try {
        await navigator.clipboard.writeText(t);
        Message.success("已复制");
    } catch {
        const ta = document.createElement("textarea");
        ta.value = t; document.body.appendChild(ta); ta.select();
        document.execCommand("copy"); document.body.removeChild(ta);
        Message.success("已复制");
    }
}

/* ---------- 加载 ---------- */
async function loadCfg() {
    try {
        const [s, rdy] = await Promise.all([GoTools?.CloudSettings(), GoTools?.CloudClientReady()]);
        cfg.enabled = !!s?.enabled;
        cfg.clientReady = !!rdy;
    } catch { /* 后端未连 */ }
}
async function loadAccounts() {
    try {
        const list = (await GoTools?.CloudListAccounts()) ?? [];
        accounts.value = list as AccountView[];
        if (cur.value) {
            const hit = accounts.value.find((a) => a.id === cur.value!.id);
            cur.value = hit ?? null;
        }
    } catch (e: any) {
        Message.error(`读取账号失败：${errText(e)}`);
    }
}
async function load(dirID?: string) {
    if (!canBrowse.value) { entries.value = []; return; }
    loading.value = true;
    try {
        const id = dirID ?? curDirID();
        entries.value = ((await GoTools?.CloudList(cur.value!.id, id)) ?? []) as Entry[];
    } catch (e: any) {
        entries.value = [];
        Message.error(`读取云盘失败：${errText(e)}`);
    } finally {
        loading.value = false;
    }
}
async function loadQuota() {
    if (!canBrowse.value) { quota.value = null; return; }
    try { quota.value = (await GoTools?.CloudQuota(cur.value!.id)) as Quota; }
    catch { quota.value = null; }
}

function pick(a: AccountView) {
    cur.value = a;
    crumbs.value = [];
    entries.value = [];
    quota.value = null;
    void load("");
    void loadQuota();
}

/* ---------- 账号增删 ---------- */
const addModal = reactive({ open: false, saving: false, kind: "google", label: "" });
function openAdd() { addModal.kind = "google"; addModal.label = ""; addModal.open = true; }
async function doAdd() {
    addModal.saving = true;
    try {
        const v = await GoTools?.CloudAddAccount(addModal.kind, addModal.label);
        Message.success("已添加，请点授权完成登录");
        addModal.open = false;
        await loadAccounts();
        if (v?.id) { const hit = accounts.value.find((a) => a.id === v.id); if (hit) pick(hit); }
    } catch (e: any) {
        Message.error(`添加失败：${errText(e)}`);
    } finally { addModal.saving = false; }
}
async function remove(a: AccountView) {
    try {
        await GoTools?.CloudRemoveAccount(a.id);
        if (cur.value?.id === a.id) { cur.value = null; entries.value = []; quota.value = null; }
        await loadAccounts();
        Message.success("已删除账号");
    } catch (e: any) {
        Message.error(`删除失败：${errText(e)}`);
    }
}

/* ---------- 授权 ---------- */
const authModal = reactive({ open: false, saving: false, acctID: "", kind: "google", url: "", code: "" });
const authTitle = computed(() => `授权 ${kindName(authModal.kind)}`);
const credPlaceholder = computed(() =>
    authModal.kind === "aliyun" ? "粘贴 refresh_token（以 Rt 开头的一长串）" : "粘贴 BDUSS 的值（392 位左右的长串）");
const credHint = computed(() =>
    authModal.kind === "aliyun"
        ? "刷新页登录 alipan.com 后从开发者请求里拿 refresh_token。属现网逆向，不保证真网长期可用。"
        : "登录 pan.baidu.com 后从 Cookie 里复制 BDUSS 的值。属现网逆向，不保证真网长期可用。");
function openAuth(a: AccountView) {
    authModal.acctID = a.id; authModal.kind = a.kind; authModal.url = ""; authModal.code = ""; authModal.open = true;
}
function onAuthOk() {
    return authModal.kind === "google" ? doExchange() : doSetCredential();
}
async function genAuthUrl() {
    try { authModal.url = (await GoTools?.CloudAuthURL(authModal.acctID)) ?? ""; }
    catch (e: any) { Message.error(`生成授权链接失败：${errText(e)}`); }
}
async function doExchange() {
    if (!authModal.code.trim()) { Message.warning("请粘贴回跳 URL 或授权码"); return; }
    authModal.saving = true;
    try {
        await GoTools?.CloudExchange(authModal.acctID, authModal.code.trim());
        Message.success("授权成功");
        authModal.open = false;
        await loadAccounts();
        if (cur.value?.id === authModal.acctID) { void load(""); void loadQuota(); }
    } catch (e: any) {
        Message.error(`授权失败：${errText(e)}`);
    } finally { authModal.saving = false; }
}
async function doSetCredential() {
    if (!authModal.code.trim()) { Message.warning("请粘贴凭证"); return; }
    authModal.saving = true;
    try {
        await GoTools?.CloudSetCredential(authModal.acctID, authModal.code.trim());
        Message.success("凭证已保存");
        authModal.open = false;
        await loadAccounts();
        if (cur.value?.id === authModal.acctID) { void load(""); void loadQuota(); }
    } catch (e: any) {
        Message.error(`保存凭证失败：${errText(e)}`);
    } finally { authModal.saving = false; }
}

/* ---------- 导航 ---------- */
function onOpen(r: Entry) {
    if (r.isDir) { crumbs.value.push({ id: r.id, name: r.name }); void load(r.id); }
    else preview(r);
}
function goUp() { if (!crumbs.value.length) return; crumbs.value.pop(); void load(curDirID()); }
function goRoot() { crumbs.value = []; void load(""); }
function goCrumb(i: number) { crumbs.value = crumbs.value.slice(0, i + 1); void load(curDirID()); }

/* ---------- 增删改 ---------- */
const prompt = reactive({
    open: false, saving: false, kind: "" as "" | "mkdir" | "rename" | "upload",
    title: "", fieldLabel: "", placeholder: "", value: "", localPath: "", target: null as Entry | null,
});
function openMkdir() {
    Object.assign(prompt, { kind: "mkdir", title: "新建目录", fieldLabel: "目录名", placeholder: "例如：报告", value: "", localPath: "", target: null, open: true, saving: false });
}
function openRename(r: Entry) {
    Object.assign(prompt, { kind: "rename", title: `重命名「${r.name}」`, fieldLabel: "新名称", placeholder: r.name, value: r.name, localPath: "", target: r, open: true, saving: false });
}
function openUpload() {
    Object.assign(prompt, { kind: "upload", title: "上传本地文件", fieldLabel: "云端文件名（留空用原名）", placeholder: "可选", value: "", localPath: "", target: null, open: true, saving: false });
}
async function chooseFile() {
    try {
        const p = await GoTools?.CloudChooseFile("选择要上传的文件");
        if (p) prompt.localPath = String(p);
    } catch (e: any) {
        Message.warning(`无法打开选择框（Web 模式请手输路径）：${errText(e)}`);
    }
}
async function doPrompt() {
    if (!cur.value) return;
    prompt.saving = true;
    try {
        if (prompt.kind === "mkdir") {
            if (!prompt.value.trim()) throw new Error("目录名不能为空");
            await GoTools?.CloudMkdir(cur.value.id, curDirID(), prompt.value.trim());
            Message.success("已创建目录");
        } else if (prompt.kind === "rename") {
            if (!prompt.target || !prompt.value.trim()) throw new Error("名称不能为空");
            await GoTools?.CloudRename(cur.value.id, prompt.target.id, prompt.value.trim());
            Message.success("已重命名");
        } else if (prompt.kind === "upload") {
            if (!prompt.localPath.trim()) throw new Error("请选择或输入本地文件路径");
            const nm = prompt.value.trim() || prompt.localPath.replace(/[\\/]+$/, "").split(/[\\/]/).pop() || "file";
            await GoTools?.CloudUpload(cur.value.id, curDirID(), nm, prompt.localPath.trim());
            Message.success("已上传");
        }
        prompt.open = false;
        await load();
    } catch (e: any) {
        Message.error(`操作失败：${errText(e)}`);
    } finally { prompt.saving = false; }
}

async function del(r: Entry) {
    if (!cur.value) return;
    try {
        await GoTools?.CloudDelete(cur.value.id, r.id);
        Message.success("已删除");
        await load();
    } catch (e: any) {
        Message.error(`删除失败：${errText(e)}`);
    }
}

async function download(r: Entry) {
    if (!cur.value || r.isDir) return;
    loading.value = true;
    try {
        const path = await GoTools?.CloudDownload(cur.value.id, r.id, r.name, "");
        Message.success(`已下载：${path}`);
    } catch (e: any) {
        Message.error(`下载失败：${errText(e)}`);
    } finally { loading.value = false; }
}

/* ---------- 移动 ---------- */
const moveModal = reactive({
    open: false, saving: false, target: null as Entry | null,
    crumbs: [] as { id: string; name: string }[], dirs: [] as Entry[],
});
async function moveLoad() {
    if (!cur.value) return;
    const id = moveModal.crumbs.length ? moveModal.crumbs[moveModal.crumbs.length - 1].id : "";
    try {
        const list = ((await GoTools?.CloudList(cur.value.id, id)) ?? []) as Entry[];
        moveModal.dirs = list.filter((e) => e.isDir && (!moveModal.target || e.id !== moveModal.target.id));
    } catch (e: any) {
        Message.error(`读取目录失败：${errText(e)}`);
    }
}
function openMove(r: Entry) {
    moveModal.target = r; moveModal.crumbs = []; moveModal.dirs = []; moveModal.open = true;
    void moveLoad();
}
function moveEnter(d: Entry) { moveModal.crumbs.push({ id: d.id, name: d.name }); void moveLoad(); }
function moveRoot() { moveModal.crumbs = []; void moveLoad(); }
function moveGo(i: number) { moveModal.crumbs = moveModal.crumbs.slice(0, i + 1); void moveLoad(); }
async function doMove() {
    if (!cur.value || !moveModal.target) return;
    moveModal.saving = true;
    try {
        await GoTools?.CloudMove(cur.value.id, moveModal.target.id, moveModal.crumbs.length ? moveModal.crumbs[moveModal.crumbs.length - 1].id : "");
        Message.success("已移动");
        moveModal.open = false;
        await load();
    } catch (e: any) {
        Message.error(`移动失败：${errText(e)}`);
    } finally { moveModal.saving = false; }
}

/* ---------- 分享 ---------- */
const shareModal = reactive({ open: false, url: "" });
async function share(r: Entry) {
    if (!cur.value) return;
    try {
        shareModal.url = (await GoTools?.CloudShareLink(cur.value.id, r.id)) ?? "";
        shareModal.open = !!shareModal.url;
    } catch (e: any) {
        Message.error(`分享失败：${errText(e)}`);
    }
}

/* ---------- 预览（接文件查看器，云端走 src=cloud 三元组流式） ---------- */
function preview(r: Entry) {
    if (!cur.value || r.isDir) return;
    const acct = cur.value.id;
    const qs = `?src=cloud&sid=${encodeURIComponent(acct)}&path=${encodeURIComponent(r.id)}&name=${encodeURIComponent(r.name)}`;
    void openToolWindow(`/tools/viewer${qs}`, "文件查看器", { single: true });
    try { Events.Emit("viewer:open-path", { src: "cloud", sid: acct, path: r.id, name: r.name }); } catch { /* 非 Wails 环境忽略 */ }
}

/* ---------- 行「更多」菜单 ---------- */
function onRowMore(key: string, r: Entry) {
    switch (key) {
        case "preview": preview(r); break;
        case "transfer": openTransfer(r); break;
        case "rename": openRename(r); break;
        case "move": openMove(r); break;
        case "share": share(r); break;
        case "del":
            Modal.warning({ title: "删除确认", content: `从云端彻底删除「${r.name}」，不可恢复，确定？`, okText: "删除", hideCancel: false, onOk: () => del(r) });
            break;
    }
}

/* ---------- 跨盘转存：目标账号 + 目录选择 ---------- */
const transferModal = reactive({
    open: false, saving: false, src: null as Entry | null, dstAcct: "",
    crumbs: [] as { id: string; name: string }[], dirs: [] as Entry[], name: "",
});
const dstOptions = computed(() => accounts.value.filter((a) => a.hasToken && a.id !== cur.value?.id));
function openTransfer(r: Entry) {
    if (!cur.value) return;
    if (!dstOptions.value.length) { Message.warning("转存需要至少另一个已授权的云账号"); return; }
    Object.assign(transferModal, { open: true, saving: false, src: r, dstAcct: dstOptions.value[0].id, crumbs: [], dirs: [], name: r.name });
    void transferLoad();
}
async function transferLoad() {
    if (!transferModal.dstAcct) return;
    const id = transferModal.crumbs.length ? transferModal.crumbs[transferModal.crumbs.length - 1].id : "";
    try {
        const list = ((await GoTools?.CloudList(transferModal.dstAcct, id)) ?? []) as Entry[];
        transferModal.dirs = list.filter((e) => e.isDir);
    } catch (e: any) { Message.error(`读取目标目录失败：${errText(e)}`); }
}
function transferEnter(d: Entry) { transferModal.crumbs.push({ id: d.id, name: d.name }); void transferLoad(); }
function transferRoot() { transferModal.crumbs = []; void transferLoad(); }
function transferGo(i: number) { transferModal.crumbs = transferModal.crumbs.slice(0, i + 1); void transferLoad(); }
function transferPickAcct(id: string) { transferModal.dstAcct = id; transferModal.crumbs = []; void transferLoad(); }
async function doTransfer() {
    if (!cur.value || !transferModal.src) return;
    transferModal.saving = true;
    try {
        const dstParent = transferModal.crumbs.length ? transferModal.crumbs[transferModal.crumbs.length - 1].id : "";
        const tr = await GoTools?.CloudTransfer(cur.value.id, transferModal.src.id, curDirID(), transferModal.dstAcct, dstParent, transferModal.name.trim() || transferModal.src.name);
        Message.success("已发起跨盘转存，可在「转存任务」查看进度");
        transferModal.open = false;
        if (tr?.id) upsertTransfer(tr as Transfer, 0);
        trDrawer.value = true;
    } catch (e: any) {
        Message.error(`转存发起失败：${errText(e)}`);
    } finally { transferModal.saving = false; }
}

/* ---------- 转存任务列表 + 实时进度 ---------- */
const transfers = ref<Transfer[]>([]);
const trPct = ref<Record<string, number>>({});
const trDrawer = ref(false);
const trRunning = computed(() => transfers.value.filter((t) => t.stage === "running").length);
async function loadTransfers() {
    try { transfers.value = ((await GoTools?.CloudListTransfers()) ?? []) as Transfer[]; }
    catch { transfers.value = []; }
}
function upsertTransfer(t: Transfer, pct: number) {
    const i = transfers.value.findIndex((x) => x.id === t.id);
    if (i >= 0) transfers.value[i] = t; else transfers.value = [t, ...transfers.value];
    trPct.value[t.id] = pct;
}
function onCloudProgress(ev: any) {
    const p = (ev?.data ?? ev) as CloudProgress;
    if (!p || !p.id || p.op !== "transfer") return; // 下载/上传只进任务中心
    const i = transfers.value.findIndex((x) => x.id === p.id);
    if (i >= 0) {
        const t = { ...transfers.value[i] };
        t.stage = p.stage; t.note = p.note;
        if (p.stage === "done" && t.size > 0) t.trans = t.size;
        transfers.value[i] = t;
    } else {
        void loadTransfers();
    }
    trPct.value[p.id] = p.percent;
}
async function cancelTransfer(t: Transfer) {
    try { await GoTools?.CloudCancelTransfer(t.id); Message.success("已请求取消"); }
    catch (e: any) { Message.error(`取消失败：${errText(e)}`); }
}
function trPercent(t: Transfer): number {
    if (t.stage === "done") return 100;
    const live = trPct.value[t.id];
    if (typeof live === "number") return live;
    if (t.size > 0 && t.trans > 0) return Math.min(99, Math.floor((t.trans * 100) / t.size));
    return 0;
}
const stageTag = (s: string) => (s === "done" ? { t: "已完成", c: "green" } : s === "error" ? { t: "失败", c: "red" } : s === "canceled" ? { t: "已取消", c: "gray" } : { t: "进行中", c: "blue" });
const acctName = (id: string) => accounts.value.find((a) => a.id === id)?.label || id;

let offProgress: any = null;
onMounted(async () => {
    await loadCfg();
    await loadAccounts();
    void loadTransfers();
    try { offProgress = Events.On("cloud:progress", (ev: any) => onCloudProgress(ev)); } catch { /* 非 Wails 环境忽略 */ }
});
onBeforeUnmount(() => { try { offProgress?.(); } catch { /* ignore */ } });
</script>

<style scoped>
.cd-page {
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
}

.cd-warn {
    margin-bottom: 10px;
    font-size: var(--base-size-small);
}

.acct {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 6px;
    padding: 8px 10px;
    border-radius: var(--base-radius-default);
    cursor: pointer;
}

.acct:hover {
    background: var(--color-fill-2);
}

.acct.active {
    background: var(--color-primary-light-1);
    box-shadow: inset 3px 0 0 var(--color-primary-6);
}

.acct-name {
    font-weight: 600;
    color: var(--color-text-1);
}

.acct-sub {
    display: flex;
    gap: 6px;
    margin-top: 4px;
}

.quota {
    margin-top: 12px;
    padding-top: 10px;
    border-top: 1px solid var(--color-border-2);
}

.quota-txt {
    font-size: var(--base-size-small);
    color: var(--color-text-2);
    margin-bottom: 4px;
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

.cd-hint {
    padding: 40px 0;
}

.crumbs {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 4px;
    margin-bottom: 10px;
    font-size: var(--base-size-small);
}

.crumb {
    color: var(--color-text-2);
    cursor: pointer;
}

.crumb:hover {
    color: rgb(var(--primary-6));
}

.crumb.cur {
    color: var(--color-text-1);
    font-weight: 600;
    cursor: default;
}

.crumb-sep {
    color: var(--color-text-4);
}

.fname {
    display: inline-flex;
    align-items: center;
    gap: 6px;
}

.fname.dir {
    cursor: pointer;
    color: rgb(var(--primary-6));
}

.move-list {
    max-height: 320px;
    overflow-y: auto;
    margin-top: 8px;
}

.move-dir {
    padding: 6px 8px;
    cursor: pointer;
    display: flex;
    align-items: center;
    gap: 8px;
    border-radius: var(--base-radius-default);
}

.move-dir:hover {
    background: var(--color-fill-2);
}

.auth-step {
    display: flex;
    flex-direction: column;
    gap: 10px;
}

.auth-h {
    color: var(--color-text-2);
    font-size: var(--base-size-small);
}

.auth-url {
    display: flex;
}

.auth-tip {
    color: var(--color-text-3);
    font-size: 12px;
    line-height: 1.6;
}

.sh-url {
    display: flex;
    margin-bottom: 8px;
}

.mono {
    font-family: Consolas, Menlo, monospace;
    font-size: var(--base-size-small);
}

.cd-table {
    width: 100%;
}

.danger {
    color: rgb(var(--danger-6));
}

.tr-list {
    display: flex;
    flex-direction: column;
    gap: 12px;
}

.tr-item {
    padding: 10px 12px;
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-default);
}

.tr-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
}

.tr-name {
    font-weight: 600;
    color: var(--color-text-1);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.tr-sub {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    margin: 4px 0 6px;
}

.tr-note {
    font-size: 12px;
    color: var(--color-text-2);
    margin-top: 4px;
    word-break: break-all;
}

.tr-ops {
    margin-top: 6px;
    text-align: right;
}

.tr-tip {
    margin-top: 14px;
    font-size: 12px;
    color: var(--color-text-3);
    line-height: 1.6;
}
</style>
