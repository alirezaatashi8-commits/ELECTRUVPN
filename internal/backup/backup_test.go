package backup

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func makeTestArchive(t *testing.T, members map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, content := range members {
		w, err := zw.Create(name)
		if err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVerifyArchive(t *testing.T) {
	path := makeTestArchive(t, map[string]string{
		"panel.db":  "sqlite snapshot",
		"xray.json": "{}",
	})
	if err := verifyArchive(path, "panel.db"); err != nil {
		t.Fatalf("valid archive rejected: %v", err)
	}
}

func TestVerifyArchiveRejectsMissingDatabase(t *testing.T) {
	path := makeTestArchive(t, map[string]string{"xray.json": "{}"})
	if err := verifyArchive(path, "panel.db"); err == nil {
		t.Fatal("archive without database artifact was accepted")
	}
}

func TestVerifyArchiveRejectsUnsafeMember(t *testing.T) {
	path := makeTestArchive(t, map[string]string{
		"panel.db": "sqlite snapshot",
		"../evil":  "must never be accepted",
	})
	if err := verifyArchive(path, "panel.db"); err == nil {
		t.Fatal("archive with unsafe member was accepted")
	}
}
