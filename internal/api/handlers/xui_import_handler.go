package handlers

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/xray"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// XUIImport accepts a 3x-ui SQLite backup, a SQLite SQL dump, or an Xray JSON
// config and translates compatible inbounds/clients into ElectruVPN records.
// It deliberately does not replace ElectruVPN's own database: the two panels
// have different schemas and unrelated account/billing tables.
func (h *InboundHandler) XUIImport(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 129<<20)
	f, header, err := c.Request.FormFile("file")
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"success": false, "msg": "3x-ui import file exceeds the 128 MiB upload limit"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "select a 3x-ui .db backup, SQLite .sql/.dump, or Xray .json file"})
		return
	}
	defer f.Close()
	name := strings.ToLower(filepath.Base(header.Filename))
	tmp, err := os.CreateTemp("", "electruvpn-xui-import-*")
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": "could not create temporary import file"})
		return
	}
	path := tmp.Name()
	defer os.Remove(path)
	if _, err = io.Copy(tmp, io.LimitReader(f, (128<<20)+1)); err != nil {
		tmp.Close()
		c.JSON(400, gin.H{"success": false, "msg": "could not read uploaded file"})
		return
	}
	if err = tmp.Close(); err != nil {
		c.JSON(500, gin.H{"success": false, "msg": "could not save uploaded file"})
		return
	}
	if st, e := os.Stat(path); e != nil || st.Size() == 0 || st.Size() > 128<<20 {
		c.JSON(413, gin.H{"success": false, "msg": "file is empty or exceeds 128 MiB"})
		return
	}

	var candidates []xuiInbound
	kind := sniffXUIImportKind(path, name)
	switch kind {
	case "json":
		b, e := os.ReadFile(path)
		if e != nil {
			c.JSON(400, gin.H{"success": false, "msg": "could not read JSON"})
			return
		}
		candidates, err = parseXrayJSON(b)
	case "links":
		err = fmt.Errorf("share links (vless://, vmess://, ...) do not contain the REALITY/TLS private key, so they cannot be imported as a server inbound; in 3x-ui use the inbound's Export option (JSON) or a .db backup")
	case "postgres":
		candidates, err = readPostgresDump(path)
	case "sqldump":
		var dbPath string
		dbPath, err = sqliteFromSQLDump(path)
		if dbPath != "" && dbPath != path {
			defer os.Remove(dbPath)
		}
		if err == nil {
			candidates, err = readXUIDatabase(dbPath)
		}
	case "sqlite":
		candidates, err = readXUIDatabase(path)
	default:
		err = fmt.Errorf("unrecognized file: expected a 3x-ui .db backup, a SQLite .sql dump, or the JSON exported from a single 3x-ui inbound / Xray config")
	}
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if len(candidates) == 0 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"success": false, "msg": "no compatible 3x-ui/Xray inbounds found in this file"})
		return
	}

	result := xuiImportResult{Source: header.Filename}
	createdIDs := make([]uint, 0, len(candidates))
	for _, raw := range candidates {
		ib, clients, why := translateXUIInbound(raw, h.S.Config.XrayBinaryPath)
		if why != "" {
			result.Skipped = append(result.Skipped, xuiImportIssue{Remark: raw.Remark, Reason: why})
			continue
		}
		if ok, reason := h.portAvailableForNetwork(ib.Port, ib.Network, 0, ib.IPMode); !ok {
			result.Skipped = append(result.Skipped, xuiImportIssue{Remark: ib.Remark, Reason: reason})
			continue
		}
		ib.Tag = fmt.Sprintf("inbound-%d-%s", ib.Port, uuid.NewString()[:8])
		if _, _, e := h.S.ValidateInboundConfig(ib); e != nil {
			result.Skipped = append(result.Skipped, xuiImportIssue{Remark: ib.Remark, Reason: "configuration validation failed: " + e.Error()})
			continue
		}
		err = h.S.DB.Transaction(func(tx *gorm.DB) error {
			if e := tx.Create(&ib).Error; e != nil {
				return e
			}
			for i := range clients {
				clients[i].InboundID = ib.ID
				base := clients[i].Email
				for n := 1; ; n++ {
					var count int64
					if e := tx.Model(&models.Client{}).Where("email = ?", clients[i].Email).Count(&count).Error; e != nil {
						return e
					}
					if count == 0 {
						break
					}
					suffix := fmt.Sprintf("-%d", n+1)
					clients[i].Email = truncateXUI(base, 128-len(suffix)) + suffix
				}
				if e := tx.Create(&clients[i]).Error; e != nil {
					return e
				}
			}
			return nil
		})
		if err != nil {
			result.Skipped = append(result.Skipped, xuiImportIssue{Remark: ib.Remark, Reason: "database insert failed: " + err.Error()})
			continue
		}
		createdIDs = append(createdIDs, ib.ID)
		result.ImportedInbounds++
		result.ImportedClients += len(clients)
	}
	if result.ImportedInbounds == 0 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"success": false, "msg": "nothing was imported; check the skipped list for unsupported protocols, invalid configs, or port conflicts", "obj": result})
		return
	}
	if err := h.S.RegenerateAndRestartXray(); err != nil {
		_ = h.S.DB.Transaction(func(tx *gorm.DB) error {
			if len(createdIDs) == 0 {
				return nil
			}
			if e := tx.Where("inbound_id IN ?", createdIDs).Delete(&models.Client{}).Error; e != nil {
				return e
			}
			return tx.Where("id IN ?", createdIDs).Delete(&models.Inbound{}).Error
		})
		_ = h.S.RegenerateAndRestartXray()
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": "import was rolled back because runtime apply failed: " + err.Error(), "obj": result})
		return
	}
	auditAction(h.S, c, "inbound.import-3x-ui", header.Filename, fmt.Sprintf("inbounds=%d clients=%d", result.ImportedInbounds, result.ImportedClients))
	c.JSON(http.StatusOK, gin.H{"success": true, "msg": "3x-ui import completed", "obj": result})
}

type xuiImportResult struct {
	Source           string           `json:"source"`
	ImportedInbounds int              `json:"importedInbounds"`
	ImportedClients  int              `json:"importedClients"`
	Skipped          []xuiImportIssue `json:"skipped,omitempty"`
}
type xuiImportIssue struct {
	Remark string `json:"remark,omitempty"`
	Reason string `json:"reason"`
}

type xuiInbound struct {
	Remark                  string
	Enable                  bool
	Listen                  string
	Port                    int
	Protocol                string
	Settings                map[string]any
	Stream                  map[string]any
	Sniffing                map[string]any
	Up, Down, Total, Expiry int64
}

func readXUIDatabase(path string) ([]xuiInbound, error) {
	db, err := gorm.Open(sqlite.Open("file:"+filepath.ToSlash(path)+"?mode=ro"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, fmt.Errorf("not a readable SQLite database or 3x-ui SQLite backup: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	defer sqlDB.Close()
	var count int64
	if err = db.Raw("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='inbounds'").Scan(&count).Error; err != nil || count == 0 {
		return nil, fmt.Errorf("database does not contain the 3x-ui 'inbounds' table")
	}
	rows, err := sqlDB.Query("SELECT * FROM inbounds")
	if err != nil {
		return nil, fmt.Errorf("could not read 3x-ui inbounds: %w", err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []xuiInbound
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := map[string]any{}
		for i, col := range cols {
			m[strings.ToLower(col)] = vals[i]
		}
		settings := map[string]any{}
		_ = json.Unmarshal([]byte(strVal(m["settings"])), &settings)
		stream := map[string]any{}
		_ = json.Unmarshal([]byte(strVal(m["stream_settings"])), &stream)
		sniff := map[string]any{}
		_ = json.Unmarshal([]byte(strVal(m["sniffing"])), &sniff)
		out = append(out, xuiInbound{Remark: strVal(m["remark"]), Enable: boolVal(m["enable"], true), Listen: strVal(m["listen"]), Port: intVal(m["port"]), Protocol: strings.ToLower(strVal(m["protocol"])), Settings: settings, Stream: stream, Sniffing: sniff, Up: int64Val(m["up"]), Down: int64Val(m["down"]), Total: int64Val(m["total"]), Expiry: int64Val(m["expiry_time"])})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	traffic := readSQLiteClientTraffic(sqlDB)
	for i := range out {
		clients, ok := out[i].Settings["clients"].([]any)
		if !ok {
			continue
		}
		for _, value := range clients {
			client, ok := value.(map[string]any)
			if !ok {
				continue
			}
			email := strVal(client["email"])
			stat, found := traffic[email]
			if !found {
				continue
			}
			client["up"] = stat["up"]
			client["down"] = stat["down"]
			if !boolVal(stat["enable"], true) {
				client["enable"] = false
			}
		}
	}
	return out, nil
}

func readSQLiteClientTraffic(db *sql.DB) map[string]map[string]any {
	result := map[string]map[string]any{}
	var exists int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='client_traffics'").Scan(&exists); err != nil || exists == 0 {
		return result
	}
	rows, err := db.Query("SELECT * FROM client_traffics")
	if err != nil {
		return result
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return result
	}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if rows.Scan(ptrs...) != nil {
			continue
		}
		entry := map[string]any{}
		for i, col := range cols {
			entry[strings.ToLower(col)] = vals[i]
		}
		email := strVal(entry["email"])
		if email != "" {
			result[email] = map[string]any{"up": int64Val(entry["up"]), "down": int64Val(entry["down"]), "enable": entry["enable"]}
		}
	}
	return result
}

// sqliteFromSQLDump restores a restricted SQLite SQL dump into a temporary,
// isolated database. The uploaded dump is never run against the live database.
func isPostgresArchive(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	b := make([]byte, 5)
	n, _ := f.Read(b)
	return n == 5 && string(b) == "PGDMP"
}
func isPostgresCopyDump(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToUpper(string(b)), "COPY PUBLIC.INBOUNDS") || strings.Contains(strings.ToUpper(string(b)), "COPY INBOUNDS")
}

// readPostgresDump extracts only the 3x-ui inbounds table from a PostgreSQL
// dump. It never restores a foreign database into ElectruVPN. Custom archives
// are decoded by pg_restore into a text stream, then COPY data is parsed locally.
func readPostgresDump(path string) ([]xuiInbound, error) {
	textPath := path
	if isPostgresArchive(path) {
		bin, err := exec.LookPath("pg_restore")
		if err != nil {
			return nil, fmt.Errorf("this is a PostgreSQL custom archive; install postgresql-client (pg_restore) on the panel server or export 3x-ui as SQLite .db")
		}
		f, err := os.CreateTemp("", "electruvpn-xui-pg-*.sql")
		if err != nil {
			return nil, err
		}
		textPath = f.Name()
		_ = f.Close()
		defer os.Remove(textPath)
		cmd := exec.Command(bin, "--data-only", "--table=inbounds", "--no-owner", "--no-privileges", "--file", textPath, path)
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("pg_restore could not read this backup: %s", strings.TrimSpace(string(out)))
		}
	}
	b, err := os.ReadFile(textPath)
	if err != nil {
		return nil, err
	}
	src := string(b)
	copyRE := regexp.MustCompile(`(?im)^COPY\s+(?:public\.)?"?inbounds"?\s*\(([^)]*)\)\s+FROM\s+stdin;\s*\r?\n`)
	matches := copyRE.FindAllStringSubmatchIndex(src, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("PostgreSQL dump has no COPY data for the 3x-ui inbounds table")
	}
	var out []xuiInbound
	for _, m := range matches {
		colsRaw := src[m[2]:m[3]]
		cols := strings.Split(colsRaw, ",")
		for i := range cols {
			cols[i] = strings.ToLower(strings.Trim(strings.TrimSpace(cols[i]), `"`))
		}
		start := m[1]
		for start < len(src) {
			end := strings.IndexByte(src[start:], '\n')
			if end < 0 {
				end = len(src) - start
			}
			line := strings.TrimSuffix(src[start:start+end], "\r")
			start += end
			if start < len(src) && src[start] == '\n' {
				start++
			}
			if line == `\.` {
				break
			}
			if line == "" {
				continue
			}
			fields := splitPostgresCopyRow(line)
			if len(fields) != len(cols) {
				continue
			}
			mrow := map[string]string{}
			for i, col := range cols {
				mrow[col] = fields[i]
			}
			settings := map[string]any{}
			_ = json.Unmarshal([]byte(mrow["settings"]), &settings)
			stream := map[string]any{}
			_ = json.Unmarshal([]byte(mrow["stream_settings"]), &stream)
			sniff := map[string]any{}
			_ = json.Unmarshal([]byte(mrow["sniffing"]), &sniff)
			out = append(out, xuiInbound{Remark: mrow["remark"], Enable: boolVal(mrow["enable"], true), Listen: mrow["listen"], Port: intVal(mrow["port"]), Protocol: strings.ToLower(mrow["protocol"]), Settings: settings, Stream: stream, Sniffing: sniff, Up: int64Val(mrow["up"]), Down: int64Val(mrow["down"]), Total: int64Val(mrow["total"]), Expiry: int64Val(mrow["expiry_time"])})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("PostgreSQL dump contained no readable inbound records")
	}
	return out, nil
}

func splitPostgresCopyRow(line string) []string {
	fields := strings.Split(line, "\t")
	for i, field := range fields {
		fields[i] = decodePostgresCopyField(field)
	}
	return fields
}
func decodePostgresCopyField(s string) string {
	if s == `\N` {
		return ""
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'v':
			b.WriteByte('\v')
		case '\\':
			b.WriteByte('\\')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func sqliteFromSQLDump(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(b) >= 5 && string(b[:5]) == "PGDMP" {
		return "", fmt.Errorf("this is a PostgreSQL custom archive (.dump); export the 3x-ui backup as SQLite .db or Xray JSON before importing")
	}
	lower := strings.ToLower(string(b))
	if !strings.Contains(lower, "create table") || !strings.Contains(lower, "inbounds") {
		return "", fmt.Errorf("SQL dump does not appear to contain a SQLite 3x-ui inbounds table")
	}
	dest, err := os.CreateTemp("", "electruvpn-xui-sql-*.db")
	if err != nil {
		return "", err
	}
	out := dest.Name()
	_ = dest.Close()
	db, err := gorm.Open(sqlite.Open(out), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		os.Remove(out)
		return "", fmt.Errorf("could not create temporary SQLite database")
	}
	sqlDB, err := db.DB()
	if err != nil {
		os.Remove(out)
		return "", err
	}
	sqlDB.SetMaxOpenConns(1) // preserve the temporary SQLite transaction connection
	defer sqlDB.Close()
	statements, err := splitSQLiteDump(string(b))
	if err != nil {
		os.Remove(out)
		return "", err
	}
	for _, stmt := range statements {
		trim := strings.TrimSpace(stripLeadingSQLComments(stmt))
		if trim == "" {
			continue
		}
		upper := strings.ToUpper(trim)
		// A dump is data, not a program. Only allow standard schema/data
		// statements in this isolated database; deny dot-commands, ATTACH,
		// extension loading, triggers and arbitrary PRAGMAs.
		allowed := strings.HasPrefix(upper, "CREATE TABLE ") || strings.HasPrefix(upper, "CREATE INDEX ") || strings.HasPrefix(upper, "CREATE UNIQUE INDEX ") || strings.HasPrefix(upper, "INSERT INTO ") || strings.HasPrefix(upper, "REPLACE INTO ") || strings.HasPrefix(upper, "DELETE FROM ") || upper == "BEGIN TRANSACTION" || upper == "BEGIN" || upper == "COMMIT" || upper == "END TRANSACTION" || upper == "END" || upper == "PRAGMA FOREIGN_KEYS=OFF" || upper == "PRAGMA FOREIGN_KEYS = OFF" || upper == "PRAGMA FOREIGN_KEYS=ON" || upper == "PRAGMA FOREIGN_KEYS = ON"
		if !allowed {
			os.Remove(out)
			return "", fmt.Errorf("SQL dump contains an unsupported statement (%s); use the original .db backup instead", firstSQLWords(upper))
		}
		if err := db.Exec(trim).Error; err != nil {
			os.Remove(out)
			return "", fmt.Errorf("could not restore SQLite SQL dump: %w", err)
		}
	}
	return out, nil
}

func stripLeadingSQLComments(s string) string {
	for {
		s = strings.TrimSpace(s)
		if strings.HasPrefix(s, "--") {
			if i := strings.IndexByte(s, '\n'); i >= 0 {
				s = s[i+1:]
				continue
			}
			return ""
		}
		if strings.HasPrefix(s, "/*") {
			if i := strings.Index(s[2:], "*/"); i >= 0 {
				s = s[i+4:]
				continue
			}
			return ""
		}
		return s
	}
}

func splitSQLiteDump(src string) ([]string, error) {
	var out []string
	start := 0
	var quote byte
	lineComment, blockComment := false, false
	for i := 0; i < len(src); i++ {
		ch := src[i]
		if lineComment {
			if ch == '\n' {
				lineComment = false
			}
			continue
		}
		if blockComment {
			if ch == '*' && i+1 < len(src) && src[i+1] == '/' {
				blockComment = false
				i++
			}
			continue
		}
		if quote != 0 {
			if ch == quote {
				if i+1 < len(src) && src[i+1] == quote {
					i++
					continue
				}
				quote = 0
			} else if ch == '\\' && quote == '\'' && i+1 < len(src) {
				i++
			}
			continue
		}
		if ch == '-' && i+1 < len(src) && src[i+1] == '-' {
			lineComment = true
			i++
			continue
		}
		if ch == '/' && i+1 < len(src) && src[i+1] == '*' {
			blockComment = true
			i++
			continue
		}
		if ch == '\'' || ch == '"' || ch == '`' {
			quote = ch
			continue
		}
		if ch == ';' {
			out = append(out, src[start:i])
			start = i + 1
		}
	}
	if quote != 0 || blockComment {
		return nil, fmt.Errorf("malformed SQL dump")
	}
	if strings.TrimSpace(src[start:]) != "" {
		out = append(out, src[start:])
	}
	return out, nil
}
func firstSQLWords(s string) string {
	fields := strings.Fields(s)
	if len(fields) > 3 {
		fields = fields[:3]
	}
	return strings.Join(fields, " ")
}

// sniffXUIImportKind decides what an uploaded file is from its content, not
// only its extension. 3x-ui's single-inbound "Export" is JSON text that users
// commonly save as .txt (or with no extension), which used to be treated as a
// SQLite database and rejected.
func sniffXUIImportKind(path, name string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	head := make([]byte, 4096)
	n, _ := f.Read(head)
	head = head[:n]
	if bytes.HasPrefix(head, []byte("SQLite format 3\x00")) {
		return "sqlite"
	}
	if bytes.HasPrefix(head, []byte("PGDMP")) {
		return "postgres"
	}
	text := bytes.TrimLeft(head, "\xef\xbb\xbf \t\r\n")
	if len(text) > 0 && (text[0] == '{' || text[0] == '[') {
		return "json"
	}
	lower := strings.ToLower(string(text))
	for _, scheme := range []string{"vless://", "vmess://", "trojan://", "ss://"} {
		if strings.HasPrefix(lower, scheme) {
			return "links"
		}
	}
	if isPostgresCopyDump(path) {
		return "postgres"
	}
	if strings.Contains(lower, "pragma") || strings.Contains(lower, "begin transaction") || strings.Contains(lower, "create table") || strings.Contains(lower, "insert into") || strings.HasSuffix(name, ".sql") || strings.HasSuffix(name, ".dump") {
		return "sqldump"
	}
	if strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".txt") {
		return "json"
	}
	return ""
}

// jsonObjField parses a 3x-ui JSON field that may be a nested object (Xray
// config / API) or a JSON-encoded string (3x-ui DB-style export).
func jsonObjField(v any) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		return x
	case string:
		out := map[string]any{}
		if strings.TrimSpace(x) != "" {
			_ = json.Unmarshal([]byte(x), &out)
		}
		return out
	}
	return map[string]any{}
}

func firstField(row map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := row[k]; ok && v != nil {
			return v
		}
	}
	return nil
}

// parseXrayJSON accepts: a full Xray config ({"inbounds":[...]}), a single
// 3x-ui inbound export ({"remark":..,"port":..,"protocol":..,"settings":"{..}",
// "streamSettings":"{..}"}), an array of those, or a 3x-ui API response that
// wraps them in {"obj": ...}.
func parseXrayJSON(b []byte) ([]xuiInbound, error) {
	b = bytes.TrimPrefix(bytes.TrimSpace(b), []byte("\xef\xbb\xbf"))
	var anyRoot any
	if err := json.Unmarshal(b, &anyRoot); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	var list []map[string]any
	var collect func(v any, depth int)
	collect = func(v any, depth int) {
		if depth > 4 {
			return
		}
		switch x := v.(type) {
		case []any:
			for _, item := range x {
				collect(item, depth+1)
			}
		case map[string]any:
			if _, ok := x["protocol"]; ok {
				if _, hasPort := x["port"]; hasPort {
					list = append(list, x)
					return
				}
			}
			for _, key := range []string{"inbounds", "obj", "data", "inbound"} {
				if child, ok := x[key]; ok {
					collect(child, depth+1)
				}
			}
		}
	}
	collect(anyRoot, 0)
	if len(list) == 0 {
		return nil, fmt.Errorf("no inbound found in this JSON; expected a 3x-ui inbound export (remark/port/protocol/settings) or an Xray config with an inbounds array")
	}
	out := make([]xuiInbound, 0, len(list))
	for _, row := range list {
		settings := jsonObjField(firstField(row, "settings"))
		stream := jsonObjField(firstField(row, "streamSettings", "stream_settings"))
		sniff := jsonObjField(firstField(row, "sniffing"))
		remark := strVal(row["remark"])
		if remark == "" {
			remark = strVal(row["tag"])
		}
		// 3x-ui exports per-client traffic in clientStats.
		if stats, ok := row["clientStats"].([]any); ok {
			byEmail := map[string]map[string]any{}
			for _, sv := range stats {
				if sm, ok := sv.(map[string]any); ok {
					byEmail[strVal(sm["email"])] = sm
				}
			}
			if clients, ok := settings["clients"].([]any); ok {
				for _, cv := range clients {
					if cm, ok := cv.(map[string]any); ok {
						if st, found := byEmail[strVal(cm["email"])]; found {
							cm["up"], cm["down"] = int64Val(st["up"]), int64Val(st["down"])
						}
					}
				}
			}
		}
		out = append(out, xuiInbound{
			Remark: remark, Enable: boolVal(row["enable"], true), Listen: strVal(row["listen"]),
			Port: intVal(row["port"]), Protocol: strings.ToLower(strVal(row["protocol"])),
			Settings: settings, Stream: stream, Sniffing: sniff,
			Up: int64Val(row["up"]), Down: int64Val(row["down"]), Total: int64Val(row["total"]),
			Expiry: int64Val(firstField(row, "expiryTime", "expiry_time")),
		})
	}
	return out, nil
}

func translateXUIInbound(raw xuiInbound, xrayBinary string) (models.Inbound, []models.Client, string) {
	proto := strings.ToLower(raw.Protocol)
	switch proto {
	case "vless", "vmess", "trojan", "shadowsocks":
	default:
		return models.Inbound{}, nil, "protocol is not supported by ElectruVPN's Xray importer: " + proto
	}
	if raw.Port < 1 || raw.Port > 65535 {
		return models.Inbound{}, nil, "invalid or missing port"
	}
	stream := raw.Stream
	network := strings.ToLower(strVal(stream["network"]))
	if network == "" || network == "raw" {
		network = "tcp"
	}
	switch network {
	case "tcp", "ws", "grpc", "xhttp", "httpupgrade", "kcp":
	default:
		return models.Inbound{}, nil, "unsupported transport: " + network
	}
	security := strings.ToLower(strVal(stream["security"]))
	if security == "" {
		security = "none"
	}
	if security != "none" && security != "tls" && security != "reality" {
		return models.Inbound{}, nil, "unsupported security mode: " + security
	}
	settings := map[string]any{}
	if proto == "shadowsocks" {
		method := strVal(raw.Settings["method"])
		if method == "" {
			method = "aes-128-gcm"
		}
		settings["method"] = method
		// 3x-ui keeps the Shadowsocks-2022 server key in settings.password.
		if pw := strVal(raw.Settings["password"]); pw != "" && xray.ShadowsocksKeyLength(method) > 0 {
			settings["password"] = pw
		}
	}
	ss := map[string]any{"network": network, "security": security}
	switch network {
	case "ws":
		v, _ := stream["wsSettings"].(map[string]any)
		headers, _ := v["headers"].(map[string]any)
		ss["wsPath"] = orXUI(strVal(v["path"]), "/")
		ss["wsHost"] = strVal(headers["Host"])
	case "grpc":
		v, _ := stream["grpcSettings"].(map[string]any)
		ss["grpcServiceName"] = strVal(v["serviceName"])
	case "xhttp":
		v, _ := stream["xhttpSettings"].(map[string]any)
		ss["xhttpPath"] = orXUI(strVal(v["path"]), "/")
		ss["xhttpHost"] = strVal(v["host"])
		ss["xhttpMode"] = orXUI(strVal(v["mode"]), "auto")
	case "httpupgrade":
		v, _ := stream["httpupgradeSettings"].(map[string]any)
		headers, _ := v["headers"].(map[string]any)
		ss["httpUpgradePath"] = orXUI(strVal(v["path"]), "/")
		ss["httpUpgradeHost"] = orXUI(strVal(v["host"]), strVal(headers["Host"]))
	case "kcp":
		network = "mkcp"
		ss["network"] = network
	case "quic":
		ss["network"] = "quic"
	}
	if security == "tls" {
		v, _ := stream["tlsSettings"].(map[string]any)
		certs, _ := v["certificates"].([]any)
		certFile, keyFile := "", ""
		if len(certs) > 0 {
			if cert, ok := certs[0].(map[string]any); ok {
				certFile = strVal(cert["certificateFile"])
				keyFile = strVal(cert["keyFile"])
			}
		}
		alpn := ""
		if a, ok := v["alpn"].([]any); ok {
			parts := []string{}
			for _, p := range a {
				parts = append(parts, strVal(p))
			}
			alpn = strings.Join(parts, ",")
		}
		ss["tlsSettings"] = map[string]any{"certFile": certFile, "keyFile": keyFile, "sni": strVal(v["serverName"]), "alpn": alpn}
	}
	if security == "reality" {
		v, _ := stream["realitySettings"].(map[string]any)
		realityClientSettings, _ := v["settings"].(map[string]any)
		dest := strVal(v["dest"])
		if dest == "" {
			dest = strVal(v["target"])
		}
		names := stringSlice(v["serverNames"])
		shorts := stringSlice(v["shortIds"])
		publicKey := strVal(realityClientSettings["publicKey"])
		if publicKey == "" {
			publicKey = strVal(v["publicKey"])
		}
		privateKey := strVal(v["privateKey"])
		// 3x-ui stores the REALITY server private key in stream_settings, but
		// normally does not store its client-facing public key. Importing an
		// empty publicKey creates links with an empty pbk= and clients cannot
		// handshake. Derive the matching key from the existing private key;
		// never generate a replacement key, which would invalidate old clients.
		if publicKey == "" && privateKey != "" {
			var deriveErr error
			publicKey, deriveErr = deriveXUIRealityPublicKey(xrayBinary, privateKey)
			if deriveErr != nil {
				return models.Inbound{}, nil, "could not derive REALITY public key from imported private key: " + deriveErr.Error()
			}
		}
		if privateKey == "" || publicKey == "" || len(names) == 0 || len(shorts) == 0 {
			return models.Inbound{}, nil, "REALITY inbound is missing private key, public key, server name, or short ID"
		}
		spiderX := strVal(realityClientSettings["spiderX"])
		if spiderX == "" {
			spiderX = strVal(v["spiderX"])
		}
		ss["realitySettings"] = map[string]any{"dest": dest, "serverNames": names, "privateKey": privateKey, "publicKey": publicKey, "shortIds": shorts, "spiderX": spiderX}
	}
	if len(raw.Sniffing) > 0 {
		ss["sniffing"] = raw.Sniffing
	}
	settingsJSON, _ := json.Marshal(settings)
	if proto == "shadowsocks" {
		settingsJSON = []byte(xray.EnsureShadowsocksServerKey(string(settingsJSON)))
	}
	streamJSON, _ := json.Marshal(ss)
	inboundExpiry := raw.Expiry
	if inboundExpiry < 0 {
		inboundExpiry = 0
	}
	ib := models.Inbound{Remark: truncateXUI(raw.Remark, 128), Enable: raw.Enable, Listen: raw.Listen, IPMode: "ipv4", SubscriptionEnabled: true, SubscriptionDefaultAddress: "all", SubscriptionPreset: "standard", Port: raw.Port, Protocol: models.Protocol(proto), Network: models.Network(network), Security: models.Security(security), Settings: string(settingsJSON), StreamSettings: string(streamJSON), Sniffing: true, Labels: "imported-3x-ui", ExpiryTime: inboundExpiry, TotalGB: xuiBytesToGB(raw.Total), Up: raw.Up, Down: raw.Down}
	if ib.Listen == "" {
		ib.Listen = "0.0.0.0"
	}
	if strings.TrimSpace(ib.Listen) == "::" || strings.Contains(ib.Listen, ":") && !strings.Contains(ib.Listen, ".") {
		ib.IPMode = "ipv6"
	}
	if ib.Remark == "" {
		ib.Remark = fmt.Sprintf("3x-ui %s %d", proto, raw.Port)
	}
	ib.Tag = fmt.Sprintf("inbound-%d-%s", ib.Port, uuid.NewString()[:8])
	clients := extractXUIClients(raw, proto)
	if len(clients) == 0 {
		return models.Inbound{}, nil, "no usable clients were found in this inbound"
	}
	return ib, clients, ""
}

// deriveXUIRealityPublicKey derives the public/password half of an X25519
// REALITY key pair using the installed Xray binary. Xray versions label this
// output either "Public key" or "Password (PublicKey)".
func deriveXUIRealityPublicKey(binary, privateKey string) (string, error) {
	if strings.TrimSpace(binary) == "" {
		return "", fmt.Errorf("Xray binary path is not configured")
	}
	out, err := exec.Command(binary, "x25519", "-i", strings.TrimSpace(privateKey)).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("xray x25519 failed: %s", strings.TrimSpace(string(out)))
	}
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(parts) != 2 {
			continue
		}
		label := strings.ToLower(strings.TrimSpace(parts[0]))
		if strings.HasPrefix(label, "public") || strings.HasPrefix(label, "password") {
			key := strings.TrimSpace(parts[1])
			if key != "" {
				return key, nil
			}
		}
	}
	return "", fmt.Errorf("Xray output did not contain a public key")
}

func extractXUIClients(raw xuiInbound, proto string) []models.Client {
	var rawClients []map[string]any
	if a, ok := raw.Settings["clients"].([]any); ok {
		for _, v := range a {
			if m, ok := v.(map[string]any); ok {
				rawClients = append(rawClients, m)
			}
		}
	}
	if proto == "shadowsocks" && len(rawClients) == 0 {
		// Xray JSON SS inbounds may use one password rather than a clients array.
		// (With a clients array, "password" is the SERVER key and must not become a client.)
		if p := strVal(raw.Settings["password"]); p != "" {
			if xray.ShadowsocksKeyLength(strVal(raw.Settings["method"])) > 0 {
				// 2022 needs a user key distinct from the server key.
				if k := newUserKeyFor(strVal(raw.Settings["method"])); k != "" {
					p = k
				}
			}
			rawClients = append(rawClients, map[string]any{"password": p, "email": raw.Remark})
		}
	}
	out := make([]models.Client, 0, len(rawClients))
	seen := map[string]bool{}
	for i, r := range rawClients {
		email := strings.TrimSpace(strVal(r["email"]))
		if email == "" {
			email = fmt.Sprintf("import-%d-%d", raw.Port, i+1)
		}
		email = truncateXUI(email, 128)
		if seen[email] {
			email = truncateXUI(email+"-"+strconv.Itoa(i+1), 128)
		}
		seen[email] = true
		expiry := int64Val(r["expiryTime"])
		if expiry < 0 {
			expiry = 0
		} // 3x-ui's first-use expiry marker is not a fixed timestamp.
		cl := models.Client{Email: email, Enable: boolVal(r["enable"], true), TotalGB: xuiBytesToGB(int64Val(r["totalGB"])), ExpiryTime: expiry, Up: int64Val(r["up"]), Down: int64Val(r["down"]), LimitIP: intVal(r["limitIp"]), Flow: strVal(r["flow"]), SubscriptionEnabled: true, SubscriptionAutoUpdate: true, SubscriptionAddressType: "all", SubscriptionStatus: "active", SubscriptionTokenVersion: 1, SubID: strings.ReplaceAll(uuid.NewString(), "-", "")[:32], CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if proto == "vless" || proto == "vmess" {
			cl.UUID = strVal(r["id"])
			if cl.UUID == "" {
				cl.UUID = strVal(r["uuid"])
			}
		} else {
			cl.Password = strVal(r["password"])
			if cl.Password == "" && proto == "shadowsocks" {
				cl.Password = strVal(r["method"]) + ":" + strVal(r["password"])
			}
		}
		if proto == "shadowsocks" && cl.Password == "" {
			cl.Password = strVal(r["password"])
		}
		if (proto == "vless" || proto == "vmess") && cl.UUID == "" {
			continue
		}
		if (proto == "trojan" || proto == "shadowsocks") && cl.Password == "" {
			continue
		}
		out = append(out, cl)
	}
	return out
}

func xuiBytesToGB(value int64) int64 {
	if value <= 0 {
		return 0
	}
	const gb = int64(1024 * 1024 * 1024)
	return (value + gb - 1) / gb
}

func strVal(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []byte:
		return string(x)
	default:
		return fmt.Sprint(x)
	}
}
func intVal(v any) int     { n, _ := strconv.Atoi(strings.TrimSpace(strVal(v))); return n }
func int64Val(v any) int64 { n, _ := strconv.ParseInt(strings.TrimSpace(strVal(v)), 10, 64); return n }
func boolVal(v any, def bool) bool {
	if v == nil {
		return def
	}
	s := strings.ToLower(strings.TrimSpace(strVal(v)))
	return s == "1" || s == "true" || s == "yes" || s == "t"
}
func stringSlice(v any) []string {
	a, ok := v.([]any)
	if !ok {
		return nil
	}
	out := []string{}
	for _, x := range a {
		if s := strVal(x); s != "" {
			out = append(out, s)
		}
	}
	return out
}
func orXUI(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
func truncateXUI(v string, max int) string { return truncateRunes(v, max) }

// newUserKeyFor returns a random base64 key sized for a Shadowsocks-2022 method.
func newUserKeyFor(method string) string {
	n := xray.ShadowsocksKeyLength(method)
	if n == 0 {
		return ""
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(b)
}
