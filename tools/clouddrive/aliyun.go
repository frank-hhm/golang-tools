package clouddrive

// 阿里云盘（aliyundrive / alipan）v2 适配器（计划-云盘管理工具 PR2）。
//
// 授权：用户从浏览器抓一份长期 refresh_token 粘进来（SetCredential 存 Refresh 字段），
// 运行期 ensureToken 用 POST /token/refresh 换 access_token（顺带缓存 default_drive_id）。
// 全请求都是 POST + JSON body + `Authorization: Bearer <access>`，与 Google 的 REST 风格不同。
//
// 现实提醒：这是**现网逆向**、无官方开放文档保证的私有端点，接口/字段随时可能变，
// 不保证真网可用；单测全部走 httptest 假后端，只验证「请求形态 + 响应解析 + 凭证流转」
// 这层契约。跨平台零 cgo、无 build tag（Linux 面板构建同样存在）。
//
// 端点做成包级变量，测试把 base 指到 httptest server 即可（照 google.go 范式）。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// 端点（测试里改这几个变量即可把流量引到 httptest）。
var (
	aliyunTokenURL = "https://api.aliyundrive.com/token/refresh"
	aliyunAPIBase  = "https://api.aliyundrive.com/v2"
)

func init() {
	register(KindAliyun, func(acctID string) (Provider, error) { return newAliyun(acctID) })
}

// aliyunProvider 实现 Provider。无状态：每次操作从账号表取 token、按需续期。
type aliyunProvider struct {
	acctID string
	hc     *http.Client
	// 单次 New() 生命周期内缓存 drive_id，避免每个操作都多打一次 user/get。
	driveID string
}

func newAliyun(acctID string) (*aliyunProvider, error) {
	return &aliyunProvider{acctID: acctID, hc: &http.Client{}}, nil
}

func (a *aliyunProvider) Name() string { return "阿里云盘" }

// aliyunRefreshResp /token/refresh 的响应（只取用到的字段）。
type aliyunRefreshResp struct {
	Access         string `json:"access_token"`
	Refresh        string `json:"refresh_token"`
	ExpiresIn      int    `json:"expires_in"`
	DefaultDriveID string `json:"default_drive_id"`
	Code           string `json:"code"`
	Message        string `json:"message"`
}

// ensureToken 返回可用的 access token + drive_id，临期自动用 refresh_token 续期。
func (a *aliyunProvider) ensureToken(ctx context.Context) (string, error) {
	b, err := loadToken(a.acctID)
	if err != nil {
		return "", err
	}
	if b.Access != "" && (b.Expiry == 0 || b.Expiry-time.Now().UnixMilli() > 60_000) {
		a.driveID = b.DriveID
		return b.Access, nil
	}
	if b.Refresh == "" {
		return "", fmt.Errorf("无有效凭证，请在授权里填入 refresh_token")
	}
	reqBody, _ := json.Marshal(map[string]any{"refresh_token": b.Refresh})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, aliyunTokenURL, bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("令牌刷新请求失败: %w", err)
	}
	defer resp.Body.Close()
	bs, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var tr aliyunRefreshResp
	_ = json.Unmarshal(bs, &tr)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || tr.Access == "" {
		msg := firstNonEmptyStr(tr.Message, tr.Code, apiErrMsg(bs))
		return "", fmt.Errorf("令牌刷新失败(%d): %s", resp.StatusCode, msg)
	}
	nb := tokenBlob{
		Access:  tr.Access,
		Refresh: orStr(tr.Refresh, b.Refresh),
		Expiry:  expiryOf(tr.ExpiresIn),
		DriveID: orStr(tr.DefaultDriveID, b.DriveID),
	}
	if err := saveToken(a.acctID, nb); err != nil {
		return "", err
	}
	a.driveID = nb.DriveID
	return nb.Access, nil
}

// drive 返回当前 drive_id（先确保 token 有效，再取缓存/凭证里的默认盘）。
func (a *aliyunProvider) drive(ctx context.Context) (string, error) {
	if _, err := a.ensureToken(ctx); err != nil {
		return "", err
	}
	if a.driveID != "" {
		return a.driveID, nil
	}
	// 极少数情况下 refresh 未回 default_drive_id：补一次 user/get。
	var out struct {
		DefaultDriveID string `json:"default_drive_id"`
	}
	if err := a.post(ctx, "/user/get", map[string]any{}, &out); err != nil {
		return "", err
	}
	a.driveID = out.DefaultDriveID
	return a.driveID, nil
}

// post 打一个 v2 JSON 端点：带 Bearer，POST body，解析 out（可为 nil）。
// 阿里即使出错也可能回 HTTP 200 + body 里的 code/message，这里一并判掉。
func (a *aliyunProvider) post(ctx context.Context, path string, body any, out any) error {
	tok, err := a.ensureToken(ctx)
	if err != nil {
		return err
	}
	bs, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, aliyunAPIBase+path, bytes.NewReader(bs))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.hc.Do(req)
	if err != nil {
		return fmt.Errorf("云盘请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("云盘返回 %d：%s", resp.StatusCode, apiErrMsg(raw))
	}
	// 200 但 body 带错误码（阿里常见：{"code":"...","message":"..."}）。
	if len(raw) > 0 {
		var probe struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &probe) == nil && probe.Code != "" && !strings.EqualFold(probe.Code, "OK") {
			return fmt.Errorf("云盘错误 %s：%s", probe.Code, orStr(probe.Message, "无详情"))
		}
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("解析云盘响应失败: %w", err)
		}
	}
	return nil
}

/* ============================ 文件条目 ============================ */

// aliyunFile v2/file/list 里的条目（只取用到的字段）。
type aliyunFile struct {
	FileID    string `json:"file_id"`
	Name      string `json:"name"`
	Type      string `json:"type"` // "folder" | "file"
	Size      int64  `json:"size"`
	UpdatedAt string `json:"updated_at"`
	MimeType  string `json:"content_type"`
	Thumbnail string `json:"thumbnail"`
}

func (f aliyunFile) toEntry() Entry {
	var mt int64
	if t, err := time.Parse(time.RFC3339, f.UpdatedAt); err == nil {
		mt = t.UnixMilli()
	}
	return Entry{
		ID: f.FileID, Name: f.Name, Path: f.Name,
		IsDir: strings.EqualFold(f.Type, "folder"),
		Size:  f.Size, MTime: mt, MIME: f.MimeType,
		ThumbURL: f.Thumbnail,
	}
}

/* ============================ Provider 方法 ============================ */

func (a *aliyunProvider) Test(ctx context.Context) error {
	_, err := a.Quota(ctx)
	return err
}

func (a *aliyunProvider) List(ctx context.Context, dirID string) ([]Entry, error) {
	drive, err := a.drive(ctx)
	if err != nil {
		return nil, err
	}
	parent := "root"
	if dirID != "" {
		parent = dirID
	}
	var out struct {
		Items []aliyunFile `json:"items"`
	}
	body := map[string]any{"drive_id": drive, "parent_file_id": parent, "limit": 200, "marker": ""}
	if err := a.post(ctx, "/file/list", body, &out); err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(out.Items))
	for _, f := range out.Items {
		entries = append(entries, f.toEntry())
	}
	return entries, nil
}

func (a *aliyunProvider) Mkdir(ctx context.Context, parentID, name string) (Entry, error) {
	drive, err := a.drive(ctx)
	if err != nil {
		return Entry{}, err
	}
	if parentID == "" {
		parentID = "root"
	}
	var f aliyunFile
	body := map[string]any{"drive_id": drive, "parent_file_id": parentID, "name": name, "type": "folder"}
	if err := a.post(ctx, "/file/create", body, &f); err != nil {
		return Entry{}, err
	}
	return f.toEntry(), nil
}

func (a *aliyunProvider) Rename(ctx context.Context, id, newName string) error {
	drive, err := a.drive(ctx)
	if err != nil {
		return err
	}
	body := map[string]any{"drive_id": drive, "file_id": id, "new_name": newName}
	return a.post(ctx, "/file/rename", body, nil)
}

func (a *aliyunProvider) Move(ctx context.Context, id, dstParentID string) error {
	drive, err := a.drive(ctx)
	if err != nil {
		return err
	}
	if dstParentID == "" {
		dstParentID = "root"
	}
	body := map[string]any{"drive_id": drive, "file_id": id, "new_parent_file_id": dstParentID}
	return a.post(ctx, "/file/move", body, nil)
}

func (a *aliyunProvider) Delete(ctx context.Context, id string) error {
	drive, err := a.drive(ctx)
	if err != nil {
		return err
	}
	// 移入回收站（可恢复），比彻底删除更符合用户预期。
	body := map[string]any{"drive_id": drive, "file_id": id}
	return a.post(ctx, "/recyclebin/trash", body, nil)
}

func (a *aliyunProvider) ShareLink(ctx context.Context, id string) (string, error) {
	drive, err := a.drive(ctx)
	if err != nil {
		return "", err
	}
	var out struct {
		ShareID string `json:"share_id"`
	}
	body := map[string]any{"drive_id": drive, "file_id_list": []string{id}}
	if err := a.post(ctx, "/share_link/create", body, &out); err != nil {
		return "", err
	}
	if out.ShareID == "" {
		return "", fmt.Errorf("云盘未返回分享 ID")
	}
	return "https://www.alipan.com/s/" + out.ShareID, nil
}

func (a *aliyunProvider) Quota(ctx context.Context) (Quota, error) {
	drive, err := a.drive(ctx)
	if err != nil {
		return Quota{}, err
	}
	var out struct {
		UsedSize  int64 `json:"used_size"`
		TotalSize int64 `json:"total_size"`
	}
	if err := a.post(ctx, "/drive/get", map[string]any{"drive_id": drive}, &out); err != nil {
		return Quota{}, err
	}
	return Quota{Used: out.UsedSize, Total: out.TotalSize}, nil
}

// Open 先取直连下载地址（get_download_url），再 GET 流式返回。调用方 Close。
func (a *aliyunProvider) Open(ctx context.Context, id string) (io.ReadCloser, int64, error) {
	drive, err := a.drive(ctx)
	if err != nil {
		return nil, 0, err
	}
	var out struct {
		URL  string `json:"url"`
		Size int64  `json:"size"`
	}
	body := map[string]any{"drive_id": drive, "file_id": id}
	if err := a.post(ctx, "/file/get_download_url", body, &out); err != nil {
		return nil, 0, err
	}
	if out.URL == "" {
		return nil, 0, fmt.Errorf("云盘未返回下载地址")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, out.URL, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := a.hc.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("云盘下载请求失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bs, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		return nil, 0, fmt.Errorf("云盘下载返回 %d：%s", resp.StatusCode, apiErrMsg(bs))
	}
	size := resp.ContentLength
	if size < 0 {
		size = out.Size // 回落 body 里报的 size
	}
	if size < 0 {
		size = -1
	}
	return resp.Body, size, nil
}

// Upload 走「create → PUT upload_url → complete」三段式简单上传（整段缓冲）。
// 分片/断点续传留给后续 PR。
func (a *aliyunProvider) Upload(ctx context.Context, parentID, name string, r io.Reader, size int64) (Entry, error) {
	drive, err := a.drive(ctx)
	if err != nil {
		return Entry{}, err
	}
	if parentID == "" {
		parentID = "root"
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return Entry{}, fmt.Errorf("读取上传内容失败: %w", err)
	}
	var create struct {
		FileID    string `json:"file_id"`
		UploadID  string `json:"upload_id"`
		UploadURL string `json:"upload_url"`
	}
	body := map[string]any{
		"drive_id":       drive,
		"parent_file_id": parentID,
		"name":           name,
		"type":           "file",
		"content_type":   "application/octet-stream",
		"size":           int64(len(data)),
	}
	if err := a.post(ctx, "/file/create", body, &create); err != nil {
		return Entry{}, err
	}
	if create.UploadURL == "" {
		return Entry{}, fmt.Errorf("云盘未返回上传地址")
	}
	put, err := http.NewRequestWithContext(ctx, http.MethodPut, create.UploadURL, bytes.NewReader(data))
	if err != nil {
		return Entry{}, err
	}
	putResp, err := a.hc.Do(put)
	if err != nil {
		return Entry{}, fmt.Errorf("云盘上传 PUT 失败: %w", err)
	}
	_ = putResp.Body.Close()
	if putResp.StatusCode < 200 || putResp.StatusCode >= 300 {
		return Entry{}, fmt.Errorf("云盘上传 PUT 返回 %d", putResp.StatusCode)
	}
	var f aliyunFile
	complete := map[string]any{
		"drive_id":  drive,
		"file_id":   create.FileID,
		"upload_id": create.UploadID,
	}
	if err := a.post(ctx, "/file/complete", complete, &f); err != nil {
		return Entry{}, err
	}
	if f.FileID == "" {
		f.FileID = create.FileID
		f.Name = name
		f.Size = int64(len(data))
		return f.toEntry(), nil
	}
	return f.toEntry(), nil
}

// firstNonEmptyStr 返回第一个非空字符串。
func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return "无详情"
}

// orStr 主值非空用主值，否则回落备选。
func orStr(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}
