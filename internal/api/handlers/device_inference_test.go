package handlers

import "testing"

func TestInferDeviceInfo(t *testing.T) {
	m, model, os, ver := inferDeviceInfo(`Mozilla/5.0 (Linux; Android 14; Pixel 8 Pro Build/AP2A.240505.002) AppleWebKit/537.36 Chrome/125.0 Mobile Safari/537.36`, "", "")
	if os != "Android" || ver != "14" || model == "" {
		t.Fatalf("unexpected android info: manufacturer=%q model=%q os=%q version=%q", m, model, os, ver)
	}
	_, model, os, ver = inferDeviceInfo(`Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148`, "", "")
	if model != "iPhone" || os != "iOS" || ver != "17.5" {
		t.Fatalf("unexpected ios info: model=%q os=%q version=%q", model, os, ver)
	}
	m, model, os, _ = inferDeviceInfo(`Mozilla/5.0 (Linux; Android 14; SM-S928B Build/UP1A.231005.007) AppleWebKit/537.36 Mobile Safari/537.36`, "", "")
	if m != "Samsung" || model == "" || os != "Android" {
		t.Fatalf("unexpected samsung info: manufacturer=%q model=%q os=%q", m, model, os)
	}
}

func TestHashUserAgent(t *testing.T) {
	a := hashUserAgent("Mozilla/5.0", "secret")
	b := hashUserAgent("Mozilla/5.0", "secret")
	c := hashUserAgent("Mozilla/5.0", "other-secret")
	if a == "" || a != b || a == c {
		t.Fatalf("unexpected keyed user-agent hash behavior")
	}
	if hashUserAgent("", "secret") != "" || hashUserAgent("Mozilla/5.0", "") != "" {
		t.Fatalf("empty input/secret must not produce an identifier")
	}
}
