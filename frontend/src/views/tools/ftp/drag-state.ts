import type { FtpDragItem } from './types';

/**
 * 跨栏拖拽的共享状态：本地栏与远程栏两个 FilePane 实例必须共用同一份。
 *
 * 为什么单独成模块，而不是写在 FilePane 的 <script setup> 里：
 * <script setup> 内的 let 是「每个组件实例一份」——本地栏拖起文件时只有本地实例拿到负载，
 * 远程实例的那份永远是 null，于是 isForeignDrag 判不出「来自另一栏」，dragover 不 preventDefault，
 * 浏览器根本不派发 drop，跨栏拖拽上传/下载全程无反应。
 *
 * 也不用 dataTransfer 传：WebView2 里同源拖拽读不到本机的完整文件路径，只能当兜底。
 */
export const paneDrag: { side: '' | 'local' | 'remote'; item: FtpDragItem | null } = {
  side: '',
  item: null,
};

/** 一趟拖拽结束（dragend / 已派发落点）就清空：残留会把下一次 dragover 误判成合法落点 */
export function resetPaneDrag() {
  paneDrag.side = '';
  paneDrag.item = null;
}
