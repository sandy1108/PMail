package imap_server

import (
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
)

func (s *serverSession) Move(w *imapserver.MoveWriter, set imap.NumSet, dest string) error {
	if err := s.writable(); err != nil {
		return err
	}
	data, seqs, err := s.copyOrMove(set, dest, true)
	if err != nil {
		return err
	}
	if len(data.SourceUIDs) > 0 {
		if err = w.WriteCopyData(data); err != nil {
			return err
		}
	}
	for i := len(seqs) - 1; i >= 0; i-- {
		if err = w.WriteExpunge(seqs[i]); err != nil {
			return err
		}
	}
	return nil
}
