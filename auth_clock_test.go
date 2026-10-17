package main

import (
	"testing"
	"time"
)

func TestLoginWindowResetsAtOneMinute(t *testing.T) {
	now := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	auth := newAuth("test", "", false)
	auth.now = func() time.Time { return now }
	for range 30 {
		if !auth.allowLogin() {
			t.Fatal("attempt within the limit was rejected")
		}
	}
	if auth.allowLogin() {
		t.Fatal("attempt 31 was allowed")
	}
	now = now.Add(time.Minute - time.Nanosecond)
	if auth.allowLogin() {
		t.Fatal("window reset early")
	}
	now = now.Add(time.Nanosecond)
	if !auth.allowLogin() {
		t.Fatal("window did not reset at one minute")
	}
}
