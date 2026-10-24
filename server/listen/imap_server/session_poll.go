package imap_server

import "github.com/emersion/go-imap/v2/imapserver"

func (s *serverSession) Poll(w *imapserver.UpdateWriter, allowExpunge bool) error {
	if s.status == UNAUTHORIZED {
		return nil
	}
	if err := s.authorize(); err != nil {
		return err
	}
	s.mailboxMu.Lock()
	mailbox := s.currentMailbox
	s.mailboxMu.Unlock()
	if mailbox == "" {
		return nil
	}
	return s.pollMailbox(w, allowExpunge, mailbox)
}

// 先通知EXPUNGE再调整序号；FETCH/STORE期间不得发送使序号失效的更新。
func (s *serverSession) pollMailbox(w *imapserver.UpdateWriter, allowExpunge bool, mailbox string) error {
	rows, _, err := s.mailboxRows(mailbox)
	if err != nil {
		return err
	}
	s.mailboxMu.Lock()
	defer s.mailboxMu.Unlock()
	if s.currentMailbox != mailbox {
		return nil
	}
	exists := map[int]bool{}
	for _, r := range rows {
		exists[r.ID] = true
	}
	if allowExpunge {
		for i := len(s.mailboxUIDs) - 1; i >= 0; i-- {
			if !exists[s.mailboxUIDs[i]] {
				if err = w.WriteExpunge(uint32(i + 1)); err != nil {
					return err
				}
				s.mailboxUIDs = append(s.mailboxUIDs[:i], s.mailboxUIDs[i+1:]...)
			}
		}
	}
	known := map[int]bool{}
	for _, id := range s.mailboxUIDs {
		known[id] = true
	}
	old := len(s.mailboxUIDs)
	for _, r := range rows {
		if !known[r.ID] {
			s.mailboxUIDs = append(s.mailboxUIDs, r.ID)
		}
	}
	if len(s.mailboxUIDs) != old {
		return w.WriteNumMessages(uint32(len(s.mailboxUIDs)))
	}
	return nil
}
