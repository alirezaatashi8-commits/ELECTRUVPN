package xray

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagerAtomicConfigWrite(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "xray.json")
	m := NewManager("/bin/sh", cfgPath)
	if err := m.WriteConfig(&Config{}); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(cfgPath); err != nil || len(b) == 0 {
		t.Fatalf("config was not written atomically: %v", err)
	}

	// /bin/sh is intentionally not started here because Manager supplies
	// xray-specific arguments. The atomic writer is the part under test.
}
