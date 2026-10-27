<template>
    <!-- 新增/编辑连接：从主页面拆出，本组件只持有表单与「测试连接」交互，保存动作回传给页面统一落库 -->
    <a-modal :visible="visible" :title="editing ? '编辑连接' : '添加新连接'" :width="460"
        @update:visible="(v: boolean) => emit('update:visible', v)" @ok="save">
        <a-form :model="connForm" layout="vertical">
            <a-form-item field="name" label="连接名称">
                <a-input v-model="connForm.name" placeholder="例如 127.0.0.1" />
            </a-form-item>
            <a-row :gutter="12">
                <a-col :span="16">
                    <a-form-item field="host" label="主机">
                        <a-input v-model="connForm.host" placeholder="127.0.0.1" />
                    </a-form-item>
                </a-col>
                <a-col :span="8">
                    <a-form-item field="port" label="端口">
                        <a-input-number v-model="connForm.port" :min="1" :max="65535" />
                    </a-form-item>
                </a-col>
            </a-row>
            <a-row :gutter="12">
                <a-col :span="16">
                    <a-form-item field="password" label="密码">
                        <a-input-password v-model="connForm.password" placeholder="无密码留空" />
                    </a-form-item>
                </a-col>
                <a-col :span="8">
                    <a-form-item field="db" label="数据库">
                        <a-input-number v-model="connForm.db" :min="0" :max="255" />
                    </a-form-item>
                </a-col>
            </a-row>
            <a-form-item label="保存位置">
                <a-radio-group v-model="saveTarget" type="button" size="medium">
                    <a-radio value="file"><icon-save /> 文件</a-radio>
                    <a-radio value="cache"><icon-storage /> 缓存</a-radio>
                </a-radio-group>
                <template #extra>
                    <div>文件：保存到用户配置目录，跨会话持久；缓存：保存到本地 localStorage。</div>
                </template>
            </a-form-item>
        </a-form>
        <template #footer>
            <div class="conn-test-footer">
                <div class="conn-test-left">
                    <a-button size="small" :loading="connTesting" @click="doTestConnection">测试连接</a-button>
                    <span v-if="connTestResult"
                        :class="['conn-test-msg', connTestStatus === 'ok' ? 'is-ok' : 'is-err']">{{ connTestResult }}</span>
                </div>
                <a-space>
                    <a-button @click="emit('update:visible', false)">取消</a-button>
                    <a-button type="primary" @click="save">保存</a-button>
                </a-space>
            </div>
        </template>
    </a-modal>
</template>

<script setup lang="ts">
import { getCurrentInstance, reactive, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import type { ConnConfig } from "../helpers";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const props = defineProps<{
    visible: boolean;
    editing: boolean;
    /** 编辑时带入的已有连接；新增传 null */
    conn: ConnConfig | null;
}>();

const emit = defineEmits<{
    (e: "update:visible", v: boolean): void;
    (e: "save", conn: ConnConfig, saveTarget: string): void;
}>();

const connForm = reactive<ConnConfig>({ id: "", name: "", host: "127.0.0.1", port: 6379, password: "", db: 0 });
const saveTarget = ref("file");

/* 测试连接：按当前表单参数临时建连（含密码/DB 验证）后立刻断开，结果就地提示 */
const connTesting = ref(false);
const connTestResult = ref("");
const connTestStatus = ref<"ok" | "err" | "">("");
function resetConnTest() {
    connTesting.value = false;
    connTestResult.value = "";
    connTestStatus.value = "";
}

// 每次打开按编辑/新增重置表单：编辑带入原值，新增回默认
watch(() => props.visible, (open) => {
    if (!open) return;
    resetConnTest();
    if (props.editing && props.conn) Object.assign(connForm, { ...props.conn });
    else Object.assign(connForm, { id: "", name: "", host: "127.0.0.1", port: 6379, password: "", db: 0 });
});

function save() {
    if (!connForm.host) { Message.warning("请填写主机"); return; }
    emit("save", { ...connForm }, saveTarget.value);
}

async function doTestConnection() {
    if (!connForm.host) { Message.warning("请先填写主机"); return; }
    connTesting.value = true;
    connTestResult.value = "";
    connTestStatus.value = "";
    try {
        const ms = await GoTools.RedisTest({ ...connForm });
        connTestStatus.value = "ok";
        connTestResult.value = `连接成功（${ms} ms）`;
    } catch (e: any) {
        connTestStatus.value = "err";
        connTestResult.value = "连接失败：" + String(e?.message || e);
    } finally {
        connTesting.value = false;
    }
}
</script>
