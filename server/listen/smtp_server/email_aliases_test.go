package smtp_server

import (
	"encoding/json"
	"errors"
	"github.com/Jinnrry/pmail/models"
	"github.com/Jinnrry/pmail/services/detail"
	"github.com/Jinnrry/pmail/services/mailaddress"
	"github.com/Jinnrry/pmail/utils/context"
	"testing"
)

func TestUnknownRecipientNeverFallsBackToAdministrator(t *testing.T) {
	engine, _ := newAuditTestEngine(t)
	if _, err := engine.Insert(&models.User{Account: "admin", IsAdmin: 1}); err != nil {
		t.Fatal(err)
	}
	email := newAuditTestEmail("unknown@example.com")
	_, saved, err := saveEmail(&context.Context{}, 4, email, 0, 0, []string{"unknown@example.com"}, true, true)
	if !errors.Is(err, mailaddress.ErrRecipient) || saved != nil {
		t.Fatalf("unknown recipient accepted or delivered to admin: saved=%+v err=%v", saved, err)
	}
	assertAuditCounts(t, engine, 0, 0)
}

func TestAliasIncomingDeduplicationAndDeliverySource(t *testing.T) {
	engine, _ := newAuditTestEngine(t)
	one, two := models.User{Account: "owner"}, models.User{Account: "other"}
	if _, err := engine.Insert(&one, &two); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		uid   int
		local string
	}{{one.ID, "shopping"}, {one.ID, "github"}, {two.ID, "secret"}} {
		if _, err := mailaddress.Create(v.uid, v.local, "example.com", ""); err != nil {
			t.Fatal(err)
		}
	}
	s := &Session{Ctx: &context.Context{}}
	for _, address := range []string{"owner@example.com", "Shopping@example.com", "github@example.com", "shopping@example.com", "secret@example.com"} {
		if err := s.Rcpt(address, nil); err != nil {
			t.Fatal(err)
		}
	}
	assertSMTPCode(t, s.Rcpt("unknown@example.com", nil), 550)
	email := newAuditTestEmail("alias-dedupe@example.com") // 邮件头指向无关地址，路由必须依据信封。
	users, saved, err := saveEmail(s.Ctx, 4, email, 0, 0, s.To, true, true, s.Accepted)
	if err != nil || len(users) != 2 {
		t.Fatal(users, err)
	}
	assertAuditCounts(t, engine, 1, 2)
	for _, tc := range []struct {
		uid  int
		want int
	}{{one.ID, 3}, {two.ID, 1}} {
		d, err := detail.GetEmailDetail(&context.Context{UserID: tc.uid}, saved.Id, false)
		if err != nil || len(d.DeliveryAddresses) != tc.want {
			t.Fatal(d, err)
		}
		for _, address := range d.DeliveryAddresses {
			if tc.uid == one.ID && address == "secret@example.com" {
				t.Fatal("leaked other user's delivery address")
			}
		}
	}
	var ue models.UserEmail
	engine.Where("email_id=? AND user_id=?", saved.Id, one.ID).Get(&ue)
	if ue.IsRead != 0 {
		t.Fatal("read-only detail marked mail read")
	}
	if err = mailaddress.Delete(one.ID, 1); err != nil {
		t.Fatal(err)
	}
	d, err := detail.GetEmailDetail(&context.Context{UserID: one.ID}, saved.Id, false)
	if err != nil || len(d.DeliveryAddresses) != 3 {
		t.Fatal("deleted alias damaged history", d, err)
	}
	s.Reset()
	if len(s.Accepted) != 0 || len(s.To) != 0 {
		t.Fatal("recipient snapshot leaked across RSET")
	}
}
func TestAliasRecipientMutationDoesNotMisdeliver(t *testing.T) {
	for _, mutation := range []string{"delete-recreate", "disable-alias", "disable-owner", "delete-owner"} {
		t.Run(mutation, func(t *testing.T) {
			engine, _ := newAuditTestEngine(t)
			one, two := models.User{Account: "owner"}, models.User{Account: "other"}
			engine.Insert(&one, &two)
			a, err := mailaddress.Create(one.ID, "changing", "example.com", "")
			if err != nil {
				t.Fatal(err)
			}
			s := &Session{Ctx: &context.Context{}}
			if err = s.Rcpt(a.Address, nil); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "delete-recreate":
				mailaddress.Delete(one.ID, a.ID)
				if _, err = mailaddress.Create(two.ID, "changing", "example.com", ""); err != nil {
					t.Fatal(err)
				}
			case "disable-alias":
				mailaddress.Update(one.ID, a.ID, "", false)
			case "disable-owner":
				engine.ID(one.ID).Cols("disabled").Update(&models.User{Disabled: 1})
			case "delete-owner":
				engine.ID(one.ID).Delete(&models.User{})
			}
			email := newAuditTestEmail("changed@example.com")
			_, saved, err := saveEmail(s.Ctx, 4, email, 0, 0, s.To, true, true, s.Accepted)
			if err == nil || saved != nil {
				t.Fatal("changed recipient accepted", err)
			}
			assertAuditCounts(t, engine, 0, 0)
		})
	}
}
func TestAliasSMTPDatabaseFailureAndTransactionRollback(t *testing.T) {
	engine, _ := newAuditTestEngine(t)
	owner := models.User{Account: "owner"}
	engine.Insert(&owner)
	a, err := mailaddress.Create(owner.ID, "alias", "example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	s := &Session{Ctx: &context.Context{}}
	if err = s.Rcpt(a.Address, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = engine.Exec(`CREATE TRIGGER fail_alias_link BEFORE INSERT ON user_email BEGIN SELECT RAISE(ABORT,'forced failure'); END`); err != nil {
		t.Fatal(err)
	}
	_, saved, err := saveEmail(s.Ctx, 4, newAuditTestEmail("rollback@example.com"), 0, 0, s.To, true, true, s.Accepted)
	if err == nil || saved != nil {
		t.Fatal("partial write accepted", err)
	}
	assertAuditCounts(t, engine, 0, 0)
	engine.Close()
	assertSMTPCode(t, s.Rcpt(a.Address, nil), 451)
}
func TestAliasDeliveryMetadataSerialization(t *testing.T) {
	// 新增字段必须仅在详情返回，用户关联模型本身不能意外泄露完整元数据。
	raw, err := json.Marshal(models.UserEmail{DeliveryAddresses: `["private@example.com"]`})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	json.Unmarshal(raw, &fields)
	if _, ok := fields["DeliveryAddresses"]; ok {
		t.Fatal("raw relation exposes envelope metadata")
	}
}
