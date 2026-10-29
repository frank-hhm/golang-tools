package app

// 账号保存后的断连策略：只有「权限/凭据变化」才断开在线会话，
// 改备注这类不影响鉴权的字段不应该把用户踢下线。

import (
	"testing"

	"Tools/tools/ftp"
)

func TestStaleUsernames(t *testing.T) {
	base := ftp.Account{ID: "a1", Username: "alice", RootDir: "alice", Enabled: true}
	before := []ftp.Account{base}

	cases := []struct {
		name   string
		before []ftp.Account
		after  []ftp.Account
		in     ftp.Account
		want   []string
	}{
		{
			name:   "新建账号没有会话",
			before: nil,
			after:  []ftp.Account{{ID: "a2", Username: "bob", RootDir: "bob", Enabled: true}},
			in:     ftp.Account{Username: "bob", Password: "123456", Enabled: true},
			want:   nil,
		},
		{
			name:   "只改备注不断开",
			before: before,
			after:  []ftp.Account{{ID: "a1", Username: "alice", RootDir: "alice", Enabled: true, Note: "同事"}},
			in:     ftp.Account{ID: "a1", Username: "alice", RootDir: "alice", Enabled: true, Note: "同事"},
			want:   nil,
		},
		{
			name:   "改只读要断开",
			before: before,
			after:  []ftp.Account{{ID: "a1", Username: "alice", RootDir: "alice", ReadOnly: true, Enabled: true}},
			in:     ftp.Account{ID: "a1", Username: "alice", RootDir: "alice", ReadOnly: true, Enabled: true},
			want:   []string{"alice"},
		},
		{
			name:   "放宽为读写不断开",
			before: []ftp.Account{{ID: "a1", Username: "alice", RootDir: "alice", ReadOnly: true, Enabled: true}},
			after:  []ftp.Account{{ID: "a1", Username: "alice", RootDir: "alice", Enabled: true}},
			in:     ftp.Account{ID: "a1", Username: "alice", RootDir: "alice", Enabled: true},
			want:   nil,
		},
		{
			name:   "禁用要断开",
			before: before,
			after:  []ftp.Account{{ID: "a1", Username: "alice", RootDir: "alice"}},
			in:     ftp.Account{ID: "a1", Username: "alice", RootDir: "alice"},
			want:   []string{"alice"},
		},
		{
			name:   "改根目录要断开",
			before: before,
			after:  []ftp.Account{{ID: "a1", Username: "alice", RootDir: "shared/alice", Enabled: true}},
			in:     ftp.Account{ID: "a1", Username: "alice", RootDir: "shared/alice", Enabled: true},
			want:   []string{"alice"},
		},
		{
			name:   "改密码要断开",
			before: before,
			after:  before,
			in:     ftp.Account{ID: "a1", Username: "alice", RootDir: "alice", Password: "newpass", Enabled: true},
			want:   []string{"alice"},
		},
		{
			name:   "改名后新旧账号都要断",
			before: before,
			after:  []ftp.Account{{ID: "a1", Username: "bob", RootDir: "alice", Enabled: true}},
			in:     ftp.Account{ID: "a1", Username: "bob", RootDir: "alice", Enabled: true},
			want:   []string{"bob", "alice"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := staleUsernames(tc.before, tc.after, tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("断开名单 = %v，期望 %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("断开名单 = %v，期望 %v", got, tc.want)
				}
			}
		})
	}
}
