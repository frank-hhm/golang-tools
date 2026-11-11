package datastore

// 本文件只管一件事：把「我们自己的配置数据」摊成一份文档清单。
//
// 为什么是扫描而不是一张硬编码登记表：存量代码里有二十多处直接 os.WriteFile 落
// JSON（tools/guard、tools/mysql、tools/docspace、tools/clouddrive…），一处不改才谈得上
// 零回归；反过来，只要新服务的缓存还落在 data/<服务名>/<文件>.json 里，扫描就自然把它
// 纳入受管范围，不需要"加个服务就回来补一行登记"。appdirs 包注释里那条约定
// （"data/<服务名>：服务的数据缓存"）就是这份清单的真源。
//
// 扫描之外只有三类人工知识，写成三张表：目录级硬排除、永不外存的文件、含凭据的文件。

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"Tools/appdirs"
	"Tools/services/config"
)

const (
	// configDocKey 引导文件在清单里的键。它不在 data 根下（落用户配置目录），
	// 但它是所有设置的总和，不给它开个特例就等于"换机器设置全丢"。
	configDocKey = "app/config.json"

	// maxDocBytes 单文档上限：外存副本走的是本机 MySQL/Redis，一个几百 MB 的 json
	// 台账塞进去只会拖垮实例。超限的文档直接不纳入并在 UI 说明原因。
	maxDocBytes = 4 << 20

	// maxScanDepth 从 data 根起算的递归深度。受管配置全在 data/<服务名>/<文件> 这一层，
	// 再多一层就是各服务的运行时目录（互传会话、下载分块），不属于"配置"。
	maxScanDepth = 2
)

// excludedDirs 目录级硬排除（相对 data 根的 slash 前缀）：这些位置放的是运行时数据
// 与版本下载缓存，既不是配置，体积也不受控。
var excludedDirs = []string{
	"lanshare/uploads/",
	"redis/redis-servers/",
	"mysql/mysql-servers/",
	"nginx/nginx-servers/",
	"php/php-servers/",
	"npm/npm-servers/",
}

// excludedKeys 永不外存的文件，UI 里连开关都不给。
// panel/settings.json 存的是面板登录密码哈希与签发会话用的 Secret：把它写到另一个进程
// 能读的库里，等于把本机 0600 的防线换成"看谁能连上那台 MySQL"，不提供任何额外价值。
var excludedKeys = map[string]bool{
	"panel/settings.json": true,
}

// sensitiveKeys 含凭据的文档：默认不外存，用户显式勾选"含凭据的文档加密后外存"才纳入，
// 且写入前走 envelope.go 的信封加密。
//
// 这份名单按"文件里有没有可直接复用的口令/密钥/token"判定，宁滥勿缺：mysql 与 redis 的
// 实例登记里就摆着 RootPassword/Password 明文（各服务自己要拿它拉起服务器，退不开），
// docspace 的 secure.json 是加密空间口令，term/ftp 是登录凭据。
var sensitiveKeys = map[string]bool{
	"mysql/mysql-instances.json": true,
	"redis/redis-instances.json": true,
	"term/credentials.json":      true,
	"ftp/connections.json":       true,
	"clouddrive/accounts.json":   true,
	"clouddrive/uploads.json":    true,
	"docspace/secure.json":       true,
}

// doc 一份受管文档的扫描结果。
type doc struct {
	Key       string // 清单里的键：data 根相对路径（slash），或 configDocKey
	Path      string // 绝对路径
	Label     string // 中文展示名
	Size      int64
	MtimeMs   int64
	Digest    string // 文件内容 sha256 hex；读不动时为 ""
	Sensitive bool
	Included  bool
	Reason    string // 未纳入的原因（前端 tooltip 直接显示）
}

// docLabel 已知文件的展示名；没列到的退回文件名，保证新增服务也有可读标题。
var docLabel = map[string]string{
	configDocKey:                 "应用设置（config.json）",
	"mysql/mysql-instances.json": "MySQL 实例登记",
	"mysql/mysql-locals.json":    "MySQL 本地版本登记",
	"redis/redis-instances.json": "Redis 实例登记",
	"redis/redis-locals.json":    "Redis 本地版本登记",
	"php/php-instances.json":     "PHP 实例登记",
	"php/php-locals.json":        "PHP 本地版本登记",
	"nginx/nginx-instances.json": "Nginx 实例登记",
	"nginx/nginx-locals.json":    "Nginx 本地版本登记",
	"npm/npm-instances.json":     "npm 实例登记",
	"npm/npm-locals.json":        "npm 本地版本登记",
	"npm/npm-swaps.json":         "npm 版本切换记录",
	"npm/npmrc":                  "npm 全局配置（npmrc）",
	"guard/guard.json":           "进程守护登记",
	"clouddrive/accounts.json":   "云盘账号",
	"clouddrive/uploads.json":    "云盘分块上传会话",
	"docspace/taxonomy.json":     "文档中心分类树",
	"docspace/trash.json":        "文档中心回收站",
	"docspace/secure.json":       "文档中心加密空间",
	"docspace/shares.json":       "文档中心分享记录",
	"docspace/backups.json":      "文档中心备份台账",
	"term/credentials.json":      "终端凭据",
	"ftp/connections.json":       "FTP 连接与凭据",
	"lanshare/access.json":       "局域网互传访问记录",
	"theme/themes.json":          "主题背景配置",
	"panel/settings.json":        "面板账号与密钥（永不外存）",
}

// managedDocs 扫描出全部候选文档并判定谁被纳入。
//
// exclude 是用户在设置页显式排除的键；includeSensitive 决定是否把含凭据那一组也放进
// 清单（放进清单后仍会走信封加密，见 envelope.go）。
func managedDocs(exclude []string, includeSensitive bool) []doc {
	excluded := make(map[string]bool, len(exclude))
	for _, k := range exclude {
		excluded[k] = true
	}
	dataRoot := appdirs.DataBase()
	docs := make([]doc, 0, 32)

	// 引导文件：不在 data 根下，单独收一次。
	if p, err := config.ConfigFilePath(); err == nil {
		docs = append(docs, newDoc(configDocKey, p))
	}

	// data 根递归：只认 *.json 与 npmrc 这两种纯文本配置。
	_ = filepath.WalkDir(dataRoot, func(path string, de os.DirEntry, err error) error {
		if err != nil {
			// 目录被占用/无权限是常态（实例工作目录可能正被子进程拿着），跳过就行
			if de != nil && de.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if de.IsDir() {
			rel := relKey(dataRoot, path)
			if rel == "" {
				return nil
			}
			if strings.Count(rel, "/")+1 >= maxScanDepth || isExcludedDir(rel+"/") {
				return filepath.SkipDir
			}
			return nil
		}
		name := de.Name()
		if !strings.HasSuffix(name, ".json") && name != "npmrc" {
			return nil
		}
		key := relKey(dataRoot, path)
		if key == "" {
			return nil
		}
		docs = append(docs, newDoc(key, path))
		return nil
	})

	for i := range docs {
		decide(&docs[i], excluded, includeSensitive)
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Key < docs[j].Key })
	return docs
}

// newDoc 读一次文件头信息并算摘要。文件不存在/读不动时 Size=0、Digest=""，
// 由调用方按"这份先不管"处理，不报错——受管文档里"还没被写过"是正常状态。
func newDoc(key, path string) doc {
	d := doc{Key: key, Path: path, Label: labelOf(key, path)}
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return d
	}
	d.Size = st.Size()
	d.MtimeMs = st.ModTime().UnixMilli()
	d.Digest = digestFile(path)
	return d
}

// decide 判定一份文档这次要不要外存，并把原因写成给人看的话。
func decide(d *doc, excluded map[string]bool, includeSensitive bool) {
	switch {
	case d.Digest == "":
		d.Reason = "文件还不存在或读不出来，本次不处理"
	case excludedKeys[d.Key]:
		d.Reason = "含面板登录密钥，永不外存"
	case isExcludedDir(d.Key):
		d.Reason = "运行时目录，不是配置"
	case excluded[d.Key]:
		d.Reason = "已在设置页排除"
	case d.Size > maxDocBytes:
		d.Reason = "超过 4 MiB 单文档上限"
	case sensitiveOf(d.Key):
		d.Sensitive = true
		if includeSensitive {
			d.Included = true
		} else {
			d.Reason = "含凭据，默认不外存（勾选后加密外存）"
		}
	default:
		d.Included = true
	}
}

func sensitiveOf(key string) bool { return sensitiveKeys[key] }

func isExcludedDir(rel string) bool {
	for _, p := range excludedDirs {
		if strings.HasPrefix(rel, p) {
			return true
		}
	}
	return false
}

func relKey(root, path string) string {
	if root == "" {
		return ""
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	return rel
}

func labelOf(key, path string) string {
	if l, ok := docLabel[key]; ok {
		return l
	}
	return filepath.Base(path)
}

// digestFile 算文件 sha256（hex）。流式读，不把整份文件一次性搬进内存。
func digestFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// digestBytes 与 digestFile 同一个算法：一份内容两套摘要就是给自己埋雷——
// 写出的副本、比对的快照、恢复时的判断全都靠这三个字必须相等。
func digestBytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
