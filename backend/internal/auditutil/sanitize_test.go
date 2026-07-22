package auditutil

import (
	"strings"
	"testing"
)

func TestSanitizeJSONRedactsSensitiveKeysAndAssignments(t *testing.T) {
	raw := []byte(`{"password":"top-secret","hostKeyStatus":"pending","evidenceKey":"ev-1","message":"authorization: Bearer abc.def and pwd=hidden","nested":{"token":"response-token","stdout":"raw-command-secret","argv":["sh","-c"]}}`)
	output := string(SanitizeJSON(raw, 0))
	for _, secret := range []string{"top-secret", "abc.def", "hidden", "response-token", "raw-command-secret", `"sh"`} {
		if strings.Contains(output, secret) {
			t.Fatalf("sanitized JSON leaked %q: %s", secret, output)
		}
	}
	if !strings.Contains(output, RedactedValue) {
		t.Fatalf("sanitized JSON has no redaction marker: %s", output)
	}
	for _, diagnostic := range []string{`"hostKeyStatus":"pending"`, `"evidenceKey":"ev-1"`} {
		if !strings.Contains(output, diagnostic) {
			t.Fatalf("sanitized JSON hid diagnostic field %s: %s", diagnostic, output)
		}
	}
}
