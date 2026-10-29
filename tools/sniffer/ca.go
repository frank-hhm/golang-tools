package sniffer

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CACommonName 根证书通用名，用于安装/卸载时识别
const CACommonName = "Tools Sniffer CA"

// CA 持有自签根证书与私钥，并按域名签发叶子证书（带缓存）
type CA struct {
	cert    *x509.Certificate
	key     *rsa.PrivateKey
	certPEM []byte
	dir     string

	mu    sync.Mutex
	cache map[string]*tls.Certificate
}

// CertDir 返回证书存放目录（用户配置目录下），必要时创建
func CertDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "Tools", "sniffer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// CertPath 返回根证书文件路径（供系统安装使用）
func (ca *CA) CertPath() string {
	return filepath.Join(ca.dir, "ca.crt")
}

// LoadOrGenerateCA 加载已有根证书，不存在则生成并持久化
func LoadOrGenerateCA() (*CA, error) {
	dir, err := CertDir()
	if err != nil {
		return nil, err
	}
	keyPath := filepath.Join(dir, "ca.key")
	certPath := filepath.Join(dir, "ca.crt")

	ca := &CA{dir: dir, cache: make(map[string]*tls.Certificate)}

	// 尝试加载
	if keyPEM, kerr := os.ReadFile(keyPath); kerr == nil {
		if certPEM, cerr := os.ReadFile(certPath); cerr == nil {
			keyBlock, _ := pem.Decode(keyPEM)
			certBlock, _ := pem.Decode(certPEM)
			if keyBlock != nil && certBlock != nil {
				parsedKey, kerr2 := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
				parsedCert, cerr2 := x509.ParseCertificate(certBlock.Bytes)
				if kerr2 == nil && cerr2 == nil {
					ca.key = parsedKey
					ca.cert = parsedCert
					ca.certPEM = certPEM
					return ca, nil
				}
			}
		}
	}

	// 生成新的根 CA
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: CACommonName, Organization: []string{"Tools"}},
		NotBefore:             time.Now().Add(-24 * time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	certDER, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return nil, err
	}

	ca.key = key
	ca.cert = certDER
	ca.certPEM = certPEM
	return ca, nil
}

// CertForHost 为指定域名/IP 签发叶子证书（缓存复用）
func (ca *CA) CertForHost(host string) (*tls.Certificate, error) {
	ca.mu.Lock()
	defer ca.mu.Unlock()
	if c, ok := ca.cache[host]; ok {
		return c, nil
	}

	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host, Organization: []string{"Tools"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(825 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		// 泛域名根也一并签发，便于子域命中
		tmpl.DNSNames = []string{host}
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &leafKey.PublicKey, ca.key)
	if err != nil {
		return nil, fmt.Errorf("签发叶子证书失败: %w", err)
	}
	cert := &tls.Certificate{
		Certificate: [][]byte{der, ca.cert.Raw},
		PrivateKey:  leafKey,
	}
	ca.cache[host] = cert
	return cert, nil
}
