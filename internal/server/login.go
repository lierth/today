package server

import (
	"html"
	"net/http"
)

// The login page is the one page the React app cannot serve: it has to work
// before there is a session, and it has to work when the frontend bundle was
// never built (a `go run .` straight from a checkout). So it is a string with
// its styles inline — no template, no static file, nothing to forget to ship.
const loginPageHead = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>登录 · Today</title>
<meta name="color-scheme" content="light dark">
<link rel="icon" href="/favicon-light.png" media="(prefers-color-scheme: light)">
<link rel="icon" href="/favicon-dark.png" media="(prefers-color-scheme: dark)">
<link rel="apple-touch-icon" href="/apple-touch-icon.png">
<style>
  :root { --bg:#f4f4f6; --card:#fff; --line:#e3e3e8; --fg:#1c1c21; --muted:#85858d;
          --accent:#4f46e5; --error:#d64545; }
  @media (prefers-color-scheme: dark) {
    :root { --bg:#1a1a1e; --card:#232327; --line:#333339; --fg:#ededf1; --muted:#8b8b94;
            --accent:#a5b4fc; --error:#f28b82; }
  }
  * { box-sizing: border-box; }
  body { margin:0; min-height:100vh; display:grid; place-items:center; padding:24px;
         background:var(--bg); color:var(--fg);
         font:13px/1.4 -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC",
              "Hiragino Sans GB", "Microsoft YaHei", sans-serif; }
  form { width:100%; max-width:300px; padding:22px; border:1px solid var(--line);
         border-radius:14px; background:var(--card); box-shadow:0 1px 2px rgb(20 20 30 / 6%); }
  .mark { display:block; width:24px; height:24px; margin:0 0 10px; }
  h1 { margin:0 0 4px; font-size:17px; font-weight:600; letter-spacing:-0.01em; }
  p  { margin:0 0 16px; color:var(--muted); }
  .error { margin:0 0 12px; color:var(--error); }
  input { width:100%; height:32px; padding:0 10px; font:inherit; color:inherit;
          background:var(--bg); border:1px solid var(--line); border-radius:7px; }
  input:focus-visible { outline:2px solid var(--accent); outline-offset:1px; }
  button { width:100%; height:32px; margin-top:10px; font:inherit; font-weight:560;
           color:var(--bg); background:var(--fg); border:0; border-radius:7px; cursor:pointer; }
  button:hover { filter:brightness(1.08); }
</style>
</head>
<body>
`

const loginPageTail = `</body>
</html>
`

// loginPage renders the form. `status` and `message` come from the POST
// handler, so a wrong password answers on the same page instead of bouncing
// through a redirect (the browser would forget the message).
func loginPage(status int, message string) string {
	note := ""
	if message != "" {
		note = `<p class="error" role="alert">` + html.EscapeString(message) + `</p>`
	}
	_ = status
	return loginPageHead + `<form method="post" action="/login">
  <picture><source srcset="/favicon-dark.png" media="(prefers-color-scheme: dark)">
    <img class="mark" src="/favicon-light.png" width="24" height="24" alt=""></picture>
  <h1>今天</h1>
  <p>输入口令，查看并管理今天。</p>
  ` + note + `<input type="password" name="password" placeholder="口令" autofocus required
         autocomplete="current-password">
  <button type="submit">进入</button>
</form>
` + loginPageTail
}

func (s *Server) renderLogin(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(loginPage(status, message)))
}
