package imap_server

import (
	"github.com/Jinnrry/pmail/services/protocolsecurity"
	"github.com/Jinnrry/pmail/utils/context"
	"github.com/Jinnrry/pmail/utils/id"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2/imapserver"
)

// Server is a server instance.
//
// A server contains a list of users.
type Server struct {
	mutex sync.Mutex
}

// NewServer creates a new server.
func NewServer() *Server {
	return &Server{}
}

type Status int8

const (
	UNAUTHORIZED Status = 1
	AUTHORIZED   Status = 2
	SELECTED     Status = 3
	LOGOUT       Status = 4
)

type serverSession struct {
	mailboxMu      sync.Mutex
	mailboxUIDs    []int
	auth           *protocolsecurity.Session
	readOnly       bool
	server         *Server // immutable
	ctx            *context.Context
	status         Status
	currentMailbox string
	connectTime    time.Time
}

// NewSession creates a new IMAP session.
func (s *Server) NewSession() imapserver.Session {
	tc := &context.Context{}
	tc.SetValue(context.LogID, id.GenLogID())

	return &serverSession{
		server:      s,
		ctx:         tc,
		connectTime: time.Now(),
		status:      UNAUTHORIZED,
	}
}

func (s *serverSession) Close() error {
	if s.auth != nil {
		s.auth.Close()
	}
	return nil
}

func (s *serverSession) Subscribe(mailbox string) error {
	if err := s.authorize(); err != nil {
		return err
	}
	return nil
}

func (s *serverSession) Unsubscribe(mailbox string) error {
	if err := s.authorize(); err != nil {
		return err
	}
	return nil
}

func (s *serverSession) Unselect() error {
	if err := s.authorize(); err != nil {
		return err
	}
	s.mailboxMu.Lock()
	s.mailboxUIDs = nil
	s.currentMailbox = ""
	s.mailboxMu.Unlock()
	s.readOnly = false
	s.ctx.ReadOnly = false
	return nil
}
