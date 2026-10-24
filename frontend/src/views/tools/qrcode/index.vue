<template>
    <a-row class="qr-page" :gutter="6">
        <!-- 左侧约 25%：参数设置 -->
        <a-col :span="6" class="side-col">
            <a-card class="side-card" size="small" :body-style="{ padding: 0 }">
                <template #title>参数设置</template>
                <template #extra>
                    <a-tag size="small">{{ qrMode === "normal" ? "普通" : "透明" }}</a-tag>
                </template>
                <div class="side-body">
                    <!-- 文本 -->
                    <div class="field">
                        <div class="field-label">文本</div>
                        <a-textarea v-model="text" :auto-size="{ minRows: 4, maxRows: 8 }" placeholder="请输入文字"
                            allow-clear />
                        <div class="quick-bar">
                            <a-button size="mini" type="text" @click="vcardVisible = true">电子名片</a-button>
                            <a-button size="mini" type="text" @click="phoneVisible = true">手机号</a-button>
                            <a-button size="mini" type="text" @click="emailVisible = true">email</a-button>
                            <a-button size="mini" type="text" @click="wechatVisible = true">微信公众号</a-button>
                            <a-button size="mini" type="text" @click="wifiVisible = true">WIFI</a-button>
                            <a-button size="mini" type="text" @click="decodeVisible = true">
                                <template #icon><icon-upload /></template>传图解码
                            </a-button>
                        </div>
                    </div>

                    <!-- 通用：容错级别 -->
                    <div class="field">
                        <div class="field-label">容错级别</div>
                        <a-select v-model="ecLevel" size="small" style="width: 100%">
                            <a-option value="L">L · 7%</a-option>
                            <a-option value="M">M · 15%</a-option>
                            <a-option value="Q">Q · 25%</a-option>
                            <a-option value="H">H · 30%</a-option>
                        </a-select>
                    </div>

                    <!-- 普通二维码参数 -->
                    <template v-if="qrMode === 'normal'">
                        <a-row :gutter="8">
                            <a-col :span="12">
                                <div class="field">
                                    <div class="field-label">大小 (px)</div>
                                    <a-input-number v-model="size" :min="60" :max="1024" :step="10" size="small"
                                        hide-button style="width: 100%" />
                                </div>
                            </a-col>
                            <a-col :span="12">
                                <div class="field">
                                    <div class="field-label">外边距 (px)</div>
                                    <a-input-number v-model="margin" :min="0" :max="100" size="small" hide-button
                                        style="width: 100%" />
                                </div>
                            </a-col>
                        </a-row>
                        <a-row :gutter="8">
                            <a-col :span="12">
                                <div class="field">
                                    <div class="field-label">前景色</div>
                                    <a-color-picker v-model="fgColor" size="small" :show-alpha="false" />
                                </div>
                            </a-col>
                            <a-col :span="12">
                                <div class="field">
                                    <div class="field-label">背景色</div>
                                    <a-color-picker v-model="bgColor" size="small" :show-alpha="false" />
                                </div>
                            </a-col>
                        </a-row>
                        <div class="field">
                            <div class="field-label">Logo</div>
                            <div class="logo-row">
                                <a-button size="small" @click="pickLogo">
                                    <template #icon><icon-upload /></template>{{ logoData ? "重新上传" : "上传 Logo" }}
                                </a-button>
                                <a-button v-if="logoData" size="small" status="danger" type="text"
                                    @click="logoData = ''">移除</a-button>
                                <img v-if="logoData" :src="logoData" class="logo-thumb" alt="logo" />
                            </div>
                            <input ref="logoInput" type="file" accept="image/*" style="display: none"
                                @change="onLogoChange" />
                        </div>
                        <a-button type="primary" status="success" long class="gen-btn" @click="renderNormal">
                            <template #icon><icon-qrcode /></template>生成二维码
                        </a-button>
                    </template>

                    <!-- 透明二维码参数 -->
                    <template v-else>
                        <a-row :gutter="8">
                            <a-col :span="12">
                                <div class="field">
                                    <div class="field-label">二维码大小</div>
                                    <a-input-number v-model="tQrSize" :min="40" :max="300" size="small" hide-button
                                        style="width: 100%" />
                                </div>
                            </a-col>
                            <a-col :span="12">
                                <div class="field">
                                    <div class="field-label">颜色</div>
                                    <a-color-picker v-model="tFgColor" size="small" :show-alpha="false" />
                                </div>
                            </a-col>
                        </a-row>
                        <div class="field">
                            <div class="field-label">背景图片</div>
                            <a-button size="small" long @click="pickBg">
                                <template #icon><icon-upload /></template>{{ bgImage ? "重新上传背景" : "上传背景图片" }}
                            </a-button>
                            <input ref="bgInput" type="file" accept="image/*" style="display: none"
                                @change="onBgChange" />
                        </div>
                        <a-button v-if="bgImage" type="primary" status="success" long class="gen-btn"
                            @click="renderTransparent">
                            <template #icon><icon-qrcode /></template>生成二维码
                        </a-button>
                    </template>
                </div>
            </a-card>
        </a-col>

        <!-- 右侧：二维码预览 -->
        <a-col :span="18" class="main-col">
            <a-card class="main-panel" :body-style="{ padding: 0 }">
                <template #title>
                    <a-space :size="8">
                        <span>二维码预览</span>
                        <a-tag size="small">{{ qrMode === "normal" ? "普通二维码" : "透明二维码" }}</a-tag>
                    </a-space>
                </template>
                <template #extra>
                    <a-radio-group v-model="qrMode" type="button" size="small">
                        <a-radio value="normal">普通</a-radio>
                        <a-radio value="transparent">透明</a-radio>
                    </a-radio-group>
                </template>

                <!-- 普通二维码 -->
                <div v-show="qrMode === 'normal'" class="preview-body">
                    <div class="qr-stage">
                        <canvas v-show="normalHasResult" ref="normalCanvas" class="qr-canvas"></canvas>
                        <a-empty v-if="!normalHasResult" description="输入内容后点击左侧「生成二维码」" />
                    </div>
                    <div class="preview-foot">
                        <span class="tip">生成结果可下载保存为 PNG</span>
                        <a-button v-if="normalHasResult" size="small" @click="downloadNormal">
                            <template #icon><icon-download /></template>下载 PNG
                        </a-button>
                    </div>
                </div>

                <!-- 透明二维码 -->
                <div v-show="qrMode === 'transparent'" class="preview-body">
                    <div v-if="!bgImage" class="qr-stage">
                        <a-empty description="请先在左侧上传背景图片" />
                    </div>
                    <a-row v-else :gutter="6" class="stage-row">
                        <a-col :span="12" class="stage-col">
                            <div class="stage-pane">
                                <div class="pane-title">拖拽调整透明二维码位置</div>
                                <div class="stage-scroll">
                                    <div class="stage"
                                        :style="{ width: STAGE_W + 'px', height: stageH + 'px' }">
                                        <img :src="bgImage" class="stage-bg" alt="bg" />
                                        <img v-if="tQrPreview" :src="tQrPreview" class="stage-qr"
                                            :style="{ left: pos.x + 'px', top: pos.y + 'px', width: tQrSize + 'px', height: tQrSize + 'px' }"
                                            @pointerdown="startDrag" alt="qr" />
                                    </div>
                                </div>
                            </div>
                        </a-col>
                        <a-col :span="12" class="stage-col">
                            <div class="stage-pane">
                                <div class="pane-title">生成结果（右键另存）</div>
                                <div class="result-scroll">
                                    <a-empty v-if="!tResult" description="点击左侧「生成二维码」" />
                                    <img v-else :src="tResult" class="result-img" alt="result" />
                                </div>
                                <a-button v-if="tResult" size="small"
                                    @click="downloadDataUrl(tResult, 'qrcode-transparent')">
                                    <template #icon><icon-download /></template>下载 PNG
                                </a-button>
                            </div>
                        </a-col>
                    </a-row>
                </div>
            </a-card>
        </a-col>
    </a-row>

    <!-- ============ 快捷填充弹窗 ============ -->
    <a-modal v-model:visible="vcardVisible" title="电子名片" @ok="genVcard" ok-text="确定">
        <a-form :model="vcard" layout="vertical">
            <a-form-item field="name" label="姓名"><a-input v-model="vcard.name" /></a-form-item>
            <a-form-item field="phone" label="电话"><a-input v-model="vcard.phone" /></a-form-item>
            <a-form-item field="email" label="邮箱"><a-input v-model="vcard.email" /></a-form-item>
            <a-form-item field="org" label="公司"><a-input v-model="vcard.org" /></a-form-item>
            <a-form-item field="url" label="网址"><a-input v-model="vcard.url" /></a-form-item>
        </a-form>
    </a-modal>

    <a-modal v-model:visible="phoneVisible" title="手机号" @ok="genPhone" ok-text="确定">
        <a-form :model="phone" layout="vertical">
            <a-form-item field="type" label="类型">
                <a-radio-group v-model="phone.type">
                    <a-radio value="tel">拨打电话</a-radio>
                    <a-radio value="smsto">发送短信</a-radio>
                </a-radio-group>
            </a-form-item>
            <a-form-item field="number" label="号码"><a-input v-model="phone.number" /></a-form-item>
        </a-form>
    </a-modal>

    <a-modal v-model:visible="emailVisible" title="Email" @ok="genEmail" ok-text="确定">
        <a-form :model="email" layout="vertical">
            <a-form-item field="to" label="收件人"><a-input v-model="email.to" /></a-form-item>
            <a-form-item field="subject" label="主题"><a-input v-model="email.subject" /></a-form-item>
            <a-form-item field="body" label="正文"><a-textarea v-model="email.body" :auto-size="{ minRows: 2 }" /></a-form-item>
        </a-form>
    </a-modal>

    <a-modal v-model:visible="wechatVisible" title="微信公众号" @ok="genWechat" ok-text="确定">
        <a-form :model="wechat" layout="vertical">
            <a-form-item field="url" label="公众号链接 / 文本">
                <a-input v-model="wechat.url" placeholder="https://mp.weixin.qq.com/..." />
            </a-form-item>
        </a-form>
    </a-modal>

    <a-modal v-model:visible="wifiVisible" title="WIFI" @ok="genWifi" ok-text="确定">
        <a-form :model="wifi" layout="vertical">
            <a-form-item field="ssid" label="WIFI 名称">
                <a-input v-model="wifi.ssid" placeholder="SSID" />
                <a-checkbox v-model="wifi.hidden" style="margin-left: 8px">隐藏</a-checkbox>
            </a-form-item>
            <a-form-item field="enc" label="加密方式">
                <a-radio-group v-model="wifi.enc">
                    <a-radio value="nopass">无密码</a-radio>
                    <a-radio value="WPA">WPA加密</a-radio>
                    <a-radio value="WPA2">WPA2加密</a-radio>
                    <a-radio value="WEP">WEP加密</a-radio>
                </a-radio-group>
            </a-form-item>
            <a-form-item v-if="wifi.enc !== 'nopass'" field="password" label="WIFI 密码">
                <a-input v-model="wifi.password" placeholder="请输入WIFI密码" />
            </a-form-item>
        </a-form>
    </a-modal>

    <a-modal v-model:visible="decodeVisible" title="传图解码" :footer="false">
        <div class="tip" style="margin-bottom: 8px">选择包含二维码的图片，自动识别其中的内容。</div>
        <input ref="decodeInput" type="file" accept="image/*" @change="onDecode" />
        <div v-if="decodeMsg" :class="['decode-msg', decodeOk ? 'ok' : 'err']">{{ decodeMsg }}</div>
    </a-modal>
</template>

<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import qrcode from "qrcode-generator";
import jsQR from "jsqr";

// 中文/UTF-8 支持：默认实现按 latin1 截断，需覆盖为 UTF-8
// 注意：Vite 解析到 dist/qrcode.mjs，其未提供 stringToBytesFuncs，故自行实现
qrcode.stringToBytes = (s: string): number[] => {
    const bytes: number[] = [];
    new TextEncoder().encode(s).forEach((b) => bytes.push(b));
    return bytes;
};

/* ============ 共享状态 ============ */
const text = ref("");
const ecLevel = ref<"L" | "M" | "Q" | "H">("M");
/** 子视图：普通 / 透明 */
const qrMode = ref("normal");

/* ============ 普通二维码 ============ */
const size = ref(250);
const margin = ref(0);
const fgColor = ref("#000000");
const bgColor = ref("#ffffff");
const logoData = ref("");
const logoInput = ref<HTMLInputElement | null>(null);
const normalCanvas = ref<HTMLCanvasElement | null>(null);
const normalHasResult = ref(false);

const pickLogo = () => logoInput.value?.click();
const onLogoChange = (e: Event) => {
    const f = (e.target as HTMLInputElement).files?.[0];
    if (f) readAsDataUrl(f, (d) => (logoData.value = d));
    (e.target as HTMLInputElement).value = "";
};

/** 构建二维码模块矩阵 */
function buildQr(value: string, ec: "L" | "M" | "Q" | "H") {
    const qr = qrcode(0, ec);
    qr.addData(value, "Byte");
    qr.make();
    return { qr, count: qr.getModuleCount() };
}

const renderNormal = async () => {
    if (!text.value) {
        Message.warning("请输入内容");
        return;
    }
    let built: ReturnType<typeof buildQr>;
    try {
        built = buildQr(text.value, ecLevel.value);
    } catch {
        Message.error("内容过长，无法生成二维码");
        return;
    }
    const { qr, count } = built;
    const cell = Math.max(1, Math.floor(size.value / count));
    const m = Math.max(0, margin.value);
    const dim = cell * count + m * 2;
    normalHasResult.value = true;
    await nextTick();
    const canvas = normalCanvas.value;
    if (!canvas) return;
    canvas.width = dim;
    canvas.height = dim;
    const ctx = canvas.getContext("2d")!;
    ctx.fillStyle = bgColor.value || "#ffffff";
    ctx.fillRect(0, 0, dim, dim);
    ctx.fillStyle = fgColor.value || "#000000";
    for (let r = 0; r < count; r++) {
        for (let c = 0; c < count; c++) {
            if (qr.isDark(r, c)) ctx.fillRect(m + c * cell, m + r * cell, cell, cell);
        }
    }
    if (logoData.value) {
        const img = new Image();
        img.onload = () => {
            const ls = Math.floor(dim * 0.22);
            const lx = (dim - ls) / 2;
            const pad = Math.floor(ls * 0.14);
            ctx.fillStyle = bgColor.value || "#ffffff";
            ctx.fillRect(lx - pad, lx - pad, ls + pad * 2, ls + pad * 2);
            ctx.drawImage(img, lx, lx, ls, ls);
        };
        img.src = logoData.value;
    }
};

const downloadNormal = () => {
    if (!normalCanvas.value) return;
    downloadDataUrl(normalCanvas.value.toDataURL("image/png"), "qrcode");
};

/* ============ 透明二维码 ============ */
const STAGE_W = 320;
const tQrSize = ref(150);
const tFgColor = ref("#000000");
const bgImage = ref("");
const bgInput = ref<HTMLInputElement | null>(null);
const bgNat = reactive({ w: 0, h: 0 });
const pos = reactive({ x: 60, y: 60 });
const tQrPreview = ref("");
const tResult = ref("");

const stageH = computed(() => (bgNat.w ? Math.round((STAGE_W * bgNat.h) / bgNat.w) : STAGE_W));

const pickBg = () => bgInput.value?.click();
const onBgChange = (e: Event) => {
    const f = (e.target as HTMLInputElement).files?.[0];
    if (!f) return;
    readAsDataUrl(f, (d) => {
        bgImage.value = d;
        const img = new Image();
        img.onload = () => {
            bgNat.w = img.naturalWidth;
            bgNat.h = img.naturalHeight;
            pos.x = Math.max(0, Math.round((STAGE_W - tQrSize.value) / 2));
            pos.y = Math.max(0, Math.round((stageH.value - tQrSize.value) / 2));
        };
        img.src = d;
    });
    (e.target as HTMLInputElement).value = "";
};

/** 生成透明背景的二维码画布（仅深色模块着色） */
function buildTransparentCanvas(targetPx: number): HTMLCanvasElement | null {
    if (!text.value) return null;
    let built: ReturnType<typeof buildQr>;
    try {
        built = buildQr(text.value, ecLevel.value);
    } catch {
        return null;
    }
    const { qr, count } = built;
    const cell = Math.max(2, Math.floor(targetPx / count));
    const dim = cell * count;
    const canvas = document.createElement("canvas");
    canvas.width = dim;
    canvas.height = dim;
    const ctx = canvas.getContext("2d")!;
    ctx.clearRect(0, 0, dim, dim);
    ctx.fillStyle = tFgColor.value || "#000000";
    for (let r = 0; r < count; r++) {
        for (let c = 0; c < count; c++) {
            if (qr.isDark(r, c)) ctx.fillRect(c * cell, r * cell, cell, cell);
        }
    }
    return canvas;
}

// 实时刷新舞台上的透明二维码预览
const refreshPreview = () => {
    const c = buildTransparentCanvas(tQrSize.value);
    tQrPreview.value = c ? c.toDataURL("image/png") : "";
};
watch([text, ecLevel, tFgColor, tQrSize], refreshPreview);

const renderTransparent = () => {
    if (!bgImage.value) {
        Message.warning("请先上传背景图片");
        return;
    }
    if (!text.value) {
        Message.warning("请输入内容");
        return;
    }
    const scale = bgNat.w / STAGE_W;
    const qrCanvas = buildTransparentCanvas(tQrSize.value * scale);
    if (!qrCanvas) {
        Message.error("内容过长，无法生成二维码");
        return;
    }
    const img = new Image();
    img.onload = () => {
        const out = document.createElement("canvas");
        out.width = bgNat.w;
        out.height = bgNat.h;
        const ctx = out.getContext("2d")!;
        ctx.drawImage(img, 0, 0, bgNat.w, bgNat.h);
        ctx.drawImage(qrCanvas, pos.x * scale, pos.y * scale, tQrSize.value * scale, tQrSize.value * scale);
        tResult.value = out.toDataURL("image/png");
        Message.success("已生成");
    };
    img.src = bgImage.value;
};

/* 拖拽定位 */
let dragging = false;
let startX = 0;
let startY = 0;
let originX = 0;
let originY = 0;
const startDrag = (e: PointerEvent) => {
    dragging = true;
    startX = e.clientX;
    startY = e.clientY;
    originX = pos.x;
    originY = pos.y;
    window.addEventListener("pointermove", onDragMove);
    window.addEventListener("pointerup", endDrag);
    e.preventDefault();
};
const onDragMove = (e: PointerEvent) => {
    if (!dragging) return;
    const nx = originX + (e.clientX - startX);
    const ny = originY + (e.clientY - startY);
    pos.x = Math.min(Math.max(0, nx), STAGE_W - tQrSize.value);
    pos.y = Math.min(Math.max(0, ny), stageH.value - tQrSize.value);
};
const endDrag = () => {
    dragging = false;
    window.removeEventListener("pointermove", onDragMove);
    window.removeEventListener("pointerup", endDrag);
};

/* ============ 快捷填充弹窗 ============ */
const vcardVisible = ref(false);
const vcard = reactive({ name: "", phone: "", email: "", org: "", url: "" });
const genVcard = () => {
    const p: string[] = [];
    if (vcard.name) p.push(`N:${vcard.name}`);
    if (vcard.phone) p.push(`TEL:${vcard.phone}`);
    if (vcard.email) p.push(`EMAIL:${vcard.email}`);
    if (vcard.org) p.push(`ORG:${vcard.org}`);
    if (vcard.url) p.push(`URL:${vcard.url}`);
    apply(`MECARD:${p.join(";")};;`);
};

const phoneVisible = ref(false);
const phone = reactive({ type: "tel" as "tel" | "smsto", number: "" });
const genPhone = () => {
    if (!phone.number) return Message.warning("请输入号码");
    apply(`${phone.type}:${phone.number}`);
};

const emailVisible = ref(false);
const email = reactive({ to: "", subject: "", body: "" });
const genEmail = () => {
    if (!email.to) return Message.warning("请输入收件人");
    let s = `mailto:${email.to}`;
    const q: string[] = [];
    if (email.subject) q.push(`subject=${encodeURIComponent(email.subject)}`);
    if (email.body) q.push(`body=${encodeURIComponent(email.body)}`);
    if (q.length) s += "?" + q.join("&");
    apply(s);
};

const wechatVisible = ref(false);
const wechat = reactive({ url: "" });
const genWechat = () => {
    if (!wechat.url) return Message.warning("请输入链接或文本");
    apply(wechat.url);
};

const wifiVisible = ref(false);
const wifi = reactive({ ssid: "", hidden: false, enc: "WPA" as "nopass" | "WPA" | "WPA2" | "WEP", password: "" });
const genWifi = () => {
    if (!wifi.ssid) return Message.warning("请输入 WIFI 名称");
    const type = wifi.enc === "WPA2" ? "WPA" : wifi.enc;
    let s = `WIFI:T:${type};`;
    if (type !== "nopass" && wifi.password) s += `P:${wifi.password};`;
    s += `S:${wifi.ssid};`;
    if (wifi.hidden) s += "H:true;";
    apply(s + ";");
};

const apply = (s: string) => {
    text.value = s;
    vcardVisible.value = phoneVisible.value = emailVisible.value = wechatVisible.value = wifiVisible.value = false;
    Message.success("已填入文本框");
};

/* ============ 传图解码 ============ */
const decodeVisible = ref(false);
const decodeInput = ref<HTMLInputElement | null>(null);
const decodeMsg = ref("");
const decodeOk = ref(false);
const onDecode = (e: Event) => {
    const f = (e.target as HTMLInputElement).files?.[0];
    if (!f) return;
    decodeMsg.value = "";
    readAsDataUrl(f, (d) => {
        const img = new Image();
        img.onload = () => {
            const canvas = document.createElement("canvas");
            canvas.width = img.naturalWidth;
            canvas.height = img.naturalHeight;
            const ctx = canvas.getContext("2d")!;
            ctx.drawImage(img, 0, 0);
            const data = ctx.getImageData(0, 0, canvas.width, canvas.height);
            const code = jsQR(data.data, data.width, data.height);
            if (code && code.data) {
                decodeOk.value = true;
                decodeMsg.value = "识别成功，已填入文本框";
                text.value = code.data;
                setTimeout(() => (decodeVisible.value = false), 600);
            } else {
                decodeOk.value = false;
                decodeMsg.value = "未识别到二维码，请换一张清晰的图片";
            }
        };
        img.src = d;
    });
    (e.target as HTMLInputElement).value = "";
};

/* ============ 通用工具 ============ */
function readAsDataUrl(file: File, cb: (dataUrl: string) => void) {
    const reader = new FileReader();
    reader.onload = () => cb(String(reader.result || ""));
    reader.onerror = () => Message.error("读取文件失败");
    reader.readAsDataURL(file);
}

function downloadDataUrl(dataUrl: string, name: string) {
    const a = document.createElement("a");
    a.href = dataUrl;
    a.download = `${name}-${Date.now()}.png`;
    a.click();
}
</script>

<style scoped>
/* 页面根容器：a-row 铺满工具窗口内容区 */
.qr-page {
    height: 100%;
    min-height: 0;
    flex-wrap: nowrap;
}

/* 左侧参数卡片 */
.side-col {
    height: 100%;
    min-width: 0;
    display: flex;
    flex-direction: column;
}

.side-card {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

.side-card :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

.side-body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: var(--base-padding);
    padding: var(--base-padding);
    overflow-y: auto;
}

.field {
    display: flex;
    flex-direction: column;
    gap: 6px;
}

.field-label {
    color: var(--color-text-2);
    font-size: var(--base-size-small);
    font-weight: 500;
}

.quick-bar {
    display: flex;
    flex-wrap: wrap;
    gap: 2px;
    padding: 2px 6px;
    background: var(--color-fill-2);
    border-radius: var(--base-radius-default);
}

.logo-row {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
}

.logo-thumb {
    width: 28px;
    height: 28px;
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-small);
}

.gen-btn {
    margin-top: auto;
}

/* 右侧主列 */
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
}

.preview-body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: var(--base-padding);
}

/* 普通二维码预览：棋盘格背景，画布等比缩放 */
.qr-stage {
    flex: 1;
    min-height: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: var(--base-padding);
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-default);
    background: repeating-conic-gradient(var(--color-fill-2) 0% 25%, var(--color-bg-2) 0% 50%) 50% / 20px 20px;
    overflow: hidden;
}

.qr-canvas {
    max-width: 100%;
    max-height: 100%;
}

.preview-foot {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
}

.tip {
    color: var(--color-text-3);
    font-size: var(--base-size-small);
    margin: 0;
}

/* 透明二维码：左右两块面板 */
.stage-row {
    flex: 1;
    min-height: 0;
    flex-wrap: nowrap;
}

.stage-col {
    height: 100%;
    min-width: 0;
}

.stage-pane {
    height: 100%;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: var(--base-padding);
    background: var(--color-fill-1);
    border-radius: var(--base-radius-large);
}

.pane-title {
    flex: none;
    color: var(--color-text-2);
    font-size: var(--base-size-small);
    font-weight: 500;
}

.stage-scroll {
    flex: 1;
    min-height: 0;
    display: flex;
    align-items: flex-start;
    justify-content: center;
    overflow: auto;
}

.stage {
    position: relative;
    flex: none;
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-large);
    overflow: hidden;
}

.stage-bg {
    width: 100%;
    display: block;
}

.stage-qr {
    position: absolute;
    cursor: move;
    touch-action: none;
}

.result-scroll {
    flex: 1;
    min-height: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 6px;
    overflow: auto;
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-default);
    background: var(--color-bg-1);
}

.result-img {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
    border-radius: var(--base-radius-default);
}

.decode-msg {
    margin-top: var(--base-margin);
    font-size: var(--base-size-small);
}

.decode-msg.ok {
    color: rgb(var(--green-6));
}

.decode-msg.err {
    color: rgb(var(--red-6));
}
</style>
