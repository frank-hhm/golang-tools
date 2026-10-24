<template>
  <layout-body title="Go 消息提示 Demo">
    <a-card class="demo-card">
      <a-space direction="vertical" fill :size="16" style="max-width: 640px">
        <a-typography-paragraph type="secondary">
          验证 Go 后端能否像前端 <code>import {{ '{ Message }' }} from "@arco-design/web-vue"</code> 一样弹提示。
          链路：Go <code>App.ShowMessage(level, text)</code> → 在 <b>WebView 之外的桌面</b>创建原生置顶通知小窗
          （<code>/widget/toast</code>，屏幕右下角（与系统通知同位置，避开任务栏）、4 秒自动消失、新通知替换旧通知）；前端可点 ✕ 手动关闭。
        </a-typography-paragraph>

        <a-form-item label="提示内容" style="margin-bottom: 0">
          <a-input v-model="text" placeholder="要显示的消息文本" allow-clear />
        </a-form-item>

        <a-space wrap>
          <a-button type="primary" @click="fromGo('info')">Go → info</a-button>
          <a-button type="outline" status="success" @click="fromGo('success')">Go → success</a-button>
          <a-button type="outline" status="warning" @click="fromGo('warning')">Go → warning</a-button>
          <a-button type="outline" status="danger" @click="fromGo('error')">Go → error</a-button>
        </a-space>

        <a-divider style="margin: 4px 0" />

        <a-form-item label="通知标题" style="margin-bottom: 0">
          <a-input v-model="ntTitle" placeholder="通知标题（加粗首行）" allow-clear />
        </a-form-item>
        <a-form-item label="通知内容" style="margin-bottom: 0">
          <a-textarea v-model="ntContent" :auto-size="{ minRows: 2, maxRows: 4 }" placeholder="通知正文（最多显示 3 行）" />
        </a-form-item>
        <a-space wrap>
          <a-button type="primary" @click="notify()"><template #icon><icon-notification /></template>Go → 系统通知卡片</a-button>
          <a-button type="outline" status="success" @click="sysNotify()"><template #icon><icon-notification /></template>Go → 系统通知中心（原生 Toast）</a-button>
        </a-space>

        <a-divider style="margin: 4px 0" />

        <a-space wrap>
          <a-button @click="fromWeb('info')">前端直接调 info（对照）</a-button>
          <a-button @click="fromWeb('success')">前端直接调 success（对照）</a-button>
          <a-button @click="fromWeb('warning')">前端直接调 warning（对照）</a-button>
          <a-button @click="fromWeb('error')">前端直接调 error（对照）</a-button>
        </a-space>

        <a-alert type="info">
          「Go →」按钮：屏幕右下角弹出<b>原生桌面通知窗</b>（独立于当前窗口，与系统通知同位置、置顶、自动消失）；
          「前端直调」按钮：Arco Message 在<b>窗口内部</b>顶部弹出，两者对照。
          「系统通知卡片」：右下角驻留式通知（仿 Windows toast：应用名 + 标题 + 内容 + ✕），不自动消失，点 ✕ 关闭。
          「系统通知中心」：直接调 Windows 原生 Toast（Shell/WinRT 由操作系统渲染，自动进通知中心/历史，受专注助手控制）。
          后端任意位置均可调用 <code>a.ShowMessage(...)</code> / <code>a.ShowNotification(...)</code> / <code>a.ShowSystemNotification(...)</code>。
        </a-alert>
      </a-space>
    </a-card>
  </layout-body>
</template>

<script setup lang="ts">
import { getCurrentInstance, ref } from "vue";
import { Message } from "@arco-design/web-vue";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const text = ref("你好，来自 Golang 的消息提示 🎉");
const ntTitle = ref("回复完成");
const ntContent = ref("会话：完善串口调试（USB调试），功能如图 已完成回复，可打开查看。");

function notify() {
  try {
    GoTools?.ShowNotification(ntTitle.value || "通知", ntContent.value || "(无内容)");
  } catch (e: any) {
    Message.error("调用 Go 方法失败：" + (e?.message || e));
  }
}

function sysNotify() {
  try {
    GoTools?.ShowSystemNotification(ntTitle.value || "通知", ntContent.value || "(无内容)");
  } catch (e: any) {
    Message.error("调用 Go 方法失败：" + (e?.message || e));
  }
}

function fromGo(level: string) {
  try {
    GoTools?.ShowMessage(level, text.value || "(空)");
  } catch (e: any) {
    Message.error("调用 Go 方法失败：" + (e?.message || e));
  }
}

function fromWeb(level: string) {
  (Message as any)[level](text.value || "(空)");
}
</script>

<style scoped>
.demo-card {
  min-width: 820px;
  min-height: 400px;
  background: var(--color-bg-2);
  margin-bottom: 16px;
}

.demo-card code {
  padding: 1px 5px;
  border-radius: 3px;
}
</style>
