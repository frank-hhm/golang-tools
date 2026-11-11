<template>
    <a-modal :visible="visible" :title="title" :width="520" :ok-loading="saving"
        :ok-text="editing ? '保存修改' : '保存账号'" :mask-closable="false" :on-before-ok="beforeOk"
        @update:visible="(v: boolean) => emit('update:visible', v)">
        <div class="osm-hint">
            对象存储没有网页授权流程，这里一次填齐即可。bucket / endpoint / region 属非敏感信息会显示在账号上；
            <strong>AccessKey 只加密存在本地，不会回传前端</strong>，<span v-if="editing">留空即沿用原有密钥，需要换密钥时 ID 与 Secret 一起重填。</span>
            <span v-else>改密钥只能重新填写。</span>
        </div>
        <a-form ref="formRef" :model="form" layout="vertical" class="osm-form">
            <a-form-item label="显示名" field="label" :help="labelHelp">
                <a-input v-model="form.label" :placeholder="kindName" allow-clear />
            </a-form-item>
            <a-form-item label="Bucket" field="bucket" :rules="[{ required: true, message: '请填写 bucket' }]">
                <a-input v-model="form.bucket" placeholder="例如 my-bucket" allow-clear />
            </a-form-item>
            <a-form-item v-if="!isS3" label="Endpoint" field="endpoint" help="留空则按 region 用默认域名">
                <a-input v-model="form.endpoint" :placeholder="endpointHint" allow-clear />
            </a-form-item>
            <a-form-item v-else label="Endpoint" field="endpoint"
                :rules="[{ required: true, message: '通用 S3 必须填 endpoint' }]" help="自建服务可填 IP:端口，如 127.0.0.1:9000">
                <a-input v-model="form.endpoint" placeholder="例如 minio.example.com" allow-clear />
            </a-form-item>
            <a-form-item label="Region" field="region" :rules="regionRules" :help="regionHelp">
                <a-input v-model="form.region" :placeholder="regionHint" allow-clear />
            </a-form-item>
            <a-form-item label="AccessKey ID" field="akID" :rules="akIdRules">
                <a-input v-model="form.akID" :placeholder="akIdPlaceholder" allow-clear />
            </a-form-item>
            <a-form-item :label="akSecretLabel" field="akSecret" :rules="akSecretRules">
                <a-input v-model="form.akSecret" type="password" :placeholder="akSecretPlaceholder" allow-clear />
            </a-form-item>
        </a-form>
    </a-modal>
</template>

<script setup lang="ts">
/**
 * AddObjectStoreModal：对象存储三 kind（阿里 OSS / 腾讯 COS / 通用 S3）共用的建/改账号表单。
 * 新增走 CloudAddObjectStore（bucket/endpoint/region 明文入库，AK/SK 进 tools/secret 密文）；
 * 传了 account 即编辑态，走 CloudUpdateObjectStore：非敏感项回填，AK 密文不回显故留空沿用。
 * 网盘类账号（Google/阿里云盘/百度）不走这里，见 index.vue 的授权/重命名弹窗。
 */
import { computed, getCurrentInstance, nextTick, reactive, ref, watch } from 'vue';
import { Message } from '@arco-design/web-vue';

interface OSAccount { id: string; kind: string; label: string; bucket?: string; endpoint?: string; region?: string; }

const props = defineProps<{ visible: boolean; kind: string; label?: string; account?: OSAccount | null }>();
const emit = defineEmits<{
    (e: 'update:visible', v: boolean): void;
    (e: 'added'): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const form = reactive({ label: '', bucket: '', endpoint: '', region: '', akID: '', akSecret: '' });
const saving = ref(false);
const formRef = ref();

// 编辑态：传入了带 id 的 account。此时标题/按钮文案切换，AK 两处可留空沿用旧密钥。
const editing = computed(() => !!props.account?.id);

// 每次打开都重置：新增从空白开始（密钥尤其不能残留上一次的）；编辑回填非敏感项，
// AK 密文不回前端故始终留空。最后清掉上一轮的就地校验红字。
watch(
    () => props.visible,
    (v) => {
        if (!v) return;
        const acc = props.account;
        if (acc?.id) {
            form.label = acc.label ?? '';
            form.bucket = acc.bucket ?? '';
            form.endpoint = acc.endpoint ?? '';
            form.region = acc.region ?? '';
        } else {
            form.label = props.label ?? '';
            form.bucket = '';
            form.endpoint = '';
            form.region = '';
        }
        form.akID = '';
        form.akSecret = '';
        nextTick(() => formRef.value?.clear?.());
    },
);

const isOSS = computed(() => props.kind === 'aliyun_oss');
const isCOS = computed(() => props.kind === 'tencent_cos');
const isS3 = computed(() => props.kind === 's3');
const kindName = computed(() => (isOSS.value ? '阿里云 OSS' : isCOS.value ? '腾讯云 COS' : 'S3 兼容存储'));
const title = computed(() => `${editing.value ? '编辑' : '添加'}${kindName.value}账号`);
const labelHelp = computed(() => (editing.value ? '留空则沿用原名称' : '留空则按类型自动命名'));
const akSecretLabel = computed(() => (isCOS.value ? 'SecretKey' : 'AccessKey Secret'));
const akSecretRequiredMsg = computed(() => (isCOS.value ? '请填写 SecretKey' : '请填写 AccessKey Secret'));
const akIdPlaceholder = computed(() => (editing.value ? '留空沿用原有 AccessKey ID' : '例如 LTAI5t...'));
const akSecretPlaceholder = computed(() =>
    editing.value ? '留空沿用原有密钥，如需更换请连同 ID 一起填写' : '保存后不可查看，只能重新填写',
);
// 编辑态 AK 非必填（留空沿用）；新增态维持必填。成对约束在 beforeOk 里统一校验。
const akIdRules = computed(() => (editing.value ? [] : [{ required: true, message: '请填写 AccessKey ID' }]));
const akSecretRules = computed(() => (editing.value ? [] : [{ required: true, message: akSecretRequiredMsg.value }]));
const endpointHint = computed(() =>
    isOSS.value ? '留空即可，形如 oss-cn-hangzhou.aliyuncs.com' : '留空即可，形如 cos.ap-guangzhou.myqcloud.com',
);
const regionHint = computed(() => (isOSS.value ? '例如 cn-hangzhou' : isCOS.value ? '例如 ap-guangzhou' : '例如 us-east-1'));
const regionHelp = computed(() => {
    if (isS3.value) return 'SigV4 签名范围，留空按 us-east-1';
    return '只填地域名（如 cn-hangzhou / ap-guangzhou），不要把 oss-cn-hangzhou.aliyuncs.com 整串粘进来（那是 Endpoint）';
});
// OSS/COS：Endpoint 与 Region 至少填一个。两个都空时访问域名会拼成 bucket.oss-.aliyuncs.com
// 这种坏主机（DNS 解不了），后端 osHost 现在也会拒，但这里就地拦更直观。
const regionRules = computed(() => [{
    validator: (value: string, callback: (err?: string) => void) => {
        if (isS3.value) return callback(); // S3 由 endpoint 必填规则负责
        if ((value ?? '').trim() || form.endpoint.trim()) return callback();
        callback('Endpoint 与 Region 至少填一个');
    },
}]);

function errText(e: any): string {
    return String(e?.message ?? e ?? '操作失败');
}

async function beforeOk(): Promise<boolean> {
    // 就地校验：不通过时返回 false，a-modal 保持打开，红字落在对应字段下（不再弹顶部 toast）。
    const err = await formRef.value?.validate?.();
    if (err) return false;
    // 编辑态 AK 成对校验：只填一个多半是漏填，就地拦下（留空两处=沿用旧密钥，合法）。
    if (editing.value) {
        const id = form.akID.trim();
        const sk = form.akSecret.trim();
        if ((id && !sk) || (sk && !id)) {
            Message.warning('更换 AccessKey 需同时填写 ID 与 Secret；若沿用旧密钥请两处都留空');
            return false;
        }
    }
    saving.value = true;
    try {
        if (editing.value && props.account) {
            await GoTools?.CloudUpdateObjectStore(
                props.account.id,
                form.label.trim(),
                form.bucket.trim(),
                form.endpoint.trim(),
                form.region.trim(),
                form.akID.trim(),
                form.akSecret.trim(),
            );
            Message.success('账号已更新');
        } else {
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
        }
        emit('added');
        return true; // 成功后交给 a-modal 关闭
    } catch (e: any) {
        Message.error(`保存失败：${errText(e)}`);
        return false; // 保存失败同样不关闭，方便就地修改重试
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
