package handlers

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGzipOrders(t *testing.T) {
	h := wrapHandlerForGzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/user/orders", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	res := rec.Result()
	if ce := res.Header.Get("Content-Encoding"); ce != "gzip" {
		t.Fatalf("expected gzip encoding, got %q", ce)
	}

	reader, err := gzip.NewReader(res.Body)
	if err != nil {
		t.Fatalf("expected gzip reader: %v", err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read gzip body: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected non-empty body")
	}
}

func TestGzipWithdrawals(t *testing.T) {
	h := wrapHandlerForGzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/user/withdrawals", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	res := rec.Result()
	if ce := res.Header.Get("Content-Encoding"); ce != "gzip" {
		t.Fatalf("expected gzip encoding, got %q", ce)
	}

	reader, err := gzip.NewReader(res.Body)
	if err != nil {
		t.Fatalf("expected gzip reader: %v", err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read gzip body: %v", err)
	}
	if string(data) != "[]" {
		t.Fatalf("unexpected body: %s", data)
	}
}

func TestGzipSkipsWhenNotRequested(t *testing.T) {
	h := wrapHandlerForGzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/user/orders", nil)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if ce := rec.Header().Get("Content-Encoding"); ce != "" {
		t.Fatalf("expected no encoding, got %q", ce)
	}
}

func TestGzipSkipsForOtherPath(t *testing.T) {
	h := wrapHandlerForGzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if ce := rec.Header().Get("Content-Encoding"); ce != "" {
		t.Fatalf("expected no encoding, got %q", ce)
	}
}

func TestGzipSkipsOnNoContent(t *testing.T) {
	h := wrapHandlerForGzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/user/withdrawals", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	res := rec.Result()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res.StatusCode)
	}
	if ce := res.Header.Get("Content-Encoding"); ce != "" {
		t.Fatalf("expected no encoding, got %q", ce)
	}
	body, _ := io.ReadAll(res.Body)
	if len(body) != 0 {
		t.Fatalf("expected empty body, got %q", string(body))
	}
}
