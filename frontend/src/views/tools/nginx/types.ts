/** Nginx 工具页共享类型与小工具：实例配置在父页面与各弹窗子组件之间传递，集中一处避免重复声明。 */

/** 实例配置：与后端 NginxSrvSaveInstance 入参一致（字段名沿用绑定 models） */
export interface InstConfig {
    id: string; name: string; version: string; port: number; sslPort: number; domain: string; docRoot: string; workers: number; stubStatus: boolean;
    // mode: local | docker；容器模式不占本机版本库，靠 image 定位镜像
    mode: string; image: string; containerName: string;
    // PHP 后端：phpMode 空=纯静态 / fastcgi=php-cgi / proxy=反向代理到 php -S；phpPort/phpInstanceId 由后端回填
    phpMode: string; phpVersion: string; phpPort: number; phpInstanceId: string;
}

/** PHP 后端模式展示文案（父页面站点信息与新建实例弹窗共用） */
export function phpModeLabel(m: string): string {
    return m === "proxy" ? "反向代理(php -S)" : "FastCGI(php-cgi)";
}
