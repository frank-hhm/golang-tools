package datastore

// 含凭据那一组文档在离开本机前要过这一层。
//
// 做法是信封加密：一份随机 32 字节主密钥（AES-256-GCM）加密文档内容，主密钥本身交给
// 现有的 tools/secret 保护——Windows 上是 DPAPI（同一用户同一台机器才解得开），
// Linux 上退化为"base64 明文 + 文件 0600"，这一点 tools/secret 的注释里已经写明，
// 这里不重复也不粉饰：Linux 下这层不提供机密性，只挡住误读。
//
// 必须如实告知用户的边界：**主密钥只在本机**。换机器后远端那些 enc:v1: 开头的密文
// 解不开，跨机恢复只覆盖未加密那一组文档。设置页与提示文案都不许写成"加密了所以能安全
// 搬到别的机器"。

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"Tools/appdirs"
	"Tools/tools/secret"
)

// envelopePrefix 密文标记。带它才走解密，不带就原样返回——这样一份文档从"勾选加密"
// 改成"不勾"再同步过去，恢复端照样能读，不会因为多了一层前缀判断就把明文当密文解。
const envelopePrefix = "enc:v1:"

const masterKeyName = "datastore-master.key"

var (
	keyMu     sync.Mutex
	keyCached []byte
)

// masterKeyPath 主密钥落点：data/app/datastore-master.key。
func masterKeyPath() (string, error) {
	dir, err := appdirs.Data("app")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, masterKeyName), nil
}

// masterKey 取（必要时生成并落盘）本机主密钥，32 字节。
//
// 落盘的内容是 secret.Protect(base64(key)) 的结果，不是裸密钥；读回来解不开时报明确错误，
// 让人去重录而不是静默生成一把新的——静默新生成会让远端已有密文永远解不开，那才是真丢数据。
func masterKey() ([]byte, error) {
	keyMu.Lock()
	defer keyMu.Unlock()
	if len(keyCached) == 32 {
		return keyCached, nil
	}
	p, err := masterKeyPath()
	if err != nil {
		return nil, err
	}
	if raw, err := os.ReadFile(p); err == nil && len(raw) > 0 {
		plain, err := secret.Unprotect(string(raw))
		if err != nil {
			return nil, fmt.Errorf("外存主密钥解不开（换过 Windows 账号或换过机器？）：%w", err)
		}
		key, err := base64.StdEncoding.DecodeString(plain)
		if err != nil || len(key) != 32 {
			return nil, fmt.Errorf("外存主密钥文件内容异常：%s", p)
		}
		keyCached = key
		return keyCached, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	enc, err := secret.Protect(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		return nil, fmt.Errorf("保护外存主密钥失败: %w", err)
	}
	if err := os.WriteFile(p, []byte(enc), 0o600); err != nil {
		return nil, err
	}
	keyCached = key
	return keyCached, nil
}

// seal 明文 → envelopePrefix + base64(nonce || ciphertext)。
func seal(plain []byte) ([]byte, error) {
	key, err := masterKey()
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
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nonce, nonce, plain, nil)
	return []byte(envelopePrefix + base64.StdEncoding.EncodeToString(ct)), nil
}

// open 带前缀的解密，不带的原样返回。见 envelopePrefix 上方注释里的兼容考虑。
func open(data []byte) ([]byte, error) {
	if !isSealed(data) {
		return data, nil
	}
	raw, err := base64.StdEncoding.DecodeString(string(data[len(envelopePrefix):]))
	if err != nil {
		return nil, fmt.Errorf("外存密文格式错误: %w", err)
	}
	key, err := masterKey()
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
	if len(raw) < gcm.NonceSize() {
		return nil, fmt.Errorf("外存密文长度不足")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return nil, fmt.Errorf("外存密文解密失败（主密钥与加密时不是同一把）: %w", err)
	}
	return plain, nil
}

func isSealed(data []byte) bool {
	return len(data) > len(envelopePrefix) && string(data[:len(envelopePrefix)]) == envelopePrefix
}

// shouldSeal 判定一份文档写入外存前是否需要加密：只有"含凭据那一组"才加密，
// 普通配置文档保持明文——恢复端与手工排查都能直接看懂远端内容，这是副本该有的样子。
func shouldSeal(d doc) bool { return d.Sensitive }
