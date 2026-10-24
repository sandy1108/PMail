package imap_server

import (
	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/models"
	"github.com/emersion/go-imap/v2"
	"xorm.io/xorm"
)

func (s *serverSession) Copy(set imap.NumSet, dest string) (*imap.CopyData, error) {
	if err := s.authorize(); err != nil {
		return nil, err
	}
	data, _, err := s.copyOrMove(set, dest, false)
	return data, err
}

// 复制与移动都按当前邮箱的关系ID操作，避免同一正文的其他副本被一起移动。
func (s *serverSession) copyOrMove(set imap.NumSet, dest string, move bool) (*imap.CopyData, []uint32, error) {
	gid, status, valid, err := s.mailboxTarget(dest)
	if err != nil {
		return nil, nil, err
	}
	rows, seqs, err := s.selectedRows(set)
	if err != nil {
		return nil, nil, err
	}
	data := &imap.CopyData{UIDValidity: valid}
	err = db.Transaction(db.Instance, func(tx *xorm.Session) error {
		for _, r := range rows {
			copy := models.UserEmail{UserID: s.ctx.UserID, EmailID: r.EmailID, IsRead: r.IsRead, GroupId: gid, Status: status, DeliveryAddresses: r.DeliveryAddresses}
			if _, err := tx.Insert(&copy); err != nil {
				return err
			}
			data.SourceUIDs.AddNum(imap.UID(r.ID))
			data.DestUIDs.AddNum(imap.UID(copy.ID))
			if move {
				if _, err := tx.ID(r.ID).Where("user_id=?", s.ctx.UserID).Delete(&models.UserEmail{}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if move {
		ids := map[int]bool{}
		for _, r := range rows {
			ids[r.ID] = true
		}
		s.forgetUIDs(ids)
	}
	return data, seqs, nil
}
