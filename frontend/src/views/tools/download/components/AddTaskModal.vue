<template>
    <a-modal :visible="visible" title="新建下载任务" :width="560" :ok-loading="submitting" ok-text="加入队列"
        cancel-text="取消" @update:visible="(v: boolean) => emit('update:visible', v)" @ok="submit"
        @cancel="emit('update:visible', false)">
        <a-form :model="form" layout="vertical" class="atm-form">
            <a-form-item field="url" label="下载链接" required>
                <a-input v-model="form.url" placeholder="http(s)://… 或 ftp://…" allow-clear>
                    <template #prefix><icon-link /></template>
                </a-input>
            </a-form-item>

            <a-row :gutter="12">
                <a-col :span="14">
                    <a-form-item field="name" label="另存为" help="留空则从链接自动推断文件名">
                        <a-input v-model="form.name" placeholder="可选，指定文件名与扩展名" allow-clear />
                    </a-form-item>
                </a-col>
                <a-col :span="10">
                    <a-form-item field="connections" label="连接数">
                        <a-input-number v-model="form.connections" :min="1" :max="16" hide-button />
                    </a-form-item>
                </a-col>
            </a-row>

            <a-form-item field="saveDir" :help="saveDirHelp">
                <template #label>
                    <span>保存目录</span>
                </template>
                <a-input-group compact>
                    <a-input v-model="form.saveDir" placeholder="绝对路径，留空则用默认下载目录" style="width: calc(100% - 72px)"
                        allow-clear />
                    <a-button style="width: 72px" @click="chooseDir">浏览</a-button>
                </a-input-group>
            </a-form-item>

            <a-form-item field="proxy" label="代理" help="可选，形如 http://127.0.0.1:7890（下次下载生效）">
                <a-input v-model="form.proxy" placeholder="留空不走代理" allow-clear />
            </a-form-item>

            <a-form-item field="mirrors" label="备用镜像源" help="每行一个 URL，多源时可手动切换">
                <a-textarea v-model="form.mirrors" :auto-size="{ minRows: 2, maxRows: 5 }"
                    placeholder="https://mirror-2.example.com/file.zip&#10;https://mirror-3.example.com/file.zip" />
            </a-form-item>

            <a-form-item field="autoStart">
                <a-switch v-model="form.autoStart" type="round" />
                <span class="atm-hint">{{ form.autoStart ? '创建后立即开始下载' : '仅加入队列，手动开始' }}</span>
            </a-form-item>
        </a-form>
    </a-modal>
</template>

<script setup lang="ts">
/**
 * 新建下载任务弹窗。只收集参数并调 DownloadAdd，落地/分块/续传全在引擎；
 * 成功后 emit('created', task) 让父页即时把新任务并进展位（不依赖事件回环的时序）。
 */
import { getCurrentInstance, computed, reactive, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";

const props = defineProps<{ visible: boolean }>();
const emit = defineEmits<{
    (e: "update:visible", v: boolean): void;
    (e: "created", task: any): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const submitting = ref(false);
// 当前生效的全局默认下载目录（设置页“下载目录”，留空则应用目录/downloads），弹窗打开时拉一次用于提示
const defaultDir = ref("");
const saveDirHelp = computed(() =>
    defaultDir.value ? `留空存到默认下载目录：${defaultDir.value}` : "留空存到默认下载目录（可在“设置-目录”中修改）");

const form = reactive<any>({
    url: "",
    name: "",
    saveDir: "",
    connections: 4,
    proxy: "",
    mirrors: "",
    autoStart: true,
});

// 每次打开复位链接之外的敏感项，保留上次的连接数/目录等偏好，减少重复录入；并刷新默认目录提示
watch(() => props.visible, (v) => {
    if (v) {
        form.url = "";
        try {
            const p = GoTools?.DownloadDefaultDir?.();
            if (p && typeof (p as any).then === "function") {
                (p as Promise<string>).then((d) => { defaultDir.value = String(d || ""); }).catch(() => { /* */ });
            } else if (p) {
                defaultDir.value = String(p);
            }
        } catch { /* 非桌面环境忽略 */ }
    }
});

async function chooseDir() {
    try {
        const dir = await GoTools?.ChooseDir("选择下载保存目录");
        if (dir) form.saveDir = String(dir);
    } catch (e: any) {
        // 无原生窗口的面板模式：给出手填提示，不静默失败
        Message.warning({ content: String(e?.message || e || "当前环境不支持目录选择，请手动填写路径"), position: "top" });
    }
}

async function submit() {
    const url = form.url.trim();
    if (!/^https?:\/\//i.test(url) && !/^ftp:\/\//i.test(url)) {
        Message.warning("请输入 http(s):// 或 ftp:// 开头的链接");
        return;
    }
    const mirrors = form.mirrors.split(/\r?\n/).map((s) => s.trim()).filter(Boolean);
    submitting.value = true;
    try {
        const task = await GoTools.DownloadAdd(
            url,
            form.name.trim(),
            form.saveDir.trim(),
            form.connections || 4,
            form.proxy.trim(),
            mirrors.length ? mirrors : null,
            0,          // scheduledAt：计划任务属后续 PR，这里恒 0
            !!form.autoStart,
        );
        Message.success("已" + (form.autoStart ? "开始下载" : "加入队列"));
        emit("created", task);
        emit("update:visible", false);
    } catch (e: any) {
        Message.error(String(e?.message || e || "创建任务失败"));
    } finally {
        submitting.value = false;
    }
}
</script>

<style scoped>
.atm-form :deep(.arco-form-item) {
    margin-bottom: 12px;
}

.atm-hint {
    margin-left: 10px;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}
</style>
