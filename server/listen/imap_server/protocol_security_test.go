package imap_server

import (
	"crypto/sha256"
	"fmt"
	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/models"
	"github.com/Jinnrry/pmail/services/accesscode"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"strings"
	"testing"
	"time"
)

// 每个验收用例使用自己的账号及真实校验证书的TLS连接。
func protocolClient(t *testing.T) (*imapclient.Client, models.User, string, int64) {
	t.Helper()
	u := models.User{Account: strings.ToLower(t.Name()), Password: "synthetic-web-password"}
	if _, err := db.Instance.Insert(&u); err != nil {
		t.Fatal(err)
	}
	info, token, err := accesscode.CreateWithPermissions(u.ID, "client", 0, fmt.Sprintf("%x", sha256.Sum256([]byte(u.Password))), []string{"imap", "pop3"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := imapclient.DialTLS(imapTestAddr, &imapclient.Options{TLSConfig: imapTestTLS})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	if err = c.Login(u.Account+"@example.com", token).Wait(); err != nil {
		t.Fatal(err)
	}
	return c, u, token, info.ID
}
func TestProtocolAppendStoreExpunge(t *testing.T) {
	c, u, _, _ := protocolClient(t)
	raw := "From: sender@example.com\r\nTo: owner@example.com\r\nSubject: uploaded\r\nMessage-ID: <append@example.com>\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nhello\r\n"
	internalDate := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	cmd := c.Append("INBOX", int64(len(raw)), &imap.AppendOptions{Time: internalDate})
	if _, err := cmd.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	data, err := c.Select("INBOX", nil).Wait()
	if err != nil || data.NumMessages != 1 {
		t.Fatalf("APPEND did not persist: %+v %v", data, err)
	}
	dates, err := c.Fetch(imap.SeqSetNum(1), &imap.FetchOptions{InternalDate: true}).Collect()
	if err != nil || len(dates) != 1 || !dates[0].InternalDate.Equal(internalDate) {
		t.Fatal("APPEND internal date not preserved", err)
	}
	if _, err = c.Store(imap.SeqSetNum(1), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted, imap.FlagSeen}}, nil).Collect(); err != nil {
		t.Fatal(err)
	}
	count, _ := db.Instance.Where("user_id=?", u.ID).Count(&models.UserEmail{})
	if count != 1 {
		t.Fatal("STORE deleted before EXPUNGE")
	}
	if _, err = c.Store(imap.SeqSetNum(1), &imap.StoreFlags{Op: imap.StoreFlagsDel, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Collect(); err != nil {
		t.Fatal(err)
	}
	c.Expunge().Collect()
	count, _ = db.Instance.Where("user_id=?", u.ID).Count(&models.UserEmail{})
	if count != 1 {
		t.Fatal("removed deletion mark ignored")
	}
	if _, err = c.Store(imap.SeqSetNum(1), &imap.StoreFlags{Op: imap.StoreFlagsSet, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Collect(); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Expunge().Collect(); err != nil {
		t.Fatal(err)
	}
	count, _ = db.Instance.Where("user_id=?", u.ID).Count(&models.UserEmail{})
	if count != 0 {
		t.Fatal("EXPUNGE did not delete")
	}
}
func TestProtocolExamineRejectsWrites(t *testing.T) {
	c, u, _, _ := protocolClient(t)
	e := models.Email{Subject: "private"}
	db.Instance.Insert(&e)
	ue := models.UserEmail{UserID: u.ID, EmailID: e.Id}
	db.Instance.Insert(&ue)
	if _, err := c.Select("INBOX", &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Store(imap.SeqSetNum(1), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagSeen}}, nil).Collect(); err == nil {
		t.Fatal("EXAMINE allowed STORE")
	}
	if _, err := c.Fetch(imap.SeqSetNum(1), &imap.FetchOptions{BodySection: []*imap.FetchItemBodySection{{}}}).Collect(); err != nil {
		t.Fatal(err)
	}
	db.Instance.ID(ue.ID).Get(&ue)
	if ue.IsRead != 0 {
		t.Fatal("EXAMINE fetch marked message read")
	}
	if _, err := c.Select("does-not-exist", nil).Wait(); err == nil {
		t.Fatal("nonexistent mailbox selected")
	}
}

func TestProtocolIsolationAndLivePurposeChange(t *testing.T) {
	c, u, token, codeID := protocolClient(t)
	other := models.User{Account: "isolationother", Password: "synthetic"}
	db.Instance.Insert(&other)
	foreignMail := models.Email{Subject: "other-user-secret"}
	db.Instance.Insert(&foreignMail)
	foreign := models.UserEmail{UserID: other.ID, EmailID: foreignMail.Id}
	db.Instance.Insert(&foreign)
	ownMail := models.Email{Subject: "own"}
	db.Instance.Insert(&ownMail)
	own := models.UserEmail{UserID: u.ID, EmailID: ownMail.Id}
	db.Instance.Insert(&own)
	if _, err := c.Select("INBOX", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	if rows, err := c.Fetch(imap.UIDSetNum(imap.UID(foreign.ID)), &imap.FetchOptions{Envelope: true}).Collect(); err != nil || len(rows) != 0 {
		t.Fatal("foreign UID accessible", err)
	}
	if _, err := c.Store(imap.UIDSetNum(imap.UID(foreign.ID)), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Collect(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UIDExpunge(imap.UIDSetNum(imap.UID(foreign.ID))).Collect(); err != nil {
		t.Fatal(err)
	}
	ok, err := db.Instance.ID(foreign.ID).Get(&foreign)
	if err != nil || !ok || foreign.IMAPDeleted {
		t.Fatal("foreign mail altered")
	}
	if err := c.Create("archive", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	copied, err := c.Copy(imap.UIDSetNum(imap.UID(own.ID)), "archive").Wait()
	if err != nil || len(copied.DestUIDs) != 1 {
		t.Fatal("copy failed", err)
	}
	if _, err := c.Move(imap.UIDSetNum(imap.UID(own.ID)), "Junk").Wait(); err != nil {
		t.Fatal(err)
	}
	n, _ := db.Instance.Where("user_id=?", u.ID).Count(&models.UserEmail{})
	if n != 2 {
		t.Fatal("MOVE altered another copy")
	}
	version := fmt.Sprintf("%x", sha256.Sum256([]byte(u.Password)))
	if err := accesscode.UpdatePermissions(u.ID, codeID, []string{"pop3"}, version); err != nil {
		t.Fatal(err)
	}
	if _, err := accesscode.AuthenticateFor(token, "imap"); err == nil {
		t.Fatal("removed purpose accepted")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c.State() == imap.ConnStateLogout {
			return
		}
		time.Sleep(time.Millisecond * 10)
	}
	t.Fatal("idle IMAP connection survived purpose change")
}

func TestProtocolSequenceSurvivesConcurrentDeletion(t *testing.T) {
	c, u, _, _ := protocolClient(t)
	var relations []models.UserEmail
	for _, subject := range []string{"first", "second"} {
		e := models.Email{Subject: subject}
		if _, err := db.Instance.Insert(&e); err != nil {
			t.Fatal(err)
		}
		r := models.UserEmail{UserID: u.ID, EmailID: e.Id}
		db.Instance.Insert(&r)
		relations = append(relations, r)
	}
	if _, err := c.Select("INBOX", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	// 模拟另一个会话删除第一封。在收到EXPUNGE前，序号1不能重新指向第二封。
	db.Instance.ID(relations[0].ID).Delete(&models.UserEmail{})
	if _, err := c.Store(imap.SeqSetNum(1), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagSeen}}, nil).Collect(); err != nil {
		t.Fatal(err)
	}
	var remaining models.UserEmail
	db.Instance.ID(relations[1].ID).Get(&remaining)
	if remaining.IsRead != 0 {
		t.Fatal("concurrent deletion redirected STORE to another message")
	}
	if err := c.Noop().Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Store(imap.SeqSetNum(1), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagSeen}}, nil).Collect(); err != nil {
		t.Fatal(err)
	}
	db.Instance.ID(relations[1].ID).Get(&remaining)
	if remaining.IsRead != 1 {
		t.Fatal("sequence did not advance after EXPUNGE notification")
	}
}
