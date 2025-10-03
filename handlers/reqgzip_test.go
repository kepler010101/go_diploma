package handlers_test

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"testing"

	"gophermart/handlers"
)

func gzipPayload(t *testing.T, data string) *bytes.Buffer {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(data)); err != nil {
		t.Fatalf("write gzip: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return &buf
}

func TestCreateOrderGzipBody(t *testing.T) {
	authSvc := &stubAuthService{validateID: 70}
	orderSvc := &stubOrderService{created: true}
	balanceSvc := &stubBalanceService{}
	withdrawSvc := &stubWithdrawService{}
	router := handlers.NewRouter(authSvc, orderSvc, balanceSvc, withdrawSvc)

	body := gzipPayload(t, "18")
	req := httptest.NewRequest(http.MethodPost, "/api/user/orders", bytes.NewReader(body.Bytes()))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Content-Encoding", "gzip")
	req.AddCookie(&http.Cookie{Name: "token", Value: "valid"})

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", rr.Code)
	}
	if orderSvc.gotSubmitNumber != "18" {
		t.Fatalf("expected number 18, got %q", orderSvc.gotSubmitNumber)
	}
}

func TestCreateOrderBadGzip(t *testing.T) {
	authSvc := &stubAuthService{validateID: 71}
	orderSvc := &stubOrderService{}
	balanceSvc := &stubBalanceService{}
	withdrawSvc := &stubWithdrawService{}
	router := handlers.NewRouter(authSvc, orderSvc, balanceSvc, withdrawSvc)

	req := httptest.NewRequest(http.MethodPost, "/api/user/orders", bytes.NewReader([]byte("bad")))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Content-Encoding", "gzip")
	req.AddCookie(&http.Cookie{Name: "token", Value: "valid"})

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rr.Code)
	}
	if orderSvc.gotSubmitNumber != "" {
		t.Fatalf("order service should not be called, got %q", orderSvc.gotSubmitNumber)
	}
}
