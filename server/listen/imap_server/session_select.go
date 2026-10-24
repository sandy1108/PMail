package imap_server

import (
	"github.com/emersion/go-imap/v2"
	"strings"
)

func (s *serverSession) Select(mailbox string, options *imap.SelectOptions) (*imap.SelectData, error) {
	if err := s.authorize(); err != nil {
		return nil, err
	}
	rows, valid, err := s.mailboxRows(mailbox)
	if err != nil {
		return nil, err
	}
	s.readOnly = options != nil && options.ReadOnly
	s.ctx.ReadOnly = s.readOnly
	if strings.EqualFold(mailbox, "INBOX") {
		mailbox = "INBOX"
	}
	s.mailboxMu.Lock()
	s.currentMailbox = mailbox
	s.mailboxUIDs = nil
	for _, r := range rows {
		s.mailboxUIDs = append(s.mailboxUIDs, r.ID)
	}
	s.mailboxMu.Unlock()
	// 全局单调关系ID上界，避免删除末封后UIDNEXT倒退。
	next, err := nextUID()
	if err != nil {
		return nil, err
	}

	return &imap.SelectData{Flags: []imap.Flag{imap.FlagSeen, imap.FlagDeleted}, PermanentFlags: []imap.Flag{imap.FlagSeen, imap.FlagDeleted}, NumMessages: uint32(len(rows)), UIDNext: next, UIDValidity: valid}, nil
}
