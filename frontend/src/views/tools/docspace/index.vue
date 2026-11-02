<template>
  <div class="docspace">
    <!-- 顶部概览条：库目录 + 各类计数 + 动作 -->
    <div class="toolbar">
      <a-space>
        <a-button type="primary" @click="reload">
          <template #icon><icon-refresh /></template>刷新
        </a-button>
        <a-button @click="catModal.open = true"><template #icon><icon-plus /></template>新建分类</a-button>
        <a-button @click="openAddItem"><template #icon><icon-folder-add /></template>纳管文件</a-button>
        <a-button @click="trashOpen = true"><template #icon><icon-delete /></template>回收站</a-button>
        <a-button @click="vaultOpen = true"><template #icon><icon-lock /></template>加密库</a-button>
        <a-button @click="backupOpen = true"><template #icon><icon-safe /></template>备份</a-button>
        <a-button @click="noteModal.open = true"><template #icon><icon-book /></template>新建笔记</a-button>
      </a-space>
      <div class="hint">
        库目录：<span class="mono">{{ root || '--' }}</span>
        <a-divider direction="vertical" />
        分类 {{ counts.categories }} · 条目 {{ counts.items }} · 回收站 {{ counts.trash }}
      </div>
    </div>

    <a-layout class="body">
      <!-- 左：分类树 -->
      <a-layout-sider :width="240" class="left">
        <a-tree
          :data="treeData"
          :selected-keys="selectedCat"
          block-node
          @select="onSelectCat"
        />
      </a-layout-sider>

      <!-- 右：条目列表 -->
      <a-layout-content class="right">
        <div class="filterbar">
          <a-input-search
            v-model="keyword"
            placeholder="按名称 / 路径 / 标签搜索"
            allow-clear
            style="max-width: 320px"
          />
          <a-select
            v-model="tagFilter"
            placeholder="按标签筛选"
            allow-clear
            size="small"
            style="width: 160px"
          >
            <a-option v-for="t in allTags" :key="t" :value="t" :label="t" />
          </a-select>
          <a-button size="small" @click="searchOpen = true"><template #icon><icon-search /></template>高级搜索</a-button>
        </div>

        <!-- 高级搜索结果横幅：展示后端条件筛选结果，退出才回到本地过滤视图 -->
        <div v-if="advItems" class="advbar">
          <span>高级搜索命中 <strong>{{ advItems.length }}</strong> 条（按更新时间降序）</span>
          <a-link @click="advItems = null">退出结果视图</a-link>
        </div>

        <a-table
          :data="tableData"
          :loading="loading"
          :pagination="{ pageSize: 20 }"
          row-key="id"
          size="small"
          :column-resizable="true"
        >
          <template #columns>
            <a-table-column title="名称" data-index="name" :ellipsis="true" :tooltip="true" :width="180" />
            <a-table-column title="标签" :width="200">
              <template #cell="{ record }">
                <a-space wrap :size="4">
                  <a-tag v-for="t in (record.tags ?? [])" :key="t" size="small" color="arcoblue">{{ t }}</a-tag>
                  <span v-if="!(record.tags && record.tags.length)" class="muted">--</span>
                </a-space>
              </template>
            </a-table-column>
            <a-table-column title="源" data-index="src" :width="80" />
            <a-table-column title="路径" data-index="path" :ellipsis="true" :tooltip="true" />
            <a-table-column title="版本" :width="70">
              <template #cell="{ record }">{{ record.version || 0 }}</template>
            </a-table-column>
            <a-table-column title="更新时间" :width="170">
              <template #cell="{ record }">{{ fmtTime(record.updatedAt) }}</template>
            </a-table-column>
            <a-table-column title="操作" :width="430" fixed="right">
              <template #cell="{ record }">
                <a-space>
                  <a-link @click="copyPreview(record)">预览</a-link>
                  <a-link @click="openTags(record)">标签</a-link>
                  <a-link @click="openVersions(record)">版本</a-link>
                  <a-link v-if="record.src === 'local'" @click="openShare(record)">分享</a-link>
                  <a-link v-if="record.src === 'local'" @click="openEncrypt(record)">加密</a-link>
                  <a-link v-if="record.src === 'local'" @click="openUpload(record)">上传到远端</a-link>
                  <a-link v-if="record.src !== 'local'" @click="openDownload(record)">从远端取回</a-link>
                  <a-link v-if="record.src === 'local'" status="warning" @click="moveTrash(record)">回收站</a-link>
                  <a-link status="danger" @click="removeItem(record)">移出</a-link>
                </a-space>
              </template>
            </a-table-column>
          </template>
        </a-table>
      </a-layout-content>
    </a-layout>

    <!-- 新建分类 -->
    <a-modal v-model:visible="catModal.open" title="新建分类" @ok="submitCategory" :ok-loading="catModal.saving">
      <a-form :model="catForm" layout="vertical">
        <a-form-item field="name" label="名称">
          <a-input v-model="catForm.name" placeholder="分类名称" />
        </a-form-item>
        <a-form-item field="parent" label="父分类">
          <a-select v-model="catForm.parent" placeholder="留空为根分类" allow-clear>
            <a-option v-for="c in categories" :key="c.id" :value="c.id" :label="c.name" />
          </a-select>
        </a-form-item>
      </a-form>
    </a-modal>

    <!-- 纳管文件 -->
    <a-modal v-model:visible="itemModal.open" title="纳管文件（原地为主，不拷贝）" @ok="submitItem" :ok-loading="itemModal.saving">
      <a-form :model="itemForm" layout="vertical">
        <a-form-item field="src" label="源类型">
          <a-select v-model="itemForm.src">
            <a-option value="local">本机 local</a-option>
            <a-option value="sftp">SFTP 远端</a-option>
          </a-select>
        </a-form-item>
        <a-form-item v-if="itemForm.src === 'sftp'" field="sid" label="会话 ID">
          <a-input v-model="itemForm.sid" placeholder="文件传输里已建立的 SFTP 会话 id" />
        </a-form-item>
        <a-form-item field="path" label="路径">
          <a-input v-model="itemForm.path" :placeholder="itemForm.src === 'sftp' ? '/远端/绝对/路径' : '本机绝对路径'" />
        </a-form-item>
        <a-form-item field="category" label="分类">
          <a-select v-model="itemForm.category" placeholder="留空为未分类" allow-clear>
            <a-option v-for="c in categories" :key="c.id" :value="c.id" :label="c.name" />
          </a-select>
        </a-form-item>
      </a-form>
    </a-modal>

    <!-- 标签编辑 / 版本历史 / 回收站 -->
    <TagsModal
      v-model:visible="tagsModal.open"
      :item="tagsModal.item"
      :all-tags="allTags"
      @saved="reload"
    />
    <VersionsDrawer
      v-model:visible="versionsDrawer.open"
      :item="versionsDrawer.item"
      @changed="reload"
    />
    <TrashDrawer v-model:visible="trashOpen" @changed="reload" />
    <SecureVaultDrawer v-model:visible="vaultOpen" @changed="reload" />
    <BackupDrawer v-model:visible="backupOpen" :prefill="backupPrefill" @prefill-consumed="backupPrefill = undefined" @changed="reload" />
    <SearchDrawer
      v-model:visible="searchOpen"
      :categories="categories"
      :all-tags="allTags"
      @searched="onSearched"
      @changed="reload"
    />

    <!-- 局域网分享（lanshare 临时链接） -->
    <ShareModal v-model:visible="shareModal.open" :item="shareModal.item" />

    <!-- 加密（仅本机文件） -->
    <a-modal
      v-model:visible="encModal.open"
      title="加密文件（不可逆）"
      :ok-loading="encModal.saving"
      :mask-closable="false"
      @ok="submitEncrypt"
    >
      <a-alert type="warning" class="enc-warn">
        将以口令对「{{ encModal.item?.name }}」做 AES-256-GCM 加密封装。<strong>加密成功后原明文会被删除</strong>，
        只保留密文条目；口令不落盘、不可找回，忘记即永久丢失。请牢记口令。
      </a-alert>
      <a-form :model="encModal" layout="vertical">
        <a-form-item field="pw" label="口令">
          <a-input-password v-model="encModal.pw" placeholder="设置解密口令" />
        </a-form-item>
        <a-form-item field="pw2" label="确认口令">
          <a-input-password v-model="encModal.pw2" placeholder="再次输入口令" />
        </a-form-item>
      </a-form>
    </a-modal>
    <!-- 远端传输（§九 互挂）：复用文件传输的连接簿与传输队列，走 FtpUpload/FtpDownload 异步入队 -->
    <a-modal v-model:visible="upModal.open" :title="upModal.mode === 'up' ? '上传到远端' : '从远端取回'" :ok-loading="upModal.saving" @ok="submitUpload">
      <a-form :model="upForm" layout="vertical">
        <a-form-item v-if="!(upModal.mode === 'down' && upModal.item?.sid)" field="conn" label="FTP/SFTP 连接">
          <a-select v-model="upForm.conn" placeholder="选择连接（在文件传输页维护）" :loading="upModal.loadingConns">
            <a-option v-for="c in upConns" :key="c.id" :value="c.id" :label="`${c.name}（${c.type}）`" />
          </a-select>
        </a-form-item>
        <div v-else class="up-hint">条目自带会话 {{ upModal.item?.sid }}，将优先复用；会话已关时选一个连接重试。</div>
        <a-form-item v-if="upModal.mode === 'down'" field="connAlt" label="备用连接（可选，原会话失效时用）">
          <a-select v-model="upForm.connAlt" placeholder="留空则仅用原会话" allow-clear :loading="upModal.loadingConns">
            <a-option v-for="c in upConns" :key="c.id" :value="c.id" :label="`${c.name}（${c.type}）`" />
          </a-select>
        </a-form-item>
        <a-form-item v-if="upModal.mode === 'up'" field="dir" label="远端目录（需已存在）">
          <a-input v-model="upForm.dir" placeholder="如 /upload 或 /srv/docs" />
        </a-form-item>
        <a-form-item v-else field="local" label="本地目录（需已存在）">
          <a-input v-model="upForm.local" :placeholder="root ? `如 ${root}\\downloads 或直接用库根目录` : '本机绝对路径'" />
        </a-form-item>
      </a-form>
      <div class="up-hint">
        {{
          upModal.mode === 'up'
            ? `确认后经该连接上传「${upModal.item?.name}」，进度在文件传输页的传输队列里；会话不会自动关闭。`
            : `确认后经该连接取回「${upModal.item?.name}」到本地目录（同名避让），进度见传输队列；取回后可在磁盘页纳管为本地条目。`
        }}
      </div>
    </a-modal>
    <!-- 新建笔记：库内 notes/ 建 .md 并归入「笔记」分类，建完直接进查看器双栏编辑 -->
    <a-modal v-model:visible="noteModal.open" title="新建笔记" :ok-loading="noteModal.saving" @ok="submitNote">
      <a-form :model="noteModal" layout="vertical">
        <a-form-item field="name" label="标题">
          <a-input v-model="noteModal.name" placeholder="留空自动生成带时间戳的标题；无需 .md 后缀" />
        </a-form-item>
      </a-form>
      <div class="dim">笔记就是归类到「笔记」的 .md 文件，实体落在库内 notes/ 目录，在查看器里双栏编辑与预览。</div>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
/**
 * 文档中心（P1）：分类树 + 条目列表 + 标签 / 版本 / 回收站。
 * 真正的预览/编辑/Range 流式在 media 端点与 docref。方法都经全局 $GoTools 代理调用
 * （跨平台存在，Linux 面板同样可用）。
 */
import { computed, getCurrentInstance, onMounted, onUnmounted, reactive, ref } from "vue";
import { useRoute } from "vue-router";
import { Message, Modal } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import { errText, fmtTime, type Category, type Counts, type DocItem } from "./types";
import TagsModal from "./components/TagsModal.vue";
import VersionsDrawer from "./components/VersionsDrawer.vue";
import TrashDrawer from "./components/TrashDrawer.vue";
import SecureVaultDrawer from "./components/SecureVaultDrawer.vue";
import BackupDrawer from "./components/BackupDrawer.vue";
import SearchDrawer from "./components/SearchDrawer.vue";
import ShareModal from "./components/ShareModal.vue";
import { useToolWindow } from "@/views/index/useToolWindow";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const { openToolWindow } = useToolWindow();
const route = useRoute();

const loading = ref(false);
const root = ref("");
const counts = ref<Counts>({ categories: 0, items: 0, trash: 0, secure: 0, shares: 0, backups: 0 });
const categories = ref<Category[]>([]);
const items = ref<DocItem[]>([]);
const allTags = ref<string[]>([]);
const selectedCat = ref<string[]>(["__all__"]);

const keyword = ref("");
const tagFilter = ref<string>("");

const trashOpen = ref(false);
const vaultOpen = ref(false);
const backupOpen = ref(false);
/** 磁盘页跨页入口带进来的备份预填路径（§九 互挂），用完即清空 */
const backupPrefill = ref<{ src?: string; dst?: string } | undefined>(undefined);
const searchOpen = ref(false);
const shareModal = reactive<{ open: boolean; item: DocItem | null }>({ open: false, item: null });
/** 高级搜索结果：null = 未启用（走本地 filteredItems）；数组 = 展示后端命中 */
const advItems = ref<DocItem[] | null>(null);
const noteModal = reactive({ open: false, saving: false, name: "" });
const tagsModal = reactive<{ open: boolean; item: DocItem | null }>({ open: false, item: null });
const versionsDrawer = reactive<{ open: boolean; item: DocItem | null }>({ open: false, item: null });
const encModal = reactive<{ open: boolean; saving: boolean; item: DocItem | null; pw: string; pw2: string }>({
  open: false,
  saving: false,
  item: null,
  pw: "",
  pw2: "",
});

const treeData = computed(() => {
  const inCat = (id: string) => (id === "__all__" ? items.value.length : items.value.filter((it) => it.category === id).length);
  const nodes: any[] = [
    { key: "__all__", title: `全部（${inCat("__all__")}）` },
    { key: "__uncat__", title: `未分类（${items.value.filter((it) => !it.category).length}）` },
  ];
  const byParent = (p: string) => categories.value.filter((c) => (c.parent || "") === p);
  const build = (parent: string, depth: number): any[] =>
    byParent(parent).map((c) => ({
      key: c.id,
      title: `${c.name}（${inCat(c.id)}）`,
      children: depth < 5 ? build(c.id, depth + 1) : undefined,
    }));
  nodes.push(...build("", 0));
  return nodes;
});

const filteredItems = computed(() => {
  let list = items.value;
  const sel = selectedCat.value[0];
  if (sel && sel !== "__all__") {
    list = sel === "__uncat__" ? list.filter((it) => !it.category) : list.filter((it) => it.category === sel);
  }
  if (tagFilter.value) {
    list = list.filter((it) => (it.tags ?? []).includes(tagFilter.value));
  }
  const kw = keyword.value.trim().toLowerCase();
  if (kw) {
    list = list.filter(
      (it) =>
        it.name.toLowerCase().includes(kw) ||
        it.path.toLowerCase().includes(kw) ||
        (it.tags ?? []).some((t) => t.toLowerCase().includes(kw)),
    );
  }
  return list;
});

/** 表格数据：高级搜索结果优先（后端已排序），否则本地过滤视图 */
const tableData = computed(() => advItems.value ?? filteredItems.value);

function onSearched(items: DocItem[]) {
  advItems.value = items;
}

async function reload() {
  advItems.value = null; // 任何变更后回到全量视图，避免结果集与元数据不一致
  loading.value = true;
  try {
    const [r, c, cats, list, tags] = await Promise.all([
      GoTools?.DocSpaceRoot(),
      GoTools?.DocSpaceCounts(),
      GoTools?.DocSpaceCategories(),
      GoTools?.DocSpaceItems(),
      GoTools?.DocSpaceAllTags(),
    ]);
    root.value = String(r ?? "");
    if (c) counts.value = c as Counts;
    categories.value = (cats as Category[]) ?? [];
    items.value = (list as DocItem[]) ?? [];
    allTags.value = (tags as string[]) ?? [];
  } catch (e: any) {
    Message.error(`载入失败：${errText(e)}`);
  } finally {
    loading.value = false;
  }
}

function onSelectCat(keys: (string | number)[]) {
  selectedCat.value = keys.length ? [String(keys[0])] : ["__all__"];
}

/* ---------- 分类 ---------- */
const catModal = reactive({ open: false, saving: false });
const catForm = reactive({ name: "", parent: "" as string });
async function submitCategory() {
  if (!catForm.name.trim()) { Message.warning("分类名称不能为空"); return; }
  catModal.saving = true;
  try {
    await GoTools?.DocSpaceAddCategory(catForm.name.trim(), catForm.parent || "");
    Message.success("已新建分类");
    catModal.open = false;
    catForm.name = ""; catForm.parent = "";
    await reload();
  } catch (e: any) {
    Message.error(`新建分类失败：${errText(e)}`);
  } finally {
    catModal.saving = false;
  }
}

/* ---------- 条目 ---------- */
const itemModal = reactive({ open: false, saving: false });
const itemForm = reactive({ src: "local", sid: "", path: "", category: "" as string });
function openAddItem() {
  itemForm.src = "local"; itemForm.sid = ""; itemForm.path = ""; itemForm.category = selectedCat.value[0]?.startsWith("__") ? "" : (selectedCat.value[0] || "");
  itemModal.open = true;
}
async function submitItem() {
  if (!itemForm.path.trim()) { Message.warning("路径不能为空"); return; }
  itemModal.saving = true;
  try {
    await GoTools?.DocSpaceAddItem(itemForm.src, itemForm.sid, itemForm.path.trim(), itemForm.category || "");
    Message.success("已纳管");
    itemModal.open = false;
    await reload();
  } catch (e: any) {
    Message.error(`纳管失败：${errText(e)}`);
  } finally {
    itemModal.saving = false;
  }
}
function removeItem(record: DocItem) {
  Modal.confirm({
    title: "移出文档中心",
    content: `仅取消纳管「${record.name}」，不删除磁盘上的原文件（如需移入回收站请用「回收站」操作）。`,
    onOk: async () => {
      try {
        await GoTools?.DocSpaceRemoveItem(record.id);
        Message.success("已移出");
        await reload();
      } catch (e: any) {
        Message.error(`移出失败：${errText(e)}`);
      }
    },
  });
}
function moveTrash(record: DocItem) {
  Modal.confirm({
    title: "移入回收站",
    content: `将把本机文件「${record.name}」的实体移入文档回收站，并取消纳管；之后可在回收站还原。确认？`,
    onOk: async () => {
      try {
        await GoTools?.DocSpaceMoveTrash(record.id);
        Message.success("已移入回收站");
        await reload();
      } catch (e: any) {
        Message.error(`移入回收站失败：${errText(e)}`);
      }
    },
  });
}
async function copyPreview(record: DocItem) {
  try {
    const url = await GoTools?.DocSpacePreviewURL(record.src, record.sid || "", record.path, false);
    const full = `${window.location.origin}${url}`;
    await navigator.clipboard.writeText(full);
    Message.success("预览链接已复制");
  } catch (e: any) {
    Message.error(`生成预览链接失败：${errText(e)}`);
  }
}

/* ---------- 标签 / 版本抽屉 ---------- */
function openTags(record: DocItem) {
  tagsModal.item = record;
  tagsModal.open = true;
}
function openVersions(record: DocItem) {
  versionsDrawer.item = record;
  versionsDrawer.open = true;
}

/* ---------- 加密（仅本机条目） ---------- */
function openEncrypt(record: DocItem) {
  encModal.item = record;
  encModal.pw = "";
  encModal.pw2 = "";
  encModal.open = true;
}
async function submitEncrypt() {
  const it = encModal.item;
  if (!it) return;
  if (!encModal.pw) {
    Message.warning("口令不能为空");
    return;
  }
  if (encModal.pw !== encModal.pw2) {
    Message.warning("两次口令不一致");
    return;
  }
  encModal.saving = true;
  try {
    await GoTools?.SecStoreEncrypt(it.path, encModal.pw);
    // 原明文已被删除，这条本地条目成为悬空引用 → 同步取消纳管
    try {
      await GoTools?.DocSpaceRemoveItem(it.id);
    } catch {
      /* 密文已成功，取消纳管失败不阻断 */
    }
    Message.success("已加密，原明文已删除；可在加密库中查看");
    encModal.open = false;
    await reload();
  } catch (e: any) {
    Message.error(`加密失败：${errText(e)}`);
  } finally {
    encModal.saving = false;
  }
}

/* ---------- 局域网分享 ---------- */
function openShare(r: DocItem) {
  shareModal.item = r;
  shareModal.open = true;
}

/* ---------- 新建笔记 ---------- */
async function submitNote() {
  noteModal.saving = true;
  try {
    const it = (await GoTools?.DocSpaceNewNote(noteModal.name.trim())) as DocItem | undefined;
    noteModal.open = false;
    noteModal.name = "";
    await reload();
    if (it?.path) {
      // 建完直接进查看器双栏编辑：桌面开原生单例窗 / Web 走路由；已开着靠事件导航
      openToolWindow(`/tools/viewer?path=${encodeURIComponent(it.path)}`, "文件查看器", { single: true });
      try { Events.Emit("viewer:open-path", { path: it.path }); } catch { /* 非 Wails 环境忽略 */ }
    }
  } catch (e: any) {
    Message.error(`新建笔记失败：${errText(e)}`);
  } finally {
    noteModal.saving = false;
  }
}

/* ---------- 远端传输（§九 互挂）：复用连接簿 + FtpUpload/FtpDownload 异步队列 ---------- */
interface FtpConn { id: string; name: string; type: string }
const upModal = reactive<{ open: boolean; saving: boolean; loadingConns: boolean; item: DocItem | null; mode: "up" | "down" }>({ open: false, saving: false, loadingConns: false, item: null, mode: "up" });
const upForm = reactive({ conn: "", connAlt: "", dir: "/", local: "" });
const upConns = ref<FtpConn[]>([]);

async function openTransfer(r: DocItem, mode: "up" | "down") {
  upModal.item = r;
  upModal.mode = mode;
  upForm.conn = "";
  upForm.connAlt = "";
  upForm.dir = "/";
  upForm.local = root.value ? root.value + "\\downloads" : "";
  upModal.open = true;
  upModal.loadingConns = true;
  try {
    const list = await GoTools?.FtpListConnections();
    upConns.value = (list as FtpConn[]) ?? [];
    if (!upConns.value.length) Message.warning("还没有 FTP/SFTP 连接，请先到文件传输页添加");
  } catch (e: any) {
    Message.error(`载入连接列表失败：${errText(e)}`);
  } finally {
    upModal.loadingConns = false;
  }
}
function openUpload(r: DocItem) {
  void openTransfer(r, "up");
}
function openDownload(r: DocItem) {
  void openTransfer(r, "down");
}

// ensureSession 拿到一个可用会话：优先条目自带 sid（取回场景），其次现开连接。
async function ensureSession(prefer?: string, conn?: string): Promise<string> {
  if (prefer) {
    try {
      await GoTools?.FtpListDir(prefer, "."); // 轻量探活
      return prefer;
    } catch { /* 会话已关，往下走新建 */ }
  }
  if (!conn) throw new Error("请选择一个 FTP/SFTP 连接");
  const sess = await GoTools?.FtpOpenSession(conn);
  if (!sess?.id) throw new Error("未能建立会话");
  return sess.id;
}

async function submitUpload() {
  const it = upModal.item;
  if (!it?.path) { Message.warning("条目没有路径，无法传输"); return; }
  upModal.saving = true;
  try {
    if (upModal.mode === "up") {
      if (!upForm.conn) { Message.warning("请选择连接"); return; }
      const sid = await ensureSession(undefined, upForm.conn);
      await GoTools?.FtpUpload(sid, [it.path], upForm.dir.trim() || "/");
      Message.success(`已加入上传队列（会话 ${sid}），进度见文件传输页`);
    } else {
      const sid = await ensureSession(it.sid || undefined, upForm.connAlt || undefined);
      await GoTools?.FtpDownload(sid, [it.path], upForm.local.trim());
      Message.success(`已加入取回队列（会话 ${sid}），进度见文件传输页`);
    }
    upModal.open = false;
  } catch (e: any) {
    Message.error(`传输失败：${errText(e)}`);
  } finally {
    upModal.saving = false;
  }
}

/* ---------- 磁盘页跨页入口（计划 §九 互挂） ---------- */
function onBackupSrc(ev: any) {
  backupPrefill.value = { src: ev?.data?.path ?? "" };
  backupOpen.value = true;
}
function onBackupDst(ev: any) {
  backupPrefill.value = { dst: ev?.data?.path ?? "" };
  backupOpen.value = true;
}

onMounted(() => {
  try {
    Events.On("docspace:backup-src", onBackupSrc);
    Events.On("docspace:backup-dst", onBackupDst);
  } catch { /* 非 Wails 环境忽略 */ }
  // URL 预填：磁盘页对「未打开过」的文档中心窗口走 ?bkSrc= / ?bkDst= query 传递
  // （事件只对已挂载的实例有效，两条路互补；原生单例窗与 Web 路由都能带 query）
  const q = route.query as Record<string, string | string[]>;
  const bkSrc = Array.isArray(q.bkSrc) ? q.bkSrc[0] : q.bkSrc;
  const bkDst = Array.isArray(q.bkDst) ? q.bkDst[0] : q.bkDst;
  if (bkSrc || bkDst) {
    backupPrefill.value = { src: bkSrc || undefined, dst: bkDst || undefined };
    backupOpen.value = true;
  }
});
onUnmounted(() => {
  try {
    // Wails v3 的 Events.Off 只接事件名（移除该事件全部回调），不接受回调函数引用
    Events.Off("docspace:backup-src");
    Events.Off("docspace:backup-dst");
  } catch { /* 忽略 */ }
});

onMounted(reload);
</script>

<style scoped lang="less">
.docspace {
  display: flex;
  flex-direction: column;
  height: 100%;
  padding: 12px;
  box-sizing: border-box;
}
.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 10px;
}
.toolbar .hint {
  color: var(--color-text-3);
  font-size: 12px;
}
.mono {
  font-family: Consolas, Menlo, monospace;
}
.body {
  flex: 1;
  min-height: 0;
  background: var(--color-bg-2);
  border-radius: 8px;
  overflow: hidden;
}
.left {
  border-right: 1px solid var(--color-border-2);
  padding: 8px;
  overflow: auto;
}
.right {
  padding: 8px 12px;
  overflow: auto;
}
.filterbar {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 10px;
}
.muted {
  color: var(--color-text-4);
}
.dim {
  color: var(--color-text-3);
  font-size: 12px;
}
.advbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 6px 10px;
  margin-bottom: 8px;
  border-radius: 6px;
  background: var(--color-fill-2);
  font-size: 12px;
}
.enc-warn {
  margin-bottom: 12px;
}
.up-hint {
  color: var(--color-text-3);
  font-size: 12px;
  line-height: 1.6;
}
</style>
