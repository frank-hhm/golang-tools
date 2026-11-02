<template>
  <a-modal :visible="visible" :title="form.id ? '编辑服务器' : '添加服务器'" :width="560" :mask-closable="false"
    ok-text="保存并连接" cancel-text="取消" :ok-loading="busy" :on-before-ok="onBeforeOk"
    @cancel="close" @update:visible="(v: boolean) => emit('update:visible', v)">
    <a-form :model="form" layout="horizontal" :label-col-props="{ span: 5 }" :wrapper-col-props="{ span: 19 }">
      <a-form-item label="系统">
        <a-radio-group v-model="form.system" type="button" size="medium" @change="onSystemChange">
          <a-radio value="linux">Linux系统</a-radio>
          <a-radio value="windows">Windows系统</a-radio>
        </a-radio-group>
      </a-form-item>

      <a-form-item label="服务器IP" required>
        <div class="ip-row">
          <a-input v-model="form.address" class="ip-input" placeholder="请输入服务器IP地址" allow-clear />
          <a-typography-text type="secondary">端口</a-typography-text>
          <a-input-number v-model="form.port" class="port-input" :min="1" :max="65535" />
        </div>
      </a-form-item>

      <a-form-item label="凭据">
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
          <a-button size="small" @click="credVisible = true">管理</a-button>
        </div>
      </a-form-item>

      <!-- 选了解锁主机：登录方式取自凭据，这里只作展示 -->
      <a-form-item v-if="form.credID" label="登录">
        <a-typography-text type="secondary">{{ credSummary }}</a-typography-text>
      </a-form-item>

      <!-- 未选凭据：保留旧式内联填写（兼容路径） -->
      <template v-else>
        <a-form-item label="SSH账号" required>
          <a-input v-model="form.user" placeholder="root" allow-clear />
        </a-form-item>
        <a-form-item label="验证方式">
          <a-radio-group v-model="form.auth" type="button" size="medium">
            <a-radio value="password">密码验证</a-radio>
            <a-radio value="key">密钥验证</a-radio>
          </a-radio-group>
        </a-form-item>
        <a-form-item v-if="form.auth === 'password'" label="密码" required>
          <a-input-password v-model="form.password" :placeholder="form.id ? '留空表示沿用已保存的密码' : '请输入SSH密码，必填'"
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

      <a-form-item label="分组">
        <a-input v-model="form.group" placeholder="如：生产 / 测试（左栏按分组导航）" allow-clear />
      </a-form-item>
      <a-form-item label="标签">
        <a-input-tag v-model="form.tags" placeholder="回车添加，可多个" allow-clear :token-separators="[',', '，']" />
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

  <credential-modal v-model:visible="credVisible" @changed="credentials = $event" />

  <ssh-gen-modal v-model:visible="sshGenVisible" :context="sshGenCtx" @use-key="onUseKey" />
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, reactive, ref, watch } from 'vue';
import { Message } from '@arco-design/web-vue';
import CredentialModal from './CredentialModal.vue';
import type { TermCredential, TermHost } from '../types';

const props = defineProps<{ visible: boolean; host: TermHost | null }>();
const emit = defineEmits<{
  (e: 'update:visible', v: boolean): void;
  (e: 'saved', list: TermHost[], id: string): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const busy = ref(false);
const credentials = ref<TermCredential[]>([]);
const credVisible = ref(false);

/** 表单内容：密码/口令不回填（记录里本来就没有明文），编辑时留空即沿用原值 */
function emptyForm(): TermHost {
  return {
    id: '', name: '', system: 'linux', address: '', port: 22, user: 'root', auth: 'password',
    password: '', keyPath: '', credID: '', group: '', tags: [], note: '',
  };
}

const form = reactive<TermHost>(emptyForm());

const credSummary = computed(() => {
  const c = credentials.value.find((x) => x.id === form.credID);
  if (!c) return '所选凭据（保存后生效）';
  return c.auth === 'key' ? `密钥登录 · ${c.user || '账号沿用主机'} · ${c.name}` : `密码登录 · ${c.user || '账号沿用主机'} · ${c.name}`;
});

async function loadCreds() {
  try {
    credentials.value = (await GoTools?.TermListCredentials()) || [];
  } catch {
    credentials.value = [];
  }
}

watch(
  () => props.visible,
  (v) => {
    if (!v) return;
    loadCreds();
    const h = props.host;
    Object.assign(form, emptyForm(), h ? { ...h, password: '', tags: h.tags ? [...h.tags] : [] } : {});
    if (!form.tags) form.tags = [];
    busy.value = false;
    resetTest();
  },
  { immediate: true },
);

/** 系统切换只跟随默认端口（22 之外的端口说明用户自己改过，别覆盖） */
function onSystemChange() {
  if (!form.port || form.port === 22 || form.port === 3389) form.port = 22;
}

/** 选/清凭据：选了凭据就不需要内联账号密码；清空则回到旧式内联 */
function onCredChange() {
  if (form.credID) {
    form.password = '';
    form.keyPath = '';
  }
}

async function chooseKey() {
  try {
    const path = await GoTools?.TermChooseKeyFile();
    if (path) form.keyPath = path;
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '选择文件失败'), position: 'top', duration: 2000 });
  }
}

/* SSH 生成助手：把当前表单的主机上下文传进去，生成密钥后回填 keyPath */
const sshGenVisible = ref(false);
const sshGenCtx = computed(() => ({
  address: form.address, port: form.port || 22, user: form.user, keyPath: form.keyPath,
}));
function openSshGen() {
  sshGenVisible.value = true;
}
function onUseKey(payload: { keyPath: string; pubKey: string }) {
  if (payload?.keyPath) form.keyPath = payload.keyPath;
}

function validate(): string {
  if (!form.address.trim()) return '请填写服务器 IP 或域名';
  if (!form.port || form.port < 1 || form.port > 65535) return '端口需在 1-65535 之间';
  if (form.credID) return ''; // 登录方式取自凭据
  if (!form.user.trim()) return '请填写 SSH 账号';
  if (form.auth === 'password') {
    if (!form.password && !form.id) return '请输入 SSH 密码';
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
    const list: TermHost[] = await GoTools.TermSaveHost({ ...form });
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

/* 底栏“测试连接”：只完成 SSH 认证握手不建会话；走凭据时后端按 credID 解析 */
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
    const ms = await GoTools.TermTestSSH({ ...form });
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

.cred-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.cred-row :deep(.arco-select) {
  flex: 1;
}

.cred-opt-user {
  color: var(--color-text-3);
  font-size: var(--base-size-small);
}

.key-input {
  width: 300px;
}

.conn-test-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.conn-test-left {
  display: flex;
  align-items: center;
  gap: 10px;
}

.conn-test-msg {
  font-size: var(--base-size-small);
}

.conn-test-msg.is-ok {
  color: rgb(var(--green-6));
}

.conn-test-msg.is-err {
  color: rgb(var(--red-6));
}
</style>
