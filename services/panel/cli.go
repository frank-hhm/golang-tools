package panel

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// IsCLI 判断 argv 是否是一次面板管理命令（而不是启动面板本体）。
func IsCLI(args []string) bool {
	if len(args) < 2 {
		return false
	}
	switch args[1] {
	case "help", "-h", "--help",
		"show", "info",
		"passwd", "password",
		"user", "username",
		"port",
		"host",
		"entry",
		"on", "off",
		"reset",
		"restart",
		"menu":
		return true
	}
	return false
}

// RunCLI 执行面板管理命令，返回进程退出码。
//
// 存在的意义：面板改端口/改密码之后忘掉，网页已经进不去了；
// 服务器上只需要 `./go-tools-server passwd` 就能救回来，不必手改 JSON。
func RunCLI(args []string) int {
	if len(args) < 2 {
		printHelp()
		return 0
	}
	cmd := args[1]
	rest := args[2:]

	switch cmd {
	case "help", "-h", "--help":
		printHelp()
		return 0
	case "show", "info":
		return cmdShow()
	case "passwd", "password":
		return cmdPasswd(rest)
	case "user", "username":
		return cmdUser(rest)
	case "port":
		return cmdPort(rest)
	case "host":
		return cmdHost(rest)
	case "entry":
		return cmdEntry(rest)
	case "on":
		return cmdEnable(true)
	case "off":
		return cmdEnable(false)
	case "reset":
		return cmdReset()
	case "restart":
		return cmdRestart()
	case "menu":
		return cmdMenu()
	}
	printHelp()
	return 2
}

func printHelp() {
	fmt.Print(`
go-tools 面板管理命令

  用法: go-tools-server <命令> [参数]

    show                 查看当前面板配置（账号/密码是否已设/端口/入口）
    passwd [新密码]      修改面板密码；不带参数则随机生成一个
    user <账号>          修改面板账号
    port <端口>          修改监听端口（1~65535）
    host <地址>          修改监听地址（0.0.0.0 = 所有网卡，127.0.0.1 = 仅本机）
    entry <路径|off>     设置安全入口，如 entry /mypanel2026；off 关闭
    on | off             开启 / 关闭登录鉴权
    reset                重置账号为 admin 并生成随机密码
    restart              重启面板服务（systemd 或后台自我拉起）
    menu                 进入交互式菜单（对应上面的命令）
    help                 显示本帮助

  直接不带参数运行 = 启动面板服务。
`)
}

// box 打印一块带标题的等宽信息框，和面板命令行风格保持一致。
func box(title string, rows [][2]string) {
	const width = 54
	pad := func(s string, w int) string {
		// 中文按 2 个字符宽度估算，保证右侧竖线对齐
		cur := 0
		for _, r := range s {
			if r > 0x2E80 {
				cur += 2
			} else {
				cur++
			}
		}
		if cur >= w {
			return s
		}
		return s + strings.Repeat(" ", w-cur)
	}
	fmt.Printf("┌─ %s %s┐\n", title, strings.Repeat("─", maxInt(1, width-3-len(title)*2)))
	for _, r := range rows {
		fmt.Printf("│ %s %s│\n", pad(r[0], 12), pad(r[1], width-14))
	}
	fmt.Printf("└%s┘\n", strings.Repeat("─", width-1))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func cmdShow() int {
	s, generated, err := Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取面板配置失败: %v\n", err)
		return 1
	}
	if generated != "" {
		fmt.Printf("已生成新的面板配置，初始密码: %s\n", generated)
	}
	p, _ := Path()
	enabled := "已开启"
	if !s.Enabled {
		enabled = "已关闭（无需登录）"
	}
	entry := s.EntryPath
	if entry == "" {
		entry = "未设置"
	}
	box("面板信息", [][2]string{
		{"账号", s.Username},
		{"密码", "已设置（修改: passwd）"},
		{"登录鉴权", enabled},
		{"监听地址", s.ListenAddr()},
		{"安全入口", entry},
		{"会话时长", fmt.Sprintf("%d 小时", s.SessionHours)},
		{"访问地址", s.AccessURL()},
		{"配置文件", p},
		{"更新时间", s.UpdatedAt},
	})
	return 0
}

func cmdPasswd(args []string) int {
	pass := ""
	if len(args) > 0 {
		pass = strings.TrimSpace(args[0])
	}
	if pass == "" {
		pass = GeneratePassword()
		fmt.Printf("已生成随机密码: %s\n", pass)
	}
	s, err := SetPassword(pass)
	if err != nil {
		fmt.Fprintf(os.Stderr, "修改密码失败: %v\n", err)
		return 1
	}
	fmt.Printf("密码已更新（其它已登录设备将失效）。账号: %s\n", s.Username)
	return 0
}

func cmdUser(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "用法: user <账号>")
		return 2
	}
	name := strings.TrimSpace(args[0])
	if name == "" {
		fmt.Fprintln(os.Stderr, "账号不能为空")
		return 2
	}
	cur := Current()
	cur.Username = name
	if _, err := Save(cur); err != nil {
		fmt.Fprintf(os.Stderr, "修改账号失败: %v\n", err)
		return 1
	}
	// 账号变了也要推进会话代数，否则旧 Cookie 还能用
	if _, err := Update(func(s *Settings) error {
		s.Epoch++
		return nil
	}); err != nil {
		fmt.Fprintf(os.Stderr, "刷新会话失败: %v\n", err)
		return 1
	}
	fmt.Printf("账号已更新为 %s，所有设备需重新登录\n", name)
	return 0
}

func cmdPort(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "用法: port <端口>")
		return 2
	}
	port, err := strconv.Atoi(strings.TrimSpace(args[0]))
	if err != nil || port < 1 || port > 65535 {
		fmt.Fprintln(os.Stderr, "端口必须是 1~65535 之间的整数")
		return 2
	}
	cur := Current()
	cur.Port = port
	if _, err := Save(cur); err != nil {
		fmt.Fprintf(os.Stderr, "修改端口失败: %v\n", err)
		return 1
	}
	fmt.Printf("端口已改为 %d，重启面板服务后生效：%s\n", port, cur.AccessURL())
	return noteRestart()
}

func cmdHost(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "用法: host <地址>")
		return 2
	}
	host := strings.TrimSpace(args[0])
	cur := Current()
	cur.Host = host
	if _, err := Save(cur); err != nil {
		fmt.Fprintf(os.Stderr, "修改监听地址失败: %v\n", err)
		return 1
	}
	fmt.Printf("监听地址已改为 %s，重启面板服务后生效\n", host)
	return noteRestart()
}

func cmdEntry(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "用法: entry <路径|off>")
		return 2
	}
	value := strings.TrimSpace(args[0])
	if err := ValidateEntry(value); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 2
	}
	cur := Current()
	cur.EntryPath = normalizeEntry(value)
	if _, err := Update(func(s *Settings) error {
		s.EntryPath = normalizeEntry(value)
		s.Epoch++
		return nil
	}); err != nil {
		fmt.Fprintf(os.Stderr, "修改安全入口失败: %v\n", err)
		return 1
	}
	if cur.EntryPath == "" {
		fmt.Println("已关闭安全入口，直接访问根路径即可。已登录设备需重新登录。")
	} else {
		fmt.Printf("安全入口已设为 %s\n访问地址: %s\n", cur.EntryPath, cur.AccessURL())
	}
	return 0
}

func cmdEnable(on bool) int {
	if _, err := Update(func(s *Settings) error {
		s.Enabled = on
		return nil
	}); err != nil {
		fmt.Fprintf(os.Stderr, "修改鉴权开关失败: %v\n", err)
		return 1
	}
	if on {
		fmt.Println("已开启登录鉴权，重启面板服务后生效。")
	} else {
		fmt.Println("警告：已关闭登录鉴权，任何能访问该端口的人都可以直接操作面板。重启后生效。")
	}
	return noteRestart()
}

func cmdReset() int {
	pass := GeneratePassword()
	cur := Current()
	cur.Username = defaultUsername
	if err := func() error {
		_, err := Update(func(s *Settings) error {
			if err := s.setPasswordLocked(pass); err != nil {
				return err
			}
			s.Username = defaultUsername
			s.Epoch++
			s.UpdatedAt = now()
			return nil
		})
		return err
	}(); err != nil {
		fmt.Fprintf(os.Stderr, "重置失败: %v\n", err)
		return 1
	}
	fmt.Printf("已重置为默认账号 %s，新密码: %s\n", defaultUsername, pass)
	return 0
}

func cmdRestart() int {
	if err := restartPanel(); err != nil {
		fmt.Fprintf(os.Stderr, "重启失败: %v\n", err)
		return 1
	}
	fmt.Println("面板服务已重启")
	return 0
}

// noteRestart 提示"改完要重启"，并尽力尝试自动重启；失败不算命令失败。
func noteRestart() int {
	if err := restartPanel(); err != nil {
		fmt.Printf("请手动重启面板服务后生效（%v）\n", err)
		return 0
	}
	fmt.Println("面板服务已重启")
	return 0
}

func cmdMenu() int {
	in := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print(`
=========================== 面板管理命令行 ===========================
 (1) 显示面板信息              (6) 修改安全入口
 (2) 修改面板密码              (7) 开启登录鉴权
 (3) 修改面板账号              (8) 关闭登录鉴权
 (4) 修改面板端口              (9) 重启面板服务
 (5) 修改监听地址              (0) 退出
=====================================================================
请输入命令编号: `)
		if !in.Scan() {
			fmt.Println()
			return 0
		}
		choice := strings.TrimSpace(in.Text())
		switch choice {
		case "0", "q", "quit", "exit":
			return 0
		case "1":
			cmdShow()
		case "2":
			pass := ask(in, "新密码（回车则随机生成）")
			cmdPasswd([]string{pass})
		case "3":
			name := ask(in, "新账号")
			cmdUser([]string{name})
		case "4":
			port := ask(in, "新端口")
			cmdPort([]string{port})
		case "5":
			host := ask(in, "新监听地址（0.0.0.0 = 所有网卡）")
			cmdHost([]string{host})
		case "6":
			entry := ask(in, "安全入口路径（如 /mypanel2026，输入 off 关闭）")
			cmdEntry([]string{entry})
		case "7":
			cmdEnable(true)
		case "8":
			cmdEnable(false)
		case "9":
			cmdRestart()
		default:
			fmt.Println("无效编号")
		}
	}
}

func ask(in *bufio.Scanner, prompt string) string {
	fmt.Printf("%s: ", prompt)
	if !in.Scan() {
		return ""
	}
	return strings.TrimSpace(in.Text())
}
