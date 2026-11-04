package clouddrive

// PR-C 的用例：目录整体改名/移动（批量 copy 前缀）与分块上传断点续传
// （计划-云盘对象存储对接 ②③）。
//
// 全部跑在 httptest 假后端 + 测试注入的临时目录上（TestMain 把 clouddrive 的数据目录
// 指到 os.MkdirTemp，uploads.json 因此落在同一处，可以模拟「进程重启后再读同一份」）。

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

/* ============================ ③ 目录整体改名 / 移动 ============================ */

// writeTempFile 在测试临时目录里造一个本地源文件（分块续传要按文件指纹识别）。
func writeTempFile(t *testing.T, name string, bs []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, bs, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// openForUpload 打开源文件（测试结束时统一关闭）。
func openForUpload(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// seqMark / seqAfter 用调用序号做「从这一刻起」的切片标记，验续传时的精确调用序。
func seqMark(f *fakeOS) int { return len(f.seqList()) }

func seqAfter(f *fakeOS, mark int) []string { return f.seqList()[mark:] }

func TestDirRenameMovesWholePrefix(t *testing.T) {
	f := newFakeOS(t, "tree-bucket")
	o, _ := newTestOS(t, KindAliyunOSS, "OSS", f)
	if _, err := o.Mkdir(context.Background(), "", "docs"); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Mkdir(context.Background(), "docs", "sub"); err != nil {
		t.Fatal(err)
	}
	files := []struct{ parent, name, body string }{
		{"docs", "a.txt", "A"},
		{"docs", "报告 v1+2.bin", "BB"},
		{"docs/sub", "c.txt", "C"},
		{"", "keep.txt", "K"},
	}
	for _, fl := range files {
		if _, err := o.Upload(context.Background(), fl.parent, fl.name,
			bytesReader([]byte(fl.body)), int64(len(fl.body))); err != nil {
			t.Fatalf("预置 %s/%s 失败: %v", fl.parent, fl.name, err)
		}
	}

	if err := o.Rename(context.Background(), "docs/", "notes"); err != nil {
		t.Fatalf("目录整体改名应成功: %v", err)
	}
	for _, want := range []string{"notes/", "notes/a.txt", "notes/报告 v1+2.bin", "notes/sub/", "notes/sub/c.txt"} {
		if _, ok := f.get(want); !ok {
			t.Fatalf("新前缀下缺 %s，实际 keys=%v", want, f.keys())
		}
	}
	for _, gone := range []string{"docs/", "docs/a.txt", "docs/报告 v1+2.bin", "docs/sub/", "docs/sub/c.txt"} {
		if _, ok := f.get(gone); ok {
			t.Fatalf("旧 key 未删除: %s，实际 keys=%v", gone, f.keys())
		}
	}
	if _, ok := f.get("keep.txt"); !ok {
		t.Fatal("前缀外的对象不该被动到")
	}
	if got := string(f.mustGet("notes/报告 v1+2.bin")); got != "BB" {
		t.Fatalf("子对象内容未原样搬过去: %q", got)
	}
	// 每个对象都是「先全部 copy，再删源」：这样中途失败时旧目录仍完整可用。
	copies, deletes := 0, 0
	for _, s := range f.seqList() {
		switch {
		case strings.HasPrefix(s, "PUT /tree-bucket/notes"):
			copies++
		case strings.HasPrefix(s, "DELETE /tree-bucket/docs"):
			deletes++
		}
	}
	if copies != 5 || deletes != 5 {
		t.Fatalf("5 个子对象应各一次 copy 与 delete，实际 copy=%d delete=%d：%v", copies, deletes, f.seqList())
	}
	// 搬完还能列出来（列表按 CommonPrefixes 折叠，占位对象与新层级都在）。
	entries, err := o.List(context.Background(), "notes/sub")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "c.txt" {
		t.Fatalf("新层级下的列表不对: %+v", entries)
	}
}

// TestDirRenameStopsBeforeDeletingSource 搬目录必须是「先把全部对象 copy 完，再开始删源」：
// 复制中途失败时旧目录还得完整可用（若实现成「拷一个删一个」，前面的源已经没了）。
func TestDirRenameStopsBeforeDeletingSource(t *testing.T) {
	f := newFakeOS(t, "half-bucket")
	o, _ := newTestOS(t, KindS3, "S3", f)
	if _, err := o.Mkdir(context.Background(), "", "docs"); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"a.txt", "b.txt", "c.txt"} {
		if _, err := o.Upload(context.Background(), "docs", n, bytesReader([]byte(n)), 1); err != nil {
			t.Fatal(err)
		}
	}
	// 字典序上 docs/ 与 docs/a.txt 已拷到新前缀，轮到 docs/b.txt 时复制被拒（拒的是**目标** key）。
	f.failCopyToKey("notes/b.txt")
	if err := o.Rename(context.Background(), "docs/", "notes"); err == nil {
		t.Fatal("复制中途失败时改名应报错")
	}
	for _, k := range []string{"docs/", "docs/a.txt", "docs/b.txt", "docs/c.txt"} {
		if _, ok := f.get(k); !ok {
			t.Fatalf("搬失败后旧目录必须原样可用，缺 %s（keys=%v）", k, f.keys())
		}
	}
	for _, s := range f.seqList() {
		if strings.HasPrefix(s, "DELETE /half-bucket/docs") {
			t.Fatalf("一个都没拷完就开始删源：%s", s)
		}
	}

	// 故障撤掉后重跑同一操作：已拷到新前缀的那几个会被覆盖，最终应当完整搬成。
	f.allowCopy()
	if err := o.Rename(context.Background(), "docs/", "notes"); err != nil {
		t.Fatalf("搬目录应可幂等重跑: %v", err)
	}
	for _, k := range []string{"notes/", "notes/a.txt", "notes/b.txt", "notes/c.txt"} {
		if _, ok := f.get(k); !ok {
			t.Fatalf("重跑后新前缀缺 %s（keys=%v）", k, f.keys())
		}
	}
	for _, k := range f.keys() {
		if strings.HasPrefix(k, "docs") {
			t.Fatalf("重跑后旧前缀仍残留 %s（keys=%v）", k, f.keys())
		}
	}
}

func TestDirMoveIntoOtherDir(t *testing.T) {
	f := newFakeOS(t, "mv-bucket")
	o, _ := newTestOS(t, KindS3, "S3", f)
	if _, err := o.Mkdir(context.Background(), "", "docs"); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Upload(context.Background(), "docs", "a.txt", bytesReader([]byte("A")), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Mkdir(context.Background(), "", "archive"); err != nil {
		t.Fatal(err)
	}
	if err := o.Move(context.Background(), "docs/", "archive"); err != nil {
		t.Fatalf("目录移动应成功: %v", err)
	}
	if _, ok := f.get("archive/docs/a.txt"); !ok {
		t.Fatalf("未按前缀整体搬移，实际 keys=%v", f.keys())
	}
	if _, ok := f.get("archive/docs/"); !ok {
		t.Fatalf("目录占位对象应一起搬过去，实际 keys=%v", f.keys())
	}
	if _, ok := f.get("docs/a.txt"); ok {
		t.Fatalf("源目录残留未清，实际 keys=%v", f.keys())
	}
}

func TestDirMoveIntoOwnSubdirFails(t *testing.T) {
	f := newFakeOS(t, "loop-bucket")
	o, _ := newTestOS(t, KindTencentCOS, "COS", f)
	if _, err := o.Mkdir(context.Background(), "", "docs"); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Mkdir(context.Background(), "docs", "sub"); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Upload(context.Background(), "docs", "a.txt", bytesReader([]byte("A")), 1); err != nil {
		t.Fatal(err)
	}
	before := append([]string(nil), f.keys()...)
	if err := o.Move(context.Background(), "docs/", "docs/sub"); err == nil {
		t.Fatal("把目录移动到自己下面必须被拒")
	}
	if got := append([]string(nil), f.keys()...); strings.Join(got, "\n") != strings.Join(before, "\n") {
		t.Fatalf("被拒后不应改动任何对象: before=%v after=%v", before, got)
	}
}

func TestDirRenameMissingDirErrors(t *testing.T) {
	f := newFakeOS(t, "none-bucket")
	o, _ := newTestOS(t, KindAliyunOSS, "OSS", f)
	if err := o.Rename(context.Background(), "nope/", "x"); err == nil {
		t.Fatal("不存在的前缀应报错，不能当成功")
	}
}

func TestVirtualDirWithoutPlaceholderMoves(t *testing.T) {
	// 没有占位对象的「纯前缀目录」（别的客户端只 PUT 子对象时就是这个形状）。
	f := newFakeOS(t, "vir-bucket")
	o, _ := newTestOS(t, KindS3, "S3", f)
	f.seed("raw/a.txt", []byte("A"))
	if err := o.Rename(context.Background(), "raw/", "stored"); err != nil {
		t.Fatalf("无占位对象的目录也应整体改名: %v", err)
	}
	if _, ok := f.get("stored/a.txt"); !ok {
		t.Fatalf("子对象未搬到新前缀: keys=%v", f.keys())
	}
	if _, ok := f.get("raw/a.txt"); ok {
		t.Fatalf("源未清理: keys=%v", f.keys())
	}
}

func TestCopySourceIsURLEncoded(t *testing.T) {
	f := newFakeOS(t, "cs-bucket")
	o, _ := newTestOS(t, KindAliyunOSS, "OSS", f)
	if _, err := o.Upload(context.Background(), "", "报告 v1+2.bin", bytesReader([]byte("BB")), 2); err != nil {
		t.Fatal(err)
	}
	if err := o.Rename(context.Background(), "报告 v1+2.bin", "改名 后.bin"); err != nil {
		t.Fatalf("带空格/加号的源应能改名: %v", err)
	}
	if got := f.gotCopySrc(); got != "/cs-bucket/%E6%8A%A5%E5%91%8A%20v1%2B2.bin" {
		t.Fatalf("copy-source 必须是 URL 编码态（`+` 不编码会被服务端当空格）: %q", got)
	}
	if got := string(f.mustGet("改名 后.bin")); got != "BB" {
		t.Fatalf("改名后内容不对: %q", got)
	}
}

func TestDeleteDirRemovesWholePrefix(t *testing.T) {
	f := newFakeOS(t, "del-bucket")
	o, _ := newTestOS(t, KindS3, "S3", f)
	if _, err := o.Mkdir(context.Background(), "", "docs"); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Mkdir(context.Background(), "docs", "sub"); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Upload(context.Background(), "docs/sub", "c.txt", bytesReader([]byte("C")), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Upload(context.Background(), "", "keep.txt", bytesReader([]byte("K")), 1); err != nil {
		t.Fatal(err)
	}
	if err := o.Delete(context.Background(), "docs/"); err != nil {
		t.Fatalf("删目录应成功: %v", err)
	}
	for _, k := range f.keys() {
		if strings.HasPrefix(k, "docs") {
			t.Fatalf("删目录后仍残留 %s，keys=%v", k, f.keys())
		}
	}
	if _, ok := f.get("keep.txt"); !ok {
		t.Fatal("目录外的对象不该被误删")
	}
	entries, err := o.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "keep.txt" {
		t.Fatalf("列表里目录应彻底消失: %+v", entries)
	}
}

/* ============================ ② 分块断点续传 ============================ */

func TestMultipartResumeAfterCrash(t *testing.T) {
	shrinkParts(t, 1<<10, 1<<10)
	body := bytes.Repeat([]byte("z"), 2500) // 1024 + 1024 + 452 → 三片
	src := writeTempFile(t, "big.bin", body)
	f := newFakeOS(t, "resume-bucket")
	o, _ := newTestOS(t, KindTencentCOS, "COS", f)
	f.failPartWrite(2)

	if _, err := o.Upload(context.Background(), "docs", "big.bin", openForUpload(t, src), int64(len(body))); err == nil {
		t.Fatal("第 2 片被拒时上传应失败")
	}
	for _, s := range f.seqList() {
		if strings.HasPrefix(s, "DELETE /resume-bucket") {
			t.Fatalf("源是本地文件时必须留着会话，不能 Abort：%v", f.seqList())
		}
	}
	sess := listUploadSessionsForTest(o.acctID)
	if len(sess) != 1 {
		t.Fatalf("应落一条续传会话，实际 %d 条", len(sess))
	}
	if sess[0].UploadID != "upload-1" || sess[0].Key != "docs/big.bin" || sess[0].PartSize != 1<<10 {
		t.Fatalf("会话坐标不对: %+v", sess[0])
	}
	if len(sess[0].Parts) != 1 || sess[0].Parts[0].Num != 1 || sess[0].Parts[0].Size != 1024 {
		t.Fatalf("会话应只记到第 1 片: %+v", sess[0].Parts)
	}
	if sess[0].SrcPath != src || sess[0].SrcSize != int64(len(body)) {
		t.Fatalf("源文件指纹不对: %+v", sess[0])
	}

	// 「重启」：换一个 provider 实例，重新打开同一个文件再传一次。
	mark := seqMark(f)
	o2, err := New(sess[0].AcctID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o2.Upload(context.Background(), "docs", "big.bin", openForUpload(t, src), int64(len(body))); err != nil {
		t.Fatalf("续传应成功: %v", err)
	}
	want := strings.Join([]string{
		"GET /resume-bucket/docs/big.bin?uploadId=upload-1",              // 先与云端核对已传分片
		"PUT /resume-bucket/docs/big.bin?partNumber=2&uploadId=upload-1", // 只补第 2、3 片
		"PUT /resume-bucket/docs/big.bin?partNumber=3&uploadId=upload-1", // 跳过了 partNumber=1
		"POST /resume-bucket/docs/big.bin?uploadId=upload-1",             // 仍是同一个 UploadId
		"HEAD /resume-bucket/docs/big.bin",                               // statEntry
	}, "\n")
	if got := strings.Join(seqAfter(f, mark), "\n"); got != want {
		t.Fatalf("续传的调用序不对:\n got: %s\nwant: %s", got, want)
	}
	if got := f.mustGet("docs/big.bin"); !bytes.Equal(got, body) {
		t.Fatalf("续传拼出的对象与原文件不一致: got %d want %d", len(got), len(body))
	}
	if left := listUploadSessionsForTest(o.acctID); len(left) != 0 {
		t.Fatalf("Complete 成功后应清掉会话，残留 %+v", left)
	}
}

func TestMultipartResumeDropsChangedSource(t *testing.T) {
	shrinkParts(t, 1<<10, 1<<10)
	body := bytes.Repeat([]byte("a"), 2500)
	src := writeTempFile(t, "chg.bin", body)
	f := newFakeOS(t, "chg-bucket")
	o, _ := newTestOS(t, KindS3, "S3", f)
	f.failPartWrite(2)
	if _, err := o.Upload(context.Background(), "", "chg.bin", openForUpload(t, src), int64(len(body))); err == nil {
		t.Fatal("分片被拒时上传应失败")
	}
	if left := listUploadSessionsForTest(o.acctID); len(left) != 1 {
		t.Fatalf("应先落一条会话: %+v", left)
	}

	// 源文件被改过（大小与 mtime 都变）：旧 UploadId 的分片字节已不属于新内容。
	time.Sleep(20 * time.Millisecond)
	bigger := bytes.Repeat([]byte("b"), 3000)
	if err := os.WriteFile(src, bigger, 0o600); err != nil {
		t.Fatal(err)
	}
	mark := seqMark(f)
	if _, err := o.Upload(context.Background(), "", "chg.bin", openForUpload(t, src), int64(len(bigger))); err != nil {
		t.Fatalf("源变了应能重头传成功: %v", err)
	}
	after := strings.Join(seqAfter(f, mark), "\n")
	if !strings.Contains(after, "POST /chg-bucket/chg.bin?uploads") {
		t.Fatalf("源指纹变了必须重新发起分块上传：%s", after)
	}
	if strings.Contains(after, "GET /chg-bucket/chg.bin?uploadId=upload-1") {
		t.Fatalf("源变了不该再去核对旧 UploadId：%s", after)
	}
	if got := f.mustGet("chg.bin"); !bytes.Equal(got, bigger) {
		t.Fatalf("最终对象应与新内容一致: got %d want %d", len(got), len(bigger))
	}
}

func TestMultipartResumeFallsBackWhenRemotePartsGone(t *testing.T) {
	shrinkParts(t, 1<<10, 1<<10)
	body := bytes.Repeat([]byte("c"), 2500)
	src := writeTempFile(t, "gone.bin", body)
	f := newFakeOS(t, "gone-bucket")
	o, _ := newTestOS(t, KindAliyunOSS, "OSS", f)
	f.failPartWrite(2)
	if _, err := o.Upload(context.Background(), "", "gone.bin", openForUpload(t, src), int64(len(body))); err == nil {
		t.Fatal("分片被拒时上传应失败")
	}
	// 云端那份 UploadId 没了（分片被生命周期规则清掉是常态）。
	f.mu.Lock()
	f.uploads = map[string]map[int][]byte{}
	f.mu.Unlock()

	mark := seqMark(f)
	if _, err := o.Upload(context.Background(), "", "gone.bin", openForUpload(t, src), int64(len(body))); err != nil {
		t.Fatalf("核对失败后应能重头传成功: %v", err)
	}
	after := strings.Join(seqAfter(f, mark), "\n")
	if !strings.Contains(after, "GET /gone-bucket/gone.bin?uploadId=upload-1") ||
		!strings.Contains(after, "POST /gone-bucket/gone.bin?uploads") ||
		!strings.Contains(after, "partNumber=1&uploadId=upload-2") {
		t.Fatalf("应先核对旧会话再重头发一次：%s", after)
	}
	if got := f.mustGet("gone.bin"); !bytes.Equal(got, body) {
		t.Fatal("重头传后内容不一致")
	}
	if left := listUploadSessionsForTest(o.acctID); len(left) != 0 {
		t.Fatalf("成功后应清掉会话，残留 %+v", left)
	}
}

// TestMultipartResumeSkipsOnlyContiguousPrefix 云端有第 1、第 3 片但缺第 2 片：
// 中间断号就没法合成完整对象，只能保留从第 1 片起的连续前缀，后面的片重传。
func TestMultipartResumeSkipsOnlyContiguousPrefix(t *testing.T) {
	shrinkParts(t, 1<<10, 1<<10)
	body := bytes.Repeat([]byte("g"), 2500)
	src := writeTempFile(t, "gap.bin", body)
	f := newFakeOS(t, "gap-bucket")
	o, _ := newTestOS(t, KindS3, "S3", f)
	f.failPartWrite(2)
	if _, err := o.Upload(context.Background(), "", "gap.bin", openForUpload(t, src), int64(len(body))); err == nil {
		t.Fatal("第 2 片被拒时上传应失败")
	}
	// 往云端同一个 UploadId 里塞入一片**内容不对**的第 3 片（本地会话并不知道它）。
	if _, err := o.uploadPart(context.Background(), "gap.bin", 3, "upload-1", bytes.Repeat([]byte("X"), 999)); err != nil {
		t.Fatal(err)
	}

	mark := seqMark(f)
	if _, err := o.Upload(context.Background(), "", "gap.bin", openForUpload(t, src), int64(len(body))); err != nil {
		t.Fatalf("续传应成功: %v", err)
	}
	want := strings.Join([]string{
		"GET /gap-bucket/gap.bin?uploadId=upload-1",              // 核对云端实况
		"PUT /gap-bucket/gap.bin?partNumber=2&uploadId=upload-1", // 断号之后一律重传
		"PUT /gap-bucket/gap.bin?partNumber=3&uploadId=upload-1", // 原本已在云端的第 3 片也要覆盖
		"POST /gap-bucket/gap.bin?uploadId=upload-1",             // 同一 UploadId 收尾
		"HEAD /gap-bucket/gap.bin",                               // statEntry
	}, "\n")
	if got := strings.Join(seqAfter(f, mark), "\n"); got != want {
		t.Fatalf("断号续传的调用序不对:\n got: %s\nwant: %s", got, want)
	}
	if got := f.mustGet("gap.bin"); !bytes.Equal(got, body) {
		t.Fatalf("续传拼出的对象内容不对（拿回了那批 999 字节垃圾）: got %d want %d", len(got), len(body))
	}
	if left := listUploadSessionsForTest(o.acctID); len(left) != 0 {
		t.Fatalf("成功后应清掉会话: %+v", left)
	}
}

// TestMultipartResumeFallsBackWhenListPartsUnsupported 服务端不实现 ListParts（MinIO
// 等兼容服务的常态）：核对不上就不能拿旧片号冒险，必须重头传。
func TestMultipartResumeFallsBackWhenListPartsUnsupported(t *testing.T) {
	shrinkParts(t, 1<<10, 1<<10)
	body := bytes.Repeat([]byte("h"), 2500)
	src := writeTempFile(t, "nolist.bin", body)
	f := newFakeOS(t, "nolist-bucket")
	o, _ := newTestOS(t, KindS3, "S3", f)
	f.failPartWrite(2)
	if _, err := o.Upload(context.Background(), "", "nolist.bin", openForUpload(t, src), int64(len(body))); err == nil {
		t.Fatal("分片被拒时上传应失败")
	}
	if left := listUploadSessionsForTest(o.acctID); len(left) != 1 {
		t.Fatalf("应先落一条会话: %+v", left)
	}
	f.disableListParts()

	mark := seqMark(f)
	if _, err := o.Upload(context.Background(), "", "nolist.bin", openForUpload(t, src), int64(len(body))); err != nil {
		t.Fatalf("核对不了也应能重头传成功: %v", err)
	}
	after := strings.Join(seqAfter(f, mark), "\n")
	if !strings.Contains(after, "GET /nolist-bucket/nolist.bin?uploadId=upload-1") ||
		!strings.Contains(after, "POST /nolist-bucket/nolist.bin?uploads") ||
		!strings.Contains(after, "partNumber=1&uploadId=upload-2") {
		t.Fatalf("应先核对再重头发一次分块上传：%s", after)
	}
	if strings.Contains(after, "partNumber=1&uploadId=upload-1") {
		t.Fatalf("核对失败后不能再往旧 UploadId 补片：%s", after)
	}
	if got := f.mustGet("nolist.bin"); !bytes.Equal(got, body) {
		t.Fatal("重头传后内容不一致")
	}
	if left := listUploadSessionsForTest(o.acctID); len(left) != 0 {
		t.Fatalf("旧会话应被丢掉，残留 %+v", left)
	}
}

// TestMultipartResumeRefusesTruncatedListParts ListParts 回 IsTruncated（已传分片多到
// 列不完）时不能猜，宁可重头传：拿半截清单去 Complete 会拼出缺分片的坏对象。
func TestMultipartResumeRefusesTruncatedListParts(t *testing.T) {
	shrinkParts(t, 1<<10, 1<<10)
	body := bytes.Repeat([]byte("i"), 2500)
	src := writeTempFile(t, "trunc.bin", body)
	f := newFakeOS(t, "trunc-bucket")
	o, _ := newTestOS(t, KindAliyunOSS, "OSS", f)
	f.failPartWrite(3)
	if _, err := o.Upload(context.Background(), "", "trunc.bin", openForUpload(t, src), int64(len(body))); err == nil {
		t.Fatal("第 3 片被拒时上传应失败")
	}
	f.truncateListParts()

	mark := seqMark(f)
	if _, err := o.Upload(context.Background(), "", "trunc.bin", openForUpload(t, src), int64(len(body))); err != nil {
		t.Fatalf("拒接截断清单后应重头传成功: %v", err)
	}
	after := strings.Join(seqAfter(f, mark), "\n")
	if !strings.Contains(after, "GET /trunc-bucket/trunc.bin?uploadId=upload-1") ||
		!strings.Contains(after, "POST /trunc-bucket/trunc.bin?uploads") {
		t.Fatalf("应先核对再重头发一次：%s", after)
	}
	if strings.Contains(after, "partNumber=2&uploadId=upload-1") {
		t.Fatalf("清单被截断时不能再拿旧 UploadId 接着补：%s", after)
	}
	if got := f.mustGet("trunc.bin"); !bytes.Equal(got, body) {
		t.Fatal("重头传后内容不一致")
	}
}

func TestMultipartSessionExpiresAndAborts(t *testing.T) {
	shrinkParts(t, 1<<10, 1<<10)
	body := bytes.Repeat([]byte("d"), 2500)
	src := writeTempFile(t, "old.bin", body)
	f := newFakeOS(t, "ttl-bucket")
	o, _ := newTestOS(t, KindS3, "S3", f)
	f.failPartWrite(2)
	if _, err := o.Upload(context.Background(), "", "old.bin", openForUpload(t, src), int64(len(body))); err == nil {
		t.Fatal("分片被拒时上传应失败")
	}
	// 把会话时刻推到 TTL 之外（模拟「崩了之后再也没重试过」）。putUploadSession 总会
	// 把 UpdatedAt 刷成当前时间，所以只能绕过它直接改文件。
	expireSession(t, o.acctID, mustOneSession(t, o.acctID))

	mark := seqMark(f)
	if _, err := o.Upload(context.Background(), "", "old.bin", openForUpload(t, src), int64(len(body))); err != nil {
		t.Fatalf("过期会话应被丢弃并正常完成本次上传: %v", err)
	}
	after := strings.Join(seqAfter(f, mark), "\n")
	if !strings.Contains(after, "DELETE /ttl-bucket/old.bin?uploadId=upload-1") {
		t.Fatalf("过期会话应尽力 Abort 云端残留：%s", after)
	}
	if !strings.Contains(after, "POST /ttl-bucket/old.bin?uploads") {
		t.Fatalf("过期后应重新发起分块上传：%s", after)
	}
	if left := listUploadSessionsForTest(o.acctID); len(left) != 0 {
		t.Fatalf("本次结束后不应残留会话: %+v", left)
	}
}

// mustOneSession 取某账号当前唯一一条会话的 UploadId（条数不为 1 直接失败）。
func mustOneSession(t *testing.T, acctID string) string {
	t.Helper()
	sess := listUploadSessionsForTest(acctID)
	if len(sess) != 1 {
		t.Fatalf("应恰好落一条续传会话，实际 %d 条: %+v", len(sess), sess)
	}
	return sess[0].UploadID
}

// expireSession 把 uploads.json 里（账号 + UploadId）那一条的 UpdatedAt 改到 TTL 之前。
// 不能拿文本位置乱找：会话表是全账号共用的，不同假后端的 UploadId 都从 upload-1 起编，
// 按字符串找会改到别人的记录；也不能走 putUploadSession（它总会刷回当前时刻）。
func expireSession(t *testing.T, acctID, uploadID string) {
	t.Helper()
	p, err := uploadStorePath()
	if err != nil {
		t.Fatal(err)
	}
	bs, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var all []osUploadSession
	if err := json.Unmarshal(bs, &all); err != nil {
		t.Fatalf("会话文件不是合法 JSON: %v\n%s", err, bs)
	}
	old := time.Now().Add(-osUploadSessionTTL - time.Hour).UnixMilli()
	hit := 0
	for i := range all {
		if all[i].AcctID == acctID && all[i].UploadID == uploadID {
			all[i].UpdatedAt = old
			hit++
		}
	}
	if hit != 1 {
		t.Fatalf("会话文件里找不到唯一匹配的 %s/%s（命中 %d 次）: %s", acctID, uploadID, hit, bs)
	}
	out, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestUploadSessionFileCarriesNoSecrets(t *testing.T) {
	shrinkParts(t, 1<<10, 1<<10)
	src := writeTempFile(t, "sec.bin", bytes.Repeat([]byte("e"), 2500))
	f := newFakeOS(t, "sec-bucket")
	o, _ := newTestOS(t, KindTencentCOS, "COS", f)
	f.failPartWrite(3)
	if _, err := o.Upload(context.Background(), "", "sec.bin", openForUpload(t, src), 2500); err == nil {
		t.Fatal("分片被拒时上传应失败")
	}
	p, err := uploadStorePath()
	if err != nil {
		t.Fatal(err)
	}
	bs, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	text := string(bs)
	for _, leak := range []string{fakeSecret, fakeAkID, "akSecret", "access"} {
		if strings.Contains(text, leak) {
			t.Fatalf("会话文件里出现了敏感字段 %q: %s", leak, text)
		}
	}
	if !strings.Contains(text, "upload-1") || !strings.Contains(text, filepath.Base(src)) {
		t.Fatalf("会话应带着续传坐标: %s", text)
	}
}

func TestNonFileUploadStillAborts(t *testing.T) {
	// 源不是本地文件（内存流、跨盘转存）时无从校验续传内容，失败仍必须立刻 Abort。
	shrinkParts(t, 1<<10, 1<<10)
	f := newFakeOS(t, "nofile-bucket")
	o, _ := newTestOS(t, KindS3, "S3", f)
	f.failPartWrite(2)
	if _, err := o.Upload(context.Background(), "", "m.bin", bytesReader(bytes.Repeat([]byte("f"), 2500)), 2500); err == nil {
		t.Fatal("分片被拒时上传应失败")
	}
	assertSeqHas(t, f, "DELETE /nofile-bucket/m.bin?uploadId=upload-1")
	if left := listUploadSessionsForTest(o.acctID); len(left) != 0 {
		t.Fatalf("非文件源不应落会话: %+v", left)
	}
}
