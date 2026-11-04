package gitcmd

import (
	"context"
	"strings"
)

// ---------- P3：标签（git tag） ----------

// tagFmt for-each-ref 的字段模板：短名/对象类型/对象短哈希/剥离提交短哈希/创建时间/标题，TAB 分隔。
// subject 放最后并以 SplitN 限段，避免个别标题里混入 TAB 把字段切乱。
const tagFmt = "%(refname:short)%09%(objecttype)%09%(objectname:short)%09%(*objectname:short)%09%(creatordate:iso)%09%(subject)"

// TagList 列出所有标签。附注标签的 Commit 取剥离后的提交（%(*objectname)），
// 轻量标签 %(*objectname) 为空，退回 %objectname。
func TagList(ctx context.Context, repoDir string) ([]Tag, error) {
	out, err := Run(ctx, repoDir, "for-each-ref", "--format="+tagFmt, "refs/tags")
	if err != nil {
		return nil, err
	}
	list := []Tag{}
	for _, line := range strings.Split(out.Stdout, "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, "\t", 6)
		if len(f) < 6 || f[0] == "" {
			continue
		}
		t := Tag{Name: f[0], Annotated: f[1] == "tag", Date: f[4], Subject: f[5]}
		if t.Annotated && f[3] != "" {
			t.Commit = f[3] // 附注标签：用剥离到的提交
		} else {
			t.Commit = f[2] // 轻量标签：对象即提交
		}
		list = append(list, t)
	}
	return list, nil
}

// TagCreate 建标签。message 非空 → 附注标签（-a -m）；空 → 轻量标签。target 为
// 起点（提交/ref，空=HEAD）。
//
// 关于「签名」：本包一律不碰私钥/passphrase（凭据交系统，见包注释），而一旦仓库配了
// tag.gpgSign=true，附注标签会去调 gpg 并弹 pinentry 索要口令——在无头子进程里会直接挂死。
// 故这里强制注入 `-c tag.gpgSign=false`，即本工具**拒绝产出签名附注标签**，只建未签名的
// 附注/轻量标签；需要签名标签请回到命令行用你自己的密钥操作。
func TagCreate(ctx context.Context, repoDir, name, message, target string) error {
	args := []string{"-c", "tag.gpgSign=false", "tag"}
	if message != "" {
		args = append(args, "-a", "-m", message)
	}
	args = append(args, name)
	if target != "" {
		args = append(args, target)
	}
	_, err := Run(ctx, repoDir, args...)
	return err
}

// TagDelete 删本地标签（破坏操作，前端须二次确认）。仅删本地引用，不动远端。
func TagDelete(ctx context.Context, repoDir, name string) error {
	_, err := Run(ctx, repoDir, "tag", "-d", name)
	return err
}
