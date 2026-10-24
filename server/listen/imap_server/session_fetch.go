package imap_server

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/Jinnrry/pmail/config"
	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/dto/parsemail"
	"github.com/Jinnrry/pmail/dto/response"
	"github.com/Jinnrry/pmail/models"
	"github.com/Jinnrry/pmail/utils/context"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
)

// userToAddress 将 parsemail.User 转换为 imap.Address
func userToAddress(u *parsemail.User) imap.Address {
	if u == nil {
		return imap.Address{}
	}
	// 解析邮箱地址的 local 和 domain 部分
	mailbox := u.EmailAddress
	host := ""
	if at := strings.LastIndex(u.EmailAddress, "@"); at >= 0 {
		mailbox = u.EmailAddress[:at]
		host = u.EmailAddress[at+1:]
	}
	return imap.Address{
		Name:    u.Name,
		Mailbox: mailbox,
		Host:    host,
	}
}

// usersToAddresses 将用户列表转换为地址列表
func usersToAddresses(users []*parsemail.User) []imap.Address {
	if len(users) == 0 {
		return nil
	}
	addrs := make([]imap.Address, 0, len(users))
	for _, u := range users {
		if u != nil && u.EmailAddress != "" {
			addrs = append(addrs, userToAddress(u))
		}
	}
	if len(addrs) == 0 {
		return nil
	}
	return addrs
}

// buildEnvelope 构建 IMAP ENVELOPE 结构
func buildEnvelope(email *response.EmailResponseData, traEmail *parsemail.Email) *imap.Envelope {
	// From 地址
	var from []imap.Address
	if email.FromAddress != "" {
		mailbox := email.FromAddress
		host := ""
		if at := strings.LastIndex(email.FromAddress, "@"); at >= 0 {
			mailbox = email.FromAddress[:at]
			host = email.FromAddress[at+1:]
		}
		from = []imap.Address{{
			Name:    email.FromName,
			Mailbox: mailbox,
			Host:    host,
		}}
	}

	// Sender (如果没有单独的 sender，使用 from)
	var sender []imap.Address
	if traEmail.Sender != nil && traEmail.Sender.EmailAddress != "" {
		sender = []imap.Address{userToAddress(traEmail.Sender)}
	} else {
		sender = from
	}

	// Reply-To
	var replyTo []imap.Address
	if len(traEmail.ReplyTo) > 0 {
		replyTo = usersToAddresses(traEmail.ReplyTo)
	} else {
		replyTo = from
	}

	// Message-ID: use the persisted value so IMAP clients see a stable ID on every sync.
	var messageID string
	if email.MsgID != "" {
		messageID = fmt.Sprintf("<%s>", email.MsgID)
	} else {
		messageID = fmt.Sprintf("<%d@%s>", email.Id, config.Instance.Domain)
	}

	return &imap.Envelope{
		Date:      email.CreateTime,
		Subject:   email.Subject,
		From:      from,
		Sender:    sender,
		ReplyTo:   replyTo,
		To:        usersToAddresses(traEmail.To),
		Cc:        usersToAddresses(traEmail.Cc),
		Bcc:       usersToAddresses(traEmail.Bcc),
		MessageID: messageID,
		// InReplyTo 和 References 暂不支持
	}
}

func (s *serverSession) Fetch(w *imapserver.FetchWriter, numSet imap.NumSet, options *imap.FetchOptions) error {
	if err := s.authorize(); err != nil {
		return err
	}
	emails, err := s.selectedEmails(numSet)
	if err != nil {
		return err
	}
	return write(s.ctx, w, emails, options)
}

// 以实际重建MIME为准解析BODYSTRUCTURE、分段和partial，避免附件结构与返回内容不一致。
func write(ctx *context.Context, w *imapserver.FetchWriter, emails []*response.EmailResponseData, options *imap.FetchOptions) error {
	for _, email := range emails {
		parsed := parsemail.NewEmailFromModel(email.Email)
		raw := parsed.BuildBytes(ctx, false)
		if !ctx.ReadOnly {
			for _, section := range options.BodySection {
				if !section.Peek {
					if _, err := db.Instance.ID(email.UeId).Where("user_id=?", ctx.UserID).Cols("is_read").Update(&models.UserEmail{IsRead: 1}); err != nil {
						return err
					}
					email.IsRead = 1
					break
				}
			}
		}
		msg := w.CreateMessage(uint32(email.SerialNumber))
		if options.UID {
			msg.WriteUID(imap.UID(email.UeId))
		}
		if options.Envelope {
			msg.WriteEnvelope(buildEnvelope(email, parsed))
		}
		if options.BodyStructure != nil {
			msg.WriteBodyStructure(imapserver.ExtractBodyStructure(bytes.NewReader(raw)))
		}
		if options.RFC822Size {
			msg.WriteRFC822Size(int64(len(raw)))
		}
		if options.Flags {
			var fs []imap.Flag
			if email.IsRead == 1 {
				fs = append(fs, imap.FlagSeen)
			}
			if email.IMAPDeleted {
				fs = append(fs, imap.FlagDeleted)
			}
			msg.WriteFlags(fs)
		}
		if options.InternalDate {
			msg.WriteInternalDate(email.CreateTime)
		}
		for _, section := range options.BodySection {
			body := imapserver.ExtractBodySection(bytes.NewReader(raw), section)
			writer := msg.WriteBodySection(section, int64(len(body)))
			if _, err := writer.Write(body); err != nil {
				return err
			}
			if err := writer.Close(); err != nil {
				return err
			}
		}
		if err := msg.Close(); err != nil {
			return err
		}
	}
	return nil
}
