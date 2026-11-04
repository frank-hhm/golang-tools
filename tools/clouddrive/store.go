package clouddrive

// 云盘账号表（照 tools/ftp/store.go 的多账号范式）。
//
// 落盘 <data>/clouddrive/accounts.json：Account{ID,Kind,Label,...非敏感} + SecretEnc
// （access/refresh token 的密文，tools/secret 保护：Windows DPAPI / 其他平台 0600 明文）。
// 回给前端的 AccountView 永不含 SecretEnc——token 只在 Go 侧解密后用于建连。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"Tools/appdirs"
	"Tools/tools/secret"
)

// AccountKind 支持的云盘种类。google 走 OAuth 授权码；aliyun/baidu 走「粘贴式」
// 长期凭证（阿里 refresh_token / 百度 BDUSS），由 SetCredential 直接写入；
// 对象存储三家（OSS/COS/S3）用 AK/SK + bucket 配置一步建账号，见 AddObjectStore。
const (
	KindGoogle     = "google"
	KindAliyun     = "aliyun"
	KindBaidu      = "baidu"
	KindAliyunOSS  = "aliyun_oss"
	KindTencentCOS = "tencent_cos"
	KindS3         = "s3"
)

// isObjectStoreKind 是否为对象存储类账号（AK/SK 而非 OAuth/粘贴式长期凭证）。
func isObjectStoreKind(kind string) bool {
	switch kind {
	case KindAliyunOSS, KindTencentCOS, KindS3:
		return true
	}
	return false
}

// Account 落盘的完整记录（含密文），仅 Go 内部使用。
// Bucket/Endpoint/Region 只对对象存储有意义，属非敏感配置（回前端可显示）。
type Account struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	Bucket    string `json:"bucket,omitempty"`
	Endpoint  string `json:"endpoint,omitempty"`
	Region    string `json:"region,omitempty"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
	SecretEnc string `json:"secretEnc,omitempty"` // token 包密文，绝不回前端
}

// AccountView 可安全交给前端的账号（无密文）。HasToken 表示是否已完成授权。
type AccountView struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	Bucket    string `json:"bucket,omitempty"`
	Endpoint  string `json:"endpoint,omitempty"`
	Region    string `json:"region,omitempty"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
	HasToken  bool   `json:"hasToken"`
}

// tokenBlob SecretEnc 解密后的明文结构（token 只在内存里短暂存在）。
// google/aliyun 用 Access/Refresh/Expiry；baidu 用 Cookie(BDUSS)；aliyun 顺带缓存 DriveID；
// 对象存储三家只用 AkID/AkSecret（AccessKey / SecretId+SecretKey）。
type tokenBlob struct {
	Access   string `json:"access"`
	Refresh  string `json:"refresh"`
	Expiry   int64  `json:"expiry"`             // UnixMilli，access 到期时刻
	Cookie   string `json:"cookie,omitempty"`   // baidu BDUSS
	DriveID  string `json:"driveId,omitempty"`  // aliyun default_drive_id（惰性缓存）
	AkID     string `json:"akId,omitempty"`     // 对象存储 AccessKey ID / SecretId
	AkSecret string `json:"akSecret,omitempty"` // 对象存储 AccessKey Secret / SecretKey
}

var storeMu sync.Mutex

// baseOver 测试注入用：非空则账号表落在这个目录，不污染真实 appdirs。
var baseOver string

func setBaseDirForTest(dir string) { baseOver = dir }

func storePath() (string, error) {
	if baseOver != "" {
		if err := os.MkdirAll(baseOver, 0o755); err != nil {
			return "", err
		}
		return filepath.Join(baseOver, "accounts.json"), nil
	}
	dir, err := appdirs.Data("clouddrive")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "accounts.json"), nil
}

func readAccounts() ([]Account, error) {
	p, err := storePath()
	if err != nil {
		return nil, err
	}
	bs, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return []Account{}, nil
		}
		return nil, fmt.Errorf("读取云盘账号失败: %w", err)
	}
	out := []Account{}
	if len(bs) > 0 {
		if err := json.Unmarshal(bs, &out); err != nil {
			return nil, fmt.Errorf("云盘账号文件损坏: %w", err)
		}
	}
	return out, nil
}

func writeAccounts(accs []Account) error {
	p, err := storePath()
	if err != nil {
		return err
	}
	bs, err := json.MarshalIndent(accs, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, bs, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func toView(a Account) AccountView {
	return AccountView{
		ID: a.ID, Kind: a.Kind, Label: a.Label,
		Bucket: a.Bucket, Endpoint: a.Endpoint, Region: a.Region,
		CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
		HasToken: a.SecretEnc != "",
	}
}

func viewsOf(accs []Account) []AccountView {
	out := make([]AccountView, 0, len(accs))
	for _, a := range accs {
		out = append(out, toView(a))
	}
	return out
}

// ListAccounts 全部账号（新的在前），仅非敏感字段。
func ListAccounts() ([]AccountView, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	accs, err := readAccounts()
	if err != nil {
		return nil, err
	}
	return viewsOf(accs), nil
}

// AddAccount 新增一个空账号（尚未授权，SecretEnc 为空）。kind 必须是已支持的类型。
func AddAccount(kind, label string) (AccountView, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	label = strings.TrimSpace(label)
	if kind == "" {
		return AccountView{}, fmt.Errorf("未指定云盘类型")
	}
	if _, ok := lookupFactory(kind); !ok {
		return AccountView{}, fmt.Errorf("暂不支持的云盘类型: %s", kind)
	}
	if label == "" {
		label = kind
	}
	storeMu.Lock()
	defer storeMu.Unlock()
	accs, err := readAccounts()
	if err != nil {
		return AccountView{}, err
	}
	now := time.Now().UnixMilli()
	a := Account{
		ID:        fmt.Sprintf("c%d", time.Now().UnixNano()),
		Kind:      kind,
		Label:     label,
		CreatedAt: now,
		UpdatedAt: now,
	}
	accs = append([]Account{a}, accs...)
	if err := writeAccounts(accs); err != nil {
		return AccountView{}, fmt.Errorf("保存云盘账号失败: %w", err)
	}
	return toView(a), nil
}

// RemoveAccount 删除账号（连带其 token 密文；不动云端实体）。
func RemoveAccount(id string) error {
	storeMu.Lock()
	defer storeMu.Unlock()
	accs, err := readAccounts()
	if err != nil {
		return err
	}
	kept := make([]Account, 0, len(accs))
	found := false
	for _, a := range accs {
		if a.ID == id {
			found = true
			continue
		}
		kept = append(kept, a)
	}
	if !found {
		return fmt.Errorf("账号不存在: %s", id)
	}
	return writeAccounts(kept)
}

// RenameAccount 改显示名。
func RenameAccount(id, label string) error {
	label = strings.TrimSpace(label)
	if label == "" {
		return fmt.Errorf("名称不能为空")
	}
	storeMu.Lock()
	defer storeMu.Unlock()
	accs, err := readAccounts()
	if err != nil {
		return err
	}
	for i := range accs {
		if accs[i].ID == id {
			accs[i].Label = label
			accs[i].UpdatedAt = time.Now().UnixMilli()
			return writeAccounts(accs)
		}
	}
	return fmt.Errorf("账号不存在: %s", id)
}

// getAccount 取完整记录（含密文）。
func getAccount(id string) (Account, error) {
	accs, err := readAccounts()
	if err != nil {
		return Account{}, err
	}
	for _, a := range accs {
		if a.ID == id {
			return a, nil
		}
	}
	return Account{}, fmt.Errorf("账号不存在: %s", id)
}

// loadToken 解密账号的 token 包；未授权返回空 blob（ErrNotAuthorized 由调用方判）。
func loadToken(id string) (tokenBlob, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	a, err := getAccount(id)
	if err != nil {
		return tokenBlob{}, err
	}
	if a.SecretEnc == "" {
		return tokenBlob{}, fmt.Errorf("账号尚未授权")
	}
	plain, err := secret.Unprotect(a.SecretEnc)
	if err != nil {
		return tokenBlob{}, err
	}
	var b tokenBlob
	if err := json.Unmarshal([]byte(plain), &b); err != nil {
		return tokenBlob{}, fmt.Errorf("token 解析失败: %w", err)
	}
	return b, nil
}

// saveToken 加密写回 token 包（刷新后持久化新 access token）。
func saveToken(id string, b tokenBlob) error {
	storeMu.Lock()
	defer storeMu.Unlock()
	accs, err := readAccounts()
	if err != nil {
		return err
	}
	bs, err := json.Marshal(b)
	if err != nil {
		return err
	}
	enc, err := secret.Protect(string(bs))
	if err != nil {
		return err
	}
	for i := range accs {
		if accs[i].ID == id {
			accs[i].SecretEnc = enc
			accs[i].UpdatedAt = time.Now().UnixMilli()
			return writeAccounts(accs)
		}
	}
	return fmt.Errorf("账号不存在: %s", id)
}

// SetCredential 按账号种类写入「粘贴式」长期凭证：
//   - aliyun：value 是 refresh_token（存 Refresh 字段，运行期自动换 access）；
//   - baidu：value 是 BDUSS Cookie（存 Cookie 字段）。
//
// google 不走这里（它靠 OAuth 授权码交换，见 ExchangeCode），返回明确错误。
// 覆盖写入时保留同类已有的其它字段（如阿里刷新出来的 DriveID 不丢）。
func SetCredential(acctID, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("凭证不能为空")
	}
	a, err := getAccount(acctID)
	if err != nil {
		return err
	}
	switch a.Kind {
	case KindAliyun:
		b, _ := loadToken(acctID) // 无 token 时得零值，覆盖 Refresh 即可
		b.Refresh, b.Access, b.Expiry, b.Cookie = value, "", 0, ""
		return saveToken(acctID, b)
	case KindBaidu:
		b, _ := loadToken(acctID)
		b.Cookie, b.Access, b.Refresh, b.Expiry = value, "", "", 0
		return saveToken(acctID, b)
	default:
		return fmt.Errorf("该云盘类型不支持手动填写凭证，请走授权流程")
	}
}

/* ============================ 对象存储账号 ============================ */

// AddObjectStore 一步创建对象存储账号（阿里 OSS / 腾讯 COS / 通用 S3）：对象存储没有
// OAuth 授权码流程，创建即填齐。bucket/endpoint/region 为非敏感配置明文入库（回前端
// 可显示）；AccessKey ID/Secret 进 tokenBlob、经 tools/secret 加密落盘、永不回前端。
// endpoint/region 可留空（由各适配器按 kind 从默认域名推导）。
func AddObjectStore(kind, label, bucket, endpoint, region, akID, akSecret string) (AccountView, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if !isObjectStoreKind(kind) {
		return AccountView{}, fmt.Errorf("不是对象存储类型: %s", kind)
	}
	if _, ok := lookupFactory(kind); !ok {
		return AccountView{}, fmt.Errorf("暂不支持的云盘类型: %s", kind)
	}
	bucket = strings.TrimSpace(bucket)
	akID, akSecret = strings.TrimSpace(akID), strings.TrimSpace(akSecret)
	if bucket == "" {
		return AccountView{}, fmt.Errorf("bucket 不能为空")
	}
	if akID == "" || akSecret == "" {
		return AccountView{}, fmt.Errorf("AccessKey ID / Secret 不能为空")
	}
	if label = strings.TrimSpace(label); label == "" {
		label = osKindLabel(kind)
	}
	bs, err := json.Marshal(tokenBlob{AkID: akID, AkSecret: akSecret})
	if err != nil {
		return AccountView{}, err
	}
	enc, err := secret.Protect(string(bs))
	if err != nil {
		return AccountView{}, err
	}
	storeMu.Lock()
	defer storeMu.Unlock()
	accs, err := readAccounts()
	if err != nil {
		return AccountView{}, err
	}
	now := time.Now().UnixMilli()
	a := Account{
		ID:        fmt.Sprintf("c%d", time.Now().UnixNano()),
		Kind:      kind,
		Label:     label,
		Bucket:    bucket,
		Endpoint:  strings.TrimSpace(endpoint),
		Region:    strings.TrimSpace(region),
		CreatedAt: now,
		UpdatedAt: now,
		SecretEnc: enc,
	}
	accs = append([]Account{a}, accs...)
	if err := writeAccounts(accs); err != nil {
		return AccountView{}, fmt.Errorf("保存云盘账号失败: %w", err)
	}
	return toView(a), nil
}

// osKindLabel 对象存储账号的默认显示名（与注册文件里的 Name() 文案保持一致）。
func osKindLabel(kind string) string {
	switch kind {
	case KindAliyunOSS:
		return "阿里云 OSS"
	case KindTencentCOS:
		return "腾讯云 COS"
	case KindS3:
		return "S3 兼容存储"
	}
	return kind
}

// loadObjectStoreConfig 取对象存储账号的非敏感配置 + 解密后的 AK/SK（仅 Go 内部使用）。
func loadObjectStoreConfig(acctID string) (Account, tokenBlob, error) {
	a, err := getAccount(acctID)
	if err != nil {
		return Account{}, tokenBlob{}, err
	}
	if !isObjectStoreKind(a.Kind) {
		return Account{}, tokenBlob{}, fmt.Errorf("账号 %s 不是对象存储类型", acctID)
	}
	if a.Bucket == "" {
		return Account{}, tokenBlob{}, fmt.Errorf("账号尚未配置 bucket")
	}
	b, err := loadToken(acctID)
	if err != nil {
		return Account{}, tokenBlob{}, err
	}
	if b.AkID == "" || b.AkSecret == "" {
		return Account{}, tokenBlob{}, fmt.Errorf("账号尚未配置 AccessKey，请重新添加")
	}
	return a, b, nil
}
