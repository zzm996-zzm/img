package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlePayPalWebhookStoresEmail(t *testing.T) {
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
