package handlers

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
)

type gzipBody struct {
	io.ReadCloser
	raw io.ReadCloser
}

func (g *gzipBody) Close() error {
	err1 := g.ReadCloser.Close()
	err2 := g.raw.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

func decompressGzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		raw := r.Body
		reader, err := gzip.NewReader(raw)
		if err != nil {
			_ = raw.Close()
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		r.Body = &gzipBody{ReadCloser: reader, raw: raw}
		r.Header.Del("Content-Encoding")
		next.ServeHTTP(w, r)
	})
}
