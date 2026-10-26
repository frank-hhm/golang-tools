<template>
    <a-row class="redis-page" :gutter="6">
        <!-- 左侧固定 30%（连接上50% + 本地服务下50%），右侧主区域铺满剩余空间 -->
        <a-col :span="6" class="side-col">
            <a-row class="redis-pages">
                <a-col class="side-cols" :span="24">
                    <a-card class="side-card card-head-tool" size="small" :body-style="{
                        padding: 0
                    }">
                        <template #title>连接</template>
                        <template #extra>
                            <a-button size="small" @click="openAddModal()">
                                新增连接
                            </a-button>
                        </template>
                        <a-list :bordered="false" size="small">
                            <a-list-item v-for="c in savedConns" :key="c.id" @click="openConnection(c)">
                                <a-space>
                                    <com-badge :status="isOpen(c.id) ? 'success' : 'normal'" :animated="isOpen(c.id)" />
                                    <div class="conn-name">{{ c.name || (c.host + ':' + c.port) }}</div>
                                </a-space>
                                <template #actions>
                                    <a-dropdown trigger="hover" @select="(v: any) => onConnAction(v, c)">
                                        <icon-more class="conn-more" @click.stop />
                                        <template #content>
                                            <a-doption value="open"><icon-play-arrow /> 打开连接</a-doption>
                                            <a-doption value="edit"><icon-edit /> 编辑</a-doption>
                                            <a-doption value="del"><icon-delete /> 删除</a-doption>
                                        </template>
                                    </a-dropdown>
                                </template>
                            </a-list-item>
                            <a-list-item v-if="savedConns.length === 0">
                                <div class="conn-empty">暂无连接</div>
                            </a-list-item>
                        </a-list>
                    </a-card>
                </a-col>
                <a-col class="side-cols mt6" :span="24">
                    <a-card class="side-card card-head-tool" size="small" :body-style="{
                        padding: 0
                    }">
                        <template #title>本地服务</template>
                        <template #extra>
                            <div class="srv-head-actions">
                                <a-button size="small" @click="openInstModal()">
                                    新建实例
                                </a-button>
                                <a-button size="small" @click="openVersionsModal()">
                                    版本管理
                                </a-button>
                            </div>
                        </template>
                        <!-- 引擎没起时先把话说明白：否则要等点「启动」撞一条管道错误才知道是 Docker 没跑 -->
                        <common-docker-off class="srv-docker-off" :count="dockerInstCount" tool-label="Redis" />
                        <a-list :bordered="false" size="small">
                            <a-list-item v-for="inst in srvInstances" :key="inst.id">
                                <a-space>
                                    <com-badge :status="srvStates[inst.id]?.running ? 'success' : 'normal'"
                                        :animated="!!srvStates[inst.id]?.running" />
                                    <a-tooltip :content="inst.name + ':' + inst.port">
                                        <span class="conn-name">{{ inst.name }}</span>
                                    </a-tooltip>
                                    <!-- 容器实例不占本机版本库，用镜像 tag 标身份 -->
                                    <a-tooltip v-if="inst.mode === 'docker'"
                                        :content="imgMissing(inst.image)
                                            ? `镜像 ${inst.image} 本机还没有，启动时会先自动拉取（可能几分钟）`
                                            : `镜像 ${inst.image}：这就是该实例的 Redis 版本`">
                                        <a-tag :color="imgMissing(inst.image) ? 'orange' : 'purple'">Docker
                                            {{ imgTag(inst.image) }}</a-tag>
                                    </a-tooltip>
                                    <a-tag v-else color="arcoblue">v{{ inst.version }}</a-tag>
                                </a-space>
                                <template #actions>

                                    <a-dropdown trigger="hover" @select="(v: any) => onSrvAction(v, inst)">
                                        <icon-more class="conn-more" @click.stop />
                                        <template #content>
                                            <a-doption v-if="!srvStates[inst.id]?.running"
                                                value="start"><icon-play-arrow />
                                                启动</a-doption>
                                            <a-doption v-else value="stop"><icon-pause /> 停止</a-doption>
                                            <a-doption value="logs"><icon-list /> 日志/状态</a-doption>
                                            <a-doption value="connect"><icon-link /> 连接</a-doption>
                                            <a-doption value="edit"><icon-edit /> 编辑</a-doption>
                                            <a-doption value="del"><icon-delete /> 删除</a-doption>
                                        </template>
                                    </a-dropdown>
                                </template>
                            </a-list-item>
                            <a-list-item v-if="srvInstances.length === 0">
                                <div class="conn-empty">暂无实例</div>
                            </a-list-item>
                        </a-list>
                    </a-card>
                </a-col>
            </a-row>
        </a-col>
        <a-col :span="18" class="main-col">
            <a-card class="main-panel empty-card card-head-tool" v-if="openTabs.length === 0">
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
                        <a-radio value="keys"><icon-list /> 键详情</a-radio>
                        <a-radio value="slowlog"><icon-clock-circle /> 慢日志</a-radio>
                        <a-radio value="monitor"><icon-scan /> 监控命令</a-radio>
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
                            <a-button size="small" @click="loadStatus()"><template #icon><icon-refresh /></template>刷新</a-button>
                        </template>
                        <div class="stat-cards">
                            <div class="stat-card">
                                <div class="stat-circle stat-circle-up"><icon-clock-circle /></div>
                                <div class="stat-info">
                                    <div class="stat-label">运行时间</div>
                                    <div class="stat-value">{{ formatUptime(st.status?.uptimeSeconds || 0) }}</div>
                                </div>
                            </div>
                            <div class="stat-card">
                                <div class="stat-circle stat-circle-client"><icon-user /></div>
                                <div class="stat-info">
                                    <div class="stat-label">已连客户端</div>
                                    <div class="stat-value">{{ st.status?.connectedClients ?? 0 }}</div>
                                </div>
                            </div>
                            <div class="stat-card">
                                <div class="stat-circle stat-circle-key"><icon-list /></div>
                                <div class="stat-info">
                                    <div class="stat-label">键总数</div>
                                    <div class="stat-value">{{ st.status?.totalKeys ?? 0 }}</div>
                                </div>
                            </div>
                            <div class="stat-card">
                                <div class="stat-circle stat-circle-mem"><icon-thunderbolt /></div>
                                <div class="stat-info">
                                    <div class="stat-label">内存使用</div>
                                    <div class="stat-value">{{ st.status?.usedMemoryHuman || '0' }}</div>
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
    <a-modal v-model:visible="st.connModal" :title="st.connEditing ? '编辑连接' : '添加新连接'" :width="460"
        @ok="saveConnection">
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
                <a-radio-group v-model="st.saveTarget" type="button" size="medium">
                    <a-radio value="file"><icon-save /> 文件</a-radio>
                    <a-radio value="cache"><icon-storage /> 缓存</a-radio>
                </a-radio-group>
                <template #extra>
                    <div>文件：保存到用户配置目录，跨会话持久；缓存：保存到本地 localStorage。</div>
                </template>
            </a-form-item>
        </a-form>
    </a-modal>

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

    <!-- 本地服务：版本管理（宽度 820：Docker 镜像表带上「使用实例」列后 680 会横向溢出） -->
    <!-- 固定高度：三个页签内容高矮不一，不钉住的话切一下弹窗就跳一次高度 -->
    <a-modal v-model:visible="versionsVisible" title="Redis 版本管理" :width="820" :footer="false"
        :body-style="{ height: 'min(600px, calc(100vh - 220px))', overflowY: 'auto' }">
        <a-space direction="vertical" fill :size="12">
            <!-- 类型切换：本地 / 线上 / Docker 只看一类，不用在长弹窗里找 -->
            <a-radio-group size="medium" v-model="verTab" type="button">
                <a-radio value="local">已安装版本 {{ localVersions.length }}</a-radio>
                <a-radio value="remote">在线可安装 {{ remoteVersions.length }}</a-radio>
                <a-radio value="docker">Docker 实例 {{ dockerInstCount }}</a-radio>
            </a-radio-group>
            <!-- 已安装版本 -->
            <div v-show="verTab === 'local'">
            <a-card size="small" :body-style="{ padding: '8px 12px' }">
                <template #title><span class="ver-sec-title"><icon-storage /> 已安装版本</span></template>
                <div class="ver-scroll">
                    <div v-for="v in localVersions" :key="v.version + v.source" class="ver-item">
                        <a-tag size="small"
                            :color="v.source === 'bundled' ? 'green' : (v.source === 'downloaded' ? 'arcoblue' : (v.source === 'system' ? 'gray' : 'orange'))">{{
                                srcLabel(v.source) }}</a-tag>
                        <span class="ver-name">Redis {{ v.version }}</span>
                        <a-tooltip :content="v.path">
                            <span class="ver-path">{{ v.path }}</span>
                        </a-tooltip>
                        <a-popconfirm :content="`确定删除 Redis ${v.version}？若仍有运行中的实例引用该版本会被拒绝。`"
                            type="warning" position="lt" :disabled="v.source === 'bundled' || v.source === 'system'" @ok="removeVersion(v)">
                            <a-button size="small" status="danger" :disabled="v.source === 'bundled' || v.source === 'system'">删除</a-button>
                        </a-popconfirm>
                    </div>
                    <a-empty v-if="localVersions.length === 0" :image="false" description="尚未安装任何版本，可从下方在线安装或添加本机目录" />
                </div>
                <div class="ver-actions">
                    <a-input v-model="localDirInput" size="small" placeholder="本机 redis-server 所在目录" allow-clear />
                    <a-button size="small" @click="chooseLocalDir"><template
                            #icon><icon-folder /></template>选目录</a-button>
                    <a-button size="small" @click="addLocalDir">添加</a-button>
                </div>
                <div class="ver-hint">离线安装：将 redis zip 放入 <span class="mono">{{ srvServersDir }}</span> 后点「安装」即可</div>
            </a-card>
            </div>

            <!-- Docker 镜像与容器：Docker 实例的「版本」就是镜像 tag（redis:7.4-alpine） -->
            <div v-show="verTab === 'docker'">
                <docker-images keyword="redis" tool-label="Redis" repo="redis" :presets="redisImagePresets"
                    :instances="srvInstances" :states="srvStates" :reload-key="verReload" @create="newInstanceFromImage"
                    @use="editSrvById" @changed="refreshSrvStates" />
            </div>

            <!-- 在线版本 -->
            <div v-show="verTab === 'remote'">
            <a-card size="small" :body-style="{ padding: '8px 12px' }">
                <template #title>
                    <span class="ver-sec-title"><icon-cloud /> 在线版本（GitHub redis-windows 发行版，&gt;=7.0）</span>
                </template>
                <template #extra>
                    <a-button size="small" :loading="remoteLoading" @click="loadRemoteVersions">
                        <template #icon><icon-refresh /></template>刷新
                    </a-button>
                </template>
                <div class="ver-scroll">
                    <div v-for="r in remoteVersions" :key="r.version" class="ver-item">
                        <span class="ver-name">Redis {{ r.version }}</span>
                        <span class="ver-path">{{ r.size ? (r.size / 1024 / 1024).toFixed(0) + ' MB' : '' }}</span>
                        <a-button size="small"
                            :disabled="!!installProgress[r.version] || installedSet.has(r.version)"
                            @click="installVersion(r)">{{ installedSet.has(r.version) ? '已安装' : '安装' }}</a-button>
                    </div>
                    <a-empty v-if="remoteVersions.length === 0" :image="false"
                        :description="remoteLoading ? '加载中…' : '点「刷新」获取版本列表'" />
                </div>
                <div v-for="(p, ver) in installProgress" :key="'p' + ver" class="inst-prog">
                    <div class="inst-prog-row">
                        <span class="ver-name">{{ ver }}</span>
                        <a-progress :percent="(p.percent || 0) / 100" size="small"
                            :status="p.stage === 'error' ? 'danger' : 'normal'" style="flex: 1" />
                        <span class="inst-stage">{{ stageText(p) }}</span>
                        <a-button v-if="p.stage === 'error'" size="small" status="danger"
                            @click="retryInstall(ver as string)">
                            <template #icon><icon-refresh /></template>重试
                        </a-button>
                    </div>
                    <div v-if="p.stage === 'error' && p.note" class="inst-err">
                        <a-tooltip :content="p.note" position="tl">
                            <a-alert type="error" :show-icon="false">{{ p.note }}</a-alert>
                        </a-tooltip>
                    </div>
                </div>
            </a-card>

            <!-- 安装日志 -->
            <a-card size="small" :body-style="{ padding: '8px 12px' }">
                <template #title><span class="ver-sec-title"><icon-history /> 安装日志</span></template>
                <template #extra>
                    <a-button size="small" @click="installLogs = []"><template
                            #icon><icon-delete /></template>清空</a-button>
                </template>
                <div class="ver-scroll inst-log">
                    <div v-for="(l, i) in installLogs" :key="i" class="inst-log-line">{{ l }}</div>
                    <a-empty v-if="installLogs.length === 0" :image="false" description="暂无安装记录" />
                </div>
            </a-card>
            </div>
        </a-space>
    </a-modal>

    <!-- 本地服务：实例编辑 -->
    <a-modal v-model:visible="instVisible" :title="instForm.id ? '编辑实例' : '新建实例'" :width="520"
        :body-style="{ maxHeight: '72vh' }" @ok="saveInstance">
        <!-- 引擎没起时先把原因说清楚，否则要到启动那一步才报错；吸顶不占表单位 -->
        <common-modal-tip v-if="instForm.mode === 'docker' && dockerErr" :text="dockerErr">
          <a-button size="small" @click="openDockerWindow">打开 Docker 面板</a-button>
        </common-modal-tip>
        <a-form :model="instForm" layout="vertical" class="inst-form">
            <a-form-item field="name" label="实例名称">
                <a-input v-model="instForm.name" placeholder="例如 local-dev" />
            </a-form-item>
            <a-form-item field="mode" label="运行模式">
                <a-radio-group size="medium" v-model="instForm.mode" type="button" @change="onModeChange">
                    <a-radio value="local"><icon-computer /> 本地</a-radio>
                    <a-radio value="docker"><icon-experiment /> Docker</a-radio>
                </a-radio-group>
                <template #extra>
                    <template v-if="instForm.mode === 'docker'">
                        容器内跑 redis-server，数据目录挂载到 /data；首次启动会自动拉取镜像（可能几分钟）
                    </template>
                    <template v-else>使用本机插件目录下的 redis-server，可在「版本管理」获取</template>
                </template>
            </a-form-item>
            <!-- 本地：版本单占一行；Docker：镜像 + 容器名同一行，两种模式行数一致，切模式时弹窗高度不跳 -->
            <a-form-item v-if="instForm.mode !== 'docker'" field="version" label="Redis 版本" tooltip="需已安装，可在「版本管理」获取">
                <a-select v-model="instForm.version" placeholder="需已安装，可在「版本管理」获取">
                    <a-option v-for="v in localVersions" :key="v.version" :value="v.version">
                        {{ v.version }}（{{ srcLabel(v.source) }}）
                    </a-option>
                </a-select>
            </a-form-item>
            <a-row v-else :gutter="12">
                <a-col :span="14">
                    <a-form-item class="img-item" field="image" label="镜像" required>
                        <div class="img-row">
                            <a-select v-model="instForm.image" class="img-select" placeholder="选择已安装镜像"
                                allow-create filterable :loading="dockerImagesLoading">
                                <!-- 自闭合的 a-option 没有文字，label 会是空串，下拉里全是空白行 —— 显式写上文字 -->
                                <a-option v-for="im in imageOptions" :key="im" :value="im">{{ im }}</a-option>
                                <template #empty>
                                    <a-empty :image="false" description="无可用镜像" />
                                </template>
                            </a-select>
                            <!-- 配镜像时随手打开 Docker 管理窗口（拉取 / 看进度） -->
                            <a-tooltip content="打开 Docker 管理窗口：拉镜像、看拉取进度">
                                <a-button size="small" class="img-open" @click="openDockerWindow">
                                    <template #icon><icon-storage /></template>
                                </a-button>
                            </a-tooltip>
                        </div>
                        <template #help>
                            <span v-if="dockerImagesLoading">正在读取本地镜像…</span>
                            <span v-else-if="!imageOptions.length && !instForm.image" class="img-none">
                                本地无可用镜像，<a-link @click="openDockerWindow">点击去安装</a-link>
                            </span>
                        </template>
                    </a-form-item>
                </a-col>
                <a-col :span="10">
                    <a-form-item field="containerName" label="容器名" tooltip="留空自动生成 go-tools-redis-实例ID">
                        <a-input v-model="instForm.containerName" placeholder="留空自动生成" />
                    </a-form-item>
                </a-col>
            </a-row>
            <a-row :gutter="12">
                <a-col :span="12">
                    <a-form-item field="port" label="端口" :error="portError"
                        tooltip="只监听 127.0.0.1，Docker 与本地模式一样不对外暴露">
                        <a-input-number v-model="instForm.port" :min="1" :max="65535"
                            @change="portError = ''" @blur="checkPortTaken" />
                    </a-form-item>
                </a-col>
                <a-col :span="12">
                    <a-form-item field="maxMemoryMB" label="最大内存 (MB)" tooltip="0 = 不限">
                        <a-input-number v-model="instForm.maxMemoryMB" :min="0" />
                    </a-form-item>
                </a-col>
            </a-row>
            <a-form-item field="password" label="密码" tooltip="留空 = 不设置 requirepass">
                <a-input-password v-model="instForm.password" placeholder="留空则不设置" />
            </a-form-item>
            <a-form-item field="appendOnly" label="持久化 AOF" tooltip="开启后每次写入追加到 appendonly.aof，重启不丢数据">
                <a-switch v-model="instForm.appendOnly" />
            </a-form-item>
            <a-form-item field="dataDir" label="数据目录"
                :tooltip="instForm.mode === 'docker' ? 'Docker 模式该目录会挂载进容器 /data，RDB/AOF 都落在这儿' : '留空 = 程序数据目录下按实例 ID 分配'">
                <a-input v-model="instForm.dataDir" placeholder="空 = 程序数据目录下按 ID 分配" />
            </a-form-item>
        </a-form>
    </a-modal>

    <!-- 本地服务：日志/状态 -->
    <a-modal v-model:visible="logsVisible" :title="logsInst ? `实例 ${logsInst.name}（:${logsInst.port}）` : '实例日志'"
        :width="660" :footer="false">
        <div v-if="logsStatus" class="ls-grid">
            <div class="ls-item"><span>状态</span><b :class="{ ok: logsStatus.running }">{{ logsStatus.running ? '运行中'
                : '已停止'
                    }}</b></div>
            <div class="ls-item"><span>版本</span><b>{{ logsStatus.version || '-' }}</b></div>
            <div class="ls-item"><span>运行模式</span><b>{{ logsStatus.mode === 'docker' ? 'Docker 容器' : '本地进程'
                    }}</b></div>
            <!-- 容器里 redis-server 的 pid 在虚拟机 namespace，报给看只会误导，改看容器名与容器状态 -->
            <div class="ls-item" v-if="logsStatus.mode === 'docker'">
                <span>容器</span>
                <a-tooltip :content="logsStatus.containerId || logsStatus.container || '-'">
                    <b>{{ logsStatus.container || '-' }}{{ logsStatus.containerState ? `（${logsStatus.containerState}）` :
                            '' }}</b>
                </a-tooltip>
            </div>
            <div class="ls-item" v-else><span>PID</span><b>{{ logsStatus.pid || '-' }}</b></div>
            <div class="ls-item"><span>运行时长</span><b>{{ fmtUptimeS(logsStatus.uptimeSeconds) }}</b></div>
            <div class="ls-item"><span>已连客户端</span><b>{{ logsStatus.connectedClients ?? '-' }}</b></div>
            <div class="ls-item"><span>键总数</span><b>{{ logsStatus.totalKeys ?? '-' }}</b></div>
            <div class="ls-item"><span>内存使用</span><b>{{ logsStatus.usedMemoryHuman || '-' }}</b></div>
        </div>
        <div ref="logBoxRef" class="ls-log">
            <div v-for="(l, i) in logsLines" :key="i" class="ls-line">{{ l }}</div>
            <div v-if="logsLines.length === 0" class="conn-empty">暂无日志</div>
        </div>
        <div class="ls-actions">
            <a-button size="small" @click="logsLines = []"><template #icon><icon-delete /></template>清空</a-button>
            <a-button size="small" :disabled="!logsStatus?.running" @click="connectInstance(logsInst)">
                <template #icon><icon-link /></template>连接此实例
            </a-button>
        </div>
    </a-modal>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, nextTick, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import { useRoute } from "vue-router";
import { useToolWindow } from "@/views/index/useToolWindow";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const CACHE_KEY = "redis_connections_cache";

interface ConnConfig { id: string; name: string; host: string; port: number; password: string; db: number }
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
    saveTarget: "file",
    addKeyModal: false,
    batchKeyModal: false,
});

const connForm = reactive<ConnConfig>({ id: "", name: "", host: "127.0.0.1", port: 6379, password: "", db: 0 });
const keyForm = reactive({ key: "", value: "", ttl: 0 });
const batchForm = reactive({ keyPrefix: "key", valuePrefix: "content", start: 1, end: 100, ttl: 0 });
const batchGenerating = ref(false);

const activeTabName = computed(() => openTabs.value.find((t) => t.id === st.activeTab)?.name || "");
const activeCfg = computed(() => openTabs.value.find((t) => t.id === st.activeTab)?.cfg);

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

async function persistConnections() {
    const data = JSON.parse(JSON.stringify(savedConns.value));
    // 缓存始终镜像一份，避免丢失
    localStorage.setItem(CACHE_KEY, JSON.stringify(data));
    if (st.saveTarget === "file") {
        try {
            await GoTools.RedisSaveConnections(data);
        } catch (e) {
            /* ignore */
        }
    }
}

function openAddModal() {
    st.connEditing = false;
    Object.assign(connForm, { id: "", name: "", host: "127.0.0.1", port: 6379, password: "", db: 0 });
    st.connModal = true;
}

function openEditModal(c: ConnConfig) {
    st.connEditing = true;
    Object.assign(connForm, { ...c });
    st.connModal = true;
}

async function saveConnection() {
    if (!connForm.host) {
        Message.warning("请填写主机");
        return;
    }
    const item: ConnConfig = { ...connForm };
    if (!item.name) item.name = `${item.host}:${item.port}`;
    if (st.connEditing) {
        const i = savedConns.value.findIndex((c) => c.id === item.id);
        if (i >= 0) savedConns.value[i] = item;
    } else {
        item.id = "c" + Date.now().toString(36);
        savedConns.value.push(item);
    }
    await persistConnections();
    st.connModal = false;
    Message.success("已保存到" + (st.saveTarget === "file" ? "文件" : "缓存"));
}

function onConnAction(v: string, c: ConnConfig) {
    if (v === "open") openConnection(c);
    else if (v === "edit") openEditModal(c);
    else if (v === "del") confirmDeleteConn(c);
}

function confirmDeleteConn(c: ConnConfig) {
    Modal.warning({
        title: "删除连接",
        content: `确定删除连接「${c.name}」？`,
        hideCancel: false,
        onOk: async () => {
            if (isOpen(c.id)) closeTab(c.id);
            savedConns.value = savedConns.value.filter((x) => x.id !== c.id);
            await persistConnections();
        },
    });
}

async function openConnection(c: ConnConfig) {
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
    { title: "命令", dataIndex: "command" },
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
    { title: "Value", dataIndex: "value" },
];
const hashData = computed(() => Object.entries(st.keyValue.hash || {}).map(([field, value]) => ({ field, value })));

const listCols = [
    { title: "#", dataIndex: "idx", width: 70 },
    { title: "Value", dataIndex: "value" },
];
const listData = computed(() => (st.keyValue.list || []).map((value: string, i: number) => ({ idx: i, value })));

const setCols = [{ title: "Member", dataIndex: "value" }];
const setData = computed(() => (st.keyValue.set || []).map((value: string) => ({ value })));

const zsetCols = [
    { title: "Member", dataIndex: "member" },
    { title: "Score", dataIndex: "score", width: 140 },
];
const zsetData = computed(() => st.keyValue.zset || []);

const strLines = computed<string[]>(() => String(st.keyValue.str ?? "").split("\n"));

/* ============================ 工具函数 ============================ */

function formatTTL(ttl: number): string {
    if (ttl < 0) return "持久";
    const h = Math.floor(ttl / 3600);
    const m = Math.floor((ttl % 3600) / 60);
    const s = ttl % 60;
    const p = (n: number) => n.toString().padStart(2, "0");
    return `${p(h)}:${p(m)}:${p(s)}`;
}

function formatUptime(sec: number): string {
    const d = Math.floor(sec / 86400);
    const h = Math.floor((sec % 86400) / 3600);
    if (d > 0) return `${d} 天 ${h} 小时`;
    if (h > 0) return `${h} 小时`;
    return `${Math.floor(sec / 60)} 分钟`;
}

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

/* TTL 本地倒计时（仅展示，不改变真实值） */
watch(
    () => st.keyValue.ttl,
    (v) => {
        if (typeof v === "number" && v > 0) {
            // 简单每秒递减，切换键时重置
        }
    },
);

/* ============================ 本地服务管理 ============================ */

interface SrvInstanceConfig {
    id: string; name: string; version: string; port: number;
    password: string; appendOnly: boolean; maxMemoryMB: number; dataDir: string;
    // mode: local | docker；容器模式不占本机版本库，靠 image 定位镜像
    mode: string; image: string; containerName: string;
}

/** Docker 实例的版本就是镜像 tag：列表里只展示 tag（7.4-alpine），完整镜像名走 tooltip */
function imgTag(image: string): string {
    const s = String(image || "");
    const i = s.indexOf(":");
    return i >= 0 ? s.slice(i + 1) : (s || "Docker");
}

/** 版本管理里 Docker 镜像区的常用 tag：alpine 体积小，官方长期维护 */
const redisImagePresets = ["redis:7.4-alpine", "redis:7.4", "redis:7.2-alpine", "redis:7.2", "redis:alpine", "redis:7.0-alpine"];

const srvInstances = ref<SrvInstanceConfig[]>([]);
const srvStates = reactive<Record<string, any>>({});
const localVersions = ref<any[]>([]);
const remoteVersions = ref<any[]>([]);
const remoteLoading = ref(false);
const installProgress = reactive<Record<string, any>>({});
const installLogs = ref<string[]>([]);
const versionsVisible = ref(false);
/** 版本管理里 Docker 镜像区的刷新信号：弹窗不销毁，每次打开自增一次触发重拉 */
const verReload = ref(0);
/** 版本管理弹窗的类型切换：本地 / 线上 / Docker 只看一类 */
const verTab = ref<"local" | "remote" | "docker">("local");
/** 有多少实例跑在容器里（它们的版本由镜像 tag 决定，不占本机版本库） */
const dockerInstCount = computed(() => srvInstances.value.filter((i: any) => i.mode === "docker").length);
const instVisible = ref(false);
const logsVisible = ref(false);
const localDirInput = ref("");
const srvServersDir = ref("");
const logsInst = ref<SrvInstanceConfig | null>(null);
const logsStatus = ref<any>(null);
const logsLines = ref<string[]>([]);
const logBoxRef = ref<HTMLElement | null>(null);

const instForm = reactive<SrvInstanceConfig>({
    id: "", name: "", version: "", port: 6379, password: "", appendOnly: false, maxMemoryMB: 0, dataDir: "",
    mode: "local", image: "", containerName: "",
});
/** 端口占用提示：贴表单项底下（红字），保存前也用同一份结果拦一次 */
const portError = ref("");

/* Docker 模式表单：镜像候选只列本机已装好的 redis 系镜像（不猜预置 tag），列表为空就引导去安装 */
/* 名字不能回退成 dockerImages：script setup 下 kebab 标签 docker-images 会优先拿同名 setup 变量当组件，整个 Docker 区会渲染成空白 */
const dockerImageList = ref<string[]>([]);
const dockerImagesLoading = ref(false);
const dockerErr = ref("");
const imageOptions = computed(() => Array.from(new Set<string>(dockerImageList.value)));

/** 只刷新镜像清单（不写表单）：用来标记实例引用的镜像本机是否已拉取 */
async function probeDockerImages() {
    try { dockerImageList.value = (await GoTools.RedisSrvDockerImages()) || []; }
    catch (e) { /* 引擎未就绪就不做校验，免得误报 */ }
}
/** 清单非空且不含它 → 本机还没有；启动时会自动拉 */
function imgMissing(image: string): boolean {
    return !!image && dockerImageList.value.length > 0 && !dockerImageList.value.includes(image);
}

const installedSet = computed(() => new Set<string>(localVersions.value.map((v: any) => v.version)));

let offInstall: any = null;
const route = useRoute();
const { openToolWindow } = useToolWindow();
let offSelInst: any = null;
let srvTimer: any = null;

function pushInstallLog(txt: string) {
    if (!txt) return;
    const t = new Date();
    const pad = (n: number) => n.toString().padStart(2, "0");
    installLogs.value.push(`[${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}] ${txt}`);
    if (installLogs.value.length > 200) installLogs.value.splice(0, installLogs.value.length - 200);
}

function srcLabel(s: string): string {
    return s === "bundled" ? "内置" : s === "downloaded" ? "下载" : s === "system" ? "系统包" : "本机";
}

function stageText(p: any): string {
    if (!p) return "";
    if (p.stage === "downloading") return `下载中 ${p.percent || 0}%`;
    if (p.stage === "extracting") return `解压中 ${p.percent || 0}%`;
    if (p.stage === "done") return "完成";
    return "失败";
}

function fmtUptimeS(sec: number): string {
    if (!sec || sec <= 0) return "-";
    const d = Math.floor(sec / 86400); const h2 = Math.floor((sec % 86400) / 3600);
    const m = Math.floor((sec % 3600) / 60); const s = sec % 60;
    if (d > 0) return `${d}d ${h2}h ${m}m`; if (h2 > 0) return `${h2}h ${m}m`; return `${m}m ${s}s`;
}

async function loadLocalVersions() {
    try { localVersions.value = (await GoTools.RedisSrvListVersions()) || []; }
    catch (e) { localVersions.value = []; }
}

async function loadSrvInstances() {
    try { srvInstances.value = (await GoTools.RedisSrvListInstances()) || []; }
    catch (e) { srvInstances.value = []; }
    refreshSrvStates();
}

async function refreshSrvStates() {
    // 并发问：实例串行 await 就是 N 次往返，Web 模式跑在远程服务器上时尤其明显
    await Promise.all(srvInstances.value.map(async (inst) => {
        try {
            const s2 = await GoTools.RedisSrvStatus(inst.id);
            srvStates[inst.id] = s2;
            if (logsVisible.value && logsInst.value?.id === inst.id) logsStatus.value = s2;
        } catch (e) { /* 单个实例失败不影响其它实例 */ }
    }));
}

async function openVersionsModal() {
    versionsVisible.value = true;
    verTab.value = "local";
    verReload.value++;
    loadLocalVersions();
    if (!srvServersDir.value) {
        try { srvServersDir.value = (await GoTools.RedisSrvServersDir()) || ""; } catch (e) { /* ignore */ }
    }
}

async function loadRemoteVersions() {
    remoteLoading.value = true;
    try { remoteVersions.value = (await GoTools.RedisSrvRemoteVersions()) || []; }
    catch (e: any) { Message.error("获取远程版本失败: " + (e?.message || e)); }
    remoteLoading.value = false;
}

async function installVersion(r: any) {
    try {
        pushInstallLog(`提交安装请求：Redis ${r.version}`);
        await GoTools.RedisSrvInstall(r.version);
        installProgress[r.version] = { stage: "downloading", percent: 0 };
    } catch (e: any) {
        pushInstallLog(`安装请求被拒绝：${e?.message || e}`);
        Message.error("安装失败: " + (e?.message || e));
    }
}

function retryInstall(ver: string) {
    delete installProgress[ver];
    installVersion({ version: ver });
}

async function removeVersion(v: any) {
    try { await GoTools.RedisSrvRemoveVersion(v.version); Message.success("已删除"); loadLocalVersions(); }
    catch (e: any) { Message.error("删除失败: " + (e?.message || e)); }
}

async function chooseLocalDir() {
    try { const dir = await GoTools.RedisSrvChooseDir(); if (dir) localDirInput.value = dir; }
    catch (e) { /* cancel */ }
}

async function addLocalDir() {
    const dir = localDirInput.value.trim();
    if (!dir) { Message.warning("请先选择或填写目录"); return; }
    try { await GoTools.RedisSrvAddLocal(dir); Message.success("已添加本机目录"); localDirInput.value = ""; loadLocalVersions(); }
    catch (e: any) { Message.error("添加失败: " + (e?.message || e)); }
}

async function openInstModal(inst?: SrvInstanceConfig) {
    portError.value = "";
    dockerErr.value = "";
    if (inst) {
        // 历史配置没 mode/image 字段，归一化后再进表单，避免 radio 两档都没选中
        Object.assign(instForm, {
            ...inst,
            mode: inst.mode === "docker" ? "docker" : "local",
            image: inst.image || "",
            containerName: inst.containerName || "",
        });
    }
    else {
        // 默认端口避开其它实例已登记 / 系统已监听的
        let port = 6379;
        try { port = (await GoTools.RedisSrvSuggestPort(6379)) || port; } catch (e) { /* ignore */ }
        Object.assign(instForm, { id: "", name: "", version: localVersions.value[0]?.version || "", port, password: "", appendOnly: false, maxMemoryMB: 0, dataDir: "", mode: "local", image: "", containerName: "" });
    }
    if (localVersions.value.length === 0) loadLocalVersions();
    if (instForm.mode === "docker") onModeChange("docker");
    instVisible.value = true;
}

/** 切换运行模式：Docker 读本机镜像候选 + 引擎状态，不干预已有输入 */
async function onModeChange(v: any) {
    const mode = String(v ?? instForm.mode ?? "local");
    instForm.mode = mode;
    if (mode !== "docker") return;
    loadImageOptions();
    loadDockerEngine();
}

/** 版本管理里点「新建实例」：直接以该镜像开一个 Docker 实例（端口仍走可用端口探测） */
async function newInstanceFromImage(image: string) {
    versionsVisible.value = false;
    await openInstModal();
    instForm.mode = "docker";
    instForm.image = image;
    await onModeChange("docker");
}

/** 版本管理里点「使用实例」：从镜像视角转去改该实例的版本（镜像） */
async function editSrvById(id: string) {
    const inst = srvInstances.value.find((i) => i.id === id);
    if (!inst) return;
    versionsVisible.value = false;
    await openInstModal(inst);
}

async function loadImageOptions() {
    dockerImagesLoading.value = true;
    try { dockerImageList.value = (await GoTools.RedisSrvDockerImages()) || []; }
    catch (e) { dockerImageList.value = []; } // 引擎不可用时当作没有镜像，走“去安装”引导
    dockerImagesLoading.value = false;
    // 还没选值且有候选时先填第一个，省一步点击
    if (instForm.mode === "docker" && !instForm.image && dockerImageList.value.length) instForm.image = dockerImageList.value[0];
}

async function loadDockerEngine() {
    try {
        const [ok, msg] = await GoTools.RedisSrvDockerAvailable();
        dockerErr.value = ok ? "" : (msg || "Docker 引擎不可用");
    } catch (e: any) { dockerErr.value = e?.message || String(e); }
}

/** 打开 Docker 管理窗口（独立 webview，单例复用）：配镜像时直接过去拉，不用回主页找入口 */
function openDockerWindow() {
    openToolWindow("/tools/docker", "Docker管理", { single: true, width: 1200, height: 800 });
}

async function checkPortTaken() {
    if (!instForm.port) { portError.value = ""; return; }
    try {
        const reason = await GoTools.RedisSrvPortTaken(instForm.port, instForm.id || "");
        if (!reason) { portError.value = ""; return; }
        const suggest = await GoTools.RedisSrvSuggestPort(instForm.port);
        portError.value = suggest && suggest !== instForm.port ? `${reason}，可用：${suggest}` : reason;
    } catch (e) { /* ignore */ }
}

async function saveInstance() {
    if (!instForm.name) { Message.warning("请填写实例名称"); return; }
    // 两种模式的必填项不一样：本地看本机版本库，容器看镜像（后端也会再校一次）
    if (instForm.mode === "docker") {
        if (!instForm.image) { Message.warning("Docker 模式请选择或输入镜像"); return; }
    } else if (!instForm.version) { Message.warning("请选择 Redis 版本"); return; }
    if (!instForm.port) { Message.warning("请填写端口"); return; }
    // 端口占用先在本地拦一次，后端保存时还会再校验
    try {
        const reason = await GoTools.RedisSrvPortTaken(instForm.port, instForm.id || "");
        if (reason) {
            const suggest = await GoTools.RedisSrvSuggestPort(instForm.port);
            portError.value = suggest && suggest !== instForm.port ? `${reason}，可用：${suggest}` : reason;
            Message.warning(portError.value);
            return;
        }
    } catch (e) { /* 探测失败不阻断，后端兜底 */ }
    try {
        // 编辑前先记下原配置：切了版本/镜像且实例在跑，容器里还是旧版本，得重启才生效
        const old = instForm.id ? srvInstances.value.find((i) => i.id === instForm.id) : null;
        const switched = !!old && (old.mode !== instForm.mode || old.image !== instForm.image || old.version !== instForm.version);
        const wasRunning = !!old && !!srvStates[old.id]?.running;
        await GoTools.RedisSrvSaveInstance({ ...instForm });
        instVisible.value = false; Message.success("已保存"); await loadSrvInstances();
        if (switched && wasRunning) setTimeout(() => askRestartAfterSwitch({ ...instForm }), 240);
    } catch (e: any) { Message.error("保存失败: " + (e?.message || e)); }
}

/** 切了版本/镜像又没重启：跑着的还是旧版本，问一句要不要现在就换过去 */
function askRestartAfterSwitch(inst: SrvInstanceConfig) {
    const what = inst.mode === "docker" ? `镜像 ${inst.image}` : `Redis ${inst.version}`;
    Modal.confirm({
        title: "版本已切换",
        content: `${what} 已写入配置，但实例「${inst.name}」还在跑旧版本；重启后才会生效（期间会短暂中断服务）。`,
        okText: "立即重启",
        cancelText: "稍后手动重启",
        onOk: async () => {
            try {
                // Redis 没有单独的 restart，停完再起
                await GoTools.RedisSrvStop(inst.id);
                await GoTools.RedisSrvStart(inst.id);
                Message.success(`实例 ${inst.name} 已重启，新版本已生效`);
            } catch (e: any) { Message.error("重启失败: " + (e?.message || e)); }
            loadSrvInstances();
        },
    });
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
        connectInstance(inst);
    } else if (v === "edit") {
        openInstModal(inst);
    } else if (v === "del") {
        Modal.warning({
            title: "删除实例", content: `确定删除实例「${inst.name}」？运行中会先停止；数据目录保留。`, hideCancel: false,
            onOk: async () => {
                try { await GoTools.RedisSrvRemoveInstance(inst.id); delete srvStates[inst.id]; await loadSrvInstances(); }
                catch (e: any) { Message.error("删除失败: " + (e?.message || e)); }
            },
        });
    }
}

function connectInstance(inst: SrvInstanceConfig | null) {
    if (!inst) return;
    if (!srvStates[inst.id]?.running) { Message.warning("请先启动实例"); return; }
    const cfg: ConnConfig = {
        id: "srv-" + inst.id, name: `${inst.name}(本地)`,
        host: "127.0.0.1", port: inst.port, password: inst.password, db: 0,
    };
    if (!savedConns.value.some((c) => c.id === cfg.id)) {
        savedConns.value.push(cfg); persistConnections();
    }
    openConnection(cfg);
}

let logsTimer: any = null;
async function openLogs(inst: SrvInstanceConfig) {
    logsInst.value = inst;
    logsLines.value = [];
    logsStatus.value = srvStates[inst.id] || null;
    logsVisible.value = true;
    try { logsLines.value = (await GoTools.RedisSrvLogs(inst.id, 300)) || []; } catch (e) { /* ignore */ }
    if (logsTimer) clearInterval(logsTimer);
    logsTimer = setInterval(async () => {
        if (!logsVisible.value) { clearInterval(logsTimer); logsTimer = null; return; }
        try {
            logsLines.value = (await GoTools.RedisSrvLogs(inst.id, 300)) || [];
            nextTick(() => { if (logBoxRef.value) logBoxRef.value.scrollTop = logBoxRef.value.scrollHeight; });
        } catch (e) { /* ignore */ }
    }, 2000);
}

/* ============================ 生命周期 ============================ */

onMounted(async () => {
    await loadConnections();
    await loadSrvInstances();
    // 镜像清单不进表单，只用来标记「实例引用的镜像本机还没拉」
    probeDockerImages();
    // 主页点击服务实例进入（?inst=）：直接打开该实例的日志/状态详情
    const wantId = String(route.query.inst ?? "");
    if (wantId) {
        const target = srvInstances.value.find((i) => i.id === wantId);
        if (target) openLogs(target);
    }
    loadLocalVersions();
    srvTimer = setInterval(refreshSrvStates, 3000);
    // 单例窗口已打开时，主页再次点击其他实例 → 通过事件切换
    offSelInst = Events.On("tool:select-inst", (ev: any) => {
        const p = ev?.data ?? ev;
        if (p?.type !== "redis" || !p?.id) return;
        const target = srvInstances.value.find((i) => i.id === String(p.id));
        if (target) openLogs(target);
    });
    offInstall = Events.On("redis:install", (ev: any) => {
        const p = ev?.data ?? ev;
        if (!p || !p.version) return;
        if (p.stage === "info") {
            pushInstallLog(p.note || "");
        } else if (p.stage === "done") {
            delete installProgress[p.version];
            pushInstallLog(`安装完成 Redis ${p.version}` + (p.note ? "：" + p.note : ""));
            Message.success(`Redis ${p.version} 安装完成`);
            loadLocalVersions();
        } else if (p.stage === "error") {
            installProgress[p.version] = p;
            pushInstallLog(`安装失败 ${p.version}：${p.note || "未知错误"}（可将 redis zip 放入插件目录后离线安装）`);
            Modal.error({
                title: "安装失败",
                content: `${p.version}：${p.note || "未知错误"}。可将 redis zip 放入插件目录后重试（离线安装）。`,
            });
        } else {
            installProgress[p.version] = p;
        }
    });
    offMonitor = Events.On("redis:monitor", (ev: any) => {
        const line = ev?.data ?? ev;
        st.monitorLines.push(line);
        if (st.monitorLines.length > 3000) st.monitorLines.splice(0, st.monitorLines.length - 3000);
    });
});

onUnmounted(() => {
    if (offMonitor) offMonitor();
    if (offInstall) offInstall();
    if (offSelInst) offSelInst();
    if (srvTimer) clearInterval(srvTimer);
    if (logsTimer) clearInterval(logsTimer);
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

/* 左侧列（a-col，约 30% 宽）：上下两个面板各占 50% 高 */
.side-col {
    height: 100%;
    min-width: 0;
    display: flex;
    flex-direction: column;
}

.redis-pages {
    height: calc(100% - 6px);
}

.side-cols {
    height: 50%;
    width: 100%;
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

.conn-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.conn-more {
    color: var(--color-text-3);
}

.conn-empty {
    color: var(--color-text-3);
    text-align: center;
    font-size: var(--base-size-small);
    padding: var(--base-padding);
}

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

/* 运行信息统计卡片：彩色圆形图标 + 标题 + 大数字 */
.stat-cards {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    gap: var(--base-padding);
}

.stat-card {
    display: flex;
    align-items: center;
    gap: var(--base-padding);
    padding: var(--base-padding);
    background: var(--color-bg-1);
    border: 1px solid var(--color-border-1);
    border-radius: var(--base-radius-default);
    transition: box-shadow 0.2s;
}

.stat-card:hover {
    box-shadow: var(--shadow-2-bottom);
}

.stat-circle {
    width: 44px;
    height: 44px;
    border-radius: var(--base-radius-default);
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: var(--base-size-large-x);
    flex-shrink: 0;
}

.stat-circle-up {
    background: rgba(var(--orange-6), 0.12);
    color: rgb(var(--orange-6));
}

.stat-circle-client {
    background: rgba(var(--cyan-6), 0.12);
    color: rgb(var(--cyan-6));
}

.stat-circle-key {
    background: rgba(var(--purple-6), 0.12);
    color: rgb(var(--purple-6));
}

.stat-circle-mem {
    background: rgba(var(--arcoblue-6), 0.12);
    color: rgb(var(--arcoblue-6));
}

.stat-info {
    flex: 1;
    min-width: 0;
}

.stat-label {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}

.stat-value {
    font-weight: bold;
    font-size: var(--base-size-default);
    line-height: 1.2;
    color: var(--color-text-1);
    margin-top: var(--base-margin);
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

/* ---- 版本管理弹窗 ---- */
.ver-sec-title {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-weight: 600;
    font-size: var(--base-size-default);
}

.ver-scroll {
    max-height: 170px;
    overflow-y: auto;
}

.ver-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: var(--base-radius-default) var(--base-padding);
    border-radius: var(--base-radius-default);
    font-size: var(--base-size-small);
}

.ver-item:hover {
    background: var(--color-fill-2);
}

.ver-name {
    font-weight: 500;
    min-width: 100px;
}

.ver-path {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--color-text-3);
    font-size: var(--base-size-small);
}

.ver-actions {
    display: flex;
    gap: 8px;
    align-items: center;
    margin-top: var(--base-padding);
}

.ver-actions :deep(.arco-input-wrapper) {
    flex: 1;
}

.ver-hint {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    margin-top: var(--base-radius-default);
}

.mono {
    font-family: Consolas, monospace;
    background: var(--color-fill-2);
    padding: var(--base-radius-small) var(--base-radius-default);
    border-radius: var(--base-radius-small);
}

/* 安装进度 */
.inst-prog {
    margin-top: var(--base-padding);
}

.inst-prog-row {
    display: flex;
    align-items: center;
    gap: 8px;
}

.inst-stage {
    font-size: var(--base-size-small);
    color: var(--color-text-2);
    min-width: 80px;
}

/* 安装失败提示：内容是 a-alert，这里只管间距，颜色交给 alert 的类型色 */
.inst-err {
    padding: var(--base-radius-default) 0 0 0;
    word-break: break-all;
    white-space: pre-wrap;
}

/* 安装失败原因常带着下载地址：两行截断，不把版本弹窗撑高；完整内容悬停看 */
.inst-err :deep(.arco-alert-content) {
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
    white-space: pre-wrap;
}

/* 安装日志：随主题自适应的控制台风格面板 */
.inst-log {
    max-height: 130px;
    background: var(--color-fill-1);
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-default);
    padding: var(--base-radius-default) var(--base-radius-large);
}

.inst-log-line {
    font-family: Consolas, monospace;
    font-size: var(--base-size-small);
    color: var(--color-text-2);
    line-height: 1.6;
    white-space: pre-wrap;
    word-break: break-all;
}

/* 日志/状态弹窗 */
.ls-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
    gap: 8px;
    margin-bottom: var(--base-margin);
}

.ls-item {
    background: var(--color-fill-1);
    border-radius: var(--base-radius-default);
    padding: var(--base-radius-default) var(--base-radius-large);
    font-size: var(--base-size-small);
}

.ls-item span {
    color: var(--color-text-3);
    display: block;
}

.ls-item b {
    font-size: var(--base-size-small);
}

.ls-item b.ok {
    color: rgb(var(--green-6));
}

.ls-log {
    background: #1d2129;
    border-radius: var(--base-radius-default);
    padding: var(--base-padding) var(--base-radius-large);
    max-height: 320px;
    overflow-y: auto;
    font-family: Consolas, monospace;
    font-size: var(--base-size-small);
    color: #c9cdd4;
    line-height: 1.5;
}

.ls-line {
    white-space: pre-wrap;
    word-break: break-all;
}

.ls-actions {
    display: flex;
    gap: 8px;
    margin-top: var(--base-radius-large);
}

/* Docker 镜像行：下拉占满剩余宽度，右侧一个方形按钮打开 Docker 管理窗口
 * Arco 的竖排表单项内容区默认按内容宽度收缩（align-items: flex-start），空下拉会被挤成一个小方块，
 * 这里显式把内容区拉成整栏宽 */
.inst-form :deep(.img-item .arco-form-item-wrapper-col) {
    align-items: stretch;
}

.inst-form :deep(.img-item .arco-form-item-content) {
    width: 100%;
}

.img-row {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
}

.img-select {
    flex: 1;
    min-width: 0;
}

.img-open {
    flex: none;
    width: 32px;
    padding: 0;
}

/* 本地没有镜像时的提示：橙色一点，提醒可以点进去装 */
.img-none {
    color: rgb(var(--orange-6));
}

/* #help 里的“去安装”链接：跟提示文字同字号，不把行高撑开 */
.img-none :deep(.arco-link) {
    padding: 0;
    font-size: inherit;
    line-height: inherit;
}
</style>
