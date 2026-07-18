package linuxserver

import (
	"bytes"
	"testing"
)

func TestSafeOutputParserRedactsJSONAndTextSecrets(t *testing.T) {
	parser := SafeOutputParser{}
	jsonData, _, err := parser.Parse("test", `{"user":"ops","token":"abc","nested":{"password":"xyz"}}`, false)
	if err != nil || bytes.Contains(jsonData, []byte("abc")) || bytes.Contains(jsonData, []byte("xyz")) {
		t.Fatalf("JSON parse = %s, error = %v", jsonData, err)
	}
	textData, warnings, err := parser.Parse("test", "Authorization: Bearer top-secret\nnormal text", false)
	if err != nil || len(warnings) == 0 || bytes.Contains(textData, []byte("top-secret")) {
		t.Fatalf("text parse = %s, warnings = %v, error = %v", textData, warnings, err)
	}
}
