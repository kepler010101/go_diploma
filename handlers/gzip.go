package handlers

import (
	"compress/gzip"
	"net/http"
	"strings"
)

type gzipResponseWriter struct {
	http.ResponseWriter
	gzipWriter  *gzip.Writer
	useGzip     bool
	wroteHeader bool
}

func newGzipResponseWriter(w http.ResponseWriter) *gzipResponseWriter {
	return &gzipResponseWriter{ResponseWriter: w, useGzip: true}
}

func (g *gzipResponseWriter) WriteHeader(status int) {
	if g.wroteHeader {
		return
	}
	g.wroteHeader = true
	if status == http.StatusNoContent {
		g.useGzip = false
	} else if g.useGzip {
		header := g.Header()
		header.Del("Content-Length")
		header.Set("Content-Encoding", "gzip")
	}
	g.ResponseWriter.WriteHeader(status)
}

func (g *gzipResponseWriter) ensureWriter() {
	if !g.useGzip || g.gzipWriter != nil {
		return
	}
	g.gzipWriter = gzip.NewWriter(g.ResponseWriter)
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if !g.wroteHeader {
		g.WriteHeader(http.StatusOK)
	}
	if !g.useGzip {
		return g.ResponseWriter.Write(b)
	}
	g.ensureWriter()
	return g.gzipWriter.Write(b)
}

func (g *gzipResponseWriter) Flush() {
	if g.useGzip && g.gzipWriter != nil {
		_ = g.gzipWriter.Flush()
	}
	if flusher, ok := g.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (g *gzipResponseWriter) Close() {
	if g.useGzip && g.gzipWriter != nil {
		_ = g.gzipWriter.Close()
	}
}

func wrapHandlerForGzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !shouldUseGzip(r) {
			next.ServeHTTP(w, r)
			return
		}
		gw := newGzipResponseWriter(w)
		defer gw.Close()
		next.ServeHTTP(gw, r)
	})
}

func shouldUseGzip(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	path := r.URL.Path
	if path != "/api/user/orders" && path != "/api/user/withdrawals" {
		return false
	}
	accept := strings.ToLower(r.Header.Get("Accept-Encoding"))
	return strings.Contains(accept, "gzip")
}
