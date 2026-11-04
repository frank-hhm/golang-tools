package term

// ============================================================
// SSH 凭据表（与主机解耦）
//
// 一条凭据 = 一种"登录方式"：账号 + 密码，或私钥（可带口令）。
// 多台主机可复用同一条凭据——一处改密，多机生效。
//
// 落盘：<data>/term/credentials.json；密码与私钥口令只存密文
// （protectSecret/unprotectSecret：Windows DPAPI，其他平台 plain: 前缀 + 0600），
// 回给前端的记录里密码/口令恒为空串，只有真正建连时才解密使用。
//
// 锁口径：本文件与 term.go 同用 storeMu 守护两份落盘文件（hosts.json /
// credentials.json）——迁移与悬挂引用清理都要同时碰两张表，单锁最省心，
// 不存在跨锁的加锁顺序问题。公有函数先锁再调用 *Locked 内部函数。
// ============================================================

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Credential 一条可复用凭据（前端列表/表单共用；Password/Passphrase 只在保存时带上来）
type Credential struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Auth       string `json:"auth"`                 // password | key
	User       string `json:"user,omitempty"`       // 默认 SSH 账号（主机留空即沿用）
	Password   string `json:"password,omitempty"`   // 明文只在此刻出现，落盘前即加密
	KeyPath    string `json:"keyPath,omitempty"`    // 私钥文件路径（auth=key）
	Passphrase string `json:"passphrase,omitempty"` // 私钥口令，同样只在保存时出现
	UpdatedAt  int64  `json:"updatedAt"`
}

// credRecord 落盘形态：密码与口令换成密文
type credRecord struct {
	Credential
	PassEnc       string `json:"passEnc,omitempty"`
	PassphraseEnc string `json:"passphraseEnc,omitempty"`
}

// credStorePath <data>/term/credentials.json
func credStorePath() (string, error) {
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials.json"), nil
}

// readCreds 读取凭据表（调用方持 storeMu）；文件不存在返回空表
func readCreds() ([]credRecord, error) {
	path, err := credStorePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []credRecord{}, nil
		}
		return nil, fmt.Errorf("读取凭据记录失败: %w", err)
	}
	recs := []credRecord{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &recs); err != nil {
			return nil, fmt.Errorf("凭据记录文件损坏: %w", err)
		}
	}
	return recs, nil
}

// writeCreds 原子落盘凭据表（调用方持 storeMu）
func writeCreds(recs []credRecord) error {
	path, err := credStorePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// toCredential 去掉密文，得到可安全交给前端的凭据
func (r credRecord) toCredential() Credential {
	c := r.Credential
	c.Password = ""
	c.Passphrase = ""
	return c
}

func credListOf(recs []credRecord) []Credential {
	out := make([]Credential, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.toCredential())
	}
	return out
}

// normalizeCred 表单清洗：去空白、定默认 auth、校验必填（返回 error 供 Save 使用）
func normalizeCred(in *Credential) error {
	in.Name = strings.TrimSpace(in.Name)
	in.User = strings.TrimSpace(in.User)
	in.KeyPath = strings.TrimSpace(in.KeyPath)
	if in.Auth != AuthKey {
		in.Auth = AuthPassword
	}
	if in.Auth == AuthKey {
		if in.KeyPath == "" {
			return fmt.Errorf("请选择私钥文件")
		}
		if _, err := os.Stat(in.KeyPath); err != nil {
			return fmt.Errorf("私钥文件不可读: %v", err)
		}
	}
	return nil
}

// ListCredentials 凭据列表（新建的排在前面）
func ListCredentials() ([]Credential, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	recs, err := readCreds()
	if err != nil {
		return nil, err
	}
	return credListOf(recs), nil
}

// SaveCredential 新建或更新一条凭据；密码/口令留空表示"沿用原值"。
// 返回更新后的完整列表。
func SaveCredential(in Credential) ([]Credential, error) {
	if err := normalizeCred(&in); err != nil {
		return nil, err
	}
	storeMu.Lock()
	defer storeMu.Unlock()
	recs, err := readCreds()
	if err != nil {
		return nil, err
	}
	in.UpdatedAt = time.Now().Unix()
	idx := -1
	for i := range recs {
		if recs[i].ID == in.ID {
			idx = i
			break
		}
	}
	rec := credRecord{Credential: in}
	if idx >= 0 {
		rec.ID = recs[idx].ID
		rec.PassEnc = recs[idx].PassEnc
		rec.PassphraseEnc = recs[idx].PassphraseEnc
	} else {
		if rec.ID == "" {
			rec.ID = fmt.Sprintf("c%d", time.Now().UnixNano())
		}
	}
	if in.Name == "" {
		if in.User != "" {
			rec.Name = in.User
		} else {
			rec.Name = fmt.Sprintf("凭据%d", len(recs)+1)
		}
	}
	if in.Password != "" {
		enc, err := protectSecret(in.Password)
		if err != nil {
			return nil, err
		}
		rec.PassEnc = enc
	} else if in.Auth == AuthKey {
		rec.PassEnc = ""
	}
	if in.Passphrase != "" {
		enc, err := protectSecret(in.Passphrase)
		if err != nil {
			return nil, err
		}
		rec.PassphraseEnc = enc
	} else if in.Auth != AuthKey {
		rec.PassphraseEnc = ""
	}
	if idx >= 0 {
		recs[idx] = rec
	} else {
		recs = append([]credRecord{rec}, recs...)
	}
	if err := writeCreds(recs); err != nil {
		return nil, fmt.Errorf("保存凭据失败: %w", err)
	}
	return credListOf(recs), nil
}

// DeleteCredential 删除凭据，并把引用它的主机的 credID 清空（悬挂引用清理）。
// 返回更新后的凭据列表。
func DeleteCredential(id string) ([]Credential, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	recs, err := readCreds()
	if err != nil {
		return nil, err
	}
	kept := make([]credRecord, 0, len(recs))
	for _, r := range recs {
		if r.ID != id {
			kept = append(kept, r)
		}
	}
	if len(kept) != len(recs) {
		if err := writeCreds(kept); err != nil {
			return nil, fmt.Errorf("保存凭据失败: %w", err)
		}
		if err := clearDanglingCredRef(id); err != nil {
			return nil, err
		}
	}
	return credListOf(kept), nil
}

// clearDanglingCredRef 清掉所有指向 id 的主机 credID（调用方持 storeMu）。
func clearDanglingCredRef(id string) error {
	recs, err := readRecords()
	if err != nil {
		return err
	}
	changed := false
	for i := range recs {
		if recs[i].CredID == id {
			recs[i].CredID = ""
			changed = true
		}
	}
	if changed {
		return writeRecords(recs)
	}
	return nil
}

// migrateHostCreds 把带密码/私钥但没有 credID 的旧主机记录升格为一条凭据（调用方持 storeMu）。
// 幂等：升格后回填 credID；凭据 id 用 "c"+hostID 确定性生成，重复迁移只更新不新增。
// 迁移失败不阻塞启动——原样返回未迁移的记录即可。
func migrateHostCreds(recs []hostRecord) []hostRecord {
	creds, err := readCreds()
	if err != nil {
		return recs
	}
	credChanged := false
	hostChanged := false
	for i := range recs {
		h := &recs[i]
		if h.CredID != "" {
			continue
		}
		if h.PassEnc == "" && h.KeyPath == "" {
			continue
		}
		cid := "c" + h.ID
		ci := -1
		for j := range creds {
			if creds[j].ID == cid {
				ci = j
				break
			}
		}
		if ci < 0 {
			nc := credRecord{Credential: Credential{
				ID:        cid,
				Name:      h.Name,
				Auth:      h.Auth,
				User:      h.User,
				KeyPath:   h.KeyPath,
				UpdatedAt: time.Now().Unix(),
			}}
			nc.PassEnc = h.PassEnc
			creds = append([]credRecord{nc}, creds...)
			credChanged = true
		} else {
			// 已存在（重复迁移）：补齐可能更新的密文与私钥路径
			creds[ci].PassEnc = h.PassEnc
			if h.KeyPath != "" {
				creds[ci].KeyPath = h.KeyPath
			}
			if creds[ci].Auth == "" {
				creds[ci].Auth = h.Auth
			}
			if creds[ci].User == "" {
				creds[ci].User = h.User
			}
			credChanged = true
		}
		h.CredID = cid
		hostChanged = true
	}
	if credChanged {
		_ = writeCreds(creds)
	}
	if hostChanged {
		_ = writeRecords(recs)
	}
	return recs
}

// normalizeTags 清洗标签：去空白、去重、保序（供 SaveHost 使用）。
func normalizeTags(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// connectSpec 建连所需的合并视图：凭据优先，主机旧字段兜底。
type connectSpec struct {
	Auth       string
	User       string
	Password   string // 明文，只在内存里存在这一小会儿
	KeyPath    string
	Passphrase string // 明文私钥口令
}

// applyCred 把一条凭据记录合并进 spec（非空字段覆盖；调用方持 storeMu）。
// 密文解密失败会原样返回错误（例如 Windows DPAPI 密文拿到别的平台）。
func applyCred(spec *connectSpec, c credRecord) error {
	if c.Auth != "" {
		spec.Auth = c.Auth
	}
	if c.User != "" {
		spec.User = c.User
	}
	if c.KeyPath != "" {
		spec.KeyPath = c.KeyPath
	}
	if c.PassEnc != "" {
		p, err := unprotectSecret(c.PassEnc)
		if err != nil {
			return err
		}
		spec.Password = p
	}
	if c.PassphraseEnc != "" {
		p, err := unprotectSecret(c.PassphraseEnc)
		if err != nil {
			return err
		}
		spec.Passphrase = p
	}
	return nil
}

// findCred 按 id 查一条凭据记录（调用方持 storeMu）。
func findCred(id string) (credRecord, bool, error) {
	creds, err := readCreds()
	if err != nil {
		return credRecord{}, false, err
	}
	for _, c := range creds {
		if c.ID == id {
			return c, true, nil
		}
	}
	return credRecord{}, false, nil
}

// resolve 取出一条已保存主机建连所需的信息（含解密后的口令）。
func resolve(id string) (Host, connectSpec, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	recs, err := readRecords()
	if err != nil {
		return Host{}, connectSpec{}, err
	}
	for _, r := range recs {
		if r.ID != id {
			continue
		}
		h := r.toHost()
		spec := connectSpec{Auth: h.Auth, User: h.User, KeyPath: h.KeyPath}
		if spec.Auth == "" {
			spec.Auth = AuthPassword
		}
		// 主机自身残留的旧密码（未迁移或未关联凭据时的兜底）
		if r.PassEnc != "" {
			if p, perr := unprotectSecret(r.PassEnc); perr == nil {
				spec.Password = p
			}
		}
		if h.CredID != "" {
			c, ok, cerr := findCred(h.CredID)
			if cerr != nil {
				return Host{}, connectSpec{}, cerr
			}
			if ok {
				if err := applyCred(&spec, c); err != nil {
					return Host{}, connectSpec{}, err
				}
			}
		}
		return h, spec, nil
	}
	return Host{}, connectSpec{}, fmt.Errorf("连接记录不存在，可能已被删除")
}

// specFromForm 由表单主机构造建连参数：内联字段 + 若关联凭据则解密凭据。
// 用于「测试连接」：主机可能尚未保存（id 为空），但凭据已落库，故只按 CredID 查凭据。
func specFromForm(h Host) (connectSpec, error) {
	spec := connectSpec{Auth: h.Auth, User: h.User, KeyPath: h.KeyPath, Password: h.Password}
	if spec.Auth == "" {
		spec.Auth = AuthPassword
	}
	if h.CredID == "" {
		return spec, nil
	}
	storeMu.Lock()
	defer storeMu.Unlock()
	c, ok, err := findCred(h.CredID)
	if err != nil {
		return connectSpec{}, err
	}
	if ok {
		if err := applyCred(&spec, c); err != nil {
			return connectSpec{}, err
		}
	}
	return spec, nil
}

// CredentialSpec 共享凭据的解密视图（FTP⇄term 归一用，字段语义同 connectSpec）。
// Password/Passphrase 为明文，只在内存里短暂存在，调用方不得落盘。
type CredentialSpec struct {
	Auth       string
	User       string
	Password   string
	KeyPath    string
	Passphrase string
}

// ResolveCredential 按 id 解出一条共享凭据（含明文口令）；tools/ftp 等外部包
// 经绑定层叠加使用，不跨包暴露内部 connectSpec。
func ResolveCredential(id string) (CredentialSpec, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	c, ok, err := findCred(id)
	if err != nil {
		return CredentialSpec{}, err
	}
	if !ok {
		return CredentialSpec{}, fmt.Errorf("凭据不存在，可能已被删除")
	}
	spec := connectSpec{}
	if err := applyCred(&spec, c); err != nil {
		return CredentialSpec{}, err
	}
	return CredentialSpec{Auth: spec.Auth, User: spec.User, Password: spec.Password, KeyPath: spec.KeyPath, Passphrase: spec.Passphrase}, nil
}

// ResolveHostSpec 按主机 id 取出建连视图（凭据优先、旧内联字段兜底，含明文）。
// 供绑定层做跨工具导入（终端主机→SFTP 连接）；明文只在内存短暂存在，不得落盘。
func ResolveHostSpec(id string) (Host, connectSpec, error) {
	return resolve(id)
}

// SaveHostFromForm 保存一条主机记录（表单形态，含旧字段内联兜底），返回落库后的记录。
// 供绑定层做跨工具导入（如 FTP 连接→终端主机）后落库。
func SaveHostFromForm(h Host) (Host, error) {
	list, err := SaveHost(h)
	if err != nil {
		return Host{}, err
	}
	for _, x := range list {
		if x.ID == h.ID || (h.ID == "" && x.Name == h.Name && x.Address == h.Address) {
			return x, nil
		}
	}
	return h, nil
}
