package smtp_server

import (
	"database/sql"
	"errors"
	"github.com/Jinnrry/pmail/config"
	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/models"
	"github.com/Jinnrry/pmail/utils/context"
	"github.com/Jinnrry/pmail/utils/id"
	"github.com/Jinnrry/pmail/utils/password"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	log "github.com/sirupsen/logrus"
	"net"
	"strings"
)

// The Backend implements SMTP server methods.
type Backend struct{}

func (bkd *Backend) NewSession(conn *smtp.Conn) (smtp.Session, error) {

	remoteAddress := conn.Conn().RemoteAddr()
	ctx := &context.Context{}
	ctx.SetValue(context.LogID, id.GenLogID())
	log.WithContext(ctx).Debugf("新SMTP连接")

	return &Session{
		RemoteAddress: remoteAddress,
		Ctx:           ctx,
	}, nil
}

// A Session is returned after EHLO.
type Session struct {
	RemoteAddress net.Addr
	User          string
	From          string
	To            []string
	Ctx           *context.Context
}

// AuthMechanisms returns a slice of available auth mechanisms
// supported in this example.
func (s *Session) AuthMechanisms() []string {
	if !config.Instance.SMTPAuthEnabled {
		return nil
	}
	return []string{sasl.Plain, sasl.Login}
}

// Auth is the handler for supported authenticators.
func (s *Session) Auth(mech string) (sasl.Server, error) {
	if !config.Instance.SMTPAuthEnabled {
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

func (s *Session) AuthPlain(username, pwd string) error {
	if !config.Instance.SMTPAuthEnabled {
		return errors.New("AUTH disabled")
	}

	s.User = username

	var user models.User

	infos := strings.Split(username, "@")
	if len(infos) > 1 {
		username = infos[0]
	}

	_, err := db.Instance.Where("account =? and disabled=0", username).Get(&user)
	if err != nil && err != sql.ErrNoRows {
		log.Errorf("%+v", err)
	}

	if user.ID > 0 && password.Verify(user.Password, pwd) {
		s.Ctx.UserAccount = user.Account
		s.Ctx.UserID = user.ID
		s.Ctx.UserName = user.Name
		s.Ctx.IsAdmin = user.IsAdmin == 1

		return nil
	}

	return errors.New("password error")
}

func (s *Session) Mail(from string, opts *smtp.MailOptions) error {
	log.WithContext(s.Ctx).Debugf("Mail Success %+v %+v", from, opts)
	s.From = from
	return nil
}

func (s *Session) Rcpt(to string, opts *smtp.RcptOptions) error {
	log.WithContext(s.Ctx).Debugf("Rcpt Success %+v", to)

	// 入站只接收本域已启用账号，拒绝外域中继和未知用户。
	parts := strings.Split(strings.ToLower(to), "@")
	if len(parts) != 2 {
		return &smtp.SMTPError{Code: 550, Message: "Invalid recipient"}
	}
	local := strings.EqualFold(parts[1], config.Instance.Domain)
	for _, domain := range config.Instance.Domains {
		if strings.EqualFold(parts[1], domain) {
			local = true
			break
		}
	}
	if !local {
		return &smtp.SMTPError{Code: 550, Message: "Relay denied"}
	}
	var user models.User
	found, err := db.Instance.Where("account=? and disabled=0", parts[0]).Get(&user)
	if err != nil {
		return &smtp.SMTPError{Code: 451, Message: "Temporary recipient lookup failure"}
	}
	if !found {
		return &smtp.SMTPError{Code: 550, Message: "Unknown recipient"}
	}
	s.To = append(s.To, to)
	return nil
}

func (s *Session) Reset() { s.From = ""; s.To = nil }

func (s *Session) Logout() error {
	return nil
}
