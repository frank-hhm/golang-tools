//go:build windows

package docker

// ============================================================
// Docker 管理 - 镜像长任务（推送 / 导出 / 导入 / 构建 / 清理）
//
// 与拉取共用一套「可取消 + 进度」骨架：
//   beginOp → 请求里带 context → CancelImageOp 取消 → endOp
// 取消的做法是断开与 daemon 的流（daemon 侧随即停止该任务），
// 不伪造成功，也不把半成品留在本地（导入/导出会删掉临时文件）。
// 任务按名字分条登记，不同名字互不影响，所以多个拉取可以同时进行。
// ============================================================

import (
	"archive/tar"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-tools/appdirs"
)

/* ============================ 长任务取消 ============================ */

// 每个长任务按名字登记一条（"pull mysql:8.4" / "build myapp:1.0" …）。
// 名字不同互不影响：多个拉取各自一条事件流，进度不会串台。
// 名字相同（同一个镜像被再次拉取）时先断掉旧的，避免两条流写同一份进度。
type opEntry struct {
	id     uint64
	cancel context.CancelFunc
}

var (
	opMu  sync.Mutex
	ops   = map[string]opEntry{}
	opSeq uint64
)

// beginOp 登记一笔长任务；返回的 cancel 同时负责注销，defer endOp(cancel) 即可
func beginOp(name string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	opMu.Lock()
	opSeq++
	id := opSeq
	prev := ops[name]
	ops[name] = opEntry{id: id, cancel: cancel}
	opMu.Unlock()
	if prev.cancel != nil {
		prev.cancel() // 同名任务重开：先把它断掉
	}
	return ctx, func() {
		cancel()
		opMu.Lock()
		if cur, ok := ops[name]; ok && cur.id == id {
			delete(ops, name)
		}
		opMu.Unlock()
	}
}

// endOp 结束长任务（cancel 是 beginOp 返回的那个，注销已包含在里面）
func endOp(cancel context.CancelFunc) {
	if cancel != nil {
		cancel()
	}
}

// CancelImageOp 取消长任务：target 为空 = 全部取消；否则按任务名精确匹配，
// 也接受前缀（如 "pull "）或名字里的一段（如镜像引用）。返回被取消的任务名。
func CancelImageOp(target string) (string, error) {
	t := strings.TrimSpace(target)
	type hit struct {
		name   string
		cancel context.CancelFunc
	}
	opMu.Lock()
	hits := make([]hit, 0, len(ops))
	for name, e := range ops {
		if t == "" || name == t || strings.HasPrefix(name, t) || strings.Contains(name, " "+t) {
			hits = append(hits, hit{name: name, cancel: e.cancel})
		}
	}
	opMu.Unlock()
	if len(hits) == 0 {
		if t == "" {
			return "", fmt.Errorf("当前没有正在进行的镜像任务")
		}
		return "", fmt.Errorf("没有正在进行的任务：%s", t)
	}
	names := make([]string, 0, len(hits))
	for _, h := range hits {
		h.cancel()
		names = append(names, h.name)
	}
	sort.Strings(names)
	return strings.Join(names, "、"), nil
}

// CurrentImageOp 当前在跑的长任务名（多个用「、」连接，空=空闲）
func CurrentImageOp() string {
	opMu.Lock()
	defer opMu.Unlock()
	if len(ops) == 0 {
		return ""
	}
	names := make([]string, 0, len(ops))
	for name := range ops {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, "、")
}

// openStream 发起流式请求：支持 context（取消）、自定义头、任意请求体。
// 与普通请求共用并发闸门，但只占到「拿到响应头」：拉取/导出这类长任务读流时不再占名额
func openStream(ctx context.Context, method, path string, query url.Values, hdr map[string]string,
	body io.Reader, contentType string, longLived bool) (*http.Response, error) {
	release, err := apiEnter()
	if err != nil {
		return nil, err
	}
	resp, err := openStreamNoGate(ctx, method, path, query, hdr, body, contentType, longLived)
	release()
	if err != nil {
		if ctx.Err() == nil && !IsCanceled(err) {
			apiTrip(err) // 用户主动取消不算引擎故障
		}
		return nil, err
	}
	apiHeal()
	if method != http.MethodGet {
		apiInvalidate() // 标签/拉取/导入/清理都算写操作
	}
	return resp, nil
}

func openStreamNoGate(ctx context.Context, method, path string, query url.Values, hdr map[string]string,
	body io.Reader, contentType string, longLived bool) (*http.Response, error) {
	initClients()
	u := url.URL{Scheme: "http", Host: "localhost", Path: prefix() + path}
	if query != nil {
		u.RawQuery = query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	cli := httpClient
	if longLived {
		cli = streamCli
	}
	resp, err := cli.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ErrCanceled
		}
		return nil, unavailableErr(err)
	}
	return resp, nil
}

// registryAuthHeader 按镜像引用找到匹配仓库的登录信息，编码成 X-Registry-Auth。
// 没有域名的引用按 Docker Hub 处理（仓库管理里登记了 Hub 账号就会带上）。
func registryAuthHeader(ref string) string {
	host := refHost(ref)
	hubRef := host == ""
	for _, r := range ListRegistries() {
		if r.User == "" {
			continue
		}
		if hubRef {
			if !isHub(r) {
				continue
			}
		} else if !sameRegistry(r.URL, host) {
			continue
		}
		pw, err := registryPassword(r.ID)
		if err != nil {
			continue
		}
		server := host
		if hubRef {
			server = hubURL
		}
		payload := map[string]string{"username": r.User, "password": pw, "serveraddress": server}
		b, _ := json.Marshal(payload)
		return base64.StdEncoding.EncodeToString(b)
	}
	return ""
}

/* ============================ 推送 ============================ */

// TagImage 给本地镜像打一个新 tag。推送私有仓库前必须先改名：daemon 只按 ref 里的
// 仓库地址找登录信息，而本地镜像名通常不带仓库地址。
func TagImage(source, target string) error {
	src := strings.TrimSpace(source)
	repo, tag := splitRepoTag(strings.TrimSpace(target))
	if src == "" || repo == "" {
		return fmt.Errorf("镜像引用不能为空")
	}
	q := url.Values{}
	q.Set("repo", repo)
	q.Set("tag", tag)
	return do(http.MethodPost, "/images/"+src+"/tag", q, nil, nil)
}

// PushImageTo 把本地镜像 localRef 以 targetRef 的身份推出去：两者不同就先打 tag
// （不动原镜像），再推 targetRef。targetRef 为空时等同于推送 localRef。
func PushImageTo(localRef, targetRef string) error {
	local := strings.TrimSpace(localRef)
	target := strings.TrimSpace(targetRef)
	if target == "" {
		target = local
	}
	if local != target {
		if !ImageExists(local) {
			return fmt.Errorf("本地没有镜像 %s", local)
		}
		if err := TagImage(local, target); err != nil {
			return fmt.Errorf("打 tag %s → %s 失败: %w", local, target, err)
		}
	}
	return PushImage(target)
}

// PushImage 推送本地镜像到它 tag 里写的仓库（ref 形如 harbor.corp/ns/app:1.0）。
// 必须先有匹配的仓库登录信息，否则 daemon 只会回 401/403（这里提前拦下并说明原因）。
func PushImage(ref string) error {
	repo, tag := splitRepoTag(ref)
	if repo == "" {
		return fmt.Errorf("镜像引用不能为空")
	}
	host := refHost(ref)
	auth := registryAuthHeader(ref)
	if host != "" && auth == "" {
		return fmt.Errorf("未找到 %s 的登录信息：先在「仓库管理」里加上这个仓库并保存用户名密码，再推送", host)
	}
	q := url.Values{}
	q.Set("tag", tag)
	emitImageOp(ImageTaskProgress{Op: "push", Image: ref, Stage: "info", Note: "开始推送 " + ref})
	ctx, cancel := beginOp("push " + ref)
	defer endOp(cancel)
	hdr := map[string]string{}
	if auth != "" {
		hdr["X-Registry-Auth"] = auth
	}
	resp, err := openStream(ctx, http.MethodPost, "/images/"+repo+"/push", q, hdr, nil, "", true)
	if err != nil {
		return imageOpFail("push", ref, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		return imageOpFail("push", ref, apiError(resp.StatusCode, data))
	}
	dec := json.NewDecoder(resp.Body)
	tr := &layerTracker{cur: map[string]int64{}, tot: map[string]int64{}, done: map[string]bool{},
		seen: map[string]struct{}{}}
	for {
		var m struct {
			Status         string `json:"status"`
			ID             string `json:"id"`
			Progress       string `json:"progress"`
			Error          string `json:"error"`
			ProgressDetail struct {
				Current int64 `json:"current"`
				Total   int64 `json:"total"`
			} `json:"progressDetail"`
		}
		if err := dec.Decode(&m); err != nil {
			if err == io.EOF {
				break
			}
			return imageOpFail("push", ref, cancelErr(ctx, err))
		}
		if m.Error != "" {
			return imageOpFail("push", ref, fmt.Errorf("%s", m.Error))
		}
		if m.ID == "" && m.Status == "" {
			continue
		}
		complete := strings.Contains(m.Status, "Pushed") || strings.Contains(m.Status, "Layer already exists")
		tr.update(m.ID, m.ProgressDetail.Current, m.ProgressDetail.Total, complete)
		done, total, layers, doneN := tr.snapshot()
		emitImageOp(ImageTaskProgress{Op: "push", Image: ref, Stage: "running", Status: m.Status,
			Percent: tr.overall(), Bytes: done, Total: total, Note: m.Progress})
		_ = layers
		_ = doneN
	}
	emitImageOp(ImageTaskProgress{Op: "push", Image: ref, Stage: "done", Percent: 100, Note: "已推送 " + ref})
	return nil
}

/* ============================ 导出 / 导入 ============================ */

// DefaultExportPath 导出镜像的默认落盘位置：<数据目录>/docker/exports/<repo>-<tag>-<时间>.tar
func DefaultExportPath(ref string) string {
	dir, err := appdirs.Data("docker")
	if err != nil {
		dir = os.TempDir()
	} else {
		dir = filepath.Join(dir, "exports")
	}
	name := strings.NewReplacer("/", "_", ":", "-", "\\", "_").Replace(ref)
	return filepath.Join(dir, name+"-"+time.Now().Format("20060102-150405")+".tar")
}

// ExportImage 把镜像导出成 docker save 的 tar 包，流式写盘并回报进度。
// 写盘用 .part 临时文件，成功后改名：中途取消不会留下看起来完整的坏包。
func ExportImage(ref, outPath string) (string, error) {
	if strings.TrimSpace(outPath) == "" {
		return "", fmt.Errorf("导出路径不能为空")
	}
	if !strings.HasSuffix(strings.ToLower(outPath), ".tar") {
		outPath += ".tar"
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return "", fmt.Errorf("创建导出目录失败: %v", err)
	}
	part := outPath + ".part"
	emitImageOp(ImageTaskProgress{Op: "export", Image: ref, Stage: "info", Note: "开始导出到 " + outPath})
	ctx, cancel := beginOp("export " + ref)
	defer endOp(cancel)
	resp, err := openStream(ctx, http.MethodGet, "/images/"+ref+"/get", nil, nil, nil, "", true)
	if err != nil {
		return "", imageOpFail("export", ref, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		return "", imageOpFail("export", ref, apiError(resp.StatusCode, data))
	}
	f, err := os.Create(part)
	if err != nil {
		return "", imageOpFail("export", ref, err)
	}
	total := resp.ContentLength
	buf := make([]byte, 512*1024)
	var done int64
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				_ = f.Close()
				_ = os.Remove(part)
				return "", imageOpFail("export", ref, werr)
			}
			done += int64(n)
			emitImageOp(ImageTaskProgress{Op: "export", Image: ref, Stage: "running",
				Percent: pctOf(done, total), Bytes: done, Total: total,
				Note: humanBytes(done) + " / " + humanBytes(total)})
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			_ = f.Close()
			_ = os.Remove(part)
			return "", imageOpFail("export", ref, cancelErr(ctx, rerr))
		}
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(part)
		return "", imageOpFail("export", ref, err)
	}
	if err := os.Rename(part, outPath); err != nil {
		_ = os.Remove(part)
		return "", imageOpFail("export", ref, err)
	}
	emitImageOp(ImageTaskProgress{Op: "export", Image: ref, Stage: "done", Percent: 100,
		Bytes: done, Total: done, Note: "已导出 " + outPath + "（" + humanBytes(done) + "）"})
	return outPath, nil
}

// ImportImage 从 docker save 的 tar 包导入镜像（可选 ref 重命名，空则沿用包内 tag）
func ImportImage(tarPath, ref string) (string, error) {
	st, err := os.Stat(tarPath)
	if err != nil {
		return "", fmt.Errorf("读不到导入文件: %v", err)
	}
	f, err := os.Open(tarPath)
	if err != nil {
		return "", fmt.Errorf("打开导入文件失败: %v", err)
	}
	defer f.Close()
	if ref == "" {
		ref = filepath.Base(tarPath)
	}
	emitImageOp(ImageTaskProgress{Op: "import", Image: ref, Stage: "info", Note: "开始导入 " + filepath.Base(tarPath)})
	ctx, cancel := beginOp("import " + ref)
	defer endOp(cancel)
	body := &countReader{r: f, total: st.Size(), onTick: func(done int64) {
		emitImageOp(ImageTaskProgress{Op: "import", Image: ref, Stage: "running",
			Percent: pctOf(done, st.Size()), Bytes: done, Total: st.Size(),
			Note: "已上传 " + humanBytes(done) + " / " + humanBytes(st.Size())})
	}}
	resp, err := openStream(ctx, http.MethodPost, "/images/load", url.Values{"quiet": {"0"}},
		nil, body, "application/x-tar", true)
	if err != nil {
		return "", imageOpFail("import", ref, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		return "", imageOpFail("import", ref, apiError(resp.StatusCode, data))
	}
	dec := json.NewDecoder(resp.Body)
	loaded := []string{}
	last := ""
	for {
		var m struct {
			Stream string `json:"stream"`
			Error  string `json:"error"`
		}
		if err := dec.Decode(&m); err != nil {
			if err == io.EOF {
				break
			}
			return "", imageOpFail("import", ref, cancelErr(ctx, err))
		}
		if m.Error != "" {
			return "", imageOpFail("import", ref, fmt.Errorf("%s", m.Error))
		}
		if s := strings.TrimSpace(m.Stream); s != "" {
			last = s
			if strings.HasPrefix(s, "Loaded image") {
				loaded = append(loaded, strings.TrimSpace(strings.TrimPrefix(s, "Loaded image:")))
			}
			emitImageOp(ImageTaskProgress{Op: "import", Image: ref, Stage: "running", Percent: 100, Note: s})
		}
	}
	n := len(loaded)
	note := "导入完成"
	if n > 0 {
		note = "导入完成：" + strings.Join(loaded, "、")
	} else if last != "" {
		note = "导入完成：" + last
	}
	emitImageOp(ImageTaskProgress{Op: "import", Image: ref, Stage: "done", Percent: 100, Note: note})
	return note, nil
}

// countReader 上报读取进度（导入镜像时前端能看到上传字节）
type countReader struct {
	r      io.Reader
	done   int64
	total  int64
	onTick func(int64)
	last   time.Time
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.done += int64(n)
		if c.onTick != nil && time.Since(c.last) > 200*time.Millisecond {
			c.last = time.Now()
			c.onTick(c.done)
		}
	}
	return n, err
}

/* ============================ 构建 ============================ */

// BuildImage 用目录里的 Dockerfile 构建镜像（POST /build，tar 上下文流式上传）。
// 只跳过 .git 与 node_modules 这类必然无用且巨大的目录，其余原样打包。
func BuildImage(dir, dockerfile, tag string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", fmt.Errorf("构建目录不能为空")
	}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return "", fmt.Errorf("构建目录不存在: %s", dir)
	}
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	if tag == "" {
		return "", fmt.Errorf("镜像标签不能为空，如 myapp:1.0")
	}
	pr, pw := io.Pipe()
	emitImageOp(ImageTaskProgress{Op: "build", Image: tag, Stage: "info", Note: "开始构建 " + tag})
	go func() {
		tw := tar.NewWriter(pw)
		err := writeBuildContext(tw, dir)
		if cerr := tw.Close(); err == nil {
			err = cerr
		}
		_ = pw.CloseWithError(err)
	}()
	ctx, cancel := beginOp("build " + tag)
	defer endOp(cancel)
	q := url.Values{}
	q.Set("t", tag)
	q.Set("dockerfile", dockerfile)
	q.Set("rm", "1")
	resp, err := openStream(ctx, http.MethodPost, "/build", q, nil, pr, "application/x-tar", true)
	if err != nil {
		return "", imageOpFail("build", tag, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return "", imageOpFail("build", tag, apiError(resp.StatusCode, data))
	}
	dec := json.NewDecoder(resp.Body)
	step, steps := 0, 0
	for {
		var m struct {
			Stream string `json:"stream"`
			Error  string `json:"error"`
			Aux    struct {
				ID string `json:"ID"`
			} `json:"aux"`
			ErrorDetail struct {
				Message string `json:"message"`
			} `json:"errorDetail"`
		}
		if err := dec.Decode(&m); err != nil {
			if err == io.EOF {
				break
			}
			return "", imageOpFail("build", tag, cancelErr(ctx, err))
		}
		if m.Error != "" {
			return "", imageOpFail("build", tag, fmt.Errorf("%s", m.Error))
		}
		if m.ErrorDetail.Message != "" {
			return "", imageOpFail("build", tag, fmt.Errorf("%s", m.ErrorDetail.Message))
		}
		s := strings.TrimRight(m.Stream, "\r\n")
		if s == "" {
			continue
		}
		for _, line := range strings.Split(s, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// Step 3/12 决定百分比；其余行只当日志推给前端
			if strings.HasPrefix(line, "Step ") {
				if a, b, ok := parseStep(line); ok {
					step, steps = a, b
				}
			}
			emitImageOp(ImageTaskProgress{Op: "build", Image: tag, Stage: "running",
				Percent: pctOf(int64(step), int64(steps)), Note: line})
		}
	}
	if step > 0 && steps > 0 && step < steps {
		emitImageOp(ImageTaskProgress{Op: "build", Image: tag, Stage: "running",
			Percent: pctOf(int64(step), int64(steps)), Note: "构建流已结束但未跑完全部步骤，请检查输出"})
	}
	emitImageOp(ImageTaskProgress{Op: "build", Image: tag, Stage: "done", Percent: 100, Note: "已构建 " + tag})
	return tag, nil
}

// parseStep 解析 "Step 3/12 : RUN ..." 里的两个数
func parseStep(line string) (int, int, bool) {
	rest := strings.TrimPrefix(line, "Step ")
	if i := strings.Index(rest, ":"); i > 0 {
		rest = rest[:i]
	}
	parts := strings.SplitN(strings.TrimSpace(rest), "/", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	a, e1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	b, e2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if e1 != nil || e2 != nil {
		return 0, 0, false
	}
	return a, b, true
}

// writeBuildContext 把目录打成 tar（跳过 .git / node_modules / 产物目录）
func writeBuildContext(tw *tar.Writer, root string) error {
	skip := map[string]bool{".git": true, "node_modules": true, ".idea": true}
	return filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		first := strings.SplitN(rel, "/", 2)[0]
		if skip[first] {
			if fi.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		hdr, herr := tar.FileInfoHeader(fi, "")
		if herr != nil {
			return herr
		}
		hdr.Name = rel
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		f, oerr := os.Open(p)
		if oerr != nil {
			return oerr
		}
		defer f.Close()
		_, cerr := io.Copy(tw, f)
		return cerr
	})
}

/* ============================ 清理 ============================ */

// PruneImages 清理无用镜像：all=false 只清悬空镜像（<none>），true 连同未被容器使用的关联镜像
func PruneImages(all bool) (PruneReport, error) {
	ctx, cancel := beginOp("prune images")
	defer endOp(cancel)
	filters := `{"dangling":{"false":true}}`
	if !all {
		filters = `{"dangling":{"true":true}}`
	}
	q := url.Values{}
	q.Set("filters", filters)
	emitImageOp(ImageTaskProgress{Op: "prune", Image: "images", Stage: "info", Note: "开始清理无用镜像…"})
	resp, err := openStream(ctx, http.MethodPost, "/images/prune", q, nil, nil, "", false)
	if err != nil {
		return PruneReport{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		return PruneReport{}, apiError(resp.StatusCode, data)
	}
	var out struct {
		ImagesDeleted []struct {
			Deleted  string `json:"Deleted"`
			Untagged string `json:"Untagged"`
		} `json:"ImagesDeleted"`
		SpaceReclaimed int64 `json:"SpaceReclaimed"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return PruneReport{}, fmt.Errorf("解析清理结果失败: %v", err)
	}
	rep := PruneReport{Space: out.SpaceReclaimed}
	for _, d := range out.ImagesDeleted {
		if d.Deleted != "" {
			rep.Deleted = append(rep.Deleted, shortID(d.Deleted))
		} else if d.Untagged != "" {
			rep.Deleted = append(rep.Deleted, d.Untagged)
		}
	}
	note := fmt.Sprintf("清理完成：%d 个镜像，回收 %s", len(rep.Deleted), humanBytes(rep.Space))
	emitImageOp(ImageTaskProgress{Op: "prune", Image: "images", Stage: "done", Percent: 100, Note: note})
	return rep, nil
}

/* ============================ 镜像 → 容器 ============================ */

// RunImage 由镜像直接创建容器（可选立刻启动）。镜像不在本地时先拉一次：
// 从「线上镜像」直接点创建容器，不该要求用户先单独拉取。
func RunImage(spec ImageRunSpec) (string, error) {
	if strings.TrimSpace(spec.Image) == "" {
		return "", fmt.Errorf("镜像不能为空")
	}
	if !ImageExists(spec.Image) {
		if err := PullImage(spec.Image); err != nil {
			return "", fmt.Errorf("镜像不在本地，自动拉取失败: %w", err)
		}
	}
	env := []string{}
	for _, line := range strings.Split(spec.Env, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "=") {
			continue
		}
		env = append(env, line)
	}
	id, err := CreateContainer(ContainerSpec{
		Name:          strings.TrimSpace(spec.Name),
		Image:         spec.Image,
		Env:           env,
		ContainerPort: spec.ContainerPort,
		HostPort:      spec.HostPort,
	})
	if err != nil {
		return "", err
	}
	if spec.AutoStart {
		if err := StartContainer(id); err != nil {
			return id, fmt.Errorf("容器已创建（%s）但启动失败: %v", shortID(id), err)
		}
	}
	return id, nil
}

/* ============================ 进度辅助 ============================ */

func emitImageOp(p ImageTaskProgress) { emit("docker:imageop", p) }

func imageOpFail(op, ref string, err error) error {
	e := cancelErr(nil, err)
	emitImageOp(ImageTaskProgress{Op: op, Image: ref, Stage: "error", Note: e.Error()})
	return e
}

// cancelErr 把“用户点了取消”与真正的失败区分开，前端据此显示「已取消」而不是「失败」
func cancelErr(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ErrCanceled
	}
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "context canceled") {
		return ErrCanceled
	}
	return err
}

func pctOf(done, total int64) int {
	if total <= 0 {
		return 0
	}
	p := int(done * 100 / total)
	if p > 100 {
		p = 100
	}
	if p < 0 {
		p = 0
	}
	return p
}

// layerTracker 汇总各分层进度，换算成可信的整体百分比。
// 只按已见分层加权：未出现的层不参与，避免一开始就 0% 卡着不动。
type layerTracker struct {
	cur  map[string]int64
	tot  map[string]int64
	done map[string]bool
	// 出现过的层（含 daemon 没给 total 的）：只按 total 计层数时，
	// 会把「还有一层在下载但没报大小」误算成全部完成，总进度提前跳到 100%
	seen map[string]struct{}
}

func (t *layerTracker) update(id string, cur, tot int64, complete bool) {
	// 层 ID 至少是 12 位摘要：“Pulling from library/redis” 带的 ID 是 tag（latest），
	// 把它当一层会让分层数虚高一行
	if len(id) < 12 {
		return
	}
	if t.seen != nil {
		t.seen[id] = struct{}{}
	}
	if tot > 0 {
		t.tot[id] = tot
	}
	if cur > 0 {
		t.cur[id] = cur
	}
	if complete {
		t.done[id] = true
		if v, ok := t.tot[id]; ok {
			t.cur[id] = v
		}
	}
}

func (t *layerTracker) snapshot() (done, total int64, layers, doneN int) {
	for id, v := range t.tot {
		total += v
		if c, ok := t.cur[id]; ok {
			done += c
		}
	}
	if t.seen != nil {
		layers = len(t.seen)
	} else {
		layers = len(t.tot)
	}
	doneN = len(t.done)
	if doneN > layers {
		doneN = layers
	}
	if done > total {
		done = total
	}
	return
}

// untrustedBytes 小于此值的 total 视为“daemon 根本没报字节数”（只给了层元数据），
// 拿它算百分比会出现“刚开始就 99%”，这时改按已完成层数估算
const untrustedBytes = 1 << 20

func (t *layerTracker) overall() int {
	done, total, layers, doneN := t.snapshot()
	if layers > 0 && doneN >= layers {
		return 100
	}
	if total < untrustedBytes {
		if layers <= 0 {
			return 0
		}
		p := doneN * 100 / layers
		if p > 99 {
			p = 99 // 还有层在收尾：别让总进度先到 100
		}
		return p
	}
	if total <= 0 {
		return 0
	}
	p := int(done * 100 / total)
	if p > 99 {
		p = 99 // 还有层在收尾：别让总进度先到 100
	}
	return p
}

// refHost 取镜像引用里的仓库主机名；Docker Hub（不带域名的 library/xxx）返回空串
func refHost(ref string) string {
	ref = strings.TrimSpace(ref)
	ref = strings.TrimPrefix(ref, "docker.io/")
	ref = strings.TrimPrefix(ref, "index.docker.io/")
	i := strings.Index(ref, "/")
	if i <= 0 {
		return ""
	}
	first := ref[:i]
	if strings.ContainsAny(first, ".:") || first == "localhost" {
		return first
	}
	return ""
}
