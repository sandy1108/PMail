package password

import (
	"strings"
	"testing"
)

func TestSaltAndVerification(t *testing.T) {
	a, b := Encode("test-password"), Encode("test-password")
	if a == b {
		t.Fatal("password salts reused")
	}
	if !Verify(a, "test-password") || Verify(a, "wrong-password") {
		t.Fatal("verification failure")
	}
	for _, invalid := range []string{"", "d41d8cd98f00b204e9800998ecf8427e", strings.Replace(a, "m=32768", "m=999999999", 1), prefix + "invalid$invalid", a + "$more"} {
		if Verify(invalid, "test-password") {
			t.Fatal("accepted malformed or legacy hash")
		}
	}
	if Verify(a, strings.Repeat("a", 4097)) {
		t.Fatal("accepted oversized password")
	}
}
