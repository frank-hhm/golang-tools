<template>
    <a-row class="redis-page" :gutter="6">
        <!-- 左侧固定 30%（连接上50% + 本地服务下50%），右侧主区域铺满剩余空间 -->
        <a-col :span="6" class="side-col">
            <!-- 合并“外部连接”与“本地实例”为一个列表：单击只看右侧总览（未启动不拉起），双击才启动，用 a-tag 区分来源 -->
            <a-card class="side-card card-head-tool" size="small" :body-style="{ padding: 0 }">
                <template #title>连接</template>
                <template #extra>
                    <div class="srv-head-actions">
                        <a-button size="small" @click="openAddModal()">新增连接</a-button>
                        <a-button size="small" @click="openInstModal()">新建实例</a-button>
                        <a-button size="small" @click="openVersionsModal()">版本管理</a-button>
                    </div>
                </template>
                <!-- 引擎没起时先把话说明白：否则要等点「启动」撞一条管道错误才知道是 Docker 没跑 -->
                <common-docker-off class="srv-docker-off" :count="dockerInstCount" tool-label="Redis" />
                <div class="inst-list">
                    <common-item-card v-for="it in redisList" :key="it.key" :active="itemActive(it)"
                        @click="clickRedisItem(it)" @dblclick="dblRedisItem(it)" v-bind="redisReo.handlers(it.key)"
                        :class="{ 'reorder-dragging': redisReo.isDragging(it.key), 'reorder-over': redisReo.isOver(it.key) }">
                        <template #status>
                            <com-badge :status="it.live ? 'success' : 'normal'" :animated="it.live" />
                        </template>
                        <template #title>
                            <a-tooltip class="inst-name-tip" :content="it.name + ' · ' + it.sub">
                                <span class="inst-name">{{ it.name }}</span>
                            </a-tooltip>
                        </template>
                        <template #extra>
                            <!-- 删除改走气泡确认框：紧贴行内图标，不再弹居中大框 -->
                            <a-popconfirm :content="delTip(it)" type="warning" position="lt" @ok="onDeleteItem(it)">
                                <icon-delete class="conn-del" @click.stop />
                            </a-popconfirm>
                            <a-dropdown trigger="hover"
                                @select="(v: any) => it.kind === 'instance' ? onSrvAction(v, it.inst) : onConnAction(v, it.conn)">
                                <icon-more class="conn-more" @click.stop />
                                <template #content>
                                    <template v-if="it.kind === 'instance'">
                                        <a-doption v-if="!it.live" value="start"><icon-play-arrow /> 启动</a-doption>
                                        <a-doption v-else value="stop"><icon-pause /> 停止</a-doption>
                                        <a-doption value="logs"><icon-list /> 日志/状态</a-doption>
                                        <a-doption value="connect"><icon-link /> 连接</a-doption>
                                        <a-doption value="edit"><icon-edit /> 编辑</a-doption>
                                    </template>
                                    <template v-else>
                                        <a-doption value="open"><icon-play-arrow /> 打开连接</a-doption>
                                        <a-doption value="edit"><icon-edit /> 编辑</a-doption>
                                    </template>
                                </template>
                            </a-dropdown>
                        </template>
                        <div class="inst-tags">
                            <!-- 来源标识：实例 / 外部链接 -->
                            <a-tag v-if="it.kind === 'instance'" color="green" size="small">实例</a-tag>
                            <a-tag v-else color="gray" size="small">外部链接</a-tag>
                            <!-- 实例：版本/镜像身份；外部：地址 -->
                            <template v-if="it.kind === 'instance'">
                                <a-tooltip v-if="it.inst.mode === 'docker'"
                                    :content="imgMissing(it.inst.image)
                                        ? `镜像 ${it.inst.image} 本机还没有，启动时会先自动拉取（可能几分钟）`
                                        : `镜像 ${it.inst.image}：这就是该实例的 Redis 版本`">
                                    <a-tag :color="imgMissing(it.inst.image) ? 'orange' : 'purple'" size="small">Docker
                                        {{ imgTag(it.inst.image) }}</a-tag>
                                </a-tooltip>
                                <a-tag v-else color="arcoblue" size="small">v{{ it.inst.version }}</a-tag>
                            </template>
                            <span v-else class="conn-sub">{{ it.sub }}</span>
                        </div>
                    </common-item-card>
                    <div v-if="redisList.length === 0" class="conn-empty">暂无连接或实例</div>
                </div>
            </a-card>
        </a-col>
        <a-col :span="18" class="main-col">
            <!-- 本地实例总览：单击未启动实例只看这里（不自动启动）；启动走「启动」按钮或双击行，主页点卡片也落到这里 -->
            <a-card v-if="srvOvInst" class="main-panel conn-card card-head-tool" :body-style="{ padding: 0 }">
                <template #title>
                    <a-space>
                        <span class="status-addr">{{ srvOvInst.name }}(本地)</span>
                        <a-tag size="small" color="arcoblue">v{{ srvOvState?.version || srvOvInst.version }}</a-tag>
                        <a-tag size="small" :color="srvOvState?.running ? 'green' : 'gray'">{{
                            srvOvState?.running ? '运行中' : '已停止' }}</a-tag>
                        <a-tag v-if="srvOvInst.mode === 'docker'" size="small" color="purple">Docker
                            {{ imgTag(srvOvState?.image || srvOvInst.image) }}</a-tag>
                    </a-space>
                </template>
                <template #extra>
                    <a-space>
                        <a-button v-if="!srvOvState?.running" size="small" type="primary" :loading="ovBusy === 'start'"
                            @click="ovStartFromOv()">
                            <template #icon><icon-play-arrow /></template>启动
                        </a-button>
                        <a-button v-else size="small" type="primary" @click="connectInstance(srvOvInst)">
                            <template #icon><icon-link /></template>打开总览
                        </a-button>
                        <a-button size="small" @click="openLogs(srvOvInst!)">
                            <template #icon><icon-list /></template>日志
                        </a-button>
                        <a-button size="small" @click="refreshSrvStates()">
                            <template #icon><icon-refresh /></template>刷新
                        </a-button>
                    </a-space>
                </template>
                <div class="ov-body">
                    <a-row :gutter="[12, 12]" class="stat-row">
                        <a-col v-for="c in srvOvCards" :key="c.key" :xs="24" :sm="12" :md="8">
                            <common-stat-card :icon="c.icon" :tone="c.tone" :label="c.title" :value="c.value"
                                :detail="c.sub" :tip="c.tip" :ready="c.ready" />
                        </a-col>
                    </a-row>
                    <div class="info-section">
                        <div class="info-title">实例信息</div>
                        <div class="info-list">
                            <common-info-row grey label="地址" :value="`127.0.0.1:${srvOvInst.port}`" />
                            <common-info-row grey label="运行模式"
                                :value="srvOvInst.mode === 'docker' ? 'Docker 容器' : '本地进程'" />
                            <common-info-row grey label="数据目录" :value="srvOvInst.dataDir || '-'" />
                            <common-info-row grey label="持久化" :value="srvOvInst.appendOnly ? 'AOF 开启' : '仅 RDB 快照'" />
                            <common-info-row grey label="最大内存"
                                :value="srvOvInst.maxMemoryMB ? srvOvInst.maxMemoryMB + ' MB' : '未限制'" />
                            <common-info-row grey label="密码" :value="srvOvInst.password ? '已设置' : '无'" />
                        </div>
                    </div>
                </div>
            </a-card>

            <a-card class="main-panel empty-card card-head-tool" v-else-if="openTabs.length === 0">
                <!-- 空状态 -->
                <a-empty description="可以从左边选择并打开连接">
                    <a-button size="small" @click="openAddModal()">
                        <template #icon><icon-plus /></template>添加新连接
                    </a-button>
                </a-empty>
            </a-card>

            <!-- 已打开连接：用 a-card 承载，连接标签(openTabs)为 #title 的 a-radio-group，子标签为 #extra 的 a-radio-group -->
            <a-card v-else class="main-panel conn-card card-head-tool" :body-style="{ padding: 0 }">
                <template #title>
                    <a-radio-group v-model="st.activeTab" type="button" size="medium"
                        @change="(v: any) => switchTab(String(v))">
                        <a-radio v-for="t in openTabs" :key="t.id" :value="t.id">
                            <a-tooltip :content="t.name">
                                <span class="tab-label">{{ t.name }}</span>
                            </a-tooltip>
                            <icon-close class="tab-close" @click.stop="closeTab(t.id)" />
                        </a-radio>
                    </a-radio-group>
                </template>
                <template #extra>
                    <!-- 子标签 -->
                    <a-radio-group v-model="st.activeSubTab" type="button" size="medium" @change="onSubTabChange">
                        <a-radio value="status"><icon-dashboard /> 状态</a-radio>
                        <!-- 实例停止时只留状态页：数据子页全部入口隐藏 -->
                        <a-radio v-if="!instStopped" value="keys"><icon-list /> 键详情</a-radio>
                        <a-radio v-if="!instStopped" value="slowlog"><icon-clock-circle /> 慢日志</a-radio>
                        <a-radio v-if="!instStopped" value="monitor"><icon-scan /> 监控命令</a-radio>
                    </a-radio-group>
                </template>

                <div class="tab-content">
                    <!-- 状态 -->
                    <a-card v-show="st.activeSubTab === 'status'" class="status-card card-head-tool">
                        <template #title>
                            <a-space>
                                <span class="status-addr">{{ st.status?.host || activeTabName }}</span>
                                <a-tag size="small" color="red">v{{ st.status?.version || '?' }}</a-tag>
                                <a-tag size="small" color="orange">{{ st.status?.mode || 'standalone' }}</a-tag>
                                <a-tag size="small" color="purple">{{ st.status?.role || 'master' }}</a-tag>
                            </a-space>
                        </template>
                        <template #extra>
                            <a-space>
                                <!-- 本地实例：总览右上角直接给运维按钮，省去回左栏点菜单 -->
                                <template v-if="activeInst">
                                    <a-button v-if="!srvStates[activeInst.id]?.running" size="small" type="primary"
                                        :loading="ovBusy === 'start'" @click="ovToggleRun()">
                                        <template #icon><icon-play-arrow /></template>启动
                                    </a-button>
                                    <a-popconfirm v-else :content="`停止实例「${activeInst.name}」？正在处理的请求会中断。`"
                                        type="warning" position="lt" :disabled="!!ovBusy" @ok="ovToggleRun()">
                                        <a-button size="small" status="danger" :loading="ovBusy === 'stop'">
                                            <template #icon><icon-pause /></template>停止
                                        </a-button>
                                    </a-popconfirm>
                                    <a-button size="small" :loading="ovBusy === 'restart'"
                                        :disabled="!!ovBusy && ovBusy !== 'restart'" @click="ovRestart()">重启</a-button>
                                    <a-button size="small" @click="openLogs(activeInst)">
                                        <template #icon><icon-list /></template>日志
                                    </a-button>
                                </template>
                                <a-button size="small" @click="loadStatus()"><template #icon><icon-refresh /></template>刷新</a-button>
                            </a-space>
                        </template>
                        <div class="ov-body">
                            <!-- 关键指标：版式对齐 nginx（彩色圆角图标 + 标签/数值同排 + 下方明细） -->
                            <a-row :gutter="[12, 12]" class="stat-row">
                                <a-col v-for="c in redisStatCards" :key="c.key" :xs="24" :sm="12" :md="8">
                                    <common-stat-card :icon="c.icon" :tone="c.tone" :label="c.title" :value="c.value"
                                        :detail="c.sub" :tip="c.tip" :ready="c.ready" />
                                </a-col>
                            </a-row>

                            <!-- 快捷操作：状态页只留高频入口，跳其它子页；实例停止时一并隐藏 -->
                            <div v-if="!instStopped" class="quick-bar">
                                <a-button size="small" @click="goSubTab('keys')">
                                    <template #icon><icon-list /></template>浏览键
                                </a-button>
                                <a-button size="small" @click="goSubTab('slowlog')">
                                    <template #icon><icon-clock-circle /></template>慢日志
                                </a-button>
                                <a-button size="small" @click="goSubTab('monitor')">
                                    <template #icon><icon-scan /></template>监控命令
                                </a-button>
                            </div>

                            <!-- 连接信息：通用信息行（标签 | 值），与 nginx 站点信息同构 -->
                            <div class="info-section">
                                <div class="info-title">连接信息</div>
                                <div class="info-list">
                                    <common-info-row grey label="地址" :value="st.status?.host || activeTabName || '-'" />
                                    <common-info-row grey label="数据库" :value="String(activeCfg?.db ?? 0)" />
                                    <common-info-row grey label="运行模式" :value="st.status?.mode || 'standalone'" />
                                    <common-info-row grey label="主从角色" :value="st.status?.role || 'master'" />
                                    <common-info-row grey label="最大内存" :value="st.status?.maxMemoryHuman || '未限制'" />
                                </div>
                            </div>
                        </div>
                    </a-card>

                    <!-- 键详情 -->
                    <div v-show="st.activeSubTab === 'keys'" class="keys-view">
                        <a-card class="key-list-panel card-head-tool" :body-style="{ padding: 0 }">
                            <template #title>
                                <a-space>
                                    <a-input v-model="st.keyFilter" size="mini" placeholder="筛选键 (支持 * 通配)" allow-clear
                                        @press-enter="reloadKeys()">
                                        <template #prefix><icon-search /></template>
                                    </a-input>
                                    <a-button size="small" @click="reloadKeys()"><template
                                            #icon><icon-refresh /></template></a-button>
                                    <a-button size="small" @click="openAddKey()"><template
                                            #icon><icon-plus /></template></a-button>
                                    <a-button size="small" @click="openBatchKey()">批量生成</a-button>
                                </a-space>
                            </template>
                            <div class="key-list">
                                <div v-for="k in st.keys" :key="k.key" class="key-item"
                                    :class="{ active: k.key === st.selectedKey }" @click="selectKey(k.key)">
                                    
                                    <a-tag color="pinkpurple" size="small">{{ (k.type[0] || '?').toUpperCase() }}</a-tag>
                                    <a-tooltip :content="k.key">
                                        <div class="key-name">{{ k.key }}</div>
                                    </a-tooltip>
                                    <a-popconfirm :content="`确定删除键「${k.key}」？删除后不可恢复。`" type="warning"
                                        position="lt" @ok="delKey(k.key)">
                                        <icon-delete class="key-del" @click.stop />
                                    </a-popconfirm>
                                </div>
                                <a-empty v-if="st.keys.length === 0" :image="false" description="无键" />
                            </div>
                        </a-card>

                        <a-card class="key-detail-panel card-head-tool" :body-style="{ padding: 0 }">
                            <template #title>
                                <div v-if="!st.selectedKey" class="conn-empty">
                                    请选择左侧键查看
                                </div>
                                <a-space v-else>
                                    <a-tag color="pinkpurple">{{
                                        st.keyValue.type }}</a-tag>
                                    <div class="kd-key">{{ st.selectedKey }}</div>
                                    <a-tag size="small" color="arcoblue">TTL {{ formatTTL(st.keyValue.ttl)
                                    }}</a-tag>
                                </a-space>
                            </template>
                            <template #extra>
                                <a-space v-if="st.selectedKey">
                                    <a-button size="small" @click="selectKey(st.selectedKey)"><template
                                            #icon><icon-refresh /></template></a-button>
                                    <a-button v-if="st.keyValue.type === 'string'" size="small" @click="toggleEdit()">
                                        <template #icon><icon-edit /></template>{{ st.editing ? '取消' : '编辑'
                                        }}
                                    </a-button>
                                    <a-button v-if="st.editing" size="small" status="success"
                                        @click="saveKey()">
                                        <template #icon><icon-save /></template>保存
                                    </a-button>
                                    <a-button size="small" @click="copyValue()"><template
                                            #icon><icon-copy /></template>复制值</a-button>
                                    <a-popconfirm :content="`确定删除键「${st.selectedKey}」？删除后不可恢复。`" type="warning"
                                        position="lt" @ok="delKey(st.selectedKey)">
                                        <a-button size="small" status="danger"><template
                                                #icon><icon-delete /></template></a-button>
                                    </a-popconfirm>
                                </a-space>
                            </template>
                            <div class="kd-body">
                                <!-- string -->
                                <template v-if="st.keyValue.type === 'string'">
                                    <a-textarea v-if="st.editing" v-model="st.editStr" :auto-size="{ minRows: 6 }" />
                                    <div v-else class="kv-lines">
                                        <div v-for="(ln, i) in strLines" :key="i" class="kv-line">
                                            <span class="ln-no">{{ i + 1 }}</span>
                                            <span class="ln-txt">{{ ln }}</span>
                                        </div>
                                    </div>
                                </template>
                                <!-- hash -->
                                <a-table v-else-if="st.keyValue.type === 'hash'" :columns="hashCols" :data="hashData"
                                    :pagination="false" size="small" />
                                <!-- list -->
                                <a-table v-else-if="st.keyValue.type === 'list'" :columns="listCols" :data="listData"
                                    :pagination="false" size="small" />
                                <!-- set -->
                                <a-table v-else-if="st.keyValue.type === 'set'" :columns="setCols" :data="setData"
                                    :pagination="false" size="small" />
                                <!-- zset -->
                                <a-table v-else-if="st.keyValue.type === 'zset'" :columns="zsetCols" :data="zsetData"
                                    :pagination="false" size="small" />
                                <div v-else class="conn-empty">不支持的类型: {{ st.keyValue.type }}</div>
                            </div>
                        </a-card>
                    </div>

                    <!-- 慢日志 -->
                    <a-card v-show="st.activeSubTab === 'slowlog'" class="slowlog-view">
                        <template #title>
                            <div>慢日志</div>
                        </template>
                        <template #extra>
                            <a-space>
                                <a-button size="small" @click="loadSlowlog()"><template
                                        #icon><icon-refresh /></template>刷新</a-button>
                            </a-space>
                        </template>
                        <div class="table-wrap">
                            <a-table :columns="slowCols" :data="slowData" :pagination="false" size="small" />
                        </div>
                    </a-card>

                    <!-- 监控命令 -->
                    <a-card v-show="st.activeSubTab === 'monitor'" class="monitor-view">
                        <template #title>
                            <span class="mon-count">共 {{ st.monitorLines.length }} 条</span>
                        </template>
                        <template #extra>
                            <a-space>
                                <a-button size="small" v-if="!st.monitoring" @click="startMonitor()"><template
                                        #icon><icon-play-arrow /></template>开始监控</a-button>
                                <a-button size="small" v-else status="warning" @click="stopMonitor()"><template
                                        #icon><icon-pause /></template>停止</a-button>
                                <a-button size="small" @click="st.monitorLines = []"><template
                                        #icon><icon-delete /></template>清空</a-button>
                            </a-space>
                        </template>
                        <div class="mon-list">
                            <div v-for="(m, i) in st.monitorLines" :key="i" class="mon-line">
                                <span class="mon-time">{{ m.time }}</span>
                                <span class="mon-meta" v-if="m.db || m.client">[{{ m.db }} {{ m.client
                                }}]</span>
                                <span class="mon-cmd">{{ m.cmd }}</span>
                            </div>
                            <div v-if="st.monitorLines.length === 0" class="conn-empty" style="margin-top: 30px">
                                点击「开始监控」实时查看命令</div>
                        </div>
                    </a-card>
                </div>
            </a-card>
        </a-col>
    </a-row>

    <!-- 新增/编辑连接 -->
    <conn-modal v-model:visible="st.connModal" :editing="st.connEditing" :conn="editConn" @save="onConnSave" />

    <!-- 新增键 -->
    <a-modal v-model:visible="st.addKeyModal" title="新增键 (String)" :width="460" @ok="doAddKey">
        <a-form :model="keyForm" layout="vertical">
            <a-form-item field="key" label="键名"><a-input v-model="keyForm.key" placeholder="key" /></a-form-item>
            <a-form-item field="value" label="值"><a-textarea v-model="keyForm.value"
                    :auto-size="{ minRows: 3 }" /></a-form-item>
            <a-form-item field="ttl" label="过期时间（秒）" tooltip="0 = 不过期">
                <a-input-number v-model="keyForm.ttl" :min="0" />
            </a-form-item>
        </a-form>
    </a-modal>

    <!-- 批量生成键 (String) -->
    <a-modal v-model:visible="st.batchKeyModal" title="批量生成键 (String)" :width="480" :ok-loading="batchGenerating"
        @ok="doBatchKey">
        <a-form :model="batchForm" layout="vertical">
            <a-form-item field="keyPrefix" label="键名前缀">
                <a-input v-model="batchForm.keyPrefix" placeholder="例如 key" />
            </a-form-item>
            <a-form-item field="valuePrefix" label="值前缀">
                <a-input v-model="batchForm.valuePrefix" placeholder="例如 content" />
            </a-form-item>
            <a-row :gutter="12">
                <a-col :span="12">
                    <a-form-item field="start" label="起始序号">
                        <a-input-number v-model="batchForm.start" :min="0" />
                    </a-form-item>
                </a-col>
                <a-col :span="12">
                    <a-form-item field="end" label="结束序号">
                        <a-input-number v-model="batchForm.end" :min="0" />
                    </a-form-item>
                </a-col>
            </a-row>
            <a-form-item field="ttl" label="过期时间（秒）" tooltip="0 = 不过期">
                <a-input-number v-model="batchForm.ttl" :min="0" />
            </a-form-item>
            <div style="color: var(--color-text-3); font-size: var(--base-size-small)">将生成：{{ batchForm.keyPrefix
                }}{{ batchForm.start }} ~ {{ batchForm.keyPrefix }}{{ batchForm.end }}（共 {{
                    Math.max(0, batchForm.end - batchForm.start + 1) }} 个），值为 前缀+序号</div>
        </a-form>
    </a-modal>

    <!-- 版本管理：拆到 components/VersionsModal.vue（本地/线上/Docker 三页签 + 安装进度与 redis:install 事件） -->
    <versions-modal v-model:visible="versionsVisible" :srv-instances="srvInstances" :srv-states="srvStates"
        @create-instance="onCreateFromImage" @edit-instance="onEditInstanceById" @refresh-states="refreshSrvStates" />

    <!-- 本地服务：实例编辑：拆到 components/InstanceModal.vue（表单 + 镜像候选 + 端口探测 + 保存/切换重启） -->
    <instance-modal v-model:visible="instVisible" :edit="editingInst" :preset-image="presetImage"
        :srv-instances="srvInstances" :srv-states="srvStates" @saved="onInstanceSaved" />

    <!-- 本地服务：日志/状态：拆到 components/LogsModal.vue（自持日志与状态轮询） -->
    <logs-modal v-model:visible="logsVisible" :inst="logsInst" @connect="connectInstance" />
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import { useRoute } from "vue-router";
import { useReorder, applyOrder, loadOrder } from "@/utils/reorder";
import { imgTag, formatTTL, buildRedisStatCards, buildRedisSrvStatCards, type ConnConfig, type SrvInstanceConfig, type RedisListItem } from "./helpers";
import ConnModal from "./components/ConnModal.vue";
import InstanceModal from "./components/InstanceModal.vue";
import VersionsModal from "./components/VersionsModal.vue";
import LogsModal from "./components/LogsModal.vue";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const CACHE_KEY = "redis_connections_cache";

interface OpenTab { id: string; name: string; cfg: ConnConfig }

/** 已保存的连接列表 */
const savedConns = ref<ConnConfig[]>([]);
/** 已打开的连接标签 */
const openTabs = ref<OpenTab[]>([]);

const st = reactive({
    activeTab: "",
    activeSubTab: "status",
    // 状态
    status: null as any,
    // 键
    keys: [] as any[],
    keyFilter: "",
    selectedKey: "",
    keyValue: { type: "", ttl: -1, str: "", hash: {}, list: [], set: [], zset: [] } as any,
    editing: false,
    editStr: "",
    // 慢日志
    slowlog: [] as any[],
    // 监控
    monitoring: false,
    monitorLines: [] as any[],
    // 弹窗
    connModal: false,
    connEditing: false,
    addKeyModal: false,
    batchKeyModal: false,
});

/** 连接弹窗的编辑目标（新增为 null） */
const editConn = ref<ConnConfig | null>(null);

const keyForm = reactive({ key: "", value: "", ttl: 0 });
const batchForm = reactive({ keyPrefix: "key", valuePrefix: "content", start: 1, end: 100, ttl: 0 });
const batchGenerating = ref(false);

const activeTabName = computed(() => openTabs.value.find((t) => t.id === st.activeTab)?.name || "");
const activeCfg = computed(() => openTabs.value.find((t) => t.id === st.activeTab)?.cfg);

/** 状态页指标卡：构造逻辑下沉到 helpers.buildRedisStatCards，版式对齐 nginx */
const redisStatCards = computed(() => buildRedisStatCards(st.status));

/** 从状态页跳到其它子页：切子标签并触发对应数据加载 */
function goSubTab(tab: string) {
    st.activeSubTab = tab;
    onSubTabChange();
}

const isOpen = (id: string) => openTabs.value.some((t) => t.id === id);

/* ============================ 连接管理 ============================ */

async function loadConnections() {
    // 优先读文件，其次读缓存
    let list: ConnConfig[] = [];
    try {
        list = (await GoTools.RedisLoadConnections()) || [];
    } catch (e) {
        list = [];
    }
    if (!list || list.length === 0) {
        try {
            const cache = localStorage.getItem(CACHE_KEY);
            if (cache) list = JSON.parse(cache) || [];
        } catch (e) {
            list = [];
        }
    }
    savedConns.value = (list || []).map((c) => ({ ...c }));
}

async function persistConnections(target: string) {
    const data = JSON.parse(JSON.stringify(savedConns.value));
    // 缓存始终镜像一份，避免丢失
    localStorage.setItem(CACHE_KEY, JSON.stringify(data));
    if (target === "file") {
        try {
            await GoTools.RedisSaveConnections(data);
        } catch (e) {
            /* ignore */
        }
    }
}

function openAddModal() {
    st.connEditing = false;
    editConn.value = null;
    st.connModal = true;
}

function openEditModal(c: ConnConfig) {
    st.connEditing = true;
    editConn.value = { ...c };
    st.connModal = true;
}

/* 连接表单与测试连接已迁入 components/ConnModal.vue，弹窗保存时回传连接与目标位置 */
async function onConnSave(conn: ConnConfig, saveTarget: string) {
    const item: ConnConfig = { ...conn };
    if (!item.name) item.name = `${item.host}:${item.port}`;
    if (st.connEditing && item.id) {
        const i = savedConns.value.findIndex((c) => c.id === item.id);
        if (i >= 0) savedConns.value[i] = item;
        else savedConns.value.push(item);
    } else {
        item.id = "c" + Date.now().toString(36);
        savedConns.value.push(item);
    }
    await persistConnections(saveTarget);
    st.connModal = false;
    Message.success("已保存到" + (saveTarget === "file" ? "文件" : "缓存"));
}

function onConnAction(v: string, c: ConnConfig) {
    if (v === "open") openConnection(c);
    else if (v === "edit") openEditModal(c);
}

async function doDeleteConn(c: ConnConfig) {
    if (isOpen(c.id)) closeTab(c.id);
    savedConns.value = savedConns.value.filter((x) => x.id !== c.id);
    await persistConnections("file");
}

async function openConnection(c: ConnConfig) {
    srvOvInst.value = null; // 进连接工作区就退出「实例总览」态
    if (isOpen(c.id)) {
        switchTab(c.id);
        return;
    }
    try {
        await GoTools.RedisOpen({ ...c });
    } catch (e: any) {
        Message.error("连接失败: " + (e?.message || e));
        return;
    }
    openTabs.value.push({ id: c.id, name: c.name || `${c.host}:${c.port}`, cfg: { ...c } });
    switchTab(c.id);
}

function switchTab(id: string) {
    if (st.monitoring) stopMonitor();
    st.activeTab = id;
    st.status = null;
    st.keys = [];
    st.selectedKey = "";
    st.slowlog = [];
    st.monitorLines = [];
    loadSubTab();
}

function closeTab(id: string) {
    if (activeTabName.value && st.activeTab === id && st.monitoring) stopMonitor();
    GoTools.RedisClose(id);
    openTabs.value = openTabs.value.filter((t) => t.id !== id);
    if (st.activeTab === id) {
        if (openTabs.value.length > 0) switchTab(openTabs.value[openTabs.value.length - 1].id);
        else st.activeTab = "";
    }
}

/* ============================ 子标签数据 ============================ */

function onSubTabChange() {
    if (st.monitoring && st.activeSubTab !== "monitor") stopMonitor();
    loadSubTab();
}

function loadSubTab() {
    if (!st.activeTab) return;
    // 本地实例已停止：Go 侧那条连接随进程一起失效，再请求只会拿到「连接未打开」，
    // 顺手把旧快照清掉，不要把停机前的数字当成实时值继续展示
    if (instStopped.value) {
        st.status = null;
        st.keys = [];
        st.slowlog = [];
        return;
    }
    if (st.activeSubTab === "status") loadStatus();
    else if (st.activeSubTab === "keys") reloadKeys();
    else if (st.activeSubTab === "slowlog") loadSlowlog();
}

async function loadStatus() {
    try {
        const s = await GoTools.RedisStatus(st.activeTab);
        s.host = activeCfg.value ? `${activeCfg.value.host}:${activeCfg.value.port}` : "";
        st.status = s;
    } catch (e: any) {
        Message.error("获取状态失败: " + (e?.message || e));
    }
}

async function reloadKeys() {
    try {
        let pattern = st.keyFilter.trim();
        if (pattern && !pattern.includes("*")) pattern = "*" + pattern + "*";
        const res = await GoTools.RedisScan(st.activeTab, pattern || "*", 200, 2000);
        st.keys = res.keys || [];
    } catch (e: any) {
        Message.error("扫描键失败: " + (e?.message || e));
    }
}

async function selectKey(key: string) {
    st.selectedKey = key;
    st.editing = false;
    try {
        st.keyValue = await GoTools.RedisGetKey(st.activeTab, key);
    } catch (e: any) {
        Message.error("读取键失败: " + (e?.message || e));
    }
}

function toggleEdit() {
    st.editing = !st.editing;
    if (st.editing) st.editStr = st.keyValue.str || "";
}

async function saveKey() {
    try {
        await GoTools.RedisSetString(st.activeTab, st.selectedKey, st.editStr, st.keyValue.ttl > 0 ? st.keyValue.ttl : 0);
        Message.success("已保存");
        st.editing = false;
        selectKey(st.selectedKey);
    } catch (e: any) {
        Message.error("保存失败: " + (e?.message || e));
    }
}

async function delKey(key: string) {
    try {
        await GoTools.RedisDeleteKeys(st.activeTab, [key]);
        if (st.selectedKey === key) {
            st.selectedKey = "";
            st.keyValue = { type: "", ttl: -1, str: "", hash: {}, list: [], set: [], zset: [] };
        }
        reloadKeys();
    } catch (e: any) {
        Message.error("删除失败: " + (e?.message || e));
    }
}

function openAddKey() {
    Object.assign(keyForm, { key: "", value: "", ttl: 0 });
    st.addKeyModal = true;
}

async function doAddKey() {
    if (!keyForm.key) {
        Message.warning("请填写键名");
        return;
    }
    try {
        await GoTools.RedisSetString(st.activeTab, keyForm.key, keyForm.value, keyForm.ttl);
        Message.success("已创建");
        st.addKeyModal = false;
        reloadKeys();
        selectKey(keyForm.key);
    } catch (e: any) {
        Message.error("创建失败: " + (e?.message || e));
    }
}

function openBatchKey() {
    if (!st.activeTab) {
        Message.warning("请先打开一个连接");
        return;
    }
    st.batchKeyModal = true;
}

async function doBatchKey() {
    if (!st.activeTab) {
        Message.warning("请先打开一个连接");
        return;
    }
    if (!batchForm.keyPrefix) {
        Message.warning("请填写键名前缀");
        return;
    }
    if (batchForm.end < batchForm.start) {
        Message.warning("结束序号不能小于起始序号");
        return;
    }
    batchGenerating.value = true;
    try {
        const n = await GoTools.RedisBatchSetString(st.activeTab, batchForm.keyPrefix, batchForm.valuePrefix,
            batchForm.start, batchForm.end, batchForm.ttl);
        Message.success(`已批量生成 ${n ?? (batchForm.end - batchForm.start + 1)} 个键`);
        st.batchKeyModal = false;
        await reloadKeys();
    } catch (e: any) {
        Message.error("批量生成失败: " + (e?.message || e));
    } finally {
        batchGenerating.value = false;
    }
}

/* ============================ 慢日志 ============================ */

async function loadSlowlog() {
    try {
        st.slowlog = (await GoTools.RedisSlowlog(st.activeTab, 100)) || [];
    } catch (e: any) {
        Message.error("获取慢日志失败: " + (e?.message || e));
    }
}

const slowCols = [
    { title: "序号", dataIndex: "id", width: 80 },
    { title: "耗时(μs)", dataIndex: "duration", width: 110 },
    { title: "时间", slotName: "time", width: 170 },
    { title: "命令", dataIndex: "command", ellipsis: true, tooltip: true },
    { title: "客户端", dataIndex: "clientAddr", width: 160 },
];
const slowData = computed(() =>
    st.slowlog.map((e: any) => ({ ...e, time: e.time ? new Date(e.time * 1000).toLocaleString() : "" })),
);

/* ============================ 监控命令 ============================ */

let offMonitor: any = null;

async function startMonitor() {
    if (!activeCfg.value) return;
    try {
        await GoTools.RedisStopMonitor();
        st.monitorLines = [];
        await GoTools.RedisStartMonitor({ ...activeCfg.value });
        st.monitoring = true;
    } catch (e: any) {
        Message.error("启动监控失败: " + (e?.message || e));
    }
}

async function stopMonitor() {
    try {
        await GoTools.RedisStopMonitor();
    } catch (e) {
        /* ignore */
    }
    st.monitoring = false;
}

/* ============================ 键详情表格列 ============================ */

const hashCols = [
    { title: "Field", dataIndex: "field", width: 240, ellipsis: true, tooltip: true },
    { title: "Value", dataIndex: "value", ellipsis: true, tooltip: true },
];
const hashData = computed(() => Object.entries(st.keyValue.hash || {}).map(([field, value]) => ({ field, value })));

const listCols = [
    { title: "#", dataIndex: "idx", width: 70 },
    { title: "Value", dataIndex: "value", ellipsis: true, tooltip: true },
];
const listData = computed(() => (st.keyValue.list || []).map((value: string, i: number) => ({ idx: i, value })));

const setCols = [{ title: "Member", dataIndex: "value", ellipsis: true, tooltip: true }];
const setData = computed(() => (st.keyValue.set || []).map((value: string) => ({ value })));

const zsetCols = [
    { title: "Member", dataIndex: "member", ellipsis: true, tooltip: true },
    { title: "Score", dataIndex: "score", width: 140 },
];
const zsetData = computed(() => st.keyValue.zset || []);

const strLines = computed<string[]>(() => String(st.keyValue.str ?? "").split("\n"));

/* ============================ 工具函数 ============================ */

function copyValue() {
    const v = st.keyValue.type === "string" ? st.keyValue.str : JSON.stringify(st.keyValue[st.keyValue.type] ?? {}, null, 2);
    const input = document.createElement("textarea");
    input.value = v || "";
    document.body.appendChild(input);
    input.select();
    document.execCommand("copy");
    document.body.removeChild(input);
    Message.success("已复制");
}

/* ============================ 本地服务管理 ============================ */

const srvInstances = ref<SrvInstanceConfig[]>([]);
const srvStates = reactive<Record<string, any>>({});
const versionsVisible = ref(false);
/** 有多少实例跑在容器里（它们的版本由镜像 tag 决定，不占本机版本库） */
const dockerInstCount = computed(() => srvInstances.value.filter((i: any) => i.mode === "docker").length);
const instVisible = ref(false);
const logsVisible = ref(false);
/** 日志弹窗的目标实例 */
const logsInst = ref<SrvInstanceConfig | null>(null);
/** 实例弹窗的编辑目标（新建为 null） */
const editingInst = ref<SrvInstanceConfig | null>(null);
/** 从版本管理带入的镜像：非空则新建实例直接进 Docker 模式 */
const presetImage = ref("");

/** 右侧「本地实例总览」的目标实例：非空时取代连接工作区；数据复用 3s 一轮的 srvStates 快照，不单开轮询 */
const srvOvInst = ref<SrvInstanceConfig | null>(null);
const srvOvState = computed(() => (srvOvInst.value ? srvStates[srvOvInst.value.id] || null : null));
const srvOvCards = computed(() => (srvOvInst.value ? buildRedisSrvStatCards(srvOvState.value, srvOvInst.value) : []));

/* 镜像清单只用来标记「实例引用的镜像本机还没拉」（表单/候选/引擎状态已迁入 InstanceModal） */
/* 名字不能回退成 dockerImages：script setup 下 kebab 标签 docker-images 会优先拿同名 setup 变量当组件，整个 Docker 区会渲染成空白 */
const dockerImageList = ref<string[]>([]);

/** 只刷新镜像清单（不写表单）：用来标记实例引用的镜像本机是否已拉取 */
async function probeDockerImages() {
    try { dockerImageList.value = (await GoTools.RedisSrvDockerImages()) || []; }
    catch (e) { /* 引擎未就绪就不做校验，免得误报 */ }
}
/** 清单非空且不含它 → 本机还没有；启动时会自动拉 */
function imgMissing(image: string): boolean {
    return !!image && dockerImageList.value.length > 0 && !dockerImageList.value.includes(image);
}

const route = useRoute();
let offSelInst: any = null;
let offOpenPanel: any = null;
let srvTimer: any = null;

async function loadSrvInstances() {
    try { srvInstances.value = (await GoTools.RedisSrvListInstances()) || []; }
    catch (e) { srvInstances.value = []; }
    refreshSrvStates();
}

async function refreshSrvStates() {
    // 并发问：实例串行 await 就是 N 次往返，Web 模式跑在远程服务器上时尤其明显
    await Promise.all(srvInstances.value.map(async (inst) => {
        try {
            srvStates[inst.id] = await GoTools.RedisSrvStatus(inst.id);
        } catch (e) { /* 单个实例失败不影响其它实例 */ }
    }));
}

function openVersionsModal() {
    // 版本弹窗自持本地/线上/Docker 三页签与安装进度，打开时的重置逻辑在 VersionsModal 内
    versionsVisible.value = true;
}

function openInstModal(inst?: SrvInstanceConfig) {
    // 表单/镜像候选/端口探测/保存与切换重启都迁入 InstanceModal，这里只决定编辑目标与预置镜像
    editingInst.value = inst ? { ...inst } : null;
    presetImage.value = "";
    instVisible.value = true;
}

/** 实例弹窗保存/重启后：重拉实例列表与状态 */
function onInstanceSaved() {
    loadSrvInstances();
}

/** 版本管理里点「新建实例」：关版本窗，带镜像打开实例弹窗（Docker 模式） */
function onCreateFromImage(image: string) {
    versionsVisible.value = false;
    editingInst.value = null;
    presetImage.value = image;
    instVisible.value = true;
}

/** 版本管理里点「使用实例」：关版本窗，转去编辑该实例 */
function onEditInstanceById(id: string) {
    const inst = srvInstances.value.find((i) => i.id === id);
    if (!inst) return;
    versionsVisible.value = false;
    openInstModal(inst);
}

async function onSrvAction(v: string, inst: SrvInstanceConfig) {
    if (v === "start") {
        try { await GoTools.RedisSrvStart(inst.id); Message.success(`实例 ${inst.name} 已启动`); }
        catch (e: any) { Message.error("启动失败: " + (e?.message || e)); }
        refreshSrvStates();
    } else if (v === "stop") {
        try { await GoTools.RedisSrvStop(inst.id); Message.success(`实例 ${inst.name} 已停止`); }
        catch (e: any) { Message.error("停止失败: " + (e?.message || e)); }
        refreshSrvStates();
    } else if (v === "logs") {
        openLogs(inst);
    } else if (v === "connect") {
        focusInstance(inst);
    } else if (v === "edit") {
        openInstModal(inst);
    }
}

/** 正在启动中的实例 id：防止连点重复拉起进程 */
const instStarting = new Set<string>();

/** 连上本地实例并展开数据总览：只在确认已启动后调用，本身绝不拉起实例 */
function connectInstance(inst: SrvInstanceConfig | null) {
    if (!inst) return;
    const cfg: ConnConfig = {
        id: "srv-" + inst.id, name: `${inst.name}(本地)`,
        host: "127.0.0.1", port: inst.port, password: inst.password, db: 0,
    };
    // 不再把实例回写成一条 savedConn：合并后的左栏已用实例行代表，避免重复
    openConnection(cfg);
}

/** 单击未启动实例：右侧展示「未启动总览」，只读状态，不做任何启动动作 */
function openSrvOverview(inst: SrvInstanceConfig) {
    srvOvInst.value = inst;
}

/** 主页点卡片进入 / 行菜单「连接」：先拿实时状态，运行中才连上总览，否则落在未启动总览 */
async function focusInstance(inst: SrvInstanceConfig) {
    await refreshSrvStates();
    if (srvStates[inst.id]?.running) connectInstance(inst);
    else openSrvOverview(inst);
}

/** 左栏统一列表项：外部连接与本地实例合并成一个列表（RedisListItem 类型已下沉 helpers） */
const redisRawList = computed<RedisListItem[]>(() => {
    const insts: RedisListItem[] = srvInstances.value.map((inst) => ({
        kind: "instance", key: "inst-" + inst.id, name: inst.name,
        sub: `127.0.0.1:${inst.port}`, live: !!srvStates[inst.id]?.running, inst,
    }));
    // 历史上 connectInstance 会把实例注入成 "srv-" 前缀的连接；合并后由实例行代表，过滤掉避免重复
    const conns: RedisListItem[] = savedConns.value
        .filter((c) => !c.id.startsWith("srv-"))
        .map((c) => ({
            kind: "external", key: "conn-" + c.id, name: c.name || `${c.host}:${c.port}`,
            sub: `${c.host}:${c.port}`, live: isOpen(c.id), conn: c,
        }));
    return [...insts, ...conns];
});
/* 拖拽排序：左栏列表（实例+外部连接）顺序存入用户配置 redis:list（与主页分开，随导出搬运） */
const redisOrder = ref<string[]>([]);
const redisReo = useReorder("redis:list", {
    getVisible: () => redisRawList.value.map((i) => i.key),
    onReordered: (nv) => { redisOrder.value = nv; void GoTools?.SetListOrder?.("redis:list", nv); },
});
const redisList = computed(() => applyOrder(redisRawList.value, redisOrder.value, (i) => i.key));

/** 当前右侧总览展示的是哪一行：高亮对应列表项（实例可能是连接页签或实例总览） */
function itemActive(it: RedisListItem): boolean {
    if (it.kind === "instance")
        return srvOvInst.value?.id === it.inst.id || st.activeTab === "srv-" + it.inst.id;
    return st.activeTab === it.conn.id;
}

/** 点行：运行中实例连上展开总览；未启动实例只看「未启动总览」（不启动）；外部连接连上 */
function clickRedisItem(it: RedisListItem) {
    if (it.kind === "instance") {
        if (it.live) connectInstance(it.inst);
        else openSrvOverview(it.inst);
    } else openConnection(it.conn);
}

/** 双击行：未启动实例直接拉起并连上总览；其余与单击一致（启动动作只留在这里和明确按钮） */
async function dblRedisItem(it: RedisListItem) {
    if (it.kind !== "instance" || it.live) return;
    if (instStarting.has(it.inst.id)) return; // 已在启动中，忽略重复双击
    instStarting.add(it.inst.id);
    Message.info(`实例「${it.inst.name}」未启动，正在启动…`);
    try {
        await GoTools.RedisSrvStart(it.inst.id);
    } catch (e: any) {
        Message.error("启动失败: " + (e?.message || e));
        instStarting.delete(it.inst.id);
        refreshSrvStates();
        return;
    }
    instStarting.delete(it.inst.id);
    Message.success(`实例「${it.inst.name}」已启动`);
    await refreshSrvStates();
    connectInstance(it.inst);
}

/** 删除气泡文案：按来源区分实例 / 连接 */
function delTip(it: RedisListItem): string {
    return it.kind === "instance"
        ? `确定删除实例「${it.inst.name}」？运行中会先停止；数据目录保留。`
        : `确定删除连接「${it.name}」？`;
}

/** 气泡确认后真正执行删除 */
function onDeleteItem(it: RedisListItem) {
    if (it.kind === "instance") doDeleteInst(it.inst);
    else doDeleteConn(it.conn);
}

async function doDeleteInst(inst: SrvInstanceConfig) {
    if (srvOvInst.value?.id === inst.id) srvOvInst.value = null; // 总览目标被删就退回连接区/空态
    try { await GoTools.RedisSrvRemoveInstance(inst.id); delete srvStates[inst.id]; await loadSrvInstances(); }
    catch (e: any) { Message.error("删除失败: " + (e?.message || e)); }
}

/** 当前右侧总览展示的是本地实例时返回其实体（标签页 id 以 "srv-" 前缀），外部连接返回 null */
const activeInst = computed<SrvInstanceConfig | null>(() => {
    if (!st.activeTab.startsWith("srv-")) return null;
    const id = st.activeTab.slice(4);
    return srvInstances.value.find((i) => i.id === id) || null;
});

/** 当前页签的本地实例已停止：键详情/慢日志/监控命令都拉不到数据，入口直接隐藏（外部连接无进程状态可查，不隐藏） */
const instStopped = computed(() => !!activeInst.value && !srvStates[activeInst.value.id]?.running);
// 实例被停（含崩溃、外部 kill）：切回状态页并停掉监控，避免留在一个入口已隐藏、
// 又拉不到数据的空转页面上；loadSubTab 会顺带清掉旧快照
watch(instStopped, (stopped) => {
    if (!stopped) return;
    if (st.monitoring) stopMonitor();
    if (st.activeSubTab !== "status") st.activeSubTab = "status";
    loadSubTab();
});

/** 总览运维按钮的 loading：'start' | 'stop' | 'restart' | '' */
const ovBusy = ref("");

/** 启动/停止切换（停止走气泡确认） */
async function ovToggleRun() {
    const inst = activeInst.value;
    if (!inst) return;
    const running = !!srvStates[inst.id]?.running;
    ovBusy.value = running ? "stop" : "start";
    try {
        if (running) { await GoTools.RedisSrvStop(inst.id); Message.success(`实例 ${inst.name} 已停止`); }
        else { await GoTools.RedisSrvStart(inst.id); Message.success(`实例 ${inst.name} 已启动`); }
    } catch (e: any) { Message.error((running ? "停止" : "启动") + "失败: " + (e?.message || e)); }
    finally { ovBusy.value = ""; refreshSrvStates(); }
}

/** 重启：Redis 无单独 restart，先停再起 */
async function ovRestart() {
    const inst = activeInst.value;
    if (!inst) return;
    ovBusy.value = "restart";
    try {
        await GoTools.RedisSrvStop(inst.id);
        await GoTools.RedisSrvStart(inst.id);
        Message.success(`实例 ${inst.name} 已重启`);
    } catch (e: any) { Message.error("重启失败: " + (e?.message || e)); }
    finally { ovBusy.value = ""; refreshSrvStates(); }
}

/** 总览里的「启动」：拉起后直接连上进入数据总览 */
async function ovStartFromOv() {
    const inst = srvOvInst.value;
    if (!inst || ovBusy.value) return;
    ovBusy.value = "start";
    try {
        await GoTools.RedisSrvStart(inst.id);
        Message.success(`实例 ${inst.name} 已启动`);
        await refreshSrvStates();
        connectInstance(inst);
    } catch (e: any) { Message.error("启动失败: " + (e?.message || e)); }
    finally { ovBusy.value = ""; }
}

/** 打开日志/状态弹窗：日志与状态轮询已迁入 LogsModal 自持，这里只切换目标实例 */
function openLogs(inst: SrvInstanceConfig) {
    logsInst.value = inst;
    logsVisible.value = true;
}

/* ============================ 生命周期 ============================ */

onMounted(async () => {
    // 两者各一次绑定往返，彼此不依赖：Web 模式跑在远程服务器上，串行 await 就是白白多等一个 RTT
    await Promise.all([loadConnections(), loadSrvInstances()]);
    redisOrder.value = await loadOrder("redis:list");
    // 镜像清单不进表单，只用来标记「实例引用的镜像本机还没拉」
    probeDockerImages();
    // 主页点击服务实例进入（?inst=）：运行中连上展开总览，未启动看未启动总览（不再弹日志框）
    const wantId = String(route.query.inst ?? "");
    if (wantId) {
        const target = srvInstances.value.find((i) => i.id === wantId);
        if (target) focusInstance(target);
    }
    // 下载抽屉点「装版本」进入（?open=versions）：冷启动直接弹版本管理
    if (String(route.query.open ?? "") === "versions") openVersionsModal();
    srvTimer = setInterval(refreshSrvStates, 3000);
    // 单例窗口已打开时，主页再次点击其他实例 → 通过事件切换
    offSelInst = Events.On("tool:select-inst", (ev: any) => {
        const p = ev?.data ?? ev;
        if (p?.type !== "redis" || !p?.id) return;
        const target = srvInstances.value.find((i) => i.id === String(p.id));
        if (target) focusInstance(target);
    });
    // 单例窗口已开着时，抽屉再点装任务 → 通过事件弹出版本管理
    offOpenPanel = Events.On("tool:open-panel", (ev: any) => {
        const p = ev?.data ?? ev;
        if (p?.type === "redis" && p?.panel === "versions") openVersionsModal();
    });
    // redis:install 与安装进度恢复已迁入 VersionsModal（弹窗常驻挂载，其 onMounted 自行订阅）
    offMonitor = Events.On("redis:monitor", (ev: any) => {
        const line = ev?.data ?? ev;
        st.monitorLines.push(line);
        if (st.monitorLines.length > 3000) st.monitorLines.splice(0, st.monitorLines.length - 3000);
    });
});

onUnmounted(() => {
    if (offMonitor) offMonitor();
    if (offSelInst) offSelInst();
    if (offOpenPanel) offOpenPanel();
    if (srvTimer) clearInterval(srvTimer);
    if (st.monitoring) stopMonitor();
    openTabs.value.forEach((t) => GoTools.RedisClose(t.id));
});
</script>

<style scoped>
/* 页面根容器：用 a-row 栅格铺满工具窗口内容区（.win-content 有确定高度） */
.redis-page {
    height: 100%;
    min-height: 0;
    flex-wrap: nowrap;
}

/* 左侧列（a-col，约 30% 宽）：合并后单张卡片铺满整列 */
.side-col {
    height: 100%;
    min-width: 0;
    display: flex;
    flex-direction: column;
}

/* 右侧主列（a-col，剩余宽）：承载主区域卡片 */
.main-col {
    height: 100%;
    min-width: 0;
    display: flex;
    flex-direction: column;
}

/* 左侧每个面板各自成卡片：标题走 #title、操作走 #extra，body 弹性填充供列表滚动 */
.side-card {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

.side-card :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

/* 左栏 a-list 撑满卡片高度并各自滚动 */
.side-card :deep(.arco-list) {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
}

.srv-head-actions {
    display: flex;
    align-items: center;
    gap: 2px;
}

.conn-sub {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}

.conn-more {
    color: var(--color-text-3);
}

.conn-del {
    color: var(--color-text-3);
    margin-right: 10px;
}

.conn-del:hover {
    color: rgb(var(--danger-6));
}

.conn-empty {
    color: var(--color-text-3);
    text-align: center;
    font-size: var(--base-size-small);
    padding: var(--base-padding);
}

/* 连接弹窗底栏（.conn-test-footer 等）样式已下沉到全局 styles/index.scss，四个工具的测试连接弹窗共用 */

/* 主区域 */
.main-panel {
    width: 100%;
    height: 100%;
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    background: var(--color-bg-2);
}

.main-panel :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

/* 空状态：主卡片内容垂直居中 */
.empty-card :deep(.arco-card-body) {
    align-items: center;
    justify-content: center;
}

/* 连接标签卡片：#title 内的连接单选与关闭图标 */
.conn-card :deep(.arco-card-header-title) {
    flex-wrap: wrap;
}

.tab-label {
    max-width: 140px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    display: inline-block;
    vertical-align: middle;
}

.tab-close {
    margin-left: 6px;
    font-size: var(--base-size-small);
    opacity: 0.6;
}

.tab-close:hover {
    opacity: 1;
    color: rgb(var(--red-6));
}

.tab-content {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: var(--base-padding);
    padding: var(--base-padding);
    height: calc(100% - var(--base-padding) * 2);
    overflow: hidden;
}

/* 状态 */
.status-card {
    height: 100%;
}

.status-addr {
    font-size: var(--base-size-large-x);
    font-weight: 700;
}

/* 状态页右侧主体：纵向堆叠指标卡 / 快捷条 / 信息行，超高时整块滚动 */
.status-card :deep(.arco-card-body) {
    overflow: auto;
}

.ov-body {
    padding: 14px;
    display: flex;
    flex-direction: column;
    gap: 16px;
}

.stat-row {
    margin: 0;
}

.quick-bar {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
}

/* 连接信息：长文本省略 + tooltip，单列信息行 */
.info-section {
    display: flex;
    flex-direction: column;
    gap: 8px;
}

.info-title {
    font-size: var(--base-size-default);
    font-weight: 600;
    color: var(--color-text-1);
}

.info-list {
    display: flex;
    flex-direction: column;
}

/* 左侧列表卡片：外壳（边框/圆角/选中竖条/hover）由 common-item-card 提供，这里只留内容样式 */
.inst-list {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: calc(var(--base-padding) / 2);
}

.inst-name-tip {
    flex: 1;
    min-width: 0;
}

.inst-name {
    display: block;
    font-weight: 600;
    color: var(--color-text-1);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.inst-tags {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
}

/* 键详情 */
.keys-view {
    flex: 1;
    min-height: 0;
    display: flex;
    gap: var(--base-padding);
}

.key-list-panel {
    width: 300px;
    flex-shrink: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

/* 左右两栏卡片主体撑满并内部滚动 */
.key-list-panel :deep(.arco-card-body),
.key-detail-panel :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

.key-list {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
}

.key-item {
    display: flex;
    align-items: center;
    gap: var(--base-padding);
    padding: var(--base-padding);
    font-size: var(--base-size-small);
    cursor: pointer;
    border-bottom: 1px solid var(--color-fill-1);
}

.key-item:hover {
    background: var(--color-fill-2);
}

.key-item.active {
    background: var(--color-primary-light-1);
}

.key-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.key-del {
    color: var(--color-text-4);
    opacity: 0;
}

.key-item:hover .key-del {
    opacity: 1;
}

.type-badge {
    display: inline-block;
    min-width: 18px;
    text-align: center;
    font-size: 11px;
    font-weight: 700;
    color: #fff;
    border-radius: 3px;
    padding: 1px 4px;
    text-transform: uppercase;
}

.t-string {
    background: #722ed1;
}

.t-hash {
    background: #f53f3f;
}

.t-list {
    background: #ff7d00;
}

.t-set {
    background: #00b42a;
}

.t-zset {
    background: #165dff;
}

.t-none {
    background: #86909c;
}

.key-detail-panel {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

.kd-key {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.kd-body {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: var(--base-padding);
}

.kv-lines {
    font-family: Consolas, monospace;
    font-size: var(--base-size-small);
    line-height: 1.7;
}

.kv-line {
    display: flex;
}

.ln-no {
    width: 36px;
    text-align: right;
    padding-right: var(--base-radius-large);
    color: var(--color-text-4);
    user-select: none;
    flex-shrink: 0;
}

.ln-txt {
    white-space: pre-wrap;
    word-break: break-all;
}

/* 慢日志 / 通用子视图：撑满剩余高度，工具栏固定、内容区滚动 */
.slowlog-view,
.monitor-view {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

.view-toolbar {
    flex-shrink: 0;
    margin-bottom: var(--base-padding);
}

.table-wrap {
    flex: 1;
    min-height: 0;
    overflow: auto;
}

/* 监控 */
.mon-toolbar {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: var(--base-margin);
}

.mon-count {
    color: var(--color-text-3);
    font-size: var(--base-size-small);
}

.mon-list {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-default);
    background: var(--color-bg-1);
    color: var(--color-text-1);
    font-family: Consolas, monospace;
    font-size: var(--base-size-small);
    padding: var(--base-padding);
}

.mon-line {
    display: flex;
    gap: 8px;
    line-height: 1.6;
    white-space: pre-wrap;
    word-break: break-all;
}

.mon-time {
    color: #86909c;
    flex-shrink: 0;
}

.mon-meta {
    color: #ffb400;
    flex-shrink: 0;
}

.mon-cmd {
    color: #4ac7ff;
}

/* ---- 本地服务面板 ---- */
/* srv-list 复用 conn-list 的 flex 行为，无需额外限高 */

/* 版本管理 / 安装进度 / 日志状态 / 实例表单的样式已随各自弹窗迁入 components/*.vue 的 scoped 样式 */
</style>
