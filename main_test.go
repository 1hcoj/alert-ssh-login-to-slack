//go:build linux

package main

import (
	"testing"
	"time"
)

func TestEventToLogin(t *testing.T) {
	var event rawLoginEvent
	event.Uid = 4294967294
	event.Pid = 1234
	copyInt8(event.Connection[:], "SSH_CONNECTION=203.0.113.10 54321 192.0.2.20 22")

	at := time.Date(2026, time.June, 19, 12, 30, 0, 0, time.UTC)
	got, err := eventToLogin(event, "server-1", at)
	if err != nil {
		t.Fatal(err)
	}
	if got.ClientIP != "203.0.113.10" || got.ServerIP != "192.0.2.20" {
		t.Fatalf("unexpected addresses: %#v", got)
	}
	if got.Username != "uid:4294967294" {
		t.Fatalf("unexpected username fallback: %q", got.Username)
	}
}

func TestEventToLoginRejectsMalformedConnection(t *testing.T) {
	var event rawLoginEvent
	copyInt8(event.Connection[:], "SSH_CONNECTION=not-an-ip 1 192.0.2.20 22")

	if _, err := eventToLogin(event, "server-1", time.Now()); err == nil {
		t.Fatal("expected malformed connection to be rejected")
	}
}

func copyInt8(destination []int8, value string) {
	for index := range value {
		destination[index] = int8(value[index])
	}
}
