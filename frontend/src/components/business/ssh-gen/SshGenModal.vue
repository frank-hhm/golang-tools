<template>
  <a-modal :visible="visible" title="SSH 生成助手" :width="640" :mask-closable="modalClosable"
    :esc-to-close="modalClosable" :body-style="{ height: '480px', overflowY: 'auto' }"
    @update:visible="(v: boolean) => emit('update:visible', v)" @cancel="close">
    <!-- 统一保存目录：默认取 SSHDefaultDir()，可换；结果区「另存」都写到这里。淡底胶囊条，与弹窗内容区分层 -->
    <div class="sg-dir">
      <icon-folder />
      <span class="sg-dir-label">保存目录</span>
      <a-input v-model="saveDir" size="small" class="sg-dir-input" placeholder="结果与密钥的保存目录" readonly borderless />
      <a-button size="small" @click="pickDir">更改</a-button>
    </div>

    <a-tabs v-model:active-key="tab" type="rounded" class="sg-tabs">
      <!-- ==================== 生成密钥对 ==================== -->
      <a-tab-pane key="key" title="生成密钥对">
        <!-- 标签改纵向 + 短字段两列并排，和 FTP 连接表单同一度量；横向空标签栏那套去掉 -->
        <a-form :model="k" layout="vertical" class="sg-form">
          <a-form-item label="算法">
            <a-radio-group v-model="k.type" type="button" size="small" @change="onTypeChange">
              <a-radio value="ed25519">ed25519（推荐）</a-radio>
              <a-radio value="rsa">RSA</a-radio>
              <a-radio value="ecdsa">ECDSA</a-radio>
            </a-radio-group>
          </a-form-item>
          <a-row :gutter="8">
            <a-col v-if="k.type === 'rsa'" :span="12">
              <a-form-item label="位数">
                <a-select v-model="k.bits" size="small">
                  <a-option :value="2048">2048</a-option>
                  <a-option :value="3072">3072</a-option>
                  <a-option :value="4096">4096</a-option>
                </a-select>
              </a-form-item>
            </a-col>
            <a-col v-else-if="k.type === 'ecdsa'" :span="12">
              <a-form-item label="曲线">
                <a-select v-model="k.bits" size="small">
                  <a-option :value="256">P-256</a-option>
                  <a-option :value="384">P-384</a-option>
                  <a-option :value="521">P-521</a-option>
                </a-select>
              </a-form-item>
            </a-col>
            <a-col :span="k.type === 'ed25519' ? 24 : 12">
              <a-form-item label="注释">
                <a-input v-model="k.comment" size="small" placeholder="留空自动取 user@host" allow-clear />
              </a-form-item>
            </a-col>
          </a-row>
          <a-row :gutter="8">
            <a-col :span="12">
              <a-form-item label="文件名">
                <a-input v-model="k.name" size="small" placeholder="留空自动命名（id_类型）" allow-clear />
              </a-form-item>
            </a-col>
            <a-col :span="12">
              <a-form-item label="私钥口令">
                <a-input-password v-model="k.passphrase" size="small" placeholder="可留空；仅本次加密用" allow-clear />
              </a-form-item>
            </a-col>
          </a-row>
          <a-form-item v-if="k.passphrase" label="确认口令">
            <a-input-password v-model="k.passConfirm" size="small" placeholder="再次输入口令" allow-clear />
          </a-form-item>
        </a-form>

        <template v-if="kp">
          <a-divider margin="12px 0" />
          <div class="sg-pair">
            <div class="sg-fp">
              <span class="sg-fp-k">私钥路径</span>
              <span class="sg-fp-v">{{ kp.privPath }}</span>
              <a-button size="mini" type="text" @click="copyPlain(kp.privPath)">
                <template #icon><icon-copy /></template>复制
              </a-button>
            </div>
            <div class="sg-fp">
              <span class="sg-fp-k">指纹 SHA256</span>
              <span class="sg-fp-v">{{ kp.fingerprintSHA256 }}</span>
              <a-button size="mini" type="text" @click="copyPlain(kp.fingerprintSHA256)">
                <template #icon><icon-copy /></template>复制
              </a-button>
            </div>
            <ResultBlock label="公钥行（authorized_keys）" :text="kp.pubKeyLine" :dir="saveDir"
              :filename="pubFilename" mono hint="部署或粘贴到远端用这一行" />
            <div class="sg-row">
              <a-button size="small" type="outline" @click="useForConn">用于当前连接（回填私钥）</a-button>
              <a-button size="small" @click="toDeploy">去部署公钥 →</a-button>
            </div>
          </div>
        </template>
      </a-tab-pane>

      <!-- ==================== 生成连接片段 ==================== -->
      <a-tab-pane key="cmds" title="生成连接片段">
        <a-form :model="h" layout="vertical" class="sg-form">
          <a-row :gutter="8">
            <a-col :span="16">
              <a-form-item label="主机">
                <a-input v-model="h.address" size="small" placeholder="IP 或域名" allow-clear />
              </a-form-item>
            </a-col>
            <a-col :span="8">
              <a-form-item label="端口">
                <a-input-number v-model="h.port" size="small" :min="1" :max="65535" />
              </a-form-item>
            </a-col>
          </a-row>
          <a-row :gutter="8">
            <a-col :span="12">
              <a-form-item label="用户">
                <a-input v-model="h.user" size="small" placeholder="root" allow-clear />
              </a-form-item>
            </a-col>
            <a-col :span="12">
              <a-form-item label="别名">
                <a-input v-model="h.alias" size="small" placeholder="~/.ssh/config 的 Host 别名" allow-clear />
              </a-form-item>
            </a-col>
          </a-row>
          <a-form-item label="私钥路径">
            <a-input v-model="h.keyPath" size="small" placeholder="可选，带 -i" allow-clear />
          </a-form-item>
        </a-form>

        <ResultBlock label="ssh 命令" :text="cmds?.ssh || ''" :dir="saveDir" filename="ssh-cmd.txt" mono />
        <ResultBlock label="scp 命令" :text="cmds?.scp || ''" :dir="saveDir" filename="scp-cmd.txt" mono />
        <ResultBlock label="sftp 命令" :text="cmds?.sftp || ''" :dir="saveDir" filename="sftp-cmd.txt" mono />
        <ResultBlock label="~/.ssh/config 片段" :text="cmds?.configBlock || ''" :dir="saveDir" filename="ssh_config" mono />

        <!-- 导出入口收进 footer；导出过的凭据文本留在页内供复制/另存 -->
        <ResultBlock v-if="connBlob" label="凭据文本（不含密码/私钥）" :text="connBlob" :dir="saveDir"
          filename="ssh-conn.json" mono />
      </a-tab-pane>

      <!-- ==================== 部署公钥 ==================== -->
      <a-tab-pane key="deploy" title="部署公钥">
        <a-alert type="warning" class="sg-alert">
          仅支持 Linux/POSIX 远端；Windows 远端 OpenSSH 的授权文件要求严格 ACL，请用本机方式配置。
        </a-alert>
        <a-form :model="d" layout="vertical" class="sg-form">
          <a-row :gutter="8">
            <a-col :span="16">
              <a-form-item label="主机">
                <a-input v-model="d.address" size="small" placeholder="IP 或域名" allow-clear />
              </a-form-item>
            </a-col>
            <a-col :span="8">
              <a-form-item label="端口">
                <a-input-number v-model="d.port" size="small" :min="1" :max="65535" />
              </a-form-item>
            </a-col>
          </a-row>
          <a-row :gutter="8">
            <a-col :span="10">
              <a-form-item label="用户">
                <a-input v-model="d.user" size="small" placeholder="root" allow-clear />
              </a-form-item>
            </a-col>
            <a-col :span="14">
              <a-form-item label="认证方式">
                <a-radio-group v-model="d.auth" type="button" size="small">
                  <a-radio value="password">密码</a-radio>
                  <a-radio value="key">已有密钥</a-radio>
                </a-radio-group>
              </a-form-item>
            </a-col>
          </a-row>
          <a-row v-if="d.auth === 'password'" :gutter="8">
            <a-col :span="24">
              <a-form-item label="密码">
                <a-input-password v-model="d.password" size="small" placeholder="登录密码，仅本次使用" allow-clear />
              </a-form-item>
            </a-col>
          </a-row>
          <a-row v-else :gutter="8">
            <a-col :span="14">
              <a-form-item label="私钥文件">
                <a-input v-model="d.keyPath" size="small" placeholder="本机已有私钥路径" allow-clear />
              </a-form-item>
            </a-col>
            <a-col :span="10">
              <a-form-item label="私钥口令">
                <a-input-password v-model="d.passphrase" size="small" placeholder="私钥带口令才填" allow-clear />
              </a-form-item>
            </a-col>
          </a-row>
          <a-form-item label="公钥行">
            <a-textarea v-model="d.pubKey" :auto-size="{ minRows: 2, maxRows: 4 }" placeholder="待部署的公钥（单行）" />
          </a-form-item>
        </a-form>

        <template v-if="deployPercent !== null">
          <a-progress :percent="deployPercent" :status="deployStatus" size="small" class="sg-prog" />
          <div v-if="deployNote" class="sg-note" :class="'is-' + deployStage">{{ deployNote }}</div>
        </template>
        <a-result v-if="deployResult" :status="deployResult.existed ? 'warning' : 'success'"
          :title="deployResult.existed ? '公钥已存在，未重复添加' : '公钥部署成功'"
          :subtitle="deployResult.path" class="sg-result" />
      </a-tab-pane>
    </a-tabs>

    <!-- 主操作统一收进 footer：左说明随 tab 切换，右侧按钮靠齐；部署中禁用遮罩/ESC 防误触 -->
    <template #footer>
      <div class="sg-footer">
        <span class="sg-footer-hint">{{ footerHint }}</span>
        <a-space v-if="tab === 'key'" :size="8">
          <a-button size="small" type="primary" :loading="genBusy" @click="doGenerate">生成密钥对</a-button>
        </a-space>
        <a-space v-else-if="tab === 'cmds'" :size="8">
          <a-button size="small" :disabled="!h.address" @click="doExport">导出连接凭据</a-button>
          <a-button size="small" @click="close">关闭</a-button>
        </a-space>
        <a-space v-else :size="8">
          <a-button size="small" type="primary" :loading="deployBusy" @click="doDeploy">部署公钥</a-button>
        </a-space>
      </div>
    </template>
  </a-modal>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onBeforeUnmount, reactive, ref, watch } from 'vue';
import { Message } from '@arco-design/web-vue';
import { Events } from '@wailsio/runtime';
import ResultBlock from './ResultBlock.vue';
import type { KeyPair, Commands, DeployResult, Progress } from 'bindings/Tools/tools/sshgen/index';

/**
 * SSH 生成助手（共享弹窗，不入工具）。三 tab：生成密钥对 / 生成连接片段 / 部署公钥。
 * 宿主面板（FTP 连接表单、终端主机表单）经 props.context 传入当前主机上下文，
 * 生成密钥后 emit useKey 让宿主回填 keyPath；本组件不反向依赖任何具体面板。
 */
const props = defineProps<{
  visible: boolean;
  context?: { address?: string; port?: number; user?: string; keyPath?: string };
}>();
const emit = defineEmits<{
  (e: 'update:visible', v: boolean): void;
  (e: 'useKey', payload: { keyPath: string; pubKey: string }): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const tab = ref<'key' | 'cmds' | 'deploy'>('key');
const saveDir = ref('');
const modalClosable = ref(true);

/* footer 左侧说明：随当前 tab 切换，与右侧主操作按钮同一语境 */
const footerHint = computed(() => {
  if (tab.value === 'key') return '私钥/公钥成对写入保存目录，私钥正文不回传';
  if (tab.value === 'cmds') return '导出 JSON 凭据文本（不含密码/私钥），可另存后在其它机器导入';
  return '走 SFTP 读写 authorized_keys，已存在则不重复追加';
});

/* ---------- 密钥 tab ---------- */
const k = reactive({ type: 'ed25519', bits: 3072, comment: '', name: '', passphrase: '', passConfirm: '' });
const genBusy = ref(false);
const kp = ref<KeyPair | null>(null);
const pubFilename = computed(() => (kp.value ? `${kp.value.type}_id.pub` : 'id.pub'));

async function copyPlain(t: string) {
  if (!t) return;
  try {
    await navigator.clipboard.writeText(t);
    Message.success({ content: '已复制', position: 'top', duration: 1200 });
  } catch {
    Message.error({ content: '复制失败', position: 'top', duration: 1500 });
  }
}

function onTypeChange() {
  if (k.type === 'rsa') k.bits = 3072;
  else if (k.type === 'ecdsa') k.bits = 256;
  else k.bits = 0;
}

async function doGenerate() {
  if (k.passphrase && k.passphrase !== k.passConfirm) {
    Message.error({ content: '两次输入的口令不一致', position: 'top', duration: 2000 });
    return;
  }
  genBusy.value = true;
  try {
    const res: KeyPair = await GoTools.SSHGenKey({
      type: k.type, bits: k.bits, comment: k.comment, passphrase: k.passphrase,
      dir: saveDir.value, name: k.name,
    });
    kp.value = res;
    // 生成后把公钥带到部署 tab
    d.pubKey = res.pubKeyLine;
    Message.success({ content: '密钥对已生成', position: 'top', duration: 2000 });
  } catch (e: any) {
    Message.error({ content: '生成失败：' + String(e?.message || e), position: 'top', duration: 3000 });
  } finally {
    genBusy.value = false;
  }
}

function useForConn() {
  if (!kp.value) return;
  emit('useKey', { keyPath: kp.value.privPath, pubKey: kp.value.pubKeyLine });
  Message.success({ content: '已回填私钥路径', position: 'top', duration: 1500 });
  close();
}

function toDeploy() {
  if (!kp.value) return;
  d.pubKey = kp.value.pubKeyLine;
  if (props.context?.address) d.address = props.context.address;
  if (props.context?.user) d.user = props.context.user;
  d.port = props.context?.port || 22;
  tab.value = 'deploy';
}

/* ---------- 片段 tab ---------- */
const h = reactive({ address: '', port: 22, user: 'root', alias: '', keyPath: '' });
const cmds = ref<Commands | null>(null);
const connBlob = ref('');
let cmdsTimer: ReturnType<typeof setTimeout> | undefined;

watch([() => h.address, () => h.port, () => h.user, () => h.alias, () => h.keyPath], scheduleCmds);
function scheduleCmds() {
  if (cmdsTimer) clearTimeout(cmdsTimer);
  cmdsTimer = setTimeout(refreshCmds, 200);
}
async function refreshCmds() {
  if (!h.address.trim()) { cmds.value = null; return; }
  try {
    cmds.value = await GoTools.SSHCommands({
      address: h.address, port: h.port, user: h.user, alias: h.alias, keyPath: h.keyPath, pubKey: '', comment: '',
    });
  } catch {
    cmds.value = null;
  }
}

async function doExport() {
  if (!h.address.trim()) return;
  try {
    connBlob.value = await GoTools.SSHExportConn({
      address: h.address, port: h.port, user: h.user, alias: h.alias, keyPath: h.keyPath,
      pubKey: kp.value?.pubKeyLine || '', comment: '',
    });
  } catch (e: any) {
    Message.error({ content: '导出失败：' + String(e?.message || e), position: 'top', duration: 3000 });
  }
}

/* ---------- 部署 tab ---------- */
const d = reactive({
  address: '', port: 22, user: 'root', auth: 'password',
  password: '', keyPath: '', passphrase: '', pubKey: '',
});
const deployBusy = ref(false);
const deployResult = ref<DeployResult | null>(null);
const deployPercent = ref<number | null>(null);
const deployNote = ref('');
const deployStage = ref<'running' | 'done' | 'error' | ''>('');
const deployStatus = computed<'normal' | 'success' | 'warning' | 'danger'>(() => {
  if (deployStage.value === 'error') return 'danger';
  if (deployStage.value === 'done') return 'success';
  return 'normal';
});
let offEvent: (() => void) | undefined;

function onProgress(ev: any) {
  const p = (ev?.data ?? ev) as Progress;
  if (!p) return;
  deployStage.value = p.stage as any;
  deployPercent.value = (p.percent || 0) / 100;
  deployNote.value = p.note || '';
}

async function doDeploy() {
  if (!d.address.trim()) { Message.error({ content: '请填写目标主机', position: 'top', duration: 2000 }); return; }
  if (!d.pubKey.trim()) { Message.error({ content: '请先在密钥 tab 生成或粘贴公钥', position: 'top', duration: 2000 }); return; }
  if (d.auth === 'password' && !d.password) { Message.error({ content: '请输入登录密码', position: 'top', duration: 2000 }); return; }
  deployBusy.value = true;
  deployResult.value = null;
  deployPercent.value = 0;
  deployNote.value = '';
  deployStage.value = 'running';
  try {
    const r = await GoTools.SSHDeploy({
      address: d.address, port: d.port, user: d.user, auth: d.auth,
      password: d.password, keyPath: d.keyPath, passphrase: d.passphrase, pubKey: d.pubKey,
    });
    deployResult.value = r;
    deployStage.value = 'done';
    deployPercent.value = 1;
    // 口令/密码即时清空，不留内存引用之外的暴露面
    d.password = '';
    d.passphrase = '';
  } catch (e: any) {
    deployStage.value = 'error';
    deployNote.value = String(e?.message || e);
    Message.error({ content: '部署失败：' + String(e?.message || e), position: 'top', duration: 4000 });
  } finally {
    deployBusy.value = false;
  }
}

/* ---------- 目录 & 生命周期 ---------- */
async function loadDefaultDir() {
  if (saveDir.value) return;
  try {
    saveDir.value = (await GoTools.SSHDefaultDir()) || '';
  } catch { /* 取不到就留空，另存按钮会禁用 */ }
}

async function pickDir() {
  try {
    const dir = await GoTools.ChooseDir('选择 SSH 助手保存目录');
    if (dir) saveDir.value = dir;
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '选择目录失败'), position: 'top', duration: 2000 });
  }
}

function applyContext() {
  const c = props.context;
  if (!c) return;
  if (c.address) { h.address = c.address; d.address = c.address; }
  if (c.port) { h.port = c.port; d.port = c.port; }
  if (c.user) { h.user = c.user; d.user = c.user; }
  if (c.keyPath) { h.keyPath = c.keyPath; d.keyPath = c.keyPath; }
}

function close() {
  emit('update:visible', false);
}

watch(() => props.visible, (v) => {
  if (!v) return;
  loadDefaultDir();
  applyContext();
  refreshCmds();
  offEvent = Events.On('sshgen:progress', onProgress);
}, { immediate: true });

// 部署进行中不允许遮罩/ESC 关窗，避免半截 authorized_keys 写入被误触中断
watch(deployBusy, (v) => {
  modalClosable.value = !v;
});

onBeforeUnmount(() => {
  if (offEvent) offEvent();
  if (cmdsTimer) clearTimeout(cmdsTimer);
});
</script>

<style scoped>
/* 保存目录胶囊条：淡底圆角，把“环境信息”与下方 tab 内容分层 */
.sg-dir {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
  padding: 5px 10px;
  border-radius: var(--base-radius-small);
  background: var(--color-fill-1);
  border: 1px solid var(--color-border-1);
  color: var(--color-text-2);
}

.sg-dir-label {
  font-size: var(--base-size-small);
  white-space: nowrap;
}

.sg-dir-input {
  flex: 1;
  min-width: 0;
}

.sg-tabs :deep(.arco-tabs-content) {
  padding-top: 8px;
}

/* 纵向表单：字段间距由组容器 gap 统一管，标签贴住自己的输入框（与 FTP 连接表单同度量） */
.sg-form :deep(.arco-form-item) {
  margin-bottom: 0;
}

.sg-form :deep(.arco-form-item-label-col) {
  margin-bottom: 4px;
}

.sg-form {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

/* 数字框默认不撑满，放进 a-col 会缩成一小截 */
.sg-form :deep(.arco-input-number) {
  width: 100%;
}

.sg-row {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 8px 0;
}

/* footer：左说明右按钮（与仓库设置/任务弹窗同一交互） */
.sg-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.sg-footer-hint {
  flex: 1;
  min-width: 0;
  color: var(--color-text-3);
  font-size: var(--base-size-small);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sg-tip {
  font-size: var(--base-size-small);
}

.sg-pair {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

/* 密钥回执行：与 ResultBlock 同族的小卡片，头栏淡底 + 正文等宽值 */
.sg-fp {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 3px 4px 3px 10px;
  border: 1px solid var(--code-border);
  border-radius: var(--base-radius-small);
  background: var(--code-bg);
  font-size: var(--base-size-small);
}

.sg-fp-k {
  flex: none;
  color: var(--color-text-3);
  white-space: nowrap;
}

.sg-fp-v {
  flex: 1;
  min-width: 0;
  color: var(--code-text);
  word-break: break-all;
  font-family: var(--code-font-family);
}

.sg-alert {
  margin-bottom: 12px;
}

.sg-prog {
  margin: 8px 0;
}

.sg-note {
  font-size: var(--base-size-small);
  word-break: break-all;
}

.sg-note.is-error {
  color: rgb(var(--red-6));
}

.sg-note.is-done {
  color: rgb(var(--green-6));
}

.sg-result {
  padding: 8px 0;
}
</style>
