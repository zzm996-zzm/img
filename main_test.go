package main

import (
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

type testSitemapURLSet struct {
	URLs []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
}

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

func TestHandleSitemap(t *testing.T) {
	t.Setenv("SITE_URL", "https://onlinebox.site/")
	req := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	rr := httptest.NewRecorder()

	handleSitemap(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "application/xml") {
		t.Fatalf("expected xml content type, got %q", got)
	}
	var parsed testSitemapURLSet
	if err := xml.Unmarshal(rr.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("decode sitemap: %v body=%s", err, rr.Body.String())
	}
	if len(parsed.URLs) != len(publicPages) {
		t.Fatalf("expected %d sitemap urls, got %d: %#v", len(publicPages), len(parsed.URLs), parsed.URLs)
	}
	if parsed.URLs[0].Loc != "https://onlinebox.site/" {
		t.Fatalf("unexpected sitemap urls: %#v", parsed.URLs)
	}
	var foundCompressor bool
	for _, item := range parsed.URLs {
		if item.Loc == "https://onlinebox.site/image-compressor" {
			foundCompressor = true
		}
	}
	if !foundCompressor {
		t.Fatalf("expected image compressor URL in sitemap: %#v", parsed.URLs)
	}
}

func TestHandleRobotsIncludesSitemap(t *testing.T) {
	t.Setenv("SITE_URL", "https://onlinebox.site/")
	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	rr := httptest.NewRecorder()

	handleRobots(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if body := rr.Body.String(); !strings.Contains(body, "Allow: /") || !strings.Contains(body, "Sitemap: https://onlinebox.site/sitemap.xml") {
		t.Fatalf("unexpected robots body: %s", body)
	}
}

func TestHandleFaviconNoContent(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
	rr := httptest.NewRecorder()

	handleFavicon(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleIndexNotFoundForUnknownPath(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/missing-page", nil)
	rr := httptest.NewRecorder()

	handleIndex(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleIndexRendersRouteSpecificSEO(t *testing.T) {
	t.Setenv("SITE_URL", "https://onlinebox.site/")
	req := httptest.NewRequest(http.MethodGet, "/image-converter", nil)
	rr := httptest.NewRecorder()

	handleIndex(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, expected := range []string{
		"<title>Image Converter | Convert JPG PNG WebP Online</title>",
		"<h1>Image converter<em>convert JPG, PNG and WebP online</em></h1>",
		`<link rel="canonical" href="https://onlinebox.site/image-converter">`,
		`function convertImage()`,
		"How to use the image converter",
		"When is the image converter useful?",
		"FAQ",
		`href="/image-compressor"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected body to contain %q", expected)
		}
	}
	for _, unexpected := range []string{
		"CSV input",
		"Markdown input",
		"Creator Tools",
		"如何使用",
	} {
		if strings.Contains(body, unexpected) {
			t.Fatalf("expected focused page to omit %q", unexpected)
		}
	}
}

func TestHandleIndexRendersUtilityLandingPage(t *testing.T) {
	t.Setenv("SITE_URL", "https://onlinebox.site/")
	req := httptest.NewRequest(http.MethodGet, "/csv-to-json", nil)
	rr := httptest.NewRecorder()

	handleIndex(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, expected := range []string{
		"<title>CSV to JSON Converter | Free Online CSV JSON Tool</title>",
		"<h1>CSV to JSON converter<em>convert CSV text into JSON</em></h1>",
		`<link rel="canonical" href="https://onlinebox.site/csv-to-json">`,
		`function convertCSV()`,
		"How to use the CSV to JSON converter",
		"When should you convert CSV to JSON?",
		"CSV to JSON FAQ",
		`href="/image-compressor"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected body to contain %q", expected)
		}
	}
	for _, unexpected := range []string{
		`id="qrText"`,
		`id="markdownInput"`,
		"Live Browser Tools",
		"本页面只围绕",
		"如何使用",
	} {
		if strings.Contains(body, unexpected) {
			t.Fatalf("expected focused page to omit %q", unexpected)
		}
	}
}

func TestHandleIndexRendersEnglishDirectoryHome(t *testing.T) {
	t.Setenv("SITE_URL", "https://onlinebox.site/")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	handleIndex(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, expected := range []string{
		`<html lang="en">`,
		"<title>OnlineBox | Free Browser Tools for Images, Data and Creators</title>",
		"Free browser tools",
		"Image tools",
		"Data and creator tools",
		`href="/csv-to-json"`,
		`href="/image-compressor"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected home body to contain %q", expected)
		}
	}
	for _, unexpected := range []string{
		`id="csvInput"`,
		`id="qrText"`,
		"免费在线工具集",
	} {
		if strings.Contains(body, unexpected) {
			t.Fatalf("expected directory home to omit %q", unexpected)
		}
	}
}
