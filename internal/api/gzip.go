package api

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// gzipWriter wraps gin's ResponseWriter so every Write/WriteString call goes
// through a gzip.Writer instead. Embedding gin.ResponseWriter means every
// other method (Status, Size, Flush, Hijack, ...) is delegated automatically
// -- only the two write paths need overriding.
type gzipWriter struct {
	gin.ResponseWriter
	writer io.Writer
}

func (g *gzipWriter) Write(data []byte) (int, error) {
	return g.writer.Write(data)
}

func (g *gzipWriter) WriteString(s string) (int, error) {
	return g.writer.Write([]byte(s))
}

// gzipMiddleware transparently compresses API responses for clients that
// advertise gzip support (every browser does). It's deliberately scoped to
// the JSON /api routes only -- the static asset and HTML file handlers rely
// on Go's http.ServeContent setting an exact Content-Length up front, and
// compressing after that would leave a stale length header and a corrupted
// transfer for those specific routes.
func gzipMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !strings.Contains(c.GetHeader("Accept-Encoding"), "gzip") {
			c.Next()
			return
		}

		c.Header("Content-Encoding", "gzip")
		c.Header("Vary", "Accept-Encoding")

		gz := gzip.NewWriter(c.Writer)
		defer gz.Close()

		c.Writer = &gzipWriter{ResponseWriter: c.Writer, writer: gz}
		c.Next()
	}
}

// gzipBytes compresses raw once at the given gzip level.
func gzipBytes(raw []byte, level int) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, level)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(raw); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// compressedAsset is an in-memory gzip copy of a static file. It is rebuilt
// only when the file's modification time or size changes (the ETag), so the
// ~1.2 MB of JS/CSS is compressed once instead of being sent raw on every
// cold load.
type compressedAsset struct {
	etag string
	data []byte
}

var (
	staticGzipMu    sync.Mutex
	staticGzipCache = map[string]compressedAsset{}

	htmlGzipMu    sync.Mutex
	htmlGzipCache = map[string]htmlGzipEntry{}
)

type htmlGzipEntry struct {
	src string
	gz  []byte
}

func isCompressibleExt(ext string) bool {
	switch ext {
	case ".js", ".css", ".svg", ".json", ".html", ".txt", ".xml":
		return true
	}
	return false
}

// gzipStaticAsset returns the cached gzip copy of a static file, building it
// on first use. ok=false means "serve the file normally".
func gzipStaticAsset(full, etag string) ([]byte, bool) {
	staticGzipMu.Lock()
	if hit, found := staticGzipCache[full]; found && hit.etag == etag {
		staticGzipMu.Unlock()
		return hit.data, true
	}
	staticGzipMu.Unlock()
	raw, err := os.ReadFile(full)
	if err != nil || len(raw) < 1024 {
		return nil, false
	}
	gz, err := gzipBytes(raw, gzip.BestCompression)
	if err != nil || len(gz) >= len(raw) {
		return nil, false
	}
	staticGzipMu.Lock()
	staticGzipCache[full] = compressedAsset{etag: etag, data: gz}
	staticGzipMu.Unlock()
	return gz, true
}

// gzipHTMLCached compresses the (already base-path-injected) SPA shell and
// reuses the result while the source text is unchanged.
func gzipHTMLCached(key, src string) ([]byte, bool) {
	htmlGzipMu.Lock()
	if hit, found := htmlGzipCache[key]; found && hit.src == src {
		htmlGzipMu.Unlock()
		return hit.gz, true
	}
	htmlGzipMu.Unlock()
	gz, err := gzipBytes([]byte(src), gzip.BestCompression)
	if err != nil || len(gz) >= len(src) {
		return nil, false
	}
	htmlGzipMu.Lock()
	htmlGzipCache[key] = htmlGzipEntry{src: src, gz: gz}
	htmlGzipMu.Unlock()
	return gz, true
}

// cacheStaticAssets sets a same-day browser cache header on hashed-by-path
// static files (CSS/JS). Cheap win: repeat visits to the panel skip
// re-downloading assets that essentially never change between requests.
func cacheStaticAssets(webDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Assets are not content-hashed in the repository, so use a short
		// browser cache plus a cheap ETag instead of unsafe immutable caching.
		path := c.Request.URL.Path
		if idx := strings.Index(path, "/assets/"); idx >= 0 {
			name := strings.TrimPrefix(path[idx+len("/assets/"):], "/")
			if name != "" && !strings.Contains(name, "..") {
				if st, err := os.Stat(filepath.Join(webDir, "assets", filepath.FromSlash(name))); err == nil && !st.IsDir() {
					etag := fmt.Sprintf("W/\"%x-%x\"", st.ModTime().UnixNano(), st.Size())
					c.Header("ETag", etag)
					c.Header("Cache-Control", "no-cache")
					c.Header("Vary", "Accept-Encoding")
					if c.GetHeader("If-None-Match") == etag {
						c.Status(http.StatusNotModified)
						c.Abort()
						return
					}
					// Serve a pre-compressed copy with an exact Content-Length.
					ext := strings.ToLower(filepath.Ext(name))
					if isCompressibleExt(ext) && strings.Contains(c.GetHeader("Accept-Encoding"), "gzip") {
						full := filepath.Join(webDir, "assets", filepath.FromSlash(name))
						if gz, ok := gzipStaticAsset(full, etag); ok {
							ct := mime.TypeByExtension(ext)
							if ct == "" {
								ct = "application/octet-stream"
							}
							c.Header("Content-Encoding", "gzip")
							c.Data(http.StatusOK, ct, gz)
							c.Abort()
							return
						}
					}
				}
			}
		}
		c.Next()
	}
}
