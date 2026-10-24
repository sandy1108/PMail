package imap_server

import (
	"github.com/Jinnrry/pmail/services/protocolsecurity"
	"github.com/emersion/go-imap/v2"
)

// 客户端只接受真实邮箱及获准用途授权码，不能回退到网页密码。
func (s *serverSession) Login(username, pwd string) error {
	if s.auth == nil {
		s.auth = protocolsecurity.NewSession("imap", nil)
	}
	user, err := s.auth.Login(username, pwd)
	if err != nil {
		return authError()
	}
	s.ctx.UserID = user.ID
	s.ctx.UserName = user.Name
	s.ctx.UserAccount = user.Account
	s.status = AUTHORIZED
	return nil
}
func authError() error {
	return &imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCodeAuthenticationFailed, Text: "Invalid or inactive client credentials"}
}
func (s *serverSession) authorize() error {
	if s.auth == nil || s.auth.Check() != nil {
		return authError()
	}
	return nil
}
func (s *serverSession) writable() error {
	if err := s.authorize(); err != nil {
		return err
	}
	if s.readOnly {
		return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Mailbox is read-only"}
	}
	return nil
}
