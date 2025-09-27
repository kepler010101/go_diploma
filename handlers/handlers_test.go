package handlers_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gophermart/handlers"
	"gophermart/repo"
	"gophermart/service"
)

type stubAuthService struct {
	registerToken string
	registerErr   error
	loginToken    string
	loginErr      error
	validateID    int64
	validateErr   error
}

func (s *stubAuthService) Register(_ context.Context, login, password string) (string, error) {
	return s.registerToken, s.registerErr
}

func (s *stubAuthService) Login(_ context.Context, login, password string) (string, error) {
	return s.loginToken, s.loginErr
}

func (s *stubAuthService) ValidateToken(_ context.Context, token string) (int64, error) {
	if s.validateErr != nil {
		return 0, s.validateErr
	}
	return s.validateID, nil
}

type stubOrderService struct {
	created         bool
	submitErr       error
	listOrders      []repo.Order
	listErr         error
	gotSubmitUserID int64
	gotSubmitNumber string
	gotListUserID   int64
}

func (s *stubOrderService) SubmitOrder(_ context.Context, userID int64, number string) (bool, error) {
	s.gotSubmitUserID = userID
	s.gotSubmitNumber = number
	return s.created, s.submitErr
}

func (s *stubOrderService) ListUserOrders(_ context.Context, userID int64) ([]repo.Order, error) {
	s.gotListUserID = userID
	if s.listOrders == nil {
		return []repo.Order{}, s.listErr
	}
	return s.listOrders, s.listErr
}

type stubBalanceService struct {
	current   float64
	withdrawn float64
	err       error
	gotUserID int64
}

func (s *stubBalanceService) GetBalance(_ context.Context, userID int64) (float64, float64, error) {
	s.gotUserID = userID
	if s.err != nil {
		return 0, 0, s.err
	}
	return s.current, s.withdrawn, nil
}

type stubWithdrawService struct {
	err         error
	list        []repo.Withdrawal
	listErr     error
	gotUserID   int64
	gotOrder    string
	gotSum      float64
	gotListUser int64
}

func (s *stubWithdrawService) Withdraw(_ context.Context, userID int64, order string, amount float64) error {
	s.gotUserID = userID
	s.gotOrder = order
	s.gotSum = amount
	return s.err
}

func (s *stubWithdrawService) ListUserWithdrawals(_ context.Context, userID int64) ([]repo.Withdrawal, error) {
	s.gotListUser = userID
	if s.list != nil {
		return s.list, s.listErr
	}
	return []repo.Withdrawal{}, s.listErr
}

func TestRegisterSuccess(t *testing.T) {
	authSvc := &stubAuthService{registerToken: "token"}
	orderSvc := &stubOrderService{}
	balanceSvc := &stubBalanceService{}
	withdrawSvc := &stubWithdrawService{}
	router := handlers.NewRouter(authSvc, orderSvc, balanceSvc, withdrawSvc)

	req := httptest.NewRequest(http.MethodPost, "/api/user/register", strings.NewReader(`{"login":"foo","password":"bar"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	res := rr.Result()
	t.Cleanup(func() { res.Body.Close() })
	cookies := res.Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "token" && c.Value == "token" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected token cookie")
	}
}

func TestRegisterConflict(t *testing.T) {
	authSvc := &stubAuthService{registerErr: service.ErrLoginTaken}
	orderSvc := &stubOrderService{}
	balanceSvc := &stubBalanceService{}
	withdrawSvc := &stubWithdrawService{}
	router := handlers.NewRouter(authSvc, orderSvc, balanceSvc, withdrawSvc)

	req := httptest.NewRequest(http.MethodPost, "/api/user/register", strings.NewReader(`{"login":"foo","password":"bar"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", rr.Code)
	}
}

func TestLoginUnauthorized(t *testing.T) {
	authSvc := &stubAuthService{loginErr: service.ErrInvalidCredentials}
	orderSvc := &stubOrderService{}
	balanceSvc := &stubBalanceService{}
	withdrawSvc := &stubWithdrawService{}
	router := handlers.NewRouter(authSvc, orderSvc, balanceSvc, withdrawSvc)

	req := httptest.NewRequest(http.MethodPost, "/api/user/login", strings.NewReader(`{"login":"foo","password":"bar"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rr.Code)
	}
}

func TestCreateOrderAcceptedNew(t *testing.T) {
	authSvc := &stubAuthService{validateID: 42}
	orderSvc := &stubOrderService{created: true}
	balanceSvc := &stubBalanceService{}
	withdrawSvc := &stubWithdrawService{}
	router := handlers.NewRouter(authSvc, orderSvc, balanceSvc, withdrawSvc)

	req := httptest.NewRequest(http.MethodPost, "/api/user/orders", strings.NewReader("18"))
	req.Header.Set("Content-Type", "text/plain")
	req.AddCookie(&http.Cookie{Name: "token", Value: "valid"})
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", rr.Code)
	}
	if orderSvc.gotSubmitUserID != 42 || orderSvc.gotSubmitNumber != "18" {
		t.Fatalf("expected order service to receive user=42 number=18, got user=%d number=%s", orderSvc.gotSubmitUserID, orderSvc.gotSubmitNumber)
	}
}

func TestCreateOrderIdempotent(t *testing.T) {
	authSvc := &stubAuthService{validateID: 7}
	orderSvc := &stubOrderService{created: false}
	balanceSvc := &stubBalanceService{}
	withdrawSvc := &stubWithdrawService{}
	router := handlers.NewRouter(authSvc, orderSvc, balanceSvc, withdrawSvc)

	req := httptest.NewRequest(http.MethodPost, "/api/user/orders", strings.NewReader("0"))
	req.Header.Set("Content-Type", "text/plain")
	req.AddCookie(&http.Cookie{Name: "token", Value: "valid"})
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
}

func TestCreateOrderConflict(t *testing.T) {
	authSvc := &stubAuthService{validateID: 1}
	orderSvc := &stubOrderService{submitErr: service.ErrOrderOwnedByAnother}
	balanceSvc := &stubBalanceService{}
	withdrawSvc := &stubWithdrawService{}
	router := handlers.NewRouter(authSvc, orderSvc, balanceSvc, withdrawSvc)

	req := httptest.NewRequest(http.MethodPost, "/api/user/orders", strings.NewReader("18"))
	req.Header.Set("Content-Type", "text/plain")
	req.AddCookie(&http.Cookie{Name: "token", Value: "valid"})
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", rr.Code)
	}
}

func TestCreateOrderInvalidNumber(t *testing.T) {
	authSvc := &stubAuthService{validateID: 2}
	orderSvc := &stubOrderService{submitErr: service.ErrInvalidOrderNumber}
	balanceSvc := &stubBalanceService{}
	withdrawSvc := &stubWithdrawService{}
	router := handlers.NewRouter(authSvc, orderSvc, balanceSvc, withdrawSvc)

	req := httptest.NewRequest(http.MethodPost, "/api/user/orders", strings.NewReader("12ab"))
	req.Header.Set("Content-Type", "text/plain")
	req.AddCookie(&http.Cookie{Name: "token", Value: "valid"})
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422, got %d", rr.Code)
	}
	if orderSvc.gotSubmitNumber != "" {
		t.Fatalf("order service should not be called, got %q", orderSvc.gotSubmitNumber)
	}
}

func TestListOrdersUnauthorized(t *testing.T) {
	authSvc := &stubAuthService{}
	orderSvc := &stubOrderService{}
	balanceSvc := &stubBalanceService{}
	withdrawSvc := &stubWithdrawService{}
	router := handlers.NewRouter(authSvc, orderSvc, balanceSvc, withdrawSvc)

	req := httptest.NewRequest(http.MethodGet, "/api/user/orders", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rr.Code)
	}
}

func TestListOrdersEmpty(t *testing.T) {
	authSvc := &stubAuthService{validateID: 5}
	orderSvc := &stubOrderService{listOrders: []repo.Order{}}
	balanceSvc := &stubBalanceService{}
	withdrawSvc := &stubWithdrawService{}
	router := handlers.NewRouter(authSvc, orderSvc, balanceSvc, withdrawSvc)

	req := httptest.NewRequest(http.MethodGet, "/api/user/orders", nil)
	req.AddCookie(&http.Cookie{Name: "token", Value: "valid"})
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rr.Code)
	}
	if orderSvc.gotListUserID != 5 {
		t.Fatalf("expected list to be called with user 5, got %d", orderSvc.gotListUserID)
	}
}

func TestListOrdersSuccess(t *testing.T) {
	now := time.Now().UTC()
	earlier := now.Add(-time.Minute)
	accrual := 12.5

	orders := []repo.Order{
		{Number: "111", Status: "PROCESSED", Accrual: &accrual, UploadedAt: now},
		{Number: "222", Status: "PROCESSING", UploadedAt: earlier},
	}

	authSvc := &stubAuthService{validateID: 9}
	orderSvc := &stubOrderService{listOrders: orders}
	balanceSvc := &stubBalanceService{}
	withdrawSvc := &stubWithdrawService{}
	router := handlers.NewRouter(authSvc, orderSvc, balanceSvc, withdrawSvc)

	req := httptest.NewRequest(http.MethodGet, "/api/user/orders", nil)
	req.AddCookie(&http.Cookie{Name: "token", Value: "valid"})
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	res := rr.Result()
	t.Cleanup(func() { res.Body.Close() })
	if ct := res.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var payload []map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(payload) != 2 {
		t.Fatalf("expected 2 orders, got %d", len(payload))
	}

	if payload[0]["number"] != "111" {
		t.Fatalf("expected first number 111, got %v", payload[0]["number"])
	}
	if payload[0]["status"] != "PROCESSED" {
		t.Fatalf("expected first status PROCESSED, got %v", payload[0]["status"])
	}
	if payload[0]["accrual"].(float64) != accrual {
		t.Fatalf("expected accrual %v, got %v", accrual, payload[0]["accrual"])
	}
	if payload[0]["uploaded_at"] != now.Format(time.RFC3339) {
		t.Fatalf("unexpected uploaded_at: %v", payload[0]["uploaded_at"])
	}

	if payload[1]["number"] != "222" {
		t.Fatalf("expected second number 222, got %v", payload[1]["number"])
	}
	if payload[1]["status"] != "PROCESSING" {
		t.Fatalf("expected second status PROCESSING, got %v", payload[1]["status"])
	}
	if _, ok := payload[1]["accrual"]; ok {
		t.Fatalf("did not expect accrual for non-processed order")
	}
	if payload[1]["uploaded_at"] != earlier.Format(time.RFC3339) {
		t.Fatalf("unexpected second uploaded_at: %v", payload[1]["uploaded_at"])
	}

	if orderSvc.gotListUserID != 9 {
		t.Fatalf("expected list user id 9, got %d", orderSvc.gotListUserID)
	}
}

func TestBalanceUnauthorized(t *testing.T) {
	authSvc := &stubAuthService{}
	orderSvc := &stubOrderService{}
	balanceSvc := &stubBalanceService{}
	withdrawSvc := &stubWithdrawService{}
	router := handlers.NewRouter(authSvc, orderSvc, balanceSvc, withdrawSvc)

	req := httptest.NewRequest(http.MethodGet, "/api/user/balance", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rr.Code)
	}
}

func TestBalanceSuccess(t *testing.T) {
	authSvc := &stubAuthService{validateID: 33}
	orderSvc := &stubOrderService{}
	balanceSvc := &stubBalanceService{current: 123.45, withdrawn: 67.89}
	withdrawSvc := &stubWithdrawService{}
	router := handlers.NewRouter(authSvc, orderSvc, balanceSvc, withdrawSvc)

	req := httptest.NewRequest(http.MethodGet, "/api/user/balance", nil)
	req.AddCookie(&http.Cookie{Name: "token", Value: "valid"})
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	res := rr.Result()
	t.Cleanup(func() { res.Body.Close() })
	if ct := res.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var resp map[string]float64
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp["current"] != 123.45 {
		t.Fatalf("expected current 123.45, got %v", resp["current"])
	}
	if resp["withdrawn"] != 67.89 {
		t.Fatalf("expected withdrawn 67.89, got %v", resp["withdrawn"])
	}
	if balanceSvc.gotUserID != 33 {
		t.Fatalf("expected balance to be requested for user 33, got %d", balanceSvc.gotUserID)
	}
}

func TestWithdrawUnauthorized(t *testing.T) {
	authSvc := &stubAuthService{}
	orderSvc := &stubOrderService{}
	balanceSvc := &stubBalanceService{}
	withdrawSvc := &stubWithdrawService{}
	router := handlers.NewRouter(authSvc, orderSvc, balanceSvc, withdrawSvc)

	req := httptest.NewRequest(http.MethodPost, "/api/user/balance/withdraw", strings.NewReader(`{"order":"79927398713","sum":10}`))
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rr.Code)
	}
}

func TestWithdrawInvalidOrder(t *testing.T) {
	authSvc := &stubAuthService{validateID: 10}
	orderSvc := &stubOrderService{}
	balanceSvc := &stubBalanceService{}
	withdrawSvc := &stubWithdrawService{}
	router := handlers.NewRouter(authSvc, orderSvc, balanceSvc, withdrawSvc)

	req := httptest.NewRequest(http.MethodPost, "/api/user/balance/withdraw", strings.NewReader(`{"order":"12AB","sum":10}`))
	req.AddCookie(&http.Cookie{Name: "token", Value: "valid"})
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422, got %d", rr.Code)
	}
	if orderSvc.gotSubmitNumber != "" {
		t.Fatalf("order service should not be called, got %q", orderSvc.gotSubmitNumber)
	}
}

func TestWithdrawInsufficientFunds(t *testing.T) {
	authSvc := &stubAuthService{validateID: 11}
	orderSvc := &stubOrderService{}
	balanceSvc := &stubBalanceService{}
	withdrawSvc := &stubWithdrawService{err: repo.ErrInsufficientFunds}
	router := handlers.NewRouter(authSvc, orderSvc, balanceSvc, withdrawSvc)

	req := httptest.NewRequest(http.MethodPost, "/api/user/balance/withdraw", strings.NewReader(`{"order":"79927398713","sum":50}`))
	req.AddCookie(&http.Cookie{Name: "token", Value: "valid"})
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusPaymentRequired {
		t.Fatalf("expected status 402, got %d", rr.Code)
	}
	if withdrawSvc.gotUserID != 11 {
		t.Fatalf("expected user id 11, got %d", withdrawSvc.gotUserID)
	}
}

func TestWithdrawSuccess(t *testing.T) {
	authSvc := &stubAuthService{validateID: 12}
	orderSvc := &stubOrderService{}
	balanceSvc := &stubBalanceService{}
	withdrawSvc := &stubWithdrawService{}
	router := handlers.NewRouter(authSvc, orderSvc, balanceSvc, withdrawSvc)

	req := httptest.NewRequest(http.MethodPost, "/api/user/balance/withdraw", strings.NewReader(`{"order":"79927398713","sum":25.5}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "token", Value: "valid"})
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if withdrawSvc.gotUserID != 12 {
		t.Fatalf("expected user id 12, got %d", withdrawSvc.gotUserID)
	}
	if withdrawSvc.gotOrder != "79927398713" {
		t.Fatalf("expected order 79927398713, got %s", withdrawSvc.gotOrder)
	}
	if withdrawSvc.gotSum != 25.5 {
		t.Fatalf("expected sum 25.5, got %v", withdrawSvc.gotSum)
	}
}

func TestWithdrawalsEmpty(t *testing.T) {
	authSvc := &stubAuthService{validateID: 21}
	orderSvc := &stubOrderService{}
	balanceSvc := &stubBalanceService{}
	withdrawSvc := &stubWithdrawService{list: []repo.Withdrawal{}}
	router := handlers.NewRouter(authSvc, orderSvc, balanceSvc, withdrawSvc)

	req := httptest.NewRequest(http.MethodGet, "/api/user/withdrawals", nil)
	req.AddCookie(&http.Cookie{Name: "token", Value: "valid"})
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rr.Code)
	}
	if withdrawSvc.gotListUser != 21 {
		t.Fatalf("expected list called with user 21, got %d", withdrawSvc.gotListUser)
	}
}

func TestWithdrawalsSuccess(t *testing.T) {
	now := time.Now().UTC()
	earlier := now.Add(-time.Hour)
	withdrawals := []repo.Withdrawal{
		{OrderNumber: "A1", Sum: 10, ProcessedAt: now},
		{OrderNumber: "B2", Sum: 5.5, ProcessedAt: earlier},
	}

	authSvc := &stubAuthService{validateID: 22}
	orderSvc := &stubOrderService{}
	balanceSvc := &stubBalanceService{}
	withdrawSvc := &stubWithdrawService{list: withdrawals}
	router := handlers.NewRouter(authSvc, orderSvc, balanceSvc, withdrawSvc)

	req := httptest.NewRequest(http.MethodGet, "/api/user/withdrawals", nil)
	req.AddCookie(&http.Cookie{Name: "token", Value: "valid"})
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	res := rr.Result()
	t.Cleanup(func() { res.Body.Close() })
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var payload []map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(payload) != 2 {
		t.Fatalf("expected 2 withdrawals, got %d", len(payload))
	}

	if payload[0]["order"] != "A1" {
		t.Fatalf("expected first order A1, got %v", payload[0]["order"])
	}
	if payload[0]["sum"].(float64) != 10 {
		t.Fatalf("expected first sum 10, got %v", payload[0]["sum"])
	}
	if payload[0]["processed_at"] != now.Format(time.RFC3339) {
		t.Fatalf("unexpected processed_at for first: %v", payload[0]["processed_at"])
	}

	if payload[1]["order"] != "B2" {
		t.Fatalf("expected second order B2, got %v", payload[1]["order"])
	}
	if payload[1]["sum"].(float64) != 5.5 {
		t.Fatalf("expected second sum 5.5, got %v", payload[1]["sum"])
	}
	if payload[1]["processed_at"] != earlier.Format(time.RFC3339) {
		t.Fatalf("unexpected processed_at for second: %v", payload[1]["processed_at"])
	}

	if withdrawSvc.gotListUser != 22 {
		t.Fatalf("expected list called with user 22, got %d", withdrawSvc.gotListUser)
	}
}

func TestCreateOrderEmptyNumber(t *testing.T) {
	authSvc := &stubAuthService{validateID: 3}
	orderSvc := &stubOrderService{}
	balanceSvc := &stubBalanceService{}
	withdrawSvc := &stubWithdrawService{}
	router := handlers.NewRouter(authSvc, orderSvc, balanceSvc, withdrawSvc)

	req := httptest.NewRequest(http.MethodPost, "/api/user/orders", strings.NewReader("   "))
	req.Header.Set("Content-Type", "text/plain")
	req.AddCookie(&http.Cookie{Name: "token", Value: "valid"})
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422, got %d", rr.Code)
	}
	if orderSvc.gotSubmitNumber != "" {
		t.Fatalf("order service should not be called, got %q", orderSvc.gotSubmitNumber)
	}
}
