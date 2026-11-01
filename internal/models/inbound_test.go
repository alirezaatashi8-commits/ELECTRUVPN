package models

import (
	"testing"
	"time"
)

func TestInboundVolumeLimit(t *testing.T) {
	ib := Inbound{TotalGB: 1, Up: 512 * 1024 * 1024, Down: 512 * 1024 * 1024}
	if !ib.IsOverQuota() || !ib.IsLimited() {
		t.Fatal("expected inbound volume to be exhausted")
	}
	ib.Down = 0
	if ib.IsOverQuota() || ib.IsLimited() {
		t.Fatal("inbound below its volume must not be limited")
	}
	ib.TotalGB = 0
	ib.Up = 1 << 50
	if ib.IsOverQuota() || ib.IsLimited() {
		t.Fatal("unlimited inbound (0 GB) must never be over quota")
	}
}

func TestInboundExpiryLimit(t *testing.T) {
	ib := Inbound{}
	if ib.IsExpired() || ib.IsLimited() {
		t.Fatal("inbound without expiry must not expire")
	}
	ib.ExpiryTime = time.Now().Add(-time.Minute).UnixMilli()
	if !ib.IsExpired() || !ib.IsLimited() {
		t.Fatal("expected expired inbound")
	}
	ib.ExpiryTime = time.Now().Add(time.Hour).UnixMilli()
	if ib.IsExpired() || ib.IsLimited() {
		t.Fatal("inbound with a future expiry must not be limited")
	}
}

func TestInboundExpiryBoundaryAndUnlimited(t *testing.T) {
	ib := Inbound{}
	if ib.IsExpired() {
		t.Fatal("zero expiry must mean unlimited time")
	}
	ib.ExpiryTime = time.Now().Add(-time.Millisecond).UnixMilli()
	if !ib.IsExpired() {
		t.Fatal("expiry in the past must be expired")
	}
}

func TestInboundQuotaDoesNotOverflow(t *testing.T) {
	ib := Inbound{TotalGB: 1, Up: int64(^uint64(0) >> 1), Down: 1}
	if !ib.IsOverQuota() {
		t.Fatal("overflowing usage counters must saturate and exhaust the quota")
	}
	ib = Inbound{TotalGB: int64(^uint64(0) >> 1), Up: 1}
	if ib.IsOverQuota() {
		t.Fatal("unrepresentably large GiB quota must not overflow into a false limit")
	}
}
