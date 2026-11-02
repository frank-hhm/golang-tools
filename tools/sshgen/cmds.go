package sshgen

import (
	"fmt"
	"strings"
)

// CommandsOf 由主机上下文生成 ssh / scp / sftp 命令与 ~/.ssh/config 片段。
// 纯文本拼接，不执行；端口非 22 才带端口旗标（ssh 用 -p，scp/sftp 用 -P），
// 有密钥才带 -i，IPv6 地址补方括号，含空格的路径加引号（Windows 常态）。
func CommandsOf(h HostRef) Commands {
	port := h.Port
	if port == 0 {
		port = 22
	}
	host := strings.TrimSpace(h.Address)
	user := strings.TrimSpace(h.User)

	// 目标 user@host，IPv6 补方括号
	targetHost := host
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		targetHost = "[" + host + "]"
	}
	target := targetHost
	if user != "" {
		target = user + "@" + targetHost
	}

	keyFlag := ""
	if kp := strings.TrimSpace(h.KeyPath); kp != "" {
		keyFlag = fmt.Sprintf(` -i "%s"`, kp)
	}
	portSSH := ""
	portSCP := ""
	if port != 22 {
		portSSH = fmt.Sprintf(" -p %d", port)
		portSCP = fmt.Sprintf(" -P %d", port)
	}

	sshCmd := "ssh" + portSSH + keyFlag
	if target != "" {
		sshCmd += " " + target
	}
	scpCmd := "scp" + portSCP + keyFlag + " ./local.txt " + target + ":/remote/path"
	sftpCmd := "sftp" + portSCP + keyFlag
	if target != "" {
		sftpCmd += " " + target
	}

	return Commands{
		SSH:         sshCmd,
		SCP:         scpCmd,
		SFTP:        sftpCmd,
		ConfigBlock: configBlock(h, host, user, port),
	}
}

// configBlock 生成 ~/.ssh/config 的 Host 块。别名净化 ~ 与空白，避免产出不可解析的 config。
func configBlock(h HostRef, host, user string, port int) string {
	alias := sanitizeAlias(h.Alias)
	if alias == "" {
		alias = sanitizeAlias(host)
	}
	if alias == "" {
		alias = "myhost"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Host %s\n", alias)
	if host != "" {
		fmt.Fprintf(&b, "    HostName %s\n", host)
	}
	if user != "" {
		fmt.Fprintf(&b, "    User %s\n", user)
	}
	if port != 22 {
		fmt.Fprintf(&b, "    Port %d\n", port)
	}
	if kp := strings.TrimSpace(h.KeyPath); kp != "" {
		fmt.Fprintf(&b, "    IdentityFile \"%s\"\n", kp)
	}
	return b.String()
}

// sanitizeAlias 去掉别名里的 ~、空白与换行（这些会破坏 config 解析）。
func sanitizeAlias(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '~', ' ', '\t', '\n', '\r', '=':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
