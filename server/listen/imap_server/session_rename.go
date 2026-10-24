package imap_server

import (
	"github.com/Jinnrry/pmail/services/group"
	"github.com/emersion/go-imap/v2"
)

func (s *serverSession) Rename(mailbox, newName string) error {
	if err := s.authorize(); err != nil {
		return err
	}
	if group.IsDefaultBox(mailbox) {
		return &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Text: "This mailbox does not support rename.",
		}
	}

	err := group.Rename(s.ctx, mailbox, newName)

	if err != nil {
		return &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Text: err.Error(),
		}
	}
	return nil
}
