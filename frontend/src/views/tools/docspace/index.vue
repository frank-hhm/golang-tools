<template>
  <div class="docspace">
    <!-- 顶部：只留主操作，回收站/加密库/备份/高级搜索收进「更多」，按钮一排排长不是信息而是噪声 -->
    <div class="toolbar">
      <a-space>
        <a-button type="primary" @click="openAddItem"><template #icon><icon-plus /></template>纳管文件</a-button>
        <a-button @click="openCatModal()"><template #icon><icon-folder-add /></template>新建分类</a-button>
        <a-button @click="noteModal.open = true"><template #icon><icon-book /></template>新建笔记</a-button>
        <a-dropdown trigger="hover" :popup-max-height="false" @select="(k: any) => onToolsMenu(String(k))">
          <a-button>
            <template #icon><icon-apps /></template>更多<icon-down class="ds-caret" />
          </a-button>
          <template #content>
            <a-doption value="trash"><icon-delete />&nbsp;回收站<em v-if="counts.trash" class="menu-num">{{ counts.trash }}</em></a-doption>
            <a-doption value="vault"><icon-lock />&nbsp;加密库<em v-if="counts.secure" class="menu-num">{{ counts.secure }}</em></a-doption>
            <a-doption value="backup"><icon-safe />&nbsp;备份<em v-if="counts.backups" class="menu-num">{{ counts.backups }}</em></a-doption>
            <a-doption value="search"><icon-search />&nbsp;高级搜索</a-doption>
            <a-doption value="reload"><icon-refresh />&nbsp;刷新</a-doption>
          </template>
        </a-dropdown>
      </a-space>
      <div class="hint">
        <span class="hint-k">库目录</span>
        <span class="hint-path mono" :title="root">{{ root || '--' }}</span>
        <a-divider direction="vertical" />
        <span>{{ srcSummary }}</span>
        <a-link v-if="metaError" size="small" @click="loadMeta">元数据取数失败，重试</a-link>
      </div>
    </div>

    <StatsBar :overview="overview" :counts="counts" :kinds="kindList" />

    <a-layout class="body">
      <!-- 左：分类树（徽标数含子级，来自后端 Overview，与右面列表同一口径） -->
      <a-layout-sider :width="240" class="left">
        <div class="left-head">
          <span class="left-title">分类</span>
          <a-link size="small" @click="openCatModal()">新建</a-link>
        </div>
        <!-- 不画 show-line：Arco 的连接线在暗色下几乎看不见，亮一点又像字符画，
             缩进 + hover/选中态已经足够表达层级 -->
        <a-tree 
          :data="treeData"
          :selected-keys="selectedCat"
          block-node
          @select="onSelectCat"
        >
          <!-- 右键菜单只对真分类开放：「全部/未分类」是前端造的虚拟节点，没有实体可改名可删 -->
          <template #title="node">
            <a-tooltip :content="String(node?.title || '')" :disabled="!isRealCat(node?.key)">
              <span class="tree-title" @contextmenu.prevent.stop="onCatContext($event, node)">{{ node?.title }}</span>
            </a-tooltip>
          </template>
        </a-tree>
      </a-layout-sider>

      <!-- 右：条目列表（网格 / 表格双视图） -->
      <a-layout-content class="right">
        <div class="filterbar">
          <a-input-search
            v-model="keyword"
            class="ds-search"
            placeholder="按名称 / 路径 / 标签搜索"
            allow-clear
          />
          <a-select
            v-model="tagFilter"
            placeholder="按标签筛选"
            allow-clear
            size="small"
            class="ds-filter-select"
          >
            <a-option v-for="t in allTags" :key="t" :value="t" :label="t" />
          </a-select>
          <!-- 排序：表格自己有点列排序，只网格视图需要这个下拉 -->
          <template v-if="view === 'grid'">
            <a-select v-model="sortKey" size="small" style="width: 132px">
              <a-option value="name" label="按名称" />
              <a-option value="size" label="按大小" />
              <a-option value="mtime" label="按实体时间" />
              <a-option value="updated" label="按登记更新" />
            </a-select>
            <a-button size="small" :title="sortDesc ? '当前降序' : '当前升序'" @click="sortDesc = !sortDesc">
              <template #icon><icon-arrow-down v-if="sortDesc" /><icon-arrow-up v-else /></template>
            </a-button>
          </template>
          <a-checkbox v-model="onlyMissing" size="small">只看缺失</a-checkbox>
          <span class="flex-1" />
          <a-divider direction="vertical" class="ds-filter-div" />
          <a-radio-group v-model="view" type="button" size="small">
            <a-radio value="grid"><template #icon><icon-apps /></template>网格</a-radio>
            <a-radio value="table"><template #icon><icon-list /></template>表格</a-radio>
          </a-radio-group>
        </div>

        <KindChips v-model="kindFilter" :kinds="kindList" :total="items.length" />

        <!-- 高级搜索结果横幅：展示后端条件筛选结果，退出才回到本地过滤视图 -->
        <div v-if="advItems" class="advbar">
          <span>高级搜索命中 <strong>{{ advItems.length }}</strong> 条（按更新时间降序）</span>
          <a-link @click="advItems = null">退出结果视图</a-link>
        </div>

        <BatchBar
          :count="selected.length"
          :total="viewRows.length"
          :missing="selectedMissing"
          :all-selected="allVisibleSelected"
          @toggle-all="toggleAllVisible"
          @tag="openBatchTags"
          @category="openCategoryMove(selected)"
          @snapshot="batchSnapshot"
          @export-list="copySelectedList"
          @trash="batchTrash"
          @remove="batchRemove"
          @clear="selected = []"
        />

        <DocGrid
          v-if="view === 'grid'"
          :rows="pagedRows"
          :selected="selected"
          :loading="loading"
          @action="onAction"
          @update:selected="selected = $event"
        />
        <DocTable
          v-else
          :rows="viewRows"
          :selected="selected"
          :loading="loading"
          @action="onAction"
          @update:selected="selected = $event"
        />

        <div v-if="view === 'grid' && viewRows.length > gridPageSize" class="ds-pager">
          <a-pagination v-model:current="gridPage" :total="viewRows.length" :page-size="gridPageSize" simple show-total />
        </div>
      </a-layout-content>
    </a-layout>

    <!-- 分类树右键菜单：照 MySQL 对象树既有口径（遮罩 + 定位的 a-menu 自绘），
         不另引第三方 contextmenu 组件 -->
    <div v-if="catCtx.visible" class="ctx-mask" @click="catCtx.visible = false" @contextmenu.prevent="catCtx.visible = false" />
    <a-menu v-if="catCtx.visible && catCtx.id" class="ctx-menu" :style="{ left: catCtx.x + 'px', top: catCtx.y + 'px' }"
      @menu-item-click="onCatCtxClick">
      <a-menu-item key="rename"><icon-edit /> 重命名</a-menu-item>
      <a-menu-item key="add"><icon-folder-add /> 新建子分类</a-menu-item>
      <a-menu-item key="del" class="ctx-del"><icon-delete /> 删除分类</a-menu-item>
    </a-menu>

    <a-modal v-model:visible="catRename.open" title="重命名分类" :ok-loading="catRename.saving" @ok="submitCatRename">
      <a-form :model="catRename" layout="vertical">
        <a-form-item field="name" label="名称">
          <a-input v-model="catRename.name" placeholder="分类名称" allow-clear @press-enter="submitCatRename" />
        </a-form-item>
      </a-form>
    </a-modal>

    <!-- 新建分类 -->
    <a-modal v-model:visible="catModal.open" title="新建分类" @ok="submitCategory" :ok-loading="catModal.saving">
      <a-form :model="catForm" layout="vertical">
        <a-form-item field="name" label="名称">
          <a-input v-model="catForm.name" placeholder="分类名称" />
        </a-form-item>
        <a-form-item field="parent" label="父分类">
          <a-cascader
            v-model="catForm.parent"
            :options="catOptions"
            check-strictly
            allow-clear
            placeholder="留空为根分类"
          />
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
          <a-cascader
            v-model="itemForm.category"
            :options="catOptions"
            check-strictly
            allow-clear
            placeholder="留空为未分类"
          />
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

    <!-- 批量 / 单条改分类：ids 只有一个时也走这里，省得两套逻辑 -->
    <a-modal
      v-model:visible="catMoveModal.open"
      :title="`移到分类（${catMoveModal.ids.length} 项）`"
      :ok-loading="catMoveModal.saving"
      @ok="submitMoveCategory"
    >
      <a-cascader
        v-model="catMoveModal.target"
        :options="catOptions"
        check-strictly
        allow-clear
        placeholder="留空 = 移到未分类"
      />
      <div class="dim up-hint">只改文档中心里的归类，不动磁盘上的实体文件。</div>
    </a-modal>

    <!-- 批量标签：并集加 / 差集删，语义与后端 TagItems 一致 -->
    <a-modal
      v-model:visible="batchTagModal.open"
      :title="`批量标签（${batchTagModal.ids.length} 项）`"
      :ok-loading="batchTagModal.saving"
      @ok="submitBatchTags"
    >
      <a-form :model="batchTagModal" layout="vertical">
        <a-form-item field="add" label="追加标签">
          <a-select v-model="batchTagModal.add" multiple allow-create :data="allTags" placeholder="输入后回车添加" allow-clear />
        </a-form-item>
        <a-form-item field="remove" label="移除标签">
          <a-select v-model="batchTagModal.remove" multiple allow-create :data="allTags" placeholder="选中即从这些条目上摘掉" allow-clear />
        </a-form-item>
      </a-form>
      <div class="dim up-hint">追加走并集、移除走差集，两者同名时以移除为准；没变化的条目不会被重写登记时间。</div>
    </a-modal>

    <!-- 批量存快照 -->
    <a-modal v-model:visible="snapModal.open" :title="`存快照（${snapModal.ids.length} 项）`" :ok-loading="snapModal.saving" @ok="submitSnapshot">
      <a-input v-model="snapModal.note" placeholder="版本备注（可留空）" />
      <div class="dim up-hint">仅本机实体可快照；远端 / 缺失的条目会逐条跳过并在结果里说明原因。</div>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
/**
 * 文档中心（P1）：分类树 + 条目列表（网格 / 表格）+ 类型筛选 + 批量操作 + 标签 / 版本 / 回收站。
 * 真正的预览/编辑/Range 流式在 media 端点与 docref。方法都经全局 $GoTools 代理调用
 * （跨平台存在，Linux 面板同样可用）。
 *
 * 列表行不再是裸 DocItem：类型/大小/实体时间由 useDocMeta 把后端 Metas 按 id 拼回行上
 * （DocRow），所以统计卡、chips、缩略图、排序用的是同一份元数据，不会出现两个口径。
 */
import { computed, getCurrentInstance, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { Message, Modal } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import { errText, fmtSize, type BatchResult, type Category, type CategoryCount, type Counts, type DocItem, type DocRow, type KindCount } from "./types";
import { srcLabel, useDocMeta } from "./useDocMeta";
import type { SortKey } from "./useDocMeta";
import StatsBar from "./components/StatsBar.vue";
import KindChips from "./components/KindChips.vue";
import BatchBar from "./components/BatchBar.vue";
import DocGrid from "./components/DocGrid.vue";
import DocTable from "./components/DocTable.vue";
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

/* ---------- 视图与筛选（网格/表格、类型、只看缺失、排序、分页、多选） ---------- */
const view = ref<"grid" | "table">("grid");
const kindFilter = ref("");
const onlyMissing = ref(false);
const sortKey = ref<SortKey>("mtime");
const sortDesc = ref(true);
const selected = ref<string[]>([]);
const gridPage = ref(1);
const gridPageSize = 24;

/** 派生元数据（类型/大小/实体时间/缩略图）+ 总览统计 */
const { overview, metaError, loadMeta, enrich, sortRows } = useDocMeta();

const kindList = computed<KindCount[]>(() =>
  [...(overview.value?.kinds ?? [])].sort((a, b) => b.num - a.num),
);

/** 顶部一句“本机 12 · SFTP 3”：比单独一张源分布卡省地方，也够说明来源构成 */
const srcSummary = computed(() => {
  const srcs = overview.value?.sources ?? [];
  if (!srcs.length) return `共 ${counts.value.items} 条`;
  return srcs.map((s) => `${srcLabel(s.src)} ${s.num}`).join(" · ");
});

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
  // 徽标优先走后端 Overview：分类用「含子级」滚加值，与右面筛选后的数量同一口径；
  // 元数据没取到时退回本地数一遍，不让左树变成空括号。
  const byCat = new Map<string, CategoryCount>();
  for (const c of overview.value?.categories ?? []) byCat.set(c.id, c);
  const total = overview.value?.stats.total ?? items.value.length;
  const inCat = (id: string) => {
    if (id === "__all__") return total;
    const hit = id === "__uncat__" ? byCat.get("") : byCat.get(id);
    if (hit) return hit.inclusive;
    return id === "__uncat__"
      ? items.value.filter((it) => !it.category).length
      : items.value.filter((it) => it.category === id).length;
  };
  const byParent = (p: string) => categories.value.filter((c) => (c.parent || "") === p);
  const build = (parent: string, seen: Set<string>): any[] =>
    byParent(parent)
      .filter((c) => !seen.has(c.id))
      .map((c) => {
        const next = new Set(seen).add(c.id);
        const children = build(c.id, next);
        return { key: c.id, title: `${c.name}（${inCat(c.id)}）`, children: children.length ? children : undefined };
      });
  const nodes: any[] = [
    { key: "__all__", title: `全部（${inCat("__all__")}）` },
    { key: "__uncat__", title: `未分类（${inCat("__uncat__")}）` },
  ];
  nodes.push(...build("", new Set<string>()));
  return nodes;
});

/** 父分类 / 条目分类选择器共用：无限级 a-cascader 选项（value=id, label=name） */
const catOptions = computed(() => {
  const byParent = (p: string) => categories.value.filter((c) => (c.parent || "") === p);
  const build = (parent: string, seen: Set<string>): any[] =>
    byParent(parent)
      .filter((c) => !seen.has(c.id))
      .map((c) => {
        const next = new Set(seen).add(c.id);
        const children = build(c.id, next);
        return { value: c.id, label: c.name, children: children.length ? children : undefined };
      });
  return build("", new Set<string>());
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

/** 行管道：条目（含高级搜索命中）→ 拼元数据 → 类型/缺失筛选 → 排序。 */
const baseRows = computed<DocRow[]>(() => enrich(advItems.value ?? filteredItems.value));

const filteredRows = computed<DocRow[]>(() => {
  let list = baseRows.value;
  if (kindFilter.value) list = list.filter((r) => r.kind === kindFilter.value);
  if (onlyMissing.value) list = list.filter((r) => r.missing);
  return list;
});

/** 表格自带点列排序与分页，网格没有列头才用这里的排序下拉 + 底部翻页 */
const viewRows = computed<DocRow[]>(() =>
  view.value === "grid" ? sortRows(filteredRows.value, sortKey.value, sortDesc.value) : filteredRows.value,
);

const pagedRows = computed<DocRow[]>(() => {
  const start = (gridPage.value - 1) * gridPageSize;
  return viewRows.value.slice(start, start + gridPageSize);
});

/* ---------- 多选 ---------- */
const selSet = computed(() => new Set(selected.value));
const missingSet = computed(() => new Set(baseRows.value.filter((r) => r.missing).map((r) => r.id)));
const selectedMissing = computed(() => selected.value.filter((id) => missingSet.value.has(id)).length);
const allVisibleSelected = computed(
  () => viewRows.value.length > 0 && viewRows.value.every((r) => selSet.value.has(r.id)),
);

/** 全选只对当前可见范围生效：筛了一千条里选中时，不应当把用户看不到的也一带动上 */
function toggleAllVisible(on: boolean) {
  const visible = viewRows.value.map((r) => r.id);
  if (on) {
    const next = new Set(selected.value);
    for (const id of visible) next.add(id);
    selected.value = [...next];
    return;
  }
  const drop = new Set(visible);
  selected.value = selected.value.filter((id) => !drop.has(id));
}

// 筛选变窄后当前页可能落空，钉回第一页而不是留个空屏
watch(viewRows, (rows) => {
  const max = Math.max(1, Math.ceil(rows.length / gridPageSize));
  if (gridPage.value > max) gridPage.value = max;
});

watch([kindFilter, onlyMissing, selectedCat, keyword, tagFilter, view], () => {
  gridPage.value = 1;
});

function onSearched(found: DocItem[]) {
  advItems.value = found;
  selected.value = [];
}

/** 批量回执文案：后端逐条做，部分失败是常态，必须把“几条没成 + 第一条原因”说出来 */
function batchTip(res: BatchResult | undefined, word: string): string {
  const done = res?.done ?? 0;
  const failed = res?.failed?.length ?? 0;
  if (!failed) return `${word} ${done} 项`;
  return `${word} ${done} 项，${failed} 项未成功${res?.reason ? `：${res.reason}` : ""}`;
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
    // 选中集只保留还在列表里的：刚批量移出/入回收站的条目已经不在 items 里了，
    // 不清掉的话批量条会挂着几个点不到的幽灵 id。
    const alive = new Set(items.value.map((it) => it.id));
    selected.value = selected.value.filter((id) => alive.has(id));
    await loadMeta();
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
const catForm = reactive({ name: "", parent: undefined as string | undefined });
async function submitCategory() {
  if (!catForm.name.trim()) { Message.warning("分类名称不能为空"); return; }
  catModal.saving = true;
  try {
    await GoTools?.DocSpaceAddCategory(catForm.name.trim(), catForm.parent || "");
    Message.success("已新建分类");
    catModal.open = false;
    catForm.name = ""; catForm.parent = undefined;
    await reload();
  } catch (e: any) {
    Message.error(`新建分类失败：${errText(e)}`);
  } finally {
    catModal.saving = false;
  }
}

/* ---------- 分类树右键：重命名 / 建子分类 / 删除 ---------- */
const catCtx = reactive({ visible: false, x: 0, y: 0, id: "" });
const catRename = reactive({ open: false, saving: false, id: "", name: "" });

/** 「全部/未分类」是前端造的虚拟节点（key 以双下划线开头），没有实体，不给右键菜单 */
function isRealCat(key: unknown): boolean {
  const k = String(key ?? "");
  return !!k && !k.startsWith("__");
}

/** 顶栏与侧栏两个入口都走这里：开弹窗前把 parent 清掉，
 *  否则从右键「新建子分类」进来过一次后，普通新建会默默继承上次的父分类 */
function openCatModal() {
  catForm.parent = undefined;
  catModal.open = true;
}

function onCatContext(e: MouseEvent, node: any) {
  const key = String(node?.key || "");
  if (!isRealCat(key)) {
    catCtx.visible = false;
    return;
  }
  catCtx.id = key;
  // 钳在视口内：菜单高约 120px，靠右下角的节点不能让它溢出屏幕
  catCtx.x = Math.max(8, Math.min(e.clientX, window.innerWidth - 190));
  catCtx.y = Math.max(8, Math.min(e.clientY, window.innerHeight - 150));
  catCtx.visible = true;
}

function onCatCtxClick(k: string) {
  catCtx.visible = false;
  const cat = categories.value.find((c) => c.id === catCtx.id);
  if (!cat) return;
  if (k === "rename") {
    catRename.id = cat.id;
    catRename.name = cat.name;
    catRename.open = true;
    return;
  }
  if (k === "add") {
    catForm.parent = cat.id;
    catModal.open = true;
    return;
  }
  if (k === "del") {
    Modal.confirm({
      title: `删除分类「${cat.name}」？`,
      content: "其下条目退为未分类，子分类上提一级；条目本体与磁盘文件都不受影响。",
      okText: "删除",
      cancelText: "取消",
      onOk: async () => {
        try {
          await GoTools?.DocSpaceRemoveCategory(cat.id);
          Message.success("已删除分类");
          // 正停在这个分类上时不剪回「全部」的话，右面会空成一片取不到任何条目
          if (selectedCat.value[0] === cat.id) selectedCat.value = ["__all__"];
          await reload();
        } catch (e: any) {
          Message.error(`删除分类失败：${errText(e)}`);
        }
      },
    });
  }
}

async function submitCatRename() {
  const n = catRename.name.trim();
  if (!n) { Message.warning("分类名称不能为空"); return; }
  catRename.saving = true;
  try {
    await GoTools?.DocSpaceRenameCategory(catRename.id, n);
    Message.success("已重命名");
    catRename.open = false;
    await reload();
  } catch (e: any) {
    Message.error(`重命名失败：${errText(e)}`);
  } finally {
    catRename.saving = false;
  }
}

/* ---------- 条目 ---------- */
const itemModal = reactive({ open: false, saving: false });
const itemForm = reactive({ src: "local", sid: "", path: "", category: undefined as string | undefined });
function openAddItem() {
  itemForm.src = "local"; itemForm.sid = ""; itemForm.path = ""; itemForm.category = selectedCat.value[0]?.startsWith("__") ? undefined : (selectedCat.value[0] || undefined);
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
    content: `是移动不是删除：将把本机文件「${record.name}」的实体移入文档回收站（不修改内容、不进系统回收站），并取消纳管；之后可在回收站随时还原，还原时原位置被占用会报错并保留回收站实体。确认？`,
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
/** 预览：进自家文件查看器（/tools/viewer）看实体文件，md 等走查看器的渲染视图；
 *  与新建笔记同一通路：桌面开原生单例窗 / Web 走路由；已开着靠事件导航。
 *  （以前是复制 DocSpacePreviewURL 的 media 链接到剪贴板，去浏览器看） */
async function openPreview(r: DocItem) {
  if (!r.path) {
    Message.warning("该条目没有本机实体路径，无法预览");
    return;
  }
  openToolWindow(`/tools/viewer?path=${encodeURIComponent(r.path)}`, "文件查看器", { single: true });
  try { Events.Emit("viewer:open-path", { path: r.path }); } catch { /* 非 Wails 环境忽略 */ }
}

/** 压缩：把本机条目打包成 zip（复用 tools/archive 引擎，就地放同目录；进度见任务中心） */
async function compressItem(record: DocItem) {
  try {
    await GoTools?.ArchiveCompress({ op: "compress", paths: [record.path], format: "zip", level: 0, outDir: "", name: "" });
    Message.success(`已开始压缩「${record.name}」（进度见任务中心）`);
  } catch (e: any) {
    Message.error(`压缩失败：${errText(e)}`);
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

/* ---------- 顶部「更多」菜单 ---------- */
function onToolsMenu(key: string) {
  switch (key) {
    case "trash": trashOpen.value = true; break;
    case "vault": vaultOpen.value = true; break;
    case "backup": backupOpen.value = true; break;
    case "search": searchOpen.value = true; break;
    case "reload": void reload(); break;
  }
}

/* ---------- 单条操作派发（网格卡与表格行共用同一组 key） ---------- */
function onAction(key: string, row: DocRow) {
  switch (key) {
    case "preview": void openPreview(row); break;
    case "tags": openTags(row); break;
    case "versions": openVersions(row); break;
    case "moveCat": openCategoryMove([row.id], row.category || ""); break;
    case "compress": void compressItem(row); break;
    case "share": openShare(row); break;
    case "encrypt": openEncrypt(row); break;
    case "upload": openUpload(row); break;
    case "download": openDownload(row); break;
    // 单条走原有的详细说明弹窗（含“还原时原位置被占用”这类提示），不拿批量文案凑数
    case "trash": moveTrash(row); break;
    case "remove": removeItem(row); break;
  }
}

/* ---------- 批量：改分类 / 标签 / 快照 / 复制清单 / 回收站 / 移出 ---------- */
const catMoveModal = reactive<{ open: boolean; saving: boolean; ids: string[]; target: string | undefined }>({
  open: false, saving: false, ids: [], target: undefined,
});
const batchTagModal = reactive<{ open: boolean; saving: boolean; ids: string[]; add: string[]; remove: string[] }>({
  open: false, saving: false, ids: [], add: [], remove: [],
});
const snapModal = reactive<{ open: boolean; saving: boolean; ids: string[]; note: string }>({
  open: false, saving: false, ids: [], note: "",
});

function requireSel(): string[] {
  const ids = [...selected.value];
  if (!ids.length) Message.warning("先选中要批量操作的条目");
  return ids;
}

function openCategoryMove(ids: string[], current = "") {
  const pick = ids.length ? ids : requireSel();
  if (!pick.length) return;
  catMoveModal.ids = [...pick];
  // 多选时不预填：几个条目可能分属不同分类，填上任何一个都会让人误以为“已统一”
  catMoveModal.target = pick.length === 1 && current ? current : undefined;
  catMoveModal.open = true;
}

async function submitMoveCategory() {
  catMoveModal.saving = true;
  const cat = catMoveModal.target || "";
  try {
    const changed = Number((await GoTools?.DocSpaceMoveItemsCategory(catMoveModal.ids, cat)) ?? 0);
    const name = cat ? categories.value.find((c) => c.id === cat)?.name || cat : "未分类";
    Message.success(changed ? `已将 ${changed} 项移到「${name}」` : "分类未变化");
    catMoveModal.open = false;
    selected.value = [];
    await reload();
  } catch (e: any) {
    Message.error(`改分类失败：${errText(e)}`);
  } finally {
    catMoveModal.saving = false;
  }
}

function openBatchTags() {
  const ids = requireSel();
  if (!ids.length) return;
  batchTagModal.ids = ids;
  batchTagModal.add = [];
  batchTagModal.remove = [];
  batchTagModal.open = true;
}

async function submitBatchTags() {
  if (!batchTagModal.add.length && !batchTagModal.remove.length) {
    Message.warning("追加与移除至少填一项");
    return;
  }
  batchTagModal.saving = true;
  try {
    const changed = Number((await GoTools?.DocSpaceTagItems(batchTagModal.ids, batchTagModal.add, batchTagModal.remove)) ?? 0);
    Message.success(changed ? `标签已更新 ${changed} 项` : "标签未变化");
    batchTagModal.open = false;
    await reload();
  } catch (e: any) {
    Message.error(`批量标签失败：${errText(e)}`);
  } finally {
    batchTagModal.saving = false;
  }
}

function batchSnapshot() {
  const ids = requireSel();
  if (!ids.length) return;
  snapModal.ids = ids;
  snapModal.note = "";
  snapModal.open = true;
}

async function submitSnapshot() {
  snapModal.saving = true;
  try {
    const res = (await GoTools?.DocSpaceSnapshotItems(snapModal.ids, snapModal.note.trim())) as BatchResult;
    Message.success(batchTip(res, "已存快照"));
    snapModal.open = false;
    await reload();
  } catch (e: any) {
    Message.error(`批量快照失败：${errText(e)}`);
  } finally {
    snapModal.saving = false;
  }
}

/** 复制清单：名称 + 路径逐行，粘给 Excel / 终端都能用；体积汇总只算取到实体的那些 */
async function copySelectedList() {
  const set = new Set(selected.value);
  const rows = baseRows.value.filter((r) => set.has(r.id));
  if (!rows.length) { Message.warning("没有可复制的选中项"); return; }
  const size = rows.reduce((n, r) => n + (r.hasMeta && !r.missing ? r.size : 0), 0);
  const text = rows.map((r) => `${r.name}\t${r.path}`).join("\n");
  try {
    await navigator.clipboard.writeText(text);
    Message.success(`已复制 ${rows.length} 条清单（合计 ${fmtSize(size)}）`);
  } catch {
    Message.error("复制失败：浏览器拒绝访问剪贴板");
  }
}

function batchTrash() {
  const ids = requireSel();
  if (!ids.length) return;
  const miss = ids.filter((id) => missingSet.value.has(id)).length;
  Modal.confirm({
    title: `移入回收站（${ids.length} 项）`,
    content: `是移动不是删除：逐个把本机实体移入文档回收站并取消纳管，之后可在回收站还原。${miss ? `其中 ${miss} 项实体已缺失，会被跳过。` : ""}确认？`,
    okButtonProps: { status: "warning" },
    onOk: async () => {
      try {
        const res = (await GoTools?.DocSpaceTrashItems(ids)) as BatchResult;
        Message.success(batchTip(res, "已移入回收站"));
        selected.value = [];
        await reload();
      } catch (e: any) {
        Message.error(`批量移入回收站失败：${errText(e)}`);
      }
    },
  });
}

function batchRemove() {
  const ids = requireSel();
  if (!ids.length) return;
  Modal.confirm({
    title: `移出文档中心（${ids.length} 项）`,
    content: "仅取消纳管，不动磁盘上的原文件（要连实体一起收走请用「回收站」）。确认？",
    okButtonProps: { status: "danger" },
    onOk: async () => {
      try {
        const done = Number((await GoTools?.DocSpaceRemoveItems(ids)) ?? 0);
        Message.success(`已移出 ${done} 项`);
        selected.value = [];
        await reload();
      } catch (e: any) {
        Message.error(`批量移出失败：${errText(e)}`);
      }
    },
  });
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

// 右键菜单的 Esc 关闭：遮罩只能接鼠标，键盘关掉是 MySQL 对象树已有的习惯
function onCtxKeydown(e: KeyboardEvent) {
  if (e.key === "Escape" && catCtx.visible) catCtx.visible = false;
}
onMounted(() => window.addEventListener("keydown", onCtxKeydown));
onUnmounted(() => window.removeEventListener("keydown", onCtxKeydown));
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
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
  color: var(--color-text-3);
  font-size: 12px;
}
/* 库目录是一长串绝对路径：给它一个窄底色框并截断，避免把整条工具栏顶宽 */
.hint-k {
  color: var(--color-text-4);
}
.hint-path {
  display: inline-block;
  max-width: 280px;
  padding: 1px 8px;
  border-radius: var(--base-radius-small);
  background: var(--color-fill-2);
  color: var(--color-text-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: middle;
}
.mono {
  font-family: var(--code-font-family);
}
.ds-caret {
  margin-left: 2px;
  font-size: 12px;
}
/* 菜单项右侧的计数小徽标：只作提示，不参与主视觉 */
.menu-num {
  font-style: normal;
  margin-left: 6px;
  padding: 0 5px;
  font-size: 11px;
  line-height: 16px;
  color: var(--color-text-3);
  background: var(--color-fill-3);
  border-radius: var(--border-radius-full);
  font-variant-numeric: tabular-nums;
}
.body {
  flex: 1;
  min-height: 0;
  background: var(--color-bg-2);
  border: 1px solid var(--color-border-2);
  border-radius: var(--base-radius-default);
  overflow: hidden;
}
.left {
  border-right: 1px solid var(--color-border-2);
  padding: 8px 8px 12px;
  overflow: auto;
  background: var(--color-fill-1);
}
.left-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 4px 8px;
}
.left-title {
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.3px;
  color: var(--color-text-3);
}
/* 分类树：行高与圆角统一，hover 才给底色；选中用「中性底色 + 左侧短竖条」，
   与实体卡、列表项共用一套选中语言，不整块刷主色。
   修正：Arco 树的真实类名是 .arco-tree-node > .arco-tree-node-title，根本没有 .arco-tree-node-row；
   旧钩子把类名写错，导致下面这套 hover/选中底色、左侧短竖条与正文色从未生效（与 MySQL 对象树同一个坑），统一改用 .arco-tree-node-title。 */
.left :deep(.arco-tree-node) {
  margin-bottom: 2px;
}
.left :deep(.arco-tree-node-title) {
  position: relative;
  padding: 5px 8px;
  border-radius: var(--base-radius-default);
  font-size: 13px;
  color: var(--color-text-1);
  transition: background-color 0.15s, color 0.15s;
}
.left :deep(.arco-tree-node-title:hover) {
  background-color: var(--color-fill-3);
}
.left :deep(.arco-tree-node-selected .arco-tree-node-title),
.left :deep(.arco-tree-node-selected .arco-tree-node-title:hover) {
  background-color: var(--color-fill-3);
  color: var(--color-text-1);
  font-weight: 600;
}
.left :deep(.arco-tree-node-selected .arco-tree-node-title)::before {
  content: "";
  position: absolute;
  left: 1px;
  top: 50%;
  width: 3px;
  height: 14px;
  margin-top: -7px;
  border-radius: var(--base-radius-small);
  background: var(--app-select-rail);
}
/* 树节点标题：真分类名可能很长，超出给省略号并靠 tooltip 补全，
   不与右侧徽标争抢换行 */
.tree-title {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: middle;
  /* 未选中兜底：真实文本落在叶子 .tree-title 上，被 a-tooltip / arco-tree-node-title-text 逐层包裹，
     单靠从 .arco-tree-node-title 继承正文色在部分包裹层会断链、回落到近黑默认色，暗底上几乎看不见，故显式取 text-1。 */
  color: var(--color-text-1);
}
/* 分类树右键菜单：照 MySQL 对象树口径（fixed 遮罩 + fixed 定位的 a-menu），
   z-index 压过侧栏与内容区，菜单自身贴右上不溢出 */
.ctx-mask {
  position: fixed;
  inset: 0;
  z-index: 2000;
}
.ctx-menu {
  position: fixed;
  z-index: 2001;
  width: max-content;
  min-width: 150px;
  max-width: 240px;
  padding: 2px;
  background: var(--color-bg-popup);
  border: 1px solid var(--color-border-2);
  border-radius: var(--base-radius-default);
  box-shadow: 0 6px 18px rgb(0 0 0 / 16%);
}
:deep(.ctx-menu .arco-menu) {
  padding: 2px;
}
.ctx-del {
  color: rgb(var(--danger-6));
}
.right {
  padding: 10px 12px;
  overflow: auto;
}
.filterbar {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
  margin-bottom: 10px;
}
/* 搜索框吃掉剩余宽度但不无限变宽；标签筛选给个固定窄宽 */
.ds-search {
  flex: 1 1 220px;
  max-width: 320px;
}
.ds-filter-select {
  width: 150px;
}
.ds-filter-div {
  margin: 0 2px 0 0;
}
/* 筛选栏右推占位：视图切换始终贴右，不随筛选项多少漂移 */
.flex-1 {
  flex: 1;
  min-width: 0;
}
.ds-pager {
  display: flex;
  justify-content: center;
  padding: 12px 0 4px;
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
  border-radius: var(--base-radius-default);
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
