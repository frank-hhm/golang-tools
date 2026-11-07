<template>
  <a-modal :visible="visible" :title="form.id ? '编辑连接' : '添加连接'" :width="520" :mask-closable="false"
    ok-text="保存并连接" cancel-text="取消" :ok-loading="busy" :on-before-ok="onBeforeOk"
    @cancel="close" @update:visible="(v: boolean) => emit('update:visible', v)">
    <a-form :model="form" layout="horizontal" :label-col-props="{ span: 5 }" :wrapper-col-props="{ span: 19 }">
      <a-form-item label="协议">
        <a-space :size="8">
          <a-tag color="arcoblue">SFTP</a-tag>
          <a-typography-text type="secondary">FTP / FTPS 在后续版本提供</a-typography-text>
        </a-space>
      </a-form-item>

      <a-form-item label="主机" required>
        <div class="ip-row">
          <a-input v-model="form.host" class="ip-input" placeholder="IP 或域名" allow-clear />
          <a-typography-text type="secondary">端口</a-typography-text>
          <a-input-number v-model="form.port" class="port-input" :min="1" :max="65535" />
        </div>
      </a-form-item>

      <a-form-item label="凭据">
        <div class="cred-row">
          <a-select v-model="form.credID" placeholder="选择已保存的凭据（推荐，与终端共享）" allow-clear allow-search
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
          <a-button size="small" @click="credVisible = true">管理</a-button>
        </div>
      </a-form-item>

      <!-- 选了共享凭据：账号/登录方式取自凭据，这里只作展示（一处改密 FTP 与终端同时生效） -->
      <a-form-item v-if="form.credID" label="登录">
        <a-typography-text type="secondary">{{ credSummary }}</a-typography-text>
      </a-form-item>

      <!-- 未选凭据：保留旧式内联填写（兼容路径） -->
      <template v-else>
        <a-form-item label="账号" required>
          <a-input v-model="form.user" placeholder="root" allow-clear />
        </a-form-item>

        <a-form-item label="验证方式">
          <a-radio-group v-model="form.auth" type="button">
            <a-radio value="password">密码验证</a-radio>
            <a-radio value="key">密钥验证</a-radio>
          </a-radio-group>
        </a-form-item>

        <a-form-item v-if="form.auth === 'password'" label="密码" required>
          <a-input-password v-model="form.password" :placeholder="form.id ? '留空表示沿用已保存的密码' : '请输入登录密码'"
            allow-clear />
        </a-form-item>

        <a-form-item v-else label="私钥文件" required>
          <a-space :size="8">
            <a-input v-model="form.keyPath" placeholder="请选择 OpenSSH 私钥文件" readonly class="key-input">
              <template #prefix><icon-file /></template>
            </a-input>
            <a-button size="small" @click="chooseKey">选择</a-button>
            <a-button size="small" @click="openSshGen">生成密钥</a-button>
          </a-space>
        </a-form-item>
      </template>

      <a-form-item label="初始目录">
        <a-input v-model="form.remoteDir" placeholder="留空则进入登录后的家目录，如 /var/www" allow-clear />
      </a-form-item>

      <a-form-item label="备注">
        <a-input v-model="form.note" placeholder="如：测试服务器" allow-clear />
      </a-form-item>
    </a-form>
    <template #footer>
      <div class="conn-test-footer">
        <div class="conn-test-left">
          <a-button size="small" :loading="testing" @click="doTest">测试连接</a-button>
          <span v-if="testResult" :class="['conn-test-msg', testStatus === 'ok' ? 'is-ok' : 'is-err']">{{ testResult }}</span>
        </div>
        <a-space>
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
.ip-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.ip-input {
  flex: 1;
}

.port-input {
  width: 96px;
}

.key-input {
  width: 300px;
}

.cred-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
}

.cred-row .arco-select-view {
  flex: 1;
}

.cred-opt-user {
  color: var(--color-text-3);
  font-size: var(--base-size-small);
}
</style>
