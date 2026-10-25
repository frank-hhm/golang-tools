/** 终端连接记录（对应 Go 侧 tools/term.Host） */
export interface TermHost {
  id: string;
  name: string;
  /** linux | windows，只决定默认端口与展示 */
  system: string;
  address: string;
  port: number;
  user: string;
  /** password | key */
  auth: string;
  /** 明文密码只在表单提交时带上；读盘返回的记录里恒为空 */
  password?: string;
  /** auth=key 时的私钥路径 */
  keyPath?: string;
  note?: string;
  updatedAt?: number;
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
