/**
 * 文件查看器「图片 / PDF 编辑」共用的小工具。
 *
 * 处理全在浏览器（canvas / pdf-lib），落盘一律经跨平台绑定 FileWriteBinary，
 * 所以 Windows 桌面与 Linux 面板（浏览器访问）用的是同一份代码。
 * 路径这里都是**本机路径**（左栏 DiskList 给出的），Windows 反斜杠与 POSIX 正斜杠都能切。
 */

/** 与后端 fileview.MaxBinInline 同口径：超过这个大小不进浏览器内存做编辑 */
export const MAX_EDIT_BYTES = 25 << 20;
/** 与后端 fileview.MaxWriteBinary 同口径：产物写回上限 */
export const MAX_OUT_BYTES = 512 << 20;

export function extOf(name: string): string {
    const i = name.lastIndexOf(".");
    return i >= 0 ? name.slice(i + 1).toLowerCase() : "";
}

export function stripExt(name: string): string {
    const i = name.lastIndexOf(".");
    return i > 0 ? name.slice(0, i) : name;
}

export function baseName(p: string): string {
    return p.replace(/^.*[\\/]/, "");
}

/** 所在目录（保留原分隔符风格；根目录返回自身） */
export function parentDir(p: string): string {
    const m = p.match(/^(.*[\\/])[^\\/]*$/);
    if (m) return m[1].replace(/[\\/]+$/, "") || m[1];
    return p;
}

/** 按目录风格拼接；已有同名时不自动改名，由调用方决定是否覆盖 */
export function joinPath(dir: string, name: string): string {
    if (!dir) return name;
    const sep = dir.includes("\\") && !dir.includes("/") ? "\\" : "/";
    return dir.replace(/[\\/]+$/, "") + sep + name;
}

export function fmtBytes(n: number): string {
    if (!n) return "0 B";
    const u = ["B", "KB", "MB", "GB"];
    let i = 0, v = n;
    while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
    return (i === 0 ? v : v.toFixed(1)) + " " + u[i];
}

export function bufToBase64(buf: ArrayBuffer): string {
    const bytes = new Uint8Array(buf);
    let bin = "";
    const chunk = 0x8000;
    for (let i = 0; i < bytes.length; i += chunk) {
        bin += String.fromCharCode(...bytes.subarray(i, i + chunk));
    }
    return btoa(bin);
}

export async function blobToBase64(b: Blob): Promise<string> {
    const buf = await b.arrayBuffer();
    return bufToBase64(buf);
}

/** dataURL → 纯 base64 载荷（FileWriteBinary 只要载荷） */
export function dataURLPayload(u: string): string {
    const i = u.indexOf(",");
    return i >= 0 ? u.slice(i + 1) : u;
}

export function blobToDataURL(b: Blob): Promise<string> {
    return new Promise((resolve, reject) => {
        const fr = new FileReader();
        fr.onload = () => resolve(String(fr.result || ""));
        fr.onerror = () => reject(fr.error || new Error("读取失败"));
        fr.readAsDataURL(b);
    });
}

export function canvasToBlob(c: HTMLCanvasElement, mime: string, quality?: number): Promise<Blob> {
    return new Promise((resolve, reject) => {
        c.toBlob((b) => (b ? resolve(b) : reject(new Error("编码失败（浏览器不支持该格式）"))), mime, quality);
    });
}

/**
 * 画布 → ICO（.ico）。浏览器 toBlob 不支持 icon 编码，这里按 Vista+ 的
 * 「PNG 内嵌」图标约定手拼容器：ICONDIR(6) + ICONDIRENTRY(16) + PNG 字节。
 * 经典 ICO 上限 256×256，超出等比缩到 256 内；PNG 保留透明通道（无损）。
 */
export async function canvasToIco(c: HTMLCanvasElement): Promise<Blob> {
    const MAX = 256;
    let src = c;
    if (c.width > MAX || c.height > MAX) {
        const k = MAX / Math.max(c.width, c.height);
        src = resizeCanvas(c, Math.max(1, Math.round(c.width * k)), Math.max(1, Math.round(c.height * k)));
    }
    const png = await canvasToBlob(src, "image/png");
    const pngBuf = await png.arrayBuffer();
    const w = src.width, h = src.height;
    const head = new ArrayBuffer(6 + 16);
    const dv = new DataView(head);
    dv.setUint16(0, 0, true);                       // idReserved
    dv.setUint16(2, 1, true);                       // idType = 1（图标）
    dv.setUint16(4, 1, true);                       // idCount = 1 帧
    dv.setUint8(6, w >= 256 ? 0 : w);               // bWidth（256 记 0）
    dv.setUint8(7, h >= 256 ? 0 : h);               // bHeight
    dv.setUint8(8, 0);                              // bColorCount
    dv.setUint8(9, 0);                              // bReserved
    dv.setUint16(10, 1, true);                      // wPlanes
    dv.setUint16(12, 32, true);                     // wBitCount
    dv.setUint32(14, pngBuf.byteLength, true);      // dwBytesInRes
    dv.setUint32(18, 6 + 16, true);                 // dwImageOffset
    return new Blob([head, pngBuf], { type: "image/x-icon" });
}

/** 目标格式：png/ico 无损不带质量，jpeg/webp 有损（ico 走 PNG 内嵌，同样无损） */
export function isLossy(fmt: string): boolean {
    return fmt === "image/jpeg" || fmt === "image/webp";
}

export function fmtExt(fmt: string): string {
    return fmt === "image/jpeg" ? "jpg"
        : fmt === "image/webp" ? "webp"
            : fmt === "image/x-icon" ? "ico"
                : "png";
}

/**
 * 等比/非等比缩放到目标尺寸。
 * jpeg 无透明通道，传 bg 先把底铺掉，否则透明区会变黑。
 */
export function resizeCanvas(src: HTMLCanvasElement, w: number, h: number, bg = ""): HTMLCanvasElement {
    const out = document.createElement("canvas");
    out.width = Math.max(1, Math.round(w));
    out.height = Math.max(1, Math.round(h));
    const ctx = out.getContext("2d");
    if (!ctx) return out;
    if (bg) {
        ctx.fillStyle = bg;
        ctx.fillRect(0, 0, out.width, out.height);
    }
    ctx.imageSmoothingQuality = "high";
    ctx.drawImage(src, 0, 0, out.width, out.height);
    return out;
}

/** 本机文件 → <img>（同源 /fileview/media，画布不会被污染、可导像素） */
export function loadImgFromMedia(path: string): Promise<HTMLImageElement> {
    const n = baseName(path);
    const url = `/fileview/media?path=${encodeURIComponent(path)}&name=${encodeURIComponent(n)}`;
    return new Promise((resolve, reject) => {
        const im = new Image();
        im.onload = () => resolve(im);
        im.onerror = () => reject(new Error(`图片读取失败：${n}`));
        im.src = url;
    });
}

/** 图片位图 → 目标格式 Blob；webp/gif/bmp/png 源都能吃（解码由浏览器做，重编码只 png/jpeg/webp） */
export async function encodeImage(img: HTMLImageElement | HTMLCanvasElement, fmt: string, quality?: number, bg = "#FFFFFF"): Promise<Blob> {
    const w = img instanceof HTMLImageElement ? img.naturalWidth : img.width;
    const h = img instanceof HTMLImageElement ? img.naturalHeight : img.height;
    const c = document.createElement("canvas");
    c.width = Math.max(1, w);
    c.height = Math.max(1, h);
    const ctx = c.getContext("2d");
    if (!ctx) throw new Error("画布不可用");
    if (fmt !== "image/png") {
        ctx.fillStyle = bg;
        ctx.fillRect(0, 0, c.width, c.height);
    }
    ctx.drawImage(img, 0, 0);
    return canvasToBlob(c, fmt, quality);
}

export async function fetchArrayBuffer(url: string): Promise<ArrayBuffer> {
    const resp = await fetch(url);
    if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
    return resp.arrayBuffer();
}

/** 本机文件 → ArrayBuffer（PDF 操作用，同源流式端点） */
export function fetchLocalFile(path: string): Promise<ArrayBuffer> {
    return fetchArrayBuffer(`/fileview/media?path=${encodeURIComponent(path)}&name=${encodeURIComponent(baseName(path))}`);
}

/** ArrayBuffer/Blob → 经 FileWriteBinary 落盘 */
export async function writeBinary(goTools: any, path: string, data: ArrayBuffer | Blob): Promise<void> {
    const buf = data instanceof Blob ? await data.arrayBuffer() : data;
    const size = buf.byteLength;
    if (size > MAX_OUT_BYTES) throw new Error(`产物 ${fmtBytes(size)} 超过写回上限 ${fmtBytes(MAX_OUT_BYTES)}，请在浏览器外处理`);
    await goTools.FileWriteBinary(path, bufToBase64(buf));
}

/** 触发浏览器下载（面板模式下用户就是靠这个取产物） */
export function downloadBlob(data: Blob | ArrayBuffer, name: string): void {
    const blob = data instanceof Blob ? data : new Blob([data]);
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = name;
    document.body.appendChild(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(a.href), 4000);
}

/** 解析 "1-3,5,8-10" 这类页码范围（1 基，越界自动夹到 [1,total]，保持输入顺序） */
export function parseRanges(input: string, total: number): number[] {
    const out: number[] = [];
    for (const part of String(input || "").split(/[,，;；\s]+/)) {
        if (!part) continue;
        const m = part.match(/^(\d+)\s*[-~]\s*(\d+)$/);
        if (m) {
            let a = parseInt(m[1], 10), b = parseInt(m[2], 10);
            if (a > b) [a, b] = [b, a];
            for (let i = Math.max(1, a); i <= Math.min(total, b); i++) out.push(i);
            continue;
        }
        if (/^\d+$/.test(part)) {
            const n = parseInt(part, 10);
            if (n >= 1 && n <= total) out.push(n);
        }
    }
    return out;
}
