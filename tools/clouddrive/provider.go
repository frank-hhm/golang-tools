package clouddrive

// 云盘 provider 抽象层（计划-云盘管理工具 PR1）。
//
// 一个 Provider = 某一家的文件操作能力（浏览/增删改/下载流/上传流/配额/分享）。
// registry 按账号 Kind 构造具体适配器；store 存账号与非敏感字段，敏感 token 走
// tools/secret 加密落盘。docspace 的 docref「cloud」分支经本包把远程文件读进来。
//
// 全模块跨平台零 cgo、无 build tag：纯 net/http + encoding/json，Linux 面板构建
// 同样存在（AGENTS 硬约束）。国内两家（阿里 refresh_token / 百度 BDUSS）属现网
// 逆向、脆弱，本期（PR1）只做 Google 官方 Drive v3，其余见交付说明。

import (
	"context"
	"io"
)

// Entry 一个云盘条目。ID 是各家的原生标识（Google 是 fileId），Path 是人可读的
// 展示路径（面包屑用），不作为跨家统一的寻址键——寻址一律用 ID。
type Entry struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	IsDir    bool   `json:"isDir"`
	Size     int64  `json:"size"`
	MTime    int64  `json:"mtime"` // UnixMilli，0 = 未知
	MIME     string `json:"mime"`
	ThumbURL string `json:"thumbUrl,omitempty"`
}

// Quota 容量配额（字节）。Total 为 0 表示该家不暴露总量。
type Quota struct {
	Used  int64 `json:"used"`
	Total int64 `json:"total"`
}

// Provider 单个已授权账号的文件操作能力。所有方法带 context，便于取消与超时。
// dirID 为空表示根目录（各适配器自行映射成自己的根标识）。
type Provider interface {
	Name() string
	Test(ctx context.Context) error
	List(ctx context.Context, dirID string) ([]Entry, error)
	Mkdir(ctx context.Context, parentID, name string) (Entry, error)
	Rename(ctx context.Context, id, newName string) error
	Move(ctx context.Context, id, dstParentID string) error
	Delete(ctx context.Context, id string) error
	ShareLink(ctx context.Context, id string) (string, error)
	// Open 返回只读下载流；调用方负责 Close。size 未知时为 -1。
	Open(ctx context.Context, id string) (rc io.ReadCloser, size int64, err error)
	// Upload 把 r 上传到 parentID 下并命名 name；size 未知传 -1，适配器按需缓冲。
	Upload(ctx context.Context, parentID, name string, r io.Reader, size int64) (Entry, error)
	Quota(ctx context.Context) (Quota, error)
}
