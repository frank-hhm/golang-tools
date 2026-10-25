<template>
    <!-- 加速站与仓库管理：自己读写 daemon.json 与仓库登记，改完通知页面刷新列表 -->
    <a-modal :visible="visible" title="加速站与仓库管理" :width="820" :mask-closable="false"
        @cancel="onVisible(false)" @update:visible="onVisible">
        <div class="set-wrap">
            <!-- 单选组替代 tabs：两个视角高度一致，切来切去不跳 -->
            <a-radio-group v-model="tab" type="button" size="medium" class="set-switch">
                <a-radio value="mirror"><icon-link />加速站 / 仓库地址</a-radio>
                <a-radio value="registry"><icon-storage />私有仓库 / Hub 账号</a-radio>
            </a-radio-group>

            <div class="set-body">
                <!-- ---------- 加速站与不安全仓库 ---------- -->
                <div v-show="tab === 'mirror'" class="set-pane">
                    <a-alert type="normal" class="setup-alert">
                        写入 <span class="mono">{{ daemonCfg.file || '~/.docker/daemon.json' }}</span>，只改
                        <span class="mono">registry-mirrors</span> 与 <span class="mono">insecure-registries</span>
                        两项（其余键原样保留，改前备份 .gobak）。保存后需重启引擎才生效。
                    </a-alert>

                    <div class="set-label">镜像加速站（拉不到镜像时先配这里）</div>
                    <mirror-table v-model="mirrorDraft" />

                    <div class="set-label">私有仓库地址（http / 自签证书仓库需先登记到这里）</div>
                    <div class="chip-wrap">
                        <a-tag v-for="(m, i) in insecureDraft" :key="'i' + i" closable color="orange" class="mono"
                            @close="insecureDraft.splice(i, 1)">{{ m }}</a-tag>
                        <span v-if="!insecureDraft.length" class="dim">未配置</span>
                    </div>
                    <a-space :size="8" class="set-row">
                        <a-input v-model="insecureInput" size="small" style="width: 320px" class="mono"
                            placeholder="10.0.0.5:5000" @press-enter="addInsecure" />
                        <a-button size="small" @click="addInsecure">
                            <template #icon><icon-plus /></template>添加
                        </a-button>
                    </a-space>

                    <a-typography-text type="secondary" class="set-tip">
                        名单里的仓库允许走 http 或自签证书；改完若不重启引擎，新配置不会生效。
                    </a-typography-text>
                </div>

                <!-- ---------- 私有仓库 / Hub 账号 ---------- -->
                <div v-show="tab === 'registry'" class="set-pane">
                    <!-- scroll.x 必须有，否则操作列的 fixed="right" 不生效 -->
                    <a-table :data="registries" :pagination="false" size="small" row-key="id"
                        :scroll="{ x: '100%' }">
                        <template #columns>
                            <a-table-column title="名称" :width="150" ellipsis tooltip />
                            <a-table-column title="地址" data-index="url" ellipsis tooltip />
                            <a-table-column title="账号" :width="130">
                                <template #cell="{ record }">
                                    <span>{{ record.user || '匿名' }}</span>
                                    <a-tag v-if="record.hasPass" size="small" color="green">已存密码</a-tag>
                                </template>
                            </a-table-column>
                            <a-table-column title="命名空间" data-index="namespace" :width="110" ellipsis tooltip />
                            <!-- 固定列宽度按实际按钮排布给足：测试 / 编辑 / 删除（加载态不折行）；靠右对齐 -->
                            <a-table-column title="操作" :width="220" fixed="right" align="right">
                                <template #cell="{ record }">
                                    <a-space :size="2">
                                        <a-button size="small" :loading="regTesting === record.id"
                                            @click="testRegistry(record)">
                                            <template #icon><icon-link /></template>测试
                                        </a-button>
                                        <a-button size="small" @click="editRegistry(record)">
                                            <template #icon><icon-edit /></template>编辑
                                        </a-button>
                                        <a-popconfirm :content="`删除仓库配置「${record.name}」？`" type="warning"
                                            position="lt" @ok="removeRegistry(record)">
                                            <a-button size="small" status="danger">
                                                <template #icon><icon-delete /></template>删除
                                            </a-button>
                                        </a-popconfirm>
                                    </a-space>
                                </template>
                            </a-table-column>
                        </template>
                    </a-table>

                    <a-divider :margin="12" />
                    <a-form :model="regForm" layout="vertical">
                        <a-row :gutter="12">
                            <a-col :span="12">
                                <a-form-item field="name" label="仓库名（显示用）">
                                    <a-input v-model="regForm.name" placeholder="公司 Harbor" allow-clear />
                                </a-form-item>
                            </a-col>
                            <a-col :span="12">
                                <a-form-item field="url" label="仓库地址">
                                    <a-input v-model="regForm.url" class="mono" placeholder="https://harbor.corp.local"
                                        allow-clear />
                                </a-form-item>
                            </a-col>
                        </a-row>
                        <a-row :gutter="12">
                            <a-col :span="8">
                                <a-form-item field="user" label="用户名（空=匿名）">
                                    <a-input v-model="regForm.user" allow-clear />
                                </a-form-item>
                            </a-col>
                            <a-col :span="8">
                                <a-form-item field="password" label="密码">
                                    <a-input-password v-model="regForm.password" :placeholder="regPassPlaceholder"
                                        allow-clear />
                                </a-form-item>
                            </a-col>
                            <a-col :span="8">
                                <a-form-item field="namespace" label="默认命名空间">
                                    <a-input v-model="regForm.namespace" placeholder="如 library / 项目名" allow-clear />
                                </a-form-item>
                            </a-col>
                        </a-row>
                        <a-form-item field="note" label="备注">
                            <a-input v-model="regForm.note" placeholder="用途、负责人、限制…" allow-clear />
                        </a-form-item>
                    </a-form>
                </div>
            </div>
        </div>

        <!-- 主动作固定到底部：内容区怎么滚都不影响它 -->
        <template #footer>
            <div class="set-foot">
                <a-typography-text type="secondary" class="set-hint">
                    <template v-if="tab === 'mirror'">
                        引擎当前{{ daemonCfg.engineUp ? '在运行' : '未运行' }}；重启会断开容器（除非重新启动它们）
                    </template>
                    <template v-else>密码用 Windows DPAPI 加密保存在本机，明文不出后端</template>
                </a-typography-text>
                <a-space :size="8">
                    <template v-if="tab === 'mirror'">
                        <a-button size="small" @click="onVisible(false)">关闭</a-button>
                        <a-button size="small" :loading="daemonSaving" @click="saveDaemon(false)">保存</a-button>
                        <a-button size="small" status="warning" :loading="daemonSaving" @click="saveDaemon(true)">
                            保存并重启引擎
                        </a-button>
                    </template>
                    <template v-else>
                        <a-button v-if="regForm.id" size="small" @click="resetRegForm">放弃编辑</a-button>
                        <a-button v-if="regForm.id && regHasPass" size="small" status="danger" :loading="regSaving"
                            @click="clearRegPassword">清除密码</a-button>
                        <a-button size="small" :loading="regSaving" @click="saveRegistry">
                            {{ regForm.id ? '保存修改' : '新增仓库' }}
                        </a-button>
                    </template>
                </a-space>
            </div>
        </template>
    </a-modal>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, reactive, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import MirrorTable from "./MirrorTable.vue";

const props = defineProps<{ visible: boolean }>();

const emit = defineEmits<{
    (e: "update:visible", v: boolean): void;
    /** 仓库列表有增删改：页面要重取一份，推送弹窗的默认目标靠它 */
    (e: "changed"): void;
    /** 引擎被重启过：页面要重探引擎并刷新镜像列表 */
    (e: "engine-changed"): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

// 单选组替代 tabs：两个视角共用同一块固定高度内容区
const tab = ref<"mirror" | "registry">("mirror");

const daemonCfg = ref<any>({ file: "", mirrors: [], insecure: [], engineUp: false });
const mirrorDraft = ref<string[]>([]);
const insecureDraft = ref<string[]>([]);
const insecureInput = ref("");
const daemonSaving = ref(false);

const registries = ref<any[]>([]);
const regTesting = ref("");
const regSaving = ref(false);
const regHasPass = ref(false);
const regForm = reactive({ id: "", name: "", url: "", user: "", password: "", namespace: "", note: "" });

const regPassPlaceholder = computed(() => (regHasPass.value ? "已保存（留空不修改）" : "密码（DPAPI 加密保存）"));

watch(
    () => props.visible,
    (v) => {
        if (!v) return;
        loadDaemon();
        loadRegistries();
    },
    { immediate: true }
);

function onVisible(v: boolean) {
    emit("update:visible", v);
}

async function loadDaemon() {
    try {
        daemonCfg.value = (await GoTools.DockerSrvDaemonConfig()) || { file: "", mirrors: [], insecure: [] };
        mirrorDraft.value = [...(daemonCfg.value.mirrors || [])];
        insecureDraft.value = [...(daemonCfg.value.insecure || [])];
    } catch (e: any) {
        Message.warning("读取 daemon.json 失败: " + (e?.message || e));
    }
}

async function loadRegistries() {
    try {
        registries.value = (await GoTools.DockerSrvListRegistries()) || [];
    } catch (e: any) {
        Message.warning("读取仓库列表失败: " + (e?.message || e));
    }
}

function addInsecure() {
    const v = insecureInput.value.trim().replace(/^https?:\/\//i, "").replace(/\/+$/, "");
    if (!v) return;
    if (v.includes("/")) { Message.warning("这里只填 主机[:端口]，既不带协议也不带路径"); return; }
    if (!insecureDraft.value.includes(v)) insecureDraft.value.push(v);
    insecureInput.value = "";
}

async function saveDaemon(restart: boolean) {
    daemonSaving.value = true;
    try {
        const file = await GoTools.DockerSrvSetDaemonConfig(mirrorDraft.value, insecureDraft.value);
        daemonCfg.value = {
            ...daemonCfg.value,
            file: file || daemonCfg.value.file,
            mirrors: [...mirrorDraft.value],
            insecure: [...insecureDraft.value],
        };
        Message.success("已写入 " + (file || "daemon.json"));
        if (restart) await restartEngine();
        else Message.info("重启引擎后生效");
    } catch (e: any) {
        Message.error("保存失败: " + (e?.message || e));
    }
    daemonSaving.value = false;
}

/** 重启引擎：探活与列表刷新是页面的活，这里只通报一声 */
async function restartEngine() {
    daemonSaving.value = true;
    try {
        await GoTools.DockerSrvRestartEngine();
        Message.success("引擎已重启，新配置已生效");
    } catch (e: any) {
        Message.error("重启引擎失败: " + (e?.message || e));
    }
    daemonSaving.value = false;
    emit("engine-changed");
}

function resetRegForm() {
    regForm.id = "";
    regForm.name = "";
    regForm.url = "";
    regForm.user = "";
    regForm.password = "";
    regForm.namespace = "";
    regForm.note = "";
    regHasPass.value = false;
}

function editRegistry(rec: any) {
    regForm.id = rec.id;
    regForm.name = rec.name || "";
    regForm.url = rec.url || "";
    regForm.user = rec.user || "";
    regForm.password = "";
    regForm.namespace = rec.namespace || "";
    regForm.note = rec.note || "";
    regHasPass.value = !!rec.hasPass;
}

function regPayload() {
    return {
        id: regForm.id,
        name: regForm.name.trim(),
        url: regForm.url.trim(),
        user: regForm.user.trim(),
        namespace: regForm.namespace.trim(),
        note: regForm.note.trim(),
    };
}

async function saveRegistry() {
    const p = regPayload();
    if (!p.name) { Message.warning("请填写仓库名"); return; }
    if (!p.url) { Message.warning("请填写仓库地址，如 https://harbor.corp.local"); return; }
    regSaving.value = true;
    try {
        const saved = await GoTools.DockerSrvSaveRegistry(p, regForm.password, false);
        Message.success(`仓库「${saved?.name || p.name}」已保存`);
        resetRegForm();
        await loadRegistries();
        emit("changed");
    } catch (e: any) {
        Message.error("保存失败: " + (e?.message || e));
    }
    regSaving.value = false;
}

/** 清除已保存的密码：只清密码，仓库其它字段与地址保持不变 */
async function clearRegPassword() {
    if (!regForm.id) return;
    regSaving.value = true;
    try {
        await GoTools.DockerSrvSaveRegistry(regPayload(), "", true);
        regHasPass.value = false;
        Message.success("密码已清除");
        await loadRegistries();
        emit("changed");
    } catch (e: any) {
        Message.error("清除失败: " + (e?.message || e));
    }
    regSaving.value = false;
}

async function removeRegistry(rec: any) {
    try {
        await GoTools.DockerSrvDeleteRegistry(rec.id);
        Message.success("仓库已删除");
        if (regForm.id === rec.id) resetRegForm();
        await loadRegistries();
        emit("changed");
    } catch (e: any) {
        Message.error("删除失败: " + (e?.message || e));
    }
}

async function testRegistry(rec: any) {
    regTesting.value = rec.id;
    try {
        const msg = await GoTools.DockerSrvTestRegistry(rec.id);
        Message.success(msg || "连接正常");
    } catch (e: any) {
        Message.error(String(e?.message || e));
    }
    regTesting.value = "";
}
</script>

<style scoped>
/* 两个视角共用一块固定高度内容区：切来切去不跳动 */
.set-wrap {
    display: flex;
    flex-direction: column;
}

.set-switch {
    margin-bottom: 12px;
    align-self: flex-start;
}

.set-body {
    height: 46vh;
    min-height: 280px;
    max-height: 460px;
    overflow-y: auto;
    padding-right: 4px;
}

.set-pane {
    display: flex;
    flex-direction: column;
}

.set-label {
    margin: var(--base-margin) 0 6px;
    font-size: var(--base-size-small);
    color: var(--color-text-2);
}

.chip-wrap {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    min-height: 24px;
}

.set-row {
    margin-top: 8px;
}

.set-tip {
    display: block;
    margin-top: 12px;
    font-size: var(--base-size-small);
}

/* 主动作行：左边说明、右边按钮，固定不随内容滚动 */
.set-foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    width: 100%;
}

.set-hint {
    flex: 1;
    min-width: 0;
    font-size: var(--base-size-small);
    text-align: left;
}

.setup-alert {
    margin-bottom: var(--base-radius-default);
}

.dim {
    color: var(--color-text-3);
}

.mono {
    font-family: Consolas, monospace;
}
</style>
