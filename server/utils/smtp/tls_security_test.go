package smtp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDialTLSRejectsUntrustedCertificate(t *testing.T) {
	server := httptest.NewTLSServer(http.NotFoundHandler())
	defer server.Close()
	client, err := DialTls(strings.TrimPrefix(server.URL, "https://"), "example.com", "example.com")
	if err == nil {
		client.Close()
		t.Fatal("untrusted certificate accepted")
	}
	if !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("unexpected failure: %v", err)
	}
}
