package imap_server

import (
	"github.com/emersion/go-imap/v2"
)

func (s *serverSession) Namespace() (*imap.NamespaceData, error) {
	if err := s.authorize(); err != nil {
		return nil, err
	}
	return &imap.NamespaceData{
		Personal: []imap.NamespaceDescriptor{
			{
				Prefix: "",
				Delim:  '/',
			},
		},
	}, nil
}
