package protocolsecurity

import (
	"crypto/sha256"
	"crypto/tls"
	"fmt"
	"github.com/Jinnrry/pmail/config"
	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/internal/testcert"
	"github.com/Jinnrry/pmail/models"
	"github.com/Jinnrry/pmail/services/accesscode"
	_ "modernc.org/sqlite"
	"net"
	"path/filepath"
	"testing"
	"time"
	"xorm.io/xorm"
)

func TestProtocolGrantDomainAndLiveRevocation(t *testing.T) {
	e, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	oldDB, oldCfg := db.Instance, config.Instance
	db.Instance = e
	config.Instance = &config.Config{Domain: "example.com"}
	defer func() { db.Instance = oldDB; config.Instance = oldCfg; e.Close() }()
	if err = e.Sync2(&models.User{}, &models.AccessCode{}); err != nil {
		t.Fatal(err)
	}
	u := models.User{Account: "owner", Password: "synthetic"}
	e.Insert(&u)
	version := fmt.Sprintf("%x", sha256.Sum256([]byte(u.Password)))
	code, token, err := accesscode.CreateWithPermissions(u.ID, "client", 0, version, []string{"imap", "pop3"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"owner@external.test", "alias@example.com", "owner@example.com@external.test", "owner"} {
		s := NewSession("imap", nil)
		if _, err = s.Login(name, token); err == nil {
			t.Fatalf("invalid identity accepted %s", name)
		}
		s.Close()
	}
	closed := make(chan struct{}, 1)
	s := NewSession("imap", func() { closed <- struct{}{} })
	defer s.Close()
	if _, err = s.Login("Owner@EXAMPLE.COM", token); err != nil {
		t.Fatal(err)
	}
	if err = s.Check(); err != nil {
		t.Fatal(err)
	}
	if err = accesscode.UpdatePermissions(u.ID, code.ID, []string{"pop3"}, version); err != nil {
		t.Fatal(err)
	}
	if err = s.Check(); err == nil {
		t.Fatal("stale session survived permissions change")
	}
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("idle revoked session not closed")
	}
	if _, err = accesscode.AuthenticateFor(token, "imap"); err == nil {
		t.Fatal("removed protocol accepted")
	}
	if _, err = accesscode.AuthenticateFor(token, "pop3"); err != nil {
		t.Fatal("remaining protocol rejected", err)
	}
	if _, err = accesscode.Authenticate(token); err == nil {
		t.Fatal("protocol code allowed HTTP")
	}
}

func TestTLSMinimumAndConnectionBudget(t *testing.T) {
	cert, clientConfig, _, _, err := testcert.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ln, err := ListenTLS("127.0.0.1", 0, cert)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(time.Second))
				tc := conn.(*tls.Conn)
				tc.Handshake()
			}()
		}
	}()
	c, err := tls.Dial("tcp", ln.Addr().String(), clientConfig)
	if err != nil {
		t.Fatal("valid certificate rejected", err)
	}
	c.Close()
	old := clientConfig.Clone()
	old.MinVersion = tls.VersionTLS10
	old.MaxVersion = tls.VersionTLS11
	if c, err = tls.Dial("tcp", ln.Addr().String(), old); err == nil {
		c.Close()
		t.Fatal("obsolete TLS accepted")
	}
	ln.Close()
	<-serverDone
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	budget := &Listener{Listener: raw, connections: map[*connection]struct{}{}}
	accepted := make(chan net.Conn, 40)
	go func() {
		for {
			c, err := budget.Accept()
			if err != nil {
				close(accepted)
				return
			}
			accepted <- c
		}
	}()
	var clients []net.Conn
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()
	for i := 0; i < 32; i++ {
		c, err := net.Dial("tcp", raw.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		clients = append(clients, c)
		<-accepted
	}
	extra, err := net.Dial("tcp", raw.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer extra.Close()
	extra.SetReadDeadline(time.Now().Add(time.Second))
	if _, err = extra.Read(make([]byte, 1)); err == nil {
		t.Fatal("connection budget bypassed")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("excess connection was not closed")
	}
	budget.Close()
	for range accepted {
	}
	budget.mu.Lock()
	remaining := len(budget.connections)
	budget.mu.Unlock()
	if remaining != 0 {
		t.Fatal("listener leaked active connections")
	}
}
