package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestReturnsHTTPErrorWithStatusCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "中继流量额度已用尽"}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	api := NewAPI(server.URL)
	err := api.ReportRoomVoiceWebRTCTraffic(context.Background(), 1, 1024)

	var httpErr HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error type=%T, want HTTPError", err)
	}
	if httpErr.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("status=%d, want %d", httpErr.StatusCode, http.StatusPaymentRequired)
	}
	if httpErr.Error() != "中继流量额度已用尽" {
		t.Fatalf("message=%q", httpErr.Error())
	}
}
