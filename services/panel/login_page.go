package panel

// loginPageHTML 是登录页模板。刻意内嵌在二进制里、不经过前端打包：
//   - 未登录时不需要把 1.5MB 的 SPA 主包先下载下来（那是被拒之门外的流量）；
//   - 登录页与业务前端解耦，改面板密码/入口不会牵动前端构建。
//
// 占位符用 {{.X}}，由 text/template 填充（页面里没有用户可控的 HTML 片段）。
const loginPageHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover" />
<meta name="robots" content="noindex, nofollow" />
<title>{{.Title}} · 登录</title>
<style>
  :root{
    --bg:#070a12; --card:rgba(23,28,42,.72); --line:rgba(255,255,255,.09);
    --text:#e9edf6; --sub:#8d97ad; --brand:#3f7cff; --brand2:#22d3ee;
    --danger:#ff6b6b;
  }
  *{box-sizing:border-box}
  html,body{height:100%}
  body{
    margin:0; color:var(--text);
    font:14px/1.6 -apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif;
    background:
      radial-gradient(900px 520px at 12% -10%, rgba(63,124,255,.30), transparent 60%),
      radial-gradient(760px 480px at 92% 8%, rgba(34,211,238,.18), transparent 62%),
      radial-gradient(700px 700px at 50% 120%, rgba(124,58,237,.20), transparent 60%),
      var(--bg);
    display:flex; align-items:center; justify-content:center; padding:24px;
  }
  /* 细网格底纹：给纯色背景一点纵深，不抢主体 */
  body::before{
    content:""; position:fixed; inset:0; pointer-events:none; opacity:.35;
    background-image:linear-gradient(rgba(255,255,255,.045) 1px, transparent 1px),
                     linear-gradient(90deg, rgba(255,255,255,.045) 1px, transparent 1px);
    background-size:44px 44px;
    mask-image:radial-gradient(circle at 50% 40%, #000 0%, transparent 78%);
  }
  .card{
    position:relative; width:100%; max-width:392px; padding:34px 32px 26px;
    background:var(--card); border:1px solid var(--line); border-radius:18px;
    box-shadow:0 30px 80px -24px rgba(0,0,0,.85), inset 0 1px 0 rgba(255,255,255,.06);
    backdrop-filter:blur(18px) saturate(140%);
    animation:rise .42s cubic-bezier(.2,.9,.2,1) both;
  }
  @keyframes rise{from{opacity:0; transform:translateY(14px) scale(.985)} to{opacity:1; transform:none}}
  .brand{display:flex; align-items:center; gap:12px; margin-bottom:22px}
  .logo{
    width:44px; height:44px; border-radius:13px; flex:none; overflow:hidden;
    display:grid; place-items:center; font-weight:700; font-size:19px; letter-spacing:.5px;
    background:linear-gradient(150deg,var(--brand),#7c3aed 55%,var(--brand2));
    box-shadow:0 10px 26px -10px rgba(63,124,255,.9);
  }
  .logo img{width:100%; height:100%; object-fit:contain; background:#fff}
  .brand h1{margin:0; font-size:17px; font-weight:600; letter-spacing:.2px}
  .brand p{margin:2px 0 0; font-size:12px; color:var(--sub)}
  .err{
    display:none; margin:0 0 16px; padding:9px 12px; border-radius:10px; font-size:13px;
    color:#ffd7d7; background:rgba(255,107,107,.12); border:1px solid rgba(255,107,107,.32);
  }
  .err.show{display:block; animation:shake .3s}
  @keyframes shake{25%{transform:translateX(-5px)} 75%{transform:translateX(5px)}}
  label{display:block; font-size:12px; color:var(--sub); margin:0 0 7px 2px}
  .field{position:relative; margin-bottom:16px}
  .field .ico{
    position:absolute; left:13px; top:50%; transform:translateY(-50%);
    width:16px; height:16px; color:var(--sub); pointer-events:none;
  }
  input[type=text],input[type=password]{
    width:100%; height:44px; padding:0 42px 0 38px; color:var(--text);
    background:rgba(255,255,255,.045); border:1px solid var(--line); border-radius:11px;
    font-size:14px; outline:none; transition:border-color .16s, background .16s, box-shadow .16s;
  }
  input:focus{
    border-color:rgba(63,124,255,.85); background:rgba(63,124,255,.08);
    box-shadow:0 0 0 3px rgba(63,124,255,.16);
  }
  .eye{
    position:absolute; right:6px; top:50%; transform:translateY(-50%);
    width:32px; height:32px; border:0; background:transparent; color:var(--sub);
    cursor:pointer; border-radius:8px; display:grid; place-items:center;
  }
  .eye:hover{color:var(--text); background:rgba(255,255,255,.06)}
  button.submit{
    width:100%; height:44px; margin-top:6px; border:0; border-radius:11px; cursor:pointer;
    color:#fff; font-size:14px; font-weight:600; letter-spacing:.3px;
    background:linear-gradient(135deg,var(--brand),#6d5cff 60%,var(--brand2));
    box-shadow:0 14px 30px -14px rgba(63,124,255,1);
    transition:transform .14s, filter .14s;
  }
  button.submit:hover{filter:brightness(1.07)}
  button.submit:active{transform:translateY(1px)}
  button.submit[disabled]{opacity:.7; cursor:default}
  .foot{
    margin-top:20px; display:flex; justify-content:space-between; gap:10px;
    font-size:11px; color:var(--sub); border-top:1px solid var(--line); padding-top:12px;
  }
  .foot span{white-space:nowrap; overflow:hidden; text-overflow:ellipsis}
</style>
</head>
<body>
  <form class="card" method="post" action="{{.Action}}" autocomplete="off">
    <div class="brand">
      <div class="logo">{{if .Logo}}<img src="{{.Logo}}" alt="" />{{else}}{{.Initial}}{{end}}</div>
      <div>
        <h1>{{.Title}}</h1>
        <p>管理面板 · 请登录后使用</p>
      </div>
    </div>
    {{if .Error}}<p class="err show">{{.Error}}</p>{{end}}
    <label for="u">账号</label>
    <div class="field">
      <svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="12" cy="8" r="3.6"/><path d="M4.8 20c.6-3.6 3.6-5.6 7.2-5.6s6.6 2 7.2 5.6"/></svg>
      <input id="u" name="username" type="text" value="{{.User}}" placeholder="请输入账号" autocomplete="username" required />
    </div>
    <label for="p">密码</label>
    <div class="field">
      <svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="4.5" y="10" width="15" height="10" rx="2.6"/><path d="M8 10V7.4a4 4 0 1 1 8 0V10"/></svg>
      <input id="p" name="password" type="password" placeholder="请输入密码" autocomplete="current-password" required />
      <button class="eye" type="button" id="eye" title="显示/隐藏密码">
        <svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M2.6 12S6 5.8 12 5.8 21.4 12 21.4 12 18 18.2 12 18.2 2.6 12 2.6 12Z"/><circle cx="12" cy="12" r="2.9"/></svg>
      </button>
    </div>
    <input type="hidden" name="next" value="{{.Next}}" />
    <button class="submit" type="submit" id="go">登 录</button>
    <div class="foot">
      <span>{{.Version}}</span>
      <span>{{.Addr}}</span>
    </div>
  </form>
<script>
  (function () {
    var eye = document.getElementById('eye');
    var pwd = document.getElementById('p');
    eye && eye.addEventListener('click', function () {
      pwd.type = pwd.type === 'password' ? 'text' : 'password';
      pwd.focus();
    });
    var form = document.querySelector('form');
    var go = document.getElementById('go');
    form && form.addEventListener('submit', function () {
      go.disabled = true;
      go.textContent = '登录中…';
    });
  })();
</script>
</body>
</html>`
