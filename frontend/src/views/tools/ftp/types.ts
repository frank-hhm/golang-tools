/** 文件传输页的共享类型（与 tools/ftp 的 Go 结构字段一一对应） */

export interface FtpConnection {
  id: string;
  name: string;
  proto: string;
  host: string;
  port: number;
  user: string;
  auth: string;
  password?: string;
  keyPath?: string;
  charset?: string;
  passive?: boolean;
  remoteDir?: string;
  note?: string;
  updatedAt?: number;
}

export interface FtpSessionInfo {
  id: string;
  title: string;
  host: string;
  port: number;
  user: string;
  home: string;
}

export interface FtpEntry {
  name: string;
  path: string;
  isDir: boolean;
  size: number;
  modTime: string;
  mode: string;
  hidden: boolean;
  link: boolean;
  ext: string;
}

export interface FtpPart {
  name: string;
  path: string;
}

/** 跨栏拖拽落点事件的负载：只带传输真正需要的字段，拖拽状态才能不握 FtpEntry 实例 */
export interface FtpDragItem {
  path: string;
  name: string;
  isDir: boolean;
}

export interface FtpListing {
  path: string;
  parent: string;
  parts: FtpPart[];
  entries: FtpEntry[];
  dirs: number;
  files: number;
  size: number;
}

export interface FtpTransfer {
  id: string;
  dir: 'up' | 'down';
  name: string;
  local: string;
  remote: string;
  total: number;
  done: number;
  speed: number;
  state: 'queued' | 'running' | 'done' | 'failed' | 'canceled';
  err: string;
  started: number;
  ended: number;
}

export interface FtpPreview {
  kind: 'text' | 'image' | 'binary' | 'tooLarge';
  data: string;
  size: number;
  truncated: boolean;
}

/* ==================== 服务端 ==================== */

export interface FtpServerConfig {
  host: string;
  port: number;
  rootDir: string;
  autoStart: boolean;
}

export interface FtpAccount {
  id: string;
  username: string;
  /** 只在保存时上送；列表里恒为空，留空表示沿用原密码 */
  password?: string;
  /** 服务端根目录下的子目录，'.' 表示根 */
  rootDir: string;
  readOnly: boolean;
  enabled: boolean;
  note?: string;
  updatedAt?: number;
}

export interface FtpServerConn {
  id: string;
  username: string;
  clientIp: string;
  /** unix 秒 */
  connectedAt: number;
  readOnly: boolean;
}

export interface FtpServerStatus {
  state: 'stopped' | 'running';
  host: string;
  port: number;
  addr: string;
  rootDir: string;
  autoStart: boolean;
  startedAt: number;
  error?: string;
  accounts: number;
  conns: FtpServerConn[] | null;
  localIps: string[] | null;
}

export interface FtpServerLog {
  seq: number;
  time: string;
  level: 'info' | 'warn' | 'error';
  text: string;
}
