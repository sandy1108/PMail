package imap_server

import (
	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/models"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"xorm.io/xorm"
)

func (s *serverSession) Expunge(w *imapserver.ExpungeWriter, uids *imap.UIDSet) error {
	if err := s.writable(); err != nil {
		return err
	}
	var set imap.NumSet
	if uids != nil {
		set = *uids
	}
	rows, seqs, err := s.selectedRows(set)
	if err != nil {
		return err
	}
	err = db.Transaction(db.Instance, func(tx *xorm.Session) error {
		for _, r := range rows {
			if !r.IMAPDeleted {
				continue
			}
			if _, err := tx.ID(r.ID).Where("user_id=? AND imap_deleted=?", s.ctx.UserID, true).Delete(&models.UserEmail{}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	deleted := map[int]bool{}
	for _, r := range rows {
		if r.IMAPDeleted {
			deleted[r.ID] = true
		}
	}
	s.forgetUIDs(deleted)
	// 逆序通知，较小的序号不会因前面删除而漂移。正文保留给其他关系和后续垃圾回收。
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].IMAPDeleted {
			if err = w.WriteExpunge(seqs[i]); err != nil {
				return err
			}
		}
	}
	return nil
}
