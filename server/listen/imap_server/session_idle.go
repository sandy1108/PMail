package imap_server

import (
	"github.com/Jinnrry/pmail/models"
	"github.com/Jinnrry/pmail/utils/context"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/spf13/cast"
	"sync"
)

type idleConnection struct {
	writer  *imapserver.UpdateWriter
	session *serverSession
	mailbox string
}

type idleConnections struct {
	mu      sync.Mutex
	writers map[string]idleConnection
}

var userConnects sync.Map
var userConnectsMu sync.Mutex

func (s *serverSession) Idle(w *imapserver.UpdateWriter, stop <-chan struct{}) error {
	if err := s.authorize(); err != nil {
		return err
	}
	userId := s.ctx.UserID
	logId := cast.ToString(s.ctx.GetValue(context.LogID))

	userConnectsMu.Lock()
	connectsAny, ok := userConnects.Load(userId)
	if !ok {
		connectsAny = &idleConnections{writers: map[string]idleConnection{}}
		userConnects.Store(userId, connectsAny)
	}
	connects := connectsAny.(*idleConnections)

	connects.mu.Lock()
	connects.writers[logId] = idleConnection{writer: w, mailbox: s.currentMailbox, session: s}
	connects.mu.Unlock()
	userConnectsMu.Unlock()

	<-stop

	userConnectsMu.Lock()
	connects.mu.Lock()
	delete(connects.writers, logId)
	if len(connects.writers) == 0 {
		userConnects.Delete(userId)
	}
	connects.mu.Unlock()
	userConnectsMu.Unlock()
	return nil
}

func IdleNotice(ctx *context.Context, userId int, email *models.Email) error {
	if userId == 0 || email == nil || email.Id == 0 {
		return nil
	}

	userConnectsMu.Lock()
	connectsAny, ok := userConnects.Load(userId)
	if !ok {
		userConnectsMu.Unlock()
		return nil
	}

	connects := connectsAny.(*idleConnections)
	connects.mu.Lock()
	idleConnects := make([]idleConnection, 0, len(connects.writers))
	for _, connect := range connects.writers {
		idleConnects = append(idleConnects, connect)
	}
	connects.mu.Unlock()
	userConnectsMu.Unlock()

	for _, connect := range idleConnects {
		if connect.session.authorize() == nil {
			connect.session.pollMailbox(connect.writer, true, connect.mailbox)
		}
	}
	return nil
}
