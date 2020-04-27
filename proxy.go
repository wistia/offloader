package offloader

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	HeaderPrefix             = "Offload-"
	HeaderOffloadRequested   = HeaderPrefix + "Requested"
	HeaderRequestedMethod    = HeaderPrefix + "Method"
	HeaderRequestedUrl       = HeaderPrefix + "Url"
	HeaderForwardBody        = HeaderPrefix + "Forward-Body"
	HeaderCustomHeaderPrefix = HeaderPrefix + "X-"
)

var (
	ErrInvalidVerb = errors.New("unsupported verb")
	ErrMissingUrl  = errors.New("missing url")
)

// Handler offloads requests from an HTTP backend, modifying its behavior based on response headers that the backend
// returns. It's designed to be used with httputil.ReverseProxy as a ModifyResponse function.
func Handler(proxyResponse *http.Response) (err error) {
	// If offload hasn't been requested, then simply proxy the backend response to the client
	if _, ok := proxyResponse.Header[HeaderOffloadRequested]; !ok {
		return nil
	}

	// The response object is updated in place, so the proxy response is actually the backend response here
	proxyRequest, err := newProxyRequestFromBackendResponse(proxyResponse)
	if err != nil {
		return err
	}

	// TODO: What's a reasonable timeout? Should this be configurable by the backend too?
	c := &http.Client{
		Timeout: 30 * time.Second,
	}
	offloadResponse, err := c.Do(proxyRequest)
	if err != nil {
		return err
	}

	proxyResponse.Header = offloadResponse.Header
	proxyResponse.StatusCode = offloadResponse.StatusCode
	proxyResponse.Body = offloadResponse.Body

	return nil
}

func newProxyRequestFromBackendResponse(backendResponse *http.Response) (*http.Request, error) {
	method := strings.ToUpper(backendResponse.Header.Get(HeaderRequestedMethod))
	if !isSupportedMethod(method) {
		return nil, ErrInvalidVerb
	}

	url := backendResponse.Header.Get(HeaderRequestedUrl)
	if url == "" {
		return nil, ErrMissingUrl
	}

	var body io.Reader
	if _, ok := backendResponse.Header[HeaderForwardBody]; ok {
		body = backendResponse.Body
	}

	proxyRequest, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	prepareProxyRequestHeaders(proxyRequest, backendResponse)

	return proxyRequest, nil
}

func prepareProxyRequestHeaders(proxyRequest *http.Request, backendResponse *http.Response) {
	preservedHeaders := map[string]string{}
	for key, val := range backendResponse.Header {
		if strings.HasPrefix(key, HeaderCustomHeaderPrefix) {
			trimmedKey := strings.TrimPrefix(key, HeaderCustomHeaderPrefix)
			preservedHeaders[trimmedKey] = val[0]
		}

		// Clear out all existing response headers from the backend
		backendResponse.Header.Del(key)
	}

	proxyRequest.Header.Set("Content-type", backendResponse.Header.Get("Content-type"))

	for key, val := range preservedHeaders {
		proxyRequest.Header.Set(key, val)
	}
}

func isSupportedMethod(verb string) bool {
	switch verb {
	case http.MethodGet:
		return true
	case http.MethodPost:
		return true
	case http.MethodHead:
		return true
	default:
		return false
	}
}
