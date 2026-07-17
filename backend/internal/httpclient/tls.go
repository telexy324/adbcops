package httpclient

import (
	"crypto/tls"
	"net/http"
)

type Doer interface {
	Do(request *http.Request) (*http.Response, error)
}

// WithInsecureTLS builds an isolated client for a data source. The shared
// client and transport remain unchanged for every other outbound request.
func WithInsecureTLS(client Doer, insecure bool) Doer {
	if !insecure {
		return client
	}
	httpClient, ok := client.(*http.Client)
	if !ok {
		return client
	}
	clonedClient := *httpClient
	transport := httpClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	httpTransport, ok := transport.(*http.Transport)
	if !ok {
		return client
	}
	clonedTransport := httpTransport.Clone()
	if clonedTransport.TLSClientConfig == nil {
		clonedTransport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	} else {
		clonedTransport.TLSClientConfig = clonedTransport.TLSClientConfig.Clone()
	}
	clonedTransport.TLSClientConfig.InsecureSkipVerify = true // #nosec G402 -- explicitly enabled per trusted data source.
	clonedClient.Transport = clonedTransport
	return &clonedClient
}
