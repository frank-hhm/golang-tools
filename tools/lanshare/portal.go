package lanshare

// 分享链接的下载端点与极简首页。
// 首页必须是纯 HTML 内联样式：服务不托管任何静态资源，少一个部署变量。

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

// handleDownload /s/<token>
// 失效（不存在/过期/次数用尽/实体被删）一律 404，不区分原因也不回页面——
// 未授权的探测拿不到「这条链接曾经存在」这类信息。
// 放行后才计数，ServeFile 保留 Range 能力（大文件续传）。
func (s *httpServer) handleDownload(w http.ResponseWriter, r *http.Request) {
	token := safeSegment(r.URL.Path[len("/s/"):])
	l, ok := touchLink(token, time.Now())
	if !ok {
		http.NotFound(w, r)
		accessLine(r, "download", token, 0, false)
		return
	}
	f, err := os.Open(l.Path)
	if err != nil {
		http.NotFound(w, r)
		accessLine(r, "download", l.Name, 0, false)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Disposition", disposition(l.Name))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, l.Name, time.UnixMilli(0), f)
	accessLine(r, "download", l.Name, l.Bytes, true)
}

// safeSegment 去掉 token 里除 URL 安全字符外的一切，防注入到路径/日志。
func safeSegment(raw string) string {
	var b []byte
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			b = append(b, c)
		}
	}
	return string(b)
}

// ShareURL 为链接拼一条可发送的 URL（前端与文档中心「分享」都用它）。
// 服务未运行时返回错误，由调用方提示「先启动互传服务」。
func ShareURL(token string) (string, error) {
	st := StatusNow()
	if !st.Running {
		return "", fmt.Errorf("互传服务未运行，请先启动")
	}
	if len(st.URLs) == 0 {
		return "", fmt.Errorf("服务未绑定可访问地址")
	}
	return st.URLs[0] + "/s/" + token, nil
}

func indexHTMLFor(inbox string) string {
	return fmt.Sprintf(indexHTML, escapeHTML(inbox))
}

func escapeHTML(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			out = append(out, "&lt;"...)
		case '>':
			out = append(out, "&gt;"...)
		case '&':
			out = append(out, "&amp;"...)
		case '"':
			out = append(out, "&quot;"...)
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}

const indexHTML = `<!doctype html><html lang="zh"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>局域网互传 · 上传</title><style>
*{box-sizing:border-box}
body{font-family:system-ui,-apple-system,"Segoe UI","Microsoft YaHei",sans-serif;margin:0;min-height:100vh;padding:32px 16px;background:linear-gradient(180deg,#eef1f6,#f6f7f9);color:#1d2129;display:flex;align-items:flex-start;justify-content:center}
.card{background:#fff;border:1px solid #eceef2;border-radius:16px;padding:28px;max-width:560px;margin:0 auto;box-shadow:0 10px 30px rgba(29,33,41,.06)}
.head{display:flex;align-items:center;gap:14px}
.logo{flex:none;width:44px;height:44px;border-radius:12px;background:linear-gradient(135deg,#165dff,#4080ff);display:flex;align-items:center;justify-content:center;box-shadow:0 6px 16px rgba(22,93,255,.25)}
.logo svg{width:24px;height:24px;stroke:#fff}
h1{font-size:19px;margin:0;font-weight:600}
.sub{font-size:13px;color:#86909c;margin:2px 0 0}
.inbox{display:flex;align-items:center;gap:8px;margin:18px 0 6px;font-size:12px;color:#4e5969}
.inbox svg{width:15px;height:15px;stroke:#86909c;flex:none}
.inbox code{background:#f2f3f5;border-radius:6px;padding:4px 10px;font-size:12px;color:#4e5969;word-break:break-all;font-family:ui-monospace,Consolas,monospace}
.drop{border:2px dashed #d5d9e0;border-radius:14px;padding:34px 20px;text-align:center;cursor:pointer;transition:all .18s ease;background:#fafbfc;margin-top:8px}
.drop:hover{border-color:#4080ff;background:#f5f9ff}
.drop.on{border-color:#165dff;background:#eaf3ff;transform:scale(1.01)}
.drop .up{width:40px;height:40px;margin:0 auto 10px;stroke:#165dff;opacity:.85}
.drop .t1{font-size:15px;color:#1d2129;font-weight:500}
.drop .t2{font-size:12px;color:#86909c;margin-top:4px}
input[type=file]{display:none}
#list{margin-top:14px;display:flex;flex-direction:column;gap:6px}
.fitem{display:flex;align-items:center;gap:10px;background:#f7f8fa;border:1px solid #eef0f3;border-radius:8px;padding:9px 12px;font-size:13px}
.fitem .fn{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.fitem .fs{color:#86909c;font-size:12px;flex:none}
.fitem .fd{flex:none;width:16px;height:16px;stroke:#c9cdd4;cursor:pointer}
.fitem .fd:hover{stroke:#f53f3f}
button{display:block;width:100%%;margin-top:18px;background:linear-gradient(135deg,#165dff,#4080ff);color:#fff;border:0;border-radius:10px;padding:12px 18px;font-size:15px;font-weight:500;cursor:pointer;transition:all .18s ease;box-shadow:0 6px 16px rgba(22,93,255,.22)}
button:hover:not(:disabled){transform:translateY(-1px);box-shadow:0 8px 20px rgba(22,93,255,.3)}
button:disabled{opacity:.5;cursor:not-allowed;box-shadow:none}
#msg{font-size:13px;margin-top:14px;white-space:pre-wrap;word-break:break-all;border-radius:8px;line-height:1.6}
#msg.ok{background:#e8ffea;color:#00a843;padding:10px 12px}
#msg.err{background:#ffece8;color:#e53935;padding:10px 12px}
#msg.info{background:#e8f3ff;color:#165dff;padding:10px 12px}
.foot{display:flex;align-items:center;gap:6px;font-size:12px;color:#c97900;margin-top:16px;padding-top:14px;border-top:1px solid #f2f3f5}
.foot svg{width:15px;height:15px;stroke:#c97900;flex:none}
</style></head><body>
<div class="card">
<div class="head"><div class="logo"><svg viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 16V4M7 9l5-5 5 5"/><path d="M4 16v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2"/></svg></div>
<div><h1>上传到这台机器</h1><div class="sub">局域网设备免密投递到收件箱</div></div></div>
<div class="inbox"><svg viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h6a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/></svg>收件箱 <code>%s</code></div>
<div class="drop" id="drop"><svg class="up" viewBox="0 0 24 24" fill="none" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M7 17a4 4 0 0 1-1-7.9 5 5 0 0 1 9.6-1.1A4.5 4.5 0 0 1 17.5 17"/><path d="M12 12v8M9 15l3-3 3 3"/></svg>
<div class="t1">把文件拖到这里</div><div class="t2">或点击选择文件（可多选）</div></div>
<input id="f" type="file" multiple>
<div id="list"></div>
<button id="b" disabled>上传</button>
<div id="msg"></div>
<div class="foot"><svg viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 8h.01M11 12h1v4h1"/></svg>收件箱里的文件来自局域网其它设备，不要直接双击运行。</div>
</div>
<script>
var drop=document.getElementById('drop'),fi=document.getElementById('f'),btn=document.getElementById('b'),msg=document.getElementById('msg'),list=document.getElementById('list');
var files=[];
function fmt(n){if(n<1024)return n+' B';var u=['KB','MB','GB'],i=-1;while(n>=1024&&i<3){n/=1024;i++}return n.toFixed(n<10?1:0)+' '+u[i]}
function render(){list.innerHTML='';files.forEach(function(f,i){var d=document.createElement('div');d.className='fitem';var fn=document.createElement('span');fn.className='fn';fn.textContent=f.name;var fs=document.createElement('span');fs.className='fs';fs.textContent=fmt(f.size);var fd=document.createElementNS('http://www.w3.org/2000/svg','svg');fd.setAttribute('class','fd');fd.setAttribute('viewBox','0 0 24 24');fd.setAttribute('fill','none');fd.setAttribute('stroke-width','2');fd.innerHTML='<path d="M6 6l12 12M18 6L6 18"/>';fd.onclick=function(ev){ev.stopPropagation();files.splice(i,1);render()};d.appendChild(fn);d.appendChild(fs);d.appendChild(fd);list.appendChild(d)});btn.disabled=!files.length;btn.textContent=files.length?('上传 '+files.length+' 个文件'):'上传'}
function setFiles(fl){files=[].slice.call(fl);render()}
drop.onclick=function(){fi.click()};
fi.onchange=function(){setFiles(fi.files)};
['dragover','dragenter'].forEach(function(e){drop.addEventListener(e,function(ev){ev.preventDefault();drop.classList.add('on')})});
['dragleave','drop'].forEach(function(e){drop.addEventListener(e,function(ev){ev.preventDefault();drop.classList.remove('on')})});
drop.addEventListener('drop',function(ev){if(ev.dataTransfer.files.length)setFiles(ev.dataTransfer.files)});
btn.onclick=function(){if(!files.length)return;var fd=new FormData();files.forEach(function(f){fd.append('file',f,f.name)});btn.disabled=true;msg.className='info';msg.textContent='上传中…';
fetch('/upload',{method:'POST',body:fd}).then(function(r){return r.text().then(function(t){return[r.status,t]})}).then(function(a){if(a[0]<400){msg.className='ok';files=[];fi.value='';render();msg.textContent=a[1]||'上传成功'}else{msg.className='err';msg.textContent=a[1]||('失败 '+a[0]);btn.disabled=false}}).catch(function(e){msg.className='err';msg.textContent='失败：'+e;btn.disabled=false})};
</script></body></html>`

// disposition 文件名可能含非 ASCII，用 RFC 5987 的 filename* 形式。
func disposition(name string) string {
	ascii := make([]rune, 0, len(name))
	for _, r := range name {
		if r < 0x80 && r != '"' && r != '\\' {
			ascii = append(ascii, r)
		} else {
			ascii = append(ascii, '_')
		}
	}
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, string(ascii), encURI(name))
}

func encURI(s string) string {
	const hex = "0123456789ABCDEF"
	b := make([]byte, 0, len(s)*3)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b = append(b, c)
			continue
		}
		b = append(b, '%', hex[c>>4], hex[c&0xF])
	}
	return string(b)
}
