<template>
  <a-modal :visible="visible" :title="form.id ? '编辑连接' : '添加连接'" :width="560" :mask-closable="false"
    ok-text="保存并连接" cancel-text="取消" :ok-loading="busy" :on-before-ok="onBeforeOk"
    @cancel="close" @update:visible="(v: boolean) => emit('update:visible', v)">
    <!-- 三段式：服务器 / 登录方式 / 打开后。标签改纵向 + 分组小标题，横向布局那条空标签栏整条去掉，
         弹窗矮一截；主机与端口、账号与验证方式、目录与备注各自并排，不再一行只放一个字段 -->
    <a-form :model="form" layout="vertical" class="cf-form">
      <div class="cf-group">
        <div class="cf-group-head">
          <span class="cf-group-title">服务器</span>
          <span class="cf-group-tail">
            <a-tag size="small" color="arcoblue">SFTP</a-tag>
            <span class="cf-group-dim">FTP / FTPS 在后续版本提供</span>
          </span>
        </div>
        <a-row :gutter="8">
          <a-col :span="17">
            <a-form-item label="主机" required>
              <a-input v-model="form.host" placeholder="IP 或域名，如 192.168.1.10" allow-clear />
            </a-form-item>
          </a-col>
          <a-col :span="7">
            <a-form-item label="端口">
              <a-input-number v-model="form.port" :min="1" :max="65535" :step="1" :precision="0" />
            </a-form-item>
          </a-col>
        </a-row>
      </div>

      <div class="cf-group">
        <div class="cf-group-head">
          <span class="cf-group-title">登录方式</span>
          <span class="cf-group-dim">凭据与终端共用一份，改一次两边生效</span>
        </div>

        <a-form-item label="共享凭据">
          <div class="cred-row">
            <a-select v-model="form.credID" placeholder="选择已保存的凭据（推荐）" allow-clear allow-search
              @change="onCredChange">
              <a-option v-for="c in credentials" :key="c.id" :value="c.id">
                <a-space :size="6">
                  <a-tag size="small" :color="c.auth === 'key' ? 'purple' : 'arcoblue'">
                    {{ c.auth === 'key' ? '密钥' : '密码' }}
                  </a-tag>
                  <span>{{ c.name }}</span>
                  <span v-if="c.user" class="cred-opt-user">{{ c.user }}</span>
                </a-space>
              </a-option>
            </a-select>
            <a-button size="small" @click="credVisible = true">
              <template #icon><icon-settings /></template>管理
            </a-button>
          </div>
          <template #extra>选不到就留空，下面手动填账号与密码 / 私钥。</template>
        </a-form-item>

        <!-- 选了共享凭据：账号与登录方式取自凭据，这里只作展示（一处改密 FTP 与终端同时生效） -->
        <div v-if="form.credID" class="cf-picked">
          <icon-check-circle-fill class="cf-picked-ic" />
          <span class="cf-picked-text">{{ credSummary }}</span>
          <a-link size="mini" class="cf-picked-undo" @click="clearCred">改为手动填写</a-link>
        </div>

        <!-- 未选凭据：保留旧式内联填写（兼容路径） -->
        <template v-else>
          <a-row :gutter="8">
            <a-col :span="12">
              <a-form-item label="账号" required>
                <a-input v-model="form.user" placeholder="root" allow-clear />
              </a-form-item>
            </a-col>
            <a-col :span="12">
              <a-form-item label="验证方式">
                <a-radio-group v-model="form.auth" type="button" size="medium">
                  <a-radio value="password">密码验证</a-radio>
                  <a-radio value="key">密钥验证</a-radio>
                </a-radio-group>
              </a-form-item>
            </a-col>
          </a-row>

          <a-form-item v-if="form.auth === 'password'" label="密码" required>
            <a-input-password v-model="form.password" :placeholder="form.id ? '留空表示沿用已保存的密码' : '请输入登录密码'"
              allow-clear />
          </a-form-item>

          <a-form-item v-else label="私钥文件" required>
            <div class="cf-key-row">
              <a-input v-model="form.keyPath" placeholder="请选择 OpenSSH 私钥文件" readonly class="cf-key-input">
                <template #prefix><icon-file /></template>
              </a-input>
              <a-button size="small" @click="chooseKey">选择</a-button>
              <a-button size="small" @click="openSshGen">生成密钥</a-button>
            </div>
          </a-form-item>
        </template>
      </div>

      <div class="cf-group">
        <div class="cf-group-head">
          <span class="cf-group-title">打开后</span>
          <span class="cf-group-dim">都可留空</span>
        </div>
        <a-row :gutter="8">
          <a-col :span="15">
            <a-form-item label="初始目录">
              <a-input v-model="form.remoteDir" placeholder="如 /var/www，留空进家目录" allow-clear>
                <template #prefix><icon-folder /></template>
              </a-input>
            </a-form-item>
          </a-col>
          <a-col :span="9">
            <a-form-item label="备注">
              <a-input v-model="form.note" placeholder="如：测试服务器" allow-clear />
            </a-form-item>
          </a-col>
        </a-row>
      </div>
    </a-form>
    <template #footer>
      <div class="conn-test-footer">
        <div class="conn-test-left">
          <a-button size="small" :loading="testing" @click="doTest">测试连接</a-button>
          <span v-if="testResult" :class="['conn-test-msg', testStatus === 'ok' ? 'is-ok' : 'is-err']">
            <icon-check-circle-fill v-if="testStatus === 'ok'" />
            <icon-exclamation-circle-fill v-else />{{ testResult }}
          </span>
        </div>
        <a-space :size="8">
          <a-button @click="close">取消</a-button>
          <a-button type="primary" :loading="busy" @click="onOk">保存并连接</a-button>
        </a-space>
      </div>
    </template>
  </a-modal>

  <ssh-gen-modal v-model:visible="sshGenVisible" :context="sshGenCtx" @use-key="onUseKey" />
  <!-- 凭据管理直接复用终端页的凭据弹窗：同一份 credentials.json，不写第二套 -->
  <credential-modal v-model:visible="credVisible" @changed="credentials = $event" />
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, reactive, ref, watch } from 'vue';
import { Message } from '@arco-design/web-vue';
import type { FtpConnection } from '../types';
import type { TermCredential } from '../../term/types';
import CredentialModal from '../../term/components/CredentialModal.vue';

const props = defineProps<{ visible: boolean; connection: FtpConnection | null }>();
const emit = defineEmits<{
  (e: 'update:visible', v: boolean): void;
  (e: 'saved', list: FtpConnection[], id: string): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const busy = ref(false);
const credentials = ref<TermCredential[]>([]);
const credVisible = ref(false);

const credSummary = computed(() => {
  const c = credentials.value.find((x) => x.id === form.credID);
  if (!c) return '所选凭据（保存后生效）';
  return c.auth === 'key' ? `密钥登录 · ${c.user || '账号沿用连接'} · ${c.name}` : `密码登录 · ${c.user || '账号沿用连接'} · ${c.name}`;
});

async function loadCreds() {
  try {
    credentials.value = (await GoTools?.TermListCredentials()) || [];
  } catch {
    credentials.value = [];
  }
}

/** 选/清凭据：选了就不需要内联账号密码；清空则回到旧式内联 */
function onCredChange() {
  if (form.credID) {
    form.password = '';
    form.keyPath = '';
  }
}

/** 展示条上的「改为手动填写」：退回内联分支（选凭据时已清过，这里只摘关联） */
function clearCred() {
  form.credID = '';
}

/** 密码不回填（记录里本来就没有明文），编辑时留空即沿用原密码 */
function emptyForm(): FtpConnection {
  return {
    id: '',
    name: '',
    proto: 'sftp',
    host: '',
    port: 22,
    user: 'root',
    auth: 'password',
    password: '',
    keyPath: '',
    credID: '',
    remoteDir: '',
    note: '',
  };
}

const form = reactive<FtpConnection>(emptyForm());

watch(
  () => props.visible,
  (v) => {
    if (!v) return;
    loadCreds();
    Object.assign(form, emptyForm(), props.connection ? { ...props.connection, password: '' } : {});
    busy.value = false;
    resetTest();
  },
  { immediate: true },
);

async function chooseKey() {
  try {
    const path = await GoTools?.FtpChooseKeyFile();
    if (path) form.keyPath = path;
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '选择文件失败'), position: 'top', duration: 2000 });
  }
}

/* SSH 生成助手：把当前连接的主机上下文传进去，生成密钥后回填 keyPath */
const sshGenVisible = ref(false);
const sshGenCtx = computed(() => ({
  address: form.host, port: form.port || 22, user: form.user, keyPath: form.keyPath,
}));
function openSshGen() {
  sshGenVisible.value = true;
}
function onUseKey(payload: { keyPath: string; pubKey: string }) {
  if (payload?.keyPath) form.keyPath = payload.keyPath;
}

function validate(): string {
  if (!form.host.trim()) return '请填写主机 IP 或域名';
  if (!form.port || form.port < 1 || form.port > 65535) return '端口需在 1-65535 之间';
  if (form.credID) return ''; // 登录方式取自共享凭据
  if (!form.user.trim()) return '请填写登录账号';
  if (form.auth === 'password') {
    if (!form.password && !form.id) return '请输入登录密码';
  } else if (!form.keyPath) {
    return '请选择私钥文件';
  }
  return '';
}

async function onBeforeOk(): Promise<boolean> {
  const err = validate();
  if (err) {
    Message.error({ content: err, position: 'top', duration: 2000 });
    return false;
  }
  busy.value = true;
  try {
    const list: FtpConnection[] = await GoTools.FtpSaveConnection({ ...form });
    // 新建时后端把记录插在列表最前，保存后的 id 以返回列表为准
    const id = form.id || list[0]?.id || '';
    emit('saved', list, id);
    return true;
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '保存失败'), position: 'top', duration: 3000 });
    return false;
  } finally {
    busy.value = false;
  }
}

function close() {
  emit('update:visible', false);
}

/* 底栏“测试连接”：只完成 SSH 认证握手不开 sftp 子系统；编辑时密码留空由后端沿用已存密码 */
const testing = ref(false);
const testResult = ref('');
const testStatus = ref<'ok' | 'err' | ''>('');
function resetTest() {
  testing.value = false;
  testResult.value = '';
  testStatus.value = '';
}
async function doTest() {
  const err = validate();
  if (err) {
    Message.error({ content: err, position: 'top', duration: 2000 });
    return;
  }
  testing.value = true;
  testResult.value = '';
  testStatus.value = '';
  try {
    const ms = await GoTools.FtpTestConnection({ ...form });
    testStatus.value = 'ok';
    testResult.value = `连接成功（${ms} ms）`;
  } catch (e: any) {
    testStatus.value = 'err';
    testResult.value = '连接失败：' + String(e?.message || e);
  } finally {
    testing.value = false;
  }
}

/** 自定义底栏的“保存并连接”：走原 onBeforeOk 逻辑，成功后关窗 */
async function onOk() {
  if (await onBeforeOk()) emit('update:visible', false);
}
</script>

<style scoped>
/* 分组：小标题走 php 扩展管理同一度量（13px/600 + 右侧说明），组间用发丝线分段 */
.cf-group {
  display: flex;
  flex-direction: column;
  gap: 10px; /* 字段间距统一由组容器管，不靠表单项自己的 margin */
}

.cf-group + .cf-group {
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--color-border-1);
}

.cf-group-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.cf-group-title {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
  font-size: 13px;
  color: var(--color-text-1);
}

.cf-group-tail {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.cf-group-dim {
  color: var(--color-text-3);
  font-size: var(--base-size-small);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 纵向布局下字段本身不留底边，间距全交给上面的组内 gap，免得 a-col 里那一项多撑一段 */
.cf-form :deep(.arco-form-item) {
  margin-bottom: 0;
}

/* Arco 给纵向表单项的标签留了 8px，这里压到 4px，让标签贴住自己的输入框 */
.cf-form :deep(.arco-form-item-label-col) {
  margin-bottom: 4px;
}

/* 已选凭据的回执条：主色淡底 + 图标，比原来一行灰色文字更像“已生效” */
.cf-picked {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 10px;
  border-radius: var(--base-radius-small);
  background: var(--color-primary-light-1);
  border: 1px solid rgba(var(--primary-6), 0.2);
}

.cf-picked-ic {
  flex: none;
  color: rgb(var(--green-6));
}

.cf-picked-text {
  flex: 1;
  min-width: 0;
  color: var(--color-text-1);
  font-size: var(--base-size-small);
}

.cf-picked-undo {
  flex: none;
}

.cred-row,
.cf-key-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
}

/* Arco 的 Select 根节点挂的是 arco-select-view（没有 arco-select 这个类），与终端页同一选择器 */
.cred-row :deep(.arco-select-view) {
  flex: 1;
  min-width: 0;
}

/* 数字框默认不撑满，放进 a-col 会缩成一小截，这里统一拉齐 */
.cf-form :deep(.arco-input-number) {
  width: 100%;
}

.cf-key-input {
  flex: 1;
  min-width: 0;
}

.cred-opt-user {
  color: var(--color-text-3);
  font-size: var(--base-size-small);
}

/* 测试结果加图标：绿勾/红叹号比纯色文字更容易一眼看出成没成 */
.conn-test-msg .arco-icon {
  margin-right: 4px;
  vertical-align: -2px;
}
</style>
