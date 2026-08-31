package cbl

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type loginSessions struct {
	mu       sync.Mutex
	sessions map[string]DeviceCode
}

func newLoginSessions() *loginSessions {
	return &loginSessions{sessions: map[string]DeviceCode{}}
}

func (s *loginSessions) put(device DeviceCode) string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	id := hex.EncodeToString(b[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = device
	return id
}

func (s *loginSessions) take(id string) (DeviceCode, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	device, ok := s.sessions[id]
	if ok {
		delete(s.sessions, id)
	}
	return device, ok
}

func registerUIHandlers(mux *http.ServeMux, state *cache, opts Options) {
	logins := newLoginSessions()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		handleDashboard(w, r)
	})
	mux.HandleFunc("/dashboard", handleDashboard)
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, dashboardPayloadAll(state.getAll()))
	})
	mux.HandleFunc("/api/login/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		client, err := newHTTPClient(loadProxy(opts))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		device, err := RequestDeviceCode(ctx, client, LoginOptions{Proxy: loadProxy(opts)})
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":               true,
			"id":               logins.put(device),
			"verification_url": device.VerificationURL,
			"user_code":        device.UserCode,
		})
	})
	mux.HandleFunc("/api/login/complete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		device, ok := logins.take(id)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "login session not found or already used"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 16*time.Minute)
		defer cancel()
		proxy := loadProxy(opts)
		client, err := newHTTPClient(proxy)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		creds, err := CompleteDeviceLogin(ctx, client, LoginOptions{Proxy: proxy}, device)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		path, err := saveAccountCredentials(creds)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if _, err := os.Stat(defaultCBLAuthFile()); os.IsNotExist(err) {
			_ = saveCredentials(defaultCBLAuthFile(), creds)
		}
		if err := saveUserProxy(resolvedProxy(proxy)); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			snaps, err := LoadAll(ctx, opts)
			state.setAll(snaps, err)
		}()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "auth_file": path})
	})
}

func handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = dashboardTemplate.Execute(w, nil)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func dashboardPayload(snap UsageSnapshot, err error) map[string]any {
	return dashboardPayloadAll([]UsageSnapshot{snap}, err)
}

func dashboardPayloadAll(snaps []UsageSnapshot, err error) map[string]any {
	payload := map[string]any{"ok": err == nil}
	if err != nil {
		payload["error"] = err.Error()
		return payload
	}
	if len(snaps) == 0 {
		payload["ok"] = false
		payload["error"] = "no usage data available"
		return payload
	}
	snap := snaps[0]
	payload["text"] = snap.summaryLine()
	payload["tooltip"] = snap.tooltip()
	payload["class"] = snap.waybarClass()
	payload["plan"] = snap.PlanType
	payload["account_id"] = snap.AccountID
	payload["profile"] = snap.ProfileName
	payload["fetched_at"] = snap.FetchedAt
	payload["windows"] = usageCards(snap)
	payload["credits"] = creditCard(snap)
	accounts := make([]map[string]any, 0, len(snaps))
	for _, account := range snaps {
		accounts = append(accounts, accountCard(account))
	}
	payload["accounts"] = accounts
	return payload
}

func accountCard(snap UsageSnapshot) map[string]any {
	return map[string]any{
		"account_id": snap.AccountID,
		"plan":       snap.PlanType,
		"class":      snap.waybarClass(),
		"windows":    usageCards(snap),
		"credits":    creditCard(snap),
	}
}

func usageCards(snap UsageSnapshot) []map[string]any {
	cards := []map[string]any{}
	add := func(title string, win *UsageWindow) {
		if win == nil {
			return
		}
		card := map[string]any{
			"title":     title,
			"remaining": win.Remaining(),
			"used":      win.UsedPercent,
		}
		if win.ResetAt != nil {
			card["reset"] = win.ResetAt.Local().Format("Mon, 02 Jan 15:04")
		}
		cards = append(cards, card)
	}
	add("Лимит использования 5 часов", snap.PrimaryWindow)
	add("Недельный лимит использования", snap.SecondaryWindow)
	for _, extra := range snap.AdditionalRates {
		win := extra.Window
		add(extra.Name, &win)
	}
	return cards
}

func creditCard(snap UsageSnapshot) map[string]any {
	if snap.IndividualLimit != nil && snap.IndividualLimit.Limit > 0 {
		return map[string]any{
			"text": fmt.Sprintf("%.0f / %.0f", snap.IndividualLimit.Remaining, snap.IndividualLimit.Limit),
			"used": 100 - int(snap.IndividualLimit.RemainingPercent),
		}
	}
	if snap.CreditsBalance != nil {
		return map[string]any{"text": fmt.Sprintf("%.2f", *snap.CreditsBalance), "used": 0}
	}
	return map[string]any{"text": "—", "used": 0}
}

var dashboardTemplate = template.Must(template.New("dashboard").Parse(`<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>CBL Balance</title>
<style>
:root{color-scheme:dark;--bg:#070707;--card:#202020;--line:#3a3a3a;--muted:#a8a8b3;--text:#f4f4f5;--green:#2ed36f;--blue:#2f80ed;--danger:#ff5c5c}
*{box-sizing:border-box}body{margin:0;background:radial-gradient(circle at 70% 0,#1b1b28 0,#070707 34rem);color:var(--text);font:15px/1.45 system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}.wrap{max-width:1120px;margin:0 auto;padding:28px 20px 40px}.top{display:flex;align-items:flex-start;justify-content:space-between;gap:18px;margin-bottom:24px}h1{font-size:24px;margin:0 0 4px}.sub{color:var(--muted)}.actions{display:flex;gap:10px;flex-wrap:wrap}button,a.btn{border:0;border-radius:12px;background:#2d2d31;color:#fff;padding:10px 14px;text-decoration:none;cursor:pointer;font-weight:650}.primary{background:var(--blue)}.grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:24px}.card{background:var(--card);border:1px solid #2a2a2a;border-radius:22px;padding:24px;box-shadow:0 18px 50px rgba(0,0,0,.28)}.label{color:var(--muted);font-weight:650;margin-bottom:8px}.big{font-size:27px;font-weight:760;margin-bottom:16px}.bar{height:12px;background:#e8e8ec;border-radius:999px;overflow:hidden}.fill{height:100%;width:0;background:linear-gradient(90deg,#26c767,var(--green));border-radius:999px;transition:width .35s ease}.meta{color:var(--muted);font-size:13px;margin-top:14px}.credits{grid-column:span 1}.wide{grid-column:1/-1}.error{border-color:#743636;color:#ffdede}.login{display:none;margin-top:24px}.code{font-size:34px;letter-spacing:.08em;font-weight:800;padding:14px 16px;border-radius:14px;background:#111;display:inline-block}.small{font-size:13px;color:var(--muted)}@media(max-width:760px){.grid{grid-template-columns:1fr}.top{display:block}.actions{margin-top:14px}.credits{grid-column:auto}}
</style>
</head>
<body><main class="wrap"><section class="top"><div><h1>Баланс</h1><div class="sub">CBL показывает лимиты Codex и умеет входить без терминала.</div></div><div class="actions"><button class="primary" onclick="startLogin()">Add Account / Login</button><a class="btn" href="https://status.openai.com/" target="_blank" rel="noreferrer">Status Page</a><button onclick="refresh()">Refresh</button></div></section><section id="login" class="card login"></section><section id="content" class="grid"><article class="card">Загрузка…</article></section></main><script>
const el=id=>document.getElementById(id);
function pct(n){return Math.max(0,Math.min(100,Number(n)||0))}
function card(c){return '<article class="card"><div class="label">'+esc(c.title)+'</div><div class="big">'+pct(c.remaining)+' % осталось</div><div class="bar"><div class="fill" style="width:'+pct(c.remaining)+'%"></div></div><div class="meta">'+(c.reset?'Сброс: '+esc(c.reset):' ')+'</div></article>'}
function esc(s){return String(s??'').replace(/[&<>"']/g,m=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[m]))}
// All dynamic values are escaped before assigning generated markup to innerHTML.
async function refresh(){const r=await fetch('/api/status',{cache:'no-store'});const d=await r.json();if(!d.ok){el('content').innerHTML='<article class="card error wide"><b>CBL unavailable</b><div class="meta">'+esc(d.error)+'</div></article>';return}let html=(d.windows||[]).map(card).join('');html+='<article class="card credits"><div class="label">Осталось кредитов</div><div class="big">'+esc(d.credits?.text??'0')+'</div><div class="bar"><div class="fill" style="width:'+(100-pct(d.credits?.used))+'%"></div></div><div class="meta">plan: '+esc(d.plan||'unknown')+' · account: '+esc(d.account_id||'—')+'</div></article>';html+='<article class="card wide"><div class="label">Summary</div><div>'+esc(d.text)+'</div><div class="meta">'+esc(d.tooltip).replaceAll('\n','<br>')+'</div></article>';el('content').innerHTML=html}
async function startLogin(){const box=el('login');box.style.display='block';box.innerHTML='Requesting device code…';const r=await fetch('/api/login/start',{method:'POST'});const d=await r.json();if(!d.ok){box.innerHTML='<b>Login failed</b><div class="meta">'+esc(d.error)+'</div>';return}box.innerHTML='<div class="label">OpenAI device login</div><div class="small">Открой страницу и введи код:</div><p><a class="btn primary" href="'+esc(d.verification_url)+'" target="_blank" rel="noreferrer">Open login page</a></p><div class="code">'+esc(d.user_code)+'</div><p class="small">После подтверждения нажми кнопку ниже — CBL сохранит auth без терминала.</p><button class="primary" onclick="completeLogin(\''+esc(d.id)+'\')">I confirmed login</button>'}
async function completeLogin(id){const box=el('login');box.innerHTML='Waiting for OpenAI confirmation…';const r=await fetch('/api/login/complete?id='+encodeURIComponent(id),{method:'POST'});const d=await r.json();if(!d.ok){box.innerHTML='<b>Login failed</b><div class="meta">'+esc(d.error)+'</div>';return}box.innerHTML='<b>Login saved.</b><div class="meta">Status will refresh now.</div>';setTimeout(refresh,1500)}
refresh();if(location.hash==='#login'){startLogin()}setInterval(refresh,60000);
</script></body></html>`))
