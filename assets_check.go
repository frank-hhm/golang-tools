package main

import (
	"embed"
	"io/fs"
	"log"
)

// checkAssets 确认前端产物真实存在。
//
// 仓库里保留了 frontend/dist/.gitkeep，所以 //go:embed 在编译期不会失败；
// 但 dist 里到底有没有 index.html 只有运行时才知道。少了它在桌面版是白屏、
// 在 server 版是 404，两个入口都提前拦下来，给一句能照做的提示。
func checkAssets(assets embed.FS) {
	if _, err := fs.Stat(assets, "frontend/dist/index.html"); err != nil {
		log.Fatal("未找到前端产物 frontend/dist/index.html：请在 frontend 目录执行 npm install && npm run build，" +
			"或把已构建好的 dist 目录拷到 frontend/dist 后重新编译")
	}
}
