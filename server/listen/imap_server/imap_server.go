package imap_server

import (
	"crypto/tls"
	"github.com/Jinnrry/pmail/config"
	"github.com/Jinnrry/pmail/services/protocolsecurity"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	log "github.com/sirupsen/logrus"
)

var instanceTLS *imapserver.Server

func Stop() {
	if instanceTLS != nil {
		instanceTLS.Close()
		instanceTLS = nil
	}
}

// StarTLS 启动TLS端口监听，不加密的代码就懒得写了
func StarTLS() {
	instanceTLS = newIMAPServer()
	crt, err := tls.LoadX509KeyPair(config.Instance.SSLPublicKeyPath, config.Instance.SSLPrivateKeyPath)
	if err != nil {
		panic(err)
	}
	ln, err := protocolsecurity.ListenTLS(config.Instance.IMAPBind, config.Instance.IMAPPort, crt)
	if err != nil {
		panic(err)
	}
	log.Infof("IMAP TLS listening on %s:%d", config.Instance.IMAPBind, config.Instance.IMAPPort)
	if err := instanceTLS.Serve(ln); err != nil {
		panic(err)
	}
}

func newIMAPServer() *imapserver.Server {
	crt, err := tls.LoadX509KeyPair(config.Instance.SSLPublicKeyPath, config.Instance.SSLPrivateKeyPath)
	if err != nil {
		panic(err)
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{crt}, MinVersion: tls.VersionTLS12,
	}

	memServer := NewServer()

	option := &imapserver.Options{
		NewSession: func(conn *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			s := memServer.NewSession().(*serverSession)
			s.auth = protocolsecurity.NewSession("imap", func() { conn.NetConn().Close() })
			return s, nil, nil
		},
		Caps: imap.CapSet{
			imap.CapIMAP4rev1: {},
			imap.CapIdle:      {},
		},
		TLSConfig:    tlsConfig,
		InsecureAuth: false,
	}

	return imapserver.New(option)
}
