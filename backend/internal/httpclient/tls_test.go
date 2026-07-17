package httpclient

import (
	"crypto/tls"
	"net/http"
	"testing"
)

func TestWithInsecureTLSIsExplicitAndDoesNotMutateBaseClient(t *testing.T) {
	baseTransport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13}}
	base := &http.Client{Transport: baseTransport}

	configured, ok := WithInsecureTLS(base, true).(*http.Client)
	if !ok || configured == base {
		t.Fatal("expected an isolated http client")
	}
	configuredTransport, ok := configured.Transport.(*http.Transport)
	if !ok || configuredTransport == baseTransport {
		t.Fatal("expected an isolated http transport")
	}
	if !configuredTransport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("insecure TLS setting was not applied")
	}
	if baseTransport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("WithInsecureTLS mutated the base transport")
	}
	if WithInsecureTLS(base, false) != base {
		t.Fatal("disabled setting should return the base client")
	}
}
