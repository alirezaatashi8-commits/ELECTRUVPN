package api

import (
	"bytes"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestBodyLimitAllowsConfiguredUploadPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	payload := bytes.Repeat([]byte("x"), (5<<20)+1)
	cases := []struct {
		name        string
		path        string
		wantReadErr bool
	}{
		{name: "ordinary API remains limited", path: "/api/clients", wantReadErr: true},
		{name: "3x-ui database upload exceeds ordinary API limit", path: "/api/inbounds/import-3x-ui"},
		{name: "panel backup upload exceeds ordinary API limit", path: "/api/pro/backups/upload"},
		{name: "base-path 3x-ui upload", path: "/panel/api/inbounds/import-3x-ui"},
		{name: "base-path backup upload", path: "/panel/api/pro/backups/upload"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", tc.path, bytes.NewReader(payload))
			rec := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(rec)
			ctx.Request = req
			var readErr error
			requestBodyLimit(4 << 20)(ctx)
			_, readErr = io.Copy(io.Discard, ctx.Request.Body)
			if tc.wantReadErr && readErr == nil {
				t.Fatal("expected ordinary API request to hit the 4 MiB limit")
			}
			if !tc.wantReadErr && readErr != nil {
				t.Fatalf("upload path unexpectedly hit the 4 MiB limit: %v", readErr)
			}
		})
	}
}
