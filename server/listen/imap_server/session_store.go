package imap_server

import (
	"github.com/Jinnrry/pmail/db"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"xorm.io/xorm"
)

func (s *serverSession) Store(w *imapserver.FetchWriter, set imap.NumSet, flags *imap.StoreFlags, options *imap.StoreOptions) error {
	if err := s.writable(); err != nil {
		return err
	}
	for _, f := range flags.Flags {
		if f != imap.FlagSeen && f != imap.FlagDeleted {
			return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Unsupported flag"}
		}
	}
	rows, seqs, err := s.selectedRows(set)
	if err != nil {
		return err
	}
	seen, deleted := false, false
	for _, f := range flags.Flags {
		if f == imap.FlagSeen {
			seen = true
		}
		if f == imap.FlagDeleted {
			deleted = true
		}
	}
	err = db.Transaction(db.Instance, func(tx *xorm.Session) error {
		for i := range rows {
			r := &rows[i]
			switch flags.Op {
			case imap.StoreFlagsSet:
				r.IsRead = 0
				r.IMAPDeleted = deleted
				if seen {
					r.IsRead = 1
				}
			case imap.StoreFlagsAdd:
				if seen {
					r.IsRead = 1
				}
				if deleted {
					r.IMAPDeleted = true
				}
			case imap.StoreFlagsDel:
				if seen {
					r.IsRead = 0
				}
				if deleted {
					r.IMAPDeleted = false
				}
			}
			if _, err := tx.ID(r.ID).Where("user_id=?", s.ctx.UserID).Cols("is_read", "imap_deleted").Update(r); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !flags.Silent {
		for i, r := range rows {
			msg := w.CreateMessage(seqs[i])
			if _, ok := set.(imap.UIDSet); ok {
				msg.WriteUID(imap.UID(r.ID))
			}
			var fs []imap.Flag
			if r.IsRead == 1 {
				fs = append(fs, imap.FlagSeen)
			}
			if r.IMAPDeleted {
				fs = append(fs, imap.FlagDeleted)
			}
			msg.WriteFlags(fs)
			if err := msg.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}
