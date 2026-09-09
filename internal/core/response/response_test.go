package response

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleMapsErrors(t *testing.T) {
	cases := []struct {
		name   string
		h      Handler
		status int
		body   string
	}{
		{"status error", func(http.ResponseWriter, *http.Request) error { return NotFound("gone") }, 404, `{"error":"gone"}`},
		{"unknown error", func(http.ResponseWriter, *http.Request) error { return http.ErrNoCookie }, 500, `{"error":"internal error"}`},
		{"decode error", func(w http.ResponseWriter, r *http.Request) error {
			var v struct{ A int }
			return Decode(r, &v)
		}, 400, `"error":"invalid request body`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"A":"x"}`))
			Handle(tc.h).ServeHTTP(rec, req)

			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}

			if !strings.Contains(rec.Body.String(), tc.body) {
				t.Fatalf("body = %s, want it to contain %s", rec.Body.String(), tc.body)
			}
		})
	}
}

func TestDecodeRefusesUnknownMembers(t *testing.T) {
	var v struct {
		Name string `json:"name"`
	}

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"a","nope":1}`))
	if err := Decode(req, &v); err == nil {
		t.Fatal("an unknown member must be refused")
	}
}

func TestJSONCoercesInvalidUTF8(t *testing.T) {
	rec := httptest.NewRecorder()
	JSON(rec, 200, map[string]string{"v": "a\xffb"})

	if !strings.Contains(rec.Body.String(), "\ufffd") {
		t.Fatalf("invalid UTF-8 must be coerced, got %q", rec.Body.String())
	}
}
