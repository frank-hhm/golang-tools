//go:build windows

// Package ocr 用 Windows 自带的 Windows.Media.Ocr（WinRT）做文字识别。
//
// 由根目录 ocr_windows.go 归档而来：绑定入口 (*App).OcrImage 仍留在 package main，
// 这里的实现与前端只通过一个字符串进出，属于纯能力模块。
package ocr

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	"Tools/services/applog"
	"Tools/tools/screenshot"
)

// 提取文字（OCR）：调用 Windows 自带的 Windows.Media.Ocr（WinRT）。
//
// 关键点：WinRT 投影只在 Windows PowerShell 5.1（powershell.exe）里可用，
// PowerShell 7（pwsh）没有 System.Runtime.WindowsRuntime，异步桥接会失败——
// 所以这里显式调用 powershell.exe，而不是 pwsh。脚本以 -EncodedCommand
// （UTF-16LE→Base64）传入，规避引号/换行在地面命令行上的转义问题；图片路径
// 经环境变量传进去，脚本只回吐识别文本到 stdout。
//
// 依赖：系统需装有对应语言的 OCR 识别包（“中文(简体)简易识别”等）。缺包时
// TryCreateFrom* 返回 null，脚本以 OCR_UNAVAILABLE 退出，这里翻译成友好提示。

// Image 对成品图（dataURL/base64 PNG）做文字识别，返回识别出的纯文本。
func Image(data string) (string, error) {
	if data == "" {
		return "", fmt.Errorf("没有可识别的图片数据")
	}
	raw, err := screenshot.DecodeImageBase64(data)
	if err != nil {
		return "", fmt.Errorf("图片解码失败: %w", err)
	}
	ns := time.Now().UnixNano()
	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("Tools-ocr-%d.png", ns))
	out := filepath.Join(os.TempDir(), fmt.Sprintf("Tools-ocr-%d.txt", ns))
	if err := os.WriteFile(tmp, raw, 0644); err != nil {
		return "", fmt.Errorf("写入临时图片失败: %w", err)
	}
	defer os.Remove(tmp)
	defer os.Remove(out)

	cmd := exec.Command("powershell.exe",
		"-NoProfile", "-NoLogo", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-EncodedCommand", encodePSCommand(ocrPS))
	// 识别结果由脚本按 UTF-8(无BOM) 写入 out 文件，绕开控制台编码的不确定性
	cmd.Env = append(os.Environ(), "GO_TOOLS_OCR_IMG="+tmp, "GO_TOOLS_OCR_OUT="+out)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	if runErr != nil {
		msg := strings.TrimSpace(stderr.String()) + " " + strings.TrimSpace(stdout.String())
		if strings.Contains(msg, "OCR_UNAVAILABLE") {
			return "", fmt.Errorf("系统未安装可用的 OCR 语言包：请在「设置 > 应用 > 可选功能」里添加“中文(简体)简易识别”后重试")
		}
		if msg = strings.TrimSpace(msg); msg == "" {
			msg = runErr.Error()
		}
		return "", fmt.Errorf("文字识别失败: %s", firstLine(msg))
	}

	b, err := os.ReadFile(out)
	if err != nil {
		return "", fmt.Errorf("读取识别结果失败: %w", err)
	}
	text := collapseCjkSpaces(strings.TrimSpace(strings.TrimPrefix(string(b), "\ufeff")))
	if text == "" {
		return "", fmt.Errorf("未识别到文字")
	}
	applog.Infof("capture", "OCR 完成 %d 字", len([]rune(text)))
	return text, nil
}

// collapseCjkSpaces 折叠 CJK 字符之间的空格：Windows.Media.Ocr 会把每个汉字当作
// 一个“词”，用空格连起来（“你好世界”→“你 好 世 界”）。仅删两侧都是 CJK 的空格，
// 保留中英文/数字之间本来有意义的空格。
func collapseCjkSpaces(s string) string {
	rs := []rune(s)
	out := make([]rune, 0, len(rs))
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if (r == ' ' || r == '\t') && len(out) > 0 {
			prev := out[len(out)-1]
			var next rune
			for j := i + 1; j < len(rs); j++ {
				if rs[j] != ' ' && rs[j] != '\t' {
					next = rs[j]
					break
				}
			}
			if isCJK(prev) && isCJK(next) {
				continue // 丢弃这个夹在两个 CJK 之间的空格
			}
		}
		out = append(out, r)
	}
	return string(out)
}

func isCJK(r rune) bool {
	switch {
	case r >= 0x3000 && r <= 0x303F, // CJK 标点
		r >= 0x3400 && r <= 0x4DBF,   // 扩展 A
		r >= 0x4E00 && r <= 0x9FFF,   // 基本汉字
		r >= 0xF900 && r <= 0xFAFF,   // 兼容汉字
		r >= 0xFF00 && r <= 0xFFEF,   // 全角字符
		r >= 0x20000 && r <= 0x2EBEF: // 扩展 B~F
		return true
	}
	return false
}

// encodePSCommand 把脚本编码成 powershell -EncodedCommand 需要的 Base64(UTF-16LE)。
func encodePSCommand(script string) string {
	utf := utf16.Encode([]rune(script))
	b := make([]byte, 0, len(utf)*2)
	for _, u := range utf {
		b = append(b, byte(u), byte(u>>8))
	}
	return base64.StdEncoding.EncodeToString(b)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// ocrPS 在 Windows PowerShell 5.1 下加载 WinRT OCR，识别 $env:GO_TOOLS_OCR_IMG 指向的图片。
const ocrPS = `
$ErrorActionPreference = 'Stop'
try {
  $img = $env:GO_TOOLS_OCR_IMG
  if (-not $img -or -not (Test-Path $img)) { Write-Error 'OCR_NO_IMAGE'; exit 3 }
  Add-Type -AssemblyName System.Runtime.WindowsRuntime
  $null = [Windows.Media.Ocr.OcrEngine,Windows.Foundation,ContentType=WindowsRuntime]
  $null = [Windows.Graphics.Imaging.BitmapDecoder,Windows.Foundation,ContentType=WindowsRuntime]
  $null = [Windows.Storage.StorageFile,Windows.Foundation,ContentType=WindowsRuntime]
  $null = [Windows.Globalization.Language,Windows.Foundation,ContentType=WindowsRuntime]
  $asTask = ([System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object {
      $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation` + "`" + `1'
  })[0]
  function Await($op, $type) {
    $t = $asTask.MakeGenericMethod($type).Invoke($null, @($op))
    $t.Wait(-1) | Out-Null
    $t.Result
  }
  $file = Await ([Windows.Storage.StorageFile]::GetFileFromPathAsync($img)) ([Windows.Storage.StorageFile])
  $stream = Await ($file.OpenAsync([Windows.Storage.FileAccessMode]::Read)) ([Windows.Storage.Streams.IRandomAccessStream])
  $decoder = Await ([Windows.Graphics.Imaging.BitmapDecoder]::CreateAsync($stream)) ([Windows.Graphics.Imaging.BitmapDecoder])
  $bitmap = Await ($decoder.GetSoftwareBitmapAsync()) ([Windows.Graphics.Imaging.SoftwareBitmap])
  $engine = $null
  foreach ($tag in @('zh-CN','zh-Hans-CN','zh','en-US')) {
    try {
      $lang = New-Object Windows.Globalization.Language $tag
      $engine = [Windows.Media.Ocr.OcrEngine]::TryCreateFromLanguage($lang)
      if ($engine) { break }
    } catch {}
  }
  if (-not $engine) { $engine = [Windows.Media.Ocr.OcrEngine]::TryCreateFromUserProfileLanguages() }
  if (-not $engine) { Write-Error 'OCR_UNAVAILABLE'; exit 2 }
  $result = Await ($engine.RecognizeAsync($bitmap)) ([Windows.Media.Ocr.OcrResult])
  $txt = ''
  if ($result) { $txt = $result.Text }
  [System.IO.File]::WriteAllText($env:GO_TOOLS_OCR_OUT, $txt, (New-Object System.Text.UTF8Encoding($false)))
} catch {
  if ($_.Exception.Message -match 'OCR_UNAVAILABLE') { Write-Error 'OCR_UNAVAILABLE'; exit 2 }
  Write-Error $_.Exception.Message
  exit 1
}
`
