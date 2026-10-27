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
        </a-space>
      </a-form-item>

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
</template>

<script setup lang="ts">
import { getCurrentInstance, reactive, ref, watch } from 'vue';
import { Message } from '@arco-design/web-vue';
import type { FtpConnection } from '../types';

const props = defineProps<{ visible: boolean; connection: FtpConnection | null }>();
const emit = defineEmits<{
  (e: 'update:visible', v: boolean): void;
  (e: 'saved', list: FtpConnection[], id: string): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const busy = ref(false);

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
    remoteDir: '',
    note: '',
  };
}

const form = reactive<FtpConnection>(emptyForm());

watch(
  () => props.visible,
  (v) => {
    if (!v) return;
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

function validate(): string {
  if (!form.host.trim()) return '请填写主机 IP 或域名';
  if (!form.port || form.port < 1 || form.port > 65535) return '端口需在 1-65535 之间';
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
</style>
