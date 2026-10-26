package panel

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const (
	// sessionCookie 登录态 Cookie（HttpOnly，前端 JS 读不到，避免 XSS 直接偷走）
	sessionCookie = "gt_panel_session"
	// entryCookie 安全入口通过标记：访问过入口路径才发，用于区分"知道入口的人"与扫描器
	entryCookie = "gt_panel_entry"
	// entryCookieTTL 入口标记有效期
	entryCookieTTL = 30 * 24 * time.Hour
)

// sessionPayload 是 Cookie 里真正签名保护的载荷。
// Epoch 与配置里的 Epoch 比对：改密码后自增，从而使所有旧 Cookie 立即失效。
type sessionPayload struct {
	User  string `json:"u"`
	Exp   int64  `json:"e"`
	Epoch int    `json:"p"`
}

// issueSession 生成签名 Cookie 值。
func issueSession(s Settings, ttl time.Duration) (string, error) {
	p := sessionPayload{User: s.Username, Exp: time.Now().Add(ttl).Unix(), Epoch: s.Epoch}
	body, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding.EncodeToString(body)
	return enc + "." + sign(s.Secret, enc), nil
}

// verifySession 校验签名、有效期与会话代数，通过则返回用户名。
func verifySession(s Settings, raw string) (string, bool) {
	enc, sig, ok := strings.Cut(raw, ".")
	if !ok || enc == "" || sig == "" {
		return "", false
	}
	if !hmac.Equal([]byte(sig), []byte(sign(s.Secret, enc))) {
		return "", false
	}
	body, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return "", false
	}
	var p sessionPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return "", false
	}
	// 会话代数或账号对不上都算失效：改密码/改账号后旧 Cookie 立刻作废
	if p.Epoch != s.Epoch || p.User != s.Username {
		return "", false
	}
	if time.Now().Unix() >= p.Exp {
		return "", false
	}
	return p.User, true
}

func sign(secret, payload string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(payload))
	return hex.EncodeToString(m.Sum(nil))
}

// setSessionCookie 写登录 Cookie。
func setSessionCookie(w http.ResponseWriter, s Settings, ttl time.Duration) {
	val, err := issueSession(s, ttl)
	if err != nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    val,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   false, // 面板常以 http + 反向代理部署；由代理层负责 TLS
	})
}

// setEntryCookie 写安全入口通过标记（同样签名，防止伪造）。
func setEntryCookie(w http.ResponseWriter, s Settings, ok bool) {
	if !ok {
		http.SetCookie(w, &http.Cookie{
			Name:     entryCookie,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		return
	}
	val := sign(s.Secret, "entry:"+s.EntryPath)
	http.SetCookie(w, &http.Cookie{
		Name:     entryCookie,
		Value:    val,
		Path:     "/",
		MaxAge:   int(entryCookieTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// hasEntryCookie 判断请求是否带着有效的入口标记。
func hasEntryCookie(r *http.Request, s Settings) bool {
	if s.EntryPath == "" {
		return true
	}
	c, err := r.Cookie(entryCookie)
	if err != nil {
		return false
	}
	return hmac.Equal([]byte(c.Value), []byte(sign(s.Secret, "entry:"+s.EntryPath)))
}

// clearSession 删除登录 Cookie。
func clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// sessionTTL 登录有效期（从配置换算）。
func sessionTTL(s Settings) time.Duration {
	h := s.SessionHours
	if h <= 0 {
		h = defaultSessionHours
	}
	return time.Duration(h) * time.Hour
}
