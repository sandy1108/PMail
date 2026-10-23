package smtp_server

import (
	"errors"
	"github.com/pires/go-proxyproto"
	"net"
	"time"
)

// 只相信显式配置的直接连接方，并强制携带v2 TCP PROXY头。
// 解析延迟到每个连接自己的读写，慢连接不会阻塞Listener.Accept。
func proxyListener(base net.Listener, trusted []string, timeout time.Duration) net.Listener {
	ips := make([]net.IP, 0, len(trusted))
	for _, addr := range trusted {
		ips = append(ips, net.ParseIP(addr))
	}
	return &proxyproto.Listener{
		Listener:          base,
		ConnPolicy:        proxyproto.TrustProxyHeaderFrom(ips...),
		ReadHeaderTimeout: timeout,
		ValidateHeader: func(h *proxyproto.Header) error {
			if h.Version != 2 || h.Command != proxyproto.PROXY || (h.TransportProtocol != proxyproto.TCPv4 && h.TransportProtocol != proxyproto.TCPv6) {
				return errors.New("only PROXYv2 TCP is accepted")
			}
			return nil
		},
	}
}
