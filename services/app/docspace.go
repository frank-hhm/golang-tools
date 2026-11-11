package app

import (
	"fmt"

	"Tools/services/applog"
	"Tools/services/config"
	"Tools/tools/docspace"
	"Tools/tools/filekind"
	"Tools/tools/fileview"
)

/*
文档中心（docspace）绑定层

P0 只做地基：分类树与条目元数据的增删查，以及一条「统一文件源引用」到
预览端点 URL 的拼装。真正的读/写/Range 流式都在 tools/docspace 的 docref
与 tools/fileview 的 media 端点里，这里只把前端要调的入口暴露出来。

不加 build tag：分类、条目、计数在 Linux 面板里一样能用（local 源浏览的是
服务器本机文件，口径与磁盘工具一致；sftp 源经 tools/ftp 会话访问器）。
符合 AGENTS「绑定方法不能藏在 build tag 后面」。

P1 追加：标签（多对多）、版本快照/还原、回收站（仅 local 移实体；sftp 返回不支持）。
P3 追加：加密保险库（scrypt+AES-GCM，口令不落盘）——只支持本机文件。
分享不在本层：链接生成/撤销归 tools/lanshare（另一计划），本计划只在文档中心留入口。
*/

// DocSpaceCounts 汇总各类条目数（前端概览与「是否已启用」判断用）。
func (a *App) DocSpaceCounts() (docspace.Counts, error) {
	s, err := docspace.Global()
	if err != nil {
		applog.Errorf("docspace", "载入失败: %v", err)
		return docspace.Counts{}, err
	}
	return s.Counts()
}

// DocSpaceRoot 文档中心库根目录（设置页展示实体落点用）。
func (a *App) DocSpaceRoot() (string, error) {
	s, err := docspace.Global()
	if err != nil {
		return "", err
	}
	return s.Root(), nil
}

// ---------- 分类 ----------

// DocSpaceCategories 返回分类树（按 Sort 稳定排序）。
func (a *App) DocSpaceCategories() ([]docspace.Category, error) {
	s, err := docspace.Global()
	if err != nil {
		return nil, err
	}
	return s.Categories()
}

// DocSpaceAddCategory 新增分类（parent 空表示根节点），返回带 id 的节点。
func (a *App) DocSpaceAddCategory(name, parent string) (docspace.Category, error) {
	s, err := docspace.Global()
	if err != nil {
		return docspace.Category{}, err
	}
	c, err := s.AddCategory(name, parent)
	if err != nil {
		applog.Errorf("docspace", "新建分类失败: %v", err)
		return docspace.Category{}, err
	}
	applog.Infof("docspace", "新建分类: %s（id=%s parent=%s）", c.Name, c.ID, c.Parent)
	return c, nil
}

// DocSpaceRemoveCategory 删除分类，其下条目退为未分类。
func (a *App) DocSpaceRemoveCategory(id string) error {
	s, err := docspace.Global()
	if err != nil {
		return err
	}
	if err := s.RemoveCategory(id); err != nil {
		applog.Errorf("docspace", "删除分类失败 id=%s: %v", id, err)
		return err
	}
	applog.Infof("docspace", "删除分类: id=%s", id)
	return nil
}

// DocSpaceRenameCategory 改分类名。id 是条目引用的外键，只动 Name 不动 id。
func (a *App) DocSpaceRenameCategory(id, name string) error {
	s, err := docspace.Global()
	if err != nil {
		return err
	}
	if err := s.RenameCategory(id, name); err != nil {
		applog.Errorf("docspace", "重命名分类失败 id=%s: %v", id, err)
		return err
	}
	applog.Infof("docspace", "重命名分类: id=%s -> %s", id, name)
	return nil
}

// ---------- 条目 ----------

// DocSpaceItems 返回全部被纳管条目（按创建时间升序）。
func (a *App) DocSpaceItems() ([]docspace.Item, error) {
	s, err := docspace.Global()
	if err != nil {
		return nil, err
	}
	return s.Items()
}

// DocSpaceAddItem 纳管一个文件（原地为主，不拷贝）。src 空视为 local；
// sftp 源需带 sid（会话 id）。返回登记后的条目元数据。
func (a *App) DocSpaceAddItem(src, sid, path, category string) (docspace.Item, error) {
	ref, err := docspace.Parse(src, sid, path)
	if err != nil {
		return docspace.Item{}, err
	}
	s, err := docspace.Global()
	if err != nil {
		return docspace.Item{}, err
	}
	it, err := s.AddItem(ref, category)
	if err != nil {
		applog.Errorf("docspace", "纳管失败 %s: %v", ref, err)
		return docspace.Item{}, err
	}
	applog.Infof("docspace", "已纳管: %s（id=%s）", it.Name, it.ID)
	return it, nil
}

// DocSpaceRemoveItem 取消纳管（不动磁盘上的原文件）。
func (a *App) DocSpaceRemoveItem(id string) error {
	s, err := docspace.Global()
	if err != nil {
		return err
	}
	if err := s.RemoveItem(id); err != nil {
		applog.Errorf("docspace", "取消纳管失败 id=%s: %v", id, err)
		return err
	}
	applog.Infof("docspace", "取消纳管: id=%s", id)
	return nil
}

// DocSpacePreviewURL 把一条引用拼成预览端点 URL（?src=&sid=&path=），
// 供查看器 <video>/<iframe>/图片 直接指向。src 空按 local 处理，
// 与 media 端点旧参数（仅 ?path=）保持兼容。
func (a *App) DocSpacePreviewURL(src, sid, path string, download bool) (string, error) {
	ref, err := docspace.Parse(src, sid, path)
	if err != nil {
		return "", err
	}
	q := ref.Query()
	if download {
		q.Set("dl", "1")
	}
	return fileview.MediaPath + "?" + q.Encode(), nil
}

// ---------- P1 标签 ----------

// DocSpaceAllTags 返回全部条目派生的标签词表（有序去重）。
func (a *App) DocSpaceAllTags() ([]string, error) {
	s, err := docspace.Global()
	if err != nil {
		return nil, err
	}
	return s.AllTags()
}

// DocSpaceSetItemTags 覆盖设置某条目的标签。
func (a *App) DocSpaceSetItemTags(itemID string, tags []string) error {
	s, err := docspace.Global()
	if err != nil {
		return err
	}
	if err := s.SetItemTags(itemID, tags); err != nil {
		applog.Errorf("docspace", "设置标签失败 id=%s: %v", itemID, err)
		return err
	}
	return nil
}

// ---------- 分类归属调整 ----------

// DocSpaceSetItemCategory 设置单个条目的分类（category 空表示未分类）。
func (a *App) DocSpaceSetItemCategory(itemID, category string) error {
	s, err := docspace.Global()
	if err != nil {
		return err
	}
	if err := s.SetItemCategory(itemID, category); err != nil {
		applog.Errorf("docspace", "设置分类失败 id=%s: %v", itemID, err)
		return err
	}
	return nil
}

// ---------- 派生元数据与总览 ----------

// DocSpaceMetas 返回全部条目的类型/大小/修改时间，顺序与 DocSpaceItems 一致。
// 前端按下标对齐拼成一张表，因此这两个方法必须同序（都来自 Store.Items()）。
// 缩略图 URL 在这一轮里顺手填好：网格视图下几百张图逐条求 URL 就是几百次往返。
func (a *App) DocSpaceMetas() ([]docspace.ItemMeta, error) {
	s, err := docspace.Global()
	if err != nil {
		return nil, err
	}
	metas, err := s.Metas()
	if err != nil {
		return nil, err
	}
	// 引用三元组（src/sid/path）在条目里而不在元数据里：再取一次 Items()
	// 按下标对齐就够了（Metas 就是根据 Items 生成的，两者同序），
	// 不必把同一事实再存一份到 ItemMeta。
	items, err := s.Items()
	if err != nil {
		return nil, err
	}
	for i := range metas {
		// 两次取表之间条目可能被并发增删，下标就会错行——错行拼出的
		// 是“别人的缩略图”，比不给图更难发现，所以逐位校 id 一致才用。
		if !metas[i].Thumbable || i >= len(items) || metas[i].ID != items[i].ID {
			continue
		}
		ref, err := docspace.Parse(items[i].Src, items[i].SessionID, items[i].Path)
		if err != nil {
			// 拼不出引用就不给图，前端回退类型图标，不把整批元数据抖掉
			continue
		}
		metas[i].Thumb = thumbURLFor(ref, 0)
	}
	return metas, nil
}

// DocSpaceOverview 一次给出统计卡、类型分布、分类徽标与源分布。
// 合并成一个入口而不是四个：都是同一份元数据的汇总，拆开只会把同一批 Stat 拖成四轮往返。
func (a *App) DocSpaceOverview() (docspace.Overview, error) {
	s, err := docspace.Global()
	if err != nil {
		return docspace.Overview{}, err
	}
	return s.Overview()
}

// DocSpaceThumbURL 把一条引用拼成缩略图端点 URL。
// 走 HTTP 而不是把字节从绑定里返回：几百张图一次灌进来会把调用层拖垮，
// 而 <img> 天然并发懒加载 + 浏览器缓存。
// dim<=0 用后端默认尺寸；不可出图的类型直接报错，让前端回退到类型图标。
func (a *App) DocSpaceThumbURL(src, sid, path string, dim int) (string, error) {
	ref, err := docspace.Parse(src, sid, path)
	if err != nil {
		return "", err
	}
	if !filekind.Thumbable(ref.Path) {
		return "", fmt.Errorf("该类型不生成缩略图: %s", filekind.Ext(ref.Path))
	}
	return thumbURLFor(ref, dim), nil
}

// thumbURLFor 拼缩略图 URL。dim<=0 时不写 w，交给端点默认尺寸。
func thumbURLFor(ref docspace.Ref, dim int) string {
	q := ref.Query()
	if dim > 0 {
		q.Set("w", fmt.Sprintf("%d", dim))
	}
	return fileview.ThumbPath + "?" + q.Encode()
}

// ---------- 批量操作 ----------

// DocSpaceMoveItemsCategory 批量改分类，返回实际改动条数。
func (a *App) DocSpaceMoveItemsCategory(ids []string, category string) (int, error) {
	s, err := docspace.Global()
	if err != nil {
		return 0, err
	}
	n, err := s.MoveItemsCategory(ids, category)
	if err != nil {
		applog.Errorf("docspace", "批量改分类失败: %v", err)
		return 0, err
	}
	applog.Infof("docspace", "批量改分类: %d 条 → %s", n, category)
	return n, nil
}

// DocSpaceTagItems 批量加/减标签，返回实际改动条数。
func (a *App) DocSpaceTagItems(ids, add, remove []string) (int, error) {
	s, err := docspace.Global()
	if err != nil {
		return 0, err
	}
	n, err := s.TagItems(ids, add, remove)
	if err != nil {
		applog.Errorf("docspace", "批量改标签失败: %v", err)
		return 0, err
	}
	return n, nil
}

// DocSpaceRemoveItems 批量取消纳管（不动磁盘原文件）。
func (a *App) DocSpaceRemoveItems(ids []string) (int, error) {
	s, err := docspace.Global()
	if err != nil {
		return 0, err
	}
	n, err := s.RemoveItems(ids)
	if err != nil {
		applog.Errorf("docspace", "批量取消纳管失败: %v", err)
		return 0, err
	}
	applog.Infof("docspace", "批量取消纳管: %d 条", n)
	return n, nil
}

// DocSpaceTrashItems 批量移入回收站。非本机源逐条跳过，失败 id 如实回传。
func (a *App) DocSpaceTrashItems(ids []string) (docspace.BatchResult, error) {
	s, err := docspace.Global()
	if err != nil {
		return docspace.BatchResult{}, err
	}
	res, err := s.TrashItems(ids)
	if err != nil {
		applog.Errorf("docspace", "批量移入回收站失败: %v", err)
		return res, err
	}
	applog.Infof("docspace", "批量移入回收站: 成功 %d，失败 %d", res.Done, len(res.Failed))
	return res, nil
}

// DocSpaceSnapshotItems 批量存快照，保留数取配置。
func (a *App) DocSpaceSnapshotItems(ids []string, note string) (docspace.BatchResult, error) {
	s, err := docspace.Global()
	if err != nil {
		return docspace.BatchResult{}, err
	}
	res, err := s.SnapshotItems(ids, note, config.DocSpaceCfg().VersionKeep)
	if err != nil {
		applog.Errorf("docspace", "批量快照失败: %v", err)
		return res, err
	}
	applog.Infof("docspace", "批量快照: 成功 %d，失败 %d", res.Done, len(res.Failed))
	return res, nil
}

// ---------- P1 版本 ----------

// DocSpaceVersions 返回某条目的全部版本快照（按版本号升序）。
func (a *App) DocSpaceVersions(itemID string) ([]docspace.Version, error) {
	s, err := docspace.Global()
	if err != nil {
		return nil, err
	}
	return s.ListVersions(itemID)
}

// DocSpaceSnapshot 为某条目当前内容存一版快照，返回新版本。
// 保留数取配置的 versionKeep（<=0 不裁剪）。
func (a *App) DocSpaceSnapshot(itemID, note string) (docspace.Version, error) {
	s, err := docspace.Global()
	if err != nil {
		return docspace.Version{}, err
	}
	v, err := s.SnapshotItem(itemID, note, config.DocSpaceCfg().VersionKeep)
	if err != nil {
		applog.Errorf("docspace", "快照失败 id=%s: %v", itemID, err)
		return docspace.Version{}, err
	}
	applog.Infof("docspace", "已存快照: id=%s v%d", itemID, v.N)
	return v, nil
}

// DocSpaceRestore 把某版本写回原路径（写回前自动存当前版）。
func (a *App) DocSpaceRestore(itemID string, n int) error {
	s, err := docspace.Global()
	if err != nil {
		return err
	}
	if err := s.RestoreVersion(itemID, n); err != nil {
		applog.Errorf("docspace", "还原失败 id=%s v%d: %v", itemID, n, err)
		return err
	}
	applog.Infof("docspace", "已还原: id=%s v%d", itemID, n)
	return nil
}

// ---------- P1 回收站 ----------

// DocSpaceMoveTrash 把 local 条目实体移入回收站（sftp 返回不支持）。
func (a *App) DocSpaceMoveTrash(itemID string) error {
	s, err := docspace.Global()
	if err != nil {
		return err
	}
	if err := s.MoveToTrash(itemID); err != nil {
		applog.Errorf("docspace", "移入回收站失败 id=%s: %v", itemID, err)
		return err
	}
	applog.Infof("docspace", "已移入回收站: id=%s", itemID)
	return nil
}

// DocSpaceTrash 返回回收站条目（最近删除在前）。
// 先按配置的保留天数自动清理超期实体，再列出。
func (a *App) DocSpaceTrash() ([]docspace.TrashEntry, error) {
	s, err := docspace.Global()
	if err != nil {
		return nil, err
	}
	if n, err := s.PurgeTrash(config.DocSpaceCfg().TrashKeepDays); err != nil {
		applog.Warnf("docspace", "自动清理超期回收站失败: %v", err)
	} else if n > 0 {
		applog.Infof("docspace", "已自动清理 %d 条超期回收站实体", n)
	}
	return s.ListTrash()
}

// DocSpaceRestoreTrash 从回收站还原实体到原路径并重新登记条目。
func (a *App) DocSpaceRestoreTrash(entryID string) (docspace.Item, error) {
	s, err := docspace.Global()
	if err != nil {
		return docspace.Item{}, err
	}
	it, err := s.RestoreTrash(entryID)
	if err != nil {
		applog.Errorf("docspace", "还原失败 entry=%s: %v", entryID, err)
		return docspace.Item{}, err
	}
	applog.Infof("docspace", "已从回收站还原: entry=%s → id=%s", entryID, it.ID)
	return it, nil
}

// DocSpaceDeleteTrash 彻底删除一个回收站条目（连带实体）。
func (a *App) DocSpaceDeleteTrash(entryID string) error {
	s, err := docspace.Global()
	if err != nil {
		return err
	}
	if err := s.DeleteTrashEntry(entryID); err != nil {
		applog.Errorf("docspace", "彻底删除失败 entry=%s: %v", entryID, err)
		return err
	}
	applog.Infof("docspace", "已彻底删除: entry=%s", entryID)
	return nil
}

// DocSpaceEmptyTrash 清空回收站。
func (a *App) DocSpaceEmptyTrash() error {
	s, err := docspace.Global()
	if err != nil {
		return err
	}
	if err := s.EmptyTrash(); err != nil {
		applog.Errorf("docspace", "清空回收站失败: %v", err)
		return err
	}
	applog.Infof("docspace", "已清空回收站")
	return nil
}

// ---------- P3 加密保险库（仅本机文件；口令不落盘） ----------

// SecStoreList 返回保险库全部条目（最近加密在前）。
func (a *App) SecStoreList() ([]docspace.SecureEntry, error) {
	s, err := docspace.Global()
	if err != nil {
		return nil, err
	}
	return s.ListSecure()
}

// SecStoreEncrypt 加密一个本机文件：成功后原明文被删除，只留密文条目。
// 不可逆操作，前端必须先二次确认并告知会删除原文件。
func (a *App) SecStoreEncrypt(path, password string) (docspace.SecureEntry, error) {
	s, err := docspace.Global()
	if err != nil {
		return docspace.SecureEntry{}, err
	}
	e, err := s.Encrypt(path, password)
	if err != nil {
		applog.Errorf("docspace", "加密失败 %s: %v", path, err)
		return docspace.SecureEntry{}, err
	}
	applog.Infof("docspace", "已加密: %s（id=%s）", e.Path, e.ID)
	return e, nil
}

// SecStoreUnlock 用口令解密并写出明文临时视图，返回其绝对路径（供查看器打开）。
// 口令错回 ErrWrongPassword。用完必须调 SecStoreLock 删除明文。
func (a *App) SecStoreUnlock(id, password string) (string, error) {
	s, err := docspace.Global()
	if err != nil {
		return "", err
	}
	view, err := s.Unlock(id, password)
	if err != nil {
		applog.Warnf("docspace", "解锁失败 id=%s: %v", id, err)
		return "", err
	}
	applog.Infof("docspace", "已解锁: id=%s", id)
	return view, nil
}

// SecStoreLock 删除某条目的解锁明文临时视图（锁上即删）。
func (a *App) SecStoreLock(id string) error {
	s, err := docspace.Global()
	if err != nil {
		return err
	}
	if err := s.Lock(id); err != nil {
		applog.Errorf("docspace", "上锁失败 id=%s: %v", id, err)
		return err
	}
	return nil
}

// SecStoreChangePassword 校验旧口令后重新加密（新盐新 nonce）。
func (a *App) SecStoreChangePassword(id, oldPw, newPw string) error {
	s, err := docspace.Global()
	if err != nil {
		return err
	}
	if err := s.ChangePassword(id, oldPw, newPw); err != nil {
		applog.Warnf("docspace", "改密失败 id=%s: %v", id, err)
		return err
	}
	applog.Infof("docspace", "已改密: id=%s", id)
	return nil
}

// SecStoreExtractTo 用口令解密并把明文永久写到 destPath（取回明文）。
func (a *App) SecStoreExtractTo(id, password, destPath string) error {
	s, err := docspace.Global()
	if err != nil {
		return err
	}
	if err := s.ExtractTo(id, password, destPath); err != nil {
		applog.Warnf("docspace", "取回明文失败 id=%s: %v", id, err)
		return err
	}
	applog.Infof("docspace", "已取回明文: id=%s → %s", id, destPath)
	return nil
}

// SecStoreRemove 彻底删除密文条目（实体+临时视图+登记），不可恢复。
func (a *App) SecStoreRemove(id string) error {
	s, err := docspace.Global()
	if err != nil {
		return err
	}
	if err := s.RemoveSecure(id); err != nil {
		applog.Errorf("docspace", "删除密文条目失败 id=%s: %v", id, err)
		return err
	}
	applog.Infof("docspace", "已彻底删除密文条目: id=%s", id)
	return nil
}

// ---------- P1 智能搜索与在线笔记（补做） ----------

// DocSpaceSearch 按元数据条件过滤纳管条目（分类含子级/标签 AND/有无版本/
// 最近更新/大小区间），按更新时间降序。大小条件仅 local 源可判定。
// 整盘/内容检索复用既有 DiskSearch/DiskSearchContent，前端命中后可一键纳管。
func (a *App) DocSpaceSearch(f docspace.SearchFilter) ([]docspace.Item, error) {
	s, err := docspace.Global()
	if err != nil {
		return nil, err
	}
	return s.Search(f)
}

// DocSpaceNewNote 在库内 notes/ 新建一篇笔记（format 取 md|html，空默认 md）并归入「笔记」分类，
// 返回已纳管的条目（前端拿 path 去笔记模块编辑）。同名报错不覆盖。
func (a *App) DocSpaceNewNote(name string, format string) (docspace.Item, error) {
	s, err := docspace.Global()
	if err != nil {
		return docspace.Item{}, err
	}
	it, err := s.NewNote(name, format)
	if err != nil {
		applog.Warnf("docspace", "新建笔记失败 %q(%s): %v", name, format, err)
		return docspace.Item{}, err
	}
	applog.Infof("docspace", "新建笔记: %s（id=%s）", it.Name, it.ID)
	return it, nil
}

// ---------- P1 设置（总开关 / 版本保留数 / 回收站保留天数） ----------

// DocSpaceSettings 返回文档中心设置段（老配置无此段时返回默认值）。
func (a *App) DocSpaceSettings() config.DocSpaceConfig {
	return config.DocSpaceCfg()
}

// SaveDocSpaceSettings 保存文档中心设置：启用开关 + 版本保留数 + 回收站保留天数。
// 保留数/天数的合法性由 config 规范化兜底，这里只负责写入。
func (a *App) SaveDocSpaceSettings(enabled bool, versionKeep, trashKeepDays int) error {
	_, err := config.Update(func(cfg *config.AppConfig) {
		if cfg.DocSpace == nil {
			cfg.DocSpace = &config.DocSpaceConfig{}
		}
		cfg.DocSpace.Enabled = enabled
		cfg.DocSpace.VersionKeep = versionKeep
		cfg.DocSpace.TrashKeepDays = trashKeepDays
	})
	if err != nil {
		applog.Errorf("docspace", "保存文档中心设置失败: %v", err)
		return err
	}
	applog.Infof("docspace", "已保存文档中心设置: enabled=%v versionKeep=%d trashKeepDays=%d", enabled, versionKeep, trashKeepDays)
	return nil
}
