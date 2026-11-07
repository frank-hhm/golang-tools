/**
 * client-only：仅在客户端（mounted 之后）渲染默认插槽，SSR 阶段返回 null，
 * 1:1 复刻 Arco `_components/client-only.js`。trigger 用它保证弹层只在浏览器端 Teleport。
 */
import { defineComponent, onMounted, ref } from "vue";

export default defineComponent({
  name: "ClientOnly",
  setup(_props, { slots }) {
    const mounted = ref(false);
    onMounted(() => (mounted.value = true));
    return () => {
      if (mounted.value) {
        return slots.default?.();
      }
      return null;
    };
  },
});
