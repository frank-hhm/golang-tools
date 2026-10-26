<template>
  <!-- 文件传输：客户端（连出去）与服务端（被连进来）共用一页 -->
  <div class="ftp-shell">
    <div class="ftp-mode">
      <a-radio-group v-model="mode" type="button" size="small">
        <a-radio value="client">客户端</a-radio>
        <a-radio value="server">服务端</a-radio>
      </a-radio-group>
      <span class="mode-hint">
        {{ mode === 'client' ? '连接远程 SFTP 服务器，双栏上传下载' : '在本机开 SFTP 服务，给别人建账号传文件' }}
      </span>
    </div>

    <div class="ftp-body">
      <!-- 客户端常驻：切到服务端时不能把已建立的 SFTP 会话卸载掉 -->
      <client-panel v-show="mode === 'client'" />
      <server-panel v-if="serverVisited" v-show="mode === 'server'" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue';
import ClientPanel from './components/ClientPanel.vue';
import ServerPanel from './components/ServerPanel.vue';

const mode = ref<'client' | 'server'>('client');
/** 服务端首次进入才挂载：它的状态/账号/日志要读盘，没必要在只看客户端时跑一遍 */
const serverVisited = ref(false);

watch(mode, (v) => {
  if (v === 'server') serverVisited.value = true;
});
</script>

<style scoped>
.ftp-shell {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.ftp-mode {
  display: flex;
  align-items: center;
  gap: 8px;
  padding-bottom: 6px;
  flex: none;
}

.mode-hint {
  color: var(--color-text-3);
  font-size: 12px;
}

.ftp-body {
  flex: 1;
  min-height: 0;
}
</style>
