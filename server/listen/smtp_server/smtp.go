package smtp_server

import (
	"crypto/tls"
	"errors"
	"net"
	"strconv"
	"time"

	"github.com/Jinnrry/pmail/config"
	"github.com/Jinnrry/pmail/services/protocolsecurity"
	"github.com/emersion/go-smtp"
	log "github.com/sirupsen/logrus"
)

var instance *smtp.Server
var instanceTls *smtp.Server

// newSubmissionServer只用于隐式TLS提交，与入站SMTP完全隔离，不创建587监听。
func newSubmissionServer() *smtp.Server {
	s := smtp.NewServer(&Backend{Submission: true})
	s.Domain = config.Instance.SMTPHostname
	s.ReadTimeout = 30 * time.Second
	s.WriteTimeout = 30 * time.Second
	s.MaxMessageBytes = 30 * 1024 * 1024
	s.MaxRecipients = 50
	s.AllowInsecureAuth = false
	// TLS在预算监听器外包装；不设置STARTTLS能力，TLS从连接开始即生效。
	return s
}
func StartWithTLS() {
	cer, err := tls.LoadX509KeyPair(config.Instance.SSLPublicKeyPath, config.Instance.SSLPrivateKeyPath)
	if err != nil {
		log.Error("Submission certificate: ", err)
		return
	}
	listener, err := protocolsecurity.ListenTLS(config.Instance.SubmissionBind, config.Instance.SubmissionPort, cer)
	if err != nil {
		log.Error("Submission listener: ", err)
		return
	}
	defer listener.Close()
	instanceTls = newSubmissionServer()
	if err = instanceTls.Serve(listener); err != nil && !errors.Is(err, smtp.ErrServerClosed) {
		log.Error(err)
	}
}

func Start() {
	be := &Backend{}

	instance = smtp.NewServer(be)

	instance.Addr = net.JoinHostPort(config.Instance.SMTPBind, strconv.Itoa(config.Instance.SMTPPort))
	instance.Domain = config.Instance.SMTPHostname
	instance.ReadTimeout = 10 * time.Second
	instance.WriteTimeout = 10 * time.Second
	instance.MaxMessageBytes = 1024 * 1024 * 30
	instance.MaxRecipients = 50
	// force TLS for auth
	instance.AllowInsecureAuth = false
	// Load the certificate and key
	cer, err := tls.LoadX509KeyPair(config.Instance.SSLPublicKeyPath, config.Instance.SSLPrivateKeyPath)
	if err != nil {
		log.Fatal(err)
		return
	}
	// Configure the TLS support
	instance.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cer}, MinVersion: tls.VersionTLS12}

	log.Println("Starting Smtp Server Port:", instance.Addr)
	listener, err := net.Listen("tcp", instance.Addr)
	if err != nil {
		log.Fatal(err)
		return
	}
	if config.Instance.ProxyRequired {
		listener = proxyListener(listener, config.Instance.ProxyTrustedIPs, 3*time.Second)
	}
	if err := instance.Serve(listener); err != nil && !errors.Is(err, smtp.ErrServerClosed) {
		log.Error(err)
	}
}

func Stop() {
	if instance != nil {
		instance.Close()
	}
	if instanceTls != nil {
		instanceTls.Close()
	}

}
