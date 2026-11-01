package backup

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/gorm"
)

// Create makes a backup archive of runtime configs and, when using SQLite,
// a consistent database snapshot (VACUUM INTO). For PostgreSQL the panel
// records the backup of configs and expects operators to use pg_dump for
// the database itself (or an external backup solution).
func Create(db *gorm.DB, dbPath, xrayPath, singboxPath, backupDir, remoteDir string) (models.BackupRecord, error) {
	if err := os.MkdirAll(backupDir, 0750); err != nil {
		return models.BackupRecord{}, err
	}
	stamp := time.Now().UTC().Format("20060102-150405.000000000")
	path := filepath.Join(backupDir, "electruvpn-"+stamp+".zip")
	f, err := os.Create(path)
	if err != nil {
		return models.BackupRecord{}, err
	}

	zw := zip.NewWriter(f)
	sources := []string{xrayPath, singboxPath}
	dbArtifactName := ""

	// Detect driver: SQLite supports VACUUM INTO; Postgres does not.
	isSQLite := false
	if sqlDB, err := db.DB(); err == nil {
		if drv := sqlDB.Driver(); drv != nil {
			// jackc/pgx vs modernc/sqlite or mattn
			name := fmt.Sprintf("%T", drv)
			if strings.Contains(strings.ToLower(name), "sqlite") {
				isSQLite = true
			}
		}
	}
	// Fallback heuristic: if ELECTRU_DB_DSN is empty we are on SQLite.
	if os.Getenv("ELECTRU_DB_DSN") == "" {
		isSQLite = true
	}

	var tmpPath string
	if isSQLite && dbPath != "" {
		tmp, err := os.CreateTemp(backupDir, ".electruvpn-backup-*.db")
		if err != nil {
			_ = f.Close()
			_ = os.Remove(path)
			return models.BackupRecord{}, err
		}
		tmpPath = tmp.Name()
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		defer os.Remove(tmpPath)
		quoted := strings.ReplaceAll(tmpPath, "'", "''")
		if err = db.Exec("VACUUM INTO '" + quoted + "'").Error; err != nil {
			_ = f.Close()
			_ = os.Remove(path)
			return models.BackupRecord{}, err
		}
		sources = append([]string{tmpPath}, sources...)
		dbArtifactName = filepath.Base(dbPath)
	} else {
		// Prefer pg_dump when binary + DSN available (full panel data).
		dumped := false
		if dsn := strings.TrimSpace(os.Getenv("ELECTRU_DB_DSN")); dsn != "" {
			dumpPath := filepath.Join(backupDir, ".electruvpn-pg-"+stamp+".sql")
			cmd := exec.Command("pg_dump", "--no-owner", "--no-acl", "-f", dumpPath, dsn)
			if out, e := cmd.CombinedOutput(); e == nil {
				sources = append([]string{dumpPath}, sources...)
				dbArtifactName = "postgres.sql"
				dumped = true
				defer os.Remove(dumpPath)
				_ = out
			}
		}
		if !dumped {
			markerName := "DATABASE_IS_POSTGRES.txt"
			w, e := zw.Create(markerName)
			if e != nil {
				_ = f.Close()
				_ = os.Remove(path)
				return models.BackupRecord{}, e
			}
			msg := "PostgreSQL: pg_dump not available. Configs included. Run pg_dump manually for DB.\n"
			if _, e = io.WriteString(w, msg); e != nil {
				_ = f.Close()
				_ = os.Remove(path)
				return models.BackupRecord{}, e
			}
			dbArtifactName = markerName
		}
	}

	for _, src := range sources {
		if src == "" {
			continue
		}
		if _, err = os.Stat(src); err != nil {
			continue
		}
		in, e := os.Open(src)
		if e != nil {
			continue
		}
		name := filepath.Base(src)
		if tmpPath != "" && src == tmpPath {
			name = dbArtifactName
		}
		w, e := zw.Create(name)
		if e != nil {
			_ = in.Close()
			_ = f.Close()
			_ = os.Remove(path)
			return models.BackupRecord{}, e
		}
		if _, e = io.Copy(w, in); e != nil {
			_ = in.Close()
			_ = f.Close()
			_ = os.Remove(path)
			return models.BackupRecord{}, fmt.Errorf("copy %s into backup: %w", name, e)
		}
		if e = in.Close(); e != nil {
			_ = f.Close()
			_ = os.Remove(path)
			return models.BackupRecord{}, fmt.Errorf("close %s: %w", name, e)
		}
	}
	if err = zw.Close(); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return models.BackupRecord{}, err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return models.BackupRecord{}, fmt.Errorf("sync backup archive: %w", err)
	}
	if err = f.Close(); err != nil {
		_ = os.Remove(path)
		return models.BackupRecord{}, err
	}
	if err = verifyArchive(path, dbArtifactName); err != nil {
		_ = os.Remove(path)
		return models.BackupRecord{}, err
	}
	st, err := os.Stat(path)
	if err != nil {
		_ = os.Remove(path)
		return models.BackupRecord{}, err
	}
	if remoteDir != "" {
		if err := os.MkdirAll(remoteDir, 0750); err != nil {
			_ = os.Remove(path)
			return models.BackupRecord{}, err
		}
		dst := filepath.Join(remoteDir, filepath.Base(path))
		if err := copyFile(path, dst); err != nil {
			_ = os.Remove(path)
			return models.BackupRecord{}, fmt.Errorf("remote backup copy: %w", err)
		}
	}
	row := models.BackupRecord{Path: path, Size: st.Size()}
	if err := db.Create(&row).Error; err != nil {
		return row, err
	}
	return row, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".electruvpn-remote-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err = io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, dst)
}

func verifyArchive(path, dbName string) error {
	_, err := FindDatabaseMember(path, dbName)
	return err
}

// FindDatabaseMember opens a backup zip and returns the archive member name that
// holds the panel database. preferred is tried first (e.g. electruvpn.db);
// otherwise any safe .db / postgres.sql / DATABASE_IS_POSTGRES.txt is accepted
// so backups can be restored on a fresh install even when paths differ.
func FindDatabaseMember(path, preferred string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer zr.Close()

	seen := make(map[string]struct{}, len(zr.File))
	var candidates []string
	for _, entry := range zr.File {
		if entry.Name == "" || filepath.Base(entry.Name) != entry.Name || strings.Contains(entry.Name, "..") {
			return "", fmt.Errorf("unsafe archive member %q", entry.Name)
		}
		if _, exists := seen[entry.Name]; exists {
			return "", fmt.Errorf("duplicate archive member %q", entry.Name)
		}
		seen[entry.Name] = struct{}{}
		r, err := entry.Open()
		if err != nil {
			return "", fmt.Errorf("open archive member %q: %w", entry.Name, err)
		}
		if _, err := io.Copy(io.Discard, r); err != nil {
			_ = r.Close()
			return "", fmt.Errorf("read archive member %q: %w", entry.Name, err)
		}
		if err := r.Close(); err != nil {
			return "", fmt.Errorf("close archive member %q: %w", entry.Name, err)
		}
		lower := strings.ToLower(entry.Name)
		switch {
		case preferred != "" && entry.Name == preferred:
			candidates = append([]string{entry.Name}, candidates...)
		case strings.HasSuffix(lower, ".db"), entry.Name == "postgres.sql", entry.Name == "DATABASE_IS_POSTGRES.txt":
			candidates = append(candidates, entry.Name)
		}
	}
	if len(candidates) == 0 {
		if preferred != "" {
			return "", fmt.Errorf("database artifact %q is missing", preferred)
		}
		return "", fmt.Errorf("no database artifact found in backup")
	}
	// Prefer exact preferred match when present (it was prepended).
	return candidates[0], nil
}

func Prune(db *gorm.DB, backupDir string, retention int) error {
	if retention < 1 {
		retention = 14
	}
	for {
		var rows []models.BackupRecord
		if err := db.Order("id desc").Offset(retention).Limit(200).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			if row.Path != "" {
				_ = os.Remove(row.Path)
			}
			if err := db.Delete(&row).Error; err != nil {
				return err
			}
		}
	}
}
