<template>
    <div class="disk-page">
        <!-- ==================== 顶部磁盘总览 ==================== -->
        <disk-overview :drives="drives" :loading="drivesLoading" :active-root="driveRoot" :collapsed="ovCollapsed"
            @refresh="loadDrives" @toggle="ovCollapsed = !ovCollapsed" @jump="enter" />

        <!-- ==================== 文件浏览 ==================== -->
        <a-card class="file-card card-head-tool" size="small" :body-style="{ padding: 0 }">
            <template #title>
                <span class="file-title">文件浏览</span>
                <a-tag v-if="searchHits" color="arcoblue" size="small">搜索结果</a-tag>
            </template>
            <template #extra>
                <a-space :size="6">
                    <a-input v-model="keyword" size="small" class="fb-search" placeholder="按名称搜索，回车开始" allow-clear
                        @press-enter="doSearch" @clear="clearSearch">
                        <template #prefix><icon-search /></template>
                    </a-input>
                    <a-checkbox v-model="includeSub" :disabled="searching || loading">包含子目录</a-checkbox>
                    <a-button size="small" :loading="searching" :disabled="!keyword.trim()" @click="doSearch">
                        搜索
                    </a-button>
                    <a-button v-if="searchHits" size="small" @click="clearSearch">返回目录</a-button>
                </a-space>
            </template>

            <!-- 路径栏：上一级 / 主目录 / 面包屑 / 收藏 / 刷新 -->
            <div class="fb-path">
                <a-tooltip content="返回上一级">
                    <a-button size="small" :disabled="!listing.parent" @click="goUp">
                        <template #icon><icon-arrow-up /></template>
                    </a-button>
                </a-tooltip>
                <a-tooltip content="回到用户主目录">
                    <a-button size="small" @click="goHome">
                        <template #icon><icon-home /></template>
                    </a-button>
                </a-tooltip>
                <a-breadcrumb class="crumbs">
                    <a-breadcrumb-item v-for="(p, i) in listing.parts" :key="p.path">
                        <a-tooltip :content="p.path" :disabled="i === listing.parts.length - 1">
                            <span class="crumb" :class="{ cur: i === listing.parts.length - 1 }" @click="enter(p.path)">
                                {{ p.name }}
                            </span>
                        </a-tooltip>
                    </a-breadcrumb-item>
                </a-breadcrumb>
                <a-input v-model="addrInput" size="small" class="fb-addr" placeholder="粘贴或输入路径，回车跳转"
                    allow-clear @press-enter="enter(addrInput)">
                    <template #prefix><icon-folder /></template>
                </a-input>
                <a-tooltip content="复制当前路径">
                    <a-button size="small" @click="copyCurPath">
                        <template #icon><icon-copy /></template>
                    </a-button>
                </a-tooltip>
                <a-tooltip :content="isFav ? '取消收藏当前目录' : '收藏当前目录'">
                    <a-button size="small" @click="toggleFav">
                        <template #icon>
                            <icon-star-fill v-if="isFav" class="fav-on" />
                            <icon-star v-else />
                        </template>
                    </a-button>
                </a-tooltip>
                <a-tooltip content="刷新当前目录">
                    <a-button size="small" :loading="loading" @click="reload">
                        <template #icon><icon-refresh /></template>
                    </a-button>
                </a-tooltip>
            </div>

            <!-- 工具条 -->
            <div class="fb-bar">
                <a-dropdown trigger="click" @select="onUpload">
                    <a-button size="small" :disabled="busy">
                        <template #icon><icon-upload /></template>上传
                    </a-button>
                    <template #content>
                        <a-doption value="files"><icon-file /> 上传文件（复制进来）</a-doption>
                        <a-doption value="folder"><icon-folder /> 上传文件夹</a-doption>
                    </template>
                </a-dropdown>

                <a-dropdown trigger="click" @select="onCreate">
                    <a-button size="small">
                        <template #icon><icon-folder-add /></template>新建
                    </a-button>
                    <template #content>
                        <a-doption value="dir"><icon-folder-add /> 文件夹</a-doption>
                        <a-doption value="file"><icon-plus /> 空文件</a-doption>
                    </template>
                </a-dropdown>

                <a-tooltip :content="searchHits ? '搜索结果跨目录，批量改名请回到目录浏览' : '单选直接改名；多选按规则（前缀/后缀 + 自增/随机）批量改名'">
                    <a-button size="small" :disabled="!selectedPaths.length || !!searchHits" @click="renameSelected">
                        <template #icon><icon-edit /></template>重命名
                    </a-button>
                </a-tooltip>
                <a-button size="small" :disabled="!selectedPaths.length" @click="copySelected(false)">
                    <template #icon><icon-copy /></template>复制
                </a-button>
                <a-button size="small" :disabled="!selectedPaths.length" @click="copySelected(true)">
                    <template #icon><icon-scissor /></template>剪切
                </a-button>
                <a-tooltip :content="clipboard ? `粘贴 ${clipboard.paths.length} 项到当前目录` : '先复制或剪切要搬动的项目'">
                    <a-button size="small" :disabled="!canPaste" :loading="busy" @click="doPaste">
                        <template #icon><icon-paste /></template>粘贴
                    </a-button>
                </a-tooltip>

                <a-dropdown trigger="click" @select="onDeleteMenu">
                    <a-button size="small" status="danger" :disabled="!selectedPaths.length">
                        <template #icon><icon-delete /></template>删除
                    </a-button>
                    <template #content>
                        <a-doption value="trash"><icon-delete /> 移入回收站</a-doption>
                        <a-doption value="forever"><icon-exclamation-circle /> 永久删除</a-doption>
                    </template>
                </a-dropdown>

                <a-dropdown trigger="click" @select="(v: any) => enter(String(v || ''))">
                    <a-button size="small" :disabled="!favorites.length">
                        <template #icon><icon-star /></template>收藏夹
                    </a-button>
                    <template #content>
                        <a-doption v-for="p in favorites" :key="p" :value="p">
                            <icon-folder /> {{ baseName(p) || p }}
                        </a-doption>
                    </template>
                </a-dropdown>

                <a-dropdown trigger="click" @select="onMore">
                    <a-button size="small"><template #icon><icon-more /></template>更多</a-button>
                    <template #content>
                        <a-doption value="content"><icon-search /> 文件内容搜索</a-doption>
                        <a-doption value="props"><icon-info-circle /> 属性</a-doption>
                        <a-doption value="explorer"><icon-desktop /> 在资源管理器中打开</a-doption>
                        <a-doption value="shell"><icon-code /> 在此打开终端</a-doption>
                        <a-doption v-if="singleSel && !singleSel.isDir" value="docadd"><icon-plus /> 纳入文档中心</a-doption>
                        <a-doption v-if="singleSel && !singleSel.isDir" value="docver"><icon-history /> 文档版本</a-doption>
                        <a-doption v-if="singleSel && !singleSel.isDir" value="docshare"><icon-share-alt /> 局域网分享</a-doption>
                        <a-doption v-if="singleSel && !singleSel.isDir" value="cloudpush"><icon-upload /> 推送到云盘</a-doption>
                        <a-doption v-if="singleSel && !singleSel.isDir" value="docenc"><icon-lock /> 加密</a-doption>
                        <a-doption v-if="singleSel && !singleSel.isDir" value="doctrash"><icon-archive /> 移入文档回收站</a-doption>
                        <a-doption v-if="singleSel && singleSel.isDir" value="bksrc"><icon-folder /> 备份设为源</a-doption>
                        <a-doption v-if="singleSel && singleSel.isDir" value="bkdst"><icon-folder /> 备份设为目标</a-doption>
                        <a-doption v-if="selectedPaths.length" value="compress"><icon-archive /> 压缩为压缩包</a-doption>
                        <a-doption v-if="singleSel && !singleSel.isDir && isArchiveFile(singleSel)" value="extract"><icon-folder /> 解压到同级文件夹</a-doption>
                        <a-doption value="trash"><icon-archive /> 打开回收站</a-doption>
                    </template>
                </a-dropdown>

                <div class="bar-right">
                    <span v-if="clipboard" class="bar-clip">
                        剪贴板：{{ clipboard.mode === "cut" ? "剪切" : "复制" }} {{ clipboard.paths.length }} 项
                        <a-button size="mini" type="text" @click="clipboard = null">清空</a-button>
                    </span>
                    <a-radio-group v-model="view" type="button" size="mini">
                        <a-radio value="list"><icon-list /></a-radio>
                        <a-radio value="grid"><icon-apps /></a-radio>
                    </a-radio-group>
                </div>
            </div>

            <a-alert v-if="errMsg" type="error" class="fb-alert" closable @close="errMsg = ''">
                {{ errMsg }}
            </a-alert>

            <div class="fb-body">
                <!-- 列表视图 -->
                <a-table v-if="view === 'list'" class="fb-table" :data="pagedRows" :loading="loading || searching"
                    :pagination="false" size="small" row-key="path" :bordered="false"
                    :row-selection="{ type: 'checkbox', showCheckedAll: true }" v-model:selected-keys="selectedKeys"
                    :scroll="{ x: 1040, y: '100%' }" @row-dblclick="(record: any) => openEntry(record)"
                    @row-contextmenu="onRowContext">
                    <template #columns>
                        <a-table-column title="文件名称" :width="380">
                            <template #cell="{ record }">
                                <div class="name-cell">
                                    <span class="name-icon" :class="{ dir: record.isDir }">
                                        <common-file-icon :is-dir="record.isDir" :ext="record.ext" :size="18" />
                                    </span>
                                    <div class="name-box">
                                        <a-tooltip :content="record.name">
                                            <span class="name-text" :class="{ dir: record.isDir }"
                                                @click="openEntry(record)">{{ record.name }}</span>
                                        </a-tooltip>
                                        <!-- 搜索结果跨目录：把所在目录带上，不然光看名字找不到地方 -->
                                        <a-tooltip v-if="searchHits" :content="record.path">
                                            <span class="name-path">{{ parentOf(record.path) }}</span>
                                        </a-tooltip>
                                    </div>
                                    <a-tag v-if="record.link" size="small">链接</a-tag>
                                </div>
                            </template>
                        </a-table-column>
                        <a-table-column title="大小" :width="120" align="right">
                            <template #cell="{ record }">
                                <span v-if="!record.isDir" class="cell-size">{{ fmtSize(record.size) }}</span>
                                <a-tooltip :content="dirSizes[record.path] === undefined ? '递归统计整个目录的占用' : '点击重新计算'">
                                    <a-button v-if="dirSizes[record.path] === undefined" size="mini" type="text"
                                        :loading="!!sizeBusy[record.path]" @click.stop="calcSize(record)">
                                        计算
                                    </a-button>
                                    <span v-else class="cell-size is-link" @click.stop="calcSize(record)">
                                        {{ fmtSize(dirSizes[record.path]) }}
                                    </span>
                                </a-tooltip>
                            </template>
                        </a-table-column>
                        <a-table-column title="修改时间" data-index="modTime" :width="180" ellipsis />
                        <a-table-column title="权限" :width="80">
                            <template #cell="{ record }">
                                <a-tooltip content="权限位来自文件模式；Windows 上只有只读/读写之分，仅供参考">
                                    <span class="cell-mode">{{ record.mode || "—" }}</span>
                                </a-tooltip>
                            </template>
                        </a-table-column>
                        <a-table-column title="操作" :width="260" fixed="right" align="right">
                            <template #cell="{ record }">
                                <a-space :size="2">
                                    <a-button size="mini" @click="openEntry(record)">打开</a-button>
                                    <a-button size="mini" @click="openNameModal('rename', record)">改名</a-button>
                                    <a-button size="mini" @click="copyOne(record)">复制</a-button>
                                    <a-dropdown trigger="click" @select="(v: any) => onRowMore(v, record)">
                                        <a-button size="mini"><icon-more /></a-button>
                                        <template #content>
                                            <a-doption value="cut"><icon-scissor /> 剪切</a-doption>
                                            <a-doption v-if="!record.isDir" value="viewer"><icon-file /> 在查看器中打开</a-doption>
                                            <a-doption v-if="!record.isDir" value="system"><icon-launch /> 用系统程序打开</a-doption>
                                            <a-doption v-if="!record.isDir" value="download"><icon-download /> 另存为
                                            </a-doption>
                                            <a-doption value="size"><icon-storage /> 计算大小</a-doption>
                                            <a-doption value="props"><icon-info-circle /> 属性</a-doption>
                                            <a-doption value="explorer"><icon-desktop /> 在资源管理器中显示</a-doption>
                                            <a-doption value="shell"><icon-code /> 在此打开终端</a-doption>
                                            <a-doption v-if="!record.isDir" value="docadd"><icon-plus /> 纳入文档中心</a-doption>
                                            <a-doption v-if="!record.isDir" value="docver"><icon-history /> 文档版本</a-doption>
                                            <a-doption v-if="!record.isDir" value="docshare"><icon-share-alt /> 局域网分享</a-doption>
                                            <a-doption v-if="!record.isDir" value="docenc"><icon-lock /> 加密</a-doption>
                                            <a-doption v-if="!record.isDir" value="doctrash"><icon-archive /> 移入文档回收站</a-doption>
                                            <a-doption v-if="record.isDir" value="bksrc"><icon-folder /> 备份设为源</a-doption>
                                            <a-doption v-if="record.isDir" value="bkdst"><icon-folder /> 备份设为目标</a-doption>
                                            <a-doption value="compress"><icon-archive /> 压缩为压缩包</a-doption>
                                            <a-doption v-if="!record.isDir && isArchiveFile(record)" value="extract"><icon-folder /> 解压到同级文件夹</a-doption>
                                            <a-doption value="delete"><icon-delete /> 删除到回收站</a-doption>
                                            <a-doption value="forever"><icon-exclamation-circle /> 永久删除</a-doption>
                                        </template>
                                    </a-dropdown>
                                </a-space>
                            </template>
                        </a-table-column>
                    </template>
                    <template #empty>
                        <a-empty :image="false" :description="searchHits ? '没有匹配的项目' : '这个目录是空的'" />
                    </template>
                </a-table>

                <!-- 网格视图：与资源管理器的大图标一致，单击选中、双击打开 -->
                <div v-else class="fb-grid">
                    <div v-if="!pagedRows.length" class="grid-empty">
                        <a-empty :image="false" :description="searchHits ? '没有匹配的项目' : '这个目录是空的'" />
                    </div>
                    <template v-else>
                        <div v-for="r in pagedRows" :key="r.path" class="g-item"
                            :class="{ sel: selectedKeys.includes(r.path) }" @click="toggleRow(r)"
                            @dblclick="openEntry(r)" @contextmenu.prevent.stop="onRowContext(r, $event)">
                            <span class="g-icon" :class="{ dir: r.isDir }">
                                <common-file-icon :is-dir="r.isDir" :ext="r.ext" :size="40" />
                            </span>
                            <a-tooltip :content="r.name">
                                <div class="g-name">{{ r.name }}</div>
                            </a-tooltip>
                            <div class="g-meta">{{ r.isDir ? "文件夹" : fmtSize(r.size) }}</div>
                        </div>
                    </template>
                </div>
            </div>

            <!-- 页脚：条数统计 + 分页 -->
            <div class="fb-foot">
                <div class="foot-left">
                    <template v-if="searchHits">
                        <span>找到 {{ searchHits.length }} 项</span>
                        <span class="foot-dim">{{ includeSub ? "含子目录" : "仅当前层" }}</span>
                    </template>
                    <template v-else>
                        <span>共 {{ listing.dirs }} 个目录，{{ listing.files }} 个文件，文件大小 {{ fmtSize(listing.size) }}</span>
                        <a-tooltip v-if="listing.drive"
                            :content="`${listing.drive.root} 可用 ${fmtSize(listing.drive.free)}，共 ${fmtSize(listing.drive.total)}`">
                            <span class="foot-dim">盘符剩余 {{ fmtSize(listing.drive.free) }}</span>
                        </a-tooltip>
                    </template>
                    <a-button v-if="selectedKeys.length" size="mini" type="text" @click="selectedKeys = []">
                        已选 {{ selectedKeys.length }} 项 · 清空
                    </a-button>
                </div>
                <a-pagination v-model:current="page" v-model:page-size="pageSize" :total="sourceRows.length"
                    :page-size-options="[50, 100, 200, 500]" size="mini" show-total show-page-size show-jumper />
            </div>
        </a-card>

        <!-- ==================== 条目右键上下文菜单（列表行 + 网格块共用） ====================
             动作 100% 复用 onRowMore / 工具栏批量函数，不新增未实现能力；
             多选时批量类（复制/剪切/删除/压缩）作用于全部选中，单行类只作用于右键行 -->
        <div v-if="ctx.visible" class="ctx-mask" @click="closeCtx" @contextmenu.prevent="closeCtx" />
        <a-menu v-if="ctx.visible && ctx.row" ref="ctxMenuEl" class="ctx-menu" :style="{ left: ctx.x + 'px', top: ctx.y + 'px' }"
          @menu-item-click="onCtxClick">
          <a-menu-item key="open"><icon-folder v-if="ctx.row.isDir" /><icon-file v-else /> 打开</a-menu-item>
          <a-menu-item key="rename"><icon-edit /> 重命名</a-menu-item>
          <a-menu-item key="copy"><icon-copy /> 复制{{ ctxBatch ? `（${ctxCount} 项）` : "" }}</a-menu-item>
          <a-menu-item key="cut"><icon-scissor /> 剪切{{ ctxBatch ? `（${ctxCount} 项）` : "" }}</a-menu-item>
          <a-menu-item v-if="!ctx.row.isDir" key="viewer"><icon-file /> 在查看器中打开</a-menu-item>
          <a-menu-item v-if="!ctx.row.isDir" key="system"><icon-launch /> 用系统程序打开</a-menu-item>
          <a-menu-item v-if="!ctx.row.isDir" key="download"><icon-download /> 另存为</a-menu-item>
          <a-menu-item key="explorer"><icon-desktop /> 在文件夹中显示</a-menu-item>
          <a-menu-item key="shell"><icon-code /> 在此打开终端</a-menu-item>
          <a-menu-item key="size"><icon-storage /> 计算大小</a-menu-item>
          <a-menu-item key="props"><icon-info-circle /> 属性</a-menu-item>
          <a-menu-item v-if="!ctx.row.isDir" key="docadd"><icon-plus /> 纳入文档中心</a-menu-item>
          <a-menu-item v-if="!ctx.row.isDir" key="docver"><icon-history /> 文档版本</a-menu-item>
          <a-menu-item v-if="!ctx.row.isDir" key="docshare"><icon-share-alt /> 局域网分享</a-menu-item>
          <a-menu-item v-if="!ctx.row.isDir" key="docenc"><icon-lock /> 加密</a-menu-item>
          <a-menu-item v-if="!ctx.row.isDir" key="doctrash"><icon-archive /> 移入文档回收站</a-menu-item>
          <a-menu-item v-if="ctx.row.isDir" key="bksrc"><icon-folder /> 备份设为源</a-menu-item>
          <a-menu-item v-if="ctx.row.isDir" key="bkdst"><icon-folder /> 备份设为目标</a-menu-item>
          <a-menu-item key="compress"><icon-archive /> 压缩为压缩包{{ ctxBatch ? `（${ctxCount} 项）` : "" }}</a-menu-item>
          <a-menu-item v-if="!ctx.row.isDir && isArchiveFile(ctx.row)" key="extract"><icon-folder /> 解压到同级文件夹</a-menu-item>
          <a-menu-item key="delete" class="ctx-del"><icon-delete /> 删除到回收站{{ ctxBatch ? `（${ctxCount} 项）` : "" }}</a-menu-item>
          <a-menu-item key="forever" class="ctx-del"><icon-exclamation-circle /> 永久删除</a-menu-item>
        </a-menu>

        <!-- ==================== 新建 / 重命名的名称输入 ==================== -->
        <a-modal v-model:visible="nameVisible" :title="nameTitle" :mask-closable="false" :ok-text="nameOk"
            cancel-text="取消" :on-before-ok="submitName">
            <a-input v-model="nameInput" :placeholder="nameMode === 'rename' ? '新名称' : '名称'" allow-clear
                @press-enter="submitName" />
            <div class="modal-hint">不能为空，不能包含 \ / : * ? " &lt; &gt; |，也不能以空格或点结尾</div>
        </a-modal>

        <!-- ==================== 批量重命名（多选） ==================== -->
        <common-batch-rename-modal v-model:visible="batchRenVisible" :entries="batchRenEntries"
            :dir="current" :existing-names="(listing.entries || []).map((e: any) => e.name)"
            @done="onBatchRenDone" />

        <!-- ==================== 属性 ==================== -->
        <a-modal v-model:visible="propVisible" :title="`属性：${prop.name || ''}`" :footer="false" width="640px">
            <a-descriptions :column="1" size="small" bordered :label-style="{ width: '110px' }">
                <a-descriptions-item label="名称">{{ prop.name }}</a-descriptions-item>
                <a-descriptions-item label="位置">{{ prop.path }}</a-descriptions-item>
                <a-descriptions-item label="类型">
                    {{ prop.isDir ? "文件夹" : prop.ext ? prop.ext.toUpperCase() + " 文件" : "文件" }}
                </a-descriptions-item>
                <a-descriptions-item label="大小">
                    <a-spin v-if="propSizeBusy" :size="14" />
                    <span v-else>{{ prop.size < 0 ? "统计失败（可能没有权限）" : fmtSize(prop.size) }}</span>
                </a-descriptions-item>
                <a-descriptions-item label="修改时间">{{ prop.modTime || "—" }}</a-descriptions-item>
                <a-descriptions-item label="权限位">{{ prop.mode || "—" }}</a-descriptions-item>
                <a-descriptions-item label="符号链接">{{ prop.link ? "是" : "否" }}</a-descriptions-item>
            </a-descriptions>
            <div class="prop-actions">
                <a-button size="small" @click="openExplorer(prop.path)">在资源管理器中显示</a-button>
                <a-button size="small" @click="openShell(prop.isDir ? prop.path : parentOf(prop.path))">
                    在此打开终端
                </a-button>
            </div>
        </a-modal>

        <!-- ==================== 文件内容搜索 ==================== -->
        <a-modal v-model:visible="contentVisible" :title="`内容搜索：${contentKw}`" :footer="false" width="920px"
            class="content-modal">
            <div class="cm-head">
                在 {{ current }} {{ includeSub ? "及其子目录" : "当前层" }}的文本文件里查找；跳过常见二进制与 2MB
                以上文件，最多返回 200 条。
            </div>
            <a-spin :loading="contentBusy" class="cm-body">
                <a-empty v-if="!contentBusy && !contentHits.length" :image="false" description="没有命中" />
                <a-table v-else :data="contentRows" :pagination="false" size="small" row-key="_k"
                    :scroll="{ y: 420 }">
                    <template #columns>
                        <a-table-column title="文件" :width="220" ellipsis tooltip>
                            <template #cell="{ record }">
                                <span class="name-text">{{ record.name }}</span>
                            </template>
                        </a-table-column>
                        <a-table-column title="行" data-index="line" :width="70" align="right" />
                        <a-table-column title="内容" data-index="text" ellipsis tooltip />
                        <a-table-column title="" :width="120" align="right">
                            <template #cell="{ record }">
                                <a-button size="mini" @click="openExplorer(record.path)">定位文件</a-button>
                            </template>
                        </a-table-column>
                    </template>
                </a-table>
            </a-spin>
        </a-modal>

        <!-- ==================== 文档中心：版本列表（只读，归档在文档中心页） ==================== -->
        <a-modal v-model:visible="docVerModal.open" :title="`版本历史：${docVerModal.name}`" :footer="false"
            :width="560">
            <a-spin :loading="docVerModal.loading" style="display: block">
                <a-empty v-if="!docVerModal.list.length" :image="false"
                    description="暂无版本快照；可先在文档中心条目上存快照" />
                <a-table v-else :data="docVerModal.list" :pagination="false" size="small" row-key="n">
                    <template #columns>
                        <a-table-column title="版本" data-index="n" :width="70" />
                        <a-table-column title="备注" data-index="note" :ellipsis="true" :tooltip="true" />
                        <a-table-column title="大小" :width="100" align="right">
                            <template #cell="{ record }">{{ fmtSize(record.size) }}</template>
                        </a-table-column>
                        <a-table-column title="时间" :width="170">
                            <template #cell="{ record }">{{ fmtTs(record.createdAt) }}</template>
                        </a-table-column>
                    </template>
                </a-table>
                <div class="doc-foot">快照与还原在 <a-link @click="openDocSpace">文档中心</a-link> 页面管理</div>
            </a-spin>
        </a-modal>

        <!-- ==================== 文档中心：加密（仅本机文件，不可逆） ==================== -->
        <a-modal v-model:visible="docEncModal.open" title="加密文件（不可逆）" :ok-loading="docEncModal.saving"
            :mask-closable="false" @ok="submitDocEncrypt">
            <a-alert type="warning" style="margin-bottom: 12px">
                将以口令对「{{ docEncModal.name }}」做 AES-256-GCM 加密封装。<strong>加密成功后原明文会被删除</strong>，
                只保留密文条目；口令不落盘、不可找回，忘记即永久丢失。解锁与取回明文在文档中心「加密库」进行。
            </a-alert>
            <a-form :model="docEncModal" layout="vertical">
                <a-form-item field="pw" label="口令">
                    <a-input-password v-model="docEncModal.pw" placeholder="设置解密口令" />
                </a-form-item>
                <a-form-item field="pw2" label="确认口令">
                    <a-input-password v-model="docEncModal.pw2" placeholder="再次输入口令" />
                </a-form-item>
            </a-form>
        </a-modal>

        <!-- 局域网分享（复用文档中心的 ShareModal，lanshare 临时链接） -->
        <ShareModal v-model:visible="docShareModal.open" :item="docShareModal.item" />

        <!-- 推送到云盘（PR4：磁盘本地文件 → 已授权云账号根目录；上传进度进任务中心） -->
        <a-modal v-model:visible="cloudPush.open" title="推送到云盘" :ok-loading="cloudPush.saving" ok-text="开始上传"
            @ok="doCloudPush">
            <a-form :model="cloudPush" layout="vertical">
                <a-form-item label="本地文件">
                    <a-input :model-value="cloudPush.file?.name" readonly />
                </a-form-item>
                <a-form-item label="目标云盘账号">
                    <a-select v-model="cloudPush.acct">
                        <a-option v-for="o in cloudPush.accounts" :key="o.id" :value="o.id">{{ o.label }}</a-option>
                    </a-select>
                </a-form-item>
                <a-alert type="info">将上传到所选云账号的根目录（进去后可在云盘页移入子目录）。超大文件会整段缓冲，进度看右下角任务中心。</a-alert>
            </a-form>
        </a-modal>

        <!-- 压缩为压缩包（复用 tools/archive 引擎，与压缩工具页/系统右键同一套；进度与取消见任务中心） -->
        <a-modal v-model:visible="cmp.open" title="压缩为压缩包" :ok-loading="cmp.saving" ok-text="开始压缩" @ok="doCompress">
            <a-form :model="cmp" layout="vertical">
                <a-form-item label="格式">
                    <a-radio-group v-model="cmp.format" type="button">
                        <a-radio value="zip">zip（Deflate）</a-radio>
                        <a-radio value="targz">tar.gz</a-radio>
                    </a-radio-group>
                </a-form-item>
                <a-form-item label="压缩级别（1 最快 ~ 9 最小）">
                    <a-input-number v-model="cmp.level" :min="1" :max="9" style="width: 120px" />
                </a-form-item>
                <a-form-item label="输出文件名（不含扩展名）">
                    <a-input v-model="cmp.name" placeholder="留空按所选自动取名" />
                </a-form-item>
                <a-form-item label="输出目录">
                    <a-input v-model="cmp.outDir" placeholder="留空放在当前目录" />
                </a-form-item>
            </a-form>
            <div style="color: var(--color-text-3); font-size: 12px; margin-top: 4px">已选 {{ cmp.count }} 项{{ cmp.bytes ? `，约 ${fmtSize(cmp.bytes)}` : "" }}。进度与取消见右下角任务中心。</div>
        </a-modal>
    </div>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, nextTick, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import DiskOverview from "./components/DiskOverview.vue";
import CommonBatchRenameModal from "./components/BatchRenameModal.vue";
import ShareModal from "@/views/tools/docspace/components/ShareModal.vue";
import { baseName, driveRootOf, fmtSize } from "./helpers";
import { useToolWindow } from "@/views/index/useToolWindow";

/**
 * 磁盘管理（资源管理器）
 *
 * 只做三件事：用总览看各盘用量、用面包屑在目录间走、用工具条搬运文件。
 * 所有读写都走 Go 侧 Disk* 绑定，页面不碰文件系统也不拼路径（拼错了就是删错地方）。
 */

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const { openToolWindow } = useToolWindow();

/** 空目录兜底：模板里就不用到处判空 */
const emptyListing = () => ({
    path: "",
    parent: "",
    parts: [] as any[],
    entries: [] as any[],
    dirs: 0,
    files: 0,
    size: 0,
    drive: null as any,
});

/* ============================ 状态 ============================ */

const drives = ref<any[]>([]);
const drivesLoading = ref(false);
const ovCollapsed = ref(false);

const listing = ref<any>(emptyListing());
const current = ref("");
// 可编辑地址栏：跟着 current 同步，用户改写后回车才跳转（照 Viewer 的 vp-addr）
const addrInput = ref("");
watch(current, (v) => { addrInput.value = v; }, { immediate: true });
const loading = ref(false);
const busy = ref(false); // 上传/粘贴这类耗时操作，期间禁用按钮防连点
const errMsg = ref("");

const view = ref<"list" | "grid">("list");
const page = ref(1);
const pageSize = ref(100);

const keyword = ref("");
const includeSub = ref(false);
const searching = ref(false);
const searchHits = ref<any[] | null>(null); // 非 null = 正在看搜索结果

const selectedKeys = ref<(string | number)[]>([]);
const clipboard = ref<{ mode: "copy" | "cut"; paths: string[] } | null>(null);
const favorites = ref<string[]>([]);

/** 目录大小按需计算：Windows 上读目录大小要递归走一遍，不能每次都算 */
const dirSizes = reactive<Record<string, number>>({});
const sizeBusy = reactive<Record<string, boolean>>({});

/* 新建/改名 */
const nameVisible = ref(false);
const nameMode = ref<"dir" | "file" | "rename">("dir");
const nameInput = ref("");
const renameTarget = ref<any>(null);
const nameTitle = computed(() => ({ dir: "新建文件夹", file: "新建文件", rename: "重命名" })[nameMode.value]);
const nameOk = computed(() => (nameMode.value === "rename" ? "保存" : "创建"));

/* 批量重命名（多选） */
const batchRenVisible = ref(false);
const batchRenEntries = ref<any[]>([]);

/* 属性 */
const propVisible = ref(false);
const prop = ref<any>({});
const propSizeBusy = ref(false);

/* 内容搜索 */
const contentVisible = ref(false);
const contentHits = ref<any[]>([]);
const contentBusy = ref(false);
const contentKw = ref("");
/** 同一个行号可能出现在多个文件里，拼一个稳定 key 给表格（不同行重复会被复用而串行） */
const contentRows = computed(() => contentHits.value.map((h) => ({ ...h, _k: `${h.path}:${h.line}` })));

/* ============================ 派生 ============================ */

const sourceRows = computed<any[]>(() => (searchHits.value ? searchHits.value : listing.value.entries) || []);
const pagedRows = computed(() => {
    const start = (page.value - 1) * pageSize.value;
    return sourceRows.value.slice(start, start + pageSize.value);
});
const selectedPaths = computed<string[]>(() => (selectedKeys.value || []).map(String));
const singleSel = computed<any>(() =>
    selectedPaths.value.length === 1 ? sourceRows.value.find((r) => r.path === selectedPaths.value[0]) || null : null
);
const canPaste = computed(() => !!clipboard.value && !!current.value && !busy.value);
const isFav = computed(() => favorites.value.some((p) => p.toLowerCase() === current.value.toLowerCase()));
const driveRoot = computed(() => driveRootOf(current.value));

/** 目录内容变少（删除、搜索）时把页码拉回有效范围，别停在空白页 */
watch(sourceRows, (rows) => {
    const max = Math.max(1, Math.ceil(rows.length / pageSize.value));
    if (page.value > max) page.value = max;
});

/* ============================ 小工具 ============================ */

const errText = (e: any) => (typeof e === "string" ? e : e?.message || String(e || "未知错误"));

/** "C:\\a\\b.txt" → "C:\\a"（盘符根要保留反斜杠） */
function parentOf(p: string): string {
    const s = String(p || "").replace(/[\\/]+$/, "");
    const i = Math.max(s.lastIndexOf("\\"), s.lastIndexOf("/"));
    if (i < 0) return s;
    const dir = s.slice(0, i);
    return /^[A-Za-z]:$/.test(dir) ? dir + "\\" : dir;
}

/* ============================ 数据 ============================ */

async function loadDrives() {
    drivesLoading.value = true;
    try {
        drives.value = (await GoTools.DiskDrives()) || [];
    } catch (e) {
        errMsg.value = `读取盘符失败：${errText(e)}`;
    } finally {
        drivesLoading.value = false;
    }
}

async function loadFavorites() {
    try {
        favorites.value = (await GoTools.DiskFavorites()) || [];
    } catch {
        // 收藏读不到不影响浏览，静默即可
    }
}

/** 进入目录（空字符串会被忽略，避免误把当前目录清掉） */
async function enter(p: string) {
    const path = String(p || "").trim();
    if (!path) return;
    loading.value = true;
    errMsg.value = "";
    try {
        const res = await GoTools.DiskList(path);
        listing.value = res || emptyListing();
        current.value = res?.path || path;
        searchHits.value = null;
        keyword.value = "";
        selectedKeys.value = [];
        page.value = 1;
    } catch (e) {
        errMsg.value = `打不开 ${path}：${errText(e)}`;
    } finally {
        loading.value = false;
    }
}

/** 刷新：搜索结果里点刷新就按同样条件重搜，否则重读当前目录；顺手更新盘符容量 */
async function reload() {
    const kw = keyword.value.trim();
    if (searchHits.value && kw) await doSearch();
    else await enter(current.value || listing.value.path);
    loadDrives();
}

function goUp() {
    if (listing.value.parent) enter(listing.value.parent);
}

async function goHome() {
    try {
        await enter(await GoTools.DiskHome());
    } catch (e) {
        errMsg.value = `找不到用户主目录：${errText(e)}`;
    }
}

/** 复制当前目录路径到剪贴板：非安全上下文（http）下 navigator.clipboard 可能为空，给兑底提示 */
async function copyCurPath() {
    const p = String(current.value || "");
    if (!p) return;
    try {
        await navigator.clipboard.writeText(p);
        Message.success(`已复制路径：${p}`);
    } catch {
        Message.warning("当前环境不支持自动复制，请从地址栏手动复制");
    }
}

/* ============================ 打开 / 定位 ============================ */

async function openEntry(r: any) {
    if (!r) return;
    if (r.isDir) {
        enter(r.path);
        return;
    }
    // 文件优先用自带的文件查看器打开（单例窗口）；系统默认程序退到行菜单
    openInViewer(r.path);
}

// 在查看器中打开本地文件：桌面开原生单例窗 / Web 走路由；已开着时靠事件导航到新文件
function openInViewer(path: string) {
    openToolWindow(`/tools/viewer?path=${encodeURIComponent(path)}`, "文件查看器", { single: true });
    try { Events.Emit("viewer:open-path", { path }); } catch { /* 非 Wails 环境忽略 */ }
}

async function openWithSystem(path: string) {
    try {
        await GoTools.FileOpenSystem(path);
    } catch (e) {
        Message.error(`用系统程序打开失败：${errText(e)}`);
    }
}

async function openExplorer(p?: string) {
    const target = String(p || current.value || "").trim();
    if (!target) return;
    try {
        await GoTools.DiskOpenInExplorer(target);
    } catch (e) {
        Message.error(`打开资源管理器失败：${errText(e)}`);
    }
}

async function openShell(p?: string) {
    const target = String(p || current.value || "").trim();
    if (!target) return;
    try {
        await GoTools.DiskOpenShell(target);
    } catch (e) {
        Message.error(`打开终端失败：${errText(e)}`);
    }
}

async function openTrash() {
    try {
        await GoTools.DiskOpenTrash();
    } catch (e) {
        Message.error(`打开回收站失败：${errText(e)}`);
    }
}

/* ============================ 新建 / 改名 ============================ */

function openNameModal(mode: "dir" | "file" | "rename", target?: any) {
    nameMode.value = mode;
    renameTarget.value = target || null;
    nameInput.value = mode === "rename" ? target?.name || "" : "";
    nameVisible.value = true;
}

function renameSelected() {
    // 多选（≥2）：走规则化批量改名；单选：保留原来的直接改名
    if (selectedPaths.value.length >= 2) {
        const set = new Set(selectedPaths.value);
        batchRenEntries.value = sourceRows.value.filter((r) => set.has(r.path));
        batchRenVisible.value = true;
        return;
    }
    if (!singleSel.value) {
        Message.warning("请先选中要重命名的项目");
        return;
    }
    openNameModal("rename", singleSel.value);
}

/** 批量改名完成：清选中并刷新（改名后原路径已失效） */
async function onBatchRenDone() {
    selectedKeys.value = [];
    await reload();
}

/** 返回 false 时不关弹窗，错误信息留在 Message 里，用户可以改个名字再试 */
async function submitName(): Promise<boolean> {
    const name = nameInput.value.trim();
    if (!name) {
        Message.warning("请输入名称");
        return false;
    }
    try {
        if (nameMode.value === "dir") {
            await GoTools.DiskMkdir(current.value, name);
        } else if (nameMode.value === "file") {
            await GoTools.DiskNewFile(current.value, name);
        } else {
            await GoTools.DiskRename(renameTarget.value?.path, name);
        }
        Message.success(nameMode.value === "rename" ? "已重命名" : "已创建");
        // 回车与点「确定」走同一条路：这里主动关一下，回车也能收起弹窗
        nameVisible.value = false;
        await reload();
        return true;
    } catch (e) {
        Message.error(errText(e));
        return false;
    }
}

/* ============================ 复制 / 剪切 / 粘贴 / 上传 ============================ */

function copySelected(cut: boolean) {
    const paths = selectedPaths.value;
    if (!paths.length) {
        Message.warning("请先选中要处理的项目");
        return;
    }
    clipboard.value = { mode: cut ? "cut" : "copy", paths: [...paths] };
    Message.success(`已${cut ? "剪切" : "复制"} ${paths.length} 项，切到目标目录后点「粘贴」`);
}

function copyOne(r: any) {
    clipboard.value = { mode: "copy", paths: [r.path] };
    Message.success(`已复制「${r.name}」，切到目标目录后点「粘贴」`);
}

async function doPaste() {
    const cb = clipboard.value;
    if (!cb || !current.value) return;
    busy.value = true;
    try {
        const n =
            cb.mode === "cut"
                ? await GoTools.DiskMove(cb.paths, current.value)
                : await GoTools.DiskCopy(cb.paths, current.value);
        Message.success(`已${cb.mode === "cut" ? "移动" : "复制"} ${n} 项到当前目录`);
        if (cb.mode === "cut") clipboard.value = null;
        await reload();
    } catch (e) {
        errMsg.value = `${cb.mode === "cut" ? "移动" : "复制"}失败：${errText(e)}`;
    } finally {
        busy.value = false;
    }
}

async function doUpload(kind: "files" | "folder") {
    if (!current.value) return;
    busy.value = true;
    try {
        if (kind === "files") {
            const picked = await GoTools.DiskUpload(current.value);
            if (picked?.length) {
                Message.success(`已复制 ${picked.length} 个文件到当前目录`);
                await reload();
            }
        } else {
            const picked = await GoTools.DiskUploadFolder(current.value);
            if (picked) {
                Message.success(`已复制文件夹「${baseName(picked)}」到当前目录`);
                await reload();
            }
        }
    } catch (e) {
        errMsg.value = `上传失败：${errText(e)}`;
    } finally {
        busy.value = false;
    }
}

async function downloadOne(r: any) {
    if (!r || r.isDir) {
        Message.warning("请选中一个文件（文件夹请用「复制」搬到目标目录）");
        return;
    }
    try {
        const saved = await GoTools.DiskDownload(r.path);
        if (saved) Message.success(`已另存为 ${saved}`);
    } catch (e) {
        Message.error(errText(e));
    }
}

/* ============================ 删除 ============================ */

function doDelete(permanent: boolean, paths?: string[]) {
    const list = paths && paths.length ? paths : selectedPaths.value;
    if (!list.length) {
        Message.warning("请先选中要删除的项目");
        return;
    }
    const names = list.map(baseName);
    const brief = names.slice(0, 5).join("、") + (names.length > 5 ? ` 等 ${names.length} 项` : "");
    Modal.warning({
        title: permanent ? "永久删除" : "删除到回收站",
        content: permanent
            ? `将直接删除 ${brief}；不进回收站，无法恢复。`
            : `将把 ${brief} 移入系统回收站，需要时可以还原。`,
        okText: permanent ? "永久删除" : "移入回收站",
        hideCancel: false,
        onOk: async () => {
            try {
                const n = await GoTools.DiskDelete(list, permanent);
                Message.success(`已删除 ${n} 项`);
                selectedKeys.value = [];
                await reload();
            } catch (e) {
                errMsg.value = `删除失败：${errText(e)}`;
            }
        },
    });
}

/* ============================ 搜索 ============================ */

async function doSearch() {
    const kw = keyword.value.trim();
    if (!kw) {
        clearSearch();
        return;
    }
    searching.value = true;
    errMsg.value = "";
    try {
        const hits = (await GoTools.DiskSearch(current.value, kw, includeSub.value)) || [];
        searchHits.value = hits;
        page.value = 1;
        selectedKeys.value = [];
        if (!hits.length) Message.info(`当前目录下没有匹配「${kw}」的项目`);
    } catch (e) {
        errMsg.value = `搜索失败：${errText(e)}`;
    } finally {
        searching.value = false;
    }
}

function clearSearch() {
    searchHits.value = null;
    keyword.value = "";
    page.value = 1;
}

async function doSearchContent() {
    const kw = keyword.value.trim();
    if (!kw) {
        Message.warning("先在右上角输入要搜索的关键字，再点「文件内容搜索」");
        return;
    }
    contentKw.value = kw;
    contentHits.value = [];
    contentVisible.value = true;
    contentBusy.value = true;
    try {
        contentHits.value = (await GoTools.DiskSearchContent(current.value, kw, includeSub.value)) || [];
    } catch (e) {
        Message.error(`内容搜索失败：${errText(e)}`);
    } finally {
        contentBusy.value = false;
    }
}

/* ============================ 大小 / 属性 ============================ */

async function calcSize(r: any) {
    if (!r || sizeBusy[r.path]) return;
    sizeBusy[r.path] = true;
    try {
        dirSizes[r.path] = await GoTools.DiskDirSize(r.path);
    } catch (e) {
        Message.error(`统计大小失败：${errText(e)}`);
    } finally {
        sizeBusy[r.path] = false;
    }
}

async function showProps(r: any) {
    if (!r) {
        Message.warning("请先选中一个项目");
        return;
    }
    prop.value = { ...r };
    propVisible.value = true;
    if (!r.isDir) return;
    // 目录大小要递归遍历，打开属性后再补上，别让弹窗空着等
    propSizeBusy.value = true;
    try {
        prop.value.size = await GoTools.DiskDirSize(r.path);
    } catch {
        prop.value.size = -1;
    } finally {
        propSizeBusy.value = false;
    }
}

/* ============================ 收藏 ============================ */

async function saveFavorites() {
    try {
        await GoTools.DiskSaveFavorites([...favorites.value]);
    } catch (e) {
        errMsg.value = `保存收藏失败：${errText(e)}`;
    }
}

async function toggleFav() {
    const cur = current.value;
    if (!cur) return;
    const i = favorites.value.findIndex((p) => p.toLowerCase() === cur.toLowerCase());
    if (i >= 0) favorites.value.splice(i, 1);
    else favorites.value.push(cur);
    Message.success(i >= 0 ? "已取消收藏" : "已收藏当前目录");
    await saveFavorites();
}

/* ============================ 工具条回调 ============================ */

function onUpload(v: any) {
    doUpload(v === "folder" ? "folder" : "files");
}

function onCreate(v: any) {
    openNameModal(v === "file" ? "file" : "dir");
}

function onDeleteMenu(v: any) {
    doDelete(v === "forever");
}

function onMore(v: any) {
    if (v === "content") doSearchContent();
    else if (v === "props") showProps(singleSel.value);
    else if (v === "explorer") openExplorer();
    else if (v === "shell") openShell();
    else if (v === "trash") openTrash();
    else if (v === "docadd") docAdd(singleSel.value);
    else if (v === "docver") docVersions(singleSel.value);
    else if (v === "docshare") docShare(singleSel.value);
    else if (v === "cloudpush") openCloudPush(singleSel.value);
    else if (v === "docenc") docEncrypt(singleSel.value);
    else if (v === "doctrash") docTrash(singleSel.value);
    else if (v === "bksrc") docBackupPick(singleSel.value, "src");
    else if (v === "bkdst") docBackupPick(singleSel.value, "dst");
    else if (v === "compress") openCompress();
    else if (v === "extract") doExtract(singleSel.value);
}

/* ---------- 推送到云盘（PR4 与云盘页联动） ---------- */
const cloudPush = reactive({
    open: false, saving: false, file: null as any, acct: "", accounts: [] as any[],
});
async function openCloudPush(item: any) {
    if (!item || item.isDir) { Message.warning("只能推送单个文件到云盘"); return; }
    if (!GoTools?.CloudSettings || !GoTools?.CloudListAccounts) { Message.warning("当前环境不支持云盘"); return; }
    try {
        const s = await GoTools.CloudSettings();
        if (!s?.enabled) { Message.warning("云盘功能未启用，请到 设置 → 云盘管理 打开"); return; }
        const list = ((await GoTools.CloudListAccounts()) ?? []).filter((a: any) => a.hasToken);
        if (!list.length) { Message.warning("没有已授权的云盘账号，请先到云盘页添加并授权"); return; }
        Object.assign(cloudPush, { open: true, saving: false, file: item, accounts: list, acct: list[0].id });
    } catch (e: any) {
        Message.error(`读取云盘账号失败：${String(e?.message || e || e)}`);
    }
}
async function doCloudPush() {
    if (!cloudPush.file || !cloudPush.acct) return;
    cloudPush.saving = true;
    try {
        await GoTools.CloudUpload(cloudPush.acct, "", cloudPush.file.name, cloudPush.file.path);
        Message.success(`已推送到云盘：${cloudPush.file.name}（进度见任务中心）`);
        cloudPush.open = false;
    } catch (e: any) {
        Message.error(`上传失败：${String(e?.message || e || e)}`);
    } finally { cloudPush.saving = false; }
}

function onRowMore(v: any, r: any) {
    if (v === "cut") {
        clipboard.value = { mode: "cut", paths: [r.path] };
        Message.success(`已剪切「${r.name}」，切到目标目录后点「粘贴」`);
    } else if (v === "download") downloadOne(r);
    else if (v === "viewer") openInViewer(r.path);
    else if (v === "system") openWithSystem(r.path);
    else if (v === "size") calcSize(r);
    else if (v === "props") showProps(r);
    else if (v === "explorer") openExplorer(r.path);
    else if (v === "shell") openShell(r.isDir ? r.path : parentOf(r.path));
    else if (v === "delete") doDelete(false, [r.path]);
    else if (v === "forever") doDelete(true, [r.path]);
    else if (v === "docadd") docAdd(r);
    else if (v === "docver") docVersions(r);
    else if (v === "docshare") docShare(r);
    else if (v === "docenc") docEncrypt(r);
    else if (v === "doctrash") docTrash(r);
    else if (v === "bksrc") docBackupPick(r, "src");
    else if (v === "bkdst") docBackupPick(r, "dst");
    else if (v === "compress") openCompress([r.path]);
    else if (v === "extract") doExtract(r);
}

/* ============================ 条目右键上下文菜单 ============================ */
const ctx = reactive<{ visible: boolean; x: number; y: number; row: any }>({ visible: false, x: 0, y: 0, row: null });
const ctxMenuEl = ref<any>(null);
/** 右键行是否处于多选集合内：是则复制/剪切/删除/压缩等批量类作用于全部选中项 */
const ctxBatch = computed(() => !!ctx.row && selectedKeys.value.length > 1 && selectedPaths.value.includes(String(ctx.row.path)));
const ctxCount = computed(() => selectedPaths.value.length);

/** 列表 @row-contextmenu(record, ev) 与网格 @contextmenu(r, $event) 共用（Arco 把 ev 声明为 Event，这里用 any 接住 MouseEvent） */
function onRowContext(record: any, ev: any) {
    if (!record) return;
    ev.preventDefault();
    const key = String(record.path);
    // 未选中的行：单选它；已多选中的行：保留整个多选集，让批量动作自然命中全部
    if (!selectedKeys.value.includes(key)) selectedKeys.value = [key];
    ctx.row = record;
    void showCtxMenu(ev.clientX, ev.clientY);
}

/** 先按点击位置显示，再 nextTick 量真实尺寸夹回视口（照 portman 口径，不靠估计高度） */
async function showCtxMenu(x: number, y: number) {
    ctx.x = x; ctx.y = y; ctx.visible = true;
    await nextTick();
    const el = ctxMenuEl.value?.$el || ctxMenuEl.value;
    if (!el) return;
    ctx.x = Math.max(4, Math.min(x, window.innerWidth - el.offsetWidth - 4));
    ctx.y = Math.max(4, Math.min(y, window.innerHeight - el.offsetHeight - 4));
}

function closeCtx() { ctx.visible = false; }

function onCtxKeydown(e: KeyboardEvent) {
    if (e.key === "Escape" && ctx.visible) ctx.visible = false;
}

/** 菜单项点击：批量类走 selectedPaths，单行类走 ctx.row，其余全部委托既有 onRowMore */
function onCtxClick(key: string) {
    const row = ctx.row;
    ctx.visible = false;
    if (!row) return;
    if (key === "open") { openEntry(row); return; }
    if (key === "rename") { openNameModal("rename", row); return; }
    if (key === "copy") { copySelected(false); return; }
    if (key === "cut") { copySelected(true); return; }
    if (key === "compress") { openCompress(); return; }
    if (key === "delete") { doDelete(false); return; }
    if (key === "forever") { doDelete(true); return; }
    onRowMore(key, row);
}

/* ---------- 压缩 / 解压（复用 tools/archive 引擎，与压缩工具页/系统右键同一套；进度走 archive:progress 进任务中心） ---------- */
const ARCHIVE_EXTS = [".zip", ".tar.gz", ".tgz"]; // 引擎 FormatFor 只认这三种
function isArchiveFile(r: any): boolean {
    if (!r || r.isDir) return false;
    const low = String(r.name || "").toLowerCase();
    return ARCHIVE_EXTS.some((e) => low.endsWith(e));
}
const cmp = reactive({ open: false, saving: false, format: "zip", level: 6, name: "", outDir: "", paths: [] as string[], count: 0, bytes: 0 });
function buildCmpOptions() {
    return { op: "compress", paths: cmp.paths, format: cmp.format, level: cmp.level, outDir: cmp.outDir.trim(), name: cmp.name.trim() };
}
async function openCompress(explicit?: string[]) {
    const paths = explicit && explicit.length ? explicit : selectedPaths.value;
    if (!paths.length) { Message.warning("先勾选要压缩的文件/目录"); return; }
    Object.assign(cmp, { open: true, saving: false, format: "zip", level: 6, name: "", outDir: current.value, paths, count: paths.length, bytes: 0 });
    try { const pf = await GoTools?.ArchiveScan(paths); cmp.bytes = pf?.bytes || 0; } catch { /* 预扫描失败只是少个提示 */ }
    try {
        const out = await GoTools?.ArchiveSuggestName(buildCmpOptions());
        cmp.name = out ? baseName(out).replace(/\.(zip|tar\.gz|tgz)$/i, "") : "";
    } catch { /* 取不到建议名则留空由引擎自动取名 */ }
}
async function doCompress() {
    cmp.saving = true;
    try {
        await GoTools?.ArchiveCompress(buildCmpOptions());
        Message.success(`已开始压缩 ${cmp.count} 项（进度见任务中心）`);
        cmp.open = false;
    } catch (e: any) {
        Message.error(`压缩失败：${String(e?.message || e)}`);
    } finally { cmp.saving = false; }
}
async function doExtract(r: any) {
    if (!isArchiveFile(r)) { Message.warning("只能解压 zip / tar.gz / tgz"); return; }
    try {
        await GoTools?.ArchiveExtract({ op: "extract", paths: [r.path], format: "", level: 0, outDir: "", name: "" });
        Message.success(`已开始解压「${r.name}」到同级文件夹（进度见任务中心）`);
    } catch (e: any) {
        Message.error(`解压失败：${String(e?.message || e)}`);
    }
}

/* ============================ 文档中心入口（计划 §九） ============================ */

/** 已纳管条目的最小形状（只取这里用得上的字段，与 docspace.Item 对齐） */
type ManagedItem = { id: string; src: string; path: string; name: string };

/** 路径归一：分隔符统一成 / 再比大小写，Windows 不区分大小写 */
const pathKey = (p: string) => String(p || "").replace(/[\\/]+/g, "/").toLowerCase();

const fmtTs = (ms: number) => (ms ? new Date(ms).toLocaleString() : "--");

/** 按路径找已纳管条目；未纳管返回 null */
async function findManaged(path: string): Promise<ManagedItem | null> {
    try {
        const list = (await GoTools?.DocSpaceItems()) as ManagedItem[] | null;
        const key = pathKey(path);
        return (list || []).find((it) => (it.src || "local") === "local" && pathKey(it.path) === key) || null;
    } catch {
        return null;
    }
}

async function docAdd(r: any) {
    try {
        await GoTools?.DocSpaceAddItem("local", "", r.path, "");
        Message.success(`已纳管「${r.name}」，可在文档中心查看`);
    } catch (e) {
        Message.error(`纳管失败：${errText(e)}`);
    }
}

const docVerModal = reactive<{ open: boolean; loading: boolean; name: string; list: any[] }>({
    open: false,
    loading: false,
    name: "",
    list: [],
});
async function docVersions(r: any) {
    docVerModal.name = r.name;
    docVerModal.list = [];
    docVerModal.open = true;
    docVerModal.loading = true;
    try {
        const it = await findManaged(r.path);
        if (!it) {
            Message.info(`「${r.name}」尚未纳管，先「纳入文档中心」再存快照`);
            return;
        }
        docVerModal.list = ((await GoTools?.DocSpaceVersions(it.id)) as any[]) || [];
    } catch (e) {
        Message.error(`读取版本失败：${errText(e)}`);
    } finally {
        docVerModal.loading = false;
    }
}
function openDocSpace() {
    openToolWindow("/tools/docspace", "文档中心");
}

// 备份目标互挂（计划 §九 第三项）：把选中目录递给文档中心的备份抽屉预填。
// 双通道：query 负责「窗口还没开过」（新建时 URL 带入自动开抽屉），
// 事件负责「窗口已开」（query 不会重新触发 mounted）；两者都发，消费侧自洽。
function docBackupPick(r: any, mode: "src" | "dst") {
    if (!r?.isDir) { Message.warning("备份的源/目标都必须是目录"); return; }
    const q = mode === "src" ? "bkSrc" : "bkDst";
    openToolWindow(`/tools/docspace?${q}=${encodeURIComponent(r.path)}`, "文档中心");
    try { Events.Emit(`docspace:backup-${mode}`, { path: r.path }); } catch { /* 非 Wails 环境忽略 */ }
}

function docTrash(r: any) {
    Modal.warning({
        title: "移入文档回收站",
        content: `将把「${r.name}」的实体移入文档中心回收站（未纳管会先自动纳管），之后可在文档中心页的回收站还原。确认？`,
        okText: "移入",
        hideCancel: false,
        onOk: async () => {
            try {
                let it = await findManaged(r.path);
                if (!it) it = ((await GoTools?.DocSpaceAddItem("local", "", r.path, "")) as ManagedItem) || null;
                if (!it) {
                    Message.error("纳管失败，未移入");
                    return;
                }
                await GoTools?.DocSpaceMoveTrash(it.id);
                Message.success("已移入文档回收站，可在文档中心页还原");
                selectedKeys.value = [];
                await reload();
            } catch (e) {
                Message.error(`移入文档回收站失败：${errText(e)}`);
            }
        },
    });
}

const docShareModal = reactive<{ open: boolean; item: { src: string; path: string; name: string } | null }>({
    open: false,
    item: null,
});
function docShare(r: any) {
    if (!r || r.isDir) { Message.warning("当前只支持分享文件，目录需打包后分享"); return; }
    // 磁盘条目没有文档中心 id，ShareModal 只读 src/path/name 三个字段
    docShareModal.item = { src: "local", path: r.path, name: r.name };
    docShareModal.open = true;
}

const docEncModal = reactive<{ open: boolean; saving: boolean; path: string; name: string; pw: string; pw2: string }>({
    open: false,
    saving: false,
    path: "",
    name: "",
    pw: "",
    pw2: "",
});
function docEncrypt(r: any) {
    docEncModal.path = r.path;
    docEncModal.name = r.name;
    docEncModal.pw = "";
    docEncModal.pw2 = "";
    docEncModal.open = true;
}
async function submitDocEncrypt() {
    if (!docEncModal.pw) {
        Message.warning("口令不能为空");
        return;
    }
    if (docEncModal.pw !== docEncModal.pw2) {
        Message.warning("两次口令不一致");
        return;
    }
    docEncModal.saving = true;
    try {
        await GoTools?.SecStoreEncrypt(docEncModal.path, docEncModal.pw);
        Message.success("已加密，原明文已删除；可在文档中心「加密库」查看");
        docEncModal.open = false;
        await reload();
    } catch (e) {
        Message.error(`加密失败：${errText(e)}`);
    } finally {
        docEncModal.saving = false;
    }
}

/** 网格视图没有复选框，单击切换选中 */
function toggleRow(r: any) {
    const i = selectedKeys.value.indexOf(r.path);
    if (i >= 0) selectedKeys.value.splice(i, 1);
    else selectedKeys.value.push(r.path);
}

/* ============================ 初始化 ============================ */

onMounted(async () => {
    await loadDrives();
    loadFavorites();
    try {
        await enter(await GoTools.DiskHome());
    } catch (e) {
        errMsg.value = `找不到默认目录，请从上方盘符点进去：${errText(e)}`;
    }
});

// 右键菜单 Esc 关闭：遮罩只接鼠标，键盘关掉是 portman/procman 已有的习惯
onMounted(() => window.addEventListener("keydown", onCtxKeydown));
onUnmounted(() => window.removeEventListener("keydown", onCtxKeydown));
</script>

<style scoped>
.disk-page {
    height: 100%;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
}

/* 文件卡吃掉剩余高度：三段（路径/工具条/列表）+ 页脚，只有列表区滚动 */
.file-card {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

.file-card :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

.file-card :deep(.arco-card-header) {
    border-bottom: 1px solid var(--color-border-1);
    align-items: center;
}

.file-card :deep(.arco-card-header-title) {
    flex: none;
    margin-right: 12px;
}

.file-card :deep(.arco-card-header-extra) {
    flex: 1 1 auto;
    min-width: 0;
    justify-content: flex-end;
}

.file-title {
    font-size: var(--base-size-default);
    font-weight: 600;
}

.fb-search {
    width: 220px;
}

/* 路径栏 */
.fb-path {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 12px;
    border-bottom: 1px solid var(--color-border-1);
    min-height: 0;
}

.crumbs {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
}

.crumb {
    cursor: pointer;
    color: var(--color-text-2);
}

.crumb:hover {
    color: rgb(var(--primary-6));
}

.crumb.cur {
    cursor: default;
    color: var(--color-text-1);
    font-weight: 600;
}

.crumb.cur:hover {
    color: var(--color-text-1);
}

.fav-on {
    color: rgb(var(--orange-6));
}

/* 工具条 */
.fb-bar {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
    padding: 8px 12px;
    border-bottom: 1px solid var(--color-border-1);
}

.bar-right {
    margin-left: auto;
    display: flex;
    align-items: center;
    gap: 6px;
}

.bar-clip {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}

.fb-alert {
    margin: 8px 12px 0;
}

/* 列表区：只剩这一块滚动 */
.fb-body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: 0 12px;
}

.fb-body :deep(.arco-table-container) {
    flex: 1;
}

.fb-table {
    flex: 1;
    min-height: 0;
}

/* 名称列：图标 + 名字 + 可选副标题；宽度由列宽控住，长名省略号 */
.name-cell {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
}

.name-icon {
    flex: none;
    display: flex;
    align-items: center;
}

.name-box {
    flex: 1 1 auto;
    min-width: 0;
    display: flex;
    flex-direction: column;
}

.name-text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--color-text-1);
}

.name-text.dir {
    cursor: pointer;
    font-weight: 500;
}

.name-text.dir:hover {
    color: rgb(var(--primary-6));
}

.name-path {
    font-size: var(--base-size-small);
    color: var(--color-text-4);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.cell-size {
    font-variant-numeric: tabular-nums;
    color: var(--color-text-2);
}

.cell-size.is-link {
    cursor: pointer;
    color: rgb(var(--primary-6));
}

.cell-mode {
    font-family: var(--code-font-family);
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}

/* 网格视图 */
.fb-grid {
    flex: 1;
    min-height: 0;
    overflow: auto;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(112px, 1fr));
    gap: 8px;
    align-content: start;
    padding: 10px 0;
}

.grid-empty {
    grid-column: 1 / -1;
    padding: 32px 0;
    display: flex;
    justify-content: center;
}

.g-item {
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
    padding: 10px 8px;
    border: 1px solid transparent;
    border-radius: var(--base-radius-default);
    cursor: pointer;
    user-select: none;
    transition: background-color 0.15s, color 0.15s;
}

.g-item:hover {
    background: var(--color-fill-2);
}

/* 选中：中性淡底 + 左上短竖条，不用整块主色边框，与列表 / 树节点一套语言 */
.g-item.sel {
    background: var(--color-fill-3);
}

.g-item.sel::before {
    content: "";
    position: absolute;
    left: 4px;
    top: 8px;
    width: 3px;
    height: 16px;
    border-radius: var(--base-radius-small);
    background: rgb(var(--primary-6));
}

.g-icon {
    display: flex;
}

.g-name {
    width: 100%;
    text-align: center;
    font-size: var(--base-size-small);
    color: var(--color-text-1);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.g-meta {
    font-size: var(--base-size-small);
    color: var(--color-text-4);
    font-variant-numeric: tabular-nums;
}

/* 页脚 */
.fb-foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    flex-wrap: wrap;
    padding: 6px 12px;
    border-top: 1px solid var(--color-border-1);
}

.foot-left {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    min-width: 0;
}

.foot-dim {
    color: var(--color-text-4);
}

/* 弹窗 */
.modal-hint {
    margin-top: 8px;
    font-size: var(--base-size-small);
    color: var(--color-text-4);
}

.prop-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 12px;
}

/* 版本弹窗底部：去文档中心的链接行 */
.doc-foot {
    margin-top: 8px;
    color: var(--color-text-3);
    font-size: 12px;
}

.cm-head {
    margin-bottom: 10px;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}

.cm-body {
    display: block;
}

/* 地址栏：不抢面包屑的剩余宽度，窗口窄时可收缩 */
.fb-addr {
    width: 240px;
    flex: 0 1 240px;
    min-width: 120px;
}

/* 条目右键菜单：照 docspace/portman 口径（fixed 遮罩 + fixed 定位 a-menu） */
.ctx-mask {
    position: fixed;
    inset: 0;
    z-index: 2000;
}
.ctx-menu {
    position: fixed;
    z-index: 2001;
    width: max-content;
    min-width: 168px;
    max-width: 240px;
    max-height: 72vh;
    overflow-y: auto;
    padding: 2px;
    background: var(--color-bg-popup);
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-default);
    box-shadow: 0 6px 18px rgb(0 0 0 / 16%);
}
.ctx-menu :deep(.arco-menu) {
    padding: 2px;
}
.ctx-menu :deep(.arco-menu-item.ctx-del) {
    color: rgb(var(--danger-6));
}

/* 列表选中态：不再整行刷主色，改中性淡底 + 名称列左侧短竖条，
   与 Git 仓库项、docspace 树节点共用一套选中语言 */
.fb-table :deep(.arco-table-tr-selected .arco-table-td) {
    background: var(--color-fill-3);
}
.name-cell {
    position: relative;
    padding-left: 8px;
}
.fb-table :deep(.arco-table-tr-selected .name-cell::before) {
    content: "";
    position: absolute;
    left: 0;
    top: 50%;
    transform: translateY(-50%);
    width: 3px;
    height: 15px;
    border-radius: var(--base-radius-small);
    background: rgb(var(--primary-6));
}
</style>
