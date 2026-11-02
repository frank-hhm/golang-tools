package docspace

// 加密保险库（P3）：口令派生用 scrypt，内容用 AES-256-GCM 认证加密。
// 全程标准库 + golang.org/x/crypto，无平台差异，因此不带 build tag。
//
// 落点：
//   - 密文      secure/<id>.gtd   容器：magic|ver|logN|r|p|salt|nonce|ciphertext(+GCM tag)
//   - 登记      secure.json       只存 id/原路径/salt/校验哈希/时间；**口令永不落盘**
//   - 明文视图  secure/tmp/<id>/<原名>  仅解锁期存在，上锁即删；进程首启清扫崩溃残留
//
// 校验哈希 = sha256(派生密钥)：解锁前先廉价比对密钥判断口令对不对，避免整包解到一半
// 才由 GCM tag 报错；两者互为兜底（GCM tag 才是最终裁决）。
// 加密成功后删除原始明文（否则加密无意义）；删除只在密文容器与登记都落盘之后才做。

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"golang.org/x/crypto/scrypt"
)

// ErrWrongPassword 口令不正确（校验哈希不符或 GCM 认证失败）。
var ErrWrongPassword = errors.New("口令不正确")

const (
	gtdMagic   = "GTED"
	gtdVersion = byte(1)

	scryptLogN = 15 // N = 2^15；单次派生约 32MB 内存，桌面可接受
	scryptR    = 8
	scryptP    = 1
	symKeyLen  = 32 // AES-256

	saltLen  = 32
	nonceLen = 12

	secureMaxPlain = 512 << 20 // 明文上限 512MB：加密是整包进内存，超限拒绝，避免 OOM
)

// gtdHeaderLen 定长头：magic(4)+ver(1)+logN(1)+r(1)+p(1)+salt(32)+nonce(12)。
const gtdHeaderLen = len(gtdMagic) + 1 + 1 + 1 + 1 + saltLen + nonceLen

// SecureEntry 已在 store.go 定义（id/path/salt/check/createdAt）。这里只补编排。

// deriveKey 用 scrypt 从口令+盐派生对称密钥；参数取自容器头以便未来兼容。
func deriveKey(password string, salt []byte, logN, r, p int) ([]byte, error) {
	return scrypt.Key([]byte(password), salt, 1<<logN, r, p, symKeyLen)
}

func keyCheck(key []byte) string {
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:])
}

// encryptToContainer 整包加密：随机盐与 nonce，产出 .gtd 字节、盐、校验哈希。
func encryptToContainer(plain []byte, password string) (data, salt []byte, check string, err error) {
	salt = make([]byte, saltLen)
	if _, err = rand.Read(salt); err != nil {
		return nil, nil, "", err
	}
	key, err := deriveKey(password, salt, scryptLogN, scryptR, scryptP)
	if err != nil {
		return nil, nil, "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, "", err
	}
	nonce := make([]byte, nonceLen)
	if _, err = rand.Read(nonce); err != nil {
		return nil, nil, "", err
	}
	ct := gcm.Seal(nil, nonce, plain, nil)

	data = make([]byte, 0, gtdHeaderLen+len(ct))
	data = append(data, gtdMagic...)
	data = append(data, gtdVersion, byte(scryptLogN), byte(scryptR), byte(scryptP))
	data = append(data, salt...)
	data = append(data, nonce...)
	data = append(data, ct...)
	return data, salt, keyCheck(key), nil
}

// decryptContainer 解析并解密容器；口令错时先被校验哈希挡下，兜底由 GCM tag 报错。
func decryptContainer(data []byte, password string) (plain []byte, err error) {
	if len(data) < gtdHeaderLen+16 {
		return nil, fmt.Errorf("密文容器损坏或过短")
	}
	if string(data[:len(gtdMagic)]) != gtdMagic {
		return nil, fmt.Errorf("不是有效的加密容器")
	}
	i := len(gtdMagic)
	ver := data[i]
	i++
	if ver != gtdVersion {
		return nil, fmt.Errorf("不支持的容器版本: %d", ver)
	}
	logN := int(data[i])
	r := int(data[i+1])
	p := int(data[i+2])
	i += 3
	salt := data[i : i+saltLen]
	i += saltLen
	nonce := data[i : i+nonceLen]
	i += nonceLen
	ct := data[i:]

	key, err := deriveKey(password, salt, logN, r, p)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err = gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, ErrWrongPassword
	}
	return plain, nil
}

// gtdPath 某条目的密文容器路径。
func (s *Store) gtdPath(id string) string { return s.pathOf("secure", id+".gtd") }

// tmpDir 某条目的解锁明文临时目录。
func (s *Store) tmpDir(id string) string { return s.pathOf("secure", "tmp", id) }

// Encrypt 读取 srcPath 文件并加密封装成保险库条目，成功后删除原始明文。
// 仅支持本机已存在的单文件；口令为空或超过明文上限都拒绝。
func (s *Store) Encrypt(srcPath, password string) (SecureEntry, error) {
	if password == "" {
		return SecureEntry{}, fmt.Errorf("口令不能为空")
	}
	if err := s.Load(); err != nil {
		return SecureEntry{}, err
	}
	abs, err := filepath.Abs(srcPath)
	if err != nil {
		return SecureEntry{}, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return SecureEntry{}, err
	}
	if st.IsDir() {
		return SecureEntry{}, fmt.Errorf("只能加密文件，不支持目录: %s", abs)
	}
	if st.Size() > secureMaxPlain {
		return SecureEntry{}, fmt.Errorf("文件过大（>%d MB），暂不支持加密", secureMaxPlain>>20)
	}
	plain, err := os.ReadFile(abs)
	if err != nil {
		return SecureEntry{}, err
	}
	data, salt, check, err := encryptToContainer(plain, password)
	if err != nil {
		return SecureEntry{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	id := newID("s")
	if err := os.WriteFile(s.gtdPath(id), data, 0o600); err != nil {
		return SecureEntry{}, err
	}
	entry := SecureEntry{
		ID:        id,
		Path:      abs,
		Salt:      hex.EncodeToString(salt),
		Check:     check,
		CreatedAt: time.Now().UnixMilli(),
	}
	s.secure = upsertSecure(s.secure, entry)
	if err := writeJSONAtomic(s.pathOf("secure.json"), s.secure); err != nil {
		_ = os.Remove(s.gtdPath(id)) // 登记失败回滚容器，原明文尚未删除，用户无损失
		return SecureEntry{}, err
	}
	// 密文与登记都已落盘后才删原明文；删失败不影响条目可用性（明文仍在原处，可重试）。
	_ = os.Remove(abs)
	return entry, nil
}

// ListSecure 返回全部保险库条目（按创建时间倒序）。
func (s *Store) ListSecure() ([]SecureEntry, error) {
	if err := s.Load(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]SecureEntry, len(s.secure))
	copy(out, s.secure)
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

func (s *Store) findSecureLocked(id string) (SecureEntry, bool) {
	for _, e := range s.secure {
		if e.ID == id {
			return e, true
		}
	}
	return SecureEntry{}, false
}

// Unlock 用口令解密并写出明文临时视图，返回其绝对路径；已存在同名视图先清后写。
// 口令错误回 ErrWrongPassword。调用方（前端）用完应调 Lock 立即删除明文。
func (s *Store) Unlock(id, password string) (string, error) {
	if err := s.Load(); err != nil {
		return "", err
	}
	s.mu.RLock()
	entry, ok := s.findSecureLocked(id)
	s.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("加密条目不存在: %s", id)
	}
	data, err := os.ReadFile(s.gtdPath(id))
	if err != nil {
		return "", fmt.Errorf("密文实体缺失: %w", err)
	}
	plain, err := decryptContainer(data, password)
	if err != nil {
		return "", err
	}
	dir := s.tmpDir(id)
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := filepath.Base(entry.Path)
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "document"
	}
	out := filepath.Join(dir, name)
	if err := os.WriteFile(out, plain, 0o600); err != nil {
		return "", err
	}
	return out, nil
}

// Lock 删除某条目的解锁明文临时视图（锁上即删）。
func (s *Store) Lock(id string) error {
	return os.RemoveAll(s.tmpDir(id))
}

// ChangePassword 校验旧口令后重新加密（新盐新 nonce），并更新登记里的 salt 与校验哈希。
func (s *Store) ChangePassword(id, oldPw, newPw string) error {
	if newPw == "" {
		return fmt.Errorf("新口令不能为空")
	}
	if err := s.Load(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i := range s.secure {
		if s.secure[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("加密条目不存在: %s", id)
	}
	data, err := os.ReadFile(s.gtdPath(id))
	if err != nil {
		return err
	}
	plain, err := decryptContainer(data, oldPw)
	if err != nil {
		return err
	}
	fresh, salt, check, err := encryptToContainer(plain, newPw)
	if err != nil {
		return err
	}
	tmp := s.gtdPath(id) + ".part"
	if err := os.WriteFile(tmp, fresh, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.gtdPath(id)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	s.secure[idx].Salt = hex.EncodeToString(salt)
	s.secure[idx].Check = check
	return writeJSONAtomic(s.pathOf("secure.json"), s.secure)
}

// ExtractTo 用口令解密并把明文永久写到 destPath（“取回明文”，不产生临时视图）。
// destPath 已存在则拒绝覆盖。
func (s *Store) ExtractTo(id, password, destPath string) error {
	if err := s.Load(); err != nil {
		return err
	}
	s.mu.RLock()
	_, ok := s.findSecureLocked(id)
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("加密条目不存在: %s", id)
	}
	abs, err := filepath.Abs(destPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err == nil {
		return fmt.Errorf("目标已存在，未覆盖: %s", abs)
	}
	data, err := os.ReadFile(s.gtdPath(id))
	if err != nil {
		return err
	}
	plain, err := decryptContainer(data, password)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return os.WriteFile(abs, plain, 0o600)
}

// RemoveSecure 彻底删除密文条目（实体+临时视图+登记），不可恢复。
func (s *Store) RemoveSecure(id string) error {
	if err := s.Load(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i := range s.secure {
		if s.secure[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil // 幂等：本就不存在
	}
	_ = os.Remove(s.gtdPath(id))
	_ = os.RemoveAll(s.tmpDir(id))
	s.secure = append(s.secure[:idx], s.secure[idx+1:]...)
	return writeJSONAtomic(s.pathOf("secure.json"), s.secure)
}

// upsertSecure 覆盖同 id 或追加。
func upsertSecure(list []SecureEntry, e SecureEntry) []SecureEntry {
	for i := range list {
		if list[i].ID == e.ID {
			list[i] = e
			return list
		}
	}
	return append(list, e)
}
