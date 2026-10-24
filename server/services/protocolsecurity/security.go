// Package protocolsecurity集中实现客户端协议的用途认证、失效和连接预算。
package protocolsecurity

import (
	"crypto/tls"
	"errors"
	"github.com/Jinnrry/pmail/models"
	"github.com/Jinnrry/pmail/services/accesscode"
	"github.com/Jinnrry/pmail/services/mailaddress"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrAuth = errors.New("invalid or inactive client credentials")
var rate = struct {
	sync.Mutex
	tokens float64
	last   time.Time
}{tokens: 10, last: time.Now()}

func allowLogin() bool {
	rate.Lock()
	defer rate.Unlock()
	now := time.Now()
	rate.tokens += now.Sub(rate.last).Seconds() / 3
	rate.last = now
	if rate.tokens > 10 {
		rate.tokens = 10
	}
	if rate.tokens < 1 {
		return false
	}
	rate.tokens--
	return true
}

// Session仅保存授权记录ID，不保留明文码。命令复验之外，每秒检查闲置连接。
type Session struct {
	mu          sync.Mutex
	protocol    string
	grant       accesscode.Grant
	closeConn   func()
	done        chan struct{}
	closeOnce   sync.Once
	monitorOnce sync.Once
}

func NewSession(protocol string, closeConn func()) *Session {
	return &Session{protocol: protocol, closeConn: closeConn, done: make(chan struct{})}
}
func (s *Session) Login(username, token string) (models.User, error) {
	if !allowLogin() {
		return models.User{}, ErrAuth
	}
	username = strings.ToLower(strings.TrimSpace(username))
	parts := strings.Split(username, "@")
	if len(parts) != 2 || parts[0] == "" || !mailaddress.IsLocalDomain(parts[1]) {
		return models.User{}, ErrAuth
	}
	g, err := accesscode.AuthenticateFor(token, s.protocol)
	if err != nil || g.User.Account != parts[0] {
		return models.User{}, ErrAuth
	}
	s.mu.Lock()
	s.grant = g
	s.mu.Unlock()
	s.monitorOnce.Do(func() {
		go func() {
			tick := time.NewTicker(time.Second)
			defer tick.Stop()
			for {
				select {
				case <-s.done:
					return
				case <-tick.C:
					if s.Check() != nil {
						s.invalidate()
						return
					}
				}
			}
		}()
	})
	return g.User, nil
}
func (s *Session) Check() error {
	s.mu.Lock()
	g := s.grant
	s.mu.Unlock()
	if g.CodeID == 0 {
		return ErrAuth
	}
	_, err := accesscode.ValidateGrant(g.CodeID, g.Revision, s.protocol)
	if err != nil {
		return ErrAuth
	}
	return nil
}
func (s *Session) invalidate() {
	s.closeOnce.Do(func() {
		close(s.done)
		if s.closeConn != nil {
			s.closeConn()
		}
	})
}
func (s *Session) Close() { s.closeOnce.Do(func() { close(s.done) }) }

// Listener限制总连接数和空闲读写，关闭时也关闭存量连接；TLS包在预算层之外。
type Listener struct {
	net.Listener
	mu          sync.Mutex
	connections map[*connection]struct{}
	closed      bool
}
type connection struct {
	net.Conn
	owner *Listener
	once  sync.Once
}

func (c *connection) Close() error {
	var err error
	c.once.Do(func() { err = c.Conn.Close(); c.owner.mu.Lock(); delete(c.owner.connections, c); c.owner.mu.Unlock() })
	return err
}

// 保留上层更短的握手期限，禁止把其延长为统一空闲超时。
func (c *connection) SetDeadline(t time.Time) error {
	if err := c.SetReadDeadline(t); err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}
func (c *connection) SetReadDeadline(t time.Time) error {
	max := time.Now().Add(5 * time.Minute)
	if t.IsZero() || t.After(max) {
		t = max
	}
	return c.Conn.SetReadDeadline(t)
}
func (c *connection) SetWriteDeadline(t time.Time) error {
	max := time.Now().Add(30 * time.Second)
	if t.IsZero() || t.After(max) {
		t = max
	}
	return c.Conn.SetWriteDeadline(t)
}
func (l *Listener) Accept() (net.Conn, error) {
	for {
		raw, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		l.mu.Lock()
		if l.closed || len(l.connections) >= 32 {
			l.mu.Unlock()
			raw.Close()
			continue
		}
		c := &connection{Conn: raw, owner: l}
		c.SetReadDeadline(time.Now().Add(10 * time.Second))
		c.SetWriteDeadline(time.Now().Add(10 * time.Second))
		l.connections[c] = struct{}{}
		l.mu.Unlock()
		return c, nil
	}
}
func (l *Listener) Close() error {
	err := l.Listener.Close()
	l.mu.Lock()
	l.closed = true
	var all []*connection
	for c := range l.connections {
		all = append(all, c)
	}
	l.mu.Unlock()
	for _, c := range all {
		c.Close()
	}
	return err
}
func ListenTLS(bind string, port int, certificate tls.Certificate) (net.Listener, error) {
	if net.ParseIP(bind) == nil || port < 0 || port > 65535 {
		return nil, errors.New("invalid TLS bind address or port")
	}
	raw, err := net.Listen("tcp", net.JoinHostPort(bind, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	budget := &Listener{Listener: raw, connections: map[*connection]struct{}{}}
	return tls.NewListener(budget, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}), nil
}
