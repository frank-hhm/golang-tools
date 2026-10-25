package sysx

import (
	"os"
	"path/filepath"
)

// writeFileAtomic 临时文件 + rename 原子写。
//
// 用途：systemd unit、凭据文件、安装标记——这些文件"读一半"的后果都很糟，
// 所以统一走"先写全、再原子替换"。
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// flattenSingleRoot 把"归档里只有一层顶层目录"的结构提上来。
//
// 官方发行包几乎都是这个形状：nginx-1.27.0/、node-v22.11.0-win-x64/、php-8.3.36/……
// 提上来之后 <plugins>/<service>/<version>/ 直接就是内容根，各工具找二进制不用再猜一层。
// 顶层不止一项时原样保留（如 redis zip 直接把 exe 放在根）。
func flattenSingleRoot(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		return nil
	}
	inner := filepath.Join(dir, entries[0].Name())
	subs, err := os.ReadDir(inner)
	if err != nil {
		return err
	}
	// 先全体检查冲突再动手：半途 return 会留下"一部分提上来、一部分还在子目录"的残局
	for _, e := range subs {
		if _, err := os.Stat(filepath.Join(dir, e.Name())); err == nil {
			// 同名冲突：保留原结构，交给调用方判断，不在这里静默覆盖
			return nil
		}
	}
	for _, e := range subs {
		if err := os.Rename(filepath.Join(inner, e.Name()), filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	_ = os.Remove(inner) // 空目录才删得掉；还有残留就留着，不影响使用
	return nil
}
