/** 终端连接记录（对应 Go 侧 tools/term.Host） */
export interface TermHost {
  id: string;
  name: string;
  /** linux | windows，只决定默认端口与展示 */
  system: string;
  address: string;
  port: number;
  /** 旧字段：未关联凭据时使用；关联凭据时留空由凭据提供 */
  user: string;
  /** password | key（旧字段） */
  auth: string;
  /** 明文密码只在表单提交时带上；读盘返回的记录里恒为空 */
  password?: string;
  /** auth=key 时的私钥路径（旧字段） */
  keyPath?: string;
  /** 关联的凭据 id（P0：一处改密多机生效） */
  credID?: string;
  /** 分组名（左栏分组导航） */
  group?: string;
  /** 标签（列表筛选） */
  tags?: string[];
  /** 列表配色 */
  color?: string;
  note?: string;
  /** 空/ssh=远程 SSH；local=本机 shell（本地 CMD / PowerShell 默认项） */
  kind?: string;
  /** local 专用：cmd | powershell */
  shell?: string;
  updatedAt?: number;
}

/** SSH 凭据（对应 Go 侧 tools/term.Credential；password/passphrase 读回恒为空） */
export interface TermCredential {
  id: string;
  name: string;
  /** password | key */
  auth: string;
  user?: string;
  password?: string;
  keyPath?: string;
  passphrase?: string;
  updatedAt?: number;
}

/** ~/.ssh/config 解析出的主机草稿（对应 Go 侧 tools/term.HostDraft） */
export interface TermHostDraft {
  alias: string;
  hostName: string;
  user: string;
  port: number;
  identityFile: string;
  /** 本期不生效、仅如实标注的指令名（ProxyJump/ForwardAgent 等） */
  unsupported?: string[];
  /** 模式含通配符，不宜直接落库 */
  wildcard?: boolean;
}

/** known_hosts 条目（对应 Go 侧 tools/term.KnownHostEntry） */
export interface TermKnownHost {
  address: string;
  keyType: string;
  fingerprint: string;
}

/** 页面上打开的一个终端标签 */
export interface TermTab {
  /** 前端自增 key，与 Go 会话 id 解耦（会话断了标签还在） */
  key: string;
  hostId: string;
  title: string;
  /** 会话已建立（TermOpenSSH 成功返回） */
  ready?: boolean;
  /** 远端已断开 */
  dead?: boolean;
}
