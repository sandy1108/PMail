package accesscode

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/models"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

func TestLifecyclePersistenceAndSecretStorage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codes.db")
	engine, err := xorm.NewEngine("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	old := db.Instance
	db.Instance = engine
	t.Cleanup(func() { db.Instance = old; engine.Close() })
	if err = engine.Sync2(&models.User{}, &models.AccessCode{}); err != nil {
		t.Fatal(err)
	}
	user := models.User{Account: "reader", Name: "Reader", Password: "synthetic-credential-version"}
	engine.Insert(&user)
	other := models.User{Account: "other", Name: "Other", Password: "other-version"}
	engine.Insert(&other)
	code, token, err := Create(user.ID, " agent ", time.Now().Add(time.Hour).Unix(), digest(user.Password))
	if err != nil {
		t.Fatal(err)
	}
	if code.Name != "agent" || code.Scope != ReadScope || code.Digest == token || len(code.Digest) != 64 {
		t.Fatal("invalid token storage")
	}
	var stored models.AccessCode
	engine.ID(code.ID).Get(&stored)
	encoded, _ := json.Marshal(stored)
	if strings.Contains(string(encoded), token) || strings.Contains(string(encoded), stored.Digest) || strings.Contains(string(encoded), stored.CredentialDigest) {
		t.Fatal("model JSON leaks digest")
	}
	if stored.Digest != digest(token) {
		t.Fatal("digest not persisted")
	}
	_, second, err := Create(user.ID, "another", 0, digest(user.Password))
	if err != nil || second == token {
		t.Fatal("tokens not independent")
	}
	if _, err = Authenticate(token); err != nil {
		t.Fatal(err)
	}
	if _, err = Authenticate(code.Digest); err == nil {
		t.Fatal("digest usable as token")
	}
	if err = Revoke(other.ID, code.ID); err == nil {
		t.Fatal("cross-user revocation succeeded")
	}
	info, err := List(other.ID)
	if err != nil || len(info) != 0 {
		t.Fatal("cross-user listing")
	}
	// 关闭并重开数据库，授权码仍可用；不是进程内临时凭据。
	engine.Close()
	engine, err = xorm.NewEngine("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.Instance = engine
	if _, err = Authenticate(token); err != nil {
		t.Fatal("lost after database reopen", err)
	}
	engine.ID(code.ID).Cols("expires_at").Update(&models.AccessCode{ExpiresAt: time.Now().Add(-time.Second).Unix()})
	if _, err = Authenticate(token); err == nil {
		t.Fatal("expired token accepted")
	}
	info, err = List(user.ID)
	if err != nil || !info[1].Expired {
		t.Fatal("expiry status missing")
	}
	if _, err = Authenticate(second); err != nil {
		t.Fatal("one expired code invalidated other code")
	}
	revokedCode, revokedToken, err := Create(user.ID, "revoked", 0, digest(user.Password))
	if err != nil {
		t.Fatal(err)
	}
	if err = Revoke(user.ID, revokedCode.ID); err != nil {
		t.Fatal(err)
	}
	if err = Revoke(user.ID, revokedCode.ID); err != nil {
		t.Fatal("revocation not idempotent", err)
	}
	if _, err = Authenticate(revokedToken); err == nil {
		t.Fatal("revoked code accepted")
	}
	// 密码版本变化即撤销，即使之后再次设置相同明文也会生成新哈希。
	engine.ID(user.ID).Cols("password").Update(&models.User{Password: "new-version"})
	if _, err = Authenticate(second); err == nil {
		t.Fatal("old credential version accepted")
	}
	if _, _, err = Create(user.ID, "stale session", 0, digest(user.Password)); err == nil {
		t.Fatal("stale session minted a new valid code after password change")
	}
	info, err = List(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range info {
		if !item.Revoked {
			t.Fatal("password invalidation not visible")
		}
	}
	for _, tc := range []struct {
		name   string
		expiry int64
	}{{"", 0}, {" ", 0}, {strings.Repeat("a", 81), 0}, {"past", time.Now().Add(-time.Hour).Unix()}, {"negative", -1}} {
		if _, _, err = Create(user.ID, tc.name, tc.expiry, digest(user.Password)); err != ErrParams {
			t.Fatalf("accepted invalid input: %#v %v", tc, err)
		}
	}
	engine.ID(user.ID).Cols("disabled").Update(&models.User{Disabled: 1})
	if _, _, err = Create(user.ID, "disabled", 0, digest(user.Password)); err == nil {
		t.Fatal("disabled user created code")
	}
}

func TestPurposesLegacyAndGrantVersion(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "codes.db"))
	if err != nil {
		t.Fatal(err)
	}
	old := db.Instance
	db.Instance = engine
	defer func() { engine.Close(); db.Instance = old }()
	if err = engine.Sync2(&models.User{}, &models.AccessCode{}); err != nil {
		t.Fatal(err)
	}
	u := models.User{Account: "owner", Password: "synthetic"}
	engine.Insert(&u)
	other := models.User{Account: "other", Password: "different"}
	engine.Insert(&other)
	legacy, token, err := Create(u.ID, "legacy", 0, digest(u.Password))
	if err != nil {
		t.Fatal(err)
	}
	engine.ID(legacy.ID).Cols("permissions").Update(&models.AccessCode{})
	if _, err = Authenticate(token); err != nil {
		t.Fatal("legacy HTTP lost", err)
	}
	if _, err = AuthenticateFor(token, "imap"); err == nil {
		t.Fatal("legacy code escalated")
	}
	for _, values := range [][]string{nil, {}, {"imap", "imap"}, {"http.read", "http.write"}, {"smtp"}, {"http.read", "imap", "pop3", "smtp"}} {
		if _, err = NormalizePermissions(values); err == nil {
			t.Fatal("invalid purpose accepted", values)
		}
	}
	code, token, err := CreateWithPermissions(u.ID, "client", 0, digest(u.Password), []string{"imap", "pop3"})
	if err != nil {
		t.Fatal(err)
	}
	if code.Scope == ReadScope {
		t.Fatal("old server would accept protocol-only code via HTTP")
	}
	grant, err := AuthenticateFor(token, "imap")
	if err != nil {
		t.Fatal(err)
	}
	if err = UpdatePermissions(other.ID, code.ID, []string{"http.write"}, digest(other.Password)); err == nil {
		t.Fatal("foreign purpose edit accepted")
	}
	if err = UpdatePermissions(u.ID, code.ID, []string{"http.read", "imap"}, digest(u.Password)); err != nil {
		t.Fatal(err)
	}
	if _, err = ValidateGrant(grant.CodeID, grant.Revision, "imap"); err == nil {
		t.Fatal("old session survived revision change")
	}
	fresh, err := AuthenticateFor(token, "imap")
	if err != nil || fresh.Revision <= grant.Revision {
		t.Fatal("same code did not accept new revision", err)
	}
	if _, err = AuthenticateFor(token, "pop3"); err == nil {
		t.Fatal("removed protocol accepted")
	}
	engine.ID(code.ID).Cols("expires_at").Update(&models.AccessCode{ExpiresAt: time.Now().Unix() - 1})
	if _, err = ValidateGrant(fresh.CodeID, fresh.Revision, "imap"); err == nil {
		t.Fatal("expired session accepted")
	}
	if err = UpdatePermissions(u.ID, code.ID, []string{"imap"}, digest(u.Password)); err == nil {
		t.Fatal("expired record resurrected")
	}
}
