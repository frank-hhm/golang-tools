<template>
  <!--
    m-tool-cell：项目统一自绘工具格外壳（m-* 家族成员，与 m-svc-card / m-card 同一套约定）。
    版式承接首页「快捷操作」与「全部工具」弹窗里手写的 tool-cell：居中图标（可选下方一行名称）。
    背景 / 描边 / 圆角 / hover 投影 / inheritBg 全部一对一复刻 m-svc-card，保证格子与服务实例卡视觉一致；
    度量全走密度联动变量（--base-padding / --base-radius-default / --base-size-*）。

    API：
      props  icon（功能配置图标原值：icon-xxx 字体图标 / Arco 图标名两种写法都认；
                   认不出时兜底 IconApps，与旧手写格一致）
             name（非空时在图标下方画一行名称，供「全部工具」弹窗网格使用）
             inheritBg（复刻 m-svc-card：true 时不铺底、不吃玻璃磨砂，露出父级 / 全局主题背景）
      events 无——click / mouseenter / pointerdown / 拖拽 handlers 等由调用方经 attrs 透传挂在根元素上；
              reorder-dragging / reorder-over 排序反馈类同样经 class 透传（其样式在全局 styles/index.scss）。
  -->
  <div class="m-tool-cell" :class="{ 'm-tool-cell-named': !!name, 'm-tool-inherit': inheritBg }">
    <div class="m-tool-icon-wrap">
      <i v-if="isIconfontIcon(icon)" class="icon m-tool-icon" :class="icon" />
      <component v-else :is="arcoIconComp(icon) || 'IconApps'" class="m-tool-icon" />
    </div>
    <span v-if="name" class="m-tool-name">{{ name }}</span>
  </div>
</template>

<script setup lang="ts">
import { arcoIconComp, isIconfontIcon } from "@/utils/tool-icon";

withDefaults(defineProps<{
  /** 功能配置图标原值：icon-xxx 字体图标 / Arco 图标名；认不出兜底 IconApps */
  icon?: string;
  /** 非空时在图标下方画一行名称（「全部工具」弹窗网格用） */
  name?: string;
  /** 是否继承上级背景：true 时格子透明、不铺底、不吃玻璃磨砂，露出父级 / 全局主题背景 */
  inheritBg?: boolean;
}>(), {
  icon: "",
  name: "",
  inheritBg: false,
});
</script>

<style scoped>
.m-tool-cell {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  /* 作为网格项直接铺满列宽（m-tooltip 走 1:1 Arco trigger：不再包 wrapper，本格子即网格直接子节点） */
  /* width: 100%; */
  padding: var(--base-padding);
  cursor: pointer;
  /* 背景 / 描边 / 圆角 / hover 投影一对一复刻 m-svc-card，保证格子与服务实例卡一致 */
  background: var(--color-bg-2);
  border: 1px solid var(--color-border-1);
  border-radius: var(--base-radius-default);
  color:var(--color-text-1);
  transition: box-shadow 0.2s;
}

.m-tool-cell:hover {
  box-shadow: 0 4px 16px rgba(var(--primary-6), 0.08);
}

.m-tool-cell:active {
  transform: scale(0.95);
}

.m-tool-icon {
  font-size: 28px;
  color: inherit;
}

/* 带名称的格子（「全部工具」弹窗）：图标与名称纵向留间距，底部收一点高 */
.m-tool-cell-named {
  gap: 6px;
  padding: 8px 8px 4px;
}

.m-tool-name {
  font-size: var(--base-size-small);
  color: var(--color-text-1);
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-align: center;
}
</style>

<!-- 非 scoped：inheritBg 是对全站玻璃皮肤（tokens.scss 里 body.app-glass .m-tool-cell）的显式退出，
     需稳定压过带 body 祖先类的高特异性规则，故用 !important 明确「本格子不铺任何底」。 -->
<style>
.m-tool-cell.m-tool-inherit {
  background: transparent !important;
  border-color: transparent !important;
  box-shadow: none !important;
  backdrop-filter: none !important;
  -webkit-backdrop-filter: none !important;
}
</style>
