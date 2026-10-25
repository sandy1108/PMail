package http_server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/Jinnrry/pmail/models"
	"github.com/Jinnrry/pmail/services/accesscode"
	"testing"
)

// 用真实会话和路由验证用途授权，旧版本会拒绝permissions字段。
func TestAccessCodeProtocolPurposesAndHTTPWrites(t *testing.T) {
	f := newCodeFixture(t, true)
	raw := f.success(f.call("POST", "/api/settings/access_codes/create", `{"name":"mail client","permissions":["imap","pop3"]}`, "", f.cookie, true))
	var code struct {
		ID    int64  `json:"id"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &code); err != nil || code.ID == 0 {
		t.Fatal("missing code", err)
	}
	mustDeny(t, f.call("POST", "/api/email/list", `{}`, code.Token, nil, false))
	f.success(f.call("POST", "/api/settings/access_codes/update", fmt.Sprintf(`{"id":%d,"permissions":["http.write","imap","pop3"]}`, code.ID), "", f.cookie, true))
	f.success(f.call("POST", "/api/email/read", fmt.Sprintf(`{"ids":[%d]}`, f.ownID), code.Token, nil, false))
	var state models.UserEmail
	f.engine.Where("user_id=? AND email_id=?", f.owner.ID, f.ownID).Get(&state)
	if state.IsRead != 1 {
		t.Fatal("write code did not mark read")
	}
	for _, path := range []string{"/api/email/send", "/api/user/create", "/api/settings/access_codes/update", "/api/settings/aliases/create", "/api/settings/modify_password"} {
		mustDeny(t, f.call("POST", path, `{}`, code.Token, f.cookie, true))
	}
	f.success(f.call("POST", "/api/settings/access_codes/update", fmt.Sprintf(`{"id":%d,"permissions":["http.read"]}`, code.ID), "", f.cookie, true))
	mustDeny(t, f.call("POST", "/api/email/read", `{"ids":[1]}`, code.Token, nil, false))
}

func TestHTTPWriteCodeCannotModifyForeignFolders(t *testing.T) {
	f := newCodeFixture(t, true)
	version := fmt.Sprintf("%x", sha256.Sum256([]byte(f.owner.Password)))
	_, token, err := accesscode.CreateWithPermissions(f.owner.ID, "writer", 0, version, []string{"http.write"})
	if err != nil {
		t.Fatal(err)
	}
	var other models.User
	f.engine.Where("account=?", "other").Get(&other)
	foreign := models.Group{Name: "private", FullPath: "private", UserId: other.ID}
	f.engine.Insert(&foreign)
	f.engine.Where("user_id=?", other.ID).Cols("group_id").Update(&models.UserEmail{GroupId: foreign.ID})
	f.call("POST", "/api/group/del", fmt.Sprintf(`{"id":%d}`, foreign.ID), token, nil, false)
	var relation models.UserEmail
	f.engine.Where("user_id=?", other.ID).Get(&relation)
	if relation.GroupId != foreign.ID {
		t.Fatal("foreign folder deletion moved another user's mail")
	}
	f.call("POST", "/api/group/add", fmt.Sprintf(`{"name":"child","parent_id":%d}`, foreign.ID), token, nil, false)
	n, _ := f.engine.Where("user_id=? AND parent_id=?", f.owner.ID, foreign.ID).Count(&models.Group{})
	if n != 0 {
		t.Fatal("foreign parent accepted")
	}
	f.call("POST", "/api/email/move", fmt.Sprintf(`{"ids":[%d],"group_id":%d}`, f.ownID, foreign.ID), token, nil, false)
	var own models.UserEmail
	f.engine.Where("user_id=?", f.owner.ID).Get(&own)
	if own.GroupId != 0 {
		t.Fatal("mail moved into foreign folder")
	}
	for _, path := range []string{"/api/group/add", "/api/group/del", "/api/email/read", "/api/email/move", "/api/email/del"} {
		for _, body := range []string{"null", "{", "{}"} {
			f.call("POST", path, body, token, nil, false)
		}
	}
}

func TestSMTPCodeCannotUseHTTPAndClientGuideIsSessionOnly(t *testing.T) {
	f := newCodeFixture(t, true)
	data := f.success(f.call("POST", "/api/settings/access_codes/create", `{"name":"SMTP","permissions":["smtp"]}`, "", f.cookie, true))
	var code struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &code); err != nil || code.Token == "" {
		t.Fatal("missing SMTP token", err)
	}
	for _, path := range []string{"/api/user/info", "/api/email/list", "/api/email/send"} {
		mustDeny(t, f.call("POST", path, `{}`, code.Token, nil, false))
	}
	data = f.success(f.call("POST", "/api/user/info", `{}`, "", f.cookie, true))
	var info struct {
		ClientSettings map[string]any `json:"client_settings"`
	}
	if err := json.Unmarshal(data, &info); err != nil || info.ClientSettings["username"] == nil {
		t.Fatal("missing guide settings", err)
	}
	for _, key := range []string{"password", "dbDSN", "SSLPrivateKeyPath", "proxyTrustedIPs"} {
		if _, ok := info.ClientSettings[key]; ok {
			t.Fatal("private configuration leaked")
		}
	}
}
