package smtp_server

import (
	"bufio"
	"github.com/Jinnrry/pmail/models"
	"github.com/emersion/go-smtp"
	"github.com/pires/go-proxyproto"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func proxyBytes(version byte, local bool) []byte {
	h := proxyproto.HeaderProxyFromAddrs(version, &net.TCPAddr{IP: net.ParseIP("203.0.113.20"), Port: 4321}, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2525})
	if local {
		h.Command = proxyproto.LOCAL
	}
	b, err := h.Format()
	if err != nil {
		panic(err)
	}
	return b
}

func TestProxyBoundary(t *testing.T) {
	base, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener := proxyListener(base, []string{"127.0.0.1"}, 100*time.Millisecond)
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _, _ = c.Write([]byte("peer " + c.RemoteAddr().String() + "\n")) }()
		}
	}()
	cases := []struct {
		name   string
		header []byte
		good   bool
	}{
		{"v2", proxyBytes(2, false), true}, {"missing", []byte("EHLO example.test\r\n"), false},
		{"v1", proxyBytes(1, false), false}, {"local", proxyBytes(2, true), false},
		{"truncated", proxyBytes(2, false)[:20], false}, {"timeout", nil, false},
		{"after-invalid", proxyBytes(2, false), true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c, err := net.Dial("tcp", base.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			c.SetDeadline(time.Now().Add(time.Second))
			if tt.header != nil {
				c.Write(tt.header)
			}
			b, _ := bufio.NewReader(c).ReadString('\n')
			if tt.good && !strings.Contains(b, "203.0.113.20:4321") {
				t.Fatalf("real address missing: %q", b)
			}
			if !tt.good && b != "" {
				t.Fatalf("bad connection accepted: %q", b)
			}
		})
	}
	listener.Close()
	<-done
}

func TestProxyUntrustedThenTrusted(t *testing.T) {
	base, _ := net.Listen("tcp", "127.0.0.1:0")
	listener := proxyListener(base, []string{"127.0.0.2"}, time.Second)
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		c, e := listener.Accept()
		if e == nil {
			defer c.Close()
			_, e = c.Write([]byte("ok"))
		}
		done <- e
	}()
	c, _ := net.Dial("tcp", base.Addr().String())
	c.SetDeadline(time.Now().Add(time.Second))
	c.Write(proxyBytes(2, false))
	b := make([]byte, 2)
	n, _ := c.Read(b)
	c.Close()
	if n != 0 {
		t.Fatal("untrusted sender accepted")
	}
	// 无法依赖每台机器绑定127.0.0.2；确认拒绝不会导致Accept退出，直到主动关闭。
	select {
	case e := <-done:
		t.Fatalf("accept stopped on untrusted sender: %v", e)
	case <-time.After(50 * time.Millisecond):
	}
	listener.Close()
	<-done
}

func TestInboundRecipientAndReset(t *testing.T) {
	engine, _ := newAuditTestEngine(t)
	if _, err := engine.Insert(&models.User{Account: "sandai", Name: "user", Password: "unused"}, &models.User{Account: "disabled", Name: "disabled", Password: "unused", Disabled: 1}); err != nil {
		t.Fatal(err)
	}
	s := newAuditTestSession()
	for _, rcpt := range []string{"other@external.test", "unknown@example.com", "disabled@example.com", "bad"} {
		if s.Rcpt(rcpt, &smtp.RcptOptions{}) == nil {
			t.Fatalf("accepted %s", rcpt)
		}
	}
	if err := s.Rcpt("sandai@example.com", &smtp.RcptOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(s.AuthMechanisms()) != 0 || s.AuthPlain("sandai", "anything") == nil {
		t.Fatal("inbound AUTH enabled")
	}
	s.From = "sender@external.test"
	s.Reset()
	if s.From != "" || len(s.To) != 0 {
		t.Fatal("envelope carried across transactions")
	}
}

func TestProxySMTPGreeting(t *testing.T) {
	base, _ := net.Listen("tcp", "127.0.0.1:0")
	l := proxyListener(base, []string{"127.0.0.1"}, 100*time.Millisecond)
	server := smtp.NewServer(&Backend{})
	server.Domain = "mail.example.com"
	server.ReadTimeout = time.Second
	server.WriteTimeout = time.Second
	go server.Serve(l)
	defer server.Close()
	c, _ := net.Dial("tcp", base.Addr().String())
	defer c.Close()
	c.SetDeadline(time.Now().Add(time.Second))
	c.Write(proxyBytes(2, false))
	b, _ := bufio.NewReader(c).ReadString('\n')
	if !strings.HasPrefix(b, "220 ") {
		t.Fatalf("no SMTP greeting: %q", b)
	}
	// 不发送信件，不触发外部DNS或投递。
	c.Write([]byte("QUIT\r\n"))
	io.Copy(io.Discard, c)
}
