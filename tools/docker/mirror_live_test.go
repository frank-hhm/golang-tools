//go:build windows

package docker

import (
	"os"
	"testing"
)

// TestProbeMirrorLive 真的打一次公网加速站：默认跳过，需要显式开开关。
// 三段式探测（连通 / 清单 / 层数据）只有对着真站才验得出路径拼得对不对，
// 但 CI 与离线机器上不该有网络请求，所以：
//
//	GO_TOOLS_DOCKER_LIVE=1 go test ./tools/docker/ -run TestProbeMirrorLive -v
//
// 输出里 grade=empty 就是这个站「清单给得出、层数据不发字节」，
// 也就是拉取卡在 Downloading 的那种。
func TestProbeMirrorLive(t *testing.T) {
	if os.Getenv("GO_TOOLS_DOCKER_LIVE") != "1" {
		t.Skip("需要 GO_TOOLS_DOCKER_LIVE=1 才打公网")
	}
	for _, m := range []string{"https://docker.m.daocloud.io", "https://docker.1ms.run", "https://invalid.mirror.example"} {
		p := ProbeMirror(m, "")
		t.Logf("%s → grade=%s stage=%s %s | 连通 %dms 清单 %dms 首字节 %dms 收 %d/%d 字节 %.1f Mbps 层 %s\n    提示: %s",
			p.Url, p.Grade, p.Stage, p.Label, p.ConnectMs, p.ManifestMs, p.TtfbMs,
			p.Bytes, p.BlobSize, p.Mbps, p.Layer, p.Hint)
		switch p.Grade {
		case "ok", "slow", "empty", "fail":
		default:
			t.Errorf("%s 结论不在四级里: %q", m, p.Grade)
		}
	}
}

// TestProbeMirrorLiveSampleFollowsRef 用界面同款写法（带加速站主机名的完整引用）取样，
// 确认主机名会被摘掉、不会拼成 /v2/docker.1ms.run/library/redis/… 这种 404 路径
func TestProbeMirrorLiveSampleFollowsRef(t *testing.T) {
	if os.Getenv("GO_TOOLS_DOCKER_LIVE") != "1" {
		t.Skip("需要 GO_TOOLS_DOCKER_LIVE=1 才打公网")
	}
	p := ProbeMirror("https://docker.m.daocloud.io", "docker.m.daocloud.io/library/alpine:latest")
	t.Logf("grade=%s stage=%s %s | 取样名 %s 层 %s", p.Grade, p.Stage, p.Label, p.Sample, p.Layer)
	if p.Stage == "manifest" && p.Grade == "fail" {
		t.Errorf("取样名里的主机名没被摘掉：清单路径拼错了（%s）", p.Hint)
	}
}
