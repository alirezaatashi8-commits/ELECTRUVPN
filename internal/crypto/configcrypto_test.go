package crypto

import (
	"bytes"
	"testing"
)

func TestPasswordRoundTrip(t *testing.T) {
	pt := []byte("vless://secret-config-line\nsecond-line")
	env, err := EncryptWithPassword("correct-horse-battery", pt, "test")
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecryptWithPassword("correct-horse-battery", env)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, pt) {
		t.Fatalf("mismatch: %q vs %q", out, pt)
	}
	if _, err := DecryptWithPassword("wrong-password-xx", env); err == nil {
		t.Fatal("expected failure for wrong password")
	}
}

func TestMasterRoundTrip(t *testing.T) {
	key := MasterKeyFromEnv("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if len(key) != 32 {
		t.Fatalf("key len %d", len(key))
	}
	enc, err := EncryptWithMaster(key, "private-reality-key")
	if err != nil {
		t.Fatal(err)
	}
	if enc == "private-reality-key" {
		t.Fatal("expected encryption")
	}
	dec, err := DecryptWithMaster(key, enc)
	if err != nil || dec != "private-reality-key" {
		t.Fatalf("dec=%q err=%v", dec, err)
	}
}
