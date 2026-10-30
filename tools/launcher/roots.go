package launcher

// ============================================================
// 检索目录清单（跨平台部分）
//
// 平台差异只留在"这些目录叫什么路径"这一层（roots_windows.go / roots_other.go），
// 去重与折叠这类通用逻辑放在这里，避免两边各抄一份再各改各的。
// ============================================================

// compactDirs 去掉空串与重复，保持传入顺序（顺序影响同名应用的取舍，用户目录要优先）。
func compactDirs(in ...string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, p := range in {
		if p == "" {
			continue
		}
		k := lower(p)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, p)
	}
	return out
}

// lower ASCII 小写。路径比较只关心英文字母，用它换掉 strings.ToLower
// 能在"每条检索都要比上百次"的路径上省掉一次 Unicode 表查询。
// 非 ASCII（中文目录名）原样返回，此时大小写概念本就不存在。
func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
