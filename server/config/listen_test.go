package config

import "testing"

func TestListenerValidation(t *testing.T) {
	c := &Config{}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.HTTPAddress(c.HttpPort) != "127.0.0.1:18080" || c.SMTPBind != "127.0.0.1" || c.SMTPPort != 2525 {
		t.Fatal("unsafe defaults")
	}
	for _, bad := range []*Config{{HTTPBind: "bad"}, {SMTPPort: -1}, {HttpPort: 65536}, {ProxyRequired: true}, {ProxyTrustedIPs: []string{"*"}}} {
		if bad.Validate() == nil {
			t.Fatal("accepted invalid config")
		}
	}
}

func TestCookieOriginBoundary(t *testing.T) {
	for _, tc := range []struct {
		bind, origin   string
		insecure, want bool
	}{
		{"127.0.0.1", "http://127.0.0.1:18081", true, true},
		{"127.0.0.1", "https://mail.example.com:8443", false, true},
		{"0.0.0.0", "http://127.0.0.1:18081", true, false},
		{"127.0.0.1", "http://mail.example.com:18081", true, false},
		{"127.0.0.1", "http://127.0.0.1:18081", false, false},
		{"127.0.0.1", "https://mail.example.com:8443/path", false, false},
	} {
		c := &Config{IsInit: true, HTTPBind: tc.bind, WebOrigin: tc.origin, InsecureLocalCookies: tc.insecure}
		if got := c.Validate() == nil; got != tc.want {
			t.Errorf("%+v accepted=%v", tc, got)
		}
	}
}

func TestSubmissionListenerDefaultsAndConflicts(t *testing.T) {
	c := &Config{EnableSubmission: true}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.SubmissionBind != "127.0.0.1" || c.SubmissionPort != 1465 {
		t.Fatal("unsafe submission defaults")
	}
	for _, bad := range []*Config{{EnableSubmission: true, SubmissionPort: 465}, {EnableSubmission: true, SubmissionBind: "localhost"}, {EnableSubmission: true, SubmissionPort: 2525}, {EnableSubmission: true, EnableIMAP: true, SubmissionPort: 1993}} {
		if bad.Validate() == nil {
			t.Fatal("unsafe or conflicting endpoint accepted")
		}
	}
}
