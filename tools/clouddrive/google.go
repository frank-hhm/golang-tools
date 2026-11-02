package clouddrive

// Google Drive v3 适配器（官方 OAuth2 + REST，PR1 唯一落地的云）。
//
// 授权走「用户粘贴回跳 URL/代码」的手工流程，桌面与无头面板同一套：
//   AuthURLFor() 生成 consent URL（access_type=offline 拿 refresh_token）→ 用户浏览器
//   授权后把回跳地址整串（或 code= 之后的值）粘回来 → Exchange() 换 token → 存 cred。
//   之后每次调用 ensureToken 自动用 refresh_token 续期。client_id/secret 由用户在
//   设置里自备（config.Cloud），不内置。
//
// 不用 golang.org/x/oauth2：仓库依赖树里没有它，引入要联网拉包，且 google 子包还会
// 拽进 compute metadata。手工用 net/http 走标准 OAuth2 授权码流程反而更轻、可 httptest
// 测、跨平台零 cgo。端点做成包级变量，单测把 base 指到 httptest server。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"Tools/services/config"
)

// AuthURL 为某 Google 账号生成授权 URL（供用户点开授权）。
func AuthURL(acctID, state string) (string, error) {
	g, err := newGoogle(acctID)
	if err != nil {
		return "", err
	}
	return g.AuthURL(state)
}

// ExchangeCode 用授权码/回跳 URL 为某 Google 账号换取并保存 token。
func ExchangeCode(ctx context.Context, acctID, codeOrRedirect string) error {
	g, err := newGoogle(acctID)
	if err != nil {
		return err
	}
	return g.Exchange(ctx, codeOrRedirect)
}

// ClientReady 用户是否已填 Google OAuth 客户端凭证（前端据此提示先去设置）。
func ClientReady() bool {
	cfg := config.CloudCfg()
	return strings.TrimSpace(cfg.GoogleClientID) != "" && strings.TrimSpace(cfg.GoogleClientSecret) != ""
}

// 端点（测试里改这几个变量即可把流量引到 httptest）。
var (
	googleAuthURL    = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL   = "https://oauth2.googleapis.com/token"
	driveBase        = "https://www.googleapis.com/drive/v3"
	driveUploadBase  = "https://www.googleapis.com/upload/drive/v3"
	driveWebBase     = "https://drive.google.com"
	googleScopeDrive = "https://www.googleapis.com/auth/drive"
)

// gFolderMime Drive 里目录的 mimeType。
const gFolderMime = "application/vnd.google-apps.folder"

func init() {
	register(KindGoogle, func(acctID string) (Provider, error) { return newGoogle(acctID) })
}

// googleProvider 实现 Provider。无状态：每次操作从账号表取 token、按需续期。
type googleProvider struct {
	acctID   string
	clientID string
	secret   string
	redirect string
	hc       *http.Client
}

func newGoogle(acctID string) (*googleProvider, error) {
	cfg := config.CloudCfg()
	return &googleProvider{
		acctID:   acctID,
		clientID: strings.TrimSpace(cfg.GoogleClientID),
		secret:   strings.TrimSpace(cfg.GoogleClientSecret),
		redirect: strings.TrimSpace(cfg.GoogleRedirectURI),
		hc:       &http.Client{},
	}, nil
}

func (g *googleProvider) Name() string { return "Google Drive" }

func (g *googleProvider) requireClient() error {
	if g.clientID == "" || g.secret == "" {
		return fmt.Errorf("未配置 Google OAuth 客户端 ID/密钥（设置 → 云盘管理）")
	}
	return nil
}

/* ============================ OAuth2 ============================ */

// AuthURL 生成授权 URL，供用户点开后在 Google 页授权（access_type=offline 才给 refresh_token）。
func (g *googleProvider) AuthURL(state string) (string, error) {
	if err := g.requireClient(); err != nil {
		return "", err
	}
	q := url.Values{}
	q.Set("client_id", g.clientID)
	q.Set("redirect_uri", g.redirect)
	q.Set("response_type", "code")
	q.Set("scope", googleScopeDrive)
	q.Set("access_type", "offline")
	q.Set("prompt", "consent") // consent 确保每次都回传 refresh_token（首次或过期后）
	if state != "" {
		q.Set("state", state)
	}
	return googleAuthURL + "?" + q.Encode(), nil
}

type tokenResp struct {
	Access    string `json:"access_token"`
	Refresh   string `json:"refresh_token"`
	ExpiresIn int    `json:"expires_in"`
	Err       string `json:"error"`
	ErrDesc   string `json:"error_description"`
}

// Exchange 用授权码换 token 并落 cred。codeOrRedirect 允许直接粘整串回跳 URL。
func (g *googleProvider) Exchange(ctx context.Context, codeOrRedirect string) error {
	if err := g.requireClient(); err != nil {
		return err
	}
	code := extractCode(codeOrRedirect)
	if code == "" {
		return fmt.Errorf("未从回跳地址里解析出授权码")
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", g.clientID)
	form.Set("client_secret", g.secret)
	form.Set("redirect_uri", g.redirect)
	tr, err := g.postToken(ctx, form)
	if err != nil {
		return err
	}
	blob := tokenBlob{Access: tr.Access, Refresh: tr.Refresh, Expiry: expiryOf(tr.ExpiresIn)}
	if blob.Refresh == "" {
		// consent 档一般会给；万一没给，保留旧的 refresh（覆盖 access 即可）
		if old, err := loadToken(g.acctID); err == nil {
			blob.Refresh = old.Refresh
		}
	}
	return saveToken(g.acctID, blob)
}

// ensureToken 返回可用的 access token，临期自动用 refresh_token 续期。
func (g *googleProvider) ensureToken(ctx context.Context) (string, error) {
	b, err := loadToken(g.acctID)
	if err != nil {
		return "", err
	}
	// 还有 >60s 有效就直接用；否则尝试刷新。
	if b.Access != "" && (b.Expiry == 0 || b.Expiry-time.Now().UnixMilli() > 60_000) {
		return b.Access, nil
	}
	if b.Refresh == "" {
		if b.Access != "" {
			return b.Access, nil
		}
		return "", fmt.Errorf("无有效令牌，请重新授权")
	}
	if err := g.requireClient(); err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", b.Refresh)
	form.Set("client_id", g.clientID)
	form.Set("client_secret", g.secret)
	tr, err := g.postToken(ctx, form)
	if err != nil {
		return "", err
	}
	nb := tokenBlob{Access: tr.Access, Refresh: b.Refresh, Expiry: expiryOf(tr.ExpiresIn)}
	if tr.Refresh != "" {
		nb.Refresh = tr.Refresh
	}
	if err := saveToken(g.acctID, nb); err != nil {
		return "", err
	}
	return nb.Access, nil
}

func (g *googleProvider) postToken(ctx context.Context, form url.Values) (*tokenResp, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("令牌请求失败: %w", err)
	}
	defer resp.Body.Close()
	bs, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var tr tokenResp
	_ = json.Unmarshal(bs, &tr)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := tr.ErrDesc
		if msg == "" {
			msg = tr.Err
		}
		if msg == "" {
			msg = string(bs)
		}
		return nil, fmt.Errorf("令牌交换失败(%d): %s", resp.StatusCode, msg)
	}
	if tr.Access == "" {
		return nil, fmt.Errorf("令牌响应缺少 access_token")
	}
	return &tr, nil
}

// extractCode 从整串回跳 URL 或裸 code 里取出授权码。
func extractCode(s string) string {
	s = strings.TrimSpace(s)
	if u, err := url.Parse(s); err == nil && u.Query().Get("code") != "" {
		return u.Query().Get("code")
	}
	// 可能粘的是被截断/非标准 URL：手工找 code=
	if i := strings.Index(s, "code="); i >= 0 {
		rest := s[i+len("code="):]
		if j := strings.IndexAny(rest, "& \t\n"); j >= 0 {
			return rest[:j]
		}
		return rest
	}
	if strings.ContainsAny(s, "/?=&") {
		return "" // 看着像 URL 但没有 code=
	}
	return s // 就当裸授权码
}

func expiryOf(in int) int64 {
	if in <= 0 {
		return 0
	}
	return time.Now().Add(time.Duration(in) * time.Second).UnixMilli()
}

/* ============================ Drive API ============================ */

// gFile Drive 文件的 JSON 形态（只取用到的字段）。
type gFile struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	MimeType      string   `json:"mimeType"`
	Size          string   `json:"size"` // 数字字符串；文件夹无此字段
	ModifiedTime  string   `json:"modifiedTime"`
	Parents       []string `json:"parents"`
	ThumbnailLink string   `json:"thumbnailLink"`
}

func (f gFile) toEntry() Entry {
	size, _ := strconv.ParseInt(f.Size, 10, 64)
	var mt int64
	if t, err := time.Parse(time.RFC3339, f.ModifiedTime); err == nil {
		mt = t.UnixMilli()
	}
	return Entry{
		ID: f.ID, Name: f.Name, Path: f.Name,
		IsDir: f.MimeType == gFolderMime,
		Size:  size, MTime: mt, MIME: f.MimeType,
		ThumbURL: f.ThumbnailLink,
	}
}

const listFields = "files(id,name,mimeType,size,modifiedTime,parents,thumbnailLink)"

func (g *googleProvider) do(ctx context.Context, method, u string, body []byte, contentType string, out any) error {
	tok, err := g.ensureToken(ctx)
	if err != nil {
		return err
	}
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := g.hc.Do(req)
	if err != nil {
		return fmt.Errorf("云盘请求失败: %w", err)
	}
	defer resp.Body.Close()
	bs, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("云盘返回 %d：%s", resp.StatusCode, apiErrMsg(bs))
	}
	if out != nil && len(bs) > 0 {
		if err := json.Unmarshal(bs, out); err != nil {
			return fmt.Errorf("解析云盘响应失败: %w", err)
		}
	}
	return nil
}

func apiErrMsg(bs []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(bs, &e) == nil && e.Error.Message != "" {
		return e.Error.Message
	}
	s := strings.TrimSpace(string(bs))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	if s == "" {
		return "无详情"
	}
	return s
}

func (g *googleProvider) Test(ctx context.Context) error {
	_, err := g.Quota(ctx)
	return err
}

func (g *googleProvider) List(ctx context.Context, dirID string) ([]Entry, error) {
	parent := "root"
	if dirID != "" {
		parent = dirID
	}
	q := url.Values{}
	q.Set("q", fmt.Sprintf("'%s' in parents and trashed=false", parent))
	q.Set("fields", listFields)
	q.Set("pageSize", "1000")
	var out struct {
		Files []gFile `json:"files"`
	}
	if err := g.do(ctx, http.MethodGet, driveBase+"/files?"+q.Encode(), nil, "", &out); err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(out.Files))
	for _, f := range out.Files {
		entries = append(entries, f.toEntry())
	}
	return entries, nil
}

func (g *googleProvider) Mkdir(ctx context.Context, parentID, name string) (Entry, error) {
	if parentID == "" {
		parentID = "root"
	}
	meta := map[string]any{
		"name":     name,
		"mimeType": gFolderMime,
		"parents":  []string{parentID},
	}
	bs, _ := json.Marshal(meta)
	var f gFile
	if err := g.do(ctx, http.MethodPost, driveBase+"/files?fields="+url.QueryEscape("id,name,mimeType,modifiedTime"), bs, "application/json; charset=UTF-8", &f); err != nil {
		return Entry{}, err
	}
	return f.toEntry(), nil
}

func (g *googleProvider) Rename(ctx context.Context, id, newName string) error {
	bs, _ := json.Marshal(map[string]any{"name": newName})
	return g.do(ctx, http.MethodPatch, driveBase+"/files/"+url.PathEscape(id), bs, "application/json; charset=UTF-8", nil)
}

func (g *googleProvider) Move(ctx context.Context, id, dstParentID string) error {
	if dstParentID == "" {
		dstParentID = "root"
	}
	// 取当前 parents 以便 removeParents。
	var cur gFile
	if err := g.do(ctx, http.MethodGet, driveBase+"/files/"+url.PathEscape(id)+"?fields=parents", nil, "", &cur); err != nil {
		return err
	}
	q := url.Values{}
	q.Set("addParents", dstParentID)
	if len(cur.Parents) > 0 {
		q.Set("removeParents", strings.Join(cur.Parents, ","))
	}
	u := driveBase + "/files/" + url.PathEscape(id) + "?" + q.Encode()
	return g.do(ctx, http.MethodPatch, u, []byte("{}"), "application/json; charset=UTF-8", nil)
}

func (g *googleProvider) Delete(ctx context.Context, id string) error {
	return g.do(ctx, http.MethodDelete, driveBase+"/files/"+url.PathEscape(id), nil, "", nil)
}

func (g *googleProvider) ShareLink(ctx context.Context, id string) (string, error) {
	// 授予 anyone:reader（链接可见），再回一个可直接分享的查看链接。
	body, _ := json.Marshal(map[string]any{"role": "reader", "type": "anyone"})
	q := url.Values{}
	q.Set("supportsAllDrives", "true")
	u := driveBase + "/files/" + url.PathEscape(id) + "/permissions?" + q.Encode()
	if err := g.do(ctx, http.MethodPost, u, body, "application/json; charset=UTF-8", nil); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/file/d/%s/view?usp=sharing", driveWebBase, id), nil
}

func (g *googleProvider) Quota(ctx context.Context) (Quota, error) {
	var out struct {
		QuotaBytesUsed  string `json:"quotaBytesUsed"`
		QuotaBytesTotal string `json:"quotaBytesTotal"`
	}
	if err := g.do(ctx, http.MethodGet, driveBase+"/about?fields=quotaBytesUsed,quotaBytesTotal", nil, "", &out); err != nil {
		return Quota{}, err
	}
	used, _ := strconv.ParseInt(out.QuotaBytesUsed, 10, 64)
	total, _ := strconv.ParseInt(out.QuotaBytesTotal, 10, 64)
	return Quota{Used: used, Total: total}, nil
}

// Open 返回媒体流（?alt=media）。调用方 Close。size 取 Content-Length，未知为 -1。
func (g *googleProvider) Open(ctx context.Context, id string) (io.ReadCloser, int64, error) {
	tok, err := g.ensureToken(ctx)
	if err != nil {
		return nil, 0, err
	}
	u := driveBase + "/files/" + url.PathEscape(id) + "?alt=media&supportsAllDrives=true"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := g.hc.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("云盘下载请求失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bs, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		return nil, 0, fmt.Errorf("云盘下载返回 %d：%s", resp.StatusCode, apiErrMsg(bs))
	}
	var size int64 = -1
	if resp.ContentLength >= 0 {
		size = resp.ContentLength
	}
	return resp.Body, size, nil
}

// Upload 用 multipart related 简单上传（PR1：整段缓冲，size<=0 或未知时也走这条）。
// 超大/断点续传（resumable）留给后续 PR。
func (g *googleProvider) Upload(ctx context.Context, parentID, name string, r io.Reader, size int64) (Entry, error) {
	if parentID == "" {
		parentID = "root"
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return Entry{}, fmt.Errorf("读取上传内容失败: %w", err)
	}
	meta := map[string]any{"name": name, "parents": []string{parentID}}
	metaBS, _ := json.Marshal(meta)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	boundary := mw.Boundary()
	mp, err := mw.CreatePart(map[string][]string{
		"Content-Type": {"application/json; charset=UTF-8"},
	})
	if err != nil {
		return Entry{}, err
	}
	if _, err := mp.Write(metaBS); err != nil {
		return Entry{}, err
	}
	fw, err := mw.CreatePart(map[string][]string{
		"Content-Type": {"application/octet-stream"},
	})
	if err != nil {
		return Entry{}, err
	}
	if _, err := fw.Write(data); err != nil {
		return Entry{}, err
	}
	_ = mw.Close()

	tok, err := g.ensureToken(ctx)
	if err != nil {
		return Entry{}, err
	}
	u := driveUploadBase + "/files?uploadType=multipart&fields=" + url.QueryEscape("id,name,mimeType,size,modifiedTime")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(buf.Bytes()))
	if err != nil {
		return Entry{}, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "multipart/mixed; boundary="+boundary)
	resp, err := g.hc.Do(req)
	if err != nil {
		return Entry{}, fmt.Errorf("云盘上传请求失败: %w", err)
	}
	defer resp.Body.Close()
	bs, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Entry{}, fmt.Errorf("云盘上传返回 %d：%s", resp.StatusCode, apiErrMsg(bs))
	}
	var f gFile
	if err := json.Unmarshal(bs, &f); err != nil {
		return Entry{}, fmt.Errorf("解析上传响应失败: %w", err)
	}
	return f.toEntry(), nil
}
