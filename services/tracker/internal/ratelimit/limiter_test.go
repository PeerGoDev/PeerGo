package ratelimit

import (
	"testing"
	"time"
)

func TestLimiterRefillsWithoutRetainingRawKeys(t *testing.T) {
	limiter, err := New(4, 40)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	if !limiter.AllowAddress("192.0.2.1", 60, 2, now) || !limiter.AllowAddress("192.0.2.1", 60, 2, now) {
		t.Fatal("initial burst was rejected")
	}
	if limiter.AllowAddress("192.0.2.1", 60, 2, now) {
		t.Fatal("burst overflow was accepted")
	}
	if !limiter.AllowAddress("192.0.2.1", 60, 2, now.Add(time.Second)) {
		t.Fatal("one token was not refilled")
	}
}
