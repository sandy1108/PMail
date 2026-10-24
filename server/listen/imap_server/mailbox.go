package imap_server

import (
	"github.com/Jinnrry/pmail/config"
	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/dto/response"
	"github.com/Jinnrry/pmail/models"
	"github.com/Jinnrry/pmail/services/group"
	"github.com/emersion/go-imap/v2"
	"strings"
)

func mailboxError() error {
	return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Mailbox unavailable"}
}

// 邮箱路径按当前用户解析，绝不把正文ID用作邮件关系UID。
func (s *serverSession) mailboxTarget(name string) (int, int8, uint32, error) {
	if strings.EqualFold(name, "INBOX") {
		name = "INBOX"
	}
	if code, ok := models.GroupNameToCode[name]; ok {
		statuses := map[string]int8{"INBOX": 0, "Sent Messages": 1, "Drafts": 4, "Deleted Messages": 3, "Junk": 5}
		return 0, statuses[name], uint32(code), nil
	}
	g, err := group.GetGroupByFullPath(s.ctx, name)
	if err != nil || g.ID == 0 {
		return 0, 0, 0, mailboxError()
	}
	return g.ID, 0, uint32(g.ID), nil
}
func (s *serverSession) mailboxRows(name string) ([]models.UserEmail, uint32, error) {
	gid, status, valid, err := s.mailboxTarget(name)
	if err != nil {
		return nil, 0, err
	}
	var rows []models.UserEmail
	q := db.Instance.Where("user_id=? AND group_id=?", s.ctx.UserID, gid)
	if gid == 0 {
		q = q.And("status=?", status)
	}
	err = q.Asc("id").Find(&rows)
	return rows, valid, err
}

// 用有界邮箱记录匹配集合，不展开客户端传入的巨大UID范围；*解析为当前最大值。
func matchesNumbers(set imap.NumSet, seq, uid, maxSeq, maxUID uint32) bool {
	switch nums := set.(type) {
	case imap.SeqSet:
		for _, r := range nums {
			a, b := r.Start, r.Stop
			if a == 0 {
				a = maxSeq
			}
			if b == 0 {
				b = maxSeq
			}
			if a > b {
				a, b = b, a
			}
			if seq >= a && seq <= b {
				return true
			}
		}
	case imap.UIDSet:
		for _, r := range nums {
			a, b := uint32(r.Start), uint32(r.Stop)
			if a == 0 {
				a = maxUID
			}
			if b == 0 {
				b = maxUID
			}
			if a > b {
				a, b = b, a
			}
			if uid >= a && uid <= b {
				return true
			}
		}
	}
	return false
}
func (s *serverSession) selectedRows(set imap.NumSet) ([]models.UserEmail, []uint32, error) {
	s.mailboxMu.Lock()
	name := s.currentMailbox
	ids := append([]int(nil), s.mailboxUIDs...)
	s.mailboxMu.Unlock()
	rows, _, err := s.mailboxRows(name)
	if err != nil {
		return nil, nil, err
	}
	byID := map[int]models.UserEmail{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	var out []models.UserEmail
	var seqs []uint32
	var maxUID uint32
	if len(ids) > 0 {
		maxUID = uint32(ids[len(ids)-1])
	}
	for i, id := range ids {
		r, exists := byID[id]
		if exists && (set == nil || matchesNumbers(set, uint32(i+1), uint32(id), uint32(len(ids)), maxUID)) {
			out = append(out, r)
			seqs = append(seqs, uint32(i+1))
		}
	}
	return out, seqs, nil
}

// 序号在SELECT后固定到关系UID；其他会话删除不能让本次命令悄悄指向下一封邮件。
func (s *serverSession) forgetUIDs(ids map[int]bool) {
	s.mailboxMu.Lock()
	defer s.mailboxMu.Unlock()
	kept := s.mailboxUIDs[:0]
	for _, id := range s.mailboxUIDs {
		if !ids[id] {
			kept = append(kept, id)
		}
	}
	s.mailboxUIDs = kept
}
func (s *serverSession) selectedEmails(set imap.NumSet) ([]*response.EmailResponseData, error) {
	rows, seqs, err := s.selectedRows(set)
	if err != nil {
		return nil, err
	}
	var out []*response.EmailResponseData
	for i, r := range rows {
		var e models.Email
		ok, err := db.Instance.ID(r.EmailID).Get(&e)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		out = append(out, &response.EmailResponseData{Email: e, IsRead: r.IsRead, UeId: r.ID, SerialNumber: int(seqs[i]), IMAPDeleted: r.IMAPDeleted})
	}
	return out, nil
}

// SQLite的自增序列在删除最高UID后仍保留上界，不能用当前MAX(id)代替。
func nextUID() (imap.UID, error) {
	var last models.UserEmail
	if _, err := db.Instance.Desc("id").Get(&last); err != nil {
		return 0, err
	}
	high := last.ID
	if config.Instance.DbType == config.DBTypeSQLite {
		var seq struct{ Seq int }
		if _, err := db.Instance.SQL("SELECT seq FROM sqlite_sequence WHERE name='user_email'").Get(&seq); err != nil {
			return 0, err
		}
		if seq.Seq > high {
			high = seq.Seq
		}
	}
	return imap.UID(high + 1), nil
}
