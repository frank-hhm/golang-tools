<template>
  <a-modal :visible="visible" title="SSH 凭据管理" :width="620" :mask-closable="false" :footer="false"
    @cancel="close" @update:visible="(v: boolean) => emit('update:visible', v)">
    <template #title>
      <a-space>
        <span>SSH 凭据管理</span>
        <a-tag size="small" color="gray">一处改密，多机生效</a-tag>
      </a-space>
    </template>

    <div class="cred-body">
      <!-- 左：凭据列表 -->
      <div class="cred-list">
        <div class="cred-list-head">
          <a-button size="mini" type="primary" @click="newOne">
            <template #icon><icon-plus /></template>新建
          </a-button>
        </div>
        <a-scrollbar style="height: 300px">
          <a-list :bordered="false" size="small">
            <a-list-item v-for="c in items" :key="c.id" :class="{ 'cred-active': c.id === form.id }"
              @click="pick(c)">
              <a-space>
                <a-tag size="small" :color="c.auth === 'key' ? 'purple' : 'arcoblue'">
                  {{ c.auth === 'key' ? '密钥' : '密码' }}
                </a-tag>
                <span class="cred-name">{{ c.name }}</span>
                <a-typography-text v-if="c.user" type="secondary" class="cred-user">{{ c.user }}</a-typography-text>
              </a-space>
              <template #actions>
                <a-dropdown trigger="hover" @select="(v: any) => onAction(v, c)">
                  <icon-more class="cred-more" @click.stop />
                  <template #content>
                    <a-doption value="edit"><icon-edit /> 编辑</a-doption>
                    <a-doption value="del"><icon-delete /> 删除</a-doption>
                  </template>
                </a-dropdown>
              </template>
            </a-list-item>
            <a-list-item v-if="!items.length && !loading">
              <div class="cred-empty">暂无凭据，点「新建」添加</div>
            </a-list-item>
          </a-list>
        </a-scrollbar>
      </div>

      <!-- 右：编辑表单 -->
      <div class="cred-form">
        <a-form :model="form" layout="vertical" size="small">
          <a-form-item label="名称" required>
            <a-input v-model="form.name" placeholder="如：生产部署账号" allow-clear />
          </a-form-item>
          <a-form-item label="验证方式">
            <a-radio-group v-model="form.auth" type="button" size="small">
              <a-radio value="password">密码</a-radio>
              <a-radio value="key">密钥</a-radio>
            </a-radio-group>
          </a-form-item>
          <a-form-item label="SSH 账号">
            <a-input v-model="form.user" placeholder="登录账号（主机未填账号时沿用此项）" allow-clear />
          </a-form-item>

          <template v-if="form.auth === 'password'">
            <a-form-item label="密码" :required="!form.id">
              <a-input-password v-model="form.password"
                :placeholder="form.id ? '留空表示沿用已保存的密码' : '请输入密码'" allow-clear />
            </a-form-item>
          </template>
          <template v-else>
            <a-form-item label="私钥文件" required>
              <a-space :size="8" class="key-row">
                <a-input v-model="form.keyPath" placeholder="OpenSSH 私钥文件" readonly class="key-input">
                  <template #prefix><icon-file /></template>
                </a-input>
                <a-button size="small" @click="chooseKey">选择</a-button>
              </a-space>
            </a-form-item>
            <a-form-item label="私钥口令">
              <a-input-password v-model="form.passphrase"
                :placeholder="form.id ? '留空表示沿用已保存的口令' : '无口令可留空'" allow-clear />
            </a-form-item>
          </template>

          <div class="cred-form-foot">
            <a-space>
              <a-button size="small" @click="reset">重填</a-button>
              <a-button size="small" type="primary" :loading="busy" @click="save">保存凭据</a-button>
            </a-space>
          </div>
        </a-form>
      </div>
    </div>
  </a-modal>
</template>

<script setup lang="ts">
import { getCurrentInstance, reactive, ref, watch } from 'vue';
import { Message, Modal } from '@arco-design/web-vue';
import type { TermCredential } from '../types';

const props = defineProps<{ visible: boolean }>();
const emit = defineEmits<{ (e: 'update:visible', v: boolean): void; (e: 'changed', list: TermCredential[]): void }>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const items = ref<TermCredential[]>([]);
const loading = ref(false);
const busy = ref(false);

function emptyForm(): TermCredential {
  return { id: '', name: '', auth: 'password', user: '', password: '', keyPath: '', passphrase: '' };
}
const form = reactive<TermCredential>(emptyForm());

async function load() {
  loading.value = true;
  try {
    items.value = (await GoTools?.TermListCredentials()) || [];
    emit('changed', items.value);
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '读取凭据失败'), position: 'top', duration: 3000 });
  } finally {
    loading.value = false;
  }
}

watch(() => props.visible, (v) => { if (v) { reset(); load(); } });

function newOne() {
  reset();
}

function pick(c: TermCredential) {
  Object.assign(form, { ...c, password: '', passphrase: '' });
}

function reset() {
  Object.assign(form, emptyForm());
}

function onAction(v: string, c: TermCredential) {
  if (v === 'edit') pick(c);
  else if (v === 'del') confirmRemove(c);
}

function confirmRemove(c: TermCredential) {
  Modal.warning({
    title: '删除凭据',
    content: `确定删除凭据「${c.name}」？引用它的主机将失去登录方式（需重新关联凭据）。`,
    hideCancel: false,
    onOk: async () => {
      try {
        items.value = (await GoTools?.TermDeleteCredential(c.id)) || [];
        if (form.id === c.id) reset();
        emit('changed', items.value);
        Message.success('已删除');
      } catch (e: any) {
        Message.error({ content: String(e?.message || e || '删除失败'), position: 'top', duration: 3000 });
      }
    },
  });
}

async function chooseKey() {
  try {
    const path = await GoTools?.TermChooseKeyFile();
    if (path) form.keyPath = path;
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '选择文件失败'), position: 'top', duration: 2000 });
  }
}

async function save() {
  if (!form.name.trim()) {
    Message.error('请填写凭据名称');
    return;
  }
  if (form.auth === 'key' && !form.keyPath) {
    Message.error('请选择私钥文件');
    return;
  }
  if (form.auth === 'password' && !form.id && !form.password) {
    Message.error('请输入密码');
    return;
  }
  busy.value = true;
  try {
    items.value = (await GoTools.TermSaveCredential({ ...form })) || [];
    emit('changed', items.value);
    // 保存后停留在该凭据（新建则选中新返回的第一条）
    const id = form.id || items.value[0]?.id || '';
    const found = items.value.find((x) => x.id === id);
    if (found) pick(found);
    Message.success('已保存');
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '保存失败'), position: 'top', duration: 3000 });
  } finally {
    busy.value = false;
  }
}

function close() {
  emit('update:visible', false);
}
</script>

<style scoped>
.cred-body {
  display: flex;
  gap: 12px;
}

.cred-list {
  width: 260px;
  border-right: 1px solid var(--color-border-2);
  padding-right: 8px;
}

.cred-list-head {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 6px;
}

.cred-form {
  flex: 1;
  min-width: 0;
}

.cred-name {
  max-width: 110px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cred-user {
  font-size: var(--base-size-small);
}

.cred-more {
  cursor: pointer;
  color: var(--color-text-3);
}

.cred-empty {
  width: 100%;
  text-align: center;
  color: var(--color-text-3);
  font-size: var(--base-size-small);
  padding: 10px 0;
}

.cred-active {
  background: var(--color-primary-light-1);
}

:deep(.arco-list-item) {
  cursor: pointer;
}

.key-row {
  width: 100%;
}

.key-input {
  flex: 1;
  min-width: 0;
}

.cred-form-foot {
  display: flex;
  justify-content: flex-end;
  margin-top: 8px;
}
</style>
