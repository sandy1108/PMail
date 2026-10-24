package imap_server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"github.com/Jinnrry/pmail/config"
	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/dto/parsemail"
	"github.com/Jinnrry/pmail/models"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-message"
	"io"
	"net/mail"
	"time"
	"xorm.io/xorm"
)

// APPEND仅导入当前账号的邮箱，不经过SMTP投递，不触发任何外发。
func (s *serverSession) Append(mailbox string, r imap.LiteralReader, options *imap.AppendOptions) (*imap.AppendData, error) {
	if err := s.authorize(); err != nil {
		return nil, err
	}
	gid, status, valid, err := s.mailboxTarget(mailbox)
	if err != nil {
		return nil, err
	}
	const limit = 25 * 1024 * 1024
	if r.Size() > limit {
		return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Message exceeds 25 MiB limit"}
	}
	raw, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > limit {
		return nil, mailboxError()
	}
	if _, err = mail.ReadMessage(bytes.NewReader(raw)); err != nil {
		return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Invalid message"}
	}
	if entity, err := message.Read(bytes.NewReader(raw)); entity == nil || (err != nil && !message.IsUnknownCharset(err)) {
		return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Invalid MIME message"}
	}
	parsed := parsemail.NewEmailFromReader(nil, bytes.NewReader(raw), len(raw))
	if parsed == nil || parsed.From == nil {
		return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Message sender required"}
	}
	js := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	if parsed.MsgID == "" {
		parsed.MsgID = parsemail.GenerateMsgID(config.Instance.Domain)
	}
	date := time.Now()
	if options != nil && !options.Time.IsZero() {
		date = options.Time
	}
	sendDate, err := time.Parse(time.RFC3339, parsed.Date)
	if err != nil {
		sendDate = date
	}
	e := models.Email{Subject: parsed.Subject, FromName: parsed.From.Name, FromAddress: parsed.From.EmailAddress, ReplyTo: js(parsed.ReplyTo), To: js(parsed.To), Cc: js(parsed.Cc), Bcc: js(parsed.Bcc), Sender: js(parsed.Sender), Attachments: js(parsed.Attachments), Text: sql.NullString{String: string(parsed.Text), Valid: true}, Html: sql.NullString{String: string(parsed.HTML), Valid: true}, Size: len(raw), MsgID: parsed.MsgID, CreateTime: date, SendDate: sendDate, UpdateTime: time.Now(), Status: status}
	relation := models.UserEmail{UserID: s.ctx.UserID, GroupId: gid, Status: status, Created: date}
	if options != nil {
		for _, f := range options.Flags {
			switch f {
			case imap.FlagSeen:
				relation.IsRead = 1
			case imap.FlagDeleted:
				relation.IMAPDeleted = true
			default:
				return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Unsupported flag"}
			}
		}
	}
	err = db.Transaction(db.Instance, func(tx *xorm.Session) error {
		// 禁止ORM自动覆盖客户端传入的INTERNALDATE。
		if _, err := tx.NoAutoTime().Insert(&e); err != nil {
			return err
		}
		relation.EmailID = e.Id
		_, err := tx.Insert(&relation)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &imap.AppendData{UID: imap.UID(relation.ID), UIDValidity: valid}, nil
}
