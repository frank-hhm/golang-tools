package sshgen

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

// Generate 生成一把密钥对并成对落盘（私钥 0600、公钥 0644），返回路径与公钥信息。
// 私钥正文不回传；passphrase 仅用于本次派生，不写进任何返回值或缓存。
func Generate(o KeyOptions) (KeyPair, error) {
	typ := strings.ToLower(strings.TrimSpace(o.Type))
	if typ == "" {
		typ = "ed25519"
	}

	var priv any
	switch typ {
	case "ed25519":
		_, p, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return KeyPair{}, fmt.Errorf("生成 ed25519 失败: %w", err)
		}
		priv = p
	case "rsa":
		bits := o.Bits
		if bits == 0 {
			bits = 3072
		}
		if bits != 2048 && bits != 3072 && bits != 4096 {
			return KeyPair{}, fmt.Errorf("RSA 位数仅支持 2048/3072/4096，得到 %d", o.Bits)
		}
		p, err := rsa.GenerateKey(rand.Reader, bits)
		if err != nil {
			return KeyPair{}, fmt.Errorf("生成 RSA 失败: %w", err)
		}
		priv = p
	case "ecdsa":
		curve, err := curveFor(o.Bits)
		if err != nil {
			return KeyPair{}, err
		}
		p, err := ecdsa.GenerateKey(curve, rand.Reader)
		if err != nil {
			return KeyPair{}, fmt.Errorf("生成 ECDSA 失败: %w", err)
		}
		priv = p
	default:
		return KeyPair{}, fmt.Errorf("未知密钥类型: %q", o.Type)
	}

	comment := strings.TrimSpace(o.Comment)
	if comment == "" {
		comment = defaultComment()
	}

	var (
		block *pem.Block
		err   error
	)
	if strings.TrimSpace(o.Passphrase) != "" {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, comment, []byte(o.Passphrase))
	} else {
		block, err = ssh.MarshalPrivateKey(priv, comment)
	}
	if err != nil {
		return KeyPair{}, fmt.Errorf("序列化私钥失败: %w", err)
	}
	pemBytes := pem.EncodeToMemory(block)

	sshPub, err := ssh.NewPublicKey(publicOf(priv))
	if err != nil {
		return KeyPair{}, fmt.Errorf("构造公钥失败: %w", err)
	}
	authKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	pubLine := authKey
	if comment != "" {
		pubLine = authKey + " " + comment
	}

	dir := strings.TrimSpace(o.Dir)
	if dir == "" {
		if dir, err = DefaultDir(); err != nil {
			return KeyPair{}, err
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return KeyPair{}, fmt.Errorf("创建目录失败: %w", err)
	}
	base := strings.TrimSpace(o.Name)
	if base == "" {
		base = "id_" + typ
	}
	base = uniqueBase(dir, base)
	privPath := filepath.Join(dir, base)
	pubPath := privPath + ".pub"

	if err := os.WriteFile(privPath, pemBytes, 0o600); err != nil {
		return KeyPair{}, fmt.Errorf("写私钥失败: %w", err)
	}
	if err := os.WriteFile(pubPath, []byte(pubLine+"\n"), 0o644); err != nil {
		return KeyPair{}, fmt.Errorf("写公钥失败: %w", err)
	}

	return KeyPair{
		Type:              typ,
		Comment:           comment,
		PrivPath:          privPath,
		PubPath:           pubPath,
		PubKeyLine:        pubLine,
		FingerprintSHA256: ssh.FingerprintSHA256(sshPub),
		FingerprintMD5:    ssh.FingerprintLegacyMD5(sshPub),
	}, nil
}

// curveFor 把位数映射到 ECDSA 曲线；0 默认 P256。
func curveFor(bits int) (elliptic.Curve, error) {
	switch bits {
	case 0, 256:
		return elliptic.P256(), nil
	case 384:
		return elliptic.P384(), nil
	case 521:
		return elliptic.P521(), nil
	default:
		return nil, fmt.Errorf("ECDSA 曲线仅支持 256/384/521，得到 %d", bits)
	}
}

// publicOf 从私钥取公钥（供 ssh.NewPublicKey）。
func publicOf(priv any) any {
	switch p := priv.(type) {
	case ed25519.PrivateKey:
		return p.Public()
	case *rsa.PrivateKey:
		return &p.PublicKey
	case *ecdsa.PrivateKey:
		return &p.PublicKey
	default:
		return nil
	}
}

// defaultComment 缺省注释：user@host，取不到就退回 golang-tools。
func defaultComment() string {
	u := os.Getenv("USER")
	if u == "" {
		u = os.Getenv("USERNAME")
	}
	h, _ := os.Hostname()
	switch {
	case u != "" && h != "":
		return u + "@" + h
	case u != "":
		return u
	default:
		return "golang-tools"
	}
}

// uniqueBase 重名避让：确保 <base> 与 <base>.pub 都不存在，冲突则追加 -1/-2…。
func uniqueBase(dir, name string) string {
	for i := 0; ; i++ {
		cand := name
		if i > 0 {
			cand = fmt.Sprintf("%s-%d", name, i)
		}
		if !pathExists(filepath.Join(dir, cand)) && !pathExists(filepath.Join(dir, cand+".pub")) {
			return cand
		}
	}
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
