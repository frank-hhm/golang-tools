//go:build windows

package docker

// ============================================================
// Docker 管理 - 仓库、加速站与线上镜像
//
// 两条不同性质的配置，故意分开存：
//   - 私有仓库（含 Docker Hub 登录）→ 本工具自己的 registries.json，
//     密码用 Windows DPAPI 加密（只有当前用户能解），绑定接口只回传“已保存”；
//   - 加速站 / 不安全仓库 → 引擎的 %USERPROFILE%\.docker\daemon.json，
//     只改 registry-mirrors 与 insecure-registries 两个键，其余原样保留并备份。
//
// 线上镜像：Docker Hub 走 hub.docker.com 的搜索接口（有 Star 数/简介），
// 私有仓库走标准 Registry V2 的 _catalog + tags/list（匿名或 Basic/Bearer 均可）。
// ============================================================

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go-tools/appdirs"
)

const (
	registriesFile = "registries.json"
	hubURL         = "https://index.docker.io/v1/" // Docker Hub 在 auth 配置里的规范地址
	hubAPI         = "https://hub.docker.com/v2"
	daemonJSONName = "daemon.json"
	registryHTTPTO = 15 * time.Second
)

/* ============================ 存储 ============================ */

// registryStore 落盘结构：密码单独存密文，接口层永不返回
type registryStore struct {
	Items []registryRecord `json:"items"`
}

type registryRecord struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	User      string `json:"user"`
	PassEnc   string `json:"passEnc,omitempty"` // DPAPI 密文（base64）
	Namespace string `json:"namespace"`
	Note      string `json:"note"`
}

var registryMu sync.Mutex

func registryPath() (string, error) {
	dir, err := appdirs.Data("docker")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建仓库配置目录失败: %v", err)
	}
	return filepath.Join(dir, registriesFile), nil
}

func loadRegistries() registryStore {
	var st registryStore
	p, err := registryPath()
	if err != nil {
		return st
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return st
	}
	_ = json.Unmarshal(b, &st)
	return st
}

func saveRegistries(st registryStore) error {
	p, err := registryPath()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".gotmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// ListRegistries 列出已配置仓库（不含密码）
func ListRegistries() []Registry {
	registryMu.Lock()
	defer registryMu.Unlock()
	st := loadRegistries()
	out := make([]Registry, 0, len(st.Items))
	for _, r := range st.Items {
		out = append(out, Registry{ID: r.ID, Name: r.Name, URL: r.URL, User: r.User,
			HasPass: r.PassEnc != "", Namespace: r.Namespace, Note: r.Note})
	}
	return out
}

// SaveRegistry 新增/更新仓库。password 为空表示沿用已保存的密码；
// clearPass=true 表示主动清空（改回匿名）。
func SaveRegistry(r Registry, password string, clearPass bool) (Registry, error) {
	r.URL = normalizeRegistryURL(r.URL)
	if strings.TrimSpace(r.URL) == "" {
		return Registry{}, fmt.Errorf("仓库地址不能为空，如 https://harbor.corp.local")
	}
	if r.Name == "" {
		r.Name = r.URL
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	st := loadRegistries()
	idx := -1
	for i := range st.Items {
		if st.Items[i].ID == r.ID {
			idx = i
			break
		}
	}
	rec := registryRecord{ID: r.ID, Name: r.Name, URL: r.URL, User: r.User,
		Namespace: strings.Trim(r.Namespace, "/"), Note: r.Note}
	if idx >= 0 {
		rec.PassEnc = st.Items[idx].PassEnc
		if rec.ID == "" {
			rec.ID = st.Items[idx].ID
		}
	}
	if rec.ID == "" {
		rec.ID = fmt.Sprintf("reg-%d", time.Now().UnixNano())
	}
	if clearPass {
		rec.PassEnc = ""
	} else if password != "" {
		enc, err := dpapiProtect(password)
		if err != nil {
			return Registry{}, fmt.Errorf("保存密码失败（DPAPI）: %v", err)
		}
		rec.PassEnc = enc
	}
	if idx >= 0 {
		st.Items[idx] = rec
	} else {
		st.Items = append(st.Items, rec)
	}
	if err := saveRegistries(st); err != nil {
		return Registry{}, err
	}
	return Registry{ID: rec.ID, Name: rec.Name, URL: rec.URL, User: rec.User,
		HasPass: rec.PassEnc != "", Namespace: rec.Namespace, Note: rec.Note}, nil
}

// DeleteRegistry 删除仓库配置（只影响本工具，不碰 ~/.docker/config.json）
func DeleteRegistry(id string) error {
	registryMu.Lock()
	defer registryMu.Unlock()
	st := loadRegistries()
	out := st.Items[:0]
	found := false
	for _, r := range st.Items {
		if r.ID == id {
			found = true
			continue
		}
		out = append(out, r)
	}
	if !found {
		return fmt.Errorf("仓库不存在: %s", id)
	}
	st.Items = out
	return saveRegistries(st)
}

// registryPassword 解出密码（只供内部拼认证头，绝不经绑定接口回传）
func registryPassword(id string) (string, error) {
	registryMu.Lock()
	st := loadRegistries()
	registryMu.Unlock()
	for _, r := range st.Items {
		if r.ID != id {
			continue
		}
		if r.PassEnc == "" {
			return "", nil
		}
		return dpapiUnprotect(r.PassEnc)
	}
	return "", fmt.Errorf("仓库不存在: %s", id)
}

func registryByID(id string) (Registry, bool) {
	for _, r := range ListRegistries() {
		if r.ID == id {
			return r, true
		}
	}
	return Registry{}, false
}

// normalizeRegistryURL 规整仓库地址：补协议、去尾部斜杠与 /v2
func normalizeRegistryURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	u = strings.TrimRight(u, "/")
	u = strings.TrimSuffix(u, "/v2")
	u = strings.TrimRight(u, "/")
	low := strings.ToLower(u)
	if !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") {
		u = "https://" + u
		low = strings.ToLower(u)
	}
	// Docker Hub 的几种写法（含 /v1 旧前缀）统一成规范地址，免得认证头对不上
	host := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(low, "https://"), "http://"), "/v1")
	switch host {
	case "docker.io", "index.docker.io", "registry-1.docker.io", "hub.docker.com":
		return hubURL
	}
	return u
}

// sameRegistry 判断配置里的仓库地址与镜像引用里的主机名是否同一仓库
func sameRegistry(urlOrName, host string) bool {
	if host == "" {
		return false
	}
	n := normalizeRegistryURL(urlOrName)
	if n == hubURL {
		return host == "docker.io" || host == "index.docker.io" || host == "registry-1.docker.io"
	}
	n = strings.TrimPrefix(strings.TrimPrefix(n, "https://"), "http://")
	n = strings.TrimPrefix(n, "//")
	if i := strings.IndexAny(n, "/"); i > 0 {
		n = n[:i]
	}
	return strings.EqualFold(n, host)
}

func isHub(r Registry) bool { return normalizeRegistryURL(r.URL) == hubURL }

/* ============================ 仓库连通性 ============================ */

// registryClient 仓库侧 HTTP 客户端：内网自签证书仓库若已登记为 insecure，这里也不校验证书
func registryClient(insecure bool) *http.Client {
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	if insecure {
		tr.TLSClientConfig.InsecureSkipVerify = true
	}
	return &http.Client{Timeout: registryHTTPTO, Transport: tr}
}

func registryInsecure(host string) bool {
	cfg, err := GetDaemonConfig()
	if err != nil {
		return false
	}
	for _, h := range cfg.Insecure {
		if sameRegistry(h, host) || strings.EqualFold(strings.TrimPrefix(strings.TrimPrefix(h, "http://"), "https://"), host) {
			return true
		}
	}
	return false
}

// registryProbe 用给定账号打一次 /v2/，返回可读状态（供“测试连接”与列表复用）
func registryProbe(r Registry, user, pass string) (string, error) {
	base := strings.TrimSuffix(normalizeRegistryURL(r.URL), "/")
	cli := registryClient(registryInsecure(hostOnly(base)))
	resp, err := registryReq(cli, http.MethodGet, base+"/v2/", user, pass, "")
	if err != nil {
		return "", fmt.Errorf("连不上 %s: %v", base, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return "连接正常（/v2/ 可匿名访问）", nil
	case http.StatusUnauthorized:
		if user == "" {
			return "", fmt.Errorf("%s 要求登录：请填写用户名与密码", base)
		}
		return "账号可用（/v2/ 已通过认证）", nil
	default:
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 400))
		return "", fmt.Errorf("仓库返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
}

// TestRegistry 用已保存的凭据探一次仓库，返回可读结论
func TestRegistry(id string) (string, error) {
	r, ok := registryByID(id)
	if !ok {
		return "", fmt.Errorf("仓库不存在: %s", id)
	}
	pass, _ := registryPassword(id)
	return registryProbe(r, r.User, pass)
}

// registryReq 发起仓库请求；遇 Bearer challenge 时自动换 token 重试一次
func registryReq(cli *http.Client, method, rawURL, user, pass, bearer string) (*http.Response, error) {
	req, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	} else if user != "" {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+pass)))
	}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	ch := resp.Header.Get("WWW-Authenticate")
	if !strings.HasPrefix(strings.ToLower(ch), "bearer ") {
		return resp, nil
	}
	_ = resp.Body.Close()
	tok, terr := fetchRegistryToken(cli, ch, user, pass)
	if terr != nil || tok == "" {
		return nil, fmt.Errorf("获取仓库令牌失败: %v", terr)
	}
	req2, _ := http.NewRequest(method, rawURL, nil)
	req2.Header.Set("Authorization", "Bearer "+tok)
	return cli.Do(req2)
}

// fetchRegistryToken 解析 WWW-Authenticate: Bearer realm=... 并换取 token
func fetchRegistryToken(cli *http.Client, challenge, user, pass string) (string, error) {
	realm, service, scope := parseChallenge(challenge)
	if realm == "" {
		return "", fmt.Errorf("仓库未给出认证地址")
	}
	u, err := url.Parse(realm)
	if err != nil {
		return "", err
	}
	q := u.Query()
	if service != "" {
		q.Set("service", service)
	}
	if scope != "" {
		q.Set("scope", scope)
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := cli.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("令牌接口返回 %d", resp.StatusCode)
	}
	var out struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Token != "" {
		return out.Token, nil
	}
	return out.AccessToken, nil
}

func parseChallenge(ch string) (realm, service, scope string) {
	ch = strings.TrimSpace(ch)
	i := strings.Index(ch, " ")
	if i <= 0 || !strings.EqualFold(ch[:i], "bearer") {
		return "", "", "" // 只处理 Bearer；Basic 不需要换 token
	}
	ch = ch[i+1:]
	for _, part := range strings.Split(ch, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(kv[0]))
		v := strings.Trim(strings.TrimSpace(kv[1]), `"`)
		switch k {
		case "realm":
			realm = v
		case "service":
			service = v
		case "scope":
			scope = v
		}
	}
	return
}

func hostOnly(u string) string {
	u = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	if i := strings.Index(u, "/"); i > 0 {
		u = u[:i]
	}
	return u
}

/* ============================ 线上镜像 ============================ */

// hubSearchResp Docker Hub 搜索接口的返回形状
type hubSearchResp struct {
	Count   int `json:"count"`
	Results []struct {
		RepoName         string `json:"repo_name"`
		ShortDescription string `json:"short_description"`
		StarCount        int    `json:"star_count"`
		PullCount        int64  `json:"pull_count"`
		IsOfficial       bool   `json:"is_official"`
		IsAutomated      bool   `json:"is_automated"`
	} `json:"results"`
}

// SearchHub 在 Docker Hub 上按关键词搜镜像（Star 数/来源/简介都是它给的）
func SearchHub(query string, page int) ([]HubRepo, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("请输入要搜索的镜像名，如 mysql / redis")
	}
	if page < 1 {
		page = 1
	}
	u := fmt.Sprintf("%s/search/repositories/?query=%s&page=%d&page_size=25",
		hubAPI, url.QueryEscape(query), page)
	cli := &http.Client{Timeout: 12 * time.Second}
	resp, err := cli.Get(u)
	if err != nil {
		return nil, fmt.Errorf("连不上 Docker Hub（%v）：请检查本机网络/代理，或改用私有仓库的镜像列表", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("Docker Hub 搜索返回 %d", resp.StatusCode)
	}
	var hr hubSearchResp
	if err := json.NewDecoder(resp.Body).Decode(&hr); err != nil {
		return nil, fmt.Errorf("解析 Docker Hub 结果失败: %v", err)
	}
	out := make([]HubRepo, 0, len(hr.Results))
	for _, r := range hr.Results {
		name := r.RepoName
		if !strings.Contains(name, "/") {
			name = "library/" + name
		}
		out = append(out, HubRepo{Name: name, Description: r.ShortDescription, Source: "Docker Hub",
			Stars: r.StarCount, Pulls: r.PullCount, Official: r.IsOfficial})
	}
	return out, nil
}

// RegistryRepos 列出私有仓库里的镜像（catalog + 每个仓库的 tag，最多各 50 个）
func RegistryRepos(id string) ([]HubRepo, error) {
	r, ok := registryByID(id)
	if !ok {
		return nil, fmt.Errorf("仓库不存在: %s", id)
	}
	if isHub(r) {
		return nil, fmt.Errorf("Docker Hub 没有目录接口，请用搜索查看线上镜像")
	}
	pass, _ := registryPassword(id)
	base := strings.TrimSuffix(normalizeRegistryURL(r.URL), "/")
	cli := registryClient(registryInsecure(hostOnly(base)))
	resp, err := registryReq(cli, http.MethodGet, base+"/v2/_catalog?n=100", r.User, pass, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("%s 拒绝访问目录：确认用户名密码，或该账号没有 catalog 权限", base)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s 未实现 /v2/_catalog（部分仓库关闭了目录接口），可改用完整镜像名直接拉取", base)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("读取仓库目录失败: HTTP %d", resp.StatusCode)
	}
	var cat struct {
		Repositories []string `json:"repositories"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&cat); err != nil {
		return nil, fmt.Errorf("解析仓库目录失败: %v", err)
	}
	host := hostOnly(base)
	out := make([]HubRepo, 0, len(cat.Repositories))
	for i, name := range cat.Repositories {
		if i >= 50 {
			break
		}
		item := HubRepo{Name: name, Source: r.Name,
			Description: "仓库：" + strings.ToUpper(strings.TrimPrefix(strings.TrimPrefix(r.URL, "https://"), "http://"))}
		item.Tags = registryTags(cli, base, name, r.User, pass)
		item.Name = host + "/" + name
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// registryTags 取某个仓库的 tag 列表（取不到就留空，不影响列表展示）
func registryTags(cli *http.Client, base, repo, user, pass string) string {
	resp, err := registryReq(cli, http.MethodGet, base+"/v2/"+repo+"/tags/list", user, pass, "")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return ""
	}
	var out struct {
		Tags []string `json:"tags"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil {
		return ""
	}
	if len(out.Tags) > 8 {
		out.Tags = out.Tags[:8]
	}
	return strings.Join(out.Tags, ", ")
}

/* ============================ 加速站（daemon.json） ============================ */

func daemonFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".docker")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建 %s 失败: %v", dir, err)
	}
	return filepath.Join(dir, daemonJSONName), nil
}

// GetDaemonConfig 读引擎的 daemon.json（只回传加速站与不安全仓库两项 + 其余键数量）
func GetDaemonConfig() (DaemonConfig, error) {
	p, err := daemonFilePath()
	if err != nil {
		return DaemonConfig{}, err
	}
	cfg := DaemonConfig{File: p}
	eng, _ := GetEngineStatus()
	cfg.EngineUp = eng.Available
	b, rerr := os.ReadFile(p)
	if rerr != nil {
		return cfg, nil // 没这个文件是常态：还没配过加速器
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return cfg, fmt.Errorf("%s 不是合法 JSON（%v）：先手工修正它，面板不会替你覆盖坏文件", p, err)
	}
	for k, v := range m {
		switch k {
		case "registry-mirrors":
			cfg.Mirrors = toStrings(v)
		case "insecure-registries":
			cfg.Insecure = toStrings(v)
		default:
			cfg.OtherKey++
		}
	}
	return cfg, nil
}

// SetDaemonConfig 只改 registry-mirrors 与 insecure-registries，其余键原样保留。
// 返回变更说明；改完需要重启引擎才生效（这里不替你重启）。
func SetDaemonConfig(mirrors, insecure []string) (string, error) {
	p, err := daemonFilePath()
	if err != nil {
		return "", err
	}
	m := map[string]any{}
	had := false
	if b, rerr := os.ReadFile(p); rerr == nil {
		had = true
		if err := json.Unmarshal(b, &m); err != nil {
			return "", fmt.Errorf("%s 不是合法 JSON（%v）：为避免写坏，本次未做任何修改", p, err)
		}
		if merr := os.WriteFile(p+".gobak", b, 0o644); merr != nil {
			return "", fmt.Errorf("备份原配置失败: %v", merr)
		}
	}
	clean := func(in []string) []string {
		out := make([]string, 0, len(in))
		seen := map[string]bool{}
		for _, s := range in {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if !strings.Contains(s, "://") {
				s = "https://" + s
			}
			s = strings.TrimSuffix(s, "/")
			if seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
		return out
	}
	mir := clean(mirrors)
	ins := clean(insecure)
	if len(mir) == 0 {
		delete(m, "registry-mirrors")
	} else {
		m["registry-mirrors"] = mir
	}
	if len(ins) == 0 {
		delete(m, "insecure-registries")
	} else {
		m["insecure-registries"] = ins
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	tmp := p + ".gotmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return "", fmt.Errorf("写入失败: %v", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		return "", fmt.Errorf("写入失败: %v", err)
	}
	verb := "已更新"
	if !had {
		verb = "已新建"
	}
	note := fmt.Sprintf("%s %s：加速站 %d 个、不安全仓库 %d 个", verb, p, len(mir), len(ins))
	if len(mir) == 0 && len(ins) == 0 {
		note = fmt.Sprintf("%s %s：两项都已清空", verb, p)
	}
	note += "；需重启 Docker 引擎后才生效"
	return note, nil
}

func toStrings(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
