<template>
    <a-row class="redis-page" :gutter="6">
        <!-- 左侧固定 30%（连接上50% + 本地服务下50%），右侧主区域铺满剩余空间 -->
        <a-col :span="6" class="side-col">
            <a-row class="redis-pages">
                <a-col class="side-cols" :span="24">
                    <a-card class="side-card" size="small" :body-style="{
                        padding: 0
                    }">
                        <template #title>连接</template>
                        <template #extra>
                            <a-button size="small" title="新增连接" @click="openAddModal()">
                                新增连接
                            </a-button>
                        </template>
                        <a-list :bordered="false" size="small">
                            <a-list-item v-for="c in savedConns" :key="c.id" @click="openConnection(c)">
                                <a-space>
                                    <a-badge :status="isOpen(c.id) ? 'success' : 'normal'" />
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
                    <a-card class="side-card  " size="small" :body-style="{
                        padding: 0
                    }">
                        <template #title>本地服务</template>
                        <template #extra>
                            <div class="srv-head-actions">
                                <a-button size="small" title="新建实例" @click="openInstModal()">
                                    新建实例
                                </a-button>
                                <a-button size="small" title="版本管理" @click="openVersionsModal()">
                                    版本管理
                                </a-button>
                            </div>
                        </template>
                        <a-list :bordered="false" size="small">
                            <a-list-item v-for="inst in srvInstances" :key="inst.id">
                                <a-space>
                                    <a-badge :status="srvStates[inst.id]?.running ? 'success' : 'normal'" />
                                    <span class="conn-name" :title="inst.name + ':' + inst.port">{{ inst.name
                                        }}</span>
                                    <a-tag color="arcoblue">v{{ inst.version }}</a-tag>
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
            <a-card class="main-panel empty-card" v-if="openTabs.length === 0">
                <!-- 空状态 -->
                <a-empty description="可以从左边选择并打开连接">
                    <a-button type="primary" @click="openAddModal()">
                        <template #icon><icon-plus /></template>添加新连接
                    </a-button>
                </a-empty>
            </a-card>

            <!-- 已打开连接：用 a-card 承载，连接标签(openTabs)为 #title 的 a-radio-group，子标签为 #extra 的 a-radio-group -->
            <a-card v-else class="main-panel conn-card" :body-style="{ padding: 0 }">
                <template #title>
                    <a-radio-group v-model="st.activeTab" type="button" size="medium"
                        @change="(v: any) => switchTab(String(v))">
                        <a-radio v-for="t in openTabs" :key="t.id" :value="t.id">
                            <span class="tab-label" :title="t.name">{{ t.name }}</span>
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
                    <a-card v-show="st.activeSubTab === 'status'" class="status-card">
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
                        <a-card class="key-list-panel" :body-style="{ padding: 0 }">
                            <template #title>
                                <a-space>
                                    <a-input v-model="st.keyFilter" size="mini" placeholder="筛选键 (支持 * 通配)" allow-clear
                                        @press-enter="reloadKeys()">
                                        <template #prefix><icon-search /></template>
                                    </a-input>
                                    <a-button size="mini" @click="reloadKeys()"><template
                                            #icon><icon-refresh /></template></a-button>
                                    <a-button size="mini" @click="openAddKey()"><template
                                            #icon><icon-plus /></template></a-button>
                                    <a-button size="mini" @click="openBatchKey()">批量生成</a-button>
                                </a-space>
                            </template>
                            <div class="key-list">
                                <div v-for="k in st.keys" :key="k.key" class="key-item"
                                    :class="{ active: k.key === st.selectedKey }" @click="selectKey(k.key)">
                                    
                                    <a-tag color="pinkpurple" size="small">{{ (k.type[0] || '?').toUpperCase() }}</a-tag>
                                    <div class="key-name" :title="k.key">{{ k.key }}</div>
                                    <icon-delete class="key-del" @click.stop="delKey(k.key)" />
                                </div>
                                <a-empty v-if="st.keys.length === 0" :image="false" description="无键" />
                            </div>
                        </a-card>

                        <a-card class="key-detail-panel" :body-style="{ padding: 0 }">
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
                                    <a-button v-if="st.editing" size="small" type="primary" status="success"
                                        @click="saveKey()">
                                        <template #icon><icon-save /></template>保存
                                    </a-button>
                                    <a-button size="small" @click="copyValue()"><template
                                            #icon><icon-copy /></template>复制值</a-button>
                                    <a-button size="small" status="danger" @click="delKey(st.selectedKey)"><template
                                            #icon><icon-delete /></template></a-button>
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
                                <a-button v-if="!st.monitoring" @click="startMonitor()"><template
                                        #icon><icon-play-arrow /></template>开始监控</a-button>
                                <a-button v-else status="warning" @click="stopMonitor()"><template
                                        #icon><icon-pause /></template>停止</a-button>
                                <a-button @click="st.monitorLines = []"><template
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
            <a-form-item field="ttl" label="过期时间(秒, 0=不过期)"><a-input-number v-model="keyForm.ttl"
                    :min="0" /></a-form-item>
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
            <a-form-item field="ttl" label="过期时间(秒, 0=不过期)">
                <a-input-number v-model="batchForm.ttl" :min="0" />
            </a-form-item>
            <div style="color: var(--color-text-3); font-size: var(--base-size-small)">将生成：{{ batchForm.keyPrefix
                }}{{ batchForm.start }} ~ {{ batchForm.keyPrefix }}{{ batchForm.end }}（共 {{
                    Math.max(0, batchForm.end - batchForm.start + 1) }} 个），值为 前缀+序号</div>
        </a-form>
    </a-modal>

    <!-- 本地服务：版本管理 -->
    <a-modal v-model:visible="versionsVisible" title="Redis 版本管理" :width="680" :footer="false">
        <a-space direction="vertical" fill :size="12">
            <!-- 已安装版本 -->
            <a-card size="small" :body-style="{ padding: '8px 12px' }">
                <template #title><span class="ver-sec-title"><icon-storage /> 已安装版本</span></template>
                <div class="ver-scroll">
                    <div v-for="v in localVersions" :key="v.version + v.source" class="ver-item">
                        <a-tag size="small"
                            :color="v.source === 'bundled' ? 'green' : (v.source === 'downloaded' ? 'arcoblue' : 'orange')">{{
                                srcLabel(v.source) }}</a-tag>
                        <span class="ver-name">Redis {{ v.version }}</span>
                        <span class="ver-path" :title="v.path">{{ v.path }}</span>
                        <a-button size="mini" type="text" status="danger" :disabled="v.source === 'bundled'"
                            @click="removeVersion(v)">删除</a-button>
                    </div>
                    <a-empty v-if="localVersions.length === 0" :image="false" description="尚未安装任何版本，可从下方在线安装或添加本机目录" />
                </div>
                <div class="ver-actions">
                    <a-input v-model="localDirInput" size="small" placeholder="本机 redis-server.exe 所在目录" allow-clear />
                    <a-button size="small" @click="chooseLocalDir"><template
                            #icon><icon-folder /></template>选目录</a-button>
                    <a-button size="small" type="primary" @click="addLocalDir">添加</a-button>
                </div>
                <div class="ver-hint">离线安装：将 redis zip 放入 <span class="mono">{{ srvServersDir }}</span> 后点「安装」即可</div>
            </a-card>

            <!-- 在线版本 -->
            <a-card size="small" :body-style="{ padding: '8px 12px' }">
                <template #title>
                    <span class="ver-sec-title"><icon-cloud /> 在线版本（GitHub redis-windows 发行版，&gt;=7.0）</span>
                </template>
                <template #extra>
                    <a-button size="mini" type="text" :loading="remoteLoading" @click="loadRemoteVersions">
                        <template #icon><icon-refresh /></template>刷新
                    </a-button>
                </template>
                <div class="ver-scroll">
                    <div v-for="r in remoteVersions" :key="r.version" class="ver-item">
                        <span class="ver-name">Redis {{ r.version }}</span>
                        <span class="ver-path">{{ r.size ? (r.size / 1024 / 1024).toFixed(0) + ' MB' : '' }}</span>
                        <a-button size="mini" type="primary"
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
                        <a-button v-if="p.stage === 'error'" size="mini" status="danger"
                            @click="retryInstall(ver as string)">
                            <template #icon><icon-refresh /></template>重试
                        </a-button>
                    </div>
                    <div v-if="p.stage === 'error' && p.note" class="inst-err">{{ p.note }}</div>
                </div>
            </a-card>

            <!-- 安装日志 -->
            <a-card size="small" :body-style="{ padding: '8px 12px' }">
                <template #title><span class="ver-sec-title"><icon-history /> 安装日志</span></template>
                <template #extra>
                    <a-button size="mini" type="text" @click="installLogs = []"><template
                            #icon><icon-delete /></template>清空</a-button>
                </template>
                <div class="ver-scroll inst-log">
                    <div v-for="(l, i) in installLogs" :key="i" class="inst-log-line">{{ l }}</div>
                    <a-empty v-if="installLogs.length === 0" :image="false" description="暂无安装记录" />
                </div>
            </a-card>
        </a-space>
    </a-modal>

    <!-- 本地服务：实例编辑 -->
    <a-modal v-model:visible="instVisible" :title="instForm.id ? '编辑实例' : '新建实例'" :width="460" @ok="saveInstance">
        <a-form :model="instForm" layout="vertical">
            <a-form-item field="name" label="实例名称">
                <a-input v-model="instForm.name" placeholder="例如 local-dev" />
            </a-form-item>
            <a-form-item field="version" label="Redis 版本">
                <a-select v-model="instForm.version" placeholder="需已安装，可在「版本管理」获取">
                    <a-option v-for="v in localVersions" :key="v.version" :value="v.version">
                        {{ v.version }}（{{ srcLabel(v.source) }}）
                    </a-option>
                </a-select>
            </a-form-item>
            <a-row :gutter="12">
                <a-col :span="12">
                    <a-form-item field="port" label="端口">
                        <a-input-number v-model="instForm.port" :min="1" :max="65535" @blur="checkPortTaken" />
                    </a-form-item>
                </a-col>
                <a-col :span="12">
                    <a-form-item field="maxMemoryMB" label="最大内存MB(0=不限)">
                        <a-input-number v-model="instForm.maxMemoryMB" :min="0" />
                    </a-form-item>
                </a-col>
            </a-row>
            <a-form-item field="password" label="密码（留空=无 requirepass）">
                <a-input-password v-model="instForm.password" placeholder="留空则不设置" />
            </a-form-item>
            <a-form-item field="appendOnly" label="持久化 AOF">
                <a-switch v-model="instForm.appendOnly" />
            </a-form-item>
            <a-form-item field="dataDir" label="数据目录（留空=默认）">
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
            <div class="ls-item"><span>PID</span><b>{{ logsStatus.pid || '-' }}</b></div>
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
            <a-button size="mini" @click="logsLines = []"><template #icon><icon-delete /></template>清空</a-button>
            <a-button size="mini" :disabled="!logsStatus?.running" @click="connectInstance(logsInst)">
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

function delKey(key: string) {
    Modal.warning({
        title: "删除键",
        content: `确定删除「${key}」？`,
        hideCancel: false,
        onOk: async () => {
            await GoTools.RedisDeleteKeys(st.activeTab, [key]);
            if (st.selectedKey === key) {
                st.selectedKey = "";
                st.keyValue = { type: "", ttl: -1, str: "", hash: {}, list: [], set: [], zset: [] };
            }
            reloadKeys();
        },
    });
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
}

const srvInstances = ref<SrvInstanceConfig[]>([]);
const srvStates = reactive<Record<string, any>>({});
const localVersions = ref<any[]>([]);
const remoteVersions = ref<any[]>([]);
const remoteLoading = ref(false);
const installProgress = reactive<Record<string, any>>({});
const installLogs = ref<string[]>([]);
const versionsVisible = ref(false);
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
});

const installedSet = computed(() => new Set<string>(localVersions.value.map((v: any) => v.version)));

let offInstall: any = null;
const route = useRoute();
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
    return s === "bundled" ? "内置" : s === "downloaded" ? "下载" : "本机";
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
    for (const inst of srvInstances.value) {
        try {
            const s2 = await GoTools.RedisSrvStatus(inst.id);
            srvStates[inst.id] = s2;
            if (logsVisible.value && logsInst.value?.id === inst.id) logsStatus.value = s2;
        } catch (e) { /* ignore */ }
    }
}

async function openVersionsModal() {
    versionsVisible.value = true;
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

function removeVersion(v: any) {
    Modal.warning({
        title: "删除版本",
        content: `确定删除 Redis ${v.version}？若仍有运行中的实例引用该版本会被拒绝。`,
        hideCancel: false,
        onOk: async () => {
            try { await GoTools.RedisSrvRemoveVersion(v.version); Message.success("已删除"); loadLocalVersions(); }
            catch (e: any) { Message.error("删除失败: " + (e?.message || e)); }
        },
    });
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

function openInstModal(inst?: SrvInstanceConfig) {
    if (inst) { Object.assign(instForm, { ...inst }); }
    else { Object.assign(instForm, { id: "", name: "", version: localVersions.value[0]?.version || "", port: 6379, password: "", appendOnly: false, maxMemoryMB: 0, dataDir: "" }); }
    if (localVersions.value.length === 0) loadLocalVersions();
    instVisible.value = true;
}

async function checkPortTaken() {
    if (!instForm.port) return;
    try {
        const suggest = await GoTools.RedisSrvSuggestPort(instForm.port);
        if (suggest && suggest !== instForm.port) Message.warning(`端口 ${instForm.port} 已被占用，建议改为 ${suggest}`);
    } catch (e) { /* ignore */ }
}

async function saveInstance() {
    if (!instForm.name) { Message.warning("请填写实例名称"); return; }
    if (!instForm.version) { Message.warning("请选择 Redis 版本"); return; }
    try {
        await GoTools.RedisSrvSaveInstance({ ...instForm });
        instVisible.value = false; Message.success("已保存"); await loadSrvInstances();
    } catch (e: any) { Message.error("保存失败: " + (e?.message || e)); }
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
    background: var(--color-fill-1);

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

.inst-err {
    font-size: var(--base-size-small);
    color: rgb(var(--red-6));
    padding: var(--base-radius-default) 0 0 0;
    word-break: break-all;
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
</style>
