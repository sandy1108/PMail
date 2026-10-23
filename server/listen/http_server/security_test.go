package http_server

import (
	"bytes"
	"encoding/json"
	"github.com/Jinnrry/pmail/config"
	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/models"
	"github.com/Jinnrry/pmail/session"
	"github.com/Jinnrry/pmail/utils/password"
	"github.com/alexedwards/scs/v2"
	_ "modernc.org/sqlite"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"xorm.io/xorm"
)

func TestWriteOriginAndReadRoutes(t *testing.T) {
	old := config.Instance
	defer func() { config.Instance = old }()
	config.Instance = &config.Config{WebOrigin: "https://mail.example.com:8443"}
	for _, tc := range []struct {
		method, path, origin, ctype string
		want                        bool
	}{
		{"POST", "/api/login", "https://mail.example.com:8443", "application/json", true},
		{"POST", "/api/login", "https://evil.test", "application/json", false},
		{"POST", "/api/login", "", "application/json", false},
		{"POST", "/api/login", "https://mail.example.com:8443", "text/plain", false},
		{"GET", "/api/user/create", "", "", false},
		{"GET", "/api/group", "", "", true},
		{"GET", "/api/plugin/list", "", "", true},
	} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Content-Type", tc.ctype)
		if got := validWriteOrigin(r); got != tc.want {
			t.Errorf("%s %s: %v", tc.method, tc.path, got)
		}
	}
	if _, err := getLoginInfoByToken("admin:anything:9999999999"); err == nil {
		t.Fatal("legacy token accepted")
	}
}

func TestSessionRevocationAndRoles(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	if err = engine.Sync2(&models.User{}); err != nil {
		t.Fatal(err)
	}
	oldDB, oldConfig, oldInit, oldSession := db.Instance, config.Instance, config.IsInit, session.Instance
	defer func() {
		db.Instance = oldDB
		config.Instance = oldConfig
		config.IsInit = oldInit
		session.Instance = oldSession
	}()
	db.Instance = engine
	config.IsInit = true
	config.Instance = &config.Config{Domain: "example.com", Domains: []string{"example.com"}, WebOrigin: "https://mail.example.com:8443"}
	session.Instance = scs.New()
	session.Instance.Cookie.Secure = true
	session.Instance.Cookie.SameSite = http.SameSiteStrictMode
	user := models.User{Account: "test", Name: "test", Password: password.Encode("old-password")}
	engine.Insert(&user)
	mux := http.NewServeMux()
	router(mux)
	handler := session.Instance.LoadAndSave(mux)
	call := func(path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", config.Instance.WebOrigin)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	login := func() *http.Cookie {
		w := call("/api/login", `{"account":"test@example.com","password":"old-password"}`, nil)
		cookies := w.Result().Cookies()
		if len(cookies) == 0 {
			t.Fatalf("login failed: %s", w.Body.String())
		}
		if !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
			t.Fatal("unsafe cookie")
		}
		return cookies[0]
	}
	c1, c2 := login(), login()
	if c1.Value == c2.Value {
		t.Fatal("session token reused")
	}
	w := call("/api/user/create", `{"account":"blocked","username":"blocked","password":"unused"}`, c1)
	if !strings.Contains(w.Body.String(), "No Access Privileges") {
		t.Fatalf("normal user accessed admin API: %s", w.Body.String())
	}
	call("/api/settings/modify_password", `{"password":"new-password"}`, c1)
	w = call("/api/user/info", `{}`, c2)
	var res map[string]any
	json.Unmarshal(w.Body.Bytes(), &res)
	if res["errorNo"] != float64(403) {
		t.Fatalf("old session survived password change: %s", w.Body.String())
	}
	w = call("/api/login", `{"account":"test","password":"new-password"}`, nil)
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("new password failed")
	}
	engine.ID(user.ID).Cols("disabled").Update(&models.User{Disabled: 1})
	w = call("/api/user/info", `{}`, cookies[0])
	json.Unmarshal(w.Body.Bytes(), &res)
	if res["errorNo"] != float64(403) {
		t.Fatalf("disabled user session survived: %s", w.Body.String())
	}
}
