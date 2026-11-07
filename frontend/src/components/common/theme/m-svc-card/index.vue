<template>
  <!--
    m-svc-card：项目统一自绘服务/实体实例卡外壳（m-* 家族成员，与 m-card / m-button 同一套约定）。
    版式承接首页「本地服务实例」里手写的 svc-card：上段（彩色 logo + 名称 + 一行描述）
    + 下段（状态圆点 com-badge + 文案 | 右侧操作区）。自有 m-svc-* 类名，
    度量全走密度联动变量（--base-padding / --base-margin / --base-radius-default / --base-size-*）。
    状态文案颜色复用全局 .com-state 语义类（styles/theme/style.scss）。

    API：
      props  name / desc / tone(red|blue|purple|green|arcoblue|orange|cyan|gray)
             running / onText / offText / inheritBg
      slots  logo（填字体图标或 Arco 图标）/ extra（右侧开关、标签等操作）
      events 无——click / mouseenter 等由调用方经 attrs 透传挂在根元素上；
              下段操作区自带 click.stop，点开关不会误触整卡跳转。

    背景系统约定（与 m-card 一致）：inheritBg 控制「是否继承上级背景」。
      - false（默认）：普通实例卡，自身铺底；开玻璃时吃磨砂 tint。
      - true：卡片不铺底、不吃玻璃磨砂、描边/投影透明，让父级与全局主题背景透过来。
  -->
  <div class="m-svc-card" :class="{ 'm-svc-inherit': inheritBg }">
    <div class="m-svc-top">
      <div class="m-svc-logo" :class="'tone-' + tone">
        <slot name="logo" />
      </div>
      <div class="m-svc-meta">
        <a-tooltip :content="name">
          <div class="m-svc-name">{{ name }}</div>
        </a-tooltip>
        <div class="m-svc-desc">{{ desc }}</div>
      </div>
    </div>
    <div class="m-svc-bottom" @click.stop>
      <!-- 状态圆点承担颜色语义（与 a-badge 一致），文案只做说明；运行中开启动效 -->
      <div class="com-state" :class="running ? 'is-on' : 'is-dim'">
        <com-badge :status="running ? 'success' : 'normal'" :animated="running" />
        <span>{{ running ? onText : offText }}</span>
      </div>
      <div class="m-svc-extra">
        <slot name="extra" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
withDefaults(defineProps<{
  /** 实例名（超长省略 + tooltip 看全文） */
  name?: string;
  /** 名称下一行描述（版本 · 端口等，由调用方拼好） */
  desc?: string;
  /** logo 底色语义：red / blue / purple / green / arcoblue / orange / cyan / gray；
      注意 blue 是天蓝 blue-6（MySQL 传统色，承接旧 logo-*），品牌蓝用 arcoblue（= m-stat-card 的 tone-blue） */
  tone?: string;
  /** 是否运行中：驱动状态圆点、文案与动效 */
  running?: boolean;
  /** running=true 时的状态文案 */
  onText?: string;
  /** running=false 时的状态文案 */
  offText?: string;
  /** 是否继承上级背景：true 时卡片透明、不铺底、不吃玻璃磨砂，露出父级/全局主题背景 */
  inheritBg?: boolean;
}>(), {
  name: "",
  desc: "",
  tone: "gray",
  running: false,
  onText: "已启用",
  offText: "已停止",
  inheritBg: false,
});
</script>

<style scoped>
.m-svc-card {
  background: var(--color-bg-2);
  border: 1px solid var(--color-border-1);
  border-radius: var(--base-radius-default);
  padding: var(--base-padding);
  cursor: pointer;
  transition: box-shadow 0.2s;
}

.m-svc-card:hover {
  box-shadow: 0 4px 16px rgba(var(--primary-6), 0.08);
}

.m-svc-top {
  display: flex;
  align-items: center;
  gap: var(--base-margin);
  margin-bottom: var(--base-margin);
}

.m-svc-logo {
  width: 36px;
  height: 36px;
  border-radius: var(--base-radius-default);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: var(--base-size-large);
  flex-shrink: 0;
}

.tone-red {
  background: rgba(var(--red-6), 0.05);
  color: rgb(var(--red-6));
}

.tone-blue {
  background: rgba(var(--blue-6), 0.05);
  color: rgb(var(--blue-6));
}

.tone-purple {
  background: rgba(var(--purple-6), 0.05);
  color: rgb(var(--purple-6));
}

.tone-green {
  background: rgba(var(--green-6), 0.05);
  color: rgb(var(--green-6));
}

.tone-arcoblue {
  background: rgba(var(--arcoblue-6), 0.05);
  color: rgb(var(--arcoblue-6));
}

.tone-orange {
  background: rgba(var(--orange-6), 0.05);
  color: rgb(var(--orange-6));
}

.tone-cyan {
  background: rgba(var(--cyan-6), 0.05);
  color: rgb(var(--cyan-6));
}

.tone-gray {
  background: var(--color-fill-2);
  color: var(--color-text-3);
}

.m-svc-meta {
  flex: 1;
  min-width: 0;
}

.m-svc-name {
  font-weight: 600;
  font-size: var(--base-size-default);
  color: var(--color-text-1);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.m-svc-desc {
  font-size: var(--base-size-small);
  color: var(--color-text-3);
  margin-top: var(--base-radius-small);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.m-svc-bottom {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.m-svc-extra {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
</style>

<!-- 非 scoped：inheritBg 是对全站玻璃皮肤（tokens.scss 里 body.app-glass .m-svc-card）的显式退出，
     需稳定压过带 body 祖先类的高特异性规则，故用 !important 明确「本卡片不铺任何底」。 -->
<style>
.m-svc-card.m-svc-inherit {
  background: transparent !important;
  border-color: transparent !important;
  box-shadow: none !important;
  backdrop-filter: none !important;
  -webkit-backdrop-filter: none !important;
}
</style>
