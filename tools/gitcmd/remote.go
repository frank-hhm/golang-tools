package gitcmd

import (
	"context"
	"regexp"
	"strings"
)

// ---------- P3：远端（git remote） ----------

// remoteVRe 匹配 `git remote -v` 的一行：形如 "origin<TAB>https://x (fetch)"。
// URL 用懒惰匹配吃到结尾的 " (fetch|push)"，名字与 URL 之间是 TAB 或空格。
var remoteVRe = regexp.MustCompile(`^(\S+)\s+(\S.*?)\s+\((fetch|push)\)$`)

// RemoteList 列出所有远端及其抓取/推送地址。以 `git remote` 拿全集（无 URL 的
// 奇怪远端也在列），再用 `git remote -v` 的地址填充。
func RemoteList(ctx context.Context, repoDir string) ([]Remote, error) {
	names, err := Run(ctx, repoDir, "remote")
	if err != nil {
		return nil, err
	}
	byName := map[string]*Remote{}
	order := []string{}
	for _, n := range strings.Split(names.Stdout, "\n") {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if _, dup := byName[n]; !dup {
			byName[n] = &Remote{Name: n}
			order = append(order, n)
		}
	}

	v, err := Run(ctx, repoDir, "remote", "-v")
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(v.Stdout, "\n") {
		m := remoteVRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		r := byName[m[1]]
		if r == nil {
			continue
		}
		if m[3] == "fetch" {
			r.FetchURL = m[2]
		} else {
			r.PushURL = m[2]
		}
	}

	list := []Remote{}
	for _, n := range order {
		list = append(list, *byName[n])
	}
	return list, nil
}

// RemoteAdd 新增远端。
func RemoteAdd(ctx context.Context, repoDir, name, url string) error {
	_, err := Run(ctx, repoDir, "remote", "add", name, url)
	return err
}

// RemoteRemove 删除远端（破坏操作，前端须二次确认）。
func RemoteRemove(ctx context.Context, repoDir, name string) error {
	_, err := Run(ctx, repoDir, "remote", "remove", name)
	return err
}

// RemoteSetURL 改远端地址；push=true 只改推送地址（--push），否则改抓取地址。
func RemoteSetURL(ctx context.Context, repoDir, name, url string, push bool) error {
	args := []string{"remote", "set-url"}
	if push {
		args = append(args, "--push")
	}
	args = append(args, name, url)
	_, err := Run(ctx, repoDir, args...)
	return err
}
