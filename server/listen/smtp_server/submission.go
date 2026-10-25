package smtp_server

import (
	"bytes"
	"net/mail"
	"strings"

	"github.com/Jinnrry/pmail/dto/parsemail"
	"github.com/Jinnrry/pmail/utils/context"
	"github.com/emersion/go-smtp"
)

// 信头和信封分别验证，拒绝多From、重复From及冒用Sender，管理员也不例外。
func (s *Session) validateSubmissionHeaders(raw []byte) error {
	message, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return &smtp.SMTPError{Code: 554, Message: "Invalid message headers"}
	}
	for _, key := range []string{"From", "Sender"} {
		values := message.Header[key]
		if key == "Sender" && len(values) == 0 {
			continue
		}
		if len(values) != 1 {
			return &smtp.SMTPError{Code: 553, Message: "One owned sender required"}
		}
		addresses, err := mail.ParseAddressList(values[0])
		if err != nil || len(addresses) != 1 || !s.ownsSender(addresses[0].Address) {
			return &smtp.SMTPError{Code: 553, Message: "Sender does not belong to authenticated account"}
		}
	}
	return nil
}

// 实际投递只使用RCPT信封收件人，不能让To/Cc/Bcc绕过认证或收件人数限制。
func (s *Session) submitEmail(email *parsemail.Email, sender func(*context.Context, *parsemail.Email, []*parsemail.User, string) (error, map[string]error)) error {
	if err := s.checkSubmission(); err != nil {
		return err
	}
	if email == nil || email.From == nil || !s.ownsSender(email.From.EmailAddress) || !s.ownsSender(s.From) {
		return &smtp.SMTPError{Code: 553, Message: "Sender does not belong to authenticated account"}
	}
	if email.Sender != nil && !s.ownsSender(email.Sender.EmailAddress) {
		return &smtp.SMTPError{Code: 553, Message: "Invalid Sender header"}
	}
	// BuildBytes不输出Bcc信头；保留邮件对象中的密送信息供已发送审计。
	recipients := make([]*parsemail.User, 0, len(s.To))
	for _, address := range s.To {
		recipients = append(recipients, &parsemail.User{EmailAddress: address})
	}
	if len(recipients) == 0 {
		return &smtp.SMTPError{Code: 503, Message: "RCPT required"}
	}
	envelopeFrom := strings.ToLower(s.From)
	return s.deliverOutgoingEmail(email, func(ctx *context.Context, e *parsemail.Email) (error, map[string]error) {
		if err := s.checkSubmission(); err != nil {
			return err, nil
		}
		return sender(ctx, e, recipients, envelopeFrom)
	})
}
