package redis

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

/* ============================ 事件推送 ============================ */

var emitter func(name string, data ...any)

// SetEmitter 注入事件发射器（绑定 Wails v3 app.Event.Emit）。
func SetEmitter(fn func(name string, data ...any)) {
	emitter = fn
}

func emit(name string, payload any) {
	if emitter != nil {
		emitter(name, payload)
	}
}

// MonitorLine 监控命令逐条推送给前端的负载。
type MonitorLine struct {
	Time   string `json:"time"`  // HH:MM:SS.mmm
	Stamp  string `json:"stamp"` // 服务端 unix 时间戳（秒，可含小数）
	DB     string `json:"db"`
	Client string `json:"client"`
	Cmd    string `json:"cmd"` // 命令内容
}

/* ============================ 数据结构 ============================ */

// ConnConfig Redis 连接配置。
type ConnConfig struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Password string `json:"password"`
	DB       int    `json:"db"`
}

// ServerStatus 服务器状态概览。
type ServerStatus struct {
	Version          string `json:"version"`
	Mode             string `json:"mode"`
	Role             string `json:"role"`
	UptimeSeconds    int64  `json:"uptimeSeconds"`
	ConnectedClients int64  `json:"connectedClients"`
	TotalKeys        int64  `json:"totalKeys"`
	UsedMemoryHuman  string `json:"usedMemoryHuman"`
	UsedMemory       int64  `json:"usedMemory"`
	MaxMemoryHuman   string `json:"maxMemoryHuman"`
	RedisMemory      string `json:"redisMemory"` // used_memory_human 别名展示
	Host             string `json:"host"`
}

// KeyInfo 键列表条目。
type KeyInfo struct {
	Key  string `json:"key"`
	Type string `json:"type"`
	TTL  int64  `json:"ttl"` // 秒；-1 永不过期，-2 不存在
}

// ScanResult 一次 SCAN 的结果。
type ScanResult struct {
	Cursor string    `json:"cursor"` // 新游标，"0" 表示结束
	Keys   []KeyInfo `json:"keys"`
}

// ZItem 有序集合成员。
type ZItem struct {
	Member string  `json:"member"`
	Score  float64 `json:"score"`
}

// KeyValue 键详情（按类型填充相应字段）。
type KeyValue struct {
	Key    string            `json:"key"`
	Type   string            `json:"type"`
	TTL    int64             `json:"ttl"`
	Str    string            `json:"str"`
	Hash   map[string]string `json:"hash"`
	List   []string          `json:"list"`
	Set    []string          `json:"set"`
	ZSet   []ZItem           `json:"zset"`
	Exists bool              `json:"exists"`
}

// SlowlogEntry 慢日志条目。
type SlowlogEntry struct {
	ID         int64  `json:"id"`
	Time       int64  `json:"time"`     // unix 秒
	Duration   int64  `json:"duration"` // 微秒
	Command    string `json:"command"`
	ClientAddr string `json:"clientAddr"`
	ClientName string `json:"clientName"`
	Latency    int64  `json:"latency"`
}

/* ============================ 连接管理 ============================ */

var (
	mu          sync.Mutex
	conns       = map[string]*Conn{} // 已打开连接：id -> conn
	monitorConn *Conn                // 监控专用连接
	monitorStop chan struct{}        // 监控停止信号
)

func addrOf(cfg ConnConfig) string {
	port := cfg.Port
	if port <= 0 {
		port = 6379
	}
	host := cfg.Host
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// Open 建立并登记一个连接（同 id 会先关闭旧连接）。
func Open(cfg ConnConfig) error {
	if cfg.ID == "" {
		return fmt.Errorf("连接 ID 不能为空")
	}
	c, err := dial(addrOf(cfg), cfg.Password, cfg.DB, 5*time.Second)
	if err != nil {
		return err
	}
	mu.Lock()
	if old, ok := conns[cfg.ID]; ok {
		old.Close()
	}
	conns[cfg.ID] = c
	mu.Unlock()
	return nil
}

// Close 关闭并移除指定连接。
func Close(id string) error {
	mu.Lock()
	defer mu.Unlock()
	if c, ok := conns[id]; ok {
		c.Close()
		delete(conns, id)
	}
	return nil
}

// getConn 取得已打开连接。
func getConn(id string) (*Conn, error) {
	mu.Lock()
	defer mu.Unlock()
	c, ok := conns[id]
	if !ok {
		return nil, fmt.Errorf("连接未打开")
	}
	return c, nil
}

/* ============================ 状态 ============================ */

// Status 拉取并解析服务器 INFO，返回状态概览。
func Status(id string) (ServerStatus, error) {
	st := ServerStatus{}
	c, err := getConn(id)
	if err != nil {
		return st, err
	}
	info, err := c.Command("INFO")
	if err != nil {
		return st, err
	}
	kv := parseInfo(asString(info))
	st.Version = kv["redis_version"]
	st.Mode = kv["redis_mode"]
	st.Role = kv["role"]
	st.UptimeSeconds = parseInt(kv["uptime_in_seconds"])
	st.ConnectedClients = parseInt(kv["connected_clients"])
	st.UsedMemoryHuman = kv["used_memory_human"]
	st.UsedMemory = parseInt(kv["used_memory"])
	st.MaxMemoryHuman = kv["max_memory_human"]
	if st.Mode == "" {
		st.Mode = "standalone"
	}
	if st.Role == "" {
		st.Role = "master"
	}
	// 键总数：汇总所有 dbN 的 keys=
	st.TotalKeys = countAllKeys(kv)
	return st, nil
}

func countAllKeys(kv map[string]string) int64 {
	var total int64
	for k, v := range kv {
		if strings.HasPrefix(k, "db") && strings.Contains(v, "keys=") {
			for _, part := range strings.Split(v, ",") {
				if strings.HasPrefix(part, "keys=") {
					total += parseInt(strings.TrimPrefix(part, "keys="))
				}
			}
		}
	}
	return total
}

func parseInfo(s string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(s, "\r\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, ":"); i > 0 {
			m[line[:i]] = line[i+1:]
		}
	}
	return m
}

func parseInt(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

/* ============================ 键 ============================ */

// Scan 使用 SCAN 增量获取键，并为每个键补充类型与 TTL。
func Scan(id, pattern string, count int, limit int) (ScanResult, error) {
	res := ScanResult{Cursor: "0"}
	c, err := getConn(id)
	if err != nil {
		return res, err
	}
	if count <= 0 {
		count = 200
	}
	if pattern == "" {
		pattern = "*"
	}
	if limit <= 0 {
		limit = 1000
	}
	cursor := "0"
	for {
		reply, err := c.Command("SCAN", cursor, "MATCH", pattern, "COUNT", strconv.Itoa(count))
		if err != nil {
			return res, err
		}
		arr := asArray(reply)
		if len(arr) != 2 {
			break
		}
		cursor = asString(arr[0])
		for _, k := range asStringSlice(arr[1]) {
			ki := KeyInfo{Key: k}
			if t, err := c.Command("TYPE", k); err == nil {
				ki.Type = asString(t)
			}
			if ttl, err := c.Command("TTL", k); err == nil {
				ki.TTL = asInt(ttl)
			}
			res.Keys = append(res.Keys, ki)
			if len(res.Keys) >= limit {
				res.Cursor = cursor
				return res, nil
			}
		}
		if cursor == "0" {
			break
		}
	}
	res.Cursor = cursor
	return res, nil
}

// GetKey 读取键详情，按类型返回对应结构。
func GetKey(id, key string) (KeyValue, error) {
	kv := KeyValue{Key: key}
	c, err := getConn(id)
	if err != nil {
		return kv, err
	}
	typReply, err := c.Command("TYPE", key)
	if err != nil {
		return kv, err
	}
	kv.Type = asString(typReply)
	if ttl, err := c.Command("TTL", key); err == nil {
		kv.TTL = asInt(ttl)
	}
	switch kv.Type {
	case "string":
		v, err := c.Command("GET", key)
		if err != nil {
			return kv, err
		}
		kv.Str = asString(v)
		kv.Exists = v != nil
	case "hash":
		v, err := c.Command("HGETALL", key)
		if err != nil {
			return kv, err
		}
		arr := asArray(v)
		kv.Hash = map[string]string{}
		for i := 0; i+1 < len(arr); i += 2 {
			kv.Hash[asString(arr[i])] = asString(arr[i+1])
		}
		kv.Exists = len(arr) > 0
	case "list":
		v, err := c.Command("LRANGE", key, "0", "-1")
		if err != nil {
			return kv, err
		}
		kv.List = asStringSlice(v)
		kv.Exists = true
	case "set":
		v, err := c.Command("SMEMBERS", key)
		if err != nil {
			return kv, err
		}
		kv.Set = asStringSlice(v)
		sort.Strings(kv.Set)
		kv.Exists = true
	case "zset":
		v, err := c.Command("ZRANGE", key, "0", "-1", "WITHSCORES")
		if err != nil {
			return kv, err
		}
		arr := asArray(v)
		for i := 0; i+1 < len(arr); i += 2 {
			kv.ZSet = append(kv.ZSet, ZItem{Member: asString(arr[i]), Score: asFloat(arr[i+1])})
		}
		kv.Exists = true
	default:
		kv.Exists = false
	}
	return kv, nil
}

// SetString 写入/更新 string 类型键，ttl 秒（<=0 表示不设置过期）。
func SetString(id, key, value string, ttl int) error {
	c, err := getConn(id)
	if err != nil {
		return err
	}
	if _, err := c.Command("SET", key, value); err != nil {
		return err
	}
	if ttl > 0 {
		_, _ = c.Command("EXPIRE", key, strconv.Itoa(ttl))
	} else {
		_, _ = c.Command("PERSIST", key)
	}
	return nil
}

// BatchSetString 批量生成 string 键：以 keyPrefix+序号为键、valuePrefix+序号为值，
// 序号从 start 到 end（含两端），ttl 秒（<=0 表示不设置过期）。使用管道分块写入以减少往返，
// 返回成功写入的键数量。
func BatchSetString(id, keyPrefix, valuePrefix string, start, end, ttl int) (int64, error) {
	c, err := getConn(id)
	if err != nil {
		return 0, err
	}
	if end < start {
		return 0, fmt.Errorf("结束序号不能小于起始序号")
	}
	if end-start+1 > 100000 {
		return 0, fmt.Errorf("单次批量生成数量过大（上限 100000）")
	}
	var written int64
	const chunk = 500
	for i := start; i <= end; i += chunk {
		hi := i + chunk - 1
		if hi > end {
			hi = end
		}
		cmds := make([][]string, 0, (hi-i+1)*2)
		for j := i; j <= hi; j++ {
			key := keyPrefix + strconv.Itoa(j)
			val := valuePrefix + strconv.Itoa(j)
			cmds = append(cmds, []string{"SET", key, val})
			if ttl > 0 {
				cmds = append(cmds, []string{"EXPIRE", key, strconv.Itoa(ttl)})
			}
		}
		if _, err := c.Pipeline(cmds); err != nil {
			return written, err
		}
		written += int64(hi - i + 1)
	}
	return written, nil
}

// DeleteKeys 删除若干键，返回实际删除数量。
func DeleteKeys(id string, keys []string) (int64, error) {
	c, err := getConn(id)
	if err != nil {
		return 0, err
	}
	if len(keys) == 0 {
		return 0, nil
	}
	args := append([]string{"DEL"}, keys...)
	v, err := c.Command(args...)
	if err != nil {
		return 0, err
	}
	return asInt(v), nil
}

// SetTTL 设置/清除键过期时间（ttl<=0 视为持久化）。
func SetTTL(id, key string, ttl int) error {
	c, err := getConn(id)
	if err != nil {
		return err
	}
	if ttl > 0 {
		_, err = c.Command("EXPIRE", key, strconv.Itoa(ttl))
	} else {
		_, err = c.Command("PERSIST", key)
	}
	return err
}

/* ============================ 慢日志 ============================ */

// Slowlog 拉取慢查询日志（SLOWLOG GET count）。
func Slowlog(id string, count int) ([]SlowlogEntry, error) {
	c, err := getConn(id)
	if err != nil {
		return nil, err
	}
	if count <= 0 {
		count = 50
	}
	v, err := c.Command("SLOWLOG", "GET", strconv.Itoa(count))
	if err != nil {
		return nil, err
	}
	entries := asArray(v)
	out := make([]SlowlogEntry, 0, len(entries))
	for _, e := range entries {
		arr := asArray(e)
		if len(arr) < 4 {
			continue
		}
		entry := SlowlogEntry{
			ID:       asInt(arr[0]),
			Time:     asInt(arr[1]),
			Duration: asInt(arr[2]),
		}
		cmdArgs := asArray(arr[3])
		entry.Command = strings.Join(asStringSlice(cmdArgs), " ")
		if len(arr) > 4 {
			entry.ClientAddr = asString(arr[4])
		}
		if len(arr) > 5 {
			entry.ClientName = asString(arr[5])
		}
		if len(arr) > 6 {
			entry.Latency = asInt(arr[6])
		}
		out = append(out, entry)
	}
	return out, nil
}

/* ============================ 监控命令 ============================ */

// StartMonitor 建立独立连接执行 MONITOR，将每条命令通过 redis:monitor 事件推送。
func StartMonitor(cfg ConnConfig) error {
	mu.Lock()
	if monitorConn != nil {
		mu.Unlock()
		return nil // 已在监控
	}
	mu.Unlock()

	c, err := dial(addrOf(cfg), cfg.Password, cfg.DB, 5*time.Second)
	if err != nil {
		return err
	}
	if err := c.writeCommand("MONITOR"); err != nil {
		c.Close()
		return err
	}
	// MONITOR 先返回 +OK
	ok, err := c.readReply()
	if err != nil {
		c.Close()
		return err
	}
	_ = ok

	stop := make(chan struct{})
	mu.Lock()
	monitorConn = c
	monitorStop = stop
	mu.Unlock()

	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			// 监控行是批量字符串，形如：1699999999.999999 [0 127.0.0.1:52123] "GET" "key"
			line, err := c.readReply()
			if err != nil {
				select {
				case <-stop:
				default:
					emit("redis:monitor", MonitorLine{Time: nowStamp(), Cmd: "// 监控结束: " + err.Error()})
				}
				return
			}
			emit("redis:monitor", parseMonitorLine(asString(line)))
		}
	}()
	return nil
}

// StopMonitor 停止当前监控。
func StopMonitor() error {
	mu.Lock()
	defer mu.Unlock()
	if monitorStop != nil {
		close(monitorStop)
		monitorStop = nil
	}
	if monitorConn != nil {
		monitorConn.Close()
		monitorConn = nil
	}
	return nil
}

// parseMonitorLine 解析 MONITOR 的一行输出。
func parseMonitorLine(raw string) MonitorLine {
	ml := MonitorLine{Time: nowStamp(), Cmd: raw}
	// 格式: <stamp> [<db> <client:port>] "<cmd>" "<arg>"...
	sp := strings.Index(raw, " [")
	if sp < 0 {
		return ml
	}
	ml.Stamp = strings.TrimSpace(raw[:sp])
	rest := raw[sp+1:]
	ep := strings.Index(rest, "]")
	if ep < 0 {
		return ml
	}
	inner := rest[:ep] // <db> <client:port>
	if i := strings.Index(inner, " "); i > 0 {
		ml.DB = inner[:i]
		ml.Client = inner[i+1:]
	} else {
		ml.Client = inner
	}
	ml.Cmd = strings.TrimSpace(rest[ep+1:])
	return ml
}

func nowStamp() string {
	return time.Now().Format("15:04:05.000")
}

/* ============================ 连接配置持久化（文件） ============================ */

func connectionsFilePath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", herr
		}
		base = filepath.Join(home, ".config")
	}
	dir := filepath.Join(base, "go-tools")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "redis-connections.json"), nil
}

// SaveConnections 将连接配置列表持久化到用户配置目录下的 JSON 文件。
func SaveConnections(configs []ConnConfig) (string, error) {
	path, err := connectionsFilePath()
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(configs, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// LoadConnections 从配置文件读取连接列表；文件不存在时返回空列表。
func LoadConnections() ([]ConnConfig, error) {
	path, err := connectionsFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []ConnConfig{}, nil
		}
		return nil, err
	}
	var configs []ConnConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return nil, err
	}
	return configs, nil
}

// ConnectionsFilePath 返回配置文件路径（供前端展示）。
func ConnectionsFilePath() string {
	p, _ := connectionsFilePath()
	return p
}
