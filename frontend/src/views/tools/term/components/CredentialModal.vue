<template>
  <a-modal :visible="visible" title="SSH 凭据管理" :width="640" :mask-closable="false" @cancel="close"
    @update:visible="(v: boolean) => emit('update:visible', v)">
    <div class="cred-body">
      <!-- 左：凭据列表（无题卡壳 + 圆角选中态，与任务中心弹窗同一版式） -->
      <div class="cred-list">
        <div class="cred-list-head">
          <span class="cred-list-title">共享凭据</span>
          <a-button size="mini" type="primary" @click="newOne">
            <template #icon><icon-plus /></template>新建
          </a-button>
        </div>
        <a-scrollbar style="height: 300px">
          <div class="cred-items">
            <div v-for="c in items" :key="c.id" class="cred-item" :class="{ 'cred-active': c.id === form.id }"
              @click="pick(c)">
              <icon-safe v-if="c.auth === 'key'" class="cred-item-ic" />
              <icon-lock v-else class="cred-item-ic" />
              <span class="cred-name">{{ c.name }}</span>
              <a-typography-text v-if="c.user" type="secondary" class="cred-user">{{ c.user }}</a-typography-text>
              <a-dropdown trigger="hover" @select="(v: any) => onAction(v, c)">
                <icon-more class="cred-more" @click.stop />
                <template #content>
                  <a-doption value="edit"><icon-edit /> 编辑</a-doption>
                  <a-doption value="del" style="color: rgb(var(--red-6))"><icon-delete /> 删除</a-doption>
                </template>
              </a-dropdown>
            </div>
            <div v-if="!items.length && !loading" class="cred-empty">暂无凭据，点「新建」添加</div>
          </div>
        </a-scrollbar>
      </div>

      <!-- 右：编辑表单：短字段两列并排，压缩纵向空白 -->
      <div class="cred-form">
        <a-form :model="form" layout="vertical" size="small" class="cred-vform">
          <a-row :gutter="8">
            <a-col :span="13">
              <a-form-item label="名称" required>
                <a-input v-model="form.name" placeholder="如：生产部署账号" allow-clear />
              </a-form-item>
            </a-col>
            <a-col :span="11">
              <a-form-item label="验证方式">
                <a-radio-group v-model="form.auth" type="button" size="small">
                  <a-radio value="password">密码</a-radio>
                  <a-radio value="key">密钥</a-radio>
                </a-radio-group>
              </a-form-item>
            </a-col>
          </a-row>
          <a-form-item label="SSH 账号">
            <a-input v-model="form.user" placeholder="登录账号（主机未填账号时沿用此项）" allow-clear />
          </a-form-item>

          <a-form-item v-if="form.auth === 'password'" label="密码" :required="!form.id">
            <a-input-password v-model="form.password"
              :placeholder="form.id ? '留空表示沿用已保存的密码' : '请输入密码'" allow-clear />
          </a-form-item>
          <template v-else>
            <a-form-item label="私钥文件" required>
              <div class="key-row">
                <a-input v-model="form.keyPath" placeholder="OpenSSH 私钥文件" readonly class="key-input">
                  <template #prefix><icon-file /></template>
                </a-input>
                <a-button size="small" @click="chooseKey">选择</a-button>
              </div>
            </a-form-item>
            <a-form-item label="私钥口令">
              <a-input-password v-model="form.passphrase"
                :placeholder="form.id ? '留空表示沿用已保存的口令' : '无口令可留空'" allow-clear />
            </a-form-item>
          </template>
        </a-form>
      </div>
    </div>

    <!-- 动作进 footer：左说明右按钮，与仓库设置弹窗同一交互；表单区不再自带按钮 -->
    <template #footer>
      <div class="cred-footer">
        <span class="cred-footer-hint">凭据与终端、FTP 共用一份，改一次多机生效</span>
        <a-space :size="8">
          <a-button size="small" @click="reset">重填</a-button>
          <a-button size="small" @click="close">关闭</a-button>
          <a-button size="small" type="primary" :loading="busy" @click="save">保存凭据</a-button>
        </a-space>
      </div>
    </template>
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

/* 左列：胶囊菜单项，与设置页左侧分类导航（.cat-item）同口径：
   选中中性轻底 fill-2 + 加粗 + 左侧主色短竖条，hover fill-1，主色只走竖条不铺底 */
.cred-list {
  width: 252px;
  flex: none;
  border-right: 1px solid var(--color-border-2);
  padding-right: 10px;
}

.cred-list-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}

.cred-list-title {
  font-weight: 600;
  font-size: 13px;
  color: var(--color-text-1);
}

.cred-items {
  display: flex;
  flex-direction: column;
  gap: 4px; /* 纵向间距由 flex gap 统一给，不用项 margin（相邻会折叠） */
  padding-right: 4px; /* 给滚动条留位，免得 hover 背景被裁 */
}

.cred-item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  padding: 8px 8px 8px 12px;
  border-radius: var(--base-radius-default);
  color: var(--color-text-2);
  cursor: pointer;
  transition: background-color 0.15s, color 0.15s;
}

/* 选中态的左侧短竖条：3px、高 55%，常态透明只留过渡 */
.cred-item::before {
  content: "";
  position: absolute;
  left: 0;
  top: 50%;
  transform: translateY(-50%);
  width: 3px;
  height: 55%;
  border-radius: 0 3px 3px 0;
  background: transparent;
  transition: background-color 0.15s;
}

.cred-item:hover {
  background: var(--color-fill-1);
  color: var(--color-text-1);
}

.cred-active,
.cred-active:hover {
  background: var(--color-fill-2);
  color: var(--color-text-1);
}

.cred-active::before {
  background: var(--app-select-rail);
}

.cred-item-ic {
  flex: none;
  color: inherit;
  font-size: var(--base-size-default);
}

.cred-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cred-active .cred-name {
  font-weight: 600;
}

.cred-user {
  flex: none;
  max-width: 72px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--base-size-small);
}

.cred-more {
  flex: none;
  cursor: pointer;
  color: var(--color-text-3);
}

.cred-more:hover {
  color: var(--color-text-1);
}

.cred-empty {
  width: 100%;
  text-align: center;
  color: var(--color-text-3);
  font-size: var(--base-size-small);
  padding: 24px 0;
}

.cred-form {
  flex: 1;
  min-width: 0;
}

/* 纵向表单：字段间距由容器 gap 统一管，标签贴住输入框（与 FTP 连接表单同度量） */
.cred-vform {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.cred-vform :deep(.arco-form-item) {
  margin-bottom: 0;
}

.cred-vform :deep(.arco-form-item-label-col) {
  margin-bottom: 4px;
}

.key-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
}

.key-input {
  flex: 1;
  min-width: 0;
}

/* footer：左说明右按钮 */
.cred-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.cred-footer-hint {
  color: var(--color-text-3);
  font-size: var(--base-size-small);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
