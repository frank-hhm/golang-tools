<template>
  <a-row class="video-page" :gutter="6">
    <!-- 左侧固定 30%：录制设置 + 录制状态 + 结果（与 Redis/抓包工具页布局一致） -->
    <a-col :span="6" class="side-col">
      <a-card class="side-card" size="small">
        <template #title>录制设置</template>
        <template #extra>
          <a-tag size="small" :color="statusColor">{{ statusText }}</a-tag>
        </template>
        <a-form :model="st" layout="vertical" size="small">
          <a-form-item field="mode">
            <template #label>录制模式</template>
            <a-radio-group v-model="st.mode" direction="vertical" :disabled="isBusy" @change="onModeChange">
              <a-radio value="screen"><icon-desktop /> 屏幕</a-radio>
              <a-radio value="region"><icon-fullscreen /> 区域</a-radio>
              <a-radio value="camera"><icon-camera /> 摄像头</a-radio>
            </a-radio-group>
          </a-form-item>
          <a-form-item field="fps">
            <template #label>帧率</template>
            <a-select v-model="st.fps" :disabled="isBusy" :options="fpsOptions" />
          </a-form-item>
        </a-form>
      </a-card>

      <a-card class="side-card" size="small">
        <template #title>录制状态</template>
        <div class="stat-cards">
          <div class="stat-card">
            <div class="stat-circle stat-circle-status"><icon-record /></div>
            <div class="stat-info">
              <div class="stat-label">状态</div>
              <div class="stat-value">{{ statusText }}</div>
            </div>
          </div>
          <div class="stat-card">
            <div class="stat-circle stat-circle-time"><icon-clock-circle /></div>
            <div class="stat-info">
              <div class="stat-label">时长</div>
              <div class="stat-value">{{ fmtTime(displayedElapsed) }}</div>
            </div>
          </div>
          <div class="stat-card">
            <div class="stat-circle stat-circle-size"><icon-storage /></div>
            <div class="stat-info">
              <div class="stat-label">大小</div>
              <div class="stat-value">{{ st.size ? fmtSize(st.size) : "—" }}</div>
            </div>
          </div>
        </div>
      </a-card>

      <a-card v-if="st.status === 'stopped' && st.blobUrl" class="side-card" size="small">
        <template #title>结果</template>
        <a-space direction="vertical" fill :size="8">
          <a-button type="primary" long :loading="saving" @click="saveResult">
            <template #icon><icon-download /></template>另存为…
          </a-button>
          <a-button long @click="discardResult">清空</a-button>
        </a-space>
        <div v-if="st.savedPath" class="saved-box">
          <icon-check-circle-fill class="saved-ic" />
          <div class="saved-detail">
            <div class="saved-title">视频已保存（{{ st.savedExt }}）</div>
            <div class="saved-path" :title="st.savedPath">{{ st.savedPath }}</div>
          </div>
        </div>
      </a-card>
    </a-col>

    <!-- 右侧主区：操作 + 预览 + 提示 -->
    <a-col :span="18" class="main-col">
      <a-card class="main-panel">
        <template #title>
          <a-space :size="8">
            <span class="panel-title">预览</span>
            <a-tag v-if="st.status === 'recording' || st.status === 'paused'" color="red" class="rec-tag">
              <icon-record /> {{ st.status === "paused" ? "已暂停" : "录制中" }} {{ fmtTime(displayedElapsed) }}
            </a-tag>
            <a-tag v-else-if="st.status === 'delegated'" color="arcoblue">待开始 · 请在悬浮小窗点「开始」</a-tag>
          </a-space>
        </template>
        <template #extra>
          <a-space :size="8">
            <!-- 屏幕模式 -->
            <template v-if="st.mode === 'screen'">
              <a-button size="small" v-if="st.status === 'idle'" type="primary" status="success" @click="startScreen">
                <template #icon><icon-record /></template>开始录制
              </a-button>
            </template>

            <!-- 区域模式 -->
            <template v-else-if="st.mode === 'region'">
              <template v-if="st.status === 'idle'">
                <a-button size="small" type="primary" @click="beginRegion"><template
                    #icon><icon-camera /></template>框选区域开始录制</a-button>
              </template>
              <template v-else-if="st.status === 'selecting'">
                <a-button size="small" type="outline" @click="reshoot"><template
                    #icon><icon-refresh /></template>重新截屏</a-button>
                <a-button size="small" type="primary" status="success" :disabled="!st.rect" @click="startRegion">
                  确定
                </a-button>
                <a-button size="small" @click="cancelSelect">取消</a-button>
              </template>
            </template>

            <!-- 摄像头模式 -->
            <template v-else>
              <template v-if="st.status === 'idle' || st.status === 'stopped'">
                <a-button size="small" type="primary" @click="openCamera"><template
                    #icon><icon-play-arrow /></template>打开摄像头</a-button>
              </template>
              <template v-else-if="st.status === 'ready'">
                <a-button size="small" type="primary" status="danger" @click="startCamera"><template
                    #icon><icon-record /></template>开始录制</a-button>
                <a-button size="small" @click="stopSource"><template #icon><icon-close /></template>关闭摄像头</a-button>
              </template>
              <template v-else>
                <a-button size="small" v-if="st.status === 'recording'" type="outline" @click="pauseCamera"><template
                    #icon><icon-pause /></template>暂停</a-button>
                <a-button size="small" v-else type="outline" @click="resumeCamera"><template
                    #icon><icon-play-arrow /></template>继续</a-button>
                <a-button size="small" type="primary" status="danger" @click="stopCamera"><template
                    #icon><icon-stop /></template>停止</a-button>
              </template>
            </template>
          </a-space>
        </template>

        <div class="panel-body">
          <!-- 已交给悬浮小窗控制的提示 -->
          <a-alert v-if="st.status === 'delegated'" class="widget-alert" type="warning">
            已就绪：请在{{ st.mode === "region" ? "选区右下角" : "屏幕右下角" }}的悬浮小窗点击「开始」启动录制，进行中可查看时长并点「停止」；停止后自动返回本窗口，在预览区回放并「另存为…」保存。
          </a-alert>

          <!-- 预览主区：铺满剩余高度 -->
          <div ref="previewRef" class="preview-wrap">
            <!-- 区域：选区截图 + 拖拽框选 -->
            <div v-show="st.mode === 'region' && st.status === 'selecting'" ref="snapWrap" class="snap-wrap"
              @mousedown="onDown" @mousemove="onMove" @mouseup="onUp" @mouseleave="onUp" @click="onSnapClick">
              <img v-if="st.snapUrl" :src="st.snapUrl" class="snap-img" alt="region" draggable="false"
                :style="mediaStyle" />
              <div v-if="st.rect" class="sel-rect" :style="rectStyle"></div>
              <div v-if="!st.rect" class="sel-hint">按住鼠标拖动，框选要录制的区域</div>
              <div v-else class="sel-size">{{ st.srcW }} × {{ st.srcH }} 共享源像素 · 已选 {{ st.selPx?.w }} × {{ st.selPx?.h }} ·
                单击选区内开始录制</div>
              <div v-if="st.rect" class="sel-play-hint"><icon-play-circle /> 点击确认区域录制</div>
            </div>

            <!-- 摄像头：实时预览 / 结果回放 -->
            <video
              v-show="st.mode === 'camera' && (st.status === 'ready' || st.status === 'recording' || st.status === 'paused')"
              ref="videoEl" class="live" :style="mediaStyle" muted playsinline></video>
            <video v-show="st.mode === 'camera' && st.status === 'stopped'" :src="st.blobUrl" class="live"
              :style="mediaStyle" controls autoplay></video>

            <!-- 结果/说明 -->
            <div v-if="showInfo" class="info-pane">
              <a-empty v-if="st.mode === 'screen' && st.status === 'idle' && !st.savedPath"
                description="点击「开始录制」→ 在系统对话框选择共享整个屏幕 → 本窗口自动隐藏，屏幕右下角出现计时/停止小窗" />
              <a-empty v-else-if="st.mode === 'region' && st.status === 'idle' && !st.savedPath"
                description="先在系统对话框选择共享内容（建议整个屏幕），再拖动框选录制区域，单击选区即开始录制" />
              <!-- 停止后即可直接点击播放回放（保存前后均可） -->
              <video v-if="st.blobUrl" :src="st.blobUrl" class="playback" :style="mediaStyle" controls
                preload="metadata"></video>
            </div>
          </div>

          <div class="tip">
            提示：区域录制按「先选共享内容 → 再框选区域 → 单击选区（或点「开始录制」按钮）」进行，共享只选一次；屏幕录制选完共享后直接就绪。
            就绪后本窗口隐藏并弹出置顶悬浮小窗，在小窗点「开始」才真正启动录制（未开始前不计时长），进行中可暂停/停止，避免本窗口入镜导致画面「无限缩小」。
            停止录制后自动关闭小窗返回本窗口，可直接回放预览，点「另存为…」选择任意目录保存，优先输出 MP4（H.264），内核不支持时回退 WebM。
            区域小窗出现在所选区域右下角，全屏小窗出现在屏幕右下角。摄像头录制在本窗口内完成、不含音频。
          </div>
        </div>
      </a-card>
    </a-col>
  </a-row>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onMounted, onUnmounted, reactive, ref } from "vue";
import { Events } from "@wailsio/runtime";
import { mimeExt, pickMime, recFilename, savePromptRetry, uploadVideoBlob } from "@/utils/videorec";

// 提示统一走 Go 原生通道：完成/成功 → 系统通知中心（原生 Toast）；错误/异常 → Go 自绘 Message 小窗
function notifyDone(title: string, content: string) {
  try { GoTools?.ShowSystemNotification(title, content); } catch { /* ignore */ }
}
function notifyError(text: string) {
  try { GoTools?.ShowMessage("error", text); } catch { /* ignore */ }
}

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const videoEl = ref<HTMLVideoElement>();
const snapWrap = ref<HTMLDivElement>();

const st = reactive({
  mode: "screen", // screen | region | camera
  status: "idle", // idle | selecting | ready | recording | paused | stopped | delegated
  fps: 30,
  snapUrl: "",
  srcW: 0,
  srcH: 0,
  rect: null as null | { x: number; y: number; w: number; h: number }, // 显示像素（渲染）
  selPx: null as null | { x: number; y: number; w: number; h: number }, // 物理像素（选区）
  blobUrl: "",
  size: 0,
  savedPath: "",
  savedExt: "",
});

const fpsOptions = [15, 24, 30, 60].map((n) => ({ label: n + " fps", value: n }));
const camElapsed = ref(0);
const saving = ref(false);

// 摄像头录制相关（非响应式）
let camStream: MediaStream | null = null;
let camRecorder: MediaRecorder | null = null;
let camChunks: Blob[] = [];
let camTimer = 0;
let camSession = ""; // 另存取消后保留的后端会话，重试时免重传
let offSaved: any = null;
let dragOrigin: { x: number; y: number } | null = null;
let drawing = false;
let justDragged = false; // 框选松手后紧跟的 click 不触发「单击选区开始录制」

const isBusy = computed(() => st.status === "recording" || st.status === "paused" || st.status === "delegated");
const showInfo = computed(() => st.mode !== "camera" && !(st.mode === "region" && st.status === "selecting"));
const rectStyle = computed(() => {
  const r = st.rect;
  if (!r) return {};
  return { left: r.x + "px", top: r.y + "px", width: r.w + "px", height: r.h + "px" };
});

/** 预览区尺寸测量：媒体（快照/视频）按可用高度自适应，保证等比缩放不溢出 */
const previewRef = ref<HTMLElement | null>(null);
const mediaMaxH = ref(0);
const mediaStyle = computed(() => (mediaMaxH.value > 0 ? { maxHeight: mediaMaxH.value + "px" } : undefined));
let previewRO: ResizeObserver | null = null;
function measurePreview() {
  const el = previewRef.value;
  if (!el) return;
  mediaMaxH.value = Math.max(180, el.clientHeight - 4);
}

/** 屏幕/区域录制已进行时长（秒，随 1s 上报节拍刷新）；摄像头用 camElapsed */
const recTick = ref(0);
const displayedElapsed = computed(() => (st.mode === "camera" ? camElapsed.value : recTick.value));

/** 状态文案与标签颜色（左侧设置卡标签 / 状态卡片共用） */
const statusText = computed(() => {
  switch (st.status) {
    case "idle": return "未开始";
    case "selecting": return "框选中";
    case "ready": return "摄像头就绪";
    case "recording": return "录制中";
    case "paused": return "已暂停";
    case "stopped": return "已完成";
    case "delegated": return "待开始";
    default: return st.status;
  }
});
const statusColor = computed(() => {
  switch (st.status) {
    case "recording": return "red";
    case "paused": return "orange";
    case "stopped": return "green";
    case "selecting":
    case "ready":
    case "delegated": return "arcoblue";
    default: return "gray";
  }
});

function fmtTime(s: number) {
  const m = Math.floor(s / 60);
  return `${String(m).padStart(2, "0")}:${String(s % 60).padStart(2, "0")}`;
}
function fmtSize(b: number) {
  if (b < 1024) return b + " B";
  if (b < 1024 * 1024) return (b / 1024).toFixed(1) + " KB";
  return (b / 1024 / 1024).toFixed(1) + " MB";
}
function clamp(v: number, min: number, max: number) {
  return Math.max(min, Math.min(max, v));
}

function onModeChange() {
  if (st.mode === "camera") {
    if (st.status !== "idle" && st.status !== "ready" && st.status !== "recording" && st.status !== "paused") cleanupResult();
  } else {
    stopSource();
  }
  cleanupResult(); // 切模式时释放上一结果的回放地址
  st.size = 0;
  st.savedPath = "";
  st.savedExt = "";
}

/** 结果条「另存为…」：按当前模式分发 */
function saveResult() {
  if (st.mode === "camera") saveCamera();
  else saveUploaded();
}
/** 结果条「放弃」：释放数据与后端会话，回到初始 */
function discardResult() {
  if (st.mode === "camera") { discardCamera(); return; }
  releaseRecData(); // 含 cleanupResult
  st.status = "idle";
  st.savedPath = "";
  st.savedExt = "";
}

/* ============================ 屏幕/区域录制（本窗捕获 + 悬浮小窗控制） ============================ */

let recStream: MediaStream | null = null;
let recVideo: HTMLVideoElement | null = null;
let recCanvas: HTMLCanvasElement | null = null;
let recCanvasStream: MediaStream | null = null;
let recCropTimer = 0;
let recReportTimer = 0;
let recRecorder: MediaRecorder | null = null;
let recChunks: Blob[] = [];
let recBlob: Blob | null = null;
let recSession = "";
let recName = "";
let recFrac = { x: 0, y: 0, w: 0, h: 0 };
let recStartTs = 0;
let recPausedMs = 0;
let recPauseStart = 0; // >0 表示当前处于暂停
let recDiscarding = false;
let recActive = false;
let offCmd: any = null;

function recElapsedSec() {
  if (!recStartTs) return 0; // 尚未点「开始」
  let ms = Date.now() - recStartTs - recPausedMs;
  if (recPauseStart) ms -= Date.now() - recPauseStart;
  return Math.max(0, Math.floor(ms / 1000));
}
function reportRec(state: string, note = "") {
  try { GoTools?.VideoReport(state, recElapsedSec(), note); } catch { /* ignore */ }
}

async function waitVideoReady(v: HTMLVideoElement) {
  for (let i = 0; i < 80; i++) {
    if (v.videoWidth > 0 && v.videoHeight > 0) return;
    await new Promise((r) => setTimeout(r, 50));
  }
  throw new Error("未能获取捕获画面");
}

/** 确保已有共享流：首次会弹系统「选择共享内容」对话框，已选过则直接复用（不再弹） */
async function ensureDisplayStream() {
  if (recStream && recVideo && recVideo.videoWidth > 0) return;
  const md = navigator.mediaDevices as any;
  const disp: MediaStream = await md.getDisplayMedia({ video: { frameRate: { ideal: st.fps } }, audio: false });
  recStream = disp;
  disp.getVideoTracks()[0]?.addEventListener("ended", onRecTrackEnded);
  const v = document.createElement("video");
  v.muted = true;
  v.playsInline = true;
  v.srcObject = disp;
  await v.play();
  recVideo = v;
  await waitVideoReady(v);
}

/** 从共享流抓一帧作为选区快照（与录制像素空间一致） */
function captureSnapshotFrame() {
  const v = recVideo!;
  const w = v.videoWidth, h = v.videoHeight;
  const c = document.createElement("canvas");
  c.width = w;
  c.height = h;
  c.getContext("2d")!.drawImage(v, 0, 0, w, h);
  st.snapUrl = c.toDataURL("image/png");
  st.srcW = w;
  st.srcH = h;
  st.rect = null;
  st.selPx = null;
}

/** 区域模式第一步：先选择共享内容 → 再拖动框选区域 */
async function beginRegion() {
  if (typeof GoTools?.VideoOpenWidget !== "function") {
    notifyError("需在应用窗口中使用（后端方法不可用）");
    return;
  }
  try {
    await ensureDisplayStream();
    captureSnapshotFrame();
    st.status = "selecting";
  } catch (e: any) {
    stopRecSource();
    st.status = "idle";
    notifyError("未选择共享内容：" + (e?.message || e));
  }
}

/** 屏幕模式：选择共享内容 → 直接开始录制 */
async function startScreen() {
  if (typeof GoTools?.VideoOpenWidget !== "function") {
    notifyError("需在应用窗口中使用（后端方法不可用）");
    return;
  }
  try {
    await ensureDisplayStream();
  } catch (e: any) {
    stopRecSource();
    st.status = "idle";
    notifyError("未选择共享内容：" + (e?.message || e));
    return;
  }
  await launchRecorder("screen", { x: 0, y: 0, w: 0, h: 0 }, window.screen.width, window.screen.height);
}

/** 共享流就绪 → 备好录制器 → 打开悬浮小窗（未开始状态，Go 端同时隐藏本窗口），等小窗点「开始」才真启动 */
async function launchRecorder(mode: "screen" | "region", frac: { x: number; y: number; w: number; h: number }, anchorX: number, anchorY: number) {
  const v = recVideo!;
  // 清除上一次结果的回放/提示
  cleanupResult();
  st.savedPath = "";
  st.savedExt = "";
  try {
    let stream: MediaStream = recStream!;
    if (mode === "region") {
      const c = document.createElement("canvas");
      c.width = Math.max(2, Math.round(frac.w * v.videoWidth));
      c.height = Math.max(2, Math.round(frac.h * v.videoHeight));
      recCanvas = c;
      recFrac = frac;
      recCanvasStream = c.captureStream(0) as MediaStream;
      startCropInterval();
      stream = recCanvasStream;
    }
    const mime = pickMime();
    recRecorder = new MediaRecorder(stream, mime ? { mimeType: mime, videoBitsPerSecond: 8_000_000 } : undefined);
    recChunks = [];
    recRecorder.ondataavailable = (e) => { if (e.data && e.data.size > 0) recChunks.push(e.data); };
    recRecorder.onstop = finalizeCapture;
    recName = recFilename(mode === "region" ? "rec-region" : "rec-screen", mimeExt(mime || "video/mp4"));
    recSession = "";
    recBlob = null;
    recDiscarding = false;
    recStartTs = 0;
    recPausedMs = 0;
    recPauseStart = 0;
    await GoTools.VideoOpenWidget(mode, st.fps, frac.x, frac.y, frac.w, frac.h, Math.round(anchorX), Math.round(anchorY), recName);
    st.status = "delegated";
    st.snapUrl = "";
    st.rect = null;
    st.selPx = null;
    startRecReport();
    reportRec("ready"); // 小窗显示「未开始」，等用户点开始
  } catch (e: any) {
    stopRecSource();
    recActive = false;
    st.status = mode === "region" && st.snapUrl ? "selecting" : "idle";
    notifyError("启动录制失败：" + (e?.message || e));
  }
}

/** 小窗点「开始」→ 真正启动录制器 */
function startPendingRec() {
  if (recActive || !recRecorder || recRecorder.state !== "inactive") return;
  recStartTs = Date.now();
  recPausedMs = 0;
  recPauseStart = 0;
  try {
    recRecorder.start(1000);
  } catch (e: any) {
    stopRecSource();
    reportRec("closed");
    st.status = "idle";
    notifyError("开始录制失败：" + (e?.message || e));
    return;
  }
  recActive = true;
  reportRec("recording");
}

function startCropInterval() {
  stopCropInterval();
  recCropTimer = window.setInterval(() => {
    const vv = recVideo, c = recCanvas, cs = recCanvasStream;
    if (vv && c && cs && recFrac.w > 0) {
      const ctx = c.getContext("2d");
      if (ctx) {
        ctx.drawImage(
          vv,
          Math.round(recFrac.x * vv.videoWidth), Math.round(recFrac.y * vv.videoHeight),
          c.width, c.height,
          0, 0, c.width, c.height
        );
      }
      try { (cs.getVideoTracks()[0] as any)?.requestFrame?.(); } catch { /* ignore */ }
    }
  }, Math.max(1, Math.round(1000 / st.fps)));
}
function stopCropInterval() {
  if (recCropTimer) { clearInterval(recCropTimer); recCropTimer = 0; }
}

function startRecReport() {
  stopRecReport();
  recReportTimer = window.setInterval(() => {
    if (st.status !== "delegated") return;
    recTick.value = recElapsedSec(); // 驱动界面时长显示
    // 未开始阶段持续上报 ready，防小窗错过首次事件停在「启动中」
    if (!recActive) { reportRec("ready"); return; }
    if (recRecorder && recRecorder.state === "recording" && !recPauseStart) reportRec("recording");
  }, 1000);
}
function stopRecReport() {
  if (recReportTimer) { clearInterval(recReportTimer); recReportTimer = 0; }
}

function onRecTrackEnded() {
  if (recActive && recRecorder && recRecorder.state !== "inactive") {
    // 用户通过系统共享条停止共享 → 按「停止」处理（进入保存流程）
    recRecorder.stop();
  } else if (!recActive && st.status === "delegated") {
    // 未开始前共享结束 → 关闭小窗回到初始
    stopRecSource();
    reportRec("closed");
    st.status = "idle";
    notifyDone("共享已结束", "录制未启动，已返回初始状态");
  } else if (!recActive && st.status === "selecting") {
    // 框选期间共享结束 → 回到初始状态
    stopRecSource();
    st.snapUrl = "";
    st.rect = null;
    st.selPx = null;
    st.status = "idle";
    notifyDone("共享已结束", "区域框选已取消");
  }
}

function finalizeCapture() {
  stopRecReport();
  stopCropInterval();
  const mime = recRecorder?.mimeType || "video/mp4";
  recBlob = new Blob(recChunks, { type: mime });
  recChunks = [];
  stopRecSource();
  recActive = false;
  if (recDiscarding) {
    recDiscarding = false;
    try { GoTools?.VideoWidgetDone(""); } catch { /* ignore */ }
    st.status = "idle";
    return;
  }
  // 停止即回主窗口：关闭小窗，结果/说明里展示回放与另存操作
  cleanupResult();
  st.size = recBlob.size;
  if (recBlob.size > 0) st.blobUrl = URL.createObjectURL(recBlob);
  st.status = "stopped";
  try { GoTools?.VideoWidgetDone(""); } catch { /* ignore */ }
  notifyDone("视频录制完成", `时长 ${fmtTime(recElapsedSec())} · 大小 ${fmtSize(st.size)}，请在「结果/说明」中预览并点击「另存为…」保存。`);
}

async function saveUploaded() {
  if (!recBlob || recBlob.size === 0) { notifyError("录制数据为空"); return; }
  if (saving.value) return;
  saving.value = true;
  try {
    let path: string;
    if (recSession) {
      // 上次取消过对话框：无需重传，直接再次弹出另存为
      path = await savePromptRetry(GoTools, recSession);
    } else {
      const res = await uploadVideoBlob(GoTools, recBlob, recName);
      recSession = res.session;
      path = res.path;
    }
    if (path) {
      st.savedPath = path;
      st.savedExt = path.toLowerCase().endsWith(".webm") ? "webm" : "mp4";
      notifyDone("视频已保存", path);
    }
    // 空路径 = 取消了对话框，数据保留，可再次点「另存为…」
  } catch (e: any) {
    recSession = "";
    notifyError("保存失败：" + (e?.message || e));
  } finally {
    saving.value = false;
  }
}

function releaseRecData() {
  if (recSession) { try { GoTools.VideoSaveAbort(recSession); } catch { /* ignore */ } recSession = ""; }
  recBlob = null;
  cleanupResult(); // 释放回放地址
}

function stopRecSource() {
  stopCropInterval();
  stopRecReport();
  if (recStream) { recStream.getTracks().forEach((t) => t.stop()); recStream = null; }
  if (recCanvasStream) { recCanvasStream.getTracks().forEach((t) => t.stop()); recCanvasStream = null; }
  if (recVideo) { recVideo.srcObject = null; recVideo.pause(); recVideo = null; }
  recCanvas = null;
  recRecorder = null;
}

/* ============================ 区域选区 ============================ */

/** 重新从共享流抓一帧；若共享已失效则重新走「选择共享内容」流程 */
function reshoot() {
  if (recStream && recVideo && recVideo.videoWidth > 0) {
    captureSnapshotFrame();
  } else {
    beginRegion();
  }
}

function onDown(ev: MouseEvent) {
  if (st.status !== "selecting") return;
  const r = snapWrap.value!.getBoundingClientRect();
  dragOrigin = { x: clamp(ev.clientX - r.left, 0, r.width), y: clamp(ev.clientY - r.top, 0, r.height) };
  drawing = true;
  st.rect = { x: dragOrigin.x, y: dragOrigin.y, w: 0, h: 0 };
  st.selPx = null;
}
function onMove(ev: MouseEvent) {
  if (!drawing || !dragOrigin) return;
  const r = snapWrap.value!.getBoundingClientRect();
  const cx = clamp(ev.clientX - r.left, 0, r.width);
  const cy = clamp(ev.clientY - r.top, 0, r.height);
  st.rect = {
    x: Math.min(cx, dragOrigin.x),
    y: Math.min(cy, dragOrigin.y),
    w: Math.abs(cx - dragOrigin.x),
    h: Math.abs(cy - dragOrigin.y),
  };
}
function onUp() {
  if (!drawing) return;
  drawing = false;
  justDragged = true; // 松手后紧跟的 click 不触发单击开始
  if (st.rect && (st.rect.w < 6 || st.rect.h < 6)) { st.rect = null; st.selPx = null; return; }
  computeSel();
}

/** 单击已框选区域内部 → 开始录制（免点工具栏按钮） */
function onSnapClick(ev: MouseEvent) {
  if (justDragged) { justDragged = false; return; }
  if (st.status !== "selecting" || !st.rect || !snapWrap.value) return;
  const r = snapWrap.value.getBoundingClientRect();
  const x = ev.clientX - r.left;
  const y = ev.clientY - r.top;
  const s = st.rect;
  if (x >= s.x && x <= s.x + s.w && y >= s.y && y <= s.y + s.h) startRegion();
}

/** 把框选的显示像素换算成快照物理像素选区 */
function computeSel() {
  const wrap = snapWrap.value;
  if (!wrap || !st.rect || !st.srcW) return;
  const sx = st.srcW / wrap.clientWidth;
  const sy = st.srcH / wrap.clientHeight;
  st.selPx = {
    x: Math.round(st.rect.x * sx),
    y: Math.round(st.rect.y * sy),
    w: Math.max(2, Math.round(st.rect.w * sx)),
    h: Math.max(2, Math.round(st.rect.h * sy)),
  };
}

/** 区域模式第二步：框选完成后点开始录制（共享已选过，不再弹选择框） */
async function startRegion() {
  if (!st.rect) { notifyError("请先框选区域"); return; }
  if (!recStream || !recVideo || recVideo.videoWidth === 0) {
    notifyError("共享已失效，请重新框选");
    st.status = "idle";
    return;
  }
  computeSel();
  const sel = st.selPx!;
  const frac = { x: sel.x / st.srcW, y: sel.y / st.srcH, w: sel.w / st.srcW, h: sel.h / st.srcH };
  // 小窗锚点：选区右下角（DIP）
  const anchorX = (sel.x + sel.w) * (window.screen.width / st.srcW);
  const anchorY = (sel.y + sel.h) * (window.screen.height / st.srcH);
  await launchRecorder("region", frac, anchorX, anchorY);
}

function cancelSelect() {
  if (!recActive) stopRecSource(); // 结束已选共享
  st.snapUrl = "";
  st.rect = null;
  st.selPx = null;
  st.status = "idle";
}

/* ============================ 摄像头（本窗口录制） ============================ */

async function openCamera() {
  cleanupResult();
  try {
    camStream = await navigator.mediaDevices.getUserMedia({
      video: { width: { ideal: 1280 }, height: { ideal: 720 } },
      audio: false,
    });
    const v = videoEl.value!;
    v.srcObject = camStream;
    await v.play();
    st.status = "ready";
  } catch (e: any) {
    notifyError("打开摄像头失败：" + (e?.message || e));
    stopSource();
  }
}

function startCamera() {
  if (!camStream) return;
  camChunks = [];
  camSession = "";
  const mime = pickMime();
  try {
    camRecorder = new MediaRecorder(camStream, mime ? { mimeType: mime, videoBitsPerSecond: 6_000_000 } : undefined);
  } catch (e: any) {
    notifyError("初始化录制器失败：" + (e?.message || e));
    return;
  }
  camRecorder.ondataavailable = (e) => { if (e.data && e.data.size > 0) camChunks.push(e.data); };
  camRecorder.onstop = () => {
    stopCamTimer();
    const blob = new Blob(camChunks, { type: camRecorder?.mimeType || "video/mp4" });
    st.size = blob.size;
    cleanupResult();
    st.blobUrl = URL.createObjectURL(blob);
    st.status = "stopped";
    notifyDone("视频录制完成", `大小 ${fmtSize(st.size)}，请点击「另存为…」选择保存目录。`);
  };
  camRecorder.start(1000);
  camElapsed.value = 0;
  startCamTimer();
  st.status = "recording";
}
function pauseCamera() {
  if (camRecorder && camRecorder.state === "recording") { camRecorder.pause(); stopCamTimer(); st.status = "paused"; }
}
function resumeCamera() {
  if (camRecorder && camRecorder.state === "paused") { camRecorder.resume(); startCamTimer(); st.status = "recording"; }
}
function stopCamera() {
  if (camRecorder && camRecorder.state !== "inactive") camRecorder.stop();
}
function startCamTimer() {
  stopCamTimer();
  camTimer = window.setInterval(() => { camElapsed.value++; }, 1000);
}
function stopCamTimer() {
  if (camTimer) { clearInterval(camTimer); camTimer = 0; }
}

async function saveCamera() {
  if (!st.blobUrl || saving.value) return;
  saving.value = true;
  try {
    let path: string;
    if (camSession) {
      path = await savePromptRetry(GoTools, camSession);
    } else {
      const blob = await fetch(st.blobUrl).then((r) => r.blob());
      const name = recFilename("rec-camera", mimeExt(blob.type || ""));
      const res = await uploadVideoBlob(GoTools, blob, name);
      camSession = res.session;
      path = res.path;
    }
    if (path) {
      st.savedPath = path;
      st.savedExt = path.toLowerCase().endsWith(".webm") ? "webm" : "mp4";
      notifyDone("视频已保存", path);
    }
  } catch (e: any) {
    camSession = "";
    notifyError("保存失败：" + (e?.message || e));
  } finally {
    saving.value = false;
  }
}

function discardCamera() {
  if (camSession) { try { GoTools.VideoSaveAbort(camSession); } catch { /* ignore */ } camSession = ""; }
  cleanupResult();
  stopSource();
}

/* ============================ 停止 / 清理 ============================ */

function stopSource() {
  stopCamTimer();
  if (camRecorder && camRecorder.state !== "inactive") { try { camRecorder.stop(); } catch { /* ignore */ } }
  camRecorder = null;
  if (camStream) { camStream.getTracks().forEach((t) => t.stop()); camStream = null; }
  const v = videoEl.value;
  if (v) v.srcObject = null;
  cancelSelect();
  if (st.status !== "stopped" && st.status !== "delegated" && st.status !== "ready") st.status = "idle";
}

function cleanupResult() {
  if (st.blobUrl) { URL.revokeObjectURL(st.blobUrl); st.blobUrl = ""; }
  st.size = 0;
}

onMounted(() => {
  // 预览区尺寸测量：媒体按可用高度自适应（窗口缩放时同步刷新）
  previewRO = new ResizeObserver(measurePreview);
  if (previewRef.value) previewRO.observe(previewRef.value);
  measurePreview();
  // 小窗保存完成后推送：{ video:saved } 路径（空串=放弃）
  offSaved = Events.On("video:saved", (ev: any) => {
    const p = ev?.data ?? ev;
    if (st.status === "delegated") st.status = "idle";
    if (p) {
      st.savedPath = String(p);
      st.savedExt = String(p).toLowerCase().endsWith(".webm") ? "webm" : "mp4";
    }
  });
  // 悬浮小窗 → 本窗的录制控制命令
  offCmd = Events.On("video:cmd", (ev: any) => {
    const c = String(ev?.data ?? ev ?? "");
    if (c === "start") {
      startPendingRec();
    } else if (c === "pause") {
      if (recRecorder && recRecorder.state === "recording") { recRecorder.pause(); recPauseStart = Date.now(); reportRec("paused"); }
    } else if (c === "resume") {
      if (recRecorder && recRecorder.state === "paused") {
        recRecorder.resume();
        if (recPauseStart) { recPausedMs += Date.now() - recPauseStart; recPauseStart = 0; }
        reportRec("recording");
      }
    } else if (c === "stop") {
      if (recRecorder && recRecorder.state !== "inactive") recRecorder.stop();
    } else if (c === "cancel") {
      recDiscarding = true;
      if (recActive && recRecorder && recRecorder.state !== "inactive") {
        try { recRecorder.stop(); } catch { /* ignore */ } // finalizeCapture 会走放弃分支
      } else {
        stopRecSource(); // 未开始也直接释放共享流/裁剪循环
        releaseRecData();
        recDiscarding = false;
        try { GoTools?.VideoWidgetDone(""); } catch { /* ignore */ }
        st.status = "idle";
      }
    }
  });
});

onUnmounted(() => {
  if (previewRO) { previewRO.disconnect(); previewRO = null; }
  if (offSaved) { try { offSaved(); } catch { /* ignore */ } }
  if (offCmd) { try { offCmd(); } catch { /* ignore */ } }
  stopRecSource();
  stopSource();
  cleanupResult();
});
</script>

<style scoped>
/* 页面根：两列栅格铺满，与 Redis 等工具页布局一致 */
.video-page {
  height: 100%;
  min-height: 0;
  flex-wrap: nowrap;
  background: var(--color-fill-1);
}

/* 左列：录制设置 / 录制状态 / 结果，内容不足时自然堆叠，超出可滚动 */
.side-col {
  height: 100%;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
  overflow-y: auto;
}

.side-card {
  flex-shrink: 0;
}

/* 右列：主面板卡片撑满高度 */
.main-col {
  height: 100%;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.main-panel {
  width: 100%;
  height: 100%;
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  background: var(--color-bg-2);
}

.main-panel :deep(.arco-card-body) {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding: var(--base-padding);
}

.panel-title {
  font-weight: 600;
}

/* 主面板内容：提示条 + 预览区 + 底部说明，纵向弹性填充 */
.panel-body {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: var(--base-padding);
}

.widget-alert {
  flex: none;
}

.rec-tag {
  font-variant-numeric: tabular-nums;
}

/* 状态指标：彩色圆角图标 + 标题 + 大数值（与 Redis 等工具页统一风格） */
.stat-cards {
  display: flex;
  flex-direction: column;
  gap: var(--base-padding);
}

.stat-card {
  display: flex;
  align-items: center;
  gap: var(--base-padding);
  padding: var(--base-padding);
  background: var(--color-bg-1);
  border: 1px solid var(--color-border-1);
  border-radius: var(--base-radius-default);
  transition: box-shadow 0.2s;
}

.stat-card:hover {
  box-shadow: var(--shadow-2-bottom);
}

.stat-circle {
  width: 44px;
  height: 44px;
  border-radius: var(--base-radius-default);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: var(--base-size-large-x);
  flex-shrink: 0;
}

.stat-circle-status {
  background: rgba(var(--red-6), 0.12);
  color: rgb(var(--red-6));
}

.stat-circle-time {
  background: rgba(var(--arcoblue-6), 0.12);
  color: rgb(var(--arcoblue-6));
}

.stat-circle-size {
  background: rgba(var(--purple-6), 0.12);
  color: rgb(var(--purple-6));
}

.stat-info {
  flex: 1;
  min-width: 0;
}

.stat-label {
  font-size: var(--base-size-small);
  color: var(--color-text-3);
}

.stat-value {
  font-weight: bold;
  font-size: var(--base-size-default);
  line-height: 1.2;
  color: var(--color-text-1);
  margin-top: var(--base-margin);
}

.preview-wrap {
  flex: 1;
  min-height: 0;
  border: 1px solid var(--color-border-2);
  border-radius: var(--base-radius-large);
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--color-fill-1);
  overflow: hidden;
  position: relative;
}

.live {
  max-width: 100%;
  max-height: 100%;
  background: #000;
  border-radius: var(--base-radius-default);
}

.snap-wrap {
  position: relative;
  max-width: 100%;
  cursor: crosshair;
  user-select: none;
}

.snap-img {
  max-width: 100%;
  display: block;
}

.sel-rect {
  position: absolute;
  border: 2px dashed rgb(var(--red-6));
  background: rgba(245, 63, 63, 0.12);
  pointer-events: none;
}

.sel-hint {
  position: absolute;
  top: 10px;
  left: 10px;
  background: rgba(0, 0, 0, 0.55);
  color: #fff;
  padding: var(--base-radius-default) var(--base-radius-large);
  border-radius: var(--base-radius-default);
  font-size: var(--base-size-small);
}

.sel-size {
  position: absolute;
  top: 10px;
  right: 10px;
  background: rgba(0, 0, 0, 0.55);
  color: #fff;
  padding: var(--base-radius-default) var(--base-radius-large);
  border-radius: var(--base-radius-default);
  font-size: var(--base-size-small);
}

.sel-play-hint {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  background: rgba(0, 0, 0, 0.6);
  color: #fff;
  padding: var(--base-radius-default) var(--base-padding);
  border-radius: var(--base-radius-large-x);
  font-size: var(--base-size-small);
  pointer-events: none;
  display: flex;
  align-items: center;
  gap: 6px;
}

.info-pane {
  max-height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 16px;
  overflow: auto;
}

.playback {
  width: 640px;
  max-width: 100%;
  background: #000;
  border-radius: var(--base-radius-default);
}

.saved-box {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  margin-top: var(--base-radius-large);
  background: var(--color-success-light-1);
  border: 1px solid rgb(var(--green-3));
  border-radius: var(--base-radius-default);
  padding: var(--base-radius-large) var(--base-padding);
}

.saved-detail {
  flex: 1;
  min-width: 0;
}

.saved-ic {
  color: rgb(var(--green-6));
  margin-top: var(--base-radius-small);
}

.saved-title {
  font-size: var(--base-size-small);
  font-weight: 600;
}

.saved-path {
  font-size: var(--base-size-small);
  color: var(--color-text-2);
  word-break: break-all;
}

.tip {
  flex: none;
  color: var(--color-text-3);
  font-size: var(--base-size-small);
  line-height: 1.6;
}
</style>
