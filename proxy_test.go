package offloader

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
)

func TestEndToEnd(t *testing.T) {
	offloadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		w.Header().Add("OffloadServer-Header", "1")

		escapedBody := strings.Replace(string(body), "\"", "\\\"", -1)
		fmt.Fprint(w, fmt.Sprintf(`{"offload_response": {"backend_body": "%s"}}`, escapedBody))
	}))
	defer offloadServer.Close()

	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(HeaderOffloadRequested, "1")
		w.Header().Set(HeaderRequestedUrl, offloadServer.URL)
		w.Header().Set(HeaderForwardBody, "1")
		w.Header().Set(HeaderRequestedMethod, http.MethodPost)
		w.Header().Set("Content-type", "application/json")

		fmt.Fprint(w, `{"key": "value"}`)
	}))
	defer backendServer.Close()

	target, _ := url.Parse(backendServer.URL)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ModifyResponse = Handler

	request, err := http.NewRequest("GET", "http://doesntmatter/foo/bar/baz", nil)
	if err != nil {
		t.Fatalf("unexpected request error: %v", err)
	}

	rw := httptest.NewRecorder()
	proxy.ServeHTTP(rw, request)

	body, err := ioutil.ReadAll(rw.Body)
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}

	expectedOffloadResponseBody := `{"offload_response": {"backend_body": "{\"key\": \"value\"}"}}`
	if string(body) != expectedOffloadResponseBody {
		t.Errorf("incorrect response body: wanted '%s' but got '%s'", expectedOffloadResponseBody, string(body))
	}

	if rw.Code != http.StatusOK {
		t.Errorf("incorrect response status: wanted %d but got %d", http.StatusTeapot, rw.Code)
	}

	offloadHeader := rw.Header().Get("OffloadServer-Header")
	if offloadHeader != "1" {
		t.Errorf("incorrect header 'OffloadServer-Header' from offload server: want '1' but got '%s'", offloadHeader)
	}
}

func TestNewProxyRequestFromBackendResponse(t *testing.T) {
	t.Run("proxy request has the expected URL", func(t *testing.T) {
		offloadUrl := "http://some-slow-service?foo=bar&baz=quux"

		resp := &http.Response{
			Header: http.Header{
				HeaderOffloadRequested: []string{"1"},
				HeaderRequestedMethod:  []string{"GET"},
				HeaderRequestedUrl:     []string{offloadUrl},
			},
		}

		r, err := newProxyRequestFromBackendResponse(resp)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedUrl, err := url.Parse(offloadUrl)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if *r.URL != *expectedUrl {
			t.Errorf("mismatched URLs; got %v, wanted %v", r.URL, expectedUrl)
		}
	})

	t.Run("proxy request has the expected body when asked to forward it", func(t *testing.T) {
		offloadUrl := "http://some-slow-service?foo=bar&baz=quux"
		body := "42 42 42"

		resp := &http.Response{
			Body: ioutil.NopCloser(strings.NewReader(body)),
			Header: http.Header{
				HeaderOffloadRequested: []string{"1"},
				HeaderRequestedMethod:  []string{"GET"},
				HeaderRequestedUrl:     []string{offloadUrl},
				HeaderForwardBody:      []string{"1"},
			},
		}

		r, err := newProxyRequestFromBackendResponse(resp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		proxyRequestBody, err := ioutil.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("unexpected read error: %v", err)
		}
		if string(proxyRequestBody) != body {
			t.Errorf("incorrect body: got '%s' but wanted '%s'", proxyRequestBody, body)
		}
	})

	t.Run("proxy request doesn't have a body if we don't ask for one", func(t *testing.T) {
		offloadUrl := "http://some-slow-service?foo=bar&baz=quux"
		body := "42 42 42"

		backendResponse := &http.Response{
			Body: ioutil.NopCloser(strings.NewReader(body)),
			Header: http.Header{
				HeaderOffloadRequested: []string{"1"},
				HeaderRequestedMethod:  []string{"GET"},
				HeaderRequestedUrl:     []string{offloadUrl},
			},
		}

		r, err := newProxyRequestFromBackendResponse(backendResponse)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if r.Body != nil {
			proxyRequestBody, err := ioutil.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("unexpected read error: %v", err)
			}
			t.Errorf("unexpected body: got '%s' but wanted ''", proxyRequestBody)
		}
	})

	t.Run("doesn't return an error for a valid verb", func(t *testing.T) {
		resp := &http.Response{
			Header: http.Header{
				HeaderOffloadRequested: []string{"1"},
				HeaderRequestedMethod:  []string{"GET"},
				HeaderRequestedUrl:     []string{"someurl"},
			},
		}

		_, err := newProxyRequestFromBackendResponse(resp)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("returns error for invalid verb", func(t *testing.T) {
		resp := &http.Response{
			Header: http.Header{
				HeaderOffloadRequested: []string{"1"},
				HeaderRequestedMethod:  []string{"DROP"},
				HeaderRequestedUrl:     []string{"someurl"},
			},
		}

		_, err := newProxyRequestFromBackendResponse(resp)

		if err != ErrInvalidVerb {
			t.Fatalf("expected error %v but got %v instead", ErrInvalidVerb, err)
		}
	})

	t.Run("doesn't return an error for a valid verb with unusual case", func(t *testing.T) {
		resp := &http.Response{
			Header: http.Header{
				HeaderOffloadRequested: []string{"1"},
				HeaderRequestedMethod:  []string{"gET"},
				HeaderRequestedUrl:     []string{"someurl"},
			},
		}

		_, err := newProxyRequestFromBackendResponse(resp)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("returns error for missing url", func(t *testing.T) {
		resp := &http.Response{
			Header: http.Header{
				HeaderOffloadRequested: []string{"1"},
				HeaderRequestedMethod:  []string{"GET"},
			},
		}

		_, err := newProxyRequestFromBackendResponse(resp)

		if err != ErrMissingUrl {
			t.Fatalf("expected error %v but got %v instead", ErrMissingUrl, err)
		}
	})
}

func TestPrepareProxyRequestHeaders(t *testing.T) {
	t.Run("has expected headers", func(t *testing.T) {
		ignoredHeader := "Ignore-Header"
		expectedHeaderKey := "my-header"
		expectedHeaderValue := "my header value"
		backendResponse := &http.Response{
			Header: http.Header{
				ignoredHeader: []string{"1"},
				HeaderCustomHeaderPrefix + expectedHeaderKey: []string{expectedHeaderValue},
			},
			StatusCode: http.StatusTeapot,
		}

		proxyRequest := &http.Request{
			Header: http.Header{},
		}

		prepareProxyRequestHeaders(proxyRequest, backendResponse)

		if actualHeaderValue := proxyRequest.Header.Get(expectedHeaderKey); actualHeaderValue != expectedHeaderValue {
			t.Errorf("expected header '%s' not found or has incorrect value: got '%s' but wanted '%s'", expectedHeaderKey, actualHeaderValue, expectedHeaderValue)
		}

		if actualHeaderValue := proxyRequest.Header.Get(ignoredHeader); actualHeaderValue != "" {
			t.Errorf("header should have been ignored but was passed on instead: '%s: %s'", ignoredHeader, actualHeaderValue)
		}
	})

	t.Run("can override content-type with custom value", func(t *testing.T) {
		expectedHeaderKey := "content-type"
		expectedHeaderValue := "application/x-soap-bubbles"
		backendResponse := &http.Response{
			Header: http.Header{
				HeaderCustomHeaderPrefix + expectedHeaderKey: []string{expectedHeaderValue},
			},
			StatusCode: http.StatusTeapot,
		}

		proxyRequest := &http.Request{
			Header: http.Header{
				"Content-Type": []string{"x-should-get-overridden-by-custom-value"},
			},
		}

		prepareProxyRequestHeaders(proxyRequest, backendResponse)

		if actualHeaderValue := proxyRequest.Header.Get(expectedHeaderKey); actualHeaderValue != expectedHeaderValue {
			t.Errorf("expected header '%s' not found or has incorrect value: got '%s' but wanted '%s'", expectedHeaderKey, actualHeaderValue, expectedHeaderValue)
		}
	})
}
