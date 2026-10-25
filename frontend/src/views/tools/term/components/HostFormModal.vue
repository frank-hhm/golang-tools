<template>
  <a-modal :visible="visible" :title="form.id ? '编辑服务器' : '添加服务器'" :width="520" :mask-closable="false"
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
        </a-space>
      </a-form-item>

      <a-form-item label="备注">
        <a-input v-model="form.note" placeholder="如：测试服务器" allow-clear />
      </a-form-item>
    </a-form>
  </a-modal>
</template>

<script setup lang="ts">
import { getCurrentInstance, reactive, ref, watch } from 'vue';
import { Message } from '@arco-design/web-vue';
import type { TermHost } from '../types';

const props = defineProps<{ visible: boolean; host: TermHost | null }>();
const emit = defineEmits<{
  (e: 'update:visible', v: boolean): void;
  (e: 'saved', list: TermHost[], id: string): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const busy = ref(false);

/** 表单内容：密码不回填（记录里本来就没有明文），编辑时留空即沿用原密码 */
function emptyForm(): TermHost {
  return { id: '', name: '', system: 'linux', address: '', port: 22, user: 'root', auth: 'password', password: '', keyPath: '', note: '' };
}

const form = reactive<TermHost>(emptyForm());

watch(
  () => props.visible,
  (v) => {
    if (!v) return;
    const h = props.host;
    Object.assign(form, emptyForm(), h ? { ...h, password: '' } : {});
    busy.value = false;
  },
  { immediate: true },
);

/** 系统切换只跟随默认端口（22 之外的端口说明用户自己改过，别覆盖） */
function onSystemChange() {
  if (!form.port || form.port === 22 || form.port === 3389) form.port = 22;
}

async function chooseKey() {
  try {
    const path = await GoTools?.TermChooseKeyFile();
    if (path) form.keyPath = path;
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '选择文件失败'), position: 'top', duration: 2000 });
  }
}

function validate(): string {
  if (!form.address.trim()) return '请填写服务器 IP 或域名';
  if (!form.port || form.port < 1 || form.port > 65535) return '端口需在 1-65535 之间';
  if (!form.user.trim()) return '请填写 SSH 账号';
  if (form.auth === 'password') {
    // 编辑时留空沿用旧密码；新建必须填
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
</script>

<style scoped>
/* IP 与端口同排：IP 占满剩余宽度，端口固定窄列 */
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
