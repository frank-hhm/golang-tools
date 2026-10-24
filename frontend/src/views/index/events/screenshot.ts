import { getCurrentInstance } from "vue";
import { Message } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import { useToolWindow } from "../useToolWindow";

/** 与截图预览页约定的共享存储键（同源多窗口共享 localStorage） */
const STORAGE_KEY = "tool:screenshot";

/**
 * 截图工具事件（单个 event 的封装）
 *
 * 正常路径：调后端 OpenCaptureOverlay —— Go 先冻结主屏一帧，再打开全屏无边框
 * 置顶的选区编辑浮层（/widget/capture），之后的拖框、整窗选取、缩放、标注、
 * 复制/预览全部在浮层里完成，本函数立即返回。
 *
 * 兜底路径：OpenCaptureOverlay 不可用（非 Windows、或 WebView2 创建失败）时，
 * 退回旧的 GDI 全屏遮罩 CaptureSelectedRegion，行为是「复制 + 打开预览窗」。
 */
export function useScreenshotEvent() {
  const {
    proxy: { $GoTools },
  } = getCurrentInstance() as any;
  const { openToolWindow } = useToolWindow();

  /** 触发一次区域截图 */
  const capture = async () => {
    try {
      if (typeof $GoTools?.OpenCaptureOverlay === "function") {
        await $GoTools.OpenCaptureOverlay();
        return;
      }
      await captureViaGDISelector();
    } catch (err) {
      console.error("截图失败:", err);
      Message.error("截图失败: " + err);
    }
  };

  /** 旧 GDI 遮罩选区：确认后写共享存储并打开预览窗 */
  const captureViaGDISelector = async () => {
    const data = await $GoTools.CaptureSelectedRegion("png");
    if (!data) {
      return;
    }
    try {
      localStorage.setItem(STORAGE_KEY, data);
    } catch {
      /* localStorage 不可用时忽略，仅影响新窗口预览 */
    }
    // 预览窗口若已打开（single 复用），广播通知其重新读取 localStorage 刷新；
    // 新建窗口场景下此事件无监听方，由预览页 onMounted 读 localStorage 兜底。
    try {
      Events.Emit("screenshot:updated", { t: Date.now() });
    } catch {
      /* 事件总线不可用时忽略 */
    }
    // 截图预览窗口单例：重复触发时复用并聚焦已有窗口；禁用全屏/最大化
    await openToolWindow("/tools/screenshot", "截图预览", { single: true, disableFullscreen: true });
    Message.success("截图完成，已复制到剪贴板");
  };

  return { capture };
}
