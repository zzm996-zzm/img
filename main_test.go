package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	oldDB := appDB
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "test.db"))
	t.Setenv("SESSION_SECRET", "test-secret")
	db, err := openDB()
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	appDB = db
	t.Cleanup(func() {
		db.Close()
		appDB = oldDB
	})
	return db
}

func TestHandlePayPalWebhookStoresEmail(t *testing.T) {
	setupTestDB(t)
	paidMu.Lock()
	paidEmails = map[string]paidRecord{}
	paidMu.Unlock()

	body := `{
		"id": "WH-TEST",
		"event_type": "PAYMENT.CAPTURE.COMPLETED",
		"resource": {
			"id": "CAPTURE-123",
			"payer": {
				"email_address": "Buyer@Example.COM"
			}
		}
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/paypal-webhook", strings.NewReader(body))
	req.Header.Set("Paypal-Transmission-Id", "test-transmission")
	rr := httptest.NewRecorder()

	handlePayPalWebhook(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	paidMu.RLock()
	record, ok := paidEmails["buyer@example.com"]
	paidMu.RUnlock()

	if !ok {
		t.Fatalf("expected normalized payer email to be stored")
	}
	if record.EventType != "PAYMENT.CAPTURE.COMPLETED" {
		t.Fatalf("unexpected event type: %s", record.EventType)
	}
	if record.ResourceID != "CAPTURE-123" {
		t.Fatalf("unexpected resource id: %s", record.ResourceID)
	}
	if !isPaidEmail("buyer@example.com") {
		t.Fatalf("expected completed capture to mark email paid")
	}
}

func TestHandlePayPalWebhookGetHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/paypal-webhook", nil)
	rr := httptest.NewRecorder()

	handlePayPalWebhook(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "paypal webhook endpoint ready") {
		t.Fatalf("unexpected body: %s", rr.Body.String())
	}
}

func TestRegisterLoginMeFlow(t *testing.T) {
	setupTestDB(t)

	registerReq := httptest.NewRequest(http.MethodPost, "/api/register", strings.NewReader(`{"email":"User@Example.com","password":"strong-pass"}`))
	registerRR := httptest.NewRecorder()
	handleRegister(registerRR, registerReq)
	if registerRR.Code != http.StatusOK {
		t.Fatalf("register status %d body=%s", registerRR.Code, registerRR.Body.String())
	}
	if len(registerRR.Result().Cookies()) == 0 {
		t.Fatalf("expected session cookie after register")
	}

	loginReq := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"email":"user@example.com","password":"strong-pass"}`))
	loginRR := httptest.NewRecorder()
	handleLogin(loginRR, loginReq)
	if loginRR.Code != http.StatusOK {
		t.Fatalf("login status %d body=%s", loginRR.Code, loginRR.Body.String())
	}
	cookies := loginRR.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("expected session cookie after login")
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	meReq.AddCookie(cookies[0])
	meRR := httptest.NewRecorder()
	handleMe(meRR, meReq)
	if meRR.Code != http.StatusOK {
		t.Fatalf("me status %d body=%s", meRR.Code, meRR.Body.String())
	}
	var data map[string]any
	if err := json.Unmarshal(meRR.Body.Bytes(), &data); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if data["authenticated"] != true || data["email"] != "user@example.com" {
		t.Fatalf("unexpected me response: %#v", data)
	}
}

func TestRefundRevokesPaidLicense(t *testing.T) {
	setupTestDB(t)
	setLicense("buyer@example.com", "PAYMENT.CAPTURE.COMPLETED", "CAPTURE-123", true)
	if !isPaidEmail("buyer@example.com") {
		t.Fatalf("expected setup license to be paid")
	}

	body := `{
		"event_type": "PAYMENT.CAPTURE.REFUNDED",
		"resource": {
			"id": "CAPTURE-123",
			"seller_receivable_breakdown": {
				"paypal_fee": { "value": "1.00" }
			},
			"payer": { "email_address": "buyer@example.com" }
		}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/paypal-webhook", strings.NewReader(body))
	rr := httptest.NewRecorder()
	handlePayPalWebhook(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("refund status %d body=%s", rr.Code, rr.Body.String())
	}
	if isPaidEmail("buyer@example.com") {
		t.Fatalf("expected refunded capture to revoke paid license")
	}
}
