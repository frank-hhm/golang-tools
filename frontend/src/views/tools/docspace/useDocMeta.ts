/**
 * 文档中心：条目派生元数据（类型 / 大小 / 修改时间 / 缩略图）的取数与装配。
 *
 * 为什么单独抽一层：
 *  - 类型、大小、时间不在条目 JSON 里，是每次列表经 docref 现取的派生事实，
 *    得按条目 id 拼回行上（而不是按下标：前端一重排下标就错行）；
 *  - 统计卡、类型 chips、网格缩略图用的都是同一份 Metas + Overview，
 *    散在各组件里各取一次会变成多次往返。
 *
 * Linux 面板（Web 模式）同样可用：DocSpaceMetas/DocSpaceOverview 都不带 build tag，
 * 只是 local 源在这里浏览的是服务器本机文件。
 */
import { computed, getCurrentInstance, ref } from "vue";
import { errText, type DocItem, type DocRow, type ItemMeta, type Overview } from "./types";

/** 类型 → 强调色（chips 的小圆点与徽标都用这一色）。
 * 与 common-file-icon 的扩展名色条同一套语义色，
 * 但那份表按扩展名索引、只组件内部用；chips/徽标要按 kind 上色，所以这里另给一份
 * 以 kind 为键的映射（值保持一致，改一处记得改两处）。
 */
export const KIND_COLORS: Record<string, string> = {
  image: "#2AA96B",
  audio: "#8A5CF6",
  video: "#E5484D",
  pdf: "#D93A2B",
  doc: "#2B5CE6",
  sheet: "#1A8A4C",
  text: "#5B6472",
  code: "#2B7DE9",
  archive: "#B7791F",
  binary: "#8A9099",
};

/** 取类型色；未知 kind 退到「其他」的灰 */
export const kindColor = (kind: string): string => KIND_COLORS[kind] || KIND_COLORS.binary;

/** 文件源显示名：文案按源区分，功能有无由后端给的 reachable 决定 */
export const SRC_LABELS: Record<string, string> = {
  local: "本机",
  sftp: "SFTP",
  webdav: "WebDAV",
  cloud: "云盘",
};

export const srcLabel = (src?: string): string => SRC_LABELS[src || "local"] || src || "本机";

/** 排序口径：值 = 比较函数，键 = 前端下拉的选项。
 *  实体时间优先用元数据里的 modTime，取不到（缺元数据或实体缺失）才退回登记时间。 */
export type SortKey = "name" | "size" | "mtime" | "updated" | "created";

const emptyRowMeta = {
  kind: "",
  kindLabel: "其他",
  ext: "",
  mime: "",
  size: 0,
  modTime: 0,
  missing: false,
  thumbable: false,
  thumb: "",
  hasMeta: false,
};

export function useDocMeta() {
  const GoTools = (getCurrentInstance() as any)?.appContext?.config?.globalProperties?.$GoTools;

  /** id → 元数据。用 map 而不是数组：后端 Metas 只与 Items 同序，
   *  前端一旦筛选/重排，按下标对齐就会把 A 的体积给 B。 */
  const metaMap = ref<Record<string, ItemMeta>>({});
  const overview = ref<Overview | null>(null);
  const metaReady = ref(false);
  const metaError = ref("");

  /** 取数失败不弹整页错误：列表本身（条目）已经拿到了，元数据只是让类型/大小空着 */
  async function loadMeta(): Promise<void> {
    try {
      const [metas, ov] = await Promise.all([
        GoTools?.DocSpaceMetas(),
        GoTools?.DocSpaceOverview(),
      ]);
      const map: Record<string, ItemMeta> = {};
      for (const m of (metas as ItemMeta[]) ?? []) {
        if (m?.id) map[m.id] = m;
      }
      metaMap.value = map;
      overview.value = (ov as Overview) ?? null;
      metaReady.value = true;
      metaError.value = "";
    } catch (e: any) {
      metaError.value = errText(e);
      metaMap.value = {};
      overview.value = null;
      metaReady.value = false;
    }
  }

  /** 后端 KindCount.label 是中文名称（唯一一份名称表在 Go 侧），行上的 label 就取自这里 */
  const kindLabels = computed<Record<string, string>>(() => {
    const out: Record<string, string> = {};
    for (const k of overview.value?.kinds ?? []) out[k.kind] = k.label || k.kind;
    return out;
  });

  /** 把条目 + 元数据拼成行。没有元数据的条目也出行（类型显 - ，不丢条目）。 */
  function enrich(items: DocItem[]): DocRow[] {
    const map = metaMap.value;
    return (items ?? []).map((it) => {
      const m = map[it.id];
      if (!m) return { ...it, ...emptyRowMeta };
      return {
        ...it,
        kind: m.kind,
        kindLabel: kindLabels.value[m.kind] || m.kind || "其他",
        ext: m.ext,
        mime: m.mime,
        size: m.size,
        modTime: m.modTime,
        missing: !!m.unreachable,
        thumbable: !!m.thumbable,
        thumb: m.thumb || "",
        hasMeta: true,
      };
    });
  }

  function labelOf(kind: string): string {
    return kindLabels.value[kind] || kind || "其他";
  }

  /**
   * 行排序。名称按自然语言比较（数字后缀 2 排在 10 前），
   * 体积/时间缺失的行一律排在末尾——把"不知道"冒充成 0 会让排序骗人。
   */
  function sortRows(rows: DocRow[], key: SortKey, desc: boolean): DocRow[] {
    const dir = desc ? -1 : 1;
    const num = (r: DocRow) => (key === "size" ? r.size : r.modTime || r.updatedAt);
    const list = rows.slice();
    list.sort((a, b) => {
      if (key === "name") return dir * a.name.localeCompare(b.name, "zh-Hans-CN", { numeric: true, sensitivity: "base" });
      if (key === "updated") return dir * ((a.updatedAt || 0) - (b.updatedAt || 0));
      if (key === "created") return dir * ((a.createdAt || 0) - (b.createdAt || 0));
      const am = key === "size" ? a.hasMeta && !a.missing : a.hasMeta;
      const bm = key === "size" ? b.hasMeta && !b.missing : b.hasMeta;
      // 取不到真实值的行永远在后（与 desc 无关，它们不是"最小"而是"未知"）
      if (am !== bm) return am ? -1 : 1;
      if (!am && !bm) return dir * ((a.updatedAt || 0) - (b.updatedAt || 0));
      return dir * (num(a) - num(b));
    });
    return list;
  }

  return {
    metaMap,
    overview,
    metaReady,
    metaError,
    loadMeta,
    enrich,
    labelOf,
    sortRows,
    kindColor,
    srcLabel,
  };
}

/** 单条能做的事：网格卡与表格行共用同一份，避免两处清单各自漂移。
 *  tone 只给「有破坏性」的动作上色，正常项保持默认，不然下拉一眼全是警告色。
 *  为什么不用 a-doption 的 status：这一版 Arco 只认 normal|danger，且 dropdown 样式表
 *  里根本没有 status 的配色规则（实测破坏项与普通项同色），所以上色得自己加。 */
export function rowOps(row: DocRow): { k: string; t: string; tone?: string }[] {
  const local = row.src === "local" || !row.src;
  const ops: { k: string; t: string; tone?: string }[] = [
    { k: "tags", t: "编辑标签" },
    { k: "versions", t: "版本历史" },
    { k: "moveCat", t: "移到分类" },
  ];
  if (local) {
    ops.push(
      { k: "compress", t: "压缩为 zip" },
      { k: "share", t: "局域网分享" },
      { k: "encrypt", t: "加密（不可逆）", tone: "danger" },
      { k: "upload", t: "上传到远端" },
    );
  } else {
    ops.push({ k: "download", t: "从远端取回" });
  }
  // 非本机源连“移入回收站”都做不到（MoveToTrash 只对 local 成立），直接标 danger
  ops.push({ k: "trash", t: "移入回收站", tone: local ? "warn" : "danger" });
  ops.push({ k: "remove", t: "移出文档中心", tone: "danger" });
  return ops;
}
