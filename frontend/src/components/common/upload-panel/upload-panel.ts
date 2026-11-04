/**
 * common-upload-panel 的传输契约类型。
 *
 * 独立成 .ts（而非写在 SFC 里）：<script setup> 不能带 export 语句，
 * 且父页需要给 transport 供一份类型化实现，类型必须能从外部 import。
 */

/** init 回：会话 id + 服务端下发的分片大小 + 已收分片下标（续传用） */
export interface UploadInit {
    sid: string;
    chunkSize: number;
    have: number[];
}

/**
 * 注入的传输函数集，UI 与传输解耦：换一套 transport 即可复用到别的上传场景。
 * 全部返回 Promise，失败即 reject（组件按行标 error 并给重试）。
 */
export interface UploadTransport {
    /** 建/续会话：回 sid、chunkSize、已收分片下标 */
    init: (file: File) => Promise<UploadInit>;
    /** 上传第 idx 片：组件按 chunkSize 算好 [start,end)，transport 内部 file.slice 取字节 */
    part: (file: File, sid: string, idx: number, start: number, end: number) => Promise<void>;
    /** 分片齐后落定 */
    complete: (file: File, sid: string) => Promise<void>;
    /** 取消会话（无 sid 时可空实现） */
    abort: (sid?: string) => Promise<void>;
    /** 小于阈值的整包上传（可选；不提供则小文件也走切片） */
    once?: (file: File) => Promise<void>;
}
