package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoggingHandlerAddsCorrelationIDAndStructuredFields(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	handler := LoggingHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("ok"))
	}), logger)

	req := httptest.NewRequest(http.MethodPost, "/api/syncs?access_token=redacted", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	requestID := response.Header().Get("X-Request-ID")
	if len(requestID) != 32 {
		t.Fatalf("request ID length = %d, want 32", len(requestID))
	}
	var event map[string]any
	if err := json.Unmarshal(output.Bytes(), &event); err != nil {
		t.Fatalf("decode log: %v", err)
	}
	if event["msg"] != "http request" || event["request_id"] != requestID || event["method"] != "POST" || event["path"] != "/api/syncs" {
		t.Fatalf("unexpected request event: %v", event)
	}
	if event["status"] != float64(http.StatusAccepted) || event["bytes"] != float64(2) {
		t.Fatalf("unexpected response fields: %v", event)
	}
	if strings.Contains(output.String(), "access_token") {
		t.Fatal("request query string was written to logs")
	}
}
