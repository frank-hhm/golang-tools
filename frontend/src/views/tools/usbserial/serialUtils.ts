/**
 * 串口/网络调试工具的字节编解码与校验算法工具集。
 * 与后端约定：所有收发均以 hex 字符串传输，前端负责文本/hex/校验的组装。
 */

/** hex 字符串（可含空格）转字节数组 */
export function hexToBytes(hexStr: string): number[] {
    const clean = hexStr.replace(/[^0-9a-fA-F]/g, "");
    const bytes: number[] = [];
    for (let i = 0; i + 1 < clean.length; i += 2) {
        bytes.push(parseInt(clean.substr(i, 2), 16));
    }
    return bytes;
}

/** 字节数组转大写、空格分隔的 hex 字符串 */
export function bytesToHex(bytes: number[] | Uint8Array): string {
    return Array.from(bytes)
        .map((b) => b.toString(16).padStart(2, "0").toUpperCase())
        .join(" ");
}

/** UTF-8 文本转字节数组 */
export function textToBytes(text: string): number[] {
    return Array.from(new TextEncoder().encode(text));
}

/** 字节数组按 UTF-8 解码为文本（用于非 HEX 显示） */
export function bytesToText(bytes: number[]): string {
    try {
        return new TextDecoder("utf-8", { fatal: false }).decode(new Uint8Array(bytes));
    } catch {
        return String.fromCharCode(...bytes);
    }
}

/** CRC8（MAXIM/Dallas 多项式） */
function crc8(bytes: number[]): number[] {
    let crc = 0;
    for (const b of bytes) {
        crc ^= b;
        for (let i = 0; i < 8; i++) {
            crc = crc & 1 ? (crc >> 1) ^ 0x8c : crc >> 1;
        }
    }
    return [crc & 0xff];
}

/** CRC16-MODBUS */
function crc16Modbus(bytes: number[]): number[] {
    let crc = 0xffff;
    for (const b of bytes) {
        crc ^= b;
        for (let i = 0; i < 8; i++) {
            crc = crc & 1 ? (crc >> 1) ^ 0xa001 : crc >> 1;
        }
    }
    // MODBUS 低字节在前
    return [crc & 0xff, (crc >> 8) & 0xff];
}

/** CRC16-CCITT-FALSE */
function crc16Ccitt(bytes: number[]): number[] {
    let crc = 0xffff;
    for (const b of bytes) {
        crc ^= b << 8;
        for (let i = 0; i < 8; i++) {
            crc = crc & 0x8000 ? ((crc << 1) ^ 0x1021) & 0xffff : (crc << 1) & 0xffff;
        }
    }
    return [(crc >> 8) & 0xff, crc & 0xff];
}

/** CRC32（IEEE 802.3，用于附加到大端字节序） */
function crc32(bytes: number[]): number[] {
    let crc = ~0;
    for (const b of bytes) {
        crc ^= b;
        for (let i = 0; i < 8; i++) {
            crc = crc & 1 ? (crc >>> 1) ^ 0xedb88320 : crc >>> 1;
        }
    }
    crc = ~crc >>> 0;
    return [(crc >> 24) & 0xff, (crc >> 16) & 0xff, (crc >> 8) & 0xff, crc & 0xff];
}

/** 校验算法类型 */
export type ChecksumKind = "none" | "crc8" | "crc16-modbus" | "crc16-ccitt" | "crc32";

export const CHECKSUM_OPTIONS: { value: ChecksumKind; label: string }[] = [
    { value: "none", label: "None" },
    { value: "crc8", label: "CRC8" },
    { value: "crc16-modbus", label: "CRC16-MODBUS" },
    { value: "crc16-ccitt", label: "CRC16-CCITT" },
    { value: "crc32", label: "CRC32" },
];

/** 按所选校验方式在字节尾部追加校验字节，返回新数组 */
export function appendChecksum(bytes: number[], kind: ChecksumKind): number[] {
    let extra: number[] = [];
    switch (kind) {
        case "crc8":
            extra = crc8(bytes);
            break;
        case "crc16-modbus":
            extra = crc16Modbus(bytes);
            break;
        case "crc16-ccitt":
            extra = crc16Ccitt(bytes);
            break;
        case "crc32":
            extra = crc32(bytes);
            break;
        default:
            extra = [];
    }
    return extra.length ? [...bytes, ...extra] : [...bytes];
}

/** 当前时间戳 HH:MM:SS.mmm */
export function nowStamp(): string {
    const d = new Date();
    const p = (n: number, l = 2) => String(n).padStart(l, "0");
    return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${p(d.getMilliseconds(), 3)}`;
}
