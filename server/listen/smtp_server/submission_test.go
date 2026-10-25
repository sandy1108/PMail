package smtp_server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"github.com/Jinnrry/pmail/dto/parsemail"
	"github.com/Jinnrry/pmail/models"
	"github.com/Jinnrry/pmail/services/accesscode"
	"github.com/Jinnrry/pmail/services/protocolsecurity"
	"github.com/Jinnrry/pmail/utils/context"
	"math/big"
	"net"
	smtpclient "net/smtp"
	"strings"
	"testing"
	"time"
)

// 每个用例使用隔离SQLite，不触碰真实邮箱或向公网发送邮件。
func TestSubmissionAuthorizationAndSenderBoundary(t *testing.T) {
	engine, _ := newAuditTestEngine(t)
	if err := engine.Sync2(&models.AccessCode{}); err != nil {
		t.Fatal(err)
	}
	u := models.User{Account: "sender", Password: "web-password", IsAdmin: 1}
	engine.Insert(&u)
	other := models.User{Account: "other", Password: "other-password"}
	engine.Insert(&other)
	engine.Insert(&models.EmailAlias{UserID: u.ID, Address: "alias@example.com", LocalPart: "alias", Domain: "example.com", Enabled: true})
	code, token, err := accesscode.CreateWithPermissions(u.ID, "SMTP", 0, fmt.Sprintf("%x", sha256.Sum256([]byte(u.Password))), []string{"smtp"})
	if err != nil {
		t.Fatal(err)
	}
	s := &Session{Submission: true, auth: protocolsecurity.NewSession("smtp", nil), Ctx: &context.Context{}}
	defer s.Logout()
	if s.Mail("sender@example.com", nil) == nil || s.Rcpt("external@example.net", nil) == nil || s.Data(strings.NewReader("")) == nil {
		t.Fatal("unauthenticated submission accepted")
	}
	if s.AuthPlain("sender@example.com", u.Password) == nil {
		t.Fatal("web password accepted")
	}
	if s.AuthPlain("other@example.com", token) == nil {
		t.Fatal("cross-user token accepted")
	}
	if s.AuthPlain("sender@example.com", token) != nil {
		t.Fatal("valid SMTP code rejected")
	}
	if s.Ctx.IsAdmin {
		t.Fatal("SMTP token grants admin")
	}
	for _, address := range []string{"other@example.com", "sender@external.net", "alias@example.com", ""} {
		if s.Mail(address, nil) == nil {
			t.Fatal("sender impersonation", address)
		}
	}
	if err = s.Mail("sender@example.com", nil); err != nil {
		t.Fatal(err)
	}
	if err = s.Rcpt("external@example.net", nil); err != nil {
		t.Fatal("authenticated relay rejected", err)
	}
	for _, raw := range []string{"From: other@example.com\r\n\r\nx", "From: sender@example.com, other@example.com\r\n\r\nx", "From: sender@example.com\r\nSender: other@example.com\r\n\r\nx"} {
		if s.validateSubmissionHeaders([]byte(raw)) == nil {
			t.Fatal("spoofed header accepted")
		}
	}
	if err = s.validateSubmissionHeaders([]byte("From: Sender <sender@example.com>\r\n\r\nx")); err != nil {
		t.Fatal(err)
	}
	if err = accesscode.Revoke(u.ID, code.ID); err != nil {
		t.Fatal(err)
	}
	if s.Mail("sender@example.com", nil) == nil || s.Rcpt("external@example.net", nil) == nil || s.Data(strings.NewReader("")) == nil {
		t.Fatal("revoked session accepted")
	}
	inbound := &Session{Ctx: &context.Context{}}
	if len(inbound.AuthMechanisms()) != 0 || inbound.AuthPlain("sender@example.com", token) == nil {
		t.Fatal("inbound AUTH enabled")
	}
	if inbound.Rcpt("external@example.net", nil) == nil {
		t.Fatal("inbound became open relay")
	}
}

func TestSubmissionTLSWireAndRevocation(t *testing.T) {
	engine, _ := newAuditTestEngine(t)
	if err := engine.Sync2(&models.AccessCode{}); err != nil {
		t.Fatal(err)
	}
	u := models.User{Account: "wire", Password: "synthetic"}
	if _, err := engine.Insert(&u); err != nil {
		t.Fatal(err)
	}
	code, token, err := accesscode.CreateWithPermissions(u.ID, "wire", 0, fmt.Sprintf("%x", sha256.Sum256([]byte(u.Password))), []string{"smtp"})
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"mail.example.com"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	listener, err := protocolsecurity.ListenTLS("127.0.0.1", 0, certificate)
	if err != nil {
		t.Fatal(err)
	}
	server := newSubmissionServer()
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	defer func() { server.Close(); listener.Close(); <-done }()
	// 明文客户端不能得到SMTP问候，最低TLS1.2。
	raw, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	raw.SetDeadline(time.Now().Add(time.Second))
	raw.Write([]byte("EHLO unsafe.example\r\n"))
	buffer := make([]byte, 512)
	n, _ := raw.Read(buffer)
	raw.Close()
	if strings.Contains(string(buffer[:n]), "220 ") {
		t.Fatal("plaintext greeting exposed")
	}
	oldTLS, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", listener.Addr().String(), &tls.Config{ServerName: "mail.example.com", RootCAs: roots, MinVersion: tls.VersionTLS10, MaxVersion: tls.VersionTLS11})
	if err == nil {
		oldTLS.Close()
		t.Fatal("TLS1.1 accepted")
	}
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", listener.Addr().String(), &tls.Config{ServerName: "mail.example.com", RootCAs: roots, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(4 * time.Second))
	client, err := smtpclient.NewClient(conn, "mail.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := client.Extension("STARTTLS"); ok {
		t.Fatal("STARTTLS advertised on implicit listener")
	}
	if ok, _ := client.Extension("AUTH"); !ok {
		t.Fatal("TLS AUTH missing")
	}
	if err = client.Mail("wire@example.com"); err == nil {
		t.Fatal("anonymous submit accepted")
	}
	if err = client.Auth(smtpclient.PlainAuth("", "wire@example.com", token, "mail.example.com")); err != nil {
		t.Fatal(err)
	}
	if err = client.Mail("wire@example.com"); err != nil {
		t.Fatal(err)
	}
	if err = client.Rcpt("external@example.net"); err != nil {
		t.Fatal(err)
	}
	if err = accesscode.Revoke(u.ID, code.ID); err != nil {
		t.Fatal(err)
	}
	if err = client.Reset(); err != nil {
		return
	}
	if err = client.Mail("wire@example.com"); err == nil {
		t.Fatal("revoked connection accepted MAIL")
	}
}

// 使用隔离投递端验证实际信封和审计，不进行公网发信。
func TestSubmissionEnvelopeDoesNotDeliverHeaderOnlyRecipients(t *testing.T) {
	engine, _ := newAuditTestEngine(t)
	if err := engine.Sync2(&models.AccessCode{}); err != nil {
		t.Fatal(err)
	}
	u := models.User{Account: "envelope", Password: "synthetic"}
	engine.Insert(&u)
	_, token, err := accesscode.CreateWithPermissions(u.ID, "SMTP", 0, fmt.Sprintf("%x", sha256.Sum256([]byte(u.Password))), []string{"smtp"})
	if err != nil {
		t.Fatal(err)
	}
	s := &Session{Submission: true, auth: protocolsecurity.NewSession("smtp", nil), Ctx: &context.Context{}}
	defer s.Logout()
	if err = s.AuthPlain("envelope@example.com", token); err != nil {
		t.Fatal(err)
	}
	s.Mail("envelope@example.com", nil)
	s.Rcpt("accepted@example.net", nil)
	email := newAuditTestEmail("envelope-only@example.com")
	email.From = &parsemail.User{EmailAddress: "envelope@example.com"}
	email.Sender = email.From
	email.To = []*parsemail.User{{EmailAddress: "not-in-envelope@example.net"}}
	email.Cc = []*parsemail.User{{EmailAddress: "also-not-accepted@example.net"}}
	called := false
	err = s.submitEmail(email, func(ctx *context.Context, e *parsemail.Email, to []*parsemail.User, from string) (error, map[string]error) {
		called = true
		if len(to) != 1 || to[0].EmailAddress != "accepted@example.net" || from != "envelope@example.com" || ctx.UserID != u.ID {
			t.Fatal("unvalidated envelope or identity passed to transport")
		}
		return nil, nil
	})
	if err != nil || !called {
		t.Fatal("submission did not reach isolated transport", err)
	}
	assertAuditCounts(t, engine, 1, 1)
	s.Reset()
	if s.From != "" || len(s.To) != 0 {
		t.Fatal("RSET retained envelope")
	}
}
