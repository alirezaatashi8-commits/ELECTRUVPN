package models

import (
	"testing"
	"time"
)

func TestClientQuotaAndExpiry(t *testing.T) {
	c := Client{TotalGB: 1, Up: 512 * 1024 * 1024, Down: 512 * 1024 * 1024}
	if !c.IsOverQuota() {
		t.Fatal("expected quota to be exhausted")
	}
	c.TotalGB = 0
	if c.IsOverQuota() {
		t.Fatal("unlimited client must not be over quota")
	}
	c.ExpiryTime = time.Now().Add(-time.Minute).UnixMilli()
	if !c.IsExpired() {
		t.Fatal("expected expired client")
	}
}

func TestClientExpiryBoundaryAndUnlimited(t *testing.T) {
	c := Client{}
	if c.IsExpired() {
		t.Fatal("zero expiry must mean unlimited time")
	}
	c.ExpiryTime = time.Now().Add(-time.Millisecond).UnixMilli()
	if !c.IsExpired() {
		t.Fatal("expiry in the past must be expired")
	}
}

func TestClientQuotaDoesNotOverflow(t *testing.T) {
	c := Client{TotalGB: 1, Up: int64(^uint64(0) >> 1), Down: 1}
	if !c.IsOverQuota() {
		t.Fatal("overflowing usage counters must saturate and exhaust the quota")
	}
	c = Client{TotalGB: int64(^uint64(0) >> 1), Up: 1}
	if c.IsOverQuota() {
		t.Fatal("unrepresentably large GiB quota must not overflow into a false limit")
	}
	c = Client{TrafficLimitBytes: 10, Up: 9, Down: 1}
	if !c.IsOverQuota() {
		t.Fatal("precise byte quota must be enforced")
	}
}
