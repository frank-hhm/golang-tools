package gitcmd

import (
	"context"
	"strconv"
	"strings"
)

// logFieldSep 解析用的真实分隔符：NUL——subject/refs 里可能出现任意可打印字符，NUL 不会。
const logFieldSep = "\x00"

// logFmt --pretty=format 模板：每行一条记录，字段顺序 hash/short/作者/邮箱/时间/refs/subject/parents。
// 注意：必须用 git 的 %x00 转义而不是真 NUL 字符——命令行参数里带 NUL 会被
// Windows/CreateProcess 直接拒绝（fork/exec: invalid argument）。
const logFmt = "%H%x00%h%x00%an%x00%ae%x00%at%x00%d%x00%s%x00%P%x00"

// Log 取最近 limit 条提交（limit<=0 取默认 200），带父信息与布局列号 lane。
// 前端 canvas 只按 lane/行号画 DAG，不再自己算。
func Log(ctx context.Context, repoDir string, limit int) ([]Commit, error) {
	if limit <= 0 {
		limit = 200
	}
	args := []string{"log", "--decorate=short", "--pretty=format:" + logFmt, "-n", strconv.Itoa(limit)}
	out, err := Run(ctx, repoDir, args...)
	if err != nil {
		return nil, err
	}
	return parseLog(out.Stdout), nil
}

// parseLog 解析 logFmt 输出并做 lane 布局。
func parseLog(text string) []Commit {
	list := []Commit{}
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, logFieldSep)
		if len(f) < 8 {
			continue
		}
		c := Commit{
			Hash:        strings.TrimSpace(f[0]),
			Short:       strings.TrimSpace(f[1]),
			Author:      f[2],
			AuthorEmail: f[3],
			Refs:        cleanRefs(f[5]),
			Subject:     f[6],
		}
		c.Time, _ = strconv.ParseInt(strings.TrimSpace(f[4]), 10, 64)
		if ps := strings.Fields(f[7]); len(ps) > 0 {
			c.Parents = ps
		} else {
			c.Parents = []string{}
		}
		list = append(list, c)
	}
	assignLanes(list)
	return list
}

// cleanRefs 去掉 decorate 包裹："(HEAD -> main, tag: v1, origin/main)" → "HEAD -> main, tag: v1, origin/main"。
func cleanRefs(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "(")
	s = strings.TrimSuffix(s, ")")
	return s
}

// assignLanes 单遍布局：自上而下（新→旧）处理提交，维护若干"轨道"，每条轨道记住
// 它正在等待的下一个 commit 哈希（即沿某条边回溯的前沿）。
//   - 当前 commit 命中某轨道 → 占该列；首父继续沿用该列，其余父各开新列；
//   - 未命中（窗口截断/合并边已断）→ 另开一列；
//   - 轨道等到根提交（无父）后释放，列号可复用。
//
// 这是 TortoiseGit/GitKraken 简化版：不处理跨窗口悬空边的美化，够画可读 DAG。
func assignLanes(commits []Commit) {
	// lanes[i] = 该列正在等待的完整哈希集合（多条边可汇入同列，按先进先出占列）
	var waiting []map[string]bool // waiting[i] 为 nil 表示列空闲
	occupy := func(hash string) int {
		for i, w := range waiting {
			if w != nil && w[hash] {
				delete(w, hash)
				if len(w) == 0 {
					waiting[i] = nil
				}
				return i
			}
		}
		return -1
	}
	track := func(col int, hash string) {
		if waiting[col] == nil {
			waiting[col] = map[string]bool{}
		}
		waiting[col][hash] = true
	}
	newCol := func() int {
		for i, w := range waiting {
			if w == nil {
				return i
			}
		}
		waiting = append(waiting, nil)
		return len(waiting) - 1
	}

	for i := range commits {
		c := &commits[i]
		lane := occupy(c.Hash)
		if lane < 0 {
			lane = newCol()
		}
		c.Lane = lane
		if len(c.Parents) == 0 {
			continue // 根提交：该列到此为止，已释放
		}
		track(lane, c.Parents[0]) // 首父继承本列
		for _, p := range c.Parents[1:] {
			if tracked(p, waiting) { // 已有别的列在等 p（菱形合并汇合）则不重复开列
				continue
			}
			track(newCol(), p)
		}
	}
}

// tracked 检查是否已有非空轨道在等 hash（只查询，不消费）。
func tracked(hash string, waiting []map[string]bool) bool {
	for _, w := range waiting {
		if w != nil && w[hash] {
			return true
		}
	}
	return false
}
