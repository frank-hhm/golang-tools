package clouddrive

// 百度网盘（pan.baidu.com）适配器（计划-云盘管理工具 PR2）。
//
// 授权：用户从浏览器抓一份 BDUSS Cookie 粘进来（SetCredential 存 Cookie 字段），
// 之后每个请求都带 `Cookie: BDUSS=<值>`。百度 xpan 接口以**路径**为主键，故本适配器
// 用条目的完整 path 作为 Entry.ID（阿里/Google 用原生 id，接口寻址一律用 ID，语义自洽）。
//
// 现实提醒：这同样是**现网逆向**、无官方保证的私有接口（且真网常需 csrf/sign/UA 校验），
// 不保证真网可用；单测全走 httptest 假后端，只验「请求形态 + 响应解析 + 凭证流转」契约。
// 分享链接（需另一套 public API）本适配器暂不支持，如实返回错误。
//
// 跨平台零 cgo、无 build tag（Linux 面板构建同样存在）。端点为包级变量供 httptest 改写。

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 端点与应用标识（测试里改变量即可把流量引到 httptest）。
var (
	baiduWebBase   = "https://pan.baidu.com"
	baiduPCSBase   = "https://d.pcs.baidu.com"
	baiduAppID     = "25056447" // 网页端通用 app_id，非机密
	baiduUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0 Safari/537.36"
)

func init() {
	register(KindBaidu, func(acctID string) (Provider, error) { return newBaidu(acctID) })
}

// baiduProvider 实现 Provider。无状态：每次操作从账号表取 BDUSS。
type baiduProvider struct {
	acctID string
	hc     *http.Client
}

func newBaidu(acctID string) (*baiduProvider, error) {
	return &baiduProvider{acctID: acctID, hc: &http.Client{}}, nil
}

func (b *baiduProvider) Name() string { return "百度网盘" }

// bduss 取账号存的 BDUSS Cookie。
func (b *baiduProvider) bduss() (string, error) {
	tk, err := loadToken(b.acctID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(tk.Cookie) == "" {
		return "", fmt.Errorf("无 BDUSS，请在授权里粘贴 BDUSS Cookie")
	}
	return tk.Cookie, nil
}

func (b *baiduProvider) setAuth(req *http.Request, ck string) {
	req.Header.Set("Cookie", "BDUSS="+ck)
	req.Header.Set("User-Agent", baiduUserAgent)
}

// baiduRespErr 判百度响应：errno 存在且非 0 即失败。
func baiduRespErr(raw []byte, status int) error {
	if status < 200 || status >= 300 {
		return fmt.Errorf("云盘返回 %d：%s", status, apiErrMsg(raw))
	}
	var probe struct {
		Errno *int `json:"errno"`
	}
	if json.Unmarshal(raw, &probe) == nil && probe.Errno != nil && *probe.Errno != 0 {
		return fmt.Errorf("百度返回错误 errno=%d（%s）", *probe.Errno, apiErrMsg(raw))
	}
	return nil
}

// getJSON 发一个带 BDUSS 的 GET 并解析 JSON。
func (b *baiduProvider) getJSON(ctx context.Context, u string, out any) error {
	ck, err := b.bduss()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	b.setAuth(req, ck)
	resp, err := b.hc.Do(req)
	if err != nil {
		return fmt.Errorf("云盘请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err := baiduRespErr(raw, resp.StatusCode); err != nil {
		return err
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("解析云盘响应失败: %w", err)
		}
	}
	return nil
}

// postForm 发一个带 BDUSS 的 form POST（filemanager 类操作）并解析 JSON。
func (b *baiduProvider) postForm(ctx context.Context, u string, form url.Values, out any) error {
	ck, err := b.bduss()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	b.setAuth(req, ck)
	resp, err := b.hc.Do(req)
	if err != nil {
		return fmt.Errorf("云盘请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err := baiduRespErr(raw, resp.StatusCode); err != nil {
		return err
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("解析云盘响应失败: %w", err)
		}
	}
	return nil
}

// filemanagerURL 组装 xpan multimedia filemanager 端点。
func (b *baiduProvider) filemanagerURL(opera string, extra url.Values) string {
	q := url.Values{}
	q.Set("method", "filemanager")
	q.Set("opera", opera)
	q.Set("clienttype", "0")
	q.Set("app_id", baiduAppID)
	q.Set("web", "1")
	for k, vs := range extra {
		for _, v := range vs {
			q.Set(k, v)
		}
	}
	return baiduWebBase + "/rest/2.0/xpan/multimedia?" + q.Encode()
}

/* ============================ 路径工具 ============================ */

// baiduParent 取路径的父目录（"/" 的父仍是 "/"）。
func baiduParent(p string) string {
	p = strings.TrimSuffix(p, "/")
	i := strings.LastIndex(p, "/")
	if i <= 0 {
		return "/"
	}
	return p[:i]
}

// baiduJoin 拼目录与文件名成规范路径。
func baiduJoin(dir, name string) string {
	if dir == "" || dir == "/" {
		return "/" + name
	}
	return strings.TrimSuffix(dir, "/") + "/" + name
}

// baiduBase 取路径的末段文件名。
func baiduBase(p string) string {
	p = strings.TrimSuffix(p, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

/* ============================ 文件条目 ============================ */

// baiduFile xpan file list 的条目。
type baiduFile struct {
	FsID        int64  `json:"fs_id"`
	Path        string `json:"path"`
	Name        string `json:"server_filename"`
	IsDir       int    `json:"isdir"`
	Size        int64  `json:"size"`
	ServerMtime int64  `json:"server_mtime"` // 秒
}

func (f baiduFile) toEntry() Entry {
	return Entry{
		ID: f.Path, Name: f.Name, Path: f.Path,
		IsDir: f.IsDir != 0,
		Size:  f.Size, MTime: f.ServerMtime * 1000,
	}
}

/* ============================ Provider 方法 ============================ */

func (b *baiduProvider) Test(ctx context.Context) error {
	_, err := b.Quota(ctx)
	return err
}

func (b *baiduProvider) List(ctx context.Context, dirID string) ([]Entry, error) {
	dir := dirID
	if dir == "" {
		dir = "/"
	}
	q := url.Values{}
	q.Set("method", "list")
	q.Set("dir", dir)
	q.Set("order", "name")
	q.Set("desc", "0")
	q.Set("page", "1")
	q.Set("num", "200")
	q.Set("web", "1")
	q.Set("app_id", baiduAppID)
	var out struct {
		List []baiduFile `json:"list"`
	}
	u := baiduWebBase + "/rest/2.0/xpan/file?" + q.Encode()
	if err := b.getJSON(ctx, u, &out); err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(out.List))
	for _, f := range out.List {
		entries = append(entries, f.toEntry())
	}
	return entries, nil
}

func (b *baiduProvider) Mkdir(ctx context.Context, parentID, name string) (Entry, error) {
	newPath := baiduJoin(parentID, name)
	items, _ := json.Marshal([]map[string]any{{"path": newPath}})
	form := url.Values{}
	form.Set("path", string(items))
	var out struct {
		Path string `json:"path"`
	}
	if err := b.postForm(ctx, b.filemanagerURL("create", nil), form, &out); err != nil {
		return Entry{}, err
	}
	p := out.Path
	if p == "" {
		p = newPath
	}
	return Entry{ID: p, Name: name, Path: p, IsDir: true}, nil
}

func (b *baiduProvider) Rename(ctx context.Context, id, newName string) error {
	to := baiduJoin(baiduParent(id), newName)
	items, _ := json.Marshal([]map[string]any{{"path": id, "to": to}})
	form := url.Values{}
	form.Set("path", string(items))
	return b.postForm(ctx, b.filemanagerURL("rename", nil), form, nil)
}

func (b *baiduProvider) Move(ctx context.Context, id, dstParentID string) error {
	dest := dstParentID
	if dest == "" {
		dest = "/"
	}
	items, _ := json.Marshal([]map[string]any{{"path": id, "dest": dest, "newname": baiduBase(id)}})
	form := url.Values{}
	form.Set("path", string(items))
	return b.postForm(ctx, b.filemanagerURL("move", nil), form, nil)
}

func (b *baiduProvider) Delete(ctx context.Context, id string) error {
	items, _ := json.Marshal([]map[string]any{{"path": id}})
	form := url.Values{}
	form.Set("path", string(items))
	return b.postForm(ctx, b.filemanagerURL("delete", nil), form, nil)
}

func (b *baiduProvider) ShareLink(ctx context.Context, id string) (string, error) {
	// 百度公开分享要走另一套 (share) API 且风控严格，本期逆向适配器不实现，如实报错。
	return "", fmt.Errorf("百度网盘暂不支持在本工具生成分享链接，请到百度网盘网页端操作")
}

func (b *baiduProvider) Quota(ctx context.Context) (Quota, error) {
	q := url.Values{}
	q.Set("method", "info")
	q.Set("app_id", baiduAppID)
	q.Set("op", "pcs")
	var out struct {
		TotalSize json.Number `json:"total_size"`
		UsedSize  json.Number `json:"used_size"`
	}
	u := baiduPCSBase + "/rest/2.0/pcs/quota?" + q.Encode()
	if err := b.getJSON(ctx, u, &out); err != nil {
		return Quota{}, err
	}
	total, _ := out.TotalSize.Int64()
	used, _ := out.UsedSize.Int64()
	return Quota{Used: used, Total: total}, nil
}

// Open 走 pcs file download（?method=download&path=），返回只读流。调用方 Close。
func (b *baiduProvider) Open(ctx context.Context, id string) (io.ReadCloser, int64, error) {
	ck, err := b.bduss()
	if err != nil {
		return nil, 0, err
	}
	q := url.Values{}
	q.Set("method", "download")
	q.Set("app_id", baiduAppID)
	q.Set("path", id)
	u := baiduPCSBase + "/rest/2.0/pcs/file?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	b.setAuth(req, ck)
	resp, err := b.hc.Do(req)
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

// Upload 走「precreate → superfile2 上传块 → create 提交」三段式（整段缓冲 + md5）。
// 分片/断点续传留给后续 PR；真网风控可能失败，属逆向适配器的已知边界。
func (b *baiduProvider) Upload(ctx context.Context, parentID, name string, r io.Reader, size int64) (Entry, error) {
	ck, err := b.bduss()
	if err != nil {
		return Entry{}, err
	}
	target := baiduJoin(parentID, name)
	data, err := io.ReadAll(r)
	if err != nil {
		return Entry{}, fmt.Errorf("读取上传内容失败: %w", err)
	}
	sum := fmt.Sprintf("%x", md5.Sum(data))
	mtime := time.Now().Unix()

	// 1) precreate 拿 uploadid
	preItems, _ := json.Marshal([]map[string]any{
		{"path": target, "size": int64(len(data)), "time": mtime, "md5": sum, "block_list": []string{sum}},
	})
	preForm := url.Values{}
	preForm.Set("path", string(preItems))
	var pre struct {
		UploadID string `json:"uploadid"`
	}
	if err := b.postForm(ctx, b.filemanagerURL("precreate", nil), preForm, &pre); err != nil {
		return Entry{}, err
	}

	// 2) 上传首个（也是唯一一个）分块
	upQ := url.Values{}
	upQ.Set("method", "upload")
	upQ.Set("app_id", baiduAppID)
	upQ.Set("type", "json")
	upQ.Set("uploadid", pre.UploadID)
	upQ.Set("target_path", target)
	upQ.Set("partseq", "0")
	upURL := baiduPCSBase + "/rest/2.0/pcs/superfile2?" + upQ.Encode()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", name)
	if err != nil {
		return Entry{}, err
	}
	if _, err := fw.Write(data); err != nil {
		return Entry{}, err
	}
	_ = mw.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upURL, bytes.NewReader(buf.Bytes()))
	if err != nil {
		return Entry{}, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	b.setAuth(req, ck)
	resp, err := b.hc.Do(req)
	if err != nil {
		return Entry{}, fmt.Errorf("云盘上传请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Entry{}, fmt.Errorf("云盘上传返回 %d：%s", resp.StatusCode, apiErrMsg(raw))
	}
	var upOut struct {
		MD5 string `json:"md5"`
	}
	_ = json.Unmarshal(raw, &upOut)
	chunkMD5 := upOut.MD5
	if chunkMD5 == "" {
		chunkMD5 = sum
	}

	// 3) create 提交
	createItems, _ := json.Marshal([]map[string]any{
		{"path": target, "size": int64(len(data)), "time": mtime, "md5": sum,
			"uploadid": pre.UploadID, "block_list": []string{chunkMD5}},
	})
	form := url.Values{}
	form.Set("path", string(createItems))
	var cOut struct {
		Path string `json:"path"`
		FsID int64  `json:"fs_id"`
	}
	if err := b.postForm(ctx, b.filemanagerURL("create", nil), form, &cOut); err != nil {
		return Entry{}, err
	}
	p := cOut.Path
	if p == "" {
		p = target
	}
	return Entry{ID: p, Name: name, Path: p, Size: int64(len(data)), MTime: mtime * 1000}, nil
}
