package main

import (
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"os"
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

func setupBlogContent(t *testing.T, files map[string]string) {
	t.Helper()
	oldDir := blogContentDir
	dir := filepath.Join(t.TempDir(), "content", "blog")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("create blog content dir: %v", err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatalf("write blog post %s: %v", name, err)
		}
	}
	blogContentDir = dir
	t.Cleanup(func() {
		blogContentDir = oldDir
	})
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
	setupBlogContent(t, map[string]string{
		"older.md": `---
title: Older Post
date: 2026-05-01
description: Older description
---

Older content.`,
		"day-one.md": `---
title: Day One
date: 2026-05-10
description: First blog entry
---

Hello.`,
	})
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
	if len(parsed.URLs) != len(publicPages)+3 {
		t.Fatalf("expected %d sitemap urls, got %d: %#v", len(publicPages)+3, len(parsed.URLs), parsed.URLs)
	}
	if parsed.URLs[0].Loc != "https://onlinebox.site/" {
		t.Fatalf("unexpected sitemap urls: %#v", parsed.URLs)
	}
	var foundCompressor bool
	var foundPrivacy bool
	var foundM3U8 bool
	var foundJSON bool
	var foundBlog bool
	var foundBlogPost bool
	for _, item := range parsed.URLs {
		if item.Loc == "https://onlinebox.site/image-compressor" {
			foundCompressor = true
		}
		if item.Loc == "https://onlinebox.site/privacy-policy" {
			foundPrivacy = true
		}
		if item.Loc == "https://onlinebox.site/m3u8-player" {
			foundM3U8 = true
		}
		if item.Loc == "https://onlinebox.site/json-formatter" {
			foundJSON = true
		}
		if item.Loc == "https://onlinebox.site/blog" {
			foundBlog = true
		}
		if item.Loc == "https://onlinebox.site/blog/day-one" {
			foundBlogPost = true
		}
	}
	if !foundCompressor {
		t.Fatalf("expected image compressor URL in sitemap: %#v", parsed.URLs)
	}
	if !foundPrivacy {
		t.Fatalf("expected privacy policy URL in sitemap: %#v", parsed.URLs)
	}
	if !foundM3U8 {
		t.Fatalf("expected M3U8 player URL in sitemap: %#v", parsed.URLs)
	}
	if !foundJSON {
		t.Fatalf("expected JSON formatter URL in sitemap: %#v", parsed.URLs)
	}
	if !foundBlog {
		t.Fatalf("expected blog URL in sitemap: %#v", parsed.URLs)
	}
	if !foundBlogPost {
		t.Fatalf("expected blog post URL in sitemap: %#v", parsed.URLs)
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

func TestHandleAdsTXT(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ads.txt", nil)
	rr := httptest.NewRecorder()

	handleAdsTXT(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "text/plain") {
		t.Fatalf("expected text/plain content type, got %q", got)
	}
	if body := rr.Body.String(); body != "google.com, pub-1902780696242483, DIRECT, f08c47fec0942fa0\n" {
		t.Fatalf("unexpected ads.txt body: %q", body)
	}
}

func TestHandleFaviconRendersSiteIcon(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
	rr := httptest.NewRecorder()

	handleFavicon(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "image/svg+xml") {
		t.Fatalf("expected svg content type, got %q", got)
	}
	body := rr.Body.String()
	for _, expected := range []string{`<svg xmlns="http://www.w3.org/2000/svg"`, "#d4ff57", "#101113"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected favicon body to contain %q", expected)
		}
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
		"<title>HEIC to JPG Converter | Convert HEIC JPG PNG WebP Online</title>",
		"<h1>Image converter<em>convert HEIC to JPG online</em></h1>",
		`<link rel="canonical" href="https://onlinebox.site/image-converter">`,
		`heic2any.min.js`,
		`accept="image/jpeg,image/png,image/webp,image/heic,image/heif,.heic,.heif"`,
		"Supports HEIC, HEIF, JPG, PNG and WebP.",
		`function isHEICFile(file)`,
		`function convertImage()`,
		"How to use the HEIC to JPG image converter",
		"When is the HEIC to JPG image converter useful?",
		"iPhone HEIC photos into JPG",
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
		`https://www.googletagmanager.com/gtag/js?id=G-GRDT3349BV`,
		`gtag('config', 'G-GRDT3349BV')`,
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

func TestHandleIndexRendersJSONFormatter(t *testing.T) {
	t.Setenv("SITE_URL", "https://onlinebox.site/")
	req := httptest.NewRequest(http.MethodGet, "/json-formatter", nil)
	rr := httptest.NewRecorder()

	handleIndex(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, expected := range []string{
		"<title>JSON Formatter &amp; Validator | Free Online JSON Beautifier - OnlineBox</title>",
		`<meta name="description" content="Format, validate and minify JSON instantly in your browser. Free online JSON beautifier with error detection — no uploads, no server.">`,
		`<link rel="canonical" href="https://onlinebox.site/json-formatter">`,
		"<h1>JSON Formatter &amp; Validator<em>Free Online JSON Beautifier</em></h1>",
		"Paste your JSON to format, validate and minify instantly. Runs in your browser — nothing is sent to a server.",
		`id="jsonInput"`,
		`id="jsonFormattedOutput"`,
		`onclick="formatJSON()"`,
		`onclick="minifyJSON()"`,
		`onclick="copyJSONOutput()"`,
		`onclick="clearJSONTool()"`,
		`function jsonPositionFromMessage`,
		`function validateJSONLive`,
		"How to use the JSON Formatter & Validator",
		"Best use cases for JSON formatting",
		"JSON Formatter FAQ",
		"Does my JSON upload to a server?",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected body to contain %q", expected)
		}
	}
	for _, unexpected := range []string{
		`id="csvInput"`,
		`id="markdownInput"`,
		`id="m3u8Url"`,
	} {
		if strings.Contains(body, unexpected) {
			t.Fatalf("expected JSON formatter page to omit %q", unexpected)
		}
	}
}

func TestHandleIndexRendersFreeBatchCompressor(t *testing.T) {
	t.Setenv("SITE_URL", "https://onlinebox.site/")
	req := httptest.NewRequest(http.MethodGet, "/batch-image-compressor", nil)
	rr := httptest.NewRecorder()

	handleIndex(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, expected := range []string{
		"<title>Batch Image Compressor | Compress Multiple Images Online</title>",
		"<h1>Batch image compressor<em>compress multiple images locally</em></h1>",
		`multiple onchange="loadBatchFiles(this.files)"`,
		"Choose multiple images",
		`id="batchList"`,
		`function setupBatchDrop()`,
		`function compressBatchImages()`,
		"How to use the batch image compressor",
		"Compress batch",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected body to contain %q", expected)
		}
	}
	for _, unexpected := range []string{
		"PRO TOOL",
		"planned for Pro",
		"unlock batch",
		"Use the free single-image compressor first",
	} {
		if strings.Contains(body, unexpected) {
			t.Fatalf("expected batch page to omit %q", unexpected)
		}
	}
}

func TestHandleIndexRendersM3U8Player(t *testing.T) {
	t.Setenv("SITE_URL", "https://onlinebox.site/")
	req := httptest.NewRequest(http.MethodGet, "/m3u8-player", nil)
	rr := httptest.NewRecorder()

	handleIndex(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, expected := range []string{
		"<title>M3U8 Player | Free Online HLS Stream Player - OnlineBox</title>",
		`<meta name="description" content="Play any M3U8 or HLS stream directly in your browser for free. No software, no uploads — just paste the URL and watch.">`,
		`<link rel="canonical" href="https://onlinebox.site/m3u8-player">`,
		`https://cdn.jsdelivr.net/npm/hls.js@latest`,
		`id="m3u8Url"`,
		`id="m3u8Video" playsinline`,
		`class="m3u8-player"`,
		`function loadM3U8()`,
		`Hls.isSupported()`,
		`canPlayType('application/vnd.apple.mpegurl')`,
		`setTimeout(()=>m3u8Player.classList.remove('controls-visible'),2000)`,
		"How to use the M3U8 Player",
		"Best use cases for M3U8 playback",
		"M3U8 FAQ",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected body to contain %q", expected)
		}
	}
	for _, unexpected := range []string{
		`controls></video>`,
		`id="csvInput"`,
		`id="imageInput"`,
	} {
		if strings.Contains(body, unexpected) {
			t.Fatalf("expected M3U8 page to omit %q", unexpected)
		}
	}
}

func TestHandleIndexRendersEXIFViewer(t *testing.T) {
	t.Setenv("SITE_URL", "https://onlinebox.site/")
	req := httptest.NewRequest(http.MethodGet, "/exif-viewer", nil)
	rr := httptest.NewRecorder()

	handleIndex(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, expected := range []string{
		"<title>EXIF Viewer and Remover | Check and Remove Photo Metadata</title>",
		"<h1>EXIF viewer<em>view and remove photo metadata</em></h1>",
		`<link rel="canonical" href="https://onlinebox.site/exif-viewer">`,
		`exifr/dist/lite.umd.js`,
		`id="exifOutput"`,
		`function loadEXIFMetadata()`,
		`function downloadWithoutMetadata()`,
		"GPS location data",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected EXIF page to contain %q", expected)
		}
	}
}

func TestHandleIndexRendersImageWatermarkTool(t *testing.T) {
	t.Setenv("SITE_URL", "https://onlinebox.site/")
	req := httptest.NewRequest(http.MethodGet, "/image-watermark", nil)
	rr := httptest.NewRecorder()

	handleIndex(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, expected := range []string{
		"<title>Add Watermark to Image | Free Online Watermark Tool</title>",
		"<h1>Image watermark<em>add text watermark online</em></h1>",
		`<link rel="canonical" href="https://onlinebox.site/image-watermark">`,
		`id="watermarkText"`,
		`id="watermarkOpacity"`,
		`function renderWatermarkPreview()`,
		`function downloadWatermarkedImage()`,
		"Download watermarked JPG",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected watermark page to contain %q", expected)
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
		`<link rel="icon" href="/favicon.svg" type="image/svg+xml">`,
		`https://www.googletagmanager.com/gtag/js?id=G-GRDT3349BV`,
		"Free browser tools",
		"Image tools",
		"Data and creator tools",
		`href="/csv-to-json"`,
		`href="/json-formatter"`,
		`href="/image-compressor"`,
		`href="/m3u8-player"`,
		`href="/blog"`,
		`href="/privacy-policy"`,
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

func TestHandleBlogIndexRendersMarkdownPostsByDate(t *testing.T) {
	t.Setenv("SITE_URL", "https://onlinebox.site/")
	setupBlogContent(t, map[string]string{
		"older.md": `---
title: Older Post
date: 2026-05-01
description: Older description
---

Older content.`,
		"day-one.md": `---
title: Day One
date: 2026-05-10
description: First blog entry
---

Hello.`,
	})
	req := httptest.NewRequest(http.MethodGet, "/blog", nil)
	rr := httptest.NewRecorder()

	handleBlogIndex(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, expected := range []string{
		"<title>Blog | OnlineBox</title>",
		`<link rel="canonical" href="https://onlinebox.site/blog">`,
		"<h1>Blog</h1>",
		`href="/blog/day-one"`,
		"First blog entry",
		"Older description",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected blog index to contain %q", expected)
		}
	}
	if strings.Index(body, "Day One") > strings.Index(body, "Older Post") {
		t.Fatalf("expected newest post first: %s", body)
	}
}

func TestHandleBlogPostRendersMarkdownAndSEO(t *testing.T) {
	t.Setenv("SITE_URL", "https://onlinebox.site/")
	setupBlogContent(t, map[string]string{
		"day-one.md": `---
title: Day One
date: 2026-05-10
description: First blog entry
---

## Start

This is **bold** and [linked](/json-formatter).

- Local rendering
- Dark theme

` + "```go" + `
fmt.Println("safe")
` + "```" + `
`,
	})
	req := httptest.NewRequest(http.MethodGet, "/blog/day-one", nil)
	rr := httptest.NewRecorder()

	handleBlogPost(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, expected := range []string{
		"<title>Day One | OnlineBox Blog</title>",
		`<meta name="description" content="First blog entry">`,
		`<link rel="canonical" href="https://onlinebox.site/blog/day-one">`,
		"<h1>Day One</h1>",
		`<div class="date">2026-05-10</div>`,
		"<h2>Start</h2>",
		"This is <strong>bold</strong> and <a href=\"/json-formatter\">linked</a>.",
		"<li>Local rendering</li>",
		`<pre><code class="language-go">fmt.Println(&#34;safe&#34;)</code></pre>`,
		`More free browser tools &rarr; <a href="/">onlinebox.site</a>`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected blog post to contain %q", expected)
		}
	}
}

func TestHandleIndexRendersPrivacyPolicy(t *testing.T) {
	t.Setenv("SITE_URL", "https://onlinebox.site/")
	req := httptest.NewRequest(http.MethodGet, "/privacy-policy", nil)
	rr := httptest.NewRecorder()

	handleIndex(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, expected := range []string{
		"<title>Privacy Policy | OnlineBox</title>",
		`<link rel="canonical" href="https://onlinebox.site/privacy-policy">`,
		"Google Analytics",
		"Advertising and Cookies",
		"https://adssettings.google.com/",
		"https://policies.google.com/technologies/partner-sites",
		`https://www.googletagmanager.com/gtag/js?id=G-GRDT3349BV`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected privacy policy to contain %q", expected)
		}
	}
	for _, unexpected := range []string{
		`id="csvInput"`,
		`id="imageInput"`,
		`__`,
	} {
		if strings.Contains(body, unexpected) {
			t.Fatalf("expected privacy policy to omit %q", unexpected)
		}
	}
}

func TestHandleIndexRendersImprovedMarkdownParser(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/markdown-to-pdf", nil)
	rr := httptest.NewRecorder()

	handleIndex(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, expected := range []string{
		"function splitTableRow",
		"function renderTable",
		"function addListItem",
		`github-markdown.min.css`,
		`class="output preview markdown-body"`,
		`body class="markdown-body"`,
		`break-inside:avoid`,
		`@page{margin:16mm 14mm}`,
		`color-scheme:light`,
		`--color-canvas-default:#fff`,
		`.preview.markdown-body td{background:#fff;color:#1f2328}`,
		"line.startsWith('### ')",
		`line.startsWith('\x60\x60\x60')`,
		"addListItem(state,'ol'",
		"<blockquote>",
		`<div class="table-wrap"><table>`,
		".preview table",
		".preview pre",
		"Markdown PDF",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected markdown page to contain %q", expected)
		}
	}
}
