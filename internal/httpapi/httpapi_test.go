package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBearerMatchesExactly(t *testing.T) {
	if !BearerMatches("Bearer secret", "secret") {
		t.Fatal("valid bearer token rejected")
	}
	for _, header := range []string{"secret", "bearer secret", "Bearer secret-extra", "Bearer "} {
		if BearerMatches(header, "secret") {
			t.Errorf("invalid header %q accepted", header)
		}
	}
}

func TestDecodeJSONRequiresExactMediaTypeAndSingleObject(t *testing.T) {
	tests := []struct {
		contentType, body string
		want              int
	}{
		{"application/json; charset=utf-8", `{"name":"ok"}`, http.StatusOK},
		{"application/jsonx", `{"name":"no"}`, http.StatusUnsupportedMediaType},
		{"application/json", `{"name":"one"} {"name":"two"}`, http.StatusBadRequest},
		{"application/json", `{"unknown":true}`, http.StatusBadRequest},
	}
	for _, test := range tests {
		request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.body))
		request.Header.Set("Content-Type", test.contentType)
		response := httptest.NewRecorder()
		var input struct {
			Name string `json:"name"`
		}
		if DecodeJSON(response, request, &input, "invalid") {
			response.WriteHeader(http.StatusOK)
		}
		if response.Code != test.want {
			t.Errorf("%s %q status = %d, want %d", test.contentType, test.body, response.Code, test.want)
		}
	}
}
