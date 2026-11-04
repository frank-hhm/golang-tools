<template>
    <a-modal :visible="visible" :title="title" :width="520" :ok-loading="saving" ok-text="保存账号"
        :mask-closable="false" @update:visible="(v: boolean) => emit('update:visible', v)" @ok="submit">
        <div class="osm-hint">
            对象存储没有网页授权流程，这里一次填齐即可。bucket / endpoint / region 属非敏感信息会显示在账号上；
            <strong>AccessKey 只加密存在本地，不会回传前端</strong>，改密钥只能重新填写。
        </div>
        <a-form :model="form" layout="vertical" class="osm-form">
            <a-form-item label="显示名" help="留空则按类型自动命名">
                <a-input v-model="form.label" :placeholder="kindName" allow-clear />
            </a-form-item>
            <a-form-item label="Bucket" required>
                <a-input v-model="form.bucket" placeholder="例如 my-bucket" allow-clear />
            </a-form-item>
            <a-form-item v-if="!isS3" label="Endpoint" help="留空则按 region 用默认域名">
                <a-input v-model="form.endpoint" :placeholder="endpointHint" allow-clear />
            </a-form-item>
            <a-form-item v-else label="Endpoint" required help="自建服务可填 IP:端口，如 127.0.0.1:9000">
                <a-input v-model="form.endpoint" placeholder="例如 minio.example.com" allow-clear />
            </a-form-item>
            <a-form-item label="Region" :help="regionHelp">
                <a-input v-model="form.region" :placeholder="regionHint" allow-clear />
            </a-form-item>
            <a-form-item label="AccessKey ID" required>
                <a-input v-model="form.akID" placeholder="例如 LTAI5t..." allow-clear />
            </a-form-item>
            <a-form-item :label="akSecretLabel" required>
                <a-input v-model="form.akSecret" type="password" placeholder="保存后不可查看，只能重新填写" allow-clear />
            </a-form-item>
        </a-form>
    </a-modal>
</template>

<script setup lang="ts">
/**
 * AddObjectStoreModal：对象存储三 kind（阿里 OSS / 腾讯 COS / 通用 S3）共用的建账号表单。
 * 走后端一步式 CloudAddObjectStore（bucket/endpoint/region 明文入库，AK/SK 进 tools/secret 密文）。
 * 网盘类账号（Google/阿里云盘/百度）不走这里，见 index.vue 的授权弹窗。
 */
import { computed, getCurrentInstance, reactive, ref, watch } from 'vue';
import { Message } from '@arco-design/web-vue';

const props = defineProps<{ visible: boolean; kind: string; label?: string }>();
const emit = defineEmits<{
    (e: 'update:visible', v: boolean): void;
    (e: 'added'): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const form = reactive({ label: '', bucket: '', endpoint: '', region: '', akID: '', akSecret: '' });
const saving = ref(false);

// 每次打开都从空白开始（密钥尤其不能残留上一次的）。
watch(
    () => props.visible,
    (v) => {
        if (!v) return;
        form.label = props.label ?? '';
        form.bucket = '';
        form.endpoint = '';
        form.region = '';
        form.akID = '';
        form.akSecret = '';
    },
);

const isOSS = computed(() => props.kind === 'aliyun_oss');
const isCOS = computed(() => props.kind === 'tencent_cos');
const isS3 = computed(() => props.kind === 's3');
const kindName = computed(() => (isOSS.value ? '阿里云 OSS' : isCOS.value ? '腾讯云 COS' : 'S3 兼容存储'));
const title = computed(() => `添加${kindName.value}账号`);
const akSecretLabel = computed(() => (isCOS.value ? 'SecretKey' : 'AccessKey Secret'));
const endpointHint = computed(() =>
    isOSS.value ? '留空即可，形如 oss-cn-hangzhou.aliyuncs.com' : '留空即可，形如 cos.ap-guangzhou.myqcloud.com',
);
const regionHint = computed(() => (isOSS.value ? '例如 cn-hangzhou' : isCOS.value ? '例如 ap-guangzhou' : '例如 us-east-1'));
const regionHelp = computed(() => {
    if (isS3.value) return 'SigV4 签名范围，留空按 us-east-1';
    return '填了 Endpoint 留空也行（会从域名反解），建议按控制台填写';
});

function errText(e: any): string {
    return String(e?.message ?? e ?? '操作失败');
}

async function submit() {
    if (!form.bucket.trim()) { Message.warning('请填写 bucket'); return; }
    if (isS3.value && !form.endpoint.trim()) { Message.warning('通用 S3 必须填 endpoint'); return; }
    if (!form.akID.trim() || !form.akSecret.trim()) { Message.warning('AccessKey ID / Secret 都要填'); return; }
    saving.value = true;
    try {
        await GoTools?.CloudAddObjectStore(
            props.kind,
            form.label.trim(),
            form.bucket.trim(),
            form.endpoint.trim(),
            form.region.trim(),
            form.akID.trim(),
            form.akSecret.trim(),
        );
        Message.success('账号已添加，可直接浏览/转存');
        emit('update:visible', false);
        emit('added');
    } catch (e: any) {
        Message.error(`保存失败：${errText(e)}`);
    } finally {
        saving.value = false;
    }
}
</script>

<style scoped>
.osm-hint {
    font-size: 12px;
    line-height: 1.6;
    color: var(--color-text-3);
    background: var(--color-fill-2);
    border-radius: 4px;
    padding: 8px 10px;
    margin-bottom: 12px;
}

.osm-hint strong {
    color: var(--color-text-1);
}

.osm-form :deep(.arco-form-item:last-child) {
    margin-bottom: 0;
}
</style>
