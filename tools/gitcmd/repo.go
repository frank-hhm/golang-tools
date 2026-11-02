package gitcmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"Tools/appdirs"
)

// Discover 从 path 向上定位 git 仓库，返回工作树/目录信息。
// 不在任何仓库内返回 ErrNotRepo；本机没 git 返回 ErrGitNotFound。
func Discover(ctx context.Context, path string) (Repo, error) {
	if strings.TrimSpace(path) == "" {
		cwd, _ := os.Getwd()
		path = cwd
	}
	inside, err := Run(ctx, path, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return Repo{}, ErrNotRepo
	}
	repo := Repo{Path: filepath.Clean(path), InsideWorkTree: strings.TrimSpace(inside.Stdout) == "true"}
	// 裸仓：inside-work-tree 为 false，但 rev-parse --git-dir 仍成功
	bareOut, berr := Run(ctx, path, "rev-parse", "--is-bare-repository")
	if berr != nil {
		return Repo{}, ErrNotRepo
	}
	repo.Bare = strings.TrimSpace(bareOut.Stdout) == "true"
	if tl, e := Run(ctx, path, "rev-parse", "--show-toplevel"); e == nil {
		repo.Toplevel = trimTail(tl.Stdout)
	}
	if gd, e := Run(ctx, path, "rev-parse", "--absolute-git-dir"); e == nil {
		repo.GitDir = trimTail(gd.Stdout)
	}
	// 分支：symbolic-ref 在游离 HEAD 下非零退出，视为 detached
	if br, e := Run(ctx, path, "symbolic-ref", "--short", "-q", "HEAD"); e == nil {
		repo.Branch = trimTail(br.Stdout)
	} else {
		repo.Detached = true
	}
	return repo, nil
}

// RepoRef 登记表里的一条仓库记录。
type RepoRef struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	AddedAt int64  `json:"addedAt"`
}

// reposFile 仓库登记表路径：<数据目录>/git/repos.json。
func reposFile() (string, error) {
	dir, err := appdirs.Data("git")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "repos.json"), nil
}

// LoadRepos 读登记表；文件不存在返回空切片（不算错误）。
func LoadRepos() ([]RepoRef, error) {
	f, err := reposFile()
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(f)
	if err != nil {
		if os.IsNotExist(err) {
			return []RepoRef{}, nil
		}
		return nil, err
	}
	var list []RepoRef
	if len(strings.TrimSpace(string(body))) == 0 {
		return []RepoRef{}, nil
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, err
	}
	if list == nil {
		list = []RepoRef{}
	}
	return list, nil
}

func saveRepos(list []RepoRef) error {
	f, err := reposFile()
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := f + ".part"
	if err := os.WriteFile(tmp, append(body, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f)
}

// AddRepo 登记一个仓库（按路径去重）。返回最新登记表。
func AddRepo(ctx context.Context, path string) ([]RepoRef, error) {
	repo, err := Discover(ctx, path)
	if err != nil {
		return nil, err
	}
	real := repo.Toplevel
	if real == "" {
		real = repo.Path
	}
	list, err := LoadRepos()
	if err != nil {
		return nil, err
	}
	norm := normPath(real)
	for _, r := range list {
		if normPath(r.Path) == norm {
			return list, nil // 已登记
		}
	}
	list = append(list, RepoRef{Name: filepath.Base(norm), Path: norm, AddedAt: time.Now().Unix()})
	if err := saveRepos(list); err != nil {
		return nil, err
	}
	return list, nil
}

// normPath 路径归一：Clean + 解符号链接/8.3 短名（Windows 临时目录常以
// ADMINI~1 形式传入，而 git 报的是长路径，不归一会导致去重/移除对不上）。
func normPath(p string) string {
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = filepath.Clean(r)
	}
	return p
}

// RemoveRepo 从登记表移除一个路径。返回最新列表。
func RemoveRepo(path string) ([]RepoRef, error) {
	list, err := LoadRepos()
	if err != nil {
		return nil, err
	}
	norm := normPath(path)
	out := list[:0]
	for _, r := range list {
		if normPath(r.Path) != norm {
			out = append(out, r)
		}
	}
	if err := saveRepos(out); err != nil {
		return nil, err
	}
	return out, nil
}
