/**
 * config-provider/context：ConfigProvider 的注入键与上下文类型，
 * 1:1 对应 Arco `config-provider/context.js`（Arco 里只是一个 Symbol，这里补上 TS 类型）。
 *
 * 注意：getPrefixCls 依赖这里的 configProviderInjectionKey，二者必须共用同一个 Symbol，
 * 否则 inject 匹配不上，前缀链会静默退化到默认值（这正是 Arco 自身的组织方式）。
 */
import type { InjectionKey } from "vue";

export interface ConfigProviderContext {
  /** 类名前缀，覆盖默认 "m" */
  prefixCls?: string;
  /** 全局：滚动时是否关闭浮层（trigger 读取） */
  scrollToClose?: boolean;
  /** 全局：滚动时是否实时更新浮层位置（trigger 读取） */
  updateAtScroll?: boolean;
  /** 其余全局配置项按需扩展 */
  [key: string]: any;
}

export const configProviderInjectionKey: InjectionKey<ConfigProviderContext> =
  Symbol("m-config-provider");
