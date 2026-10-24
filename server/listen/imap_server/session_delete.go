package imap_server

import (
	"github.com/Jinnrry/pmail/services/group"
	"github.com/emersion/go-imap/v2"
)

func (s *serverSession) Delete(mailbox string) error {
	if err := s.authorize(); err != nil {
		return err
	}
	if group.IsDefaultBox(mailbox) {
		return mailboxError()
	}
	groupInfo, err := group.GetGroupByFullPath(s.ctx, mailbox)
	if err == nil && groupInfo.ID == 0 {
		return mailboxError()
	}

	if err != nil {
		return &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Text: err.Error(),
		}
	}
	_, err = group.DelGroup(s.ctx, groupInfo.ID)
	if err != nil {
		return &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Text: err.Error(),
		}
	}

	return nil
}
