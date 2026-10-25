package smtp_server

import (
	"errors"
	"github.com/Jinnrry/pmail/services/mailaddress"
	"github.com/Jinnrry/pmail/services/protocolsecurity"
	"github.com/Jinnrry/pmail/utils/context"
	"github.com/Jinnrry/pmail/utils/id"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	log "github.com/sirupsen/logrus"
	"net"
	"net/mail"
	"strings"
)

// The Backend implements SMTP server methods.
type Backend struct{ Submission bool }

func (bkd *Backend) NewSession(conn *smtp.Conn) (smtp.Session, error) {

	remoteAddress := conn.Conn().RemoteAddr()
	ctx := &context.Context{}
	ctx.SetValue(context.LogID, id.GenLogID())
	log.WithContext(ctx).Debugf("新SMTP连接")

	s := &Session{
		RemoteAddress: remoteAddress,
		Ctx:           ctx,
		Submission:    bkd.Submission,
	}
	if bkd.Submission {
		state, ok := conn.TLSConnectionState()
		if !ok || !state.HandshakeComplete {
			return nil, errors.New("TLS required")
		}
		s.auth = protocolsecurity.NewSession("smtp", func() { conn.Close() })
	}
	return s, nil
}

// A Session is returned after EHLO.
type Session struct {
	Submission    bool
	auth          *protocolsecurity.Session
	RemoteAddress net.Addr
	User          string
	From          string
	To            []string
	Accepted      []mailaddress.Recipient
	Ctx           *context.Context
}

// AuthMechanisms returns a slice of available auth mechanisms
// supported in this example.
func (s *Session) AuthMechanisms() []string {
	if !s.Submission || s.auth == nil {
		return nil
	}
	return []string{sasl.Plain, sasl.Login}
}

// Auth is the handler for supported authenticators.
func (s *Session) Auth(mech string) (sasl.Server, error) {
	if !s.Submission || s.auth == nil {
		return nil, errors.New("AUTH disabled")
	}
	if mech == sasl.Plain {
		return sasl.NewPlainServer(func(identity, username, password string) error {
			return s.AuthPlain(username, password)
		}), nil
	}

	if mech == sasl.Login {
		return NewLoginServer(func(username, password string) error {
			return s.AuthPlain(username, password)
		}), nil
	}

	return nil, errors.New("Auth Not Supported")
}

// AuthPlain仅接受SMTP用途授权码；错误不泄露用户或授权码状态。
func (s *Session) AuthPlain(username, pwd string) error {
	if !s.Submission || s.auth == nil {
		return errors.New("AUTH disabled")
	}
	user, err := s.auth.Login(username, pwd)
	if err != nil {
		return &smtp.SMTPError{Code: 535, Message: "Invalid client credentials"}
	}
	s.User = user.Account
	s.Ctx.UserID = user.ID
	s.Ctx.UserAccount = user.Account
	s.Ctx.UserName = user.Name
	s.Ctx.IsAdmin = false
	return nil
}
func (s *Session) checkSubmission() error {
	if s.auth == nil || s.auth.Check() != nil {
		return &smtp.SMTPError{Code: 530, Message: "Authentication required or expired"}
	}
	return nil
}

// 仅允许真实账号在本站域下发信；收件别名不授予发信身份。
func (s *Session) ownsSender(address string) bool {
	r, err := mailaddress.Resolve(address)
	return err == nil && r.AliasID == 0 && r.User.ID == s.Ctx.UserID
}

func (s *Session) Mail(from string, opts *smtp.MailOptions) error {
	if s.Submission {
		if err := s.checkSubmission(); err != nil {
			return err
		}
		if !s.ownsSender(from) {
			return &smtp.SMTPError{Code: 553, Message: "Sender does not belong to authenticated account"}
		}
	}

	log.WithContext(s.Ctx).Debugf("Mail Success %+v %+v", from, opts)
	s.From = from
	return nil
}

func (s *Session) Rcpt(to string, opts *smtp.RcptOptions) error {
	if s.Submission {
		if err := s.checkSubmission(); err != nil {
			return err
		}
		address, err := mail.ParseAddress(to)
		if err != nil || address.Address != to || !strings.Contains(to, "@") {
			return &smtp.SMTPError{Code: 553, Message: "Invalid recipient"}
		}
		if s.From == "" {
			return &smtp.SMTPError{Code: 503, Message: "MAIL required"}
		}
		if parts := strings.Split(to, "@"); len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return &smtp.SMTPError{Code: 553, Message: "Invalid recipient"}
		}
		if mailaddress.IsLocalDomain(strings.Split(to, "@")[1]) {
			if _, err = mailaddress.Resolve(to); err != nil {
				return &smtp.SMTPError{Code: 550, Message: "Unknown local recipient"}
			}
		}
		s.To = append(s.To, to)
		return nil
	}

	log.WithContext(s.Ctx).Debugf("Rcpt Success %+v", to)

	// 只允许真实账号及已登记别名；保留归属快照供DATA事务复验。
	recipient, err := mailaddress.Resolve(to)
	if err != nil {
		if errors.Is(err, mailaddress.ErrRecipient) {
			return &smtp.SMTPError{Code: 550, Message: "Unknown recipient or relay denied"}
		}
		return &smtp.SMTPError{Code: 451, Message: "Temporary recipient lookup failure"}
	}
	s.To = append(s.To, recipient.Address)
	s.Accepted = append(s.Accepted, recipient)

	return nil
}

func (s *Session) Reset() { s.From = ""; s.To = nil; s.Accepted = nil }

func (s *Session) Logout() error {
	if s.auth != nil {
		s.auth.Close()
	}
	return nil
}
