/**
 * trigger：m-* 原生组件库的浮层定位与交互引擎，1:1 复刻 Arco `trigger/trigger.js`。
 *
 * 复刻边界（与需求一致）：
 * - 除类名前缀（getPrefixCls → "m-trigger"）与组件名（"Trigger"，内部使用）外，
 *   props / emits / 事件处理 / 定位计算 / 翻面 / 滚动跟随 / 嵌套上下文 / 动画 / Teleport 结构
 *   全部逐行对应 Arco 编译形态（defineComponent + createVNode render，无 JSX，因本仓库未挂 vue-jsx 插件）。
 * - z-index 接入本库共享管理器 useZIndex（对应 Arco 的 usePopupManager("popup")），机制一致。
 * - ResizeObserver / ClientOnly / useFirstElement 等依赖均已在本库 1:1 就位。
 */
import {
  Fragment,
  Teleport,
  Transition,
  computed,
  createVNode,
  defineComponent,
  inject,
  mergeProps,
  nextTick,
  onBeforeUnmount,
  onDeactivated,
  onMounted,
  onUpdated,
  provide,
  reactive,
  ref,
  toRefs,
  vShow,
  watch,
  withDirectives,
} from "vue";
import type { PropType, Ref } from "vue";
import { getPrefixCls } from "../_utils/global-config";
import {
  getArrowStyle,
  getElementScrollRect,
  getPopupStyle,
  getScrollElements,
  getTransformOrigin,
} from "./utils";
import type { TriggerPosition, TriggerRect } from "./utils";
import ResizeObserver from "../_components/resize-observer";
import { off, on } from "../_utils/dom";
import { isEmptyChildren, mergeFirstChild } from "../_utils/vue-utils";
import usePickSlots from "../_hooks/use-pick-slots";
import { triggerInjectionKey } from "./context";
import type { TriggerContext } from "./context";
import { throttleByRaf } from "../_utils/throttle-by-raf";
import { useZIndex } from "../_hooks/use-z-index";
import { useResizeObserver } from "../_hooks/use-resize-observer";
import ClientOnly from "../_components/client-only";
import { useTeleportContainer } from "../_hooks/use-teleport-container";
import { configProviderInjectionKey } from "../config-provider/context";
import { useFirstElement } from "../_hooks/use-first-element";
import { omit } from "../_utils/omit";

const TRIGGER_EVENTS = [
  "onClick",
  "onMouseenter",
  "onMouseleave",
  "onFocusin",
  "onFocusout",
  "onContextmenu",
];

type TriggerMethod = "hover" | "click" | "contextMenu" | "focus" | "annotation";

const _Trigger = defineComponent({
  name: "Trigger",
  inheritAttrs: false,
  props: {
    popupVisible: {
      type: Boolean,
      default: undefined,
    },
    defaultPopupVisible: {
      type: Boolean,
      default: false,
    },
    trigger: {
      type: [String, Array] as PropType<TriggerMethod | TriggerMethod[]>,
      default: "hover",
    },
    position: {
      type: String as PropType<TriggerPosition>,
      default: "bottom",
    },
    disabled: {
      type: Boolean,
      default: false,
    },
    popupOffset: {
      type: Number,
      default: 0,
    },
    popupTranslate: {
      type: [Array, Object] as PropType<
        [number, number] | Partial<Record<TriggerPosition, [number, number]>>
      >,
    },
    showArrow: {
      type: Boolean,
      default: false,
    },
    alignPoint: {
      type: Boolean,
      default: false,
    },
    popupHoverStay: {
      type: Boolean,
      default: true,
    },
    blurToClose: {
      type: Boolean,
      default: true,
    },
    clickToClose: {
      type: Boolean,
      default: true,
    },
    clickOutsideToClose: {
      type: Boolean,
      default: true,
    },
    unmountOnClose: {
      type: Boolean,
      default: true,
    },
    contentClass: {
      type: [String, Array, Object] as PropType<string | string[] | Record<string, any>>,
    },
    contentStyle: {
      type: Object as PropType<Record<string, any>>,
    },
    arrowClass: {
      type: [String, Array, Object] as PropType<string | string[] | Record<string, any>>,
    },
    arrowStyle: {
      type: Object as PropType<Record<string, any>>,
    },
    popupStyle: {
      type: Object as PropType<Record<string, any>>,
    },
    animationName: {
      type: String,
      default: "fade-in",
    },
    duration: {
      type: [Number, Object] as PropType<number | { enter: number; leave: number }>,
    },
    mouseEnterDelay: {
      type: Number,
      default: 100,
    },
    mouseLeaveDelay: {
      type: Number,
      default: 100,
    },
    focusDelay: {
      type: Number,
      default: 0,
    },
    autoFitPopupWidth: {
      type: Boolean,
      default: false,
    },
    autoFitPopupMinWidth: {
      type: Boolean,
      default: false,
    },
    autoFixPosition: {
      type: Boolean,
      default: true,
    },
    popupContainer: {
      type: [String, Object] as PropType<string | HTMLElement>,
    },
    updateAtScroll: {
      type: Boolean,
      default: false,
    },
    autoFitTransformOrigin: {
      type: Boolean,
      default: false,
    },
    hideEmpty: {
      type: Boolean,
      default: false,
    },
    openedClass: {
      type: [String, Array, Object] as PropType<string | string[] | Record<string, any>>,
    },
    autoFitPosition: {
      type: Boolean,
      default: true,
    },
    renderToBody: {
      type: Boolean,
      default: true,
    },
    preventFocus: {
      type: Boolean,
      default: false,
    },
    scrollToClose: {
      type: Boolean,
      default: false,
    },
    scrollToCloseDistance: {
      type: Number,
      default: 0,
    },
  },
  emits: {
    "update:popupVisible": (_visible: boolean) => true,
    popupVisibleChange: (_visible: boolean) => true,
    show: () => true,
    hide: () => true,
    resize: () => true,
  },
  setup(props, { emit, slots, attrs }) {
    const { popupContainer } = toRefs(props);
    const prefixCls = getPrefixCls("trigger");
    const popupAttrs = computed(() => omit(attrs as Record<string, any>, TRIGGER_EVENTS));
    const configCtx = inject(configProviderInjectionKey, undefined);
    const triggerMethods = computed(() =>
      ([] as TriggerMethod[]).concat(props.trigger as TriggerMethod | TriggerMethod[]),
    );
    const childrenRefs = new Set<Ref<HTMLElement | undefined>>();
    const triggerCtx = inject(triggerInjectionKey, undefined);
    const { children, firstElement } = useFirstElement();
    const popupRef = ref<HTMLElement>();
    const popupVisible = ref<boolean>(props.defaultPopupVisible);
    const popupPosition = ref<TriggerPosition>(props.position);
    const popupStyle = ref<Record<string, any>>({});
    const transformStyle = ref<Record<string, any>>({});
    const arrowStyle = ref<Record<string, any>>({});
    const arrowRef = ref<HTMLElement>();
    const mousePosition = ref({ top: 0, left: 0 });
    let scrollPosition: [number, number] | null = null;
    let windowScrollPosition: [number, number] | null = null;
    const computedVisible = computed(() => props.popupVisible ?? popupVisible.value);
    const { teleportContainer, containerRef } = useTeleportContainer({
      popupContainer: popupContainer as Ref<string | HTMLElement | undefined>,
      visible: computedVisible,
      documentContainer: true,
    });
    const { zIndex } = useZIndex("popup", { visible: computedVisible });
    let delayTimer = 0;
    let outsideListener = false;
    let windowListener = false;
    const cleanDelayTimer = () => {
      if (delayTimer) {
        window.clearTimeout(delayTimer);
        delayTimer = 0;
      }
    };
    const updateMousePosition = (e: MouseEvent) => {
      if (props.alignPoint) {
        const { pageX, pageY } = e;
        mousePosition.value = { top: pageY, left: pageX };
      }
    };
    const updatePopupStyle = () => {
      if (!firstElement.value || !popupRef.value || !containerRef.value) {
        return;
      }
      const containerRect = containerRef.value.getBoundingClientRect();
      const triggerRect: TriggerRect = props.alignPoint
        ? {
            top: mousePosition.value.top,
            bottom: mousePosition.value.top,
            left: mousePosition.value.left,
            right: mousePosition.value.left,
            scrollTop: mousePosition.value.top,
            scrollBottom: mousePosition.value.top,
            scrollLeft: mousePosition.value.left,
            scrollRight: mousePosition.value.left,
            width: 0,
            height: 0,
          }
        : getElementScrollRect(firstElement.value, containerRect);
      const getPopupRect = () => getElementScrollRect(popupRef.value as HTMLElement, containerRect);
      const popupRect = getPopupRect();
      const { style, position } = getPopupStyle(props.position, containerRect, triggerRect, popupRect, {
        offset: props.popupOffset,
        translate: props.popupTranslate,
        customStyle: props.popupStyle,
        autoFitPosition: props.autoFitPosition,
      });
      if (props.autoFitTransformOrigin) {
        transformStyle.value = { transformOrigin: getTransformOrigin(position) };
      }
      if (props.autoFitPopupMinWidth) {
        style.minWidth = `${triggerRect.width}px`;
      } else if (props.autoFitPopupWidth) {
        style.width = `${triggerRect.width}px`;
      }
      if (popupPosition.value !== position) {
        popupPosition.value = position;
      }
      popupStyle.value = style;
      if (props.showArrow) {
        nextTick(() => {
          arrowStyle.value = getArrowStyle(position, triggerRect, getPopupRect(), {
            customStyle: props.arrowStyle,
          });
        });
      }
    };
    const changeVisible = (visible: boolean, delay?: number) => {
      if (visible === computedVisible.value && delayTimer === 0) {
        return;
      }
      const update = () => {
        popupVisible.value = visible;
        emit("update:popupVisible", visible);
        emit("popupVisibleChange", visible);
        if (visible) {
          nextTick(() => {
            updatePopupStyle();
          });
        }
      };
      if (!visible) {
        scrollPosition = null;
        windowScrollPosition = null;
      }
      if (delay) {
        cleanDelayTimer();
        if (visible !== computedVisible.value) {
          delayTimer = window.setTimeout(update, delay);
        }
      } else {
        update();
      }
    };
    const handleClick = (e: MouseEvent) => {
      (attrs as any).onClick?.(e);
      if (props.disabled || (computedVisible.value && !props.clickToClose)) {
        return;
      }
      if (triggerMethods.value.includes("click")) {
        updateMousePosition(e);
        changeVisible(!computedVisible.value);
      } else if (triggerMethods.value.includes("contextMenu") && computedVisible.value) {
        changeVisible(false);
      }
    };
    const handleMouseEnter = (e: MouseEvent) => {
      (attrs as any).onMouseenter?.(e);
      if (props.disabled || !triggerMethods.value.includes("hover")) {
        return;
      }
      updateMousePosition(e);
      changeVisible(true, props.mouseEnterDelay);
    };
    const handleMouseEnterWithContext = (e: MouseEvent) => {
      triggerCtx?.onMouseenter(e);
      handleMouseEnter(e);
    };
    const handleMouseLeave = (e: MouseEvent) => {
      (attrs as any).onMouseleave?.(e);
      if (props.disabled || !triggerMethods.value.includes("hover")) {
        return;
      }
      changeVisible(false, props.mouseLeaveDelay);
    };
    const handleMouseLeaveWithContext = (e: MouseEvent) => {
      triggerCtx?.onMouseleave(e);
      handleMouseLeave(e);
    };
    const handleFocusin = (e: FocusEvent) => {
      (attrs as any).onFocusin?.(e);
      if (props.disabled || !triggerMethods.value.includes("focus")) {
        return;
      }
      changeVisible(true, props.focusDelay);
    };
    const handleFocusout = (e: FocusEvent) => {
      (attrs as any).onFocusout?.(e);
      if (props.disabled || !triggerMethods.value.includes("focus")) {
        return;
      }
      if (!props.blurToClose) {
        return;
      }
      changeVisible(false);
    };
    const handleContextmenu = (e: MouseEvent) => {
      (attrs as any).onContextmenu?.(e);
      if (
        props.disabled ||
        !triggerMethods.value.includes("contextMenu") ||
        (computedVisible.value && !props.clickToClose)
      ) {
        return;
      }
      updateMousePosition(e);
      changeVisible(!computedVisible.value);
      e.preventDefault();
    };
    const addChildRef = (refValue: Ref<HTMLElement | undefined>) => {
      childrenRefs.add(refValue);
      triggerCtx?.addChildRef(refValue);
    };
    const removeChildRef = (refValue: Ref<HTMLElement | undefined>) => {
      childrenRefs.delete(refValue);
      triggerCtx?.removeChildRef(refValue);
    };
    provide<TriggerContext>(
      triggerInjectionKey,
      reactive({
        onMouseenter: handleMouseEnterWithContext,
        onMouseleave: handleMouseLeaveWithContext,
        addChildRef,
        removeChildRef,
      }) as TriggerContext,
    );
    const removeOutsideListener = () => {
      off(document.documentElement, "mousedown", handleOutsideClick);
      outsideListener = false;
    };
    const contentSlot = usePickSlots(slots, "content");
    const hidePopup = computed(() => props.hideEmpty && isEmptyChildren(contentSlot.value?.()));
    const handleOutsideClick = (e: Event) => {
      if (firstElement.value?.contains(e.target as Node) || popupRef.value?.contains(e.target as Node)) {
        return;
      }
      for (const item of childrenRefs) {
        if (item.value?.contains(e.target as Node)) {
          return;
        }
      }
      removeOutsideListener();
      changeVisible(false);
    };
    const isExceedThreshold = (oldPosition: [number, number], element: HTMLElement) => {
      const [scrollTop, scrollLeft] = oldPosition;
      const { scrollTop: newScrollTop, scrollLeft: newScrollLeft } = element;
      return (
        Math.abs(newScrollTop - scrollTop) >= props.scrollToCloseDistance ||
        Math.abs(newScrollLeft - scrollLeft) >= props.scrollToCloseDistance
      );
    };
    const handleScroll = throttleByRaf((e: Event) => {
      if (computedVisible.value) {
        if (props.scrollToClose || configCtx?.scrollToClose) {
          const element = e.target as HTMLElement;
          if (!scrollPosition) {
            scrollPosition = [element.scrollTop, element.scrollLeft];
          }
          if (isExceedThreshold(scrollPosition, element)) {
            changeVisible(false);
          } else {
            updatePopupStyle();
          }
        } else {
          updatePopupStyle();
        }
      }
    });
    const removeWindowScroll = () => {
      off(window, "scroll", onWindowScroll);
      windowListener = false;
    };
    const onWindowScroll = throttleByRaf((e: Event) => {
      const element = (e.target as Document).documentElement;
      if (!windowScrollPosition) {
        windowScrollPosition = [element.scrollTop, element.scrollLeft];
      }
      if (isExceedThreshold(windowScrollPosition, element)) {
        changeVisible(false);
        removeWindowScroll();
      }
    });
    const handleResize = () => {
      if (computedVisible.value) {
        updatePopupStyle();
      }
    };
    const onTargetResize = () => {
      handleResize();
      emit("resize");
    };
    const handlePopupMouseDown = (e: MouseEvent) => {
      if (props.preventFocus) {
        e.preventDefault();
      }
    };
    triggerCtx?.addChildRef(popupRef);
    const triggerCls = computed(() => (computedVisible.value ? props.openedClass : undefined));
    let scrollElements: HTMLElement[] | undefined;
    watch(computedVisible, (value) => {
      if (props.clickOutsideToClose) {
        if (!value && outsideListener) {
          removeOutsideListener();
        } else if (value && !outsideListener) {
          on(document.documentElement, "mousedown", handleOutsideClick);
          outsideListener = true;
        }
      }
      if (props.scrollToClose || configCtx?.scrollToClose) {
        on(window, "scroll", onWindowScroll);
        windowListener = true;
      }
      if (props.updateAtScroll || configCtx?.updateAtScroll) {
        if (value) {
          scrollElements = getScrollElements(firstElement.value);
          for (const item of scrollElements) {
            item.addEventListener("scroll", handleScroll);
          }
        } else if (scrollElements) {
          for (const item of scrollElements) {
            item.removeEventListener("scroll", handleScroll);
          }
          scrollElements = undefined;
        }
      }
      if (value) {
        mounted.value = true;
      }
    });
    watch(
      () => [props.autoFitPopupWidth, props.autoFitPopupMinWidth],
      () => {
        if (computedVisible.value) {
          updatePopupStyle();
        }
      },
    );
    const { createResizeObserver, destroyResizeObserver } = useResizeObserver({
      elementRef: containerRef,
      onResize: handleResize,
    });
    onMounted(() => {
      createResizeObserver();
      if (computedVisible.value) {
        updatePopupStyle();
        if (props.clickOutsideToClose && !outsideListener) {
          on(document.documentElement, "mousedown", handleOutsideClick);
          outsideListener = true;
        }
        if (props.updateAtScroll || configCtx?.updateAtScroll) {
          scrollElements = getScrollElements(firstElement.value);
          for (const item of scrollElements) {
            item.addEventListener("scroll", handleScroll);
          }
        }
      }
    });
    onUpdated(() => {
      if (computedVisible.value) {
        updatePopupStyle();
      }
    });
    onDeactivated(() => {
      changeVisible(false);
    });
    onBeforeUnmount(() => {
      triggerCtx?.removeChildRef(popupRef);
      destroyResizeObserver();
      if (outsideListener) {
        removeOutsideListener();
      }
      if (windowListener) {
        removeWindowScroll();
      }
      if (scrollElements) {
        for (const item of scrollElements) {
          item.removeEventListener("scroll", handleScroll);
        }
        scrollElements = undefined;
      }
    });
    const mounted = ref(computedVisible.value);
    const isAnimation = ref(false);
    const onAnimationStart = () => {
      isAnimation.value = true;
    };
    const handleShow = () => {
      isAnimation.value = false;
      if (computedVisible.value) {
        emit("show");
      }
    };
    const handleHide = () => {
      isAnimation.value = false;
      if (!computedVisible.value) {
        mounted.value = false;
        emit("hide");
      }
    };
    return () => {
      children.value = slots.default?.() ?? [];
      mergeFirstChild(children.value, {
        class: triggerCls.value as any,
        onClick: handleClick,
        onMouseenter: handleMouseEnter,
        onMouseleave: handleMouseLeave,
        onFocusin: handleFocusin,
        onFocusout: handleFocusout,
        onContextmenu: handleContextmenu,
      });
      return createVNode(Fragment, null, [
        props.autoFixPosition
          ? createVNode(ResizeObserver, { onResize: onTargetResize }, { default: () => [children.value] })
          : (children.value as any),
        createVNode(ClientOnly, null, {
          default: () => [
            createVNode(
              Teleport,
              { to: teleportContainer.value as any, disabled: !props.renderToBody },
              {
                default: () => [
                  (!props.unmountOnClose || computedVisible.value || mounted.value) &&
                    !hidePopup.value &&
                    createVNode(ResizeObserver, { onResize: handleResize }, {
                      default: () => [
                        createVNode(
                          "div",
                          mergeProps(
                            {
                              ref: popupRef,
                              class: [
                                `${prefixCls}-popup`,
                                `${prefixCls}-position-${popupPosition.value}`,
                              ],
                              style: {
                                ...popupStyle.value,
                                zIndex: zIndex.value,
                                pointerEvents: isAnimation.value ? "none" : "auto",
                              },
                              "trigger-placement": popupPosition.value,
                              onMouseenter: handleMouseEnterWithContext,
                              onMouseleave: handleMouseLeaveWithContext,
                              onMousedown: handlePopupMouseDown,
                            },
                            popupAttrs.value as any,
                          ),
                          [
                            createVNode(
                              Transition,
                              {
                                name: props.animationName,
                                duration: props.duration as any,
                                appear: true,
                                onBeforeEnter: onAnimationStart,
                                onAfterEnter: handleShow,
                                onBeforeLeave: onAnimationStart,
                                onAfterLeave: handleHide,
                              },
                              {
                                default: () => [
                                  withDirectives(
                                    createVNode(
                                      "div",
                                      {
                                        class: `${prefixCls}-popup-wrapper`,
                                        style: transformStyle.value,
                                      },
                                      [
                                        createVNode(
                                          "div",
                                          {
                                            class: [`${prefixCls}-content`, props.contentClass],
                                            style: props.contentStyle,
                                          },
                                          [slots.content?.()],
                                        ),
                                        props.showArrow &&
                                          createVNode(
                                            "div",
                                            {
                                              ref: arrowRef,
                                              class: [`${prefixCls}-arrow`, props.arrowClass],
                                              style: arrowStyle.value,
                                            },
                                            null,
                                          ),
                                      ],
                                    ),
                                    [[vShow, computedVisible.value]],
                                  ),
                                ],
                              },
                            ),
                          ],
                        ),
                      ],
                    }),
                ],
              },
            ),
          ],
        }),
      ]);
    };
  },
});

export default _Trigger;
export { _Trigger as Trigger };
