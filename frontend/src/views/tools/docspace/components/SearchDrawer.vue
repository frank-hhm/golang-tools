<template>
  <a-drawer
    :visible="visible"
    :width="620"
    title="智能搜索"
    unmount-on-close
    @update:visible="(v: boolean) => emit('update:visible', v)"
    @cancel="close"
  >
    <a-tabs default-active-key="meta" type="rounded">
      <!-- ==================== A：纳管条目按条件过滤 ==================== -->
      <a-tab-pane key="meta" title="纳管筛选">
        <a-form :model="form" layout="vertical">
          <a-form-item field="keyword" label="关键词（名称 / 路径 / 标签）">
            <a-input v-model="form.keyword" placeholder="大小写不敏感，留空则只按下面条件筛" allow-clear />
          </a-form-item>
          <a-form-item field="category" label="分类">
            <a-space :size="12">
              <a-select v-model="form.category" placeholder="全部分类" allow-clear style="width: 220px">
                <a-option v-for="c in categories" :key="c.id" :value="c.id" :label="c.name" />
              </a-select>
              <a-checkbox v-model="form.includeSub" :disabled="!form.category">含子分类</a-checkbox>
            </a-space>
          </a-form-item>
          <a-form-item field="tags" label="标签（同时满足）">
            <a-select v-model="form.tags" multiple allow-clear placeholder="选择标签，AND 关系" style="width: 100%">
              <a-option v-for="t in allTags" :key="t" :value="t" :label="t" />
            </a-select>
          </a-form-item>
          <a-form-item label="其它条件">
            <a-space wrap :size="12">
              <a-checkbox v-model="form.hasVersions">存过版本快照</a-checkbox>
              <a-select v-model="form.updatedWithinDays" size="small" style="width: 150px">
                <a-option :value="0" label="更新时间：不限" />
                <a-option :value="7" label="最近 7 天" />
                <a-option :value="30" label="最近 30 天" />
                <a-option :value="90" label="最近 90 天" />
              </a-select>
              <a-input-number
                v-model="form.minMB"
                size="small"
                :min="0"
                :step="1"
                placeholder="≥"
                style="width: 100px"
              >
                <template #suffix>MB</template>
              </a-input-number>
              <span class="dim">~</span>
              <a-input-number
                v-model="form.maxMB"
                size="small"
                :min="0"
                :step="1"
                placeholder="≤"
                style="width: 100px"
              >
                <template #suffix>MB</template>
              </a-input-number>
            </a-space>
            <div class="dim hintline">
              大小条件只对「本机 local」条目判定（逐个 os.Stat）；sftp 等远端条目取不到大小会被排除，不猜。
            </div>
          </a-form-item>
          <a-space>
            <a-button type="primary" :loading="metaBusy" @click="runMeta">搜索</a-button>
            <a-button @click="resetMeta">重置</a-button>
          </a-space>
        </a-form>
      </a-tab-pane>

      <!-- ==================== B：全盘 / 内容检索，命中可纳管 ==================== -->
      <a-tab-pane key="disk" title="磁盘检索">
        <a-form :model="disk" layout="vertical">
          <a-form-item field="root" label="目录（该目录及其子树内查找）">
            <a-input v-model="disk.root" placeholder="如 D:\docs 或 /srv/files" allow-clear />
          </a-form-item>
          <a-form-item field="kw" label="关键词">
            <a-space>
              <a-input v-model="disk.kw" placeholder="按文件名或文件内容" allow-clear style="width: 240px" />
              <a-checkbox v-model="disk.includeSub">含子目录</a-checkbox>
            </a-space>
          </a-form-item>
          <a-space>
            <a-button :loading="disk.busy === 'name'" @click="runDisk(false)">按文件名</a-button>
            <a-button :loading="disk.busy === 'content'" @click="runDisk(true)">按内容</a-button>
            <span class="dim">内容检索跳过二进制与超大文件，最多 200 条</span>
          </a-space>
        </a-form>

        <a-divider style="margin: 12px 0" />
        <a-empty v-if="!disk.hits.length && !disk.busy" :image="false" description="尚无检索结果" />
        <a-list v-else :bordered="false" :loading="!!disk.busy">
          <a-list-item v-for="h in disk.hits" :key="h.path">
            <div class="drow">
              <div class="dmain">
                <div class="dname">{{ h.name }}<span v-if="h.line" class="dim"> : {{ h.line }}</span></div>
                <div class="dpath mono" :title="h.text || h.path">{{ h.text || h.path }}</div>
              </div>
              <a-space class="dops">
                <a-link
                  v-if="!h.isDir"
                  :disabled="managed.has(h.path)"
                  @click="manage(h)"
                >{{ managed.has(h.path) ? "已纳管" : "纳管" }}</a-link>
                <span v-else class="dim">目录</span>
              </a-space>
            </div>
          </a-list-item>
        </a-list>
      </a-tab-pane>
    </a-tabs>
  </a-drawer>
</template>

<script setup lang="ts">
/**
 * 智能搜索抽屉（P1 顺延项）：
 * A 页按元数据条件过滤纳管条目（DocSpaceSearch，后端纯内存 + local stat）；
 * B 页复用 DiskSearch/DiskSearchContent 全盘检索，命中一键纳管——不建独立索引、
 * 不引全文检索引擎（计划 §一），闭环靠「检索 → 纳管 → 再筛选」完成。
 */
import { getCurrentInstance, reactive, ref } from "vue";
import { Message } from "@arco-design/web-vue";
import { errText, type Category, type DocItem, type SearchFilter } from "../types";

const props = defineProps<{ visible: boolean; categories: Category[]; allTags: string[] }>();
const emit = defineEmits<{
  (e: "update:visible", v: boolean): void;
  (e: "searched", items: DocItem[]): void;
  (e: "changed"): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

/* ---------- A：纳管筛选 ---------- */
const form = reactive({
  keyword: "",
  category: "" as string,
  includeSub: true,
  tags: [] as string[],
  hasVersions: false,
  updatedWithinDays: 0,
  minMB: undefined as number | undefined,
  maxMB: undefined as number | undefined,
});
const metaBusy = ref(false);

async function runMeta() {
  metaBusy.value = true;
  try {
    const f: SearchFilter = {
      keyword: form.keyword.trim(),
      category: form.category || "",
      excludeSubCats: !!form.category && !form.includeSub,
      tags: form.tags || [],
      hasVersions: form.hasVersions,
      updatedWithinDays: form.updatedWithinDays || 0,
      minSizeBytes: form.minMB && form.minMB > 0 ? Math.round(form.minMB * 1024 * 1024) : 0,
      maxSizeBytes: form.maxMB && form.maxMB > 0 ? Math.round(form.maxMB * 1024 * 1024) : 0,
      limit: 0,
    };
    const items = ((await GoTools?.DocSpaceSearch(f)) as DocItem[]) || [];
    emit("searched", items);
    emit("update:visible", false);
    Message.success(`命中 ${items.length} 条纳管条目`);
  } catch (e: any) {
    Message.error(`搜索失败：${errText(e)}`);
  } finally {
    metaBusy.value = false;
  }
}

function resetMeta() {
  form.keyword = "";
  form.category = "";
  form.includeSub = true;
  form.tags = [];
  form.hasVersions = false;
  form.updatedWithinDays = 0;
  form.minMB = undefined;
  form.maxMB = undefined;
}

/* ---------- B：磁盘检索 ---------- */
interface DiskHit {
  name: string;
  path: string;
  isDir: boolean;
  line?: number;
  text?: string;
}

const disk = reactive({
  root: "",
  kw: "",
  includeSub: true,
  busy: "" as "" | "name" | "content",
  hits: [] as DiskHit[],
});
/** 已纳管路径集合（小写归一），避免重复纳管按钮误点 */
const managed = ref<Set<string>>(new Set());

async function runDisk(byContent: boolean) {
  const root = disk.root.trim();
  const kw = disk.kw.trim();
  if (!root || !kw) {
    Message.warning("目录和关键词都要填");
    return;
  }
  disk.busy = byContent ? "content" : "name";
  try {
    if (byContent) {
      const ms = ((await GoTools?.DiskSearchContent(root, kw, disk.includeSub)) as any[]) || [];
      const seen = new Set<string>();
      const hits: DiskHit[] = [];
      for (const m of ms) {
        if (seen.has(m.path)) continue; // 每文件只列首个命中行
        seen.add(m.path);
        hits.push({ name: m.name, path: m.path, isDir: false, line: m.line, text: `${m.name}:${m.line}  ${m.text}` });
      }
      disk.hits = hits;
    } else {
      const es = ((await GoTools?.DiskSearch(root, kw, disk.includeSub)) as any[]) || [];
      disk.hits = es.map((e) => ({ name: e.name, path: e.path, isDir: !!e.isDir }));
    }
    // 刷新已纳管标记
    try {
      const items = ((await GoTools?.DocSpaceItems()) as DocItem[]) || [];
      managed.value = new Set(items.map((it) => it.path.replace(/[\\/]+/g, "/").toLowerCase()));
    } catch {
      /* 标记失败不影响检索 */
    }
    if (!disk.hits.length) Message.info("没有命中");
  } catch (e: any) {
    Message.error(`检索失败：${errText(e)}`);
  } finally {
    disk.busy = "";
  }
}

async function manage(h: DiskHit) {
  try {
    await GoTools?.DocSpaceAddItem("local", "", h.path, "");
    managed.value = new Set(managed.value).add(h.path.replace(/[\\/]+/g, "/").toLowerCase());
    Message.success(`已纳管「${h.name}」`);
    emit("changed");
  } catch (e: any) {
    Message.error(`纳管失败：${errText(e)}`);
  }
}

function close() {
  emit("update:visible", false);
}
</script>

<style scoped lang="less">
.dim {
  color: var(--color-text-3);
  font-size: 12px;
}
.hintline {
  margin-top: 4px;
}
.mono {
  font-family: var(--code-font-family);
}
.drow {
  display: flex;
  width: 100%;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.dmain {
  min-width: 0;
}
.dname {
  font-weight: 600;
}
.dpath {
  color: var(--color-text-3);
  font-size: 12px;
  max-width: 420px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
