/**
 * 视频录制共享工具：把录好的 Blob 分块上传给 Go 后端，
 * 由后端弹出原生「另存为」对话框选择保存目录并写盘。
 */

/** ArrayBuffer 转 base64（分段避免 String.fromCharCode 参数爆炸） */
function arrayBufferToBase64(buf: ArrayBuffer): string {
  const bytes = new Uint8Array(buf);
  let bin = "";
  const SEG = 0x8000;
  for (let i = 0; i < bytes.length; i += SEG) {
    bin += String.fromCharCode.apply(null, Array.from(bytes.subarray(i, i + SEG)) as number[]);
  }
  return btoa(bin);
}

/** 上传结果：会话 ID（可用于重试另存/放弃时释放）+ 保存路径（取消对话框为空串） */
export interface UploadResult {
  session: string;
  path: string;
}

/**
 * 上传视频并弹出原生另存为对话框。
 * @param goTools $GoTools 全局代理
 * @param blob 录制得到的视频
 * @param filename 建议文件名（含扩展名）
 * @param onProgress 分块进度回调
 * @returns 会话与保存路径；用户取消对话框时 path 为空字符串（会话保留，可用 savePromptRetry 重试）
 */
export async function uploadVideoBlob(
  goTools: any,
  blob: Blob,
  filename: string,
  onProgress?: (done: number, total: number) => void
): Promise<UploadResult> {
  const CHUNK = 2 * 1024 * 1024; // 每块 2MB 原始数据
  const total = Math.max(1, Math.ceil(blob.size / CHUNK));
  const session: string = await goTools.VideoSaveBegin(filename);
  try {
    for (let i = 0; i < total; i++) {
      const buf: ArrayBuffer = await blob.slice(i * CHUNK, (i + 1) * CHUNK).arrayBuffer();
      await goTools.VideoSaveChunk(session, arrayBufferToBase64(buf));
      if (onProgress) onProgress(i + 1, total);
    }
    const path: string = await goTools.VideoSavePrompt(session);
    return { session, path };
  } catch (e) {
    try { await goTools.VideoSaveAbort(session); } catch { /* ignore */ }
    throw e;
  }
}

/** 已有会话时重试原生另存为对话框 */
export async function savePromptRetry(goTools: any, session: string): Promise<string> {
  return await goTools.VideoSavePrompt(session);
}

/** 生成录制建议文件名（mp4 优先，回退 webm） */
export function recFilename(prefix: string, ext: string): string {
  const d = new Date();
  const p = (n: number) => String(n).padStart(2, "0");
  return `${prefix}-${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}-${p(d.getHours())}${p(d.getMinutes())}${p(d.getSeconds())}.${ext}`;
}

/** 选择录制编码：优先 MP4(H.264)，回退 WebM(vp9/vp8) */
export function pickMime(): string {
  const list = [
    "video/mp4;codecs=avc1.42E01E",
    "video/mp4;codecs=avc1.640028",
    "video/mp4;codecs=h264",
    "video/mp4",
    "video/webm;codecs=vp9",
    "video/webm;codecs=vp8",
    "video/webm",
  ];
  return list.find((m) => typeof MediaRecorder !== "undefined" && MediaRecorder.isTypeSupported(m)) || "";
}

/** mime -> 扩展名 */
export function mimeExt(mime: string): string {
  return mime.includes("mp4") ? "mp4" : "webm";
}
