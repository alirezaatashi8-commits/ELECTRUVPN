package handlers

import (
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"
)

func TestSingleInboundExportIsDetectedAndParsed(t *testing.T) {
	doc := `{"id":3,"up":0,"down":1400,"total":0,"remark":"ELECTRU_AR7","enable":true,"expiryTime":0,"listen":"","port":8736,"protocol":"vless",
"settings":"{\"clients\":[{\"id\":\"11111111-1111-1111-1111-111111111111\",\"email\":\"a1\",\"flow\":\"\",\"enable\":true}],\"decryption\":\"none\"}",
"streamSettings":"{\"network\":\"xhttp\",\"security\":\"reality\",\"xhttpSettings\":{\"path\":\"/x\"}}",
"sniffing":"{\"enabled\":true}","clientStats":[{"email":"a1","up":5,"down":9}]}`
	for _, name := range []string{"inbound.txt", "inbound", "inbound.json"} {
		p := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(p, []byte("\xef\xbb\xbf"+doc), 0o600); err != nil {
			t.Fatal(err)
		}
		if k := sniffXUIImportKind(p, name); k != "json" {
			t.Fatalf("%s: kind=%q", name, k)
		}
		b, _ := os.ReadFile(p)
		got, err := parseXrayJSON(b)
		if err != nil || len(got) != 1 {
			t.Fatalf("%s: %v %d", name, err, len(got))
		}
		g := got[0]
		if g.Port != 8736 || g.Protocol != "vless" || g.Remark != "ELECTRU_AR7" || g.Stream["security"] != "reality" {
			t.Fatalf("bad parse: %+v", g)
		}
		cl := extractXUIClients(g, "vless")
		if len(cl) != 1 || cl[0].UUID == "" || cl[0].Down != 9 {
			t.Fatalf("bad clients: %+v", cl)
		}
	}
	// wrapped API response + array
	for _, w := range []string{`{"success":true,"obj":` + doc + `}`, `[` + doc + `]`} {
		got, err := parseXrayJSON([]byte(w))
		if err != nil || len(got) != 1 {
			t.Fatalf("wrapped: %v %d", err, len(got))
		}
	}
}

func TestTruncateRunesKeepsPersianValid(t *testing.T) {
	in := "کاربر-تست-فارسی-طولانی"
	out := truncateRunes(in, 7)
	if !utf8.ValidString(out) || len([]rune(out)) != 7 {
		t.Fatalf("bad truncation: %q", out)
	}
	if truncateRunes("abc", 10) != "abc" {
		t.Fatal("short string changed")
	}
}
