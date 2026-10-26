package php

// ============================================================
// php.ini 配置管理：文本读写 + 扩展开关 + disable_functions + 常用项
//
// 设计要点：
//   - 逐行文本编辑（不重排、不丢注释），只改动目标行；
//   - 首次启动从版本目录 php.ini-development 复制一份到实例目录，之后完全由用户掌控；
//   - 扩展启用 = 取消 `;extension=xxx` 注释或追加 `extension=xxx`；
//     扩展禁用 = 注释掉未带引号的 `extension=xxx` 行；
//   - 每次写回前用 `php -c <ini> -r "echo 1;"` 做语法校验，失败则回滚。
// ============================================================

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

/* ============================ ini 行级工具 ============================ */

var iniKeyRe = regexp.MustCompile(`^\s*[;#]*\s*([A-Za-z0-9_.\-|\[\]]+?)\s*(:|=)`)

// iniKeyOfLine 取一行的指令名（含被注释行）；非指令行返回空串
func iniKeyOfLine(line string) string {
	m := iniKeyRe.FindStringSubmatch(line)
	if m == nil {
		return ""
	}
	if strings.HasPrefix(strings.TrimSpace(line), "[") {
		return ""
	}
	return m[1]
}

func iniIsComment(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, ";") || strings.HasPrefix(t, "#")
}

// iniGet 读取指令值（取最后一条生效的未注释行）
func iniGet(text, key string) string {
	lines := strings.Split(text, "\n")
	val := ""
	found := false
	for _, l := range lines {
		if iniIsComment(l) || iniKeyOfLine(l) != key {
			continue
		}
		if i := strings.IndexAny(l, ":="); i >= 0 {
			val = strings.TrimSpace(strings.SplitN(l[i+1:], ";", 2)[0])
			found = true
		}
	}
	if !found {
		return ""
	}
	return val
}

// iniSet 设置指令值：优先改写已存在的未注释行，其次取消注释，最后追加
func iniSet(lines []string, key, value string) []string {
	target := key + "=" + value
	for i, l := range lines {
		if iniIsComment(l) || iniKeyOfLine(l) != key {
			continue
		}
		lines[i] = target
		return lines
	}
	for i, l := range lines {
		if iniKeyOfLine(l) == key {
			lines[i] = target
			return lines
		}
	}
	return append(lines, target)
}

// iniUnset 注释掉该指令（保留原文，便于恢复）
func iniUnset(lines []string, key string) []string {
	for i, l := range lines {
		if iniKeyOfLine(l) == key && !iniIsComment(l) {
			lines[i] = ";" + l
		}
	}
	return lines
}

// iniJoin 把多行 disable_functions 写法合并为一行（PHP 支持续行，编辑时统一单行更好读）
func iniJoin(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	acc := ""
	has := false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, ";") || strings.HasPrefix(t, "#") {
			out = append(out, l)
			continue
		}
		if iniKeyOfLine(l) == "disable_functions" {
			if i := strings.IndexAny(l, "=:"); i >= 0 {
				acc += "," + strings.TrimSpace(l[i+1:])
				has = true
			}
			continue
		}
		out = append(out, l)
	}
	if !has {
		return text
	}
	val := strings.Trim(strings.Trim(acc, " '"), ",")
	val = strings.Trim(val, "'")
	val = strings.Trim(val, ",")
	return strings.Join(out, "\n") + "\ndisable_functions = " + val
}

/* ============================ php.ini 文件 ============================ */

// iniPathOf 实例 php.ini 路径：本地模式与 php.exe 同目录（PHP 默认加载位置，便于手工维护与迁移）；
// Docker 模式取实例目录下的覆盖文件（容器内挂到 conf.d，只做追加覆盖）
func iniPathOf(id string) (string, error) {
	cfg, err := findInstance(id)
	if err != nil {
		return "", err
	}
	if isDockerMode(cfg) {
		return dockerIniPath(cfg)
	}
	exe := phpExe(cfg.Version, cfg.NTS)
	if exe == "" {
		exe = phpExe(cfg.Version, !cfg.NTS)
	}
	if exe == "" {
		return "", fmt.Errorf("版本 %s 不可用，请先安装", cfg.Version)
	}
	return filepath.Join(filepath.Dir(exe), "php.ini"), nil
}

// ensureIni 实例 php.ini 不存在时生成：优先复制版本目录 php.ini-development，
// 其次 php.ini-production，都没有则用最小模板；随后校正 extension_dir。
func ensureIni(cfg *InstanceConfig, phpDir, iniPath string) error {
	if st, err := os.Stat(iniPath); err == nil && !st.IsDir() && st.Size() > 0 {
		// 已存在：仅校正 extension_dir（旧版可能写入未加引号的路径）
		text, err := os.ReadFile(iniPath)
		if err != nil {
			return err
		}
		fixed, changed := iniSetExtDir(string(text), phpDir)
		if changed && rewriteExtDir {
			return os.WriteFile(iniPath, []byte(fixed), 0o644)
		}
		return nil
	}
	written := false
	for _, src := range iniTemplateCandidates(phpDir) {
		data, err := os.ReadFile(src)
		if err != nil || len(data) == 0 {
			continue
		}
		if err := os.WriteFile(iniPath, data, 0o644); err != nil {
			return err
		}
		written = true
		break
	}
	if !written {
		if err := os.WriteFile(iniPath, []byte(minimalIni()), 0o644); err != nil {
			return err
		}
	}
	text, err := os.ReadFile(iniPath)
	if err != nil {
		return err
	}
	fixed, changed := iniSetExtDir(string(text), phpDir)
	if changed && rewriteExtDir {
		return os.WriteFile(iniPath, []byte(fixed), 0o644)
	}
	return nil
}

func minimalIni() string {
	return `; go-tools 生成的最小 php.ini（官方 php.ini-development 缺失时的兜底）
[PHP]
engine = On
short_open_tag = Off
precision = 14
output_buffering = 4096
zlib.output_compression = Off
implicit_flush = Off
serialize_precision = -1
disable_functions =
disable_classes =
zend.enable_gc = On
expose_php = On
error_reporting = E_ALL
display_errors = On
display_startup_errors = On
log_errors = On
report_memleaks = On
cgi.fix_pathinfo = 1
realpath_cache_size = 4096k
realpath_cache_ttl = 120
allow_url_fopen = On
allow_url_include = Off
default_socket_timeout = 60
memory_limit = 256M
max_execution_time = 120
upload_max_filesize = 100M
post_max_size = 100M
date.timezone = Asia/Shanghai

[Date]

[filter]

[iconv]

[intl]

[sqlite3]

[Pcre]

[Pdo]

[Pdo_mysql]

[Phar]

[mail function]

[ODBC]

[PDO ODBC]

[PostgreSQL]

[MySQLi]

[PDOMySQL]

[bcmath]

[blenc]

[browscap]

[com]

[ctype]

[curl]

[dom]

[fileinfo]

[ftp]

[gd]

[gmp]

[hash]

[imap]

[json]

[ldap]

[mbstring]

[mysql]

[mysqli]

[OCI8]

[openssl]

[pcre]

[PDO]

[recode]

[session]

[SimpleXML]

[soap]

[sockets]

[spl]

[sysvshm]

[Tidy]

[tokenizer]

[Zend OPcache]

[zlib]

; Windows 动态扩展请在「扩展管理」中开关，go-tools 会在下方追加 extension= 行
; ---- go-tools extensions ----
`
}

// iniSetExtDir 把 extension_dir 设为版本目录的绝对 ext/ 路径（值统一加引号，避免路径含 ~ 或空格时解析失败）
func iniSetExtDir(text, phpDir string) (string, bool) {
	dir := filepath.ToSlash(filepath.Join(phpDir, "ext"))
	raw := strings.TrimSpace(iniGet(text, "extension_dir"))
	// 仅当「已加引号」且路径正确时才跳过，否则重写（历史数据可能写入了未加引号的短路径）
	if len(raw) >= 2 && (raw[0] == '"' || raw[0] == '\'') && strings.HasSuffix(raw, string(raw[0])) {
		cur := filepath.Clean(filepath.FromSlash(raw[1 : len(raw)-1]))
		if cur == filepath.Clean(dir) {
			return text, false
		}
	}
	lines := strings.Split(text, "\n")
	lines = iniSet(lines, "extension_dir", "\""+dir+"\"")
	return strings.Join(lines, "\n"), true
}

/* ============================ 对外：配置文本 ============================ */

// GetConfigText 返回实例 php.ini 全文（不存在则先按模板生成）
func GetConfigText(id string) (string, error) {
	iniPath, err := iniPathOf(id)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(iniPath); err != nil {
		cfg, err := findInstance(id)
		if err != nil {
			return "", err
		}
		if isDockerMode(cfg) {
			// 容器实例没有宿主 php.exe，拿不到 php.ini-development 模板，只写一份覆盖项
			if _, err := ensureDockerIni(cfg); err != nil {
				return "", err
			}
		} else {
			exe := phpExe(cfg.Version, cfg.NTS)
			if exe == "" {
				exe = phpExe(cfg.Version, !cfg.NTS)
			}
			if exe == "" {
				return "", fmt.Errorf("版本 %s 未安装，无法生成 php.ini", cfg.Version)
			}
			if err := ensureIni(cfg, filepath.Dir(exe), iniPath); err != nil {
				return "", err
			}
		}
	}
	data, err := os.ReadFile(iniPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// SaveConfigText 校验后写回实例 php.ini，返回告警信息（校验失败即回滚）
func SaveConfigText(id, text string) error {
	cfg, err := findInstance(id)
	if err != nil {
		return err
	}
	iniPath, err := iniPathOf(id)
	if err != nil {
		return err
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	old, oldErr := os.ReadFile(iniPath)
	if err := os.WriteFile(iniPath, []byte(text), 0o644); err != nil {
		return err
	}
	if warn := lintIni(cfg, iniPath); warn != "" {
		if oldErr == nil {
			_ = os.WriteFile(iniPath, old, 0o644)
		}
		return fmt.Errorf("php.ini 校验未通过（已恢复原文件）：\n%s", warn)
	}
	return nil
}

// lintIni 用 php -c 检查配置语法，返回错误输出（空串表示通过）
func lintIni(cfg *InstanceConfig, iniPath string) string {
	exe := phpExe(cfg.Version, cfg.NTS)
	if exe == "" {
		exe = phpExe(cfg.Version, !cfg.NTS)
	}
	if exe == "" {
		return ""
	}
	cmd := exec.Command(exe, "-c", iniPath, "-r", "echo 1;")
	cmd.SysProcAttr = hideWindowAttr()
	out, err := cmd.CombinedOutput()
	msg := strings.TrimSpace(string(out))
	if strings.HasSuffix(msg, "\n1") {
		msg = strings.TrimSpace(strings.TrimSuffix(msg, "\n1")) // 去掉 -r 脚本自身的输出
	}
	if err != nil {
		if msg == "" {
			msg = err.Error()
		}
		return msg
	}
	// PHP 对 ini 解析告警不返回非零退出码，需看输出
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "syntax error") || strings.Contains(lower, "parse error") || strings.Contains(lower, "fatal error") {
		return msg
	}
	return ""
}

/* ============================ 常用配置项 ============================ */

var commonIniKeys = []string{
	"memory_limit", "max_execution_time", "upload_max_filesize", "post_max_size",
	"display_errors", "error_reporting", "date.timezone", "expose_php", "extension_dir",
}

// CommonFields 实例常用配置项
type CommonFields struct {
	MemoryLimit       string `json:"memoryLimit"`
	MaxExecutionTime  string `json:"maxExecutionTime"`
	UploadMaxFilesize string `json:"uploadMaxFilesize"`
	PostMaxSize       string `json:"postMaxSize"`
	DisplayErrors     string `json:"displayErrors"`
	ErrorReporting    string `json:"errorReporting"`
	Timezone          string `json:"timezone"`
	ExposePHP         string `json:"exposePhp"`
}

var iniKeyByField = map[string]string{
	"memoryLimit":       "memory_limit",
	"maxExecutionTime":  "max_execution_time",
	"uploadMaxFilesize": "upload_max_filesize",
	"postMaxSize":       "post_max_size",
	"displayErrors":     "display_errors",
	"errorReporting":    "error_reporting",
	"timezone":          "date.timezone",
	"exposePhp":         "expose_php",
}

// GetCommon 读取常用配置项
func GetCommon(id string) (*CommonFields, error) {
	text, err := GetConfigText(id)
	if err != nil {
		return nil, err
	}
	f := &CommonFields{}
	for field, key := range iniKeyByField {
		v := iniGet(text, key)
		// 兼容 [Date] 段内的 date.timezone 与全局 timezone
		if key == "date.timezone" && v == "" {
			v = iniGet(text, "timezone")
		}
		switch field {
		case "memoryLimit":
			f.MemoryLimit = v
		case "maxExecutionTime":
			f.MaxExecutionTime = v
		case "uploadMaxFilesize":
			f.UploadMaxFilesize = v
		case "postMaxSize":
			f.PostMaxSize = v
		case "displayErrors":
			f.DisplayErrors = v
		case "errorReporting":
			f.ErrorReporting = v
		case "timezone":
			f.Timezone = v
		case "exposePhp":
			f.ExposePHP = v
		}
	}
	return f, nil
}

// SetCommon 批量写入常用配置项（空值跳过）
func SetCommon(id string, f CommonFields) error {
	text, err := GetConfigText(id)
	if err != nil {
		return err
	}
	cfg, err := findInstance(id)
	if err != nil {
		return err
	}
	lines := strings.Split(text, "\n")
	apply := func(key, val string) {
		if strings.TrimSpace(val) == "" {
			return
		}
		lines = iniSet(lines, key, strings.TrimSpace(val))
	}
	apply("memory_limit", f.MemoryLimit)
	apply("max_execution_time", f.MaxExecutionTime)
	apply("upload_max_filesize", f.UploadMaxFilesize)
	apply("post_max_size", f.PostMaxSize)
	apply("display_errors", f.DisplayErrors)
	apply("error_reporting", f.ErrorReporting)
	apply("expose_php", f.ExposePHP)
	apply("date.timezone", f.Timezone)
	iniPath, err := iniPathOf(id)
	if err != nil {
		return err
	}
	return writeChecked(cfg, iniPath, lines)
}

/* ============================ 扩展管理 ============================ */

const extMarker = "; ---- go-tools extensions ----"

// Extensions 返回候选扩展列表（ext 目录 dll + 已加载 + 启用状态 + 无法开关的内置项）
func Extensions(id string) ([]ExtensionInfo, error) {
	cfg, err := findInstance(id)
	if err != nil {
		return nil, err
	}
	if isDockerMode(cfg) {
		return nil, dockerGateError("扩展管理")
	}
	exe := phpExe(cfg.Version, cfg.NTS)
	if exe == "" {
		exe = phpExe(cfg.Version, !cfg.NTS)
	}
	if exe == "" {
		return nil, fmt.Errorf("版本 %s 未安装", cfg.Version)
	}
	phpDir := filepath.Dir(exe)
	text, err := GetConfigText(id)
	if err != nil {
		return nil, err
	}
	loaded := map[string]bool{}
	if names, err := LoadedExtensions(id); err == nil {
		for _, l := range names {
			loaded[strings.ToLower(l)] = true
		}
	}
	out := map[string]*ExtensionInfo{}
	order := []string{}

	// ext 目录中的 dll
	if extDir := filepath.Join(phpDir, "ext"); true {
		if files, err := os.ReadDir(extDir); err == nil {
			for _, f := range files {
				name := f.Name()
				if f.IsDir() || !strings.HasSuffix(strings.ToLower(name), ".dll") {
					continue
				}
				ext := strings.TrimPrefix(strings.TrimSuffix(name, ".dll"), "php_")
				if ext == "" {
					continue
				}
				key := strings.ToLower(ext)
				if _, ok := out[key]; ok {
					continue
				}
				out[key] = &ExtensionInfo{
					Name: ext, DLL: name, Available: true,
					Enabled: iniHasActiveExt(text, ext),
					Loaded:  loaded[key],
				}
				order = append(order, key)
			}
		}
	}
	// ini 中已启用但 dll 不在 ext 目录的写法（例如 extension=php_xdebug.dll / 静态内置名）
	for _, l := range strings.Split(text, "\n") {
		if iniIsComment(l) {
			continue
		}
		k := iniKeyOfLine(l)
		if k != "extension" && k != "zend_extension" {
			continue
		}
		val := iniLineValue(l)
		if val == "" {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSuffix(val, ".dll"), "php_"), ".so")
		key := strings.ToLower(name)
		if _, ok := out[key]; ok {
			continue
		}
		out[key] = &ExtensionInfo{Name: name, DLL: val, Available: false, Enabled: true, Loaded: loaded[key]}
		order = append(order, key)
	}
	// php -m 中已加载但既无 dll 也无 ini 行的：内置扩展，仅展示
	for _, l := range loadedKeys(loaded) {
		key := strings.ToLower(l)
		if _, ok := out[key]; ok {
			continue
		}
		out[key] = &ExtensionInfo{Name: l, Available: false, Enabled: true, Loaded: true, System: true}
		order = append(order, key)
	}
	sort.Strings(order)
	list := make([]ExtensionInfo, 0, len(order))
	for _, k := range order {
		list = append(list, *out[k])
	}
	return list, nil
}

// iniHasActiveExt php.ini 中是否存在生效的 extension=xxx（兼容带 dll 后缀写法）
func iniHasActiveExt(text, ext string) bool {
	target := strings.ToLower(ext)
	for _, l := range strings.Split(text, "\n") {
		if iniIsComment(l) {
			continue
		}
		k := iniKeyOfLine(l)
		if k != "extension" && k != "zend_extension" {
			continue
		}
		val := strings.ToLower(iniLineValue(l))
		val = strings.TrimSuffix(val, ".dll")
		val = strings.TrimPrefix(val, "php_")
		if val == target {
			return true
		}
	}
	return false
}

func iniLineValue(l string) string {
	if i := strings.IndexAny(l, ":="); i >= 0 {
		return strings.Trim(strings.TrimSpace(strings.SplitN(l[i+1:], ";", 2)[0]), `"'`)
	}
	return ""
}

// isZendExt 需以 zend_extension 方式加载的扩展
func isZendExt(name string) bool {
	switch strings.ToLower(name) {
	case "opcache", "xdebug", "sourceguardian", "ioncube_loader", "simde":
		return true
	}
	return false
}

// SetExtension 启用/禁用扩展；返回 php 校验告警（不阻断，仅提示）
func SetExtension(id, name string, enable bool) (string, error) {
	cfg, err := findInstance(id)
	if err != nil {
		return "", err
	}
	if isDockerMode(cfg) {
		return "", dockerGateError("扩展管理")
	}
	exe := phpExe(cfg.Version, cfg.NTS)
	if exe == "" {
		exe = phpExe(cfg.Version, !cfg.NTS)
	}
	if exe == "" {
		return "", fmt.Errorf("版本 %s 未安装", cfg.Version)
	}
	phpDir := filepath.Dir(exe)
	text, err := GetConfigText(id)
	if err != nil {
		return "", err
	}
	lines := strings.Split(text, "\n")
	target := strings.ToLower(name)
	// extValue/present 由平台层决定：Windows 要 <php>/ext/php_<name>.dll 存在，
	// Linux 只写扩展名（可能在 scan 目录里，看文件是看不到的）。
	extValue, present := extIniValue(name, phpDir)
	if enable && !present {
		return "", fmt.Errorf("扩展 %s 不在 %s 中，无法启用", name, filepath.ToSlash(filepath.Join(phpDir, "ext")))
	}
	matched := false
	for i, l := range lines {
		k := iniKeyOfLine(l)
		if k != "extension" && k != "zend_extension" {
			continue
		}
		val := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(iniLineValue(l), "php_"), ".dll"))
		if val != target {
			continue
		}
		matched = true
		if enable {
			lines[i] = k + "=" + extValue
		} else if !iniIsComment(l) {
			lines[i] = ";" + l
		}
	}
	if enable && !matched {
		idx := -1
		for i, l := range lines {
			if strings.TrimSpace(l) == extMarker {
				idx = i
			}
		}
		line := "extension=" + extValue
		if isZendExt(name) {
			line = "zend_extension=" + extValue // opcache / xdebug 须用 zend_extension 加载
		}
		if idx >= 0 {
			lines = append(lines[:idx+1], append([]string{line}, lines[idx+1:]...)...)
		} else {
			lines = append(lines, extMarker, line)
		}
	}
	// 确保 extension_dir 正确（仅绿色版：Linux 的 extension_dir 是发行版的，不能改）
	if rewriteExtDir {
		if fixed, changed := iniSetExtDir(strings.Join(lines, "\n"), phpDir); changed {
			lines = strings.Split(fixed, "\n")
		}
	}
	iniPath, err := iniPathOf(id)
	if err != nil {
		return "", err
	}
	warn, err := writeCheckedRet(cfg, iniPath, lines)
	if err != nil {
		return "", err
	}
	return warn, nil
}

/* ============================ 禁用函数 ============================ */

// DisableFunctions 返回当前 disable_functions 列表
func DisableFunctions(id string) ([]string, error) {
	text, err := GetConfigText(id)
	if err != nil {
		return nil, err
	}
	return splitList(iniGet(iniJoin(text), "disable_functions")), nil
}

// SetDisableFunctions 整体设置 disable_functions
func SetDisableFunctions(id string, list []string) error {
	cfg, err := findInstance(id)
	if err != nil {
		return err
	}
	text, err := GetConfigText(id)
	if err != nil {
		return err
	}
	lines := strings.Split(iniJoin(text), "\n")
	lines = iniSet(lines, "disable_functions", joinList(list))
	iniPath, err := iniPathOf(id)
	if err != nil {
		return err
	}
	return writeChecked(cfg, iniPath, lines)
}

// AddDisableFunctions 追加禁用函数（已存在的忽略）
func AddDisableFunctions(id string, names []string) error {
	cur, err := DisableFunctions(id)
	if err != nil {
		return err
	}
	set := map[string]bool{}
	for _, n := range cur {
		set[strings.ToLower(n)] = true
	}
	out := cur
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || set[strings.ToLower(n)] {
			continue
		}
		set[strings.ToLower(n)] = true
		out = append(out, n)
	}
	return SetDisableFunctions(id, out)
}

// RemoveDisableFunctions 解除禁用
func RemoveDisableFunctions(id string, names []string) error {
	cur, err := DisableFunctions(id)
	if err != nil {
		return err
	}
	drop := map[string]bool{}
	for _, n := range names {
		drop[strings.ToLower(strings.TrimSpace(n))] = true
	}
	out := cur[:0]
	for _, n := range cur {
		if drop[strings.ToLower(n)] {
			continue
		}
		out = append(out, n)
	}
	return SetDisableFunctions(id, out)
}

// commonlyDisabled 常见建议禁用的危险函数（前端快捷添加用）
func CommonlyDisabled() []string {
	return []string{
		"exec", "passthru", "shell_exec", "proc_open", "proc_get_status",
		"popen", "curl_multi_exec", "parse_ini_file", "show_source",
		"system", "dl", "putenv", "fsockopen", "pfsockopen",
		"apache_child_terminate", "posix_kill", "posix_mkfifo", "posix_setuid",
		"posix_getpwuid", "leak", "symlink", "link", "popen",
	}
}

func splitList(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(strings.Trim(p, `"'`))
		if p == "" || seen[strings.ToLower(p)] {
			continue
		}
		seen[strings.ToLower(p)] = true
		out = append(out, p)
	}
	return out
}

func joinList(list []string) string {
	var out []string
	seen := map[string]bool{}
	for _, p := range list {
		p = strings.TrimSpace(p)
		if p == "" || seen[strings.ToLower(p)] {
			continue
		}
		seen[strings.ToLower(p)] = true
		out = append(out, p)
	}
	return strings.Join(out, ",")
}

/* ============================ php -i 解析 ============================ */

// iniMust 实例 php.ini 路径（不存在不报错）
func iniMust(id string) string {
	p, _ := iniPathOf(id)
	return p
}

var phpIniScannedRe = regexp.MustCompile(`Scan this dir for additional \.ini files\s+=>\s+(.*)`)

// runPHPInfo 执行 php -i 并解析关键项
func runPHPInfo(exe, iniPath string) (*PHPInfo, error) {
	args := []string{"-i"}
	if iniPath != "" {
		if _, err := os.Stat(iniPath); err == nil {
			args = []string{"-c", iniPath, "-i"}
		}
	}
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = hideWindowAttr()
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parsePHPInfo(string(out)), nil
}

func parsePHPInfo(text string) *PHPInfo {
	info := &PHPInfo{}
	flat := map[string]string{}
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimRight(l, "\r")
		if l == "" || strings.HasPrefix(l, "php)") || strings.HasPrefix(l, "|") || strings.HasPrefix(l, "+") {
			continue
		}
		i := strings.Index(l, "=>")
		if i < 0 {
			if j := strings.Index(l, "=> "); j >= 0 {
				i = j
			}
		}
		if i <= 0 {
			continue
		}
		k := strings.TrimSpace(l[:i])
		v := strings.TrimSpace(l[i+2:])
		flat[k] = v
	}
	info.Version = flat["PHP Version"]
	info.SAPI = flat["Server API"]
	info.PHPIni = flat["Loaded Configuration File"]
	info.ExtensionDir = flat["extension_dir"]
	info.MemoryLimit = flat["memory_limit"]
	info.UploadMaxFilesize = flat["upload_max_filesize"]
	info.PostMaxSize = flat["post_max_size"]
	info.MaxExecTime = flat["max_execution_time"]
	info.DisplayErrors = flat["display_errors"]
	info.Timezone = flat["date.timezone"]
	info.DisableFunctions = flat["disable_functions"]
	info.Loaded = modulesFrom(phpInfoModules(text))
	return info
}

// loadedKeys 已加载扩展名的稳定排序副本
func loadedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// phpInfoModules 从 php -i 输出中抽取已加载扩展名：
// 「PHP Core」与「Additional .ini files parsed」之间的段落标题即为扩展名
// （如「[PHP Session]」→ Session、「[PHP bcmath Extension]」→ bcmath）。
func phpInfoModules(text string) []string {
	var out []string
	in := false
	for _, l := range strings.Split(text, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case t == "PHP Core":
			in = true
			continue
		case strings.HasPrefix(t, "Additional .ini files parsed"), strings.HasPrefix(t, "Configure Command"):
			in = false
			continue
		}
		if !in {
			continue
		}
		m := phpSectionRe.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		name := strings.TrimSpace(strings.TrimSuffix(m[1], " Extension"))
		if name == "" || strings.EqualFold(name, "Core") {
			continue
		}
		out = append(out, name)
	}
	return out
}

var phpSectionRe = regexp.MustCompile(`^\[PHP\s+(.+)\]\s*$`)

func modulesFrom(names []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range names {
		k := strings.ToLower(n)
		if n == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// LoadedExtensions 返回 php -m 实测的已加载扩展名
func LoadedExtensions(id string) ([]string, error) {
	cfg, err := findInstance(id)
	if err != nil {
		return nil, err
	}
	if isDockerMode(cfg) {
		return nil, dockerGateError("容器内扩展列表（php -m）")
	}
	exe := phpExe(cfg.Version, cfg.NTS)
	if exe == "" {
		exe = phpExe(cfg.Version, !cfg.NTS)
	}
	if exe == "" {
		return nil, fmt.Errorf("版本 %s 未安装", cfg.Version)
	}
	iniPath := iniMust(id)
	args := []string{"-m"}
	if st, err := os.Stat(iniPath); err == nil && !st.IsDir() {
		args = []string{"-c", iniPath, "-m"}
	}
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = hideWindowAttr()
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, l := range strings.Split(string(out), "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		names = append(names, l)
	}
	return modulesFrom(names), nil
}

// Info 返回实例 PHP 配置概览（基于 php -i）
func Info(id string) (*PHPInfo, error) {
	cfg, err := findInstance(id)
	if err != nil {
		return nil, err
	}
	if isDockerMode(cfg) {
		return nil, dockerGateError("phpinfo（需容器内执行 php -i，本工具未实现 docker exec）")
	}
	exe := phpExe(cfg.Version, cfg.NTS)
	if exe == "" {
		exe = phpExe(cfg.Version, !cfg.NTS)
	}
	if exe == "" {
		return nil, fmt.Errorf("版本 %s 未安装", cfg.Version)
	}
	return runPHPInfo(exe, iniMust(id))
}

/* ============================ 写回（含校验） ============================ */

func writeChecked(cfg *InstanceConfig, iniPath string, lines []string) error {
	_, err := writeCheckedRet(cfg, iniPath, lines)
	return err
}

// writeCheckedRet 写回并校验；返回 (php 告警文本, 错误)。校验失败自动回滚原文件。
func writeCheckedRet(cfg *InstanceConfig, iniPath string, lines []string) (string, error) {
	text := strings.Join(lines, "\n")
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	old, oldErr := os.ReadFile(iniPath)
	if err := os.WriteFile(iniPath, []byte(text), 0o644); err != nil {
		return "", err
	}
	warn := lintIni(cfg, iniPath)
	if warn != "" && strings.Contains(warn, "syntax error") {
		if oldErr == nil {
			_ = os.WriteFile(iniPath, old, 0o644)
		}
		return "", fmt.Errorf("php.ini 语法错误（已恢复原文件）：\n%s", warn)
	}
	return warn, nil
}
