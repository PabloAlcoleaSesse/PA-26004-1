package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthAndReadiness(t *testing.T) {
	for _, tc := range []struct {
		path  string
		dbErr error
		want  int
	}{
		{"/healthz", errors.New("offline"), 200},
		{"/readyz", nil, 200},
		{"/readyz", errors.New("secret database details"), 503},
		{"/missing", nil, 404},
	} {
		t.Run(tc.path+http.StatusText(tc.want), func(t *testing.T) {
			called := false
			h := NewHandler(func(ctx context.Context) error {
				called = true
				if _, ok := ctx.Deadline(); !ok {
					t.Error("readiness check must have a deadline")
				}
				return tc.dbErr
			})
			r := httptest.NewRecorder()
			h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if r.Code != tc.want {
				t.Fatalf("status = %d, want %d", r.Code, tc.want)
			}
			if called != (tc.path == "/readyz") {
				t.Fatal("unexpected database check")
			}
			if strings.Contains(r.Body.String(), "secret") {
				t.Fatal("database error leaked")
			}
		})
	}
}

func TestHealthRejectsWrites(t *testing.T) {
	h := NewHandler(func(context.Context) error { t.Fatal("unexpected database check"); return nil })
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/healthz", nil))
	if r.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", r.Code)
	}
}
