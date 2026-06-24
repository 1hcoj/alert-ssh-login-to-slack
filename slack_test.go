package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSlackNotifierChatPostMessage(t *testing.T) {
	var received slackMessage
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", request.Method)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer xoxb-test-token" {
			t.Errorf("unexpected authorization header: %q", got)
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true,"channel":"C123","ts":"123.456"}`))
	}))
	defer server.Close()

	notifier := newSlackNotifier("xoxb-test-token", "C123")
	notifier.endpoint = server.URL
	notifier.client = server.Client()

	err := notifier.Notify(context.Background(), login{
		Username:   "alice",
		UID:        1000,
		PID:        123,
		ClientIP:   "203.0.113.10",
		ClientPort: "50000",
		ServerIP:   "192.0.2.10",
		ServerPort: "22",
		Hostname:   "server-1",
		OccurredAt: time.Date(2026, time.June, 19, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if received.Channel != "C123" {
		t.Fatalf("unexpected channel: %q", received.Channel)
	}
	if !strings.Contains(received.Text, "alice") {
		t.Fatalf("fallback text does not contain username: %q", received.Text)
	}
}

func TestSlackNotifierReturnsAPIErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":false,"error":"not_in_channel"}`))
	}))
	defer server.Close()

	notifier := newSlackNotifier("xoxb-test-token", "C123")
	notifier.endpoint = server.URL
	notifier.client = server.Client()

	err := notifier.Notify(context.Background(), login{})
	if err == nil || !strings.Contains(err.Error(), "not_in_channel") {
		t.Fatalf("expected not_in_channel error, got %v", err)
	}
}
