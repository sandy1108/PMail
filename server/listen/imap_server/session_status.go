package imap_server

import (
	"github.com/emersion/go-imap/v2"
)

func (s *serverSession) Status(mailbox string, options *imap.StatusOptions) (*imap.StatusData, error) {
	if err := s.authorize(); err != nil {
		return nil, err
	}
	rows, valid, err := s.mailboxRows(mailbox)
	if err != nil {
		return nil, err
	}
	next, err := nextUID()
	if err != nil {
		return nil, err
	}

	count, unseen := uint32(len(rows)), uint32(0)
	for _, r := range rows {
		if r.IsRead == 0 {
			unseen++
		}
	}
	return &imap.StatusData{Mailbox: mailbox, NumMessages: &count, NumUnseen: &unseen, UIDNext: next, UIDValidity: valid}, nil
}
