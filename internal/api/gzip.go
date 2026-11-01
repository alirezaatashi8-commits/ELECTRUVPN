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

// gzipPool reuses gzip.Writer instances. A fresh gzip.NewWriter allocates
// several hundred KB of compressor state, so creating one per API request
// dominated allocation volume (GC churn / RSS) on busy panels.
var gzipPool = sync.Pool{
	New: func() any {
		// BestSpeed: dynamic JSON still shrinks 5-8x at a fraction of the CPU.
		w, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
		return w
	},
}

// gzipWriter wraps gin's ResponseWriter so Write/WriteString go through a
// pooled gzip.Writer. The compressor is attached lazily on the first body
// write, so responses with no body (204/304, bare AbortWithStatus, HEAD) are
// no longer sent with a bogus "Content-Encoding: gzip" header and an empty
// gzip stream. Embedding gin.ResponseWriter delegates every other method.
type gzipWriter struct {
	gin.ResponseWriter
	gz          *gzip.Writer
	started     bool
	passthrough bool
}

func (g *gzipWriter) start() {
	if g.started {
		return
	}
	g.started = true
	h := g.ResponseWriter.Header()
	switch g.ResponseWriter.Status() {
	case http.StatusNoContent, http.StatusNotModified:
		g.passthrough = true
	}
	// Respect handlers that already encoded the body or set an exact length.
	if h.Get("Content-Encoding") != "" || h.Get("Content-Length") != "" {
		g.passthrough = true
	}
	if g.passthrough {
		return
	}
	g.gz = gzipPool.Get().(*gzip.Writer)
	g.gz.Reset(g.ResponseWriter)
	h.Set("Content-Encoding", "gzip")
	h.Del("Content-Length")
}

func (g *gzipWriter) Write(data []byte) (int, error) {
	g.start()
	if g.passthrough {
		return g.ResponseWriter.Write(data)
	}
	return g.gz.Write(data)
}

func (g *gzipWriter) WriteString(s string) (int, error) {
	g.start()
	if g.passthrough {
		return g.ResponseWriter.WriteString(s)
	}
	return g.gz.Write([]byte(s))
}

// Flush keeps streaming responses (logs/SSE) live: push buffered compressed
// bytes out before flushing the underlying connection.
func (g *gzipWriter) Flush() {
	if g.gz != nil {
		_ = g.gz.Flush()
	}
	g.ResponseWriter.Flush()
}

func (g *gzipWriter) release() {
	if g.gz == nil {
		return
	}
	_ = g.gz.Close()
	g.gz.Reset(io.Discard)
	gzipPool.Put(g.gz)
	g.gz = nil
}

// gzipMiddleware transparently compresses API responses for clients that
// advertise gzip support (every browser does). It's deliberately scoped to
// the JSON /api routes only -- the static asset and HTML file handlers rely
// on Go's http.ServeContent setting an exact Content-Length up front, and
// compressing after that would leave a stale length header and a corrupted
// transfer for those specific routes.
func gzipMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !strings.Contains(c.GetHeader("Accept-Encoding"), "gzip") ||
			c.Request.Method == http.MethodHead ||
			strings.EqualFold(c.GetHeader("Connection"), "upgrade") {
			c.Next()
			return
		}
		c.Writer.Header().Add("Vary", "Accept-Encoding")
		gw := &gzipWriter{ResponseWriter: c.Writer}
		defer gw.release()
		c.Writer = gw
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
