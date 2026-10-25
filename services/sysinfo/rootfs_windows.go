//go:build windows

package sysinfo

// rootFS 是磁盘占用统计的取样路径。Windows 上看系统盘，其它平台看根分区。
const rootFS = "C:"
