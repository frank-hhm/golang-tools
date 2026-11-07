<template>
  <a-modal
    :visible="visible"
    title="局域网分享"
    :width="560"
    :ok-loading="creating"
    :ok-text="srv.running ? '生成链接' : '启动服务并生成'"
    @update:visible="(v: boolean) => emit('update:visible', v)"
    @cancel="close"
    @ok="submit"
  >
    <a-alert :type="srv.running ? 'info' : 'warning'" class="sh-warn">
      <template v-if="srv.running">
        链接对本机局域网内设备开放，<strong>拿到地址的同一网络设备即可下载</strong>；
        服务停着的时候所有链接立刻失效。当前监听：
        <span class="mono">{{ srv.urls.join(" / ") || "（未绑定地址）" }}</span>
      </template>
      <template v-else>
        互传服务未启动。点「启动服务并生成」会在 <span class="mono">{{ DEFAULT_PORT }}</span>
        起一个局域网 HTTP 监听（占用自动 +1），默认 2 小时后自动停止。
        <strong>该端口不经面板登录墙，对局域网开放</strong>。
      </template>
    </a-alert>

    <a-descriptions :column="1" size="small" class="sh-file">
      <template #title>分享文件</template>
      <a-descriptions-item label="名称">{{ item?.name || "--" }}</a-descriptions-item>
      <a-descriptions-item label="路径">
        <span class="mono break">{{ item?.path || "--" }}</span>
      </a-descriptions-item>
    </a-descriptions>

    <a-form :model="form" layout="vertical">
      <a-form-item field="hours" label="有效期">
        <a-radio-group v-model="form.hours" type="button" size="small">
          <a-radio :value="1">1 小时</a-radio>
          <a-radio :value="24">24 小时</a-radio>
          <a-radio :value="168">7 天</a-radio>
          <a-radio :value="0">不过期</a-radio>
        </a-radio-group>
      </a-form-item>
      <a-form-item field="max" label="下载次数上限">
        <a-radio-group v-model="form.max" type="button" size="small">
          <a-radio :value="1">1 次</a-radio>
          <a-radio :value="5">5 次</a-radio>
          <a-radio :value="20">20 次</a-radio>
          <a-radio :value="0">不限</a-radio>
        </a-radio-group>
      </a-form-item>
      <a-form-item field="stop" label="服务自动停止">
        <a-select v-model="form.stopMin" :disabled="srv.running" size="small" style="width: 180px">
          <a-option :value="30">30 分钟后</a-option>
          <a-option :value="120">2 小时后</a-option>
          <a-option :value="0">不自动停</a-option>
        </a-select>
      </a-form-item>
    </a-form>

    <div v-if="result" class="sh-result">
      <div class="sh-result-t">链接已生成（失效条件到即自动拒绝）：</div>
      <a-input-group class="sh-urlgrp">
        <a-input :model-value="result" readonly class="mono" />
        <a-button @click="copy(result)"><template #icon><icon-copy /></template>复制</a-button>
      </a-input-group>
      <div class="sh-tip">目录暂不支持分享（要打包，等 tools/archive 接入）；撤销请到互传页链接列表。</div>
    </div>
  </a-modal>
</template>

<script setup lang="ts">
/**
 * 分享弹窗（文档中心 P3 / 磁盘页 §九 共用）：为 local 条目登记 lanshare 临时链接。
 * 服务没跑就先 Start 再 CreateLink——顺序反过来会得到一条"暂时打不开"的链接。
 * 只登记授权不搬实体：文件停在原位，撤销只删登记。
 */
import { getCurrentInstance, reactive, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { errText, type ShareItem } from "../types";

const DEFAULT_PORT = 36490;

const props = defineProps<{ visible: boolean; item: ShareItem | null }>();
const emit = defineEmits<{ (e: "update:visible", v: boolean): void }>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const srv = reactive({ running: false, port: 0, urls: [] as string[] });
const form = reactive({ hours: 24, max: 0, stopMin: 120 });
const creating = ref(false);
const result = ref("");

async function refreshStatus() {
  try {
    const st = await GoTools?.LanShareStatus();
    srv.running = !!st?.running;
    srv.port = st?.port ?? 0;
    srv.urls = (st?.urls as string[]) ?? [];
  } catch {
    srv.running = false;
    srv.urls = [];
  }
}

watch(
  () => props.visible,
  (v) => {
    if (v) {
      result.value = "";
      void refreshStatus();
    }
  },
);

async function submit() {
  const it = props.item;
  if (!it?.path) {
    Message.warning("只有本机文件可以分享");
    return;
  }
  if (it.src !== "local") {
    Message.warning("远端条目请先到对应远端做分享，这里只分享本机文件");
    return;
  }
  creating.value = true;
  try {
    if (!srv.running) {
      const st = await GoTools?.LanShareStart(DEFAULT_PORT, "", form.stopMin);
      if (!st?.running) throw new Error("服务未能启动");
      srv.running = true;
      srv.urls = (st?.urls as string[]) ?? [];
    }
    const l = await GoTools?.LanShareCreateLink(it.path, form.hours, form.max);
    if (!l?.token) throw new Error("链接登记失败");
    result.value = `${srv.urls[0] ?? ""}/s/${l.token}`;
    Message.success("分享链接已生成");
  } catch (e: any) {
    Message.error(`分享失败：${errText(e)}`);
  } finally {
    creating.value = false;
  }
}

async function copy(t: string) {
  if (!t) return;
  try {
    await navigator.clipboard.writeText(t);
    Message.success("已复制");
  } catch {
    // WebView2 非安全上下文里 clipboard API 会被拒，退回 execCommand
    const ta = document.createElement("textarea");
    ta.value = t;
    document.body.appendChild(ta);
    ta.select();
    document.execCommand("copy");
    document.body.removeChild(ta);
    Message.success("已复制");
  }
}

function close() {
  emit("update:visible", false);
}
</script>

<style scoped lang="less">
.sh-warn {
  margin-bottom: 12px;
}
.sh-file {
  margin-bottom: 8px;
}
.mono {
  font-family: var(--code-font-family);
}
.break {
  word-break: break-all;
}
.sh-result {
  margin-top: 4px;
}
.sh-result-t {
  font-size: 13px;
  margin-bottom: 6px;
}
.sh-urlgrp {
  display: flex;
}
.sh-tip {
  margin-top: 8px;
  font-size: 12px;
  color: var(--color-text-3);
}
</style>
