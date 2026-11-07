/**
 * global-config：m-* 原生组件库的类名前缀与全局配置入口，1:1 复刻 Arco `_utils/global-config.js`。
 *
 * 所有组件的类名都由 getPrefixCls(name) 生成 `${prefix}-${name}`，prefix 取值链：
 *   ConfigProvider.prefixCls → 全局 $m.classPrefix → 默认 CLASS_PREFIX("m")。
 * 这样组件内部只写语义名（如 getPrefixCls("select-view")），前缀集中可控，
 * 避免把 "m-" 硬散落在上千处模板字符串里（这正是 Arco 自身的做法）。
 */
import { getCurrentInstance, inject } from "vue";
import { configProviderInjectionKey } from "../config-provider/context";

/** 组件名前缀（自动导入/全局注册用），对应 Arco 的 "A" */
const COMPONENT_PREFIX = "M";
/** 类名前缀默认值，对应 Arco 的 "arco" */
const CLASS_PREFIX = "m";
/** 挂载在 app.config.globalProperties 上的全局配置键，对应 Arco 的 "$arco" */
const GLOBAL_CONFIG_NAME = "$m";

export interface GlobalConfig {
  classPrefix?: string;
  componentPrefix?: string;
}

export const getComponentPrefix = (options?: GlobalConfig): string =>
  options?.componentPrefix ?? COMPONENT_PREFIX;

/** 在 app 安装阶段设置全局类名前缀（默认已是 "m"，一般无需调用；保留以对齐 Arco 能力） */
export const setGlobalConfig = (
  app: { config: { globalProperties: Record<string, any> } },
  options?: GlobalConfig,
): void => {
  if (options && options.classPrefix) {
    app.config.globalProperties[GLOBAL_CONFIG_NAME] = {
      ...app.config.globalProperties[GLOBAL_CONFIG_NAME],
      classPrefix: options.classPrefix,
    };
  }
};

/**
 * 生成组件类名前缀。
 * @param componentName 组件语义名，如 "select-view" / "form-item"；留空返回裸前缀。
 */
export const getPrefixCls = (componentName?: string): string => {
  const instance = getCurrentInstance();
  const configProvider = inject(configProviderInjectionKey, undefined);
  const prefix =
    configProvider?.prefixCls ??
    instance?.appContext.config.globalProperties[GLOBAL_CONFIG_NAME]?.classPrefix ??
    CLASS_PREFIX;
  return componentName ? `${prefix}-${componentName}` : prefix;
};
