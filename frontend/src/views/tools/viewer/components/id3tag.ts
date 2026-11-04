import { fmtTime } from "./mediaCore";

/*
MP3 标签前端解析器：不引第三方库，用 HTTP Range 只取需要的字节。
后端 /fileview/media 是 ServeContent（支持 Range/206），所以：
- ID3v2：先取 10 字节头拿标签总长，再按长度取头后数据（上限 3MB，封面内嵌其中）；
- ID3v1：末 128 字节兜底（无 v2 时）；
两者都缺时返回空结果，调用方回退文件名展示。
支持 ID3v2.3/2.4（v2.2 帧名过旧不单独适配），文本帧编码 0/1/2/3 全覆盖
（Latin-1 / UTF-16 带 BOM / UTF-16BE / UTF-8），unsynchronisation 按帧去 0xFF00。
封面转 blob URL 供 <img>，dominantColor 用 32x32 画布均值取主色（氛围背景用）。
*/

export interface ID3Result {
    title?: string;
    artist?: string;
    album?: string;
    /** ID3v1 的 Genre 字节查表（仅作信息行点缀，v2 的 TCO/TKE 不细解析） */
    genre?: string;
    cover?: { url: string; mime: string };
    /** 封面主色 "rgb(r, g, b)"，取不到为 undefined */
    tint?: string;
}

/** Range 取一段字节；端点不支持 Range 或出错返回 null（调用方降级）。 */
async function fetchRange(url: string, start: number, end: number): Promise<Uint8Array | null> {
    try {
        const res = await fetch(url, { headers: { Range: `bytes=${start}-${end}` } });
        if (!res.ok && res.status !== 206) return null;
        const buf = await res.arrayBuffer();
        return new Uint8Array(buf);
    } catch {
        return null;
    }
}

function readUint8be(view: DataView, pos: number, len: number): number {
    let n = 0;
    for (let i = 0; i < len; i++) n = n * 256 + view.getUint8(pos + i);
    return n;
}

/** ID3v2 头里的长度：4 字节 syncsafe（每字节 7 位）。 */
function syncsafe(view: DataView, pos: number): number {
    return (
        (view.getUint8(pos) << 21) |
        (view.getUint8(pos + 1) << 14) |
        (view.getUint8(pos + 2) << 7) |
        view.getUint8(pos + 3)
    );
}

/** 文本帧解码：enc 0=Latin-1 1=UTF-16(BOM) 2=UTF-16BE 3=UTF-8。 */
function decodeText(buf: Uint8Array): string {
    if (!buf.length) return "";
    const enc = buf[0];
    const body = buf.subarray(1);
    try {
        if (enc === 1) return new TextDecoder("utf-16").decode(body).replace(/\0+$/, "");
        if (enc === 2) return new TextDecoder("utf-16be").decode(body).replace(/\0+$/, "");
        return new TextDecoder(enc === 0 ? "iso-8859-1" : "utf-8").decode(body).replace(/\0+$/, "");
    } catch {
        return "";
    }
}

/** 去帧级 unsynchronisation：0xFF 0x00 → 0xFF。 */
function desync(buf: Uint8Array): Uint8Array {
    const out = new Uint8Array(buf.length);
    let w = 0;
    for (let i = 0; i < buf.length; i++) {
        out[w++] = buf[i];
        if (buf[i] === 0xff && i + 1 < buf.length && buf[i + 1] === 0x00) i++;
    }
    return out.subarray(0, w);
}

function apic(buf: Uint8Array): { mime: string; data: Uint8Array } | null {
    if (buf.length < 4) return null;
    const enc = buf[0];
    let p = 1;
    let mime = "image/jpeg";
    if (enc === 1 || enc === 2) {
        // UTF-16 系：MIME 前的终止符按单字节 00 处理不严格，但规范里 MIME 恒为 Latin-1
        mime = new TextDecoder("iso-8859-1").decode(buf.subarray(1, buf.indexOf(0, 1)));
        p = buf.indexOf(0, 1) + 1;
    } else {
        const z = buf.indexOf(0, 1);
        if (z < 0) return null;
        mime = new TextDecoder("iso-8859-1").decode(buf.subarray(1, z));
        p = z + 1;
    }
    p++; // picture type 1 字节（3=封面正合适，其余也照收）
    // 描述（按 enc 宽度找终止 0），之后到帧尾全是图像数据
    if (enc === 1 || enc === 2) {
        while (p + 1 < buf.length && !(buf[p] === 0 && buf[p + 1] === 0)) p += 2;
        p += 2;
    } else {
        while (p < buf.length && buf[p] !== 0) p++;
        p++;
    }
    const data = buf.subarray(Math.min(p, buf.length));
    if (!data.length) return null;
    if (!/^image\//.test(mime)) mime = "image/jpeg";
    return { mime, data };
}

const GENRES = [
    "Blues", "Classic Rock", "Country", "Dance", "Disco", "Funk", "Grunge",
    "Hip-Hop", "Jazz", "Metal", "New Age", "Oldies", "Other", "Pop", "R&B",
    "Rap", "Reggae", "Rock", "Techno", "Industrial", "Alternative", "Ska",
    "Death Metal", "Pranks", "Soundtrack", "Euro-Techno", "Ambient",
];

/** 32x32 缩放均值取主色；画布被污染或解码失败返回 undefined。 */
async function dominantColor(url: string): Promise<string | undefined> {
    try {
        const img = await new Promise<HTMLImageElement>((resolve, reject) => {
            const im = new Image();
            im.onload = () => resolve(im);
            im.onerror = reject;
            im.src = url;
        });
        const size = 32;
        const canvas = document.createElement("canvas");
        canvas.width = size;
        canvas.height = size;
        const ctx = canvas.getContext("2d");
        if (!ctx) return undefined;
        ctx.drawImage(img, 0, 0, size, size);
        const px = ctx.getImageData(0, 0, size, size).data;
        let r = 0, g = 0, b = 0, n = 0;
        for (let i = 0; i < px.length; i += 4) {
            if (px[i + 3] < 64) continue;
            r += px[i]; g += px[i + 1]; b += px[i + 2]; n++;
        }
        if (!n) return undefined;
        return `rgb(${Math.round(r / n)}, ${Math.round(g / n)}, ${Math.round(b / n)})`;
    } catch {
        return undefined;
    }
}

/** 解析 mp3 标签。url 需支持 Range（/fileview/media 同源端点）。 */
export async function parseID3(url: string): Promise<ID3Result> {
    const out: ID3Result = {};
    // —— ID3v2 ——
    const head = await fetchRange(url, 0, 9);
    if (head && head.length === 10 && head[0] === 0x49 && head[1] === 0x44 && head[2] === 0x33) {
        const view = new DataView(head.buffer);
        const major = head[3];
        const flags = head[6];
        const tagSize = syncsafe(view, 6 + 4);
        const whole = await fetchRange(url, 0, Math.min(10 + tagSize, 3 * 1024 * 1024) - 1);
        if (whole && whole.length >= 10) {
            let body = whole.subarray(10);
            if (flags & 0x40) {
                // 扩展头：v3 是 4 字节长度(不含自身)，v4 是 syncsafe(含自身)
                const bv = new DataView(body.buffer, body.byteOffset, body.byteLength);
                const ext = major >= 4 ? syncsafe(bv, 0) : bv.getUint32(0) + 4;
                body = body.subarray(Math.min(ext, body.length));
            }
            const unsyncAll = (flags & 0x80) !== 0;
            let p = 0;
            while (p + 10 <= body.length) {
                const seg = body.subarray(p, p + 10);
                const id = String.fromCharCode(seg[0], seg[1], seg[2], seg[3]);
                if (!/^[A-Z0-9]{4}$/.test(id)) break; // padding 开始
                const bv = new DataView(body.buffer, body.byteOffset, body.length);
                let size = major >= 4 ? syncsafe(bv, p + 4) : bv.getUint32(p + 4);
                const fflags = bv.getUint16(p + 8);
                p += 10;
                if (size <= 0 || p + size > body.length) break;
                let data = body.subarray(p, p + size);
                p += size;
                if (unsyncAll || (fflags & 0x02)) data = desync(data);
                if (id === "TIT2") out.title = decodeText(data);
                else if (id === "TPE1") out.artist = decodeText(data);
                else if (id === "TALB") out.album = decodeText(data);
                else if (id === "APIC" && !out.cover) {
                    const pic = apic(data);
                    if (pic) {
                        out.cover = {
                            // slice 成独立 ArrayBuffer，避开 Uint8Array<ArrayBufferLike> 不兼容 BlobPart 的类型坑
                            url: URL.createObjectURL(new Blob([pic.data.slice().buffer], { type: pic.mime })),
                            mime: pic.mime,
                        };
                    }
                }
            }
        }
    }
    // —— ID3v1 兜底（无 v2 文本帧时）——
    if (!out.title && !out.artist) {
        const tail = await fetchRange(url, -128, -1);
        if (tail && tail.length === 128 && tail[0] === 0x54 && tail[1] === 0x41 && tail[2] === 0x47) {
            const dec = (s: number, e: number) =>
                new TextDecoder("iso-8859-1").decode(tail.subarray(s, e)).replace(/\0.*$/, "").trim();
            out.title = out.title || dec(3, 33);
            out.artist = out.artist || dec(33, 63);
            out.album = out.album || dec(63, 93);
            const gi = tail[127];
            out.genre = out.genre || (gi >= 0 && gi < GENRES.length ? GENRES[gi] : "");
        }
    }
    if (out.cover) out.tint = await dominantColor(out.cover.url);
    return out;
}

/** 释放 parseID3 产生的封面 blob URL。 */
export function releaseID3(r: ID3Result | null) {
    if (r?.cover) URL.revokeObjectURL(r.cover.url);
}

/** 剩余时间显示：-2:42（Apple Music 式右侧负数）。 */
export function fmtRemain(current: number, duration: number): string {
    const left = duration - current;
    if (!isFinite(left) || left <= 0) return "0:00";
    return "-" + fmtTime(left);
}
