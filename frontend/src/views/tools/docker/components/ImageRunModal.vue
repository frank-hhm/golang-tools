<template>
    <!-- 由镜像创建容器：表单自己管，点确定把最终值抛给页面去调接口 -->
    <a-modal :visible="visible" title="创建容器" :width="520" :mask-closable="false" :on-before-ok="submit"
        @cancel="onVisible(false)" @update:visible="onVisible">
        <a-form :model="form" layout="vertical">
            <a-form-item field="image" label="镜像">
                <a-input v-model="form.image" class="mono" allow-clear />
            </a-form-item>
            <a-form-item field="name" label="容器名（留空自动生成）">
                <a-input v-model="form.name" placeholder="仅字母/数字/下划线/中划线" />
            </a-form-item>
            <a-row :gutter="12">
                <a-col :span="12">
                    <a-form-item field="containerPort" label="容器端口（0=不发布）">
                        <a-input-number v-model="form.containerPort" :min="0" :max="65535" style="width: 100%" />
                    </a-form-item>
                </a-col>
                <a-col :span="12">
                    <a-form-item field="hostPort" label="宿主机端口（0=随机）" :error="hostPortError">
                        <a-input-number v-model="form.hostPort" :min="0" :max="65535" style="width: 100%"
                            @change="hostPortError = ''" />
                    </a-form-item>
                </a-col>
            </a-row>
            <a-form-item field="env" label="环境变量（每行一个 KEY=VALUE）">
                <a-textarea v-model="form.env" :auto-size="{ minRows: 2, maxRows: 5 }"
                    placeholder="MYSQL_ROOT_PASSWORD=123456" />
            </a-form-item>
            <a-checkbox v-model="form.autoStart">创建后立即启动</a-checkbox>
        </a-form>
    </a-modal>
</template>

<script setup lang="ts">
import { getCurrentInstance, reactive, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import type { RunForm } from "../helpers";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const props = defineProps<{
    visible: boolean;
    /** 打开时要填进「镜像」的引用 */
    image: string;
}>();

const emit = defineEmits<{
    (e: "update:visible", v: boolean): void;
    (e: "submit", form: RunForm): void;
}>();

const form = reactive<RunForm>({
    image: "",
    name: "",
    containerPort: 0,
    hostPort: 0,
    env: "",
    autoStart: true,
});

/** 宿主机端口占用提示（被实例登记或进程监听），贴表单项底下 */
const hostPortError = ref("");

/** 常见镜像的默认端口：填好只是为了少查一次文档，用户随时可改 */
function guessPort(ref: string): number {
    const r = ref.toLowerCase();
    if (r.includes("mysql") || r.includes("mariadb")) return 3306;
    if (r.includes("redis")) return 6379;
    if (r.includes("postgres")) return 5432;
    if (r.includes("mongo")) return 27017;
    if (r.includes("nginx") || r.includes("httpd")) return 80;
    return 0;
}

// 每次打开都按新镜像重置：上一次填的容器名/变量不该跟着串到下一个镜像上
watch(
    () => props.visible,
    (v) => {
        if (!v) return;
        form.image = props.image;
        form.name = "";
        const p = guessPort(props.image);
        form.containerPort = p;
        form.hostPort = p;
        form.env = "";
        form.autoStart = true;
        hostPortError.value = "";
        // 宿主机端口默认也要能用：按镜像惯例的容器端口去找一个没被占的宿主机端口
        if (p > 0) {
            GoTools?.DockerSuggestPort?.(p)
                .then((free: number) => {
                    // 期间用户可能已手动改过，只在本轮仍是默认值时替换
                    if (free && free !== p && form.hostPort === p) form.hostPort = free;
                })
                .catch(() => { /* 探测失败就留默认值 */ });
        }
    },
    { immediate: true }
);

function onVisible(v: boolean) {
    emit("update:visible", v);
}

/** 表单不合格就拦在弹窗里（返回 false 不关），合格才交给页面去拉镜像/建容器 */
async function submit(): Promise<boolean> {
    const image = form.image.trim();
    if (!image) {
        Message.warning("镜像不能为空");
        return false;
    }
    const name = form.name.trim();
    if (name && !/^[a-zA-Z0-9][a-zA-Z0-9_.-]*$/.test(name)) {
        Message.warning("容器名只能包含字母/数字/下划线/中划线/点");
        return false;
    }
    // 宿主机端口被占就不用提交了，Docker 启动时才报“port is already allocated”更难懂
    if (form.hostPort > 0) {
        try {
            const reason = await GoTools?.DockerPortTaken?.(form.hostPort);
            if (reason) {
                let suggest = form.hostPort;
                try { suggest = (await GoTools.DockerSuggestPort(form.hostPort)) || suggest; } catch (e) { /* ignore */ }
                hostPortError.value = suggest !== form.hostPort ? `${reason}，可用：${suggest}` : reason;
                Message.warning(hostPortError.value);
                return false;
            }
        } catch (e) { /* 探测失败不阻断，交给 docker 自己报错 */ }
    }
    emit("submit", { ...form, image, name });
    return true;
}
</script>
