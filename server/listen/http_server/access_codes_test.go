package http_server

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jinnrry/pmail/config"
	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/dto/parsemail"
	"github.com/Jinnrry/pmail/models"
	"github.com/Jinnrry/pmail/services/accesscode"
	"github.com/Jinnrry/pmail/session"
	"github.com/Jinnrry/pmail/utils/context"
	"github.com/Jinnrry/pmail/utils/password"
	"github.com/alexedwards/scs/v2"
	"xorm.io/xorm"
)

type codeFixture struct {
	t              *testing.T
	engine         *xorm.Engine
	handler        http.Handler
	cookie         *http.Cookie
	owner          models.User
	ownID, otherID int
}

func newCodeFixture(t *testing.T, seedSession ...bool) *codeFixture {
	t.Helper()
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	oldDB, oldConfig, oldInit, oldSession := db.Instance, config.Instance, config.IsInit, session.Instance
	t.Cleanup(func() {
		engine.Close()
		db.Instance, config.Instance, config.IsInit, session.Instance = oldDB, oldConfig, oldInit, oldSession
	})
	db.Instance = engine
	config.IsInit = true
	config.Instance = &config.Config{Domain: "example.com", Domains: []string{"example.com"}, WebOrigin: "https://mail.example.com"}
	session.Instance = scs.New()
	if err = engine.Sync2(&models.User{}, &models.Email{}, &models.UserEmail{}, &models.Group{}, &models.Rule{}, &models.AccessCode{}, &models.EmailAlias{}); err != nil {
		t.Fatal(err)
	}
	f := &codeFixture{t: t, engine: engine}
	f.owner = models.User{Account: "owner", Name: "Owner", Password: password.Encode("test-password"), IsAdmin: 1}
	if _, err = engine.Insert(&f.owner); err != nil {
		t.Fatal(err)
	}
	other := models.User{Account: "other", Name: "Other", Password: password.Encode("other-password")}
	engine.Insert(&other)
	atts, _ := json.Marshal([]parsemail.Attachment{{Filename: "test.txt", ContentType: "text/plain", Content: []byte("fixture attachment"), ContentID: "part1"}, {Filename: "empty.txt", ContentType: "text/plain", Content: []byte{}}})
	for i, uid := range []int{f.owner.ID, other.ID} {
		m := models.Email{Subject: fmt.Sprintf("private-%d", uid), Text: sql.NullString{String: "synthetic body", Valid: true}, Attachments: string(atts)}
		if _, err = engine.Insert(&m); err != nil {
			t.Fatal(err)
		}
		if _, err = engine.Insert(&models.UserEmail{UserID: uid, EmailID: m.Id}); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			f.ownID = m.Id
		} else {
			f.otherID = m.Id
		}
	}
	mux := http.NewServeMux()
	router(mux)
	f.handler = session.Instance.LoadAndSave(mux)
	var w *httptest.ResponseRecorder
	if len(seedSession) > 0 && seedSession[0] {
		// 与实际登录一致地建立测试会话；路由仍走真实会话校验，避免无关的全局限流影响测试。
		seed := session.Instance.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			encoded, _ := json.Marshal(f.owner)
			digest := sha256.Sum256([]byte(f.owner.Password))
			session.Instance.Put(r.Context(), "user", string(encoded))
			session.Instance.Put(r.Context(), "credential", fmt.Sprintf("%x", digest))
		}))
		w = httptest.NewRecorder()
		seed.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	} else {
		w = f.call("POST", "/api/login", `{"account":"owner","password":"test-password"}`, "", nil, true)
	}
	if len(w.Result().Cookies()) == 0 {
		t.Fatalf("session: %s", w.Body.String())
	}
	f.cookie = w.Result().Cookies()[0]

	return f
}
func (f *codeFixture) call(method, path, body, token string, cookie *http.Cookie, origin bool) *httptest.ResponseRecorder {
	f.t.Helper()
	r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if origin {
		r.Header.Set("Origin", config.Instance.WebOrigin)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}
func (f *codeFixture) success(w *httptest.ResponseRecorder) json.RawMessage {
	f.t.Helper()
	var r struct {
		ErrorNo int             `json:"errorNo"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil || w.Code != 200 || r.ErrorNo != 0 {
		f.t.Fatalf("expected success, HTTP %d: %s", w.Code, w.Body.String())
	}
	return r.Data
}
func (f *codeFixture) create() (string, int) {
	f.t.Helper()
	data := f.success(f.call("POST", "/api/settings/access_codes/create", `{"name":"test agent","expires_at":0}`, "", f.cookie, true))
	var r struct {
		Token string `json:"token"`
		ID    int    `json:"id"`
	}
	json.Unmarshal(data, &r)
	if r.Token == "" || r.ID == 0 {
		f.t.Fatalf("missing one-time token: %s", data)
	}
	return r.Token, r.ID
}
func mustDeny(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != 401 && w.Code != 403 {
		t.Fatalf("expected HTTP denial, got %d: %s", w.Code, w.Body.String())
	}
}
func TestAccessCodeReadOnlyAndOwnership(t *testing.T) {
	f := newCodeFixture(t)
	token, _ := f.create()
	f.success(f.call("GET", "/api/group", "", token, nil, false))
	data := f.success(f.call("POST", "/api/email/list", `{"page_size":15,"current_page":1}`, token, nil, false))
	if !strings.Contains(string(data), fmt.Sprintf("private-%d", f.owner.ID)) || strings.Contains(string(data), fmt.Sprintf("private-%d", f.owner.ID+1)) {
		t.Fatalf("unexpected list: %s", data)
	}
	f.success(f.call("POST", "/api/email/detail", fmt.Sprintf(`{"id":%d}`, f.ownID), token, nil, false))
	var ue models.UserEmail
	f.engine.Where("email_id=? AND user_id=?", f.ownID, f.owner.ID).Get(&ue)
	if ue.IsRead != 0 {
		t.Fatal("read token marked email read")
	}
	w := f.call("GET", fmt.Sprintf("/attachments/download/%d/0", f.ownID), "", token, nil, false)
	if w.Body.String() != "fixture attachment" {
		t.Fatalf("attachment: %s", w.Body.String())
	}
	w = f.call("POST", "/api/email/detail", fmt.Sprintf(`{"id":%d}`, f.otherID), token, nil, false)
	if strings.Contains(w.Body.String(), "synthetic body") || strings.Contains(w.Body.String(), `"errorNo":0`) {
		t.Fatal("cross-user body leaked")
	}
	w = f.call("GET", fmt.Sprintf("/attachments/download/%d/0", f.otherID), "", token, nil, false)
	if strings.Contains(w.Body.String(), "fixture attachment") || strings.Contains(w.Body.String(), `"errorNo":0`) {
		t.Fatal("cross-user attachment leaked")
	}
	for _, path := range []string{"/api/email/send", "/api/email/del", "/api/email/read", "/api/email/move", "/api/group/add", "/api/group/del", "/api/logout", "/api/login", "/api/user/create", "/api/user/edit", "/api/user/info", "/api/user/list", "/api/rule/get", "/api/rule/add", "/api/rule/update", "/api/rule/del", "/api/plugin/list", "/api/settings/modify_password", "/api/settings/access_codes/list", "/api/settings/access_codes/create", "/api/settings/access_codes/revoke"} {
		mustDeny(t, f.call("POST", path, `{}`, token, f.cookie, true))
	}
	for _, path := range []string{fmt.Sprintf("/attachments/download/%d/-1", f.ownID), fmt.Sprintf("/attachments/download/%d/999", f.ownID), "/attachments/download/0/0"} {
		w = f.call("GET", path, "", token, nil, false)
		if strings.Contains(w.Body.String(), "fixture attachment") {
			t.Fatal("invalid index accepted")
		}
	}
	mustDeny(t, f.call("PUT", "/api/email/detail", `{}`, token, nil, false))
	f.success(f.call("POST", "/api/email/detail", fmt.Sprintf(`{"id":%d}`, f.ownID), "", f.cookie, true))
	f.engine.Where("id=?", ue.ID).Get(&ue)
	if ue.IsRead != 1 {
		t.Fatal("web detail no longer marks read")
	}
}
func TestAccessCodeLifecycleAndSessionIsolation(t *testing.T) {
	f := newCodeFixture(t)
	token, id := f.create()
	data := f.success(f.call("POST", "/api/settings/access_codes/list", `{}`, "", f.cookie, true))
	if strings.Contains(string(data), token) || strings.Contains(string(data), "digest") {
		t.Fatal("secret in list")
	}
	mustDeny(t, f.call("GET", "/api/group", "", "invalid", f.cookie, false))
	mustDeny(t, f.call("POST", "/api/settings/access_codes/create", `{"name":"bad"}`, "", f.cookie, false))
	f.success(f.call("POST", "/api/settings/access_codes/revoke", fmt.Sprintf(`{"id":%d}`, id), "", f.cookie, true))
	mustDeny(t, f.call("GET", "/api/group", "", token, nil, false))
	token, _ = f.create()
	f.engine.ID(f.owner.ID).Cols("disabled").Update(&models.User{Disabled: 1})
	mustDeny(t, f.call("GET", "/api/group", "", token, nil, false))
	f.engine.ID(f.owner.ID).Cols("disabled").Update(&models.User{Disabled: 0})
	f.success(f.call("POST", "/api/settings/modify_password", `{"password":"changed-password"}`, "", f.cookie, true))
	mustDeny(t, f.call("GET", "/api/group", "", token, nil, false))
}

func TestAccessCodeMalformedRequestsAndDefaultDeny(t *testing.T) {
	f := newCodeFixture(t)
	token, _ := f.create()
	for _, value := range []string{"", "Basic abc", "Bearer", "Bearer bad", "Bearer " + token + " extra"} {
		r := httptest.NewRequest("GET", "/api/group", nil)
		r.Header.Set("Authorization", value)
		r.AddCookie(f.cookie)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		mustDeny(t, w)
	}
	r := httptest.NewRequest("GET", "/api/group", nil)
	r.Header.Add("Authorization", "Bearer "+token)
	r.Header.Add("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	mustDeny(t, w)
	r = httptest.NewRequest("GET", "/api/group", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Origin", "https://evil.example")
	w = httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	mustDeny(t, w)
	r = httptest.NewRequest("POST", "/api/email/list", strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "text/plain")
	w = httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	mustDeny(t, w)
	called := false
	h := contextIterceptor(func(_ *context.Context, w http.ResponseWriter, r *http.Request) { called = true })
	r = httptest.NewRequest("POST", "/api/future/write", strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	h(w, r)
	mustDeny(t, w)
	if called {
		t.Fatal("future route allowed")
	}
	for _, body := range []string{`{"page_size":-1}`, `{"page_size":10000}`, `{"current_page":-1}`, `{"page_size":100,"current_page":9223372036854775807}`} {
		w = f.call("POST", "/api/email/list", body, token, nil, false)
		var res struct {
			ErrorNo int `json:"errorNo"`
		}
		json.Unmarshal(w.Body.Bytes(), &res)
		if res.ErrorNo == 0 {
			t.Fatalf("invalid pagination allowed: %s", body)
		}
	}
	for _, body := range []string{`{"name":""}`, `{"name":"bad","scope":"mail.write"}`, `{"name":"bad"} {}`, `{"name":"bad","expires_at":-1}`} {
		w = f.call("POST", "/api/settings/access_codes/create", body, "", f.cookie, true)
		var res struct {
			ErrorNo int `json:"errorNo"`
		}
		json.Unmarshal(w.Body.Bytes(), &res)
		if res.ErrorNo == 0 {
			t.Fatalf("invalid code params allowed: %s", body)
		}
	}
	// 无需Cookie或Origin，读取不得创建新网页会话；附件也可通过CID读取。
	w = f.call("GET", fmt.Sprintf("/attachments/%d/part1", f.ownID), "", token, nil, false)
	if w.Body.String() != "fixture attachment" || len(w.Result().Cookies()) != 0 {
		t.Fatal("unexpected CID or session behavior")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("token response may be cached")
	}
}

func TestNormalUserCanManageOnlyOwnCodes(t *testing.T) {
	f := newCodeFixture(t)
	f.engine.ID(f.owner.ID).Cols("is_admin").Update(&models.User{IsAdmin: 0})
	token, _ := f.create()
	f.success(f.call("POST", "/api/email/detail", fmt.Sprintf(`{"id":%d}`, f.ownID), token, nil, false))
	// 即使知道其他用户的授权码ID，也不能在网页登录会话中撤销。
	var other models.User
	f.engine.ID(f.owner.ID + 1).Get(&other)
	version := fmt.Sprintf("%x", sha256.Sum256([]byte(other.Password)))
	otherCode, _, err := accesscode.Create(other.ID, "other agent", 0, version)
	if err != nil {
		t.Fatal(err)
	}
	w := f.call("POST", "/api/settings/access_codes/revoke", fmt.Sprintf(`{"id":%d}`, otherCode.ID), "", f.cookie, true)
	var result struct {
		ErrorNo int `json:"errorNo"`
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	if result.ErrorNo == 0 {
		t.Fatal("cross-user revoke via session")
	}
	data := f.success(f.call("POST", "/api/settings/access_codes/list", `{}`, "", f.cookie, true))
	if strings.Contains(string(data), "other agent") {
		t.Fatal("other code metadata leaked")
	}
}

func TestAccessCodeEmptyAttachmentAndBodyLimit(t *testing.T) {
	f := newCodeFixture(t)
	token, _ := f.create()
	w := f.call("GET", fmt.Sprintf("/attachments/download/%d/1", f.ownID), "", token, nil, false)
	if w.Code != 200 || w.Body.Len() != 0 || !strings.Contains(w.Header().Get("Content-Disposition"), "empty.txt") {
		t.Fatalf("zero-byte attachment failed: %s", w.Body.String())
	}
	w = f.call("POST", "/api/email/list", `{"keyword":"`+strings.Repeat("a", 70<<10)+`"}`, token, nil, false)
	if strings.Contains(w.Body.String(), `"errorNo":0`) {
		t.Fatal("oversized body accepted")
	}
}
