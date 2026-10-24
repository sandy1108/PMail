package pop3_server

import (
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/Jinnrry/pmail/config"
	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/dto/parsemail"
	"github.com/Jinnrry/pmail/internal/testcert"
	"github.com/Jinnrry/pmail/models"
	"github.com/Jinnrry/pmail/services/accesscode"
	"github.com/Jinnrry/pmail/services/protocolsecurity"
	"net"
	"net/textproto"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTLSProtocolTransactions(t *testing.T) {
	dir := t.TempDir()
	cert, clientTLS, _, _, err := testcert.Create(dir)
	if err != nil {
		t.Fatal(err)
	}
	oldDB, oldCfg := db.Instance, config.Instance
	config.Instance = &config.Config{Domain: "example.com", DbType: config.DBTypeSQLite, DbDSN: filepath.Join(dir, "mail.db")}
	if err = db.Init(""); err != nil {
		t.Fatal(err)
	}
	ln, err := protocolsecurity.ListenTLS("127.0.0.1", 0, cert)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { servePOP3(ln); close(done) }()
	defer func() { ln.Close(); <-done; db.Instance.Close(); db.Instance = oldDB; config.Instance = oldCfg }()
	u := models.User{Account: "owner", Password: "synthetic-web-password"}
	db.Instance.Insert(&u)
	other := models.User{Account: "other", Password: "other"}
	db.Instance.Insert(&other)
	for _, uid := range []int{other.ID, u.ID, u.ID} {
		attachments, _ := json.Marshal([]parsemail.Attachment{{Filename: "fixture.txt", ContentType: "text/plain", Content: []byte("attached-content")}})
		e := models.Email{Subject: fmt.Sprintf("private-%d", uid), FromAddress: "sender@example.com", Attachments: string(attachments), MsgID: fmt.Sprintf("%d@example.com", uid), Text: sql.NullString{String: "hello\r\n.dot", Valid: true}}
		db.Instance.Insert(&e)
		db.Instance.Insert(&models.UserEmail{UserID: uid, EmailID: e.Id})
	}
	version := fmt.Sprintf("%x", sha256.Sum256([]byte(u.Password)))
	info, token, err := accesscode.CreateWithPermissions(u.ID, "client", 0, version, []string{"pop3", "imap"})
	if err != nil {
		t.Fatal(err)
	}
	connect := func(pass string) (*textproto.Conn, *tls.Conn) {
		t.Helper()
		c, err := tls.Dial("tcp", ln.Addr().String(), clientTLS)
		if err != nil {
			t.Fatal(err)
		}
		c.SetDeadline(time.Now().Add(5 * time.Second))
		p := textproto.NewConn(c)
		p.ReadLine()
		p.PrintfLine("USER owner@example.com")
		p.ReadLine()
		p.PrintfLine("PASS %s", pass)
		return p, c
	}
	command := func(p *textproto.Conn, cmd string) string {
		t.Helper()
		p.PrintfLine("%s", cmd)
		line, err := p.ReadLine()
		if err != nil {
			t.Fatal(err)
		}
		return line
	}
	p, c := connect(u.Password)
	line, _ := p.ReadLine()
	if !strings.HasPrefix(line, "-ERR") {
		t.Fatal("web password accepted")
	}
	c.Close()
	p, c = connect(token)
	line, _ = p.ReadLine()
	if !strings.HasPrefix(line, "+OK") {
		t.Fatal(line)
	}
	if line = command(p, "STAT"); !strings.HasPrefix(line, "+OK 2 ") {
		t.Fatal(line)
	}
	command(p, "UIDL")
	uids, err := p.ReadDotLines()
	if err != nil || len(uids) != 2 || !strings.HasPrefix(uids[0], "1 ") || !strings.HasPrefix(uids[1], "2 ") {
		t.Fatal(uids, err)
	}
	if line = command(p, "RETR 1"); !strings.HasPrefix(line, "+OK") {
		t.Fatal(line)
	}
	raw, err := p.ReadDotBytes()
	if err != nil || !strings.Contains(string(raw), fmt.Sprintf("private-%d", u.ID)) || strings.Contains(string(raw), fmt.Sprintf("private-%d", other.ID)) {
		t.Fatal("wrong body", err)
	}
	command(p, "TOP 1 0")
	headers, err := p.ReadDotBytes()
	if err != nil || !strings.Contains(string(headers), "Subject:") || strings.Contains(string(headers), "attached-content") {
		t.Fatal("TOP body limit incorrect", err)
	}
	parsed := parsemail.NewEmailFromReader(nil, strings.NewReader(string(raw)), len(raw))
	if parsed == nil || len(parsed.Attachments) != 1 || string(parsed.Attachments[0].Content) != "attached-content" {
		t.Fatal("attachment did not survive POP3")
	}
	if line = command(p, "RETR 3"); !strings.HasPrefix(line, "-ERR") {
		t.Fatal("DB ID accepted as sequence")
	}
	command(p, "DELE 1")
	c.Close()
	// 断线后等待事务解锁，不把时序碰巧当成正确性。
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, busy := mailboxLocks.Load(u.ID); !busy {
			break
		}
		time.Sleep(time.Millisecond)
	}
	p, c = connect(token)
	line, _ = p.ReadLine()
	if !strings.HasPrefix(line, "+OK") {
		t.Fatal(line)
	}
	command(p, "DELE 1")
	command(p, "RSET")
	command(p, "QUIT")
	c.Close()
	n, _ := db.Instance.Where("user_id=? AND status=0", u.ID).Count(&models.UserEmail{})
	if n != 2 {
		t.Fatal("disconnect or RSET deleted mail")
	}
	p, c = connect(token)
	line, _ = p.ReadLine()
	if !strings.HasPrefix(line, "+OK") {
		t.Fatal(line)
	}
	command(p, "DELE 1")
	command(p, "QUIT")
	c.Close()
	n, _ = db.Instance.Where("user_id=? AND status=0", u.ID).Count(&models.UserEmail{})
	if n != 1 {
		t.Fatal("QUIT did not commit deletion")
	}
	n, _ = db.Instance.Where("user_id=? AND status=0", other.ID).Count(&models.UserEmail{})
	if n != 1 {
		t.Fatal("foreign mail changed")
	}
	p, c = connect(token)
	line, _ = p.ReadLine()
	if !strings.HasPrefix(line, "+OK") {
		t.Fatal(line)
	}
	if err = accesscode.Revoke(u.ID, info.ID); err != nil {
		t.Fatal(err)
	}
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err = p.ReadLine(); err == nil {
		t.Fatal("revoked idle connection alive")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("revocation did not close connection", err)
	}
	c.Close()
}
