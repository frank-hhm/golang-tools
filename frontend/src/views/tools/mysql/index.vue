<template>
  <a-row class="mysql-page" :gutter="6">
    <a-col :span="5" class="side-col">
      <!-- 合并「外部连接」与「本地实例」为一个列表：点连接进库树工作区，点实例进右侧总览，用 a-tag 区分来源 -->
      <a-card class="side-card card-head-tool" size="small" :body-style="{ padding: 0 }">
        <template #title>连接</template>
        <template #extra>
          <div class="srv-head-actions">
            <a-button size="small" @click="openAddModal()">新增连接</a-button>
            <a-button size="small" @click="lspRef?.openInstModal()">新建实例</a-button>
            <a-button size="small" @click="lspRef?.openVersionsModal()">版本管理</a-button>
          </div>
        </template>
        <!-- 引擎没起时先把话说明白：否则要等点「启动」撞一条管道错误才知道是 Docker 没跑 -->
        <common-docker-off class="srv-docker-off" :count="dockerInstCount" tool-label="MySQL" />
        <div class="inst-list">
          <common-item-card v-for="it in mysqlList" :key="it.key" :active="itemActive(it)"
                      @click="clickMysqlItem(it)" @dblclick="dblMysqlItem(it)"
                      v-bind="mysqlReo.handlers(it.key)"
                      :class="{ 'reorder-dragging': mysqlReo.isDragging(it.key), 'reorder-over': mysqlReo.isOver(it.key) }">
            <template #status>
              <com-badge :status="it.live ? 'success' : 'normal'" :animated="it.live" />
            </template>
            <template #title>
              <a-tooltip class="inst-name-tip" :content="it.name + ' · ' + it.sub">
                <span class="inst-name">{{ it.name }}</span>
              </a-tooltip>
            </template>
            <template #extra>
              <a-dropdown trigger="hover" @select="(v: any) => onItemMenu(v, it)">
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
                  <!-- 删除收进下拉：下拉项内嵌不了气泡确认，改走确认框 -->
                  <a-doption value="del" status="danger"><icon-delete /> 删除</a-doption>
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
                    : `镜像 ${it.inst.image}：这就是该实例的 MySQL 版本`">
                  <a-tag :color="imgMissing(it.inst.image) ? 'orange' : 'purple'" size="small">Docker
                    {{ imgTagOf(it.inst.image) }}</a-tag>
                </a-tooltip>
                <a-tag v-else color="arcoblue" size="small">v{{ it.inst.version }}</a-tag>
              </template>
              <span v-else class="conn-sub">{{ it.sub }}</span>
            </div>
          </common-item-card>
          <div v-if="mysqlList.length === 0" class="conn-empty">暂无连接或实例</div>
        </div>
      </a-card>
      <!-- 实例逻辑与弹窗仍由子组件持有（弹窗规范）；列表渲染已上提到上面的合并卡 -->
      <LocalServicePanel ref="lspRef" @sync="onLspSync" @select="openSrvOverview" @connect="onSrvConnect" />
    </a-col>
    <a-col :span="19" class="main-col">
      <!-- 实例总览（对齐 PHP 主区域）：点左栏实例进来，状态栅格 + 实时错误日志 -->
      <a-card v-if="srvCfg" class="srv-panel card-head-tool" :body-style="{ padding: 0 }">
        <template #title>
          <a-space>
            <com-badge :status="srvState?.running ? 'success' : 'normal'" :animated="!!srvState?.running" />
            <span>{{ srvCfg.name }}</span>
            <a-tooltip v-if="srvCfg.mode === 'docker'" :content="srvCfg.image || ''">
              <a-tag color="purple" size="small">Docker {{ imgTagOf(srvCfg.image) }} · :{{ srvCfg.port }}</a-tag>
            </a-tooltip>
            <a-tag v-else color="arcoblue" size="small">mysqld :{{ srvCfg.port }}</a-tag>
          </a-space>
        </template>
        <template #extra>
          <a-space>
            <a-button size="small" :disabled="!!srvState?.running || srvStartBusy || srvStopBusy"
              :loading="srvStartBusy" @click="srvDoStart">启动</a-button>
            <a-popconfirm :content="`停止实例「${srvCfg.name}」？正在处理的请求会中断。`" type="warning" position="lt"
              :disabled="!srvState?.running || srvStopBusy" @ok="srvDoStop">
              <a-button size="small" status="danger"
                :disabled="!srvState?.running || srvStartBusy || srvStopBusy" :loading="srvStopBusy">
                {{ srvStopBusy ? '停止中' : '停止' }}</a-button>
            </a-popconfirm>
            <a-button size="small" :disabled="srvStartBusy || srvStopBusy || !srvState?.running"
              :loading="srvStartBusy" @click="srvDoRestart">{{ srvStartBusy ? '重启中' : '重启' }}</a-button>
            <a-button size="small" type="primary" :disabled="!srvState?.running" @click="srvConnect">
              <template #icon><icon-link /></template>连接此实例</a-button>
          </a-space>
        </template>
        <div class="ov-body">
          <!-- 关键指标：版式对齐 nginx（彩色圆角图标 + 标签/数值同排 + 下方明细） -->
          <a-row :gutter="[12, 12]" class="stat-row">
            <a-col v-for="c in srvStatCards" :key="c.key" :xs="24" :sm="12" :md="8">
              <common-stat-card :icon="c.icon" :tone="c.tone" :label="c.title" :value="c.value"
                :detail="c.sub" :tip="c.tip" :ready="c.ready" />
            </a-col>
          </a-row>

          <!-- 快捷操作：错误日志改到弹窗里看，面板只留高频入口 -->
          <div class="quick-bar">
            <a-button size="small" type="primary" :disabled="!srvState?.running" @click="srvConnect">
              <template #icon><icon-link /></template>连接此实例
            </a-button>
            <a-button size="small" @click="openSrvLog">
              <template #icon><icon-face-folder-open /></template>查看日志
            </a-button>
          </div>

          <!-- 详细信息：通用信息行（标签 | 值 | 操作），数据目录可一键打开 -->
          <div class="info-section">
            <div class="info-title">实例信息</div>
            <div class="info-list">
              <common-info-row grey v-if="srvCfg.mode === 'docker'" label="容器"
                :tip="'容器状态：' + (srvState?.containerState || '-')" :value="srvState?.container || '-'" />
              <common-info-row grey v-else label="PID" :value="String(srvState?.pid || '-')" />
              <common-info-row grey label="数据目录" :value="srvState?.dataDir || '-'" action-text="打开"
                @action="openPath(srvState?.dataDir)" />
              <common-info-row grey label="运行模式" :value="srvCfg.mode === 'docker' ? 'Docker 容器' : '本地 mysqld'" />
            </div>
          </div>
        </div>
      </a-card>
      <a-row v-else :gutter="6" class="main-row">
        <a-col :span="6" class="main-row-col" v-if="st.activeConnId">
          <a-card class="side-card tree-card card-head-tool" size="small" :body-style="{ padding: 0 }">
            <template #title>
              <a-space>
                <span>数据库</span>
                <icon-storage /> {{ activeConnName }}
              </a-space>
            </template>
            <template #extra>
              <a-button size="small" @click="refreshTree"><template
                  #icon><icon-refresh /></template></a-button>
            </template>
            <div class="tree-wrap">
              <a-tree :data="treeData" v-model:selectedKeys="st.selectedKeys" v-model:expanded-keys="st.expandedKeys"
                :load-more="loadMore" blockNode :show-line="true" @select="onTreeSelect">
                <template #title="node">
                  <a-tooltip :content="node?.title || ''" :disabled="!node?.title">
                    <span class="tree-title"
                      @contextmenu.prevent.stop="onNodeContextMenu($event, node)">{{ node?.title }}</span>
                  </a-tooltip>
                </template>
              </a-tree>
              <a-empty v-if="treeData.length === 0" :image="false" description="无数据库" />
            </div>
          </a-card>
        </a-col>
        <a-col :span="st.activeConnId ? 18 : 24" class="main-row-col">
          <!-- 右侧：主内容 -->
          <div class="main-panel">
            <div v-if="!st.activeConnId" class="empty-state">
              <a-empty description="请从左侧打开一个 MySQL 连接，或点选实例查看总览">
                <a-button size="small" @click="openAddModal()"><template #icon><icon-plus /></template>新建连接</a-button>
              </a-empty>
            </div>

            <template v-else>
              <a-card class="main-card card-head-tool">
                <template #title>
                  <a-space>
                    <icon-storage /> {{ activeConnName }}
                    <template v-if="st.activeDb"> / {{ st.activeDb }}</template>
                    <template v-if="st.table"> / {{ st.table }}</template>
                    <template v-if="st.view"> / {{ st.view }}</template>
                    <template v-if="st.routine"> / {{ st.routine }}</template>
                  </a-space>
                </template>
                <template #extra>
                  <!-- 右侧顶部工具栏：查询 / 备份 / 事务 / SQL日志 -->
                  <a-space>
                    <a-button size="small" @click="setMode('query')"><template
                        #icon><icon-code /></template>查询</a-button>
                    <a-button size="small" @click="setMode('backup')"><template
                        #icon><icon-save /></template>备份</a-button>
                    <a-button size="small" @click="setMode('tx')"><template
                        #icon><icon-select /></template>事务</a-button>
                    <a-tooltip content="SQL 执行日志：开关 / 目录 / 清空">
                      <a-button size="small" @click="openSqlLog()"><template
                          #icon><icon-file /></template>SQL日志</a-button>
                    </a-tooltip>
                  </a-space>
                </template>
                <!-- 表：数据 -->
                <div v-if="st.mode === 'table'" class="content">
                  <a-card class="table-card" size="small">
                    <template #title>
                      <a-radio-group size="medium" v-model="st.tableTab" type="button" @change="onTableTab">
                        <a-radio value="data"><icon-list /> 数据</a-radio>
                        <a-radio value="struct"><icon-storage /> 设计表</a-radio>
                        <a-radio value="ddl"><icon-code /> DDL</a-radio>
                      </a-radio-group>
                    </template>
                    <template #extra>
                      <a-space v-if="st.tableTab === 'data'">
                        <div v-if="!st.schema.pk" class="grid-warn">无单列主键，行编辑/删除已禁用</div>
                        <a-input v-model="st.data.filter" size="mini" placeholder="WHERE 条件（不含 WHERE）" allow-clear
                          style="width: 280px" @press-enter="loadData()">
                          <template #prefix><icon-filter /></template>
                        </a-input>
                        <a-button size="small" @click="loadData()"><template
                            #icon><icon-search /></template>筛选</a-button>
                        <a-button size="small" @click="clearFilter()"><template
                            #icon><icon-refresh /></template>重置</a-button>
                        <a-button size="small" @click="openInsert()"><template
                            #icon><icon-plus /></template>新增行</a-button>
                        <a-button size="small" @click="loadData()"><template
                            #icon><icon-download /></template>刷新</a-button>
                        <a-button size="small" status="danger"
                          :disabled="!st.schema.pk || st.data.selectedKeys.length === 0"
                          @click="askDeleteSelected()"><template #icon><icon-delete /></template>删除选中{{ st.data.selectedKeys.length ? `(${st.data.selectedKeys.length})` : '' }}</a-button>
                        <span class="grid-note">{{ st.data.note }}</span>
                      </a-space>
                      <a-space v-if="st.tableTab === 'struct'">
                        <a-button size="small" @click="addColumnModal()"><template
                            #icon><icon-plus /></template>增加列</a-button>
                        <a-button size="small" @click="createIndexModal()"><template
                            #icon><icon-plus /></template>新建索引</a-button>
                        <a-button size="small" @click="loadSchema()"><template
                            #icon><icon-refresh /></template>刷新</a-button>
                        <a-button size="small" status="danger" @click="askDropTable()"><template
                            #icon><icon-delete /></template>删除表</a-button>
                      </a-space>
                      <a-space v-if="st.tableTab === 'ddl'">
                        <a-button size="small" @click="copyText(st.schema.ddl)"><template
                            #icon><icon-copy /></template>复制</a-button>
                      </a-space>
                    </template>
                    <div v-show="st.tableTab === 'data'" class="tab-pane tab-pane-data">
                      <a-table class="data-table" :columns="dataCols" :data="st.data.rows" :pagination="tablePagination"
                        :scroll="{ x: 'max-content', y: '100%' }" :column-resizable="true" :scrollbar="false"
                        size="small" row-key="__rid" v-model:selectedKeys="st.data.selectedKeys"
                        :row-selection="{ type: 'checkbox', showCheckedAll: true, onlyCurrent: true }"
                        @page-change="goPage" @page-size-change="onPageSize" @sorter-change="onSorter">
                        <template #rowno="{ rowIndex }">{{ (st.data.page - 1) * st.data.size + rowIndex + 1
                        }}</template>
                        <template #cell="{ record, column, rowIndex }">
                          <template v-if="st.edit && st.edit.r === rowIndex && st.edit.c === column.dataIndex">
                            <a-select v-if="st.edit.kind === 'enum'" v-model="st.edit.val" size="mini"
                              class="cell-editor" :options="st.edit.options" :allow-clear="!!st.edit.meta.nullable"
                              @change="commitEdit(record, String(column.dataIndex))" @keyup.esc="st.edit = null" />
                            <a-date-picker v-else-if="st.edit.kind === 'datetime'" v-model="st.edit.val" size="mini"
                              show-time value-format="YYYY-MM-DD HH:mm:ss" class="cell-editor"
                              :allow-clear="!!st.edit.meta.nullable"
                              @change="commitEdit(record, String(column.dataIndex))" @keyup.esc="st.edit = null" />
                            <a-date-picker v-else-if="st.edit.kind === 'date'" v-model="st.edit.val" size="mini"
                              value-format="YYYY-MM-DD" class="cell-editor" :allow-clear="!!st.edit.meta.nullable"
                              @change="commitEdit(record, String(column.dataIndex))" @keyup.esc="st.edit = null" />
                            <a-time-picker v-else-if="st.edit.kind === 'time'" v-model="st.edit.val" size="mini"
                              format="HH:mm:ss" class="cell-editor" :allow-clear="!!st.edit.meta.nullable"
                              @change="commitEdit(record, String(column.dataIndex))" @keyup.esc="st.edit = null" />
                            <a-input v-else v-model="st.edit.val" size="mini" autofocus class="cell-editor"
                              :inputmode="st.edit.kind === 'number' ? 'decimal' : undefined"
                              @press-enter="commitEdit(record, String(column.dataIndex))" @keyup.esc="st.edit = null"
                              @blur="commitEdit(record, String(column.dataIndex))" />
                          </template>
                          <span v-else class="cell-txt" :class="{ nullv: record[column.dataIndex] === null }"
                            @dblclick="startEdit(rowIndex, String(column.dataIndex), record)">{{
                              record[column.dataIndex]
                                === null ? 'NULL' : record[column.dataIndex] }}</span>
                        </template>
                        <template #op="{ record }">
                          <a-tooltip :content="st.schema.pk ? '删除该行' : '无单列主键，删除已禁用'">
                            <a-button size="small" status="danger" :disabled="!st.schema.pk"
                              @click="askDeleteRow(record)"><icon-delete /></a-button>
                          </a-tooltip>
                        </template>
                        <template #empty><a-empty :image="false" description="无数据" /></template>
                        <template #pagination-left>
                          <span class="pg-total">共 {{ st.data.total }} 条</span>
                        </template>
                      </a-table>
                    </div>

                    <div v-show="st.tableTab === 'struct'" class="tab-pane tab-pane-scroll">
                      <div class="struct-title">列</div>
                      <a-table :columns="structColCols" :data="st.schema.columns" :pagination="false" size="small"
                        row-key="name" :scroll="{ x: '100%' }">
                        <template #colOp="{ record }">
                          <a-button size="small" @click="editColumnModal(record)"><icon-edit /></a-button>
                          <a-button size="small" status="danger" @click="askDropColumn(record)"><icon-delete /></a-button>
                        </template>
                      </a-table>
                      <div class="struct-title">索引</div>
                      <a-table :columns="structIdxCols" :data="st.schema.indexes" :pagination="false" size="small"
                        row-key="name" :scroll="{ x: '100%' }">
                        <template #idxOp="{ record }">
                          <a-button size="small" status="danger" :disabled="record.primary"
                            @click="askDropIndex(record)">删除</a-button>
                        </template>
                      </a-table>
                    </div>

                    <div v-show="st.tableTab === 'ddl'" class="tab-pane tab-pane-scroll">
                      <pre class="ddl-box">{{ st.schema.ddl }}</pre>
                    </div>
                  </a-card>
                </div>

                <!-- 视图 -->
                <div v-else-if="st.mode === 'view'" class="content">
                  <div class="grid-toolbar">
                    <span class="obj-title">视图：{{ st.view }}</span>
                    <a-button size="small" @click="loadViewData()"><template
                        #icon><icon-refresh /></template>刷新数据</a-button>
                    <a-button size="small" status="danger" @click="askDropView()"><template
                        #icon><icon-delete /></template>删除视图</a-button>
                  </div>
                  <a-table class="read-table" :columns="toReadCols(st.data.columns)" :data="st.data.rows"
                    :pagination="false" size="small" :scroll="{ x: 'max-content', y: '100%' }" :column-resizable="true"
                    :scrollbar="false" />
                </div>

                <!-- 函数 -->
                <div v-else-if="st.mode === 'routine'" class="content">
                  <div class="grid-toolbar">
                    <span class="obj-title">函数：{{ st.routine }}（{{ st.routineType }}）</span>
                    <a-button size="small" status="danger" @click="askDropRoutine()"><template
                        #icon><icon-delete /></template>删除</a-button>
                  </div>
                  <div class="struct-title">定义</div>
                  <pre class="ddl-box">{{ st.routineDef || '（无定义，可能是内置函数）' }}</pre>
                </div>

                <!-- 查询 -->
                <div v-else-if="st.mode === 'query'" class="content">
                  <div class="grid-toolbar">
                    <icon-thunderbolt class="tool-ic" />
                    <a-select v-model="st.query.connId" size="mini" :options="openedConnOptions" style="width: 170px"
                      placeholder="连接" @change="onPanelConnChange('query')" />
                    <a-select v-model="st.query.db" size="mini" :options="queryDbOptions" style="width: 170px"
                      placeholder="数据库" allow-search @change="onPanelDbChange('query')" />
                    <a-button size="small" @click="runQuery()"><template
                        #icon><icon-play-arrow /></template>运行
                      (Ctrl+Enter)</a-button>
                    <span class="grid-note">{{ st.query.note }}</span>
                  </div>
                  <a-textarea v-model="st.query.sql" :auto-size="{ minRows: 5, maxRows: 12 }"
                    placeholder="输入 SQL 语句，例如 SELECT * FROM xxx LIMIT 100;" @keydown="onQueryKey" />
                  <a-table class="read-table" :columns="toReadCols(st.query.result.columns)"
                    :data="st.query.result.rows" :pagination="false" size="small"
                    :scroll="{ x: 'max-content', y: '100%' }" :column-resizable="true" :scrollbar="false" />
                </div>

                <!-- 备份 -->
                <div v-else-if="st.mode === 'backup'" class="content">
                  <div class="grid-toolbar">
                    <icon-save class="tool-ic" />
                    <a-select v-model="st.backup.connId" size="mini" :options="openedConnOptions" style="width: 170px"
                      placeholder="连接" @change="onPanelConnChange('backup')" />
                    <a-select v-model="st.backup.db" size="mini" :options="backupDbOptions" style="width: 170px"
                      placeholder="数据库" allow-search @change="onPanelDbChange('backup')" />
                    <a-checkbox v-model="st.backup.all">全部表</a-checkbox>
                    <a-select v-if="!st.backup.all" v-model="st.backup.tables" :options="backupTableOptions" multiple
                      placeholder="选择表" size="mini" style="width: 300px" allow-clear />
                    <a-button size="small" :loading="st.backup.loading" @click="genBackup()"><template
                        #icon><icon-save /></template>生成备份</a-button>
                    <a-button size="small" :disabled="!st.backup.sql" @click="downloadBackup()"><template
                        #icon><icon-download /></template>下载 .sql</a-button>
                  </div>
                  <div class="struct-title">备份预览（{{ st.backup.sql.length }} 字符）</div>
                  <pre class="ddl-box">{{ st.backup.sql || '点击「生成备份」导出当前数据库的 SQL 转储。' }}</pre>
                </div>

                <!-- 事务 -->
                <div v-else-if="st.mode === 'tx'" class="content">
                  <div class="grid-toolbar">
                    <icon-select class="tool-ic" />
                    <a-select v-model="st.tx.connId" size="mini" :options="openedConnOptions" style="width: 170px"
                      placeholder="连接" :disabled="!!st.tx.id" @change="onPanelConnChange('tx')" />
                    <a-select v-model="st.tx.db" size="mini" :options="txDbOptions" style="width: 170px"
                      placeholder="数据库" :disabled="!!st.tx.id" allow-search @change="onPanelDbChange('tx')" />
                    <!-- 事务开着就是一件持续下去的事：圆点闪一下比一块色块更容易扫到 -->
                    <span v-if="st.tx.id" class="com-state is-on">
                      <com-badge status="success" :animated="true" />
                      事务进行中 {{ st.tx.id }}
                    </span>
                    <a-button size="small" :disabled="!!st.tx.id" @click="txBegin()"><template
                        #icon><icon-play-arrow /></template>开始事务</a-button>
                    <a-button size="small" status="success" :disabled="!st.tx.id" @click="txCommit()"><template
                        #icon><icon-check /></template>提交</a-button>
                    <a-button size="small" status="warning" :disabled="!st.tx.id" @click="txRollback()"><template
                        #icon><icon-close /></template>回滚</a-button>
                    <a-button size="small" :disabled="!st.tx.id" @click="txRun()"><template
                        #icon><icon-code /></template>执行语句</a-button>
                  </div>
                  <a-textarea v-model="st.tx.sql" :auto-size="{ minRows: 4, maxRows: 10 }" :disabled="!st.tx.id"
                    placeholder="在事务中执行 SQL（可多次），最后提交或回滚" />
                  <a-table class="read-table" :columns="toReadCols(st.tx.result.columns)" :data="st.tx.result.rows"
                    :pagination="false" size="small" :scroll="{ x: 'max-content', y: '100%' }" :column-resizable="true"
                    :scrollbar="false" />
                  <div class="struct-title">事务日志</div>
                  <pre class="ddl-box tx-log">{{ st.tx.log.join('\n') || '（暂无）' }}</pre>
                </div>

                <div v-else class="empty-state">
                  <a-empty description="选择左侧的表/视图/函数，或使用上方「查询 / 备份 / 事务」" />
                </div>
              </a-card>
            </template>
          </div>
        </a-col>
      </a-row>
    </a-col>

    <!-- 连接编辑 -->
    <a-modal v-model:visible="st.connModal" :title="st.connEditing ? '编辑连接' : '新建连接'" :width="480" @ok="saveConnection">
      <a-form :model="connForm" layout="vertical">
        <a-form-item field="name" label="连接名称"><a-input v-model="connForm.name"
            placeholder="例如 localhost_3306" /></a-form-item>
        <a-row :gutter="12">
          <a-col :span="16"><a-form-item field="host" label="主机"><a-input v-model="connForm.host"
                placeholder="127.0.0.1" /></a-form-item></a-col>
          <a-col :span="8"><a-form-item field="port" label="端口"><a-input-number v-model="connForm.port" :min="1"
                :max="65535" /></a-form-item></a-col>
        </a-row>
        <a-row :gutter="12">
          <a-col :span="12"><a-form-item field="user" label="用户名"><a-input v-model="connForm.user"
                placeholder="root" /></a-form-item></a-col>
          <a-col :span="12"><a-form-item field="password" label="密码"><a-input-password v-model="connForm.password"
                placeholder="密码" /></a-form-item></a-col>
        </a-row>
        <a-form-item field="database" label="默认数据库（可留空）"><a-input v-model="connForm.database"
            placeholder="留空则在左侧选择" /></a-form-item>
        <a-form-item label="保存位置">
          <a-radio-group v-model="st.saveTarget" type="button" size="medium">
            <a-radio value="file"><icon-save /> 文件</a-radio>
            <a-radio value="cache"><icon-storage /> 缓存</a-radio>
          </a-radio-group>
        </a-form-item>
      </a-form>
      <template #footer>
        <div class="conn-test-footer">
          <div class="conn-test-left">
            <a-button size="small" :loading="connTesting" @click="doTestConnection">测试连接</a-button>
            <span v-if="connTestResult" :class="['conn-test-msg', connTestStatus === 'ok' ? 'is-ok' : 'is-err']">{{
              connTestResult }}</span>
          </div>
          <a-space>
            <a-button @click="st.connModal = false">取消</a-button>
            <a-button type="primary" @click="saveConnection">保存</a-button>
          </a-space>
        </div>
      </template>
    </a-modal>

    <!-- 新增行 -->
    <a-modal v-model:visible="st.insertModal" title="新增行" :width="520" @ok="doInsert">
      <a-form layout="vertical" :model="insertData">
        <a-form-item v-for="col in insertCols" :key="col.name" :label="col.name + ' (' + col.type + ')'">
          <div style="display:flex; gap:8px; align-items:center">
            <a-select v-if="isEnumCol(col)" v-model="insertData[col.name]" :disabled="insertNull[col.name]"
              :options="enumOptionsOf(col)" placeholder="选择枚举值" allow-clear style="flex:1" />
            <a-input v-else v-model="insertData[col.name]" :disabled="insertNull[col.name]"
              :placeholder="col.comment || col.extra || ''" style="flex:1" />
            <a-checkbox v-model="insertNull[col.name]">NULL</a-checkbox>
          </div>
        </a-form-item>
      </a-form>
    </a-modal>

    <!-- 新建索引 -->
    <a-modal v-model:visible="st.indexModal" title="新建索引" :width="460" @ok="doCreateIndex">
      <a-form layout="vertical" :model="indexForm">
        <a-form-item label="索引名称"><a-input v-model="indexForm.name" placeholder="idx_xxx" /></a-form-item>
        <a-form-item label="列（逗号分隔）"><a-input v-model="indexForm.columns" placeholder="col1, col2" /></a-form-item>
        <a-form-item><a-checkbox v-model="indexForm.unique">唯一索引</a-checkbox></a-form-item>
      </a-form>
    </a-modal>

    <!-- 新增/编辑列 -->
    <a-modal v-model:visible="st.colModal" :title="st.colEditing ? '编辑列：' + st.colOldName : '增加列'" :width="540"
      @ok="saveColumn">
      <a-form layout="vertical" :model="colForm">
        <a-row :gutter="12">
          <a-col :span="10"><a-form-item label="列名"><a-input v-model="colForm.name"
                placeholder="column_name" /></a-form-item></a-col>
          <a-col :span="14">
            <a-form-item label="类型">
              <div class="type-row">
                <a-select v-model="colTypeBase" :options="colTypeOptions" allow-search allow-create size="small"
                  class="type-sel" @change="onColTypeChange" />
                <a-input v-model="colTypeLimit" size="small" class="type-limit" :disabled="typeNoLimit"
                  :placeholder="typeLimitPlaceholder" />
              </div>
              <template #extra>{{ typeHint }}</template>
            </a-form-item>
          </a-col>
        </a-row>
        <a-form-item label="默认值（留空=无；可填 NULL / CURRENT_TIMESTAMP）"><a-input v-model="colForm.default" /></a-form-item>
        <a-form-item label="注释"><a-input v-model="colForm.comment" /></a-form-item>
        <a-form-item label="位置（置于某列之后，默认末尾）"><a-select v-model="colForm.after" :options="afterOptions" allow-clear
            placeholder="默认末尾" /></a-form-item>
        <a-space :size="20">
          <a-checkbox v-model="colForm.nullable">允许空 (NULL)</a-checkbox>
          <a-checkbox v-model="colForm.autoIncrement">自增</a-checkbox>
          <a-checkbox v-model="colForm.primaryKey">主键</a-checkbox>
        </a-space>
      </a-form>
    </a-modal>

    <!-- 表/视图重命名 -->
    <a-modal v-model:visible="st.renameModal" :title="'重命名' + (st.renameTarget?.kind === 'view' ? '视图' : '表')"
      :width="420" @ok="doRename">
      <a-form layout="vertical" :model="st">
        <a-form-item :label="'原名称：' + (st.renameTarget?.name || '')">
          <a-input v-model="st.renameName" placeholder="新名称" @press-enter="doRename" />
        </a-form-item>
      </a-form>
    </a-modal>

    <!-- 复制表 -->
    <a-modal v-model:visible="st.copyModal" title="复制表" :width="440" @ok="doCopyTable">
      <a-form layout="vertical" :model="st">
        <a-form-item label="新表名"><a-input v-model="st.copyName" placeholder="新表名"
            @press-enter="doCopyTable" /></a-form-item>
        <a-form-item>
          <a-checkbox v-model="st.copyWithData">同时复制数据</a-checkbox>
        </a-form-item>
        <div class="modal-hint">源表：{{ st.copyTarget?.db }} / {{ st.copyTarget?.name }}（表结构与索引一并复制）</div>
      </a-form>
    </a-modal>

    <!-- 新建表 -->
    <a-modal v-model:visible="st.newTableModal" title="新建表 (DDL)" :width="560" @ok="doCreateTable">
      <a-textarea v-model="st.newTableDDL" :auto-size="{ minRows: 8 }"
        placeholder="CREATE TABLE my_table (&#10;  id INT AUTO_INCREMENT PRIMARY KEY,&#10;  name VARCHAR(64),&#10;  created DATETIME&#10;) ENGINE=InnoDB;" />
    </a-modal>

    <!-- SQL 执行日志（MySQL 工具级设置，交互与样式对齐设置页） -->
    <a-modal v-model:visible="st.sqlLog.visible" title="SQL 执行日志" :width="680" :footer="false" unmount-on-close>
      <div class="sql-log-pane">
        <a-form :model="st.sqlLog" layout="vertical" :style="{ maxWidth: '760px' }">
          <a-form-item field="enabled">
            <template #label>记录 SQL 执行日志</template>
            <a-switch type="round" v-model="st.sqlLog.enabled" @change="saveSqlLog()" />
            <span class="hint">关闭后不再写入 SQL 日志文件；开启后记录手动执行的 SQL 与增删改/DDL，数据浏览类自动查询不记录。</span>
          </a-form-item>

          <a-form-item field="dir">
            <template #label>日志目录</template>
            <div class="path-row">
              <a-input v-model="st.sqlLog.dir"
                :placeholder="`留空使用默认目录（${st.sqlLog.defaultDir || '程序目录下 logs'}）`" allow-clear
                @blur="saveSqlLog()" @press-enter="saveSqlLog()" @clear="saveSqlLog()" />
              <a-button size="small" @click="chooseSqlLogDir()">选择目录</a-button>
              <a-button size="small" @click="resetSqlLogDir()">恢复默认</a-button>
            </div>
            <div class="hint">日志按日期自动分文件，如 mysql-sql-YYYYMMDD.log。当前目录：{{ st.sqlLog.resolvedDir || '（未加载）' }}</div>
          </a-form-item>
        </a-form>

        <div class="log-head">
          <span class="struct-title">日志文件（{{ st.sqlLog.files.length }} 个，共 {{ fmtSize(st.sqlLog.totalSize) }}）</span>
          <span class="auto-save-tip">{{ st.sqlLog.saving ? '正在保存…' : '修改后自动保存' }}</span>
          <a-button size="small" @click="openSqlLogDir()">打开日志目录</a-button>
          <a-button size="small" @click="refreshSqlLog()">刷新</a-button>
          <a-button size="small" status="danger" :disabled="!st.sqlLog.files.length" @click="askClearSqlLog()">清空日志</a-button>
        </div>
        <a-table :columns="sqlLogCols" :data="st.sqlLog.files" :pagination="false" size="small" row-key="name"
          :scroll="{ y: 240 }">
          <template #empty><a-empty :image="false" description="暂无日志文件" /></template>
        </a-table>
      </div>
    </a-modal>

    <!-- 危险操作二次确认（清空/删除/批量删除/危险 SQL） -->
    <a-modal v-model:visible="st.danger.visible" :title="st.danger.title" :width="540" :ok-text="st.danger.okText"
      :ok-button-props="{ status: 'danger' }" :mask-closable="false" :esc-to-close="false" @ok="confirmDanger"
      @cancel="cancelDanger">
      <div class="danger-body">
        <div class="danger-desc">{{ st.danger.desc }}</div>
        <pre v-if="st.danger.sql" class="ddl-box danger-sql">{{ st.danger.sql }}</pre>
        <div v-if="st.danger.keyword" class="danger-kw">
          <div>请输入 <b>{{ st.danger.keyword }}</b> 以确认：</div>
          <a-input v-model="st.danger.input" placeholder="输入确认词" @press-enter="confirmDanger" />
        </div>
      </div>
    </a-modal>

    <!-- 对象树右键菜单 -->
    <div v-if="ctx.visible" class="ctx-mask" @click="ctx.visible = false" @contextmenu.prevent="ctx.visible = false" />
    <a-menu v-if="ctx.visible && ctx.node" class="ctx-menu" :style="{ left: ctx.x + 'px', top: ctx.y + 'px' }"
      @menu-item-click="onCtxMenuClick">
      <template v-if="ctx.node.kind === 'table'">
        <a-menu-item key="open">打开表</a-menu-item>
        <a-menu-item key="design">设计表</a-menu-item>
        <a-menu-item key="new">新建表</a-menu-item>
        <a-sub-menu key="copy">
          <template #title>复制表</template>
          <a-menu-item key="copy-data">结构和数据</a-menu-item>
          <a-menu-item key="copy-struct">仅结构</a-menu-item>
        </a-sub-menu>
        <a-menu-item key="rename">重命名</a-menu-item>
        <a-menu-item key="clear">清空表</a-menu-item>
        <a-menu-item key="truncate">截断表</a-menu-item>
        <a-menu-item key="drop" class="ctx-del">删除表</a-menu-item>
        <a-menu-item key="refresh">刷新</a-menu-item>
      </template>
      <template v-else-if="ctx.node.kind === 'view'">
        <a-menu-item key="open">打开视图</a-menu-item>
        <a-menu-item key="rename">重命名</a-menu-item>
        <a-menu-item key="drop" class="ctx-del">删除视图</a-menu-item>
        <a-menu-item key="refresh">刷新</a-menu-item>
      </template>
      <template v-else-if="ctx.node.kind === 'func'">
        <a-menu-item key="open">查看定义</a-menu-item>
        <a-menu-item key="drop" class="ctx-del">删除{{ ctx.node.rtype === 'PROCEDURE' ? '存储过程' : '函数' }}</a-menu-item>
        <a-menu-item key="refresh">刷新</a-menu-item>
      </template>
      <template v-else>
        <a-menu-item v-if="ctx.node.kind === 'db' || ctx.node.gkind === 't' || ctx.node.gkind === 'empty'"
          key="new">新建表</a-menu-item>
        <a-menu-item key="refresh">刷新</a-menu-item>
      </template>
    </a-menu>

    <!-- 错误日志（实时）：点「查看日志」弹出，打开才轮询（轮询由子组件自管） -->
    <LogModal v-model="srvLogVisible" :instance-id="srvCfg?.id || ''" :instance-name="srvCfg?.name || ''"
      :port="srvCfg?.port || 0" :running="!!srvState?.running" :is-docker="srvCfg?.mode === 'docker'" />
  </a-row>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, h, nextTick, onMounted, onUnmounted, reactive, ref } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import LocalServicePanel from "./LocalServicePanel.vue";
import LogModal from "./components/LogModal.vue";
import { sysNotify } from "@/utils/notify";
import { useReorder, applyOrder, loadOrder } from "@/utils/reorder";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const CACHE_KEY = "mysql_connections_cache";

/* 只读结果表格列：视图/查询/事务复用，null 以 NULL 斜体显示 */
function toReadCols(columns: string[]) {
  return (columns || []).map((c) => ({
    title: c, dataIndex: c, ellipsis: true, tooltip: true, width: 160,
    render: ({ record }: any) => h("span", { class: record[c] === null ? "nullv" : undefined }, record[c] === null ? "NULL" : String(record[c])),
  }));
}

interface ConnConfig { id: string; name: string; host: string; port: number; user: string; password: string; database: string }

/** 与子组件 LocalServicePanel 同构：合并列表渲染需要用到实例字段 */
interface SrvInstanceConfig {
  id: string; name: string; version: string; port: number;
  rootPassword: string; maxConnections: number;
  mode: string; image: string; containerName: string
}

/** 左栏统一列表项：外部连接与本地实例合并成一个列表 */
type MysqlListItem =
  | { kind: "instance"; key: string; name: string; sub: string; live: boolean; inst: SrvInstanceConfig }
  | { kind: "external"; key: string; name: string; sub: string; live: boolean; conn: ConnConfig };

/** 双击单元格的编辑态：按列类型选择控件 */
interface CellEdit {
  r: number;
  c: string;
  val: any;
  kind: "text" | "enum" | "number" | "date" | "datetime" | "time";
  options: { label: string; value: string }[];
  meta: any;
}

/** 右键菜单目标节点 */
interface CtxTarget { key: string; kind: string; gkind: string; db: string; name: string; rtype?: string }

const savedConns = ref<ConnConfig[]>([]);
const openedIds = ref<string[]>([]);

const emptyResult = () => ({ columns: [] as string[], rows: [] as any[], affected: 0, lastID: 0, elapsed: 0, note: "" });

const st = reactive({
  activeConnId: "",
  activeDb: "",
  databases: [] as string[],
  tables: [] as any[],
  views: [] as any[],
  routines: [] as any[],
  selectedKeys: [] as string[],
  expandedKeys: [] as string[],
  mode: "none", // none|table|view|routine|query|backup|tx
  tableTab: "data",
  table: "",
  view: "",
  routine: "",
  routineType: "",
  routineDef: "",
  schema: { columns: [], indexes: [], ddl: "", pk: "" } as any,
  data: { columns: [], rows: [], total: 0, page: 1, size: 50, filter: "", orderBy: "", desc: false, note: "", selectedKeys: [] as string[] } as any,
  edit: null as null | CellEdit,
  query: { connId: "", db: "", databases: [] as string[], sql: "", result: emptyResult(), note: "" } as any,
  tx: { connId: "", db: "", databases: [] as string[], id: "", sql: "", result: emptyResult(), log: [] as string[] } as any,
  backup: { connId: "", db: "", databases: [] as string[], tableList: [] as any[], all: true, tables: [] as string[], sql: "", loading: false } as any,
  // 弹窗
  connModal: false,
  connEditing: false,
  saveTarget: "file",
  insertModal: false,
  indexModal: false,
  newTableModal: false,
  newTableDDL: "",
  colModal: false,
  colEditing: false,
  colOldName: "",
  renameModal: false,
  renameTarget: null as CtxTarget | null,
  renameName: "",
  copyModal: false,
  copyTarget: null as CtxTarget | null,
  copyName: "",
  copyWithData: true,
  // SQL 执行日志（工具级设置）
  sqlLog: { visible: false, enabled: false, dir: "", defaultDir: "", resolvedDir: "", files: [] as any[], totalSize: 0, saving: false } as any,
  // 危险操作二次确认（输入确认词后才执行）
  danger: { visible: false, title: "", desc: "", sql: "", keyword: "", input: "", okText: "确认执行", pending: null as null | (() => any) } as any,
});

const connForm = reactive<ConnConfig>({ id: "", name: "", host: "127.0.0.1", port: 3306, user: "root", password: "", database: "" });
const insertData = reactive<Record<string, string>>({});
const insertNull = reactive<Record<string, boolean>>({});
const indexForm = reactive({ name: "", columns: "", unique: false });
const colForm = reactive({ name: "", type: "varchar(255)", nullable: true, default: "", autoIncrement: false, primaryKey: false, comment: "", after: "" });

/* 列类型：下拉选基础类型，limit 单独输入；enum/set 的 limit 为枚举值 */
const COL_TYPES = [
  "tinyint", "tinyint unsigned", "smallint", "smallint unsigned", "mediumint", "mediumint unsigned",
  "int", "int unsigned", "bigint", "bigint unsigned",
  "decimal", "numeric", "float", "double", "bit", "bool",
  "char", "varchar", "tinytext", "text", "mediumtext", "longtext",
  "binary", "varbinary", "tinyblob", "blob", "mediumblob", "longblob",
  "date", "datetime", "timestamp", "time", "year", "enum", "set", "json",
];
const NO_LIMIT_TYPES = new Set(["date", "datetime", "timestamp", "time", "json"]);
const colTypeBase = ref("varchar");
const colTypeLimit = ref("255");
const colTypeOptions = computed(() => {
  const list = [...COL_TYPES];
  const cur = colTypeBase.value.trim().toLowerCase();
  if (cur && !list.includes(cur)) list.unshift(cur); // 编辑时保留库里的原始类型
  return list.map((t) => ({ label: t, value: t }));
});
const typeNoLimit = computed(() => NO_LIMIT_TYPES.has(baseTypeName(colTypeBase.value)));
const typeIsEnum = computed(() => /^(enum|set)$/i.test(baseTypeName(colTypeBase.value)));
const typeLimitPlaceholder = computed(() => {
  if (typeNoLimit.value) return "无需长度";
  if (typeIsEnum.value) return "值1,值2（英文逗号）";
  return "如 255 或 10,2";
});
const typeHint = computed(() => {
  if (typeNoLimit.value) return "该类型无需长度/精度";
  if (typeIsEnum.value) return "枚举/集合值用英文逗号分隔，例如：small,medium,large";
  return "生成类型示例：varchar(255)、decimal(10,2)";
});

/** 解析 enum('a','b') 括号内的可选值 */
function parseEnumValues(args: string): string[] {
  const out: string[] = [];
  const re = /'((?:[^']|'')*)'/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(args || ""))) out.push(m[1].replace(/''/g, "'"));
  return out;
}

/** 从完整类型串还原「基础类型 + limit」，如 decimal(10,2) -> decimal / 10,2 */
function parseColTypeDef(t: string) {
  const s = String(t || "").trim();
  const m = /^([a-zA-Z]+)\s*(?:\(([\s\S]*)\))?\s*(.*)$/.exec(s);
  if (!m) return { base: s, limit: "" };
  let base = (m[1] || "").toLowerCase();
  const flags = (m[3] || "").trim().toLowerCase();
  if (flags) base += " " + flags; // unsigned / zerofill
  let limit = (m[2] || "").trim();
  if (/^(enum|set)$/i.test(m[1] || "")) limit = parseEnumValues(limit).join(",");
  else limit = limit.replace(/\s+/g, "");
  return { base: base || "varchar", limit };
}

/** 取基础类型名（去掉 unsigned/zerofill 等修饰） */
function baseTypeName(t: string): string {
  return (String(t || "").trim().toLowerCase().split(/\s+/)[0]) || "";
}

/** 切换类型时重置 limit 为常用值 */
const TYPE_DEFAULT_LIMIT: Record<string, string> = {
  decimal: "10,2", numeric: "10,2",
  varchar: "255", varbinary: "255", char: "64", binary: "64",
  bit: "1", enum: "a,b", set: "a,b",
};
function onColTypeChange(v: any) {
  const base = baseTypeName(String(v ?? ""));
  if (NO_LIMIT_TYPES.has(base)) { colTypeLimit.value = ""; return; }
  colTypeLimit.value = TYPE_DEFAULT_LIMIT[base] ?? "";
}

/** 根据下拉与 limit 组装最终类型串；非法返回空串（修饰符放括号之后，如 int(11) unsigned） */
function buildColType(): string {
  const raw = colTypeBase.value.trim().toLowerCase();
  if (!raw) return "";
  const m = /^([a-z]+)\s*(.*)$/.exec(raw);
  const name = m?.[1] || raw;
  const flags = (m?.[2] || "").trim();
  const tail = flags ? " " + flags : "";
  if (NO_LIMIT_TYPES.has(name)) return name + tail;
  const lim = colTypeLimit.value.trim();
  if (!lim) return name + tail;
  if (/^(enum|set)$/.test(name)) {
    const vals = lim.split(",").map((s) => s.trim().replace(/^'(.*)'$/, "$1")).filter((s) => s !== "");
    if (!vals.length) return "";
    return name + "(" + vals.map((v) => "'" + v.replace(/'/g, "''") + "'").join(",") + ")" + tail;
  }
  if (!/^\d+(\s*,\s*\d+)?$/.test(lim)) return "";
  return name + "(" + lim.replace(/\s+/g, "") + ")" + tail;
}

/** 列默认值校验（NULL / CURRENT_TIMESTAMP 放行） */
function validateColDefault(type: string, raw: string): string {
  const v = String(raw || "").trim();
  if (!v || /^null$/i.test(v)) return "";
  const t = type.toLowerCase();
  if (/^current_timestamp(\(\d*\))?$/i.test(v)) return "";
  if (isNumericColumnType(t)) return /^-?\d+(\.\d+)?$/.test(v) ? "" : "数值类型的默认值必须是数字";
  if (/^(date|datetime|timestamp)\b/.test(t)) return Number.isNaN(Date.parse(v.replace(" ", "T"))) ? "默认值不是有效日期" : "";
  return "";
}

const activeConnName = computed(() => savedConns.value.find((c) => c.id === st.activeConnId)?.name || "");
const isOpen = (id: string) => openedIds.value.includes(id);
const openedConnOptions = computed(() =>
  savedConns.value.filter((c) => openedIds.value.includes(c.id)).map((c) => ({ label: c.name || `${c.host}:${c.port}`, value: c.id })));
const queryDbOptions = computed(() => (st.query.databases || []).map((d: string) => ({ label: d, value: d })));
const backupDbOptions = computed(() => (st.backup.databases || []).map((d: string) => ({ label: d, value: d })));
const txDbOptions = computed(() => (st.tx.databases || []).map((d: string) => ({ label: d, value: d })));
const backupTableOptions = computed(() => (st.backup.tableList || []).map((t: any) => ({ label: t.name, value: t.name })));
const afterOptions = computed(() => (st.schema.columns || []).map((c: any) => ({ label: c.name, value: c.name })));
const insertCols = computed<any[]>(() => (st.schema.columns || []).filter((c: any) => !(c.extra || "").toLowerCase().includes("auto_increment")));

/* 插入表单：枚举列用下拉选择 */
function isEnumCol(col: any): boolean {
  return /^enum\(/i.test(String(col?.type || ""));
}
function enumOptionsOf(col: any): { label: string; value: string }[] {
  const m = /^enum\((.*)\)/i.exec(String(col?.type || ""));
  return parseEnumValues(m?.[1] || "").map((v) => ({ label: v, value: v }));
}
const pageSizeNums = [25, 50, 100, 200, 500];

/* 数据表格列：行号 + 动态列（服务端排序）+ 操作；双击单元格编辑 */
const dataCols = computed<any[]>(() => {
  const cols: any[] = [
    { title: "#", dataIndex: "__rowno", slotName: "rowno", width: 64, align: "center", resizable: false },
  ];
  for (const c of st.data.columns) {
    cols.push({
      title: c, dataIndex: c, slotName: "cell", ellipsis: true, tooltip: true, width: 160,
      sortable: { sortDirections: ["ascend", "descend"], sortOrder: st.data.orderBy === c ? (st.data.desc ? "descend" : "ascend") : undefined },
    });
  }
  cols.push({ title: "操作", dataIndex: "__op", slotName: "op", width: 64, align: "center", resizable: false, fixed: "right" });
  return cols;
});

const tablePagination = computed(() => ({
  current: st.data.page, pageSize: st.data.size, total: st.data.total,
  pageSizeOptions: pageSizeNums, showTotal: false, simple: true,
}));

/* 设计表：列 / 索引 */
const structColCols = computed<any[]>(() => [
  { title: "#", dataIndex: "position", width: 60 },
  { title: "列名", dataIndex: "name", ellipsis: true, tooltip: true },
  { title: "类型", dataIndex: "type", ellipsis: true, tooltip: true },
  { title: "空", dataIndex: "nullable", width: 70, render: ({ record }: any) => h("span", record.nullable ? "是" : "否") },
  { title: "键", dataIndex: "key", width: 80 },
  { title: "默认", dataIndex: "default", ellipsis: true, tooltip: true },
  { title: "额外", dataIndex: "extra", ellipsis: true, tooltip: true },
  { title: "注释", dataIndex: "comment", ellipsis: true, tooltip: true },
  { title: "操作", dataIndex: "__op", slotName: "colOp", width: 90, align: "center", fixed: "right" },
]);
const structIdxCols = computed<any[]>(() => [
  { title: "名称", dataIndex: "name", ellipsis: true, tooltip: true },
  { title: "唯一", dataIndex: "unique", width: 70, render: ({ record }: any) => h("span", record.unique ? "是" : "否") },
  { title: "主键", dataIndex: "primary", width: 70, render: ({ record }: any) => h("span", record.primary ? "是" : "否") },
  { title: "类型", dataIndex: "type", width: 120 },
  { title: "列", dataIndex: "columns", ellipsis: true, tooltip: true, render: ({ record }: any) => h("span", (record.columns || []).join(", ")) },
  { title: "操作", dataIndex: "__op", slotName: "idxOp", width: 90, align: "center", fixed: "right" },
]);

const treeData = ref<any[]>([]);
/** 树节点 key → 元信息，避免解析标识符（库/表名可能含特殊字符） */
const nodeMap = new Map<string, { kind: "db" | "table" | "view" | "func"; db: string; name: string; rtype?: string }>();

function buildGroups(db: string, t: any[], v: any[], r: any[]) {
  const groups: any[] = [];
  if (t.length) groups.push({ key: `grp:${db}:t`, title: `表 (${t.length})`, isLeaf: false, children: t.map((x) => { const k = `table:${db}:${x.name}`; nodeMap.set(k, { kind: "table", db, name: x.name }); return { key: k, title: x.name, isLeaf: true }; }) });
  if (v.length) groups.push({ key: `grp:${db}:v`, title: `视图 (${v.length})`, isLeaf: false, children: v.map((x) => { const k = `view:${db}:${x.name}`; nodeMap.set(k, { kind: "view", db, name: x.name }); return { key: k, title: x.name, isLeaf: true }; }) });
  if (r.length) groups.push({ key: `grp:${db}:r`, title: `函数 (${r.length})`, isLeaf: false, children: r.map((x) => { const k = `func:${db}:${x.name}`; nodeMap.set(k, { kind: "func", db, name: x.name, rtype: x.type }); return { key: k, title: x.name, isLeaf: true }; }) });
  if (groups.length === 0) groups.push({ key: `grp:${db}:empty`, title: "（空）", isLeaf: true, selectable: false });
  return groups;
}

/* ============================ 连接 ============================ */

async function loadConnections() {
  let list: ConnConfig[] = [];
  try { list = (await GoTools.MySQLLoadConnections()) || []; } catch (e) { list = []; }
  if (!list || list.length === 0) {
    try { const c = localStorage.getItem(CACHE_KEY); if (c) list = JSON.parse(c) || []; } catch (e) { list = []; }
  }
  savedConns.value = (list || []).map((x) => ({ ...x }));
}

async function persistConnections() {
  const data = JSON.parse(JSON.stringify(savedConns.value));
  localStorage.setItem(CACHE_KEY, JSON.stringify(data));
  if (st.saveTarget === "file") {
    try { await GoTools.MySQLSaveConnections(data); } catch (e) { /* ignore */ }
  }
}

function openAddModal() {
  st.connEditing = false;
  Object.assign(connForm, { id: "", name: "", host: "127.0.0.1", port: 3306, user: "root", password: "", database: "" });
  resetConnTest();
  st.connModal = true;
}
function openEditModal(c: ConnConfig) {
  st.connEditing = true;
  Object.assign(connForm, { ...c });
  resetConnTest();
  st.connModal = true;
}
async function saveConnection() {
  if (!connForm.host) { Message.warning("请填写主机"); return; }
  const item: ConnConfig = { ...connForm };
  if (!item.name) item.name = `${item.host}:${item.port}`;
  if (st.connEditing) {
    const i = savedConns.value.findIndex((c) => c.id === item.id);
    if (i >= 0) savedConns.value[i] = item;
  } else {
    item.id = "m" + Date.now().toString(36);
    savedConns.value.push(item);
  }
  await persistConnections();
  st.connModal = false;
  Message.success("已保存到" + (st.saveTarget === "file" ? "文件" : "缓存"));
}
/* 测试连接：按当前表单参数临时建连 Ping 后立刻断开，结果就地提示；用临时 id 避免碰到同名已登记连接 */
const connTesting = ref(false);
const connTestResult = ref("");
const connTestStatus = ref<"ok" | "err" | "">("");
function resetConnTest() {
  connTesting.value = false;
  connTestResult.value = "";
  connTestStatus.value = "";
}
async function doTestConnection() {
  if (!connForm.host) { Message.warning("请先填写主机"); return; }
  connTesting.value = true;
  connTestResult.value = "";
  connTestStatus.value = "";
  try {
    const ms = await GoTools.MySQLTest({ ...connForm, id: "conn-test" });
    connTestStatus.value = "ok";
    connTestResult.value = `连接成功（${ms} ms）`;
  } catch (e: any) {
    connTestStatus.value = "err";
    connTestResult.value = "连接失败：" + String(e?.message || e);
  } finally {
    connTesting.value = false;
  }
}
function onConnAction(v: string, c: ConnConfig) {
  if (v === "open") openConnection(c);
  else if (v === "edit") openEditModal(c);
}
/** 删除连接（确认由合并卡行内气泡承担） */
async function removeConn(c: ConnConfig) {
  try {
    if (isOpen(c.id)) {
      await GoTools.MySQLClose(c.id);
      openedIds.value = openedIds.value.filter((x) => x !== c.id);
    }
  } catch (e) { /* 关闭失败也允许移出列表 */ }
  if (st.activeConnId === c.id) resetActive();
  savedConns.value = savedConns.value.filter((x) => x.id !== c.id);
  await persistConnections();
  Message.success("连接已删除");
}

async function openConnection(c: ConnConfig) {
  leaveSrvOverview(); // 打开连接时切回连接工作区
  try {
    await GoTools.MySQLOpen({ ...c });
  } catch (e: any) { Message.error(String(e?.message || e)); return; }
  if (!isOpen(c.id)) openedIds.value.push(c.id);
  st.activeConnId = c.id;
  st.mode = "none";
  if (c.database) {
    st.activeDb = c.database;
  } else {
    st.activeDb = "";
  }
  await refreshDatabases();
  if (st.activeDb) { st.selectedKeys = ["db:" + st.activeDb]; await expandDbNode(st.activeDb); }
  else st.selectedKeys = [];
}

/** 本地服务组件请求连接：并入已保存连接后打开 */
function onSrvConnect(cfg: ConnConfig) {
  if (!savedConns.value.some((c) => c.id === cfg.id)) {
    savedConns.value.push({ ...cfg });
    persistConnections();
  }
  openConnection(cfg);
}

/* ===== 实例总览（对齐 PHP 主区域）：点左栏实例 → 右侧状态栅格 + 实时错误日志 ===== */
const srvCfg = ref<any>(null);
const srvState = ref<any>(null);
/** 错误日志弹窗可见性：轮询与内容都在 LogModal 内，父只开关 */
const srvLogVisible = ref(false);
const srvStartBusy = ref(false);
const srvStopBusy = ref(false);
let srvStateTimer: any = null;

/** 实例总览指标卡：版式对齐 nginx，把运行态/时长/连接/活跃/端口/版本收成一排卡片 */
const srvStatCards = computed(() => {
  const s: any = srvState.value, c: any = srvCfg.value;
  if (!c) return [] as any[];
  const running = !!s?.running;
  const docker = c.mode === "docker";
  return [
    {
      key: "state", title: "运行状态",
      value: running ? "运行中" : "已停止",
      sub: docker ? (s?.container || "容器") : (s?.pid ? "PID " + s.pid : "未启动"),
      icon: "server", tone: running ? "green" : "gray", ready: running,
      tip: running ? "端口已监听，mysqld 在跑" : "实例当前没有对外服务",
    },
    {
      key: "uptime", title: "运行时长", value: fmtUptimeS(s?.uptimeSeconds),
      sub: running ? "自本次启动累计" : "实例未运行", icon: "uptime", tone: "blue", ready: running,
      tip: "进程存活时长；重启后归零",
    },
    {
      key: "conn", title: "线程连接", value: s?.threadsConnected ?? "—",
      sub: "当前活动连接线程", icon: "net", tone: "cyan", ready: running,
      tip: "Threads_connected：当前连到服务器的客户端数",
    },
    {
      key: "active", title: "线程活跃", value: s?.threadsRunning ?? "—",
      sub: "正在执行语句", icon: "gauge", tone: "orange", ready: running,
      tip: "Threads_running：正在执行语句的线程数，突增说明有慢查询/锁等待",
    },
    {
      key: "port", title: "端口", value: c.port ? ":" + c.port : "—",
      sub: s?.maxConnections ? "最大连接 " + s.maxConnections : "127.0.0.1 仅本机", icon: "plug", tone: "purple", ready: !!c.port,
      tip: "实例监听端口（仅本机）",
    },
    {
      key: "ver", title: docker ? "镜像" : "MySQL 版本",
      value: docker ? imgTagOf(s?.image || c.image || "") : (s?.version || c.version || "—"),
      sub: docker ? "容器镜像 tag" : "本机安装版本", icon: docker ? "image" : "tag", tone: "red",
      ready: docker ? !!(s?.image || c.image) : !!(s?.version || c.version),
      tip: docker ? "Docker 实例的版本由镜像 tag 决定" : "本机安装的 MySQL 版本",
    },
  ];
});

/** Docker 镜像 tag（mysql:8.4 → 8.4），与本地服务面板标签一致 */
function imgTagOf(img: string) {
  const s = String(img || "");
  const i = s.lastIndexOf(":");
  return i >= 0 ? s.slice(i + 1) : s;
}

function fmtUptimeS(sec?: number) {
  if (!sec || sec <= 0) return "-";
  const s = Math.floor(sec);
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m ${s % 60}s`;
  return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;
}

async function refreshSrvState() {
  const cfg = srvCfg.value;
  if (!cfg) return;
  try { srvState.value = await GoTools.MySQLSrvStatus(cfg.id); } catch (e) { /* 保留旧值，不打断面板 */ }
}

function stopSrvTimers() {
  if (srvStateTimer) { clearInterval(srvStateTimer); srvStateTimer = null; }
}

/** 打开实例总览：状态 3s 一轮；错误日志改到弹窗里，打开才轮询（由 LogModal 自管） */
async function openSrvOverview(inst: any) {
  if (!inst?.id) return;
  srvCfg.value = { ...inst };
  srvState.value = null;
  stopSrvTimers();
  await refreshSrvState();
  srvStateTimer = setInterval(refreshSrvState, 3000);
}

/** 退出总览（打开连接时调用），右栏回到连接工作区 */
function leaveSrvOverview() {
  srvCfg.value = null;
  srvState.value = null;
  srvLogVisible.value = false;
  stopSrvTimers();
}

/* ===== 左栏合并列表：实例数据由子组件 sync 事件上抛，操作回调经 lspRef 下发 ===== */
const lspRef = ref<any>(null);
const instList = ref<SrvInstanceConfig[]>([]);
const instStates = reactive<Record<string, any>>({});
const dockerImageList = ref<string[]>([]);
const dockerInstCount = computed(() => instList.value.filter((i) => i.mode === "docker").length);

function onLspSync(p: any) {
  instList.value = (p?.instances || []).map((i: any) => ({ ...i }));
  for (const k of Object.keys(instStates)) delete instStates[k];
  Object.assign(instStates, p?.states || {});
  dockerImageList.value = p?.dockerImages || [];
}

/** 镜像清单非空且不含它 → 本机还没拉；启动时会自动拉 */
function imgMissing(image: string): boolean {
  return !!image && dockerImageList.value.length > 0 && !dockerImageList.value.includes(image);
}

const mysqlRawList = computed<MysqlListItem[]>(() => {
  const insts: MysqlListItem[] = instList.value.map((inst) => ({
    kind: "instance", key: "inst-" + inst.id, name: inst.name,
    sub: `127.0.0.1:${inst.port}`, live: !!instStates[inst.id]?.running, inst,
  }));
  // 历史上“连接实例”会把实例注入成 "srv-" 前缀的连接；合并后由实例行代表，过滤掉避免重复
  const conns: MysqlListItem[] = savedConns.value
    .filter((c) => !c.id.startsWith("srv-"))
    .map((c) => ({
      kind: "external", key: "conn-" + c.id, name: c.name || `${c.host}:${c.port}`,
      sub: `${c.host}:${c.port}`, live: isOpen(c.id), conn: c,
    }));
  return [...insts, ...conns];
});
/* 拖拽排序：左栏列表（实例+外部连接）顺序存入用户配置 mysql:list（与主页分开，随导出搬运） */
const mysqlOrder = ref<string[]>([]);
const mysqlReo = useReorder("mysql:list", {
  getVisible: () => mysqlRawList.value.map((i) => i.key),
  onReordered: (nv) => { mysqlOrder.value = nv; void GoTools?.SetListOrder?.("mysql:list", nv); },
});
const mysqlList = computed(() => applyOrder(mysqlRawList.value, mysqlOrder.value, (i) => i.key));

/** 当前右侧展示的是哪一行：实例看 srvCfg，连接看 activeConnId */
function itemActive(it: MysqlListItem): boolean {
  return it.kind === "instance" ? srvCfg.value?.id === it.inst.id : st.activeConnId === it.conn.id;
}

/** 点行：实例进右侧总览，连接进库树工作区（都不启动实例） */
function clickMysqlItem(it: MysqlListItem) {
  if (it.kind === "instance") openSrvOverview(it.inst);
  else openConnection(it.conn);
}

/** 双击行：未启动实例直接拉起（交给 LocalServicePanel 的启动处理）；已启动/外部连接与单击一致 */
function dblMysqlItem(it: MysqlListItem) {
  if (it.kind === "instance" && !it.live) lspRef.value?.onSrvAction("start", it.inst);
}

/** 删除确认文案：按来源区分实例 / 连接 */
function delTip(it: MysqlListItem): string {
  return it.kind === "instance"
    ? `确定删除实例「${it.inst.name}」？运行中会先停止；数据目录保留。`
    : `确定删除连接「${it.name}」？已打开的会话会一起关闭。`;
}

/** 统一列表下拉菜单入口：删除走确认框，其余分派给实例 / 连接 */
function onItemMenu(v: string, it: MysqlListItem) {
  if (v === "del") {
    Modal.warning({
      title: it.kind === "instance" ? "删除实例" : "删除连接",
      content: delTip(it),
      okText: "删除",
      cancelText: "取消",
      hideCancel: false,
      onOk: () => onDeleteItem(it),
    });
    return;
  }
  if (it.kind === "instance") lspRef.value?.onSrvAction(v, it.inst);
  else onConnAction(v, it.conn);
}

/** 确认后真正执行删除：实例交给子组件，连接本地处理 */
function onDeleteItem(it: MysqlListItem) {
  if (it.kind === "instance") lspRef.value?.removeInstance(it.inst);
  else removeConn(it.conn);
}

async function srvDoStart() {
  const cfg = srvCfg.value;
  if (!cfg || srvStartBusy.value || srvStopBusy.value) return;
  srvStartBusy.value = true;
  try {
    await GoTools.MySQLSrvStart(cfg.id);
    Message.success(`实例 ${cfg.name} 已启动`);
  } catch (e: any) { Message.error("启动失败: " + (e?.message || e)); }
  finally { srvStartBusy.value = false; refreshSrvState(); }
}

async function srvDoStop() {
  const cfg = srvCfg.value;
  if (!cfg || srvStopBusy.value) return;
  srvStopBusy.value = true;
  try {
    await GoTools.MySQLSrvStop(cfg.id);
    Message.success(`实例 ${cfg.name} 已停止`);
  } catch (e: any) { Message.error("停止失败: " + (e?.message || e)); }
  finally { srvStopBusy.value = false; refreshSrvState(); }
}

/** 没有单独的重启接口：停完再起（与本地服务面板「切版本后重启」一致） */
async function srvDoRestart() {
  const cfg = srvCfg.value;
  if (!cfg || srvStartBusy.value || srvStopBusy.value) return;
  srvStartBusy.value = true;
  try {
    await GoTools.MySQLSrvStop(cfg.id);
    await GoTools.MySQLSrvStart(cfg.id);
    Message.success(`实例 ${cfg.name} 已重启`);
  } catch (e: any) { Message.error("重启失败: " + (e?.message || e)); }
  finally { srvStartBusy.value = false; refreshSrvState(); }
}

/** 从总览进连接：并入已保存连接并在右侧打开工作区（自动退出总览） */
function srvConnect() {
  const cfg = srvCfg.value;
  if (!cfg) return;
  if (!srvState.value?.running) { Message.warning("请先启动实例"); return; }
  onSrvConnect({
    id: "srv-" + cfg.id, name: `${cfg.name}(${cfg.mode === "docker" ? "容器" : "本地"})`,
    host: "127.0.0.1", port: cfg.port, user: "root", password: cfg.rootPassword, database: "",
  } as any);
}

/** 打开错误日志弹窗（轮询由子组件按可见性自管） */
function openSrvLog() {
  srvLogVisible.value = true;
}

/** 用系统默认程序打开文件或所在目录；属桌面能力，失败给提示 */
async function openPath(p?: string) {
  if (!p) { Message.warning("路径为空，无法打开"); return; }
  try { await GoTools.DiskOpenFile(p); }
  catch (e: any) { Message.error("打开失败：" + String(e?.message || e)); }
}

function resetActive() {
  st.activeConnId = ""; st.activeDb = ""; st.tables = []; st.views = []; st.routines = [];
  st.databases = []; st.mode = "none"; st.table = "";
}

async function refreshDatabases() {
  try { st.databases = (await GoTools.MySQLListDatabases(st.activeConnId)) || []; }
  catch (e: any) { Message.error(String(e?.message || e)); return; }
  nodeMap.clear();
  st.expandedKeys = [];
  treeData.value = st.databases.map((db) => {
    const key = "db:" + db;
    nodeMap.set(key, { kind: "db", db, name: db });
    return { key, title: db, isLeaf: false };
  });
}

/** 工具栏刷新：重建库列表后保持当前库展开 */
async function refreshTree() {
  await refreshDatabases();
  if (st.activeDb) {
    st.selectedKeys = ["db:" + st.activeDb];
    await expandDbNode(st.activeDb);
  }
}

/** 拉取指定库的表/视图/函数 */
async function fetchObjects(db: string) {
  const [t, v, r] = await Promise.all([
    GoTools.MySQLListTables(st.activeConnId, db),
    GoTools.MySQLListViews(st.activeConnId, db),
    GoTools.MySQLListRoutines(st.activeConnId, db),
  ]);
  return { t: t || [], v: v || [], r: r || [] };
}

/** 展开数据库节点时懒加载其表/视图/函数 */
function loadMore(node: any) {
  const meta = nodeMap.get(node.key);
  if (!meta || meta.kind !== "db") return Promise.resolve();
  return fetchObjects(meta.db)
    .then(({ t, v, r }) => {
      node.children = buildGroups(meta.db, t, v, r);
      if (meta.db === st.activeDb) { st.tables = t; st.views = v; st.routines = r; }
    })
    .catch((e: any) => { Message.error(String(e?.message || e)); });
}

/** 重新加载当前库对象，并同步树节点（若已展开） */
async function reloadDb(db: string) {
  try {
    const { t, v, r } = await fetchObjects(db);
    st.tables = t; st.views = v; st.routines = r;
    const node = treeData.value.find((n: any) => n.key === "db:" + db);
    if (node && node.children) node.children = buildGroups(db, t, v, r);
  } catch (e: any) { Message.error(String(e?.message || e)); }
}

function loadObjects() { return reloadDb(st.activeDb); }

/** 展开数据库节点：拉取最新对象并展开「表」分组，便于直接看到表列表 */
async function expandDbNode(db: string) {
  const node = treeData.value.find((n: any) => n.key === "db:" + db);
  if (!node) return;
  try {
    const { t, v, r } = await fetchObjects(db);
    node.children = buildGroups(db, t, v, r);
    if (db === st.activeDb) { st.tables = t; st.views = v; st.routines = r; }
  } catch (e: any) { Message.error(String(e?.message || e)); return; }
  const keys = new Set(st.expandedKeys.map(String));
  keys.add(node.key);
  const tKey = `grp:${db}:t`;
  if ((node.children || []).some((g: any) => g.key === tKey)) keys.add(tKey);
  st.expandedKeys = [...keys];
}

/* ============================ 对象树右键菜单 ============================ */

const ctx = reactive({
  visible: false,
  x: 0,
  y: 0,
  node: null as CtxTarget | null,
});

function onNodeContextMenu(e: MouseEvent, node: any) {
  const key = String(node?.key || "");
  if (!key) return;
  const meta = nodeMap.get(key);
  let target: CtxTarget;
  if (meta) {
    target = { key, kind: meta.kind, gkind: "", db: meta.db, name: meta.name, rtype: meta.rtype || "" };
  } else if (key.startsWith("grp:")) {
    const parts = key.split(":");
    target = { key, kind: "group", gkind: parts[2] || "", db: parts[1] || "", name: "" };
  } else {
    return;
  }
  ctx.node = target;
  ctx.x = Math.max(8, Math.min(e.clientX, window.innerWidth - 250));
  ctx.y = Math.max(8, Math.min(e.clientY, window.innerHeight - 330));
  ctx.visible = true;
}

/** Esc 关闭右键菜单 */
function onCtxKeydown(e: KeyboardEvent) {
  if (e.key === "Escape" && ctx.visible) ctx.visible = false;
}

/** 刷新指定库的树节点（非当前库仅重建懒加载子节点） */
async function refreshDbTree(db: string) {
  if (!db) return;
  if (db === st.activeDb) { await reloadDb(db); return; }
  try {
    const { t, v, r } = await fetchObjects(db);
    const node = treeData.value.find((n: any) => n.key === "db:" + db);
    if (node && node.children) node.children = buildGroups(db, t, v, r);
  } catch (e: any) { Message.error(String(e?.message || e)); }
}

async function onCtxMenuClick(key: string) {
  const n = ctx.node;
  ctx.visible = false;
  if (!n) return;
  if (key === "refresh") {
    await refreshDbTree(n.db);
    // 若刷新对象正是当前打开的表/视图/函数，同步刷新内容
    if (st.activeDb === n.db) {
      if (n.kind === "table" && st.mode === "table" && st.table === n.name) { await loadSchema(); await loadData(); }
      else if (n.kind === "view" && st.mode === "view" && st.view === n.name) await loadViewData();
      else if (n.kind === "func" && st.mode === "routine" && st.routine === n.name) openRoutine(n.name);
    }
    return;
  }
  if (key === "new") { openNewTable(n.db); return; }
  if (key === "open") {
    st.activeDb = n.db;
    st.selectedKeys = [n.key];
    if (n.kind === "table") await openTable(n.name);
    else if (n.kind === "view") await openView(n.name);
    else if (n.kind === "func") { await reloadDb(n.db); openRoutine(n.name); }
    return;
  }
  if (key === "design") {
    st.activeDb = n.db;
    st.selectedKeys = [n.key];
    await openTable(n.name);
    st.tableTab = "struct";
    return;
  }
  if (key === "rename") { openRename(n); return; }
  if (key === "copy-data" || key === "copy-struct") { openCopyTable(n, key === "copy-data"); return; }
  if (key === "clear") { askClearTable(n); return; }
  if (key === "truncate") { askTruncateTable(n); return; }
  if (key === "drop") { askDropObject(n); return; }
}

/** 右键菜单的 DDL 执行（清空/截断/删除），并同步树与已打开页面 */
async function ctxExecObject(n: CtxTarget, sql: string, okMsg: string, dropped: boolean) {
  try {
    await GoTools.MySQLExecute(st.activeConnId, n.db, sql);
    Message.success(okMsg);
    const active = st.activeDb === n.db;
    if (dropped && active) {
      if (n.kind === "table" && st.mode === "table" && st.table === n.name) { st.mode = "none"; st.table = ""; }
      if (n.kind === "view" && st.mode === "view" && st.view === n.name) { st.mode = "none"; st.view = ""; }
      if (n.kind === "func" && st.mode === "routine" && st.routine === n.name) { st.mode = "none"; st.routine = ""; }
      st.selectedKeys = [];
    }
    if (!dropped && active && n.kind === "table" && st.mode === "table" && st.table === n.name) await loadData();
    await refreshDbTree(n.db);
  } catch (e: any) { Message.error(String(e?.message || e)); }
}

function openNewTable(db: string) {
  if (db) st.activeDb = db;
  st.newTableDDL = "";
  st.newTableModal = true;
}

function openRename(n: CtxTarget) {
  st.renameTarget = n;
  st.renameName = n.name;
  st.renameModal = true;
}

async function doRename() {
  const n = st.renameTarget;
  if (!n) return;
  const name = st.renameName.trim();
  const nErr = validateIdentifier(name, n.kind === "view" ? "视图名" : "表名");
  if (nErr) { Message.warning(nErr); return; }
  if (name === n.name) { st.renameModal = false; return; }
  try {
    await GoTools.MySQLExecute(st.activeConnId, n.db, `RENAME TABLE ${qt(n.db, n.name)} TO ${qt(n.db, name)}`);
    Message.success("已重命名");
    st.renameModal = false;
    if (st.activeDb === n.db) {
      if (n.kind === "table" && st.table === n.name) st.table = name;
      if (n.kind === "view" && st.view === n.name) st.view = name;
      st.selectedKeys = [`${n.kind}:${n.db}:${name}`];
    }
    await refreshDbTree(n.db);
  } catch (e: any) { Message.error(String(e?.message || e)); }
}

function openCopyTable(n: CtxTarget, withData: boolean) {
  st.copyTarget = n;
  st.copyName = n.name + "_copy";
  st.copyWithData = withData;
  st.copyModal = true;
}

async function doCopyTable() {
  const n = st.copyTarget;
  if (!n) return;
  const name = st.copyName.trim();
  const nErr = validateIdentifier(name, "新表名");
  if (nErr) { Message.warning(nErr); return; }
  if (name === n.name) { Message.warning("新表名不能与原表相同"); return; }
  const sql = `CREATE TABLE ${qt(n.db, name)} LIKE ${qt(n.db, n.name)}`
    + (st.copyWithData ? `;INSERT INTO ${qt(n.db, name)} SELECT * FROM ${qt(n.db, n.name)}` : "");
  try {
    await GoTools.MySQLExecute(st.activeConnId, n.db, sql);
    Message.success("表已复制");
    st.copyModal = false;
    await refreshDbTree(n.db);
  } catch (e: any) { Message.error(String(e?.message || e)); }
}

async function setMode(m: string) {
  st.mode = m;
  if (m === "query" || m === "backup" || m === "tx") {
    const p: any = (st as any)[m];
    p.connId = st.activeConnId;
    p.db = st.activeDb;
    await panelLoadDatabases(m);
    if (m === "backup") await panelLoadTables(m);
  }
}

/** 加载某个面板（查询/备份/事务）所选连接的数据库列表 */
async function panelLoadDatabases(m: string) {
  const p: any = (st as any)[m];
  if (!p.connId) { p.databases = []; return; }
  try { p.databases = (await GoTools.MySQLListDatabases(p.connId)) || []; }
  catch (e: any) { p.databases = []; Message.error(String(e?.message || e)); }
}

/** 备份面板：加载所选库的表列表 */
async function panelLoadTables(m: string) {
  const p: any = (st as any)[m];
  if (m !== "backup") return;
  if (!p.connId || !p.db) { p.tableList = []; return; }
  try { p.tableList = (await GoTools.MySQLListTables(p.connId, p.db)) || []; }
  catch { p.tableList = []; }
}

async function onPanelConnChange(m: string) {
  const p: any = (st as any)[m];
  p.db = "";
  if (m === "backup") { p.tables = []; p.tableList = []; p.sql = ""; }
  await panelLoadDatabases(m);
}

async function onPanelDbChange(m: string) {
  if (m === "backup") { st.backup.tables = []; await panelLoadTables("backup"); }
}

function onTreeSelect(keys: (string | number)[]) {
  if (!keys || keys.length === 0) return;
  const meta = nodeMap.get(String(keys[0]));
  if (!meta) return;
  if (meta.kind === "db") {
    st.activeDb = meta.db; st.mode = "none"; st.table = "";
    expandDbNode(meta.db);
  }
  else if (meta.kind === "table") { st.activeDb = meta.db; openTable(meta.name); }
  else if (meta.kind === "view") { st.activeDb = meta.db; openView(meta.name); }
  else if (meta.kind === "func") { st.activeDb = meta.db; openRoutine(meta.name); }
}

/* ============================ 表 ============================ */

async function openTable(name: string) {
  st.table = name; st.mode = "table"; st.tableTab = "data";
  st.data.filter = ""; st.data.orderBy = ""; st.data.desc = false; st.data.page = 1;
  st.schema = { columns: [], indexes: [], ddl: "", pk: "" };
  await loadSchema();
  await loadData();
}

async function loadSchema() {
  try { st.schema = await GoTools.MySQLTableSchema(st.activeConnId, st.activeDb, st.table); }
  catch (e: any) { Message.error(String(e?.message || e)); }
}

async function loadData() {
  const fErr = validateFilter(st.data.filter);
  if (fErr) { Message.warning(fErr); return; }
  try {
    const res = await GoTools.MySQLTableData(st.activeConnId, st.activeDb, st.table, st.data.filter, st.data.orderBy, st.data.desc, st.data.page, st.data.size);
    st.data.columns = res.columns || [];
    // 注入行标识：表格多选/批量删除依赖 row-key
    st.data.rows = (res.rows || []).map((r: any, i: number) => ({ ...r, __rid: `${st.data.page}:${i}` }));
    st.data.total = res.affected || 0;
    st.data.note = res.note || "";
    st.data.selectedKeys = [];
    st.edit = null;
  } catch (e: any) { Message.error(String(e?.message || e)); }
}

function onTableTab(k: string | number | boolean) { if (String(k) === "data" && st.data.rows.length === 0) loadData(); }
function clearFilter() { st.data.filter = ""; st.data.orderBy = ""; st.data.desc = false; st.data.page = 1; loadData(); }
function goPage(p: number) { st.data.page = p; loadData(); }
function onPageSize(s: number) { st.data.size = s; st.data.page = 1; loadData(); }
function onSorter(dataIndex: string, direction: string) {
  if (!direction) { st.data.orderBy = ""; st.data.desc = false; }
  else { st.data.orderBy = String(dataIndex); st.data.desc = direction === "descend"; }
  st.data.page = 1; loadData();
}

/* ---- 标识符 / 筛选条件校验 ---- */

/** 标识符校验（列/索引/表名）：字母、数字、下划线、$ 或中文，最长 64 */
function validateIdentifier(name: string, label: string): string {
  const n = String(name || "").trim();
  if (!n) return `请填写${label}`;
  if (!/^[A-Za-z0-9_$\u4e00-\u9fa5]+$/.test(n)) return `${label}只能包含字母、数字、下划线、$ 或中文`;
  if (n.length > 64) return `${label}长度不能超过 64 个字符`;
  return "";
}

/** 筛选条件校验：禁止多语句与注释 */
function validateFilter(filter: string): string {
  const f = String(filter || "");
  if (!f.trim()) return "";
  if (f.includes(";")) return "筛选条件不能包含分号（不支持多语句）";
  if (/--|#|\/\*/.test(f)) return "筛选条件不能包含 SQL 注释";
  return "";
}

/* ---- 列类型判断与单元格校验 ---- */
const INT_LIMITS: Record<string, [string, string]> = {
  tinyint: ["-128", "127"], smallint: ["-32768", "32767"], mediumint: ["-8388608", "8388607"],
  int: ["-2147483648", "2147483647"], bigint: ["-9223372036854775808", "9223372036854775807"],
};

function isNumericColumnType(t: string): boolean {
  return /^(tinyint|smallint|mediumint|int|integer|bigint|decimal|numeric|float|double|real|bit)\b/.test(t);
}

/** 可接受空串的字符串类型（enum/set/json 清空时应写 NULL） */
function isTextColumnType(t: string): boolean {
  return /^(char|varchar|tinytext|text|mediumtext|longtext|binary|varbinary|tinyblob|blob|mediumblob|longblob)\b/.test(t);
}

/** 整数类型的取值范围（支持 unsigned） */
function intRangeOf(t: string): [bigint, bigint] | null {
  const m = /^(tinyint|smallint|mediumint|int|integer|bigint)(\s+unsigned)?/.exec(t);
  if (!m) return null;
  const base = m[1] === "integer" ? "int" : m[1];
  const [lo, hi] = INT_LIMITS[base];
  return m[2] ? [0n, BigInt(hi) * 2n + 1n] : [BigInt(lo), BigInt(hi)];
}

/** 按列类型选编辑器：enum -> 下拉，date/time -> 选择器，数值 -> 数字校验 */
function cellKindOf(t: string): CellEdit["kind"] {
  const s = String(t || "").toLowerCase();
  if (/^enum\(/.test(s)) return "enum";
  if (/^(datetime|timestamp)\b/.test(s)) return "datetime";
  if (/^date\b/.test(s)) return "date";
  if (/^time\b/.test(s)) return "time";
  if (isNumericColumnType(s)) return "number";
  return "text";
}

/** 单元格值校验（非空值）；返回空串表示通过 */
function validateValue(meta: any, raw: any): string {
  const t = String(meta?.type || "").toLowerCase();
  const val = String(raw ?? "").trim();
  if (!val) return "";
  const em = /^enum\((.*)\)/.exec(t);
  if (em) {
    const list = parseEnumValues(em[1]);
    return list.includes(val) ? "" : "值必须是：" + list.join(" / ");
  }
  const range = intRangeOf(t);
  if (range) {
    if (!/^-?\d+$/.test(val)) return "请输入整数";
    const n = BigInt(val);
    if (n < range[0] || n > range[1]) return "超出 " + t + " 的取值范围";
    return "";
  }
  if (/^(decimal|numeric|float|double|real)\b/.test(t)) {
    return /^-?\d+(\.\d+)?([eE][+-]?\d+)?$/.test(val) ? "" : "请输入数字";
  }
  if (/^date\b/.test(t)) {
    return /^\d{4}-\d{2}-\d{2}$/.test(val) && !Number.isNaN(Date.parse(val + "T00:00:00")) ? "" : "日期格式应为 YYYY-MM-DD";
  }
  if (/^(datetime|timestamp)\b/.test(t)) {
    return /^\d{4}-\d{2}-\d{2}( \d{2}:\d{2}(:\d{2})?)?$/.test(val) ? "" : "时间格式应为 YYYY-MM-DD HH:mm:ss";
  }
  if (/^time\b/.test(t)) return /^\d{1,3}:\d{2}(:\d{2})?$/.test(val) ? "" : "时间格式应为 HH:mm:ss";
  if (/^year\b/.test(t)) {
    const y = Number(val);
    return /^\d{4}$/.test(val) && y >= 1901 && y <= 2155 ? "" : "年份范围 1901-2155";
  }
  if (/^json\b/.test(t)) {
    try { JSON.parse(String(raw)); return ""; } catch { return "JSON 格式不正确"; }
  }
  const m = /^(?:var)?char\((\d+)\)/.exec(t);
  if (m && [...val].length > Number(m[1])) return "长度不能超过 " + m[1];
  return "";
}

function startEdit(ri: number, c: string, row: any) {
  if (!st.schema.pk) { Message.warning("无单列主键，无法编辑"); return; }
  if (c === st.schema.pk) { Message.warning("主键列不可直接编辑，请使用 SQL 修改"); return; }
  const meta = (st.schema.columns || []).find((x: any) => x.name === c) || { name: c, type: "", nullable: true };
  const kind = cellKindOf(String(meta.type || ""));
  let val = row[c] === null || row[c] === undefined ? "" : String(row[c]);
  let options: { label: string; value: string }[] = [];
  if (kind === "enum") {
    const m = /^enum\((.*)\)/i.exec(String(meta.type));
    options = parseEnumValues(m?.[1] || "").map((v) => ({ label: v, value: v }));
  } else if (kind === "date") {
    val = val.slice(0, 10); // 驱动把 DATE 解析成 2006-01-02 15:04:05
  } else if (kind === "datetime" && !/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/.test(val)) {
    val = val.slice(0, 10) + (val.length > 10 ? " " + val.slice(11, 19) : " 00:00:00");
  }
  st.edit = { r: ri, c, val, kind, options, meta };
}

async function commitEdit(row: any, c: string) {
  if (!st.edit || st.edit.c !== c) return;
  const ed = st.edit;
  const val = ed.val === null || ed.val === undefined ? "" : String(ed.val).trim();
  if (val === "") {
    if (!ed.meta?.nullable) { Message.warning("该列不允许为空"); return; }
  } else {
    const err = validateValue(ed.meta, val);
    if (err) { Message.warning(err); return; }
  }
  const type = String(ed.meta?.type || "").toLowerCase();
  const curRaw = row[c] === null || row[c] === undefined ? "" : String(row[c]);
  const cur = ed.kind === "date" ? curRaw.slice(0, 10) : curRaw;
  if (val === cur) { st.edit = null; return; }
  st.edit = null;
  // 可空且非字符串类型：清空时写 NULL（避免空串导致日期/数字报错）
  const send = val === "" && ed.meta?.nullable && !isTextColumnType(type) ? "\u0000NULL" : val;
  try {
    await GoTools.MySQLUpdateCell(st.activeConnId, st.activeDb, st.table, st.schema.pk, String(row[st.schema.pk]), c, send);
    Message.success("已更新");
    loadData();
  } catch (e: any) { Message.error(String(e?.message || e)); }
}

function openInsert() {
  if (!st.schema.columns.length) { Message.warning("请先加载表结构"); return; }
  Object.keys(insertData).forEach((k) => delete insertData[k]);
  Object.keys(insertNull).forEach((k) => delete insertNull[k]);
  insertCols.value.forEach((c: any) => { insertData[c.name] = ""; insertNull[c.name] = false; });
  st.insertModal = true;
}
async function doInsert() {
  const data: Record<string, string> = {};
  for (const c of insertCols.value as any[]) {
    if (insertNull[c.name]) { data[c.name] = "\u0000NULL"; continue; }
    const raw = insertData[c.name];
    const isEmpty = raw === "" || raw === undefined || raw === null;
    if (isEmpty) {
      const autoInc = String(c.extra || "").toLowerCase().includes("auto_increment");
      const hasDefault = c.default !== null && c.default !== undefined && String(c.default) !== "";
      if (!c.nullable && !autoInc && !hasDefault) {
        Message.warning(`列「${c.name}」不允许为空且无默认值，请填写`);
        return;
      }
      continue;
    }
    const err = validateValue(c, raw);
    if (err) { Message.warning(`${c.name}：${err}`); return; }
    data[c.name] = raw;
  }
  if (Object.keys(data).length === 0) { Message.warning("请填写至少一个字段"); return; }
  try {
    await GoTools.MySQLInsertRow(st.activeConnId, st.activeDb, st.table, data);
    Message.success("已新增");
    st.insertModal = false;
    loadData();
  } catch (e: any) { Message.error(String(e?.message || e)); }
}

/** 删除行：危险操作二次确认 */
function askDeleteRow(row: any) {
  if (!st.schema.pk) return;
  const pkVal = String(row[st.schema.pk]);
  askDanger({
    title: "删除数据行",
    desc: `确定删除表「${st.table}」中主键 ${st.schema.pk}=${pkVal} 的行？此操作不可撤销。`,
    sql: `DELETE FROM ${qt(st.activeDb, st.table)} WHERE ${bt(st.schema.pk)} = ${qv(pkVal)}`,
    okText: "删除",
    action: () => deleteRow(row),
  });
}

function deleteRow(row: any) {
  if (!st.schema.pk) return;
  GoTools.MySQLDeleteRow(st.activeConnId, st.activeDb, st.table, st.schema.pk, String(row[st.schema.pk]))
    .then(() => loadData())
    .catch((e: any) => Message.error(String(e?.message || e)));
}

/** 批量删除选中行：输入表名确认后执行 */
function askDeleteSelected() {
  if (!st.schema.pk) { Message.warning("该表无单列主键，无法批量删除"); return; }
  const keys = st.data.selectedKeys || [];
  if (!keys.length) { Message.warning("请先勾选要删除的行"); return; }
  const rows = (st.data.rows || []).filter((r: any) => keys.includes(r.__rid));
  const pkVals = rows.map((r: any) => String(r[st.schema.pk]));
  if (!pkVals.length) { Message.warning("未匹配到选中行，请刷新后重试"); return; }
  const preview = pkVals.slice(0, 20).map(qv).join(", ") + (pkVals.length > 20 ? ", …" : "");
  askDanger({
    title: "批量删除数据",
    desc: `将从表「${st.table}」删除 ${pkVals.length} 行（主键 ${st.schema.pk}）。此操作不可撤销。`,
    sql: `DELETE FROM ${qt(st.activeDb, st.table)} WHERE ${bt(st.schema.pk)} IN (${preview})`,
    keyword: st.table,
    okText: `删除 ${pkVals.length} 行`,
    action: () => doDeleteSelected(pkVals),
  });
}
async function doDeleteSelected(pkVals: string[]) {
  try {
    const n = await GoTools.MySQLDeleteRows(st.activeConnId, st.activeDb, st.table, st.schema.pk, pkVals);
    Message.success(`已删除 ${n} 行`);
    st.data.selectedKeys = [];
    await loadData();
  } catch (e: any) { Message.error(String(e?.message || e)); }
}

/** 删除表（结构页按钮）：输入表名确认 */
function askDropTable() {
  const name = st.table;
  askDanger({
    title: "删除表",
    desc: `确定删除表「${name}」？表结构与全部数据都会被永久删除。`,
    sql: `DROP TABLE ${qt(st.activeDb, name)}`,
    keyword: name,
    okText: "删除表",
    action: dropTable,
  });
}

function dropTable() {
  GoTools.MySQLExecute(st.activeConnId, st.activeDb, "DROP TABLE " + qt(st.activeDb, st.table)).then(async () => {
    Message.success("表已删除");
    st.mode = "none"; st.table = ""; st.selectedKeys = [];
    await loadObjects();
  }).catch((e: any) => Message.error(String(e?.message || e)));
}

function bt(s: string) { return "`" + String(s).replace(/`/g, "``") + "`"; }
/** 库名限定对象名，避免依赖连接的当前库 */
function qt(db: string, name: string) { return bt(db) + "." + bt(name); }

function createIndexModal() { Object.assign(indexForm, { name: "", columns: "", unique: false }); st.indexModal = true; }
async function doCreateIndex() {
  const name = indexForm.name.trim();
  if (!indexForm.name || !indexForm.columns) { Message.warning("请填写索引名和列"); return; }
  const nErr = validateIdentifier(name, "索引名");
  if (nErr) { Message.warning(nErr); return; }
  const rawCols = indexForm.columns.split(",").map((s) => s.trim()).filter((s) => s !== "");
  if (!rawCols.length) { Message.warning("请填写索引列"); return; }
  for (const c of rawCols) {
    const e = validateIdentifier(c, "索引列");
    if (e) { Message.warning(e); return; }
  }
  const cols = rawCols.map(bt).join(",");
  const sql = `CREATE ${indexForm.unique ? "UNIQUE " : ""}INDEX ${bt(name)} ON ${qt(st.activeDb, st.table)} (${cols})`;
  try { await GoTools.MySQLExecute(st.activeConnId, st.activeDb, sql); Message.success("索引已创建"); st.indexModal = false; loadSchema(); }
  catch (e: any) { Message.error(String(e?.message || e)); }
}

/** 删除索引：危险操作二次确认 */
function askDropIndex(idx: any) {
  askDanger({
    title: "删除索引",
    desc: `确定删除表「${st.table}」的索引「${idx.name}」？删除后相关查询可能变慢。`,
    sql: `DROP INDEX ${bt(idx.name)} ON ${qt(st.activeDb, st.table)}`,
    okText: "删除",
    action: () => dropIndex(idx),
  });
}
function dropIndex(idx: any) {
  GoTools.MySQLExecute(st.activeConnId, st.activeDb, `DROP INDEX ${bt(idx.name)} ON ${qt(st.activeDb, st.table)}`)
    .then(() => loadSchema())
    .catch((e: any) => Message.error(String(e?.message || e)));
}

/* ---- 列增/改/删 ---- */
function setColType(t: string) {
  const { base, limit } = parseColTypeDef(t);
  colTypeBase.value = base || "varchar";
  colTypeLimit.value = limit;
}

function addColumnModal() {
  st.colEditing = false; st.colOldName = "";
  Object.assign(colForm, { name: "", type: "varchar(255)", nullable: true, default: "", autoIncrement: false, primaryKey: false, comment: "", after: "" });
  setColType("varchar(255)");
  st.colModal = true;
}
function editColumnModal(col: any) {
  st.colEditing = true; st.colOldName = col.name;
  Object.assign(colForm, {
    name: col.name, type: col.type, nullable: !!col.nullable,
    default: col.default || "",
    autoIncrement: (col.extra || "").toLowerCase().includes("auto_increment"),
    primaryKey: col.key === "PRI",
    comment: col.comment || "", after: "",
  });
  setColType(col.type || "");
  st.colModal = true;
}
async function saveColumn() {
  const name = colForm.name.trim();
  const nErr = validateIdentifier(name, "列名");
  if (nErr) { Message.warning(nErr); return; }
  const type = buildColType();
  if (!type) {
    Message.warning(typeIsEnum.value ? "请填写枚举值（英文逗号分隔）" : "类型 limit 需为数字，如 255 或 10,2");
    return;
  }
  const dErr = validateColDefault(type, colForm.default);
  if (dErr) { Message.warning(dErr); return; }
  if (st.colEditing && st.colOldName === st.schema.pk && name !== st.colOldName) {
    Message.warning("主键列不允许重命名，请先调整主键");
    return;
  }
  const payload = { ...colForm, name, type };
  if (st.colEditing) {
    // 修改列可能重建表并截断数据，需二次确认
    askDanger({
      title: "修改列确认",
      desc: `将修改列「${st.colOldName}」的定义${name !== st.colOldName ? `并重命名为「${name}」` : ""}。修改类型/可空性可能导致数据截断，请确认。`,
      okText: "保存修改",
      action: () => doSaveColumn(payload),
    });
    return;
  }
  await doSaveColumn(payload);
}
async function doSaveColumn(payload: any) {
  try {
    if (st.colEditing) await GoTools.MySQLModifyColumn(st.activeConnId, st.activeDb, st.table, st.colOldName, payload);
    else await GoTools.MySQLAddColumn(st.activeConnId, st.activeDb, st.table, payload);
    Message.success("已保存"); st.colModal = false; await loadSchema();
  } catch (e: any) { Message.error(String(e?.message || e)); }
}

/** 删除列：输入列名确认 */
function askDropColumn(col: any) {
  askDanger({
    title: "删除列",
    desc: `确定删除表「${st.table}」的列「${col.name}」？该列数据将全部丢失，不可恢复。`,
    sql: `ALTER TABLE ${qt(st.activeDb, st.table)} DROP COLUMN ${bt(col.name)}`,
    keyword: col.name,
    okText: "删除列",
    action: () => dropColumn(col),
  });
}
function dropColumn(col: any) {
  GoTools.MySQLDropColumn(st.activeConnId, st.activeDb, st.table, col.name)
    .then(() => loadSchema())
    .catch((e: any) => Message.error(String(e?.message || e)));
}

function doCreateTable() {
  const ddl = st.newTableDDL.trim();
  if (!ddl) { Message.warning("请输入 DDL"); return; }
  const first = (/^[A-Za-z]+/.exec(stripSqlLeading(ddl))?.[0] || "").toUpperCase();
  if (first !== "CREATE") { Message.warning("新建表仅支持 CREATE 语句"); return; }
  if (splitSqlStatements(ddl).length !== 1) { Message.warning("请一次只提交一条 CREATE TABLE 语句"); return; }
  GoTools.MySQLExecute(st.activeConnId, st.activeDb, ddl).then(() => {
    Message.success("表已创建"); st.newTableModal = false; loadObjects();
  }).catch((e: any) => Message.error(String(e?.message || e)));
}

/* ============================ 视图 / 函数 ============================ */

async function openView(name: string) {
  st.view = name; st.mode = "view";
  await loadViewData();
}
async function loadViewData() {
  try {
    const res = await GoTools.MySQLExecute(st.activeConnId, st.activeDb, "SELECT * FROM " + bt(st.view) + " LIMIT 500");
    st.data.columns = res.columns || []; st.data.rows = res.rows || [];
  } catch (e: any) { Message.error(String(e?.message || e)); }
}
/** 删除视图：输入视图名确认 */
function askDropView() {
  const name = st.view;
  askDanger({
    title: "删除视图",
    desc: `确定删除视图「${name}」？`,
    sql: `DROP VIEW ${qt(st.activeDb, name)}`,
    keyword: name,
    okText: "删除视图",
    action: dropView,
  });
}
function dropView() {
  GoTools.MySQLExecute(st.activeConnId, st.activeDb, "DROP VIEW " + qt(st.activeDb, st.view)).then(async () => {
    Message.success("视图已删除");
    st.mode = "none"; st.view = ""; st.selectedKeys = [];
    await loadObjects();
  }).catch((e: any) => Message.error(String(e?.message || e)));
}

function openRoutine(name: string) {
  const r = st.routines.find((x) => x.name === name);
  st.routine = name; st.routineType = r?.type || ""; st.routineDef = r?.definition || ""; st.mode = "routine";
}
/** 删除函数/存储过程：输入名称确认 */
function askDropRoutine() {
  const kind = st.routineType === "PROCEDURE" ? "PROCEDURE" : "FUNCTION";
  const label = kind === "PROCEDURE" ? "存储过程" : "函数";
  const name = st.routine;
  askDanger({
    title: `删除${label}`,
    desc: `确定删除${label}「${name}」？`,
    sql: `DROP ${kind} ${qt(st.activeDb, name)}`,
    keyword: name,
    okText: "删除",
    action: dropRoutine,
  });
}
function dropRoutine() {
  const kind = st.routineType === "PROCEDURE" ? "PROCEDURE" : "FUNCTION";
  GoTools.MySQLExecute(st.activeConnId, st.activeDb, `DROP ${kind} ` + qt(st.activeDb, st.routine)).then(async () => {
    Message.success("已删除");
    st.mode = "none"; st.routine = ""; st.selectedKeys = [];
    await loadObjects();
  }).catch((e: any) => Message.error(String(e?.message || e)));
}

/* ============================ SQL 校验与危险操作确认 ============================ */

/** 去除 SQL 注释（保留字符串字面量内容） */
function stripSqlComments(s: string): string {
  const src = String(s || "");
  let out = "", i = 0, inQ = "";
  while (i < src.length) {
    const ch = src[i];
    if (inQ) {
      out += ch; i++;
      if (ch === "\\" && inQ !== "`" && i < src.length) { out += src[i]; i++; continue; }
      if (ch === inQ) { if (src[i] === inQ) { out += src[i]; i++; continue; } inQ = ""; }
      continue;
    }
    if (ch === "'" || ch === '"' || ch === "`") { inQ = ch; out += ch; i++; continue; }
    if (ch === "-" && src[i + 1] === "-") { const j = src.indexOf("\n", i); i = j < 0 ? src.length : j; out += " "; continue; }
    if (ch === "#") { const j = src.indexOf("\n", i); i = j < 0 ? src.length : j; out += " "; continue; }
    if (ch === "/" && src[i + 1] === "*") { const j = src.indexOf("*/", i + 2); i = j < 0 ? src.length : j + 2; out += " "; continue; }
    out += ch; i++;
  }
  return out;
}

/** 去掉前导注释与空白，取有效起始处 */
function stripSqlLeading(s: string): string {
  return stripSqlComments(s).trimStart();
}

/** 拆分多语句（忽略字符串/反引号内的分号），用于执行前校验 */
function splitSqlStatements(sql: string): string[] {
  const s = stripSqlComments(sql);
  const out: string[] = [];
  let buf = "", i = 0, inQ = "";
  while (i < s.length) {
    const ch = s[i];
    if (inQ) {
      buf += ch; i++;
      if (ch === "\\" && inQ !== "`" && i < s.length) { buf += s[i]; i++; continue; }
      if (ch === inQ) { if (s[i] === inQ) { buf += s[i]; i++; continue; } inQ = ""; }
      continue;
    }
    if (ch === "'" || ch === '"' || ch === "`") { inQ = ch; buf += ch; i++; continue; }
    if (ch === ";") { if (buf.trim()) out.push(buf.trim()); buf = ""; i++; continue; }
    buf += ch; i++;
  }
  if (buf.trim()) out.push(buf.trim());
  return out;
}

/** 是否存在顶层 WHERE（忽略子查询内的 WHERE），用于判断全表 UPDATE/DELETE */
function hasTopWhere(stmt: string): boolean {
  const s = stripSqlComments(stmt);
  let depth = 0, i = 0, inQ = "", word = "";
  const isWordChar = (c: string) => /[A-Za-z0-9_$]/.test(c);
  while (i < s.length) {
    const ch = s[i];
    if (inQ) {
      if (ch === "\\" && inQ !== "`") { i += 2; continue; }
      if (ch === inQ) { if (s[i + 1] === inQ) { i += 2; continue; } inQ = ""; }
      i++; continue;
    }
    if (ch === "'" || ch === '"' || ch === "`") { inQ = ch; i++; word = ""; continue; }
    if (ch === "(") { depth++; i++; word = ""; continue; }
    if (ch === ")") { depth = Math.max(0, depth - 1); i++; word = ""; continue; }
    if (isWordChar(ch)) {
      word += ch; i++;
      if (depth === 0 && word.toLowerCase() === "where") return true;
      continue;
    }
    word = ""; i++;
  }
  return false;
}

interface SqlRisk { level: "read" | "write" | "danger"; word: string; reason: string }

/** 单条语句风险评估：DROP/TRUNCATE、无 WHERE 的 DELETE/UPDATE 视为高危 */
function assessSql(stmt: string): SqlRisk {
  const s = stripSqlLeading(stmt);
  const word = (/^[A-Za-z]+/.exec(s)?.[0] || "").toLowerCase();
  if (!word) return { level: "read", word: "", reason: "" };
  if (["select", "show", "describe", "desc", "explain", "with", "help"].includes(word)) {
    return { level: "read", word, reason: "" };
  }
  if (word === "drop") return { level: "danger", word: "DROP", reason: "DROP 会删除数据库对象且不可恢复" };
  if (word === "truncate") return { level: "danger", word: "TRUNCATE", reason: "TRUNCATE 会清空全表并重置自增值" };
  if (word === "delete") {
    return hasTopWhere(s)
      ? { level: "write", word: "DELETE", reason: "" }
      : { level: "danger", word: "DELETE", reason: "DELETE 未带 WHERE 条件，将删除全表数据" };
  }
  if (word === "update") {
    return hasTopWhere(s)
      ? { level: "write", word: "UPDATE", reason: "" }
      : { level: "danger", word: "UPDATE", reason: "UPDATE 未带 WHERE 条件，将修改全表数据" };
  }
  if (word === "alter" && /\bdrop\b/i.test(s)) {
    return { level: "danger", word: "ALTER", reason: "ALTER ... DROP 会删除列/索引等对象" };
  }
  return { level: "write", word: word.toUpperCase(), reason: "" };
}

/** 危险操作确认弹窗：keyword 非空时必须原文输入后才执行 */
function askDanger(opts: { title: string; desc: string; sql?: string; keyword?: string; okText?: string; action: () => any }) {
  st.danger.visible = true;
  st.danger.title = opts.title;
  st.danger.desc = opts.desc;
  st.danger.sql = opts.sql || "";
  st.danger.keyword = opts.keyword || "";
  st.danger.input = "";
  st.danger.okText = opts.okText || "确认执行";
  st.danger.pending = opts.action;
}
function confirmDanger() {
  const kw = String(st.danger.keyword || "");
  if (kw && String(st.danger.input || "").trim() !== kw) {
    Message.warning(`请输入「${kw}」以确认`);
    return;
  }
  const fn = st.danger.pending;
  st.danger.visible = false;
  st.danger.pending = null;
  if (!fn) return;
  try {
    const r = fn();
    if (r && typeof r.catch === "function") r.catch((e: any) => Message.error(String(e?.message || e)));
  } catch (e: any) { Message.error(String(e?.message || e)); }
}
function cancelDanger() {
  st.danger.pending = null;
  st.danger.input = "";
}

/** SQL 预览用字面量（仅用于确认弹窗展示，不参与执行） */
function qv(v: any): string {
  if (v === null || v === undefined) return "NULL";
  return "'" + String(v).replace(/\\/g, "\\\\").replace(/'/g, "''") + "'";
}

/** 文件大小展示 */
function fmtSize(n: number): string {
  const v = Number(n) || 0;
  if (v < 1024) return v + " B";
  if (v < 1024 * 1024) return (v / 1024).toFixed(1) + " KB";
  return (v / 1024 / 1024).toFixed(1) + " MB";
}

/* ============================ SQL 执行日志（工具级设置） ============================ */

const sqlLogCols = computed<any[]>(() => [
  { title: "文件", dataIndex: "name", ellipsis: true, tooltip: true },
  { title: "大小", dataIndex: "size", width: 90, render: ({ record }: any) => fmtSize(record.size) },
  { title: "更新时间", dataIndex: "modTime", width: 170 },
]);

async function openSqlLog() {
  st.sqlLog.visible = true;
  await refreshSqlLog();
}
async function refreshSqlLog() {
  try {
    const s = await GoTools.MySQLGetSQLLogSettings();
    st.sqlLog.enabled = !!s.enabled;
    st.sqlLog.dir = s.dir || "";
    st.sqlLog.defaultDir = s.defaultDir || "";
    st.sqlLog.resolvedDir = s.resolvedDir || "";
    st.sqlLog.files = s.files || [];
    st.sqlLog.totalSize = s.totalSize || 0;
  } catch (e: any) { Message.error(String(e?.message || e)); }
}

// 自动保存队列：串行执行，避免快速切换开关/目录时旧值覆盖新值
let sqlLogSaveQueue: Promise<void> = Promise.resolve();
/** 保存 SQL 日志设置（参考设置页：修改即保存，不弹成功提示） */
function saveSqlLog(): Promise<void> {
  sqlLogSaveQueue = sqlLogSaveQueue.then(async () => {
    st.sqlLog.saving = true;
    try {
      const dir = await GoTools.MySQLSetSQLLogSettings(!!st.sqlLog.enabled, st.sqlLog.dir || "");
      st.sqlLog.resolvedDir = dir || "";
      await refreshSqlLog();
    } catch (e: any) {
      Message.error(String(e?.message || e));
      await refreshSqlLog();
    } finally {
      st.sqlLog.saving = false;
    }
  });
  return sqlLogSaveQueue;
}
/** 恢复默认目录：清空配置后按默认目录（程序目录下 logs）保存 */
async function resetSqlLogDir() {
  st.sqlLog.dir = "";
  await saveSqlLog();
}
async function chooseSqlLogDir() {
  try {
    const dir = await GoTools.MySQLChooseSQLLogDir();
    if (!dir) return;
    st.sqlLog.dir = dir;
    await saveSqlLog();
  } catch (e: any) { Message.error(String(e?.message || e)); }
}
async function openSqlLogDir() {
  try { await GoTools.MySQLOpenSQLLogDir(); }
  catch (e: any) { Message.error(String(e?.message || e)); }
}
/** 清空日志：需输入确认词 */
function askClearSqlLog() {
  askDanger({
    title: "清空 SQL 日志",
    desc: `将删除日志目录中的 ${st.sqlLog.files.length} 个 SQL 日志文件，此操作不可恢复。`,
    keyword: st.sqlLog.files.length ? "清空日志" : "",
    okText: "清空",
    action: async () => {
      const n = await GoTools.MySQLClearSQLLog();
      Message.success(n > 0 ? `已清空 ${n} 个日志文件` : "没有可清空的日志");
      await refreshSqlLog();
    },
  });
}

/* ============================ 危险操作（对象树右键） ============================ */

/** 清空表：输入表名确认 */
function askClearTable(n: CtxTarget) {
  askDanger({
    title: "清空表数据",
    desc: `将删除表「${n.name}」中的全部数据（保留表结构）。此操作不可撤销，建议先备份。`,
    sql: "DELETE FROM " + qt(n.db, n.name),
    keyword: n.name,
    okText: "清空数据",
    action: () => ctxExecObject(n, "DELETE FROM " + qt(n.db, n.name), "已清空", false),
  });
}

/** 截断表：输入表名确认 */
function askTruncateTable(n: CtxTarget) {
  askDanger({
    title: "截断表",
    desc: `将清空表「${n.name}」并重置自增值，不可回滚。`,
    sql: "TRUNCATE TABLE " + qt(n.db, n.name),
    keyword: n.name,
    okText: "截断",
    action: () => ctxExecObject(n, "TRUNCATE TABLE " + qt(n.db, n.name), "已截断", false),
  });
}

/** 删除表/视图/函数：输入对象名确认 */
function askDropObject(n: CtxTarget) {
  if (n.kind === "view") {
    askDanger({
      title: "删除视图",
      desc: `确定删除视图「${n.name}」？`,
      sql: "DROP VIEW " + qt(n.db, n.name),
      keyword: n.name,
      okText: "删除视图",
      action: () => ctxExecObject(n, "DROP VIEW " + qt(n.db, n.name), "视图已删除", true),
    });
    return;
  }
  if (n.kind === "func") {
    const kind = n.rtype === "PROCEDURE" ? "PROCEDURE" : "FUNCTION";
    const label = kind === "PROCEDURE" ? "存储过程" : "函数";
    askDanger({
      title: `删除${label}`,
      desc: `确定删除${label}「${n.name}」？`,
      sql: `DROP ${kind} ` + qt(n.db, n.name),
      keyword: n.name,
      okText: "删除",
      action: () => ctxExecObject(n, `DROP ${kind} ` + qt(n.db, n.name), label + "已删除", true),
    });
    return;
  }
  askDanger({
    title: "删除表",
    desc: `确定删除表「${n.name}」？表结构与全部数据都会被永久删除。`,
    sql: "DROP TABLE " + qt(n.db, n.name),
    keyword: n.name,
    okText: "删除表",
    action: () => ctxExecObject(n, "DROP TABLE " + qt(n.db, n.name), "表已删除", true),
  });
}

/* ============================ 查询 ============================ */

/** 运行 SQL：先校验，再对高危语句弹出二次确认 */
async function runQuery() {
  if (!st.query.connId) { Message.warning("请先选择连接"); return; }
  const sql = String(st.query.sql || "").trim();
  if (!sql) { Message.warning("请输入 SQL"); return; }
  const stmts = splitSqlStatements(sql);
  if (!stmts.length) { Message.warning("未检测到可执行的 SQL 语句"); return; }
  const risks = stmts.map(assessSql);
  if (risks.some((r) => !r.word)) { Message.warning("存在无法识别的语句，已阻止执行"); return; }
  const dangers = risks.filter((r) => r.level === "danger");
  if (dangers.length) {
    askDanger({
      title: "危险 SQL 确认",
      desc: `检测到 ${dangers.length} 条高风险语句：\n` + dangers.map((r) => `· ${r.word}：${r.reason}`).join("\n"),
      sql,
      keyword: dangers[0].word,
      okText: "继续执行",
      action: () => execQuery(sql),
    });
    return;
  }
  await execQuery(sql);
}
async function execQuery(sql: string) {
  st.query.note = "执行中…";
  try {
    const res = await GoTools.MySQLExecute(st.query.connId, st.query.db, sql);
    st.query.result = res;
    st.query.note = `${res.note} · ${res.elapsed}ms`;
  } catch (e: any) { st.query.note = ""; Message.error(String(e?.message || e)); }
}
function onQueryKey(e: KeyboardEvent) { if (e.ctrlKey && e.key === "Enter") { e.preventDefault(); runQuery(); } }

/* ============================ 备份 ============================ */

async function genBackup() {
  if (!st.backup.connId) { Message.warning("请先选择连接"); return; }
  if (!st.backup.db) { Message.warning("请先选择数据库"); return; }
  st.backup.loading = true;
  try {
    const tables = st.backup.all ? [] : st.backup.tables;
    st.backup.sql = await GoTools.MySQLBackup(st.backup.connId, st.backup.db, tables);
    Message.success("备份已生成");
    // 大库备份耗时较长，用户可能已切到别的页面：再发一条系统原生通知
    sysNotify("MySQL 备份完成", `${st.backup.db}（${st.backup.all ? "全部表" : tables.length + " 张表"}）`);
  } catch (e: any) { Message.error(String(e?.message || e)); }
  st.backup.loading = false;
}
function downloadBackup() {
  const blob = new Blob([st.backup.sql], { type: "text/plain;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url; a.download = `${st.backup.db}_backup_${Date.now()}.sql`;
  document.body.appendChild(a); a.click(); document.body.removeChild(a);
  URL.revokeObjectURL(url);
}

/* ============================ 事务 ============================ */

async function txBegin() {
  if (!st.tx.connId) { Message.warning("请先选择连接"); return; }
  try {
    st.tx.id = await GoTools.MySQLBeginTx(st.tx.connId, st.tx.db);
    st.tx.log.push("BEGIN -> " + st.tx.id);
    st.tx.result = emptyResult();
  } catch (e: any) { Message.error(String(e?.message || e)); }
}
async function txRun() {
  if (!st.tx.id || !st.tx.sql.trim()) return;
  const sql = st.tx.sql.trim();
  const dangers = splitSqlStatements(sql).map(assessSql).filter((r) => r.level === "danger");
  if (dangers.length) {
    askDanger({
      title: "危险 SQL 确认（事务）",
      desc: `事务中检测到 ${dangers.length} 条高风险语句：\n` + dangers.map((r) => `· ${r.word}：${r.reason}`).join("\n"),
      sql,
      keyword: dangers[0].word,
      okText: "继续执行",
      action: () => doTxRun(sql),
    });
    return;
  }
  await doTxRun(sql);
}
async function doTxRun(sql: string) {
  try {
    const res = await GoTools.MySQLTxExecute(st.tx.id, sql);
    st.tx.result = res;
    st.tx.log.push("EXEC " + sql.split("\n")[0].slice(0, 60) + " -> " + res.note);
  } catch (e: any) { st.tx.log.push("ERROR " + (e?.message || e)); Message.error(String(e?.message || e)); }
}
async function txCommit() {
  try { await GoTools.MySQLCommitTx(st.tx.id); st.tx.log.push("COMMIT"); Message.success("已提交"); st.tx.id = ""; }
  catch (e: any) { Message.error(String(e?.message || e)); }
}
async function txRollback() {
  try { await GoTools.MySQLRollbackTx(st.tx.id); st.tx.log.push("ROLLBACK"); Message.success("已回滚"); st.tx.id = ""; }
  catch (e: any) { Message.error(String(e?.message || e)); }
}

/* ============================ 工具 ============================ */

function copyText(t: string) {
  const el = document.createElement("textarea"); el.value = t || ""; document.body.appendChild(el); el.select();
  document.execCommand("copy"); document.body.removeChild(el); Message.success("已复制");
}

onMounted(async () => {
  document.addEventListener("keydown", onCtxKeydown);
  await loadConnections();
  mysqlOrder.value = await loadOrder("mysql:list");
});

onUnmounted(() => {
  document.removeEventListener("keydown", onCtxKeydown);
  stopSrvTimers();
});
</script>

<style scoped>
.mysql-page {
  display: flex;
  height: 100%;
  min-height: 0;
}

.side-col {
  height: 100%;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.side-cols {
  height: 50%;
  width: 100%;
  display: flex;
  flex-direction: column;
}

.left-panel {
  height: calc(100% - 6px);
}

.tree-card {
  flex: 1;
  height: 100%;
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

/* 左栏列表行（连接/实例）对齐 PHP：整行可点、选中高亮、hover 背景 */
.side-card :deep(.arco-list-item) {
  cursor: pointer;
}

.side-card :deep(.arco-list-item.active),
.side-card :deep(.arco-list-item.active:hover) {
  background: var(--color-primary-light-1);
}

.side-card :deep(.arco-list-item:hover) {
  background: var(--color-fill-2);
}

.conn-empty {
  color: var(--color-text-3);
  font-size: 12px;
  padding: 6px 0;
}

.main-col {
  height: 100%;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.main-row {
  height: 100%;
}

.main-row-col {
  height: 100%;
}

.tree-wrap {
  flex: 1;
  min-height: 0;
  overflow: auto;
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

.conn-sub {
  font-size: var(--base-size-small);
  color: var(--color-text-3);
}

.srv-head-actions {
  display: flex;
  align-items: center;
  gap: 2px;
}

.srv-docker-off {
  margin: 6px 8px 0;
}

/* 合并后的左栏列表：行由 common-item-card 提供外壳，这里只留内容样式 */
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
}

.main-panel {
  width: 100%;
  height: 100%;
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  background: var(--color-bg-2);
}

/* ---- 实例总览（对齐 PHP 主区域） ---- */
.srv-panel {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.srv-panel :deep(.arco-card-body) {
  flex: 1;
  min-height: 0;
  overflow: auto;
  display: flex;
  flex-direction: column;
}

/* 实例总览右侧主体：纵向堆叠指标卡 / 快捷条 / 信息行，超高时整块滚动 */
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

/* 实例信息：长文本省略 + tooltip，单列信息行 */
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

.empty-state {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  color: var(--color-text-3);
}

/* 主内容卡片：撑满 main-panel，body 弹性填充供内部表格/内容各自滚动 */
.main-card {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.main-card :deep(.arco-card-body) {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.tool-ic {
  color: var(--color-text-2);
}

.content {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.grid-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: var(--base-margin);
  flex-wrap: wrap;
}

.grid-note {
  color: var(--color-text-3);
  font-size: var(--base-size-small);
}

.grid-warn {
  color: rgb(var(--orange-6));
  font-size: var(--base-size-small);
}

.obj-title {
  font-weight: 600;
}

.data-table {
  width: 100%;
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

/* a-table 内部逐层撑满：spin -> container -> content -> body，表头固定、表体滚动 */
.data-table :deep(.arco-spin),
.read-table :deep(.arco-spin) {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.data-table :deep(.arco-table-container),
.read-table :deep(.arco-table-container) {
  flex: 1;
  min-height: 0;
}

/* 表体占满表头剩余高度，内容超出时才出现滚动条 */
.data-table :deep(.arco-table-body),
.read-table :deep(.arco-table-body) {
  flex: 1;
  min-height: 0;
}

/* 分页条固定在数据面板底部：总数在左（pagination-left 插槽），翻页在右 */
.data-table :deep(.arco-table-pagination) {
  flex: none;
  margin-top: 8px;
}

.pg-total {
  margin-right: auto;
  color: var(--color-text-3);
  font-size: var(--base-size-small);
}

/* 表详情卡（替代 a-tabs）：#title 用 a-radio-group 切换，body 弹性填充供数据表内部滚动 */
.table-card {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.table-card :deep(.arco-card-body) {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.tab-pane {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.tab-pane-data {
  overflow: hidden;
}

.tab-pane-scroll {
  overflow: auto;
}

.data-table :deep(.arco-table-cell) {
  font-size: var(--base-size-small);
}

/* 只读结果表（视图/查询/事务）：撑满内容区并用 a-table 自身滚动（表头固定） */
.read-table {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.cell-txt {
  display: inline-block;
  min-width: 100%;
  cursor: cell;
}

:deep(.nullv) {
  color: var(--color-text-4);
  font-style: italic;
}

.struct-title {
  font-weight: 600;
  margin: var(--base-radius-large) 0 var(--base-radius-default);
  font-size: var(--base-size-small);
}

.ddl-box {
  background: var(--color-fill-1);
  border: 1px solid var(--color-border-2);
  border-radius: var(--base-radius-default);
  padding: var(--base-radius-large);
  font-family: Consolas, monospace;
  font-size: var(--base-size-small);
  white-space: pre-wrap;
  word-break: break-all;
  flex: 1;
  min-height: 0;
  overflow: auto;
}

.tx-log {
  flex: none;
  max-height: 200px;
}

/* ---- 单元格编辑器（双击编辑） ---- */
.cell-editor {
  width: 100%;
  min-width: 120px;
}

/* ---- 列类型下拉 + limit 输入 ---- */
.type-row {
  display: flex;
  gap: 8px;
  align-items: center;
}

.type-sel {
  flex: 1 1 auto;
  min-width: 0;
}

.type-limit {
  flex: 0 0 116px;
  width: 116px;
}

.modal-hint {
  color: var(--color-text-3);
  font-size: var(--base-size-small);
}

/* ---- 对象树标题（右键区） ---- */
:deep(.arco-tree-node-title-text) {
  flex: 1 1 auto;
  min-width: 0;
}

.tree-title {
  display: block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 选中态：主题主色为黑白时默认样式对比不足，改用中性底色 + 正文色 */
.tree-card :deep(.arco-tree-node-selected .arco-tree-node-title),
.tree-card :deep(.arco-tree-node-selected .arco-tree-node-title:hover) {
  color: var(--color-text-1);
  background-color: var(--color-fill-3);
  font-weight: 600;
}

/* ---- 对象树右键菜单 ---- */
.ctx-mask {
  position: fixed;
  inset: 0;
  z-index: 2000;
}

.ctx-menu {
  position: fixed;
  z-index: 2001;
  width: max-content;
  min-width: 150px;
  max-width: 240px;
  max-height: 72vh;
  overflow-y: auto;
  padding: 2px;
  background: var(--color-bg-popup);
  border: 1px solid var(--color-border-2);
  border-radius: var(--base-radius-default);
  box-shadow: 0 6px 18px rgb(0 0 0 / 16%);
}

:deep(.ctx-menu .arco-menu-inner) {
  padding: 2px;
}

.ctx-del {
  color: rgb(var(--danger-6));
}

/* ---- SQL 执行日志弹窗（交互与样式对齐设置页） ---- */
.sql-log-pane :deep(.arco-form-item-content-flex) {
  flex-wrap: wrap;
  row-gap: 2px;
}

.sql-log-pane :deep(.arco-form-item-content-flex) > .hint {
  flex: 0 0 100%;
  width: 100%;
}

.path-row {
  display: flex;
  gap: 8px;
  width: 100%;
}

.path-row :deep(.arco-input-wrapper) {
  flex: 1;
}

.hint {
  display: block;
  margin-top: 4px;
  color: var(--color-text-3);
  font-size: var(--base-size-small);
  line-height: 1.5;
}

.auto-save-tip {
  font-size: var(--base-size-small);
  color: var(--color-text-3);
  white-space: nowrap;
}

.log-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 4px 0 8px;
}

.log-head .struct-title {
  margin: 0 auto 0 0;
}

/* ---- 危险操作确认 ---- */
.danger-desc {
  color: var(--color-text-1);
  white-space: pre-line;
  line-height: 1.7;
  margin-bottom: 10px;
}

.danger-sql {
  flex: none;
  max-height: 200px;
  margin: 0 0 12px;
}

.danger-kw {
  display: flex;
  flex-direction: column;
  gap: 8px;
  color: var(--color-text-2);
}

</style>
