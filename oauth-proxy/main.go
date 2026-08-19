// Decap CMS 的 GitHub OAuth 中繼站。
//
// 為什麼需要它：GitHub 的 OAuth 最後一步要拿 client_secret 去換 access token，
// secret 不能放在瀏覽器裡，所以必須有一個後端代勞。這支程式只做這件事，不存任何資料、
// 沒有資料庫，記憶體用量幾 MB。
//
// 路由：
//
//	GET /auth      把使用者導去 GitHub 授權頁
//	GET /callback  GitHub 帶 code 回來，換成 token 後用 postMessage 丟回 /admin 視窗
//	GET /healthz   健康檢查
//
// 環境變數：
//
//	GITHUB_CLIENT_ID      必填
//	GITHUB_CLIENT_SECRET  必填
//	ALLOWED_ORIGIN        選填但強烈建議，例如 https://peicheng0413.github.io
//	                      設了之後只有這個網站拿得到 token
//	PORT                  預設 8080
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	githubAuthorizeURL = "https://github.com/login/oauth/authorize"
	githubTokenURL     = "https://github.com/login/oauth/access_token"
	stateCookie        = "decap_state"
)

type config struct {
	clientID      string
	clientSecret  string
	allowedOrigin string
}

func main() {
	cfg := config{
		clientID:      os.Getenv("GITHUB_CLIENT_ID"),
		clientSecret:  os.Getenv("GITHUB_CLIENT_SECRET"),
		allowedOrigin: os.Getenv("ALLOWED_ORIGIN"),
	}
	if cfg.clientID == "" || cfg.clientSecret == "" {
		log.Fatal("缺少 GITHUB_CLIENT_ID 或 GITHUB_CLIENT_SECRET")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("GET /auth", cfg.handleAuth)
	mux.HandleFunc("GET /callback", cfg.handleCallback)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Decap CMS OAuth proxy：可用路由 /auth、/callback", http.StatusNotFound)
	})

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("OAuth proxy listening on :%s", port)
	log.Fatal(srv.ListenAndServe())
}

// handleAuth 把使用者送去 GitHub 授權頁，同時把一次性 state 存進 cookie 擋 CSRF。
func (c config) handleAuth(w http.ResponseWriter, r *http.Request) {
	state, err := randomState()
	if err != nil {
		http.Error(w, "產生 state 失敗", http.StatusInternalServerError)
		return
	}

	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "repo,user" // 私有 repo 也能用
	}

	q := url.Values{}
	q.Set("client_id", c.clientID)
	q.Set("redirect_uri", externalBase(r)+"/callback")
	q.Set("scope", scope)
	q.Set("state", state)

	http.SetCookie(w, &http.Cookie{
		Name:     stateCookie,
		Value:    state,
		Path:     "/",
		MaxAge:   600,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, githubAuthorizeURL+"?"+q.Encode(), http.StatusFound)
}

// handleCallback 用 code 換 token，然後把結果 postMessage 回開啟這個視窗的 /admin 頁面。
func (c config) handleCallback(w http.ResponseWriter, r *http.Request) {
	// state cookie 用過即丟
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	saved, err := r.Cookie(stateCookie)
	if code == "" || state == "" || err != nil || saved.Value != state {
		c.render(w, "", "state 不符或缺少授權碼，請重新登入一次")
		return
	}

	token, err := c.exchange(code, externalBase(r)+"/callback")
	if err != nil {
		c.render(w, "", err.Error())
		return
	}
	c.render(w, token, "")
}

func (c config) exchange(code, redirectURI string) (string, error) {
	body, err := json.Marshal(map[string]string{
		"client_id":     c.clientID,
		"client_secret": c.clientSecret,
		"code":          code,
		"redirect_uri":  redirectURI,
	})
	if err != nil {
		return "", fmt.Errorf("組裝請求失敗")
	}

	req, err := http.NewRequest(http.MethodPost, githubTokenURL, strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("建立請求失敗")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "decap-cms-oauth-proxy")

	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("連不上 GitHub：%v", err)
	}
	defer res.Body.Close()

	var payload struct {
		AccessToken      string `json:"access_token"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("GitHub 回應無法解析")
	}
	if payload.AccessToken == "" {
		if payload.ErrorDescription != "" {
			return "", fmt.Errorf("GitHub 拒絕：%s", payload.ErrorDescription)
		}
		return "", fmt.Errorf("GitHub 沒有回傳 access token")
	}
	return payload.AccessToken, nil
}

// Decap 的握手協定：這個彈出視窗先對 opener 喊一聲 "authorizing:github"，
// opener 回話之後，才把結果丟回它的 origin。
var resultPage = template.Must(template.New("result").Parse(`<!doctype html>
<html lang="zh-Hant">
<head><meta charset="utf-8"><title>登入中…</title></head>
<body style="font-family: system-ui, sans-serif; padding: 2rem">
<p>{{ .Message }}</p>
<script>
  (function () {
    var payload = {{ .Payload }};
    var allowed = {{ .Allowed }};
    function send(origin) {
      if (allowed && origin !== allowed) return;
      window.opener.postMessage(payload, origin);
    }
    window.addEventListener('message', function (e) { send(e.origin); }, false);
    window.opener.postMessage('authorizing:github', allowed || '*');
  })();
</script>
</body>
</html>`))

func (c config) render(w http.ResponseWriter, token, errMsg string) {
	var payload, message string
	if token != "" {
		data, _ := json.Marshal(map[string]string{"token": token, "provider": "github"})
		payload = "authorization:github:success:" + string(data)
		message = "登入成功，可以關掉這個視窗了。"
	} else {
		data, _ := json.Marshal(map[string]string{"message": errMsg})
		payload = "authorization:github:error:" + string(data)
		message = "登入失敗：" + errMsg
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = resultPage.Execute(w, struct {
		Payload template.JS
		Allowed template.JS
		Message string
	}{
		Payload: template.JS(mustJSON(payload)),
		Allowed: template.JS(mustJSON(c.allowedOrigin)),
		Message: message,
	})
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// externalBase 還原使用者實際看到的網址：fly 走 proxy，本機 TLS 資訊在 header 裡。
func externalBase(r *http.Request) string {
	scheme := "https"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	} else if r.TLS == nil && strings.HasPrefix(r.Host, "localhost") {
		scheme = "http"
	}
	host := r.Host
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host = fwd
	}
	return scheme + "://" + host
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
