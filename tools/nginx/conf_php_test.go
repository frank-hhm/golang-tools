package nginx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// genConf 用临时目录跑一遍 ensureConf，返回生成的 nginx.conf 文本。
// DocRoot 显式指向已存在目录，避免默认站点目录副作用；Version 留空使
// include 退回相对名（mime.types / fastcgi_params），断言只聚焦 PHP 处理块。
func genConf(t *testing.T, cfg InstanceConfig) string {
	t.Helper()
	prefix := t.TempDir()
	docRoot := t.TempDir()
	confDir := filepath.Join(prefix, "conf")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatal(err)
	}
	confPath := filepath.Join(confDir, "nginx.conf")
	cfg.ID = "test-inst"
	cfg.Name = "test-site"
	cfg.Port = 8080
	cfg.DocRoot = docRoot
	if err := ensureConf(&cfg, prefix, confPath); err != nil {
		t.Fatalf("ensureConf: %v", err)
	}
	data, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestEnsureConfPhpStatic 纯静态：try_files =404，无 fastcgi_pass / proxy_pass。
func TestEnsureConfPhpStatic(t *testing.T) {
	got := genConf(t, InstanceConfig{PhpMode: PhpModeStatic})
	if !strings.Contains(got, "try_files $uri $uri/ =404;") {
		t.Errorf("纯静态应含 =404 兜底，实得:\n%s", got)
	}
	if strings.Contains(got, "fastcgi_pass") || strings.Contains(got, "proxy_pass") {
		t.Errorf("纯静态不应出现 PHP 后端指令，实得:\n%s", got)
	}
	// 纯静态首页优先 index.html
	if !strings.Contains(got, "index  index.html index.php index.htm;") {
		t.Errorf("纯静态应 index.html 优先，实得:\n%s", got)
	}
}

// TestEnsureConfPhpFastCGI fastcgi 模式：写 fastcgi_pass 指向关联端口。
func TestEnsureConfPhpFastCGI(t *testing.T) {
	got := genConf(t, InstanceConfig{PhpMode: PhpModeFastCGI, PhpVersion: "7.3.33", PhpPort: 9000})
	if !strings.Contains(got, "fastcgi_pass   127.0.0.1:9000;") {
		t.Errorf("fastcgi 应生成 fastcgi_pass 后端指向，实得:\n%s", got)
	}
	if !strings.Contains(got, "fastcgi_param  SCRIPT_FILENAME $document_root$fastcgi_script_name;") {
		t.Errorf("fastcgi 应设置 SCRIPT_FILENAME，实得:\n%s", got)
	}
	if strings.Contains(got, "proxy_pass") {
		t.Errorf("fastcgi 模式不应出现 proxy_pass，实得:\n%s", got)
	}
	// PHP 站首页必须 index.php 优先，避开 ThinkPHP/Laravel 自带的 index.html 占位页
	if !strings.Contains(got, "index  index.php index.html index.htm;") {
		t.Errorf("fastcgi 应 index.php 优先，实得:\n%s", got)
	}
}

// TestEnsureConfPhpProxy proxy 模式：proxy_pass 到 php -S 后端。
func TestEnsureConfPhpProxy(t *testing.T) {
	got := genConf(t, InstanceConfig{PhpMode: PhpModeProxy, PhpVersion: "7.3.33", PhpPort: 8123})
	if !strings.Contains(got, "proxy_pass       http://127.0.0.1:8123;") {
		t.Errorf("proxy 应生成 proxy_pass 后端指向，实得:\n%s", got)
	}
	if !strings.Contains(got, "try_files $uri $uri/ /index.php?$query_string;") {
		t.Errorf("proxy 应用前端控制器兜底，实得:\n%s", got)
	}
	if strings.Contains(got, "fastcgi_pass") {
		t.Errorf("proxy 模式不应出现 fastcgi_pass，实得:\n%s", got)
	}
	if !strings.Contains(got, "index  index.php index.html index.htm;") {
		t.Errorf("proxy 应 index.php 优先，实得:\n%s", got)
	}
}

// TestEnsureConfPhpNoPortFallback 关联端口缺失（后端未建/未启动）时退回静态，
// 绝不能写出 127.0.0.1:0。
func TestEnsureConfPhpNoPortFallback(t *testing.T) {
	for _, mode := range []string{PhpModeFastCGI, PhpModeProxy} {
		got := genConf(t, InstanceConfig{PhpMode: mode, PhpVersion: "7.3.33", PhpPort: 0})
		if strings.Contains(got, "127.0.0.1:0") {
			t.Errorf("%s 未关联端口时不应写出 :0 后端，实得:\n%s", mode, got)
		}
		if !strings.Contains(got, "try_files $uri $uri/ =404;") {
			t.Errorf("%s 未关联端口应退回静态 =404，实得:\n%s", mode, got)
		}
	}
}
