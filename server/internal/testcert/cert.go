// Package testcert仅为隔离协议测试生成临时证书，不用于部署。
package testcert

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

func Create(dir string) (tls.Certificate, *tls.Config, string, string, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, "", "", err
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}

	der, err := x509.CreateCertificate(rand.Reader, template, template, pub, priv)
	if err != nil {
		return tls.Certificate{}, nil, "", "", err
	}
	key, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return tls.Certificate{}, nil, "", "", err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err = os.WriteFile(certPath, certPEM, 0600); err != nil {
		return tls.Certificate{}, nil, "", "", err
	}
	if err = os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return tls.Certificate{}, nil, "", "", err
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(certPEM)
	return pair, &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS12}, certPath, keyPath, err
}
