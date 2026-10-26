<template>
  <a-modal :visible="visible" :title="form.id ? '编辑账号' : '添加账号'" :width="540" :mask-closable="false"
    ok-text="保存" cancel-text="取消" :ok-loading="busy" :on-before-ok="onBeforeOk"
    @cancel="close" @update:visible="(v: boolean) => emit('update:visible', v)">
    <a-form :model="form" layout="horizontal" :label-col-props="{ span: 5 }" :wrapper-col-props="{ span: 19 }">
      <a-form-item label="用户名" required>
        <a-input v-model="form.username" placeholder="如 alice（不能用空格/斜杠/冒号）" allow-clear />
      </a-form-item>

      <a-form-item label="密码" :required="!form.id">
        <a-input-password v-model="form.password" :placeholder="form.id ? '留空表示沿用原密码' : '至少 6 位'"
          allow-clear />
      </a-form-item>

      <a-form-item label="根目录">
        <a-input v-model="form.rootDir" placeholder="留空默认使用用户名；填 . 表示服务端根目录" allow-clear>
          <template #prefix><icon-folder /></template>
        </a-input>
      </a-form-item>

      <a-form-item label="权限">
        <a-radio-group v-model="form.readOnly" type="button">
          <a-radio :value="false">读写</a-radio>
          <a-radio :value="true">只读</a-radio>
        </a-radio-group>
      </a-form-item>

      <a-form-item label="启用">
        <a-space :size="8">
          <a-switch v-model="form.enabled" size="small" />
          <a-typography-text type="secondary">禁用会立即断开该账号的在线连接</a-typography-text>
        </a-space>
      </a-form-item>

      <a-form-item label="备注">
        <a-input v-model="form.note" placeholder="如：给同事传文件用" allow-clear />
      </a-form-item>
    </a-form>

    <a-alert type="info" :show-icon="true">
      账号只能访问服务端根目录下的子目录：路径里的 .. 与软链都会被拦在沙箱内，
      该目录在保存时会自动创建。
    </a-alert>
  </a-modal>
</template>

<script setup lang="ts">
import { getCurrentInstance, reactive, ref, watch } from 'vue';
import { Message } from '@arco-design/web-vue';
import type { FtpAccount } from '../types';

const props = defineProps<{ visible: boolean; account: FtpAccount | null }>();
const emit = defineEmits<{
  (e: 'update:visible', v: boolean): void;
  (e: 'saved', list: FtpAccount[]): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const busy = ref(false);

/** 密码不回填（列表里本来就没有明文），编辑时留空即沿用原密码 */
function emptyForm(): FtpAccount {
  return { id: '', username: '', password: '', rootDir: '', readOnly: false, enabled: true, note: '' };
}

const form = reactive<FtpAccount>(emptyForm());

watch(
  () => props.visible,
  (v) => {
    if (!v) return;
    const src = props.account;
    Object.assign(form, emptyForm(), src ? { ...src, password: '' } : {});
    busy.value = false;
  },
  { immediate: true },
);

function validate(): string {
  if (!form.username.trim()) return '请填写用户名';
  if (!form.id && !form.password) return '请设置登录密码';
  if (form.password && form.password.length < 6) return '密码至少 6 位';
  if (!form.id && !form.enabled) return '新账号请保持启用（禁用后无法登录）';
  return '';
}

async function onBeforeOk(): Promise<boolean> {
  const err = validate();
  if (err) {
    Message.error({ content: err, position: 'top', duration: 2500 });
    return false;
  }
  busy.value = true;
  try {
    const list: FtpAccount[] = await GoTools.FtpSaveAccount({ ...form });
    emit('saved', list);
    return true;
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '保存账号失败'), position: 'top', duration: 3000 });
    return false;
  } finally {
    busy.value = false;
  }
}

function close() {
  emit('update:visible', false);
}
</script>
