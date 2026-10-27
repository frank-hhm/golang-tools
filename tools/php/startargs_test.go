package php

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestPhpStartArgs 校验两种运行模式的启动参数拼装（不实际起进程）：
// cgi=true 走 php-cgi -b（TCP FastCGI，不带 -t/-d router，docroot 由 nginx 传入）；
// 否则 php -S 内置 HTTP 服务器（-t 指 docroot，可选路由脚本追加在末尾）。
func TestPhpStartArgs(t *testing.T) {
	const ini = `/data/inst/php.ini`

	t.Run("cgi", func(t *testing.T) {
		got := phpStartArgs(true, 9000, "/www/site", ini, "router.php")
		want := []string{"-b", "127.0.0.1:9000", "-c", ini}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("cgi 参数 = %v, 期望 %v（cgi 模式不应带 -t/router）", got, want)
		}
	})

	t.Run("server", func(t *testing.T) {
		got := phpStartArgs(false, 8080, "/www/site", ini, "")
		want := []string{"-S", "127.0.0.1:8080", "-t", "/www/site", "-c", ini}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("server 参数 = %v, 期望 %v", got, want)
		}
	})

	t.Run("serverRouterRelative", func(t *testing.T) {
		got := phpStartArgs(false, 8080, "/www/site", ini, "router.php")
		want := []string{"-S", "127.0.0.1:8080", "-t", "/www/site", "-c", ini, filepath.Join("/www/site", "router.php")}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("相对路由脚本应拼接 docroot: got %v, want %v", got, want)
		}
	})

	t.Run("serverRouterAbs", func(t *testing.T) {
		// 用当前平台的绝对路径（Windows 需盘符），否则 filepath.IsAbs 判 false 会被拼接
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		absDoc := filepath.Join(cwd, "site")
		absRouter := filepath.Join(cwd, "router.php")
		got := phpStartArgs(false, 8080, absDoc, ini, absRouter)
		want := []string{"-S", "127.0.0.1:8080", "-t", absDoc, "-c", ini, absRouter}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("绝对路由脚本应原样保留: got %v, want %v", got, want)
		}
	})
}
