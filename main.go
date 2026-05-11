package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type paidRecord struct {
	Email      string    `json:"email"`
	EventType  string    `json:"event_type"`
	ResourceID string    `json:"resource_id,omitempty"`
	ReceivedAt time.Time `json:"received_at"`
}

var (
	paidMu     sync.RWMutex
	paidEmails = map[string]paidRecord{}
	appDB      *sql.DB
	authMu     sync.Mutex
	authHits   = map[string][]time.Time{}
	secretOnce sync.Once
	secretKey  string
)

func init() {
	for _, page := range publicPages {
		publicPageByPath[page.Path] = page
	}
}

const (
	sessionCookieName = "imgtools_session"
	passwordIter      = 120000
	maxJSONBody       = 32 << 10
	authWindow        = 10 * time.Minute
	authMaxHits       = 30
)

type publicPage struct {
	Path        string
	Title       string
	Description string
	Heading     string
	Accent      string
	Intro       string
	Kind        string
	PageTool    string
	PageUtility string
}

var publicPages = []publicPage{
	{
		Path:        "/",
		Title:       "OnlineBox | Free Browser Tools for Images, Data and Creators",
		Description: "Free online browser tools for image compression, image conversion, resizing, QR codes, CSV to JSON, Markdown to PDF and creator utilities. Most tools run locally in your browser.",
		Heading:     "Free browser tools",
		Accent:      "for images, data and creators",
		Intro:       "A focused collection of lightweight tools that run in your browser. Compress images, convert files, generate QR codes, format data and create shareable assets without installing anything.",
		Kind:        "home",
		PageTool:    "compress",
	},
	{
		Path:        "/image-compressor",
		Title:       "Image Compressor to 200KB | Free Online JPG PNG WebP Tool",
		Description: "Compress JPG, PNG and WebP images to a target size such as 200KB, 500KB or 1MB. The image is processed locally in your browser.",
		Heading:     "Image compressor",
		Accent:      "compress images to a target KB size",
		Intro:       "Upload an image, choose a target file size and download a smaller JPG. Useful for forms, applications, ecommerce uploads and social media images.",
		Kind:        "image",
		PageTool:    "compress",
	},
	{
		Path:        "/image-converter",
		Title:       "HEIC to JPG Converter | Convert HEIC JPG PNG WebP Online",
		Description: "Convert HEIC, HEIF, JPG, PNG and WebP images online. HEIC to JPG conversion runs locally in your browser with no file uploads.",
		Heading:     "Image converter",
		Accent:      "convert HEIC to JPG online",
		Intro:       "Choose an iPhone HEIC photo or a JPG, PNG or WebP image and convert it in your browser. Useful for uploads, publishing and quick format fixes.",
		Kind:        "image",
		PageTool:    "convert",
	},
	{
		Path:        "/image-resizer",
		Title:       "Image Resizer | Resize Images Online by Width and Height",
		Description: "Resize images online with custom width and height. Choose contain, cover or stretch mode for avatars, product images and platform uploads.",
		Heading:     "Image resizer",
		Accent:      "resize images by width and height",
		Intro:       "Enter the target width and height, choose how the image should fit, and download a resized JPG for avatars, product listings and upload requirements.",
		Kind:        "image",
		PageTool:    "resize",
	},
	{
		Path:        "/batch-image-compressor",
		Title:       "Batch Image Compressor | Compress Multiple Images Online",
		Description: "Compress multiple JPG, PNG and WebP images online with one target size. Batch image compression runs locally in your browser.",
		Heading:     "Batch image compressor",
		Accent:      "compress multiple images locally",
		Intro:       "Select multiple images, choose a target file size and download compressed JPG files one by one. Useful for product photos, documents and social media batches.",
		Kind:        "image",
		PageTool:    "batch",
	},
	{
		Path:        "/exif-viewer",
		Title:       "EXIF Viewer and Remover | Check and Remove Photo Metadata",
		Description: "View and remove photo EXIF metadata online. Check camera, date and GPS data, then export a clean JPG locally in your browser.",
		Heading:     "EXIF viewer",
		Accent:      "view and remove photo metadata",
		Intro:       "Upload a photo to inspect EXIF metadata such as camera model, date, lens and GPS fields, then download a clean JPG with metadata removed.",
		Kind:        "image",
		PageTool:    "exif",
	},
	{
		Path:        "/image-watermark",
		Title:       "Add Watermark to Image | Free Online Watermark Tool",
		Description: "Add a text watermark to an image online. Choose position, size and opacity, then download a watermarked JPG locally in your browser.",
		Heading:     "Image watermark",
		Accent:      "add text watermark online",
		Intro:       "Upload an image, add your brand or handle as a text watermark, adjust placement and opacity, then download the result.",
		Kind:        "image",
		PageTool:    "watermark",
	},
	{
		Path:        "/qr-code-generator",
		Title:       "QR Code Generator | Free Online QR Code PNG",
		Description: "Generate a QR code online from a link or text, customize colors and download a PNG image for campaigns, menus, profiles and print materials.",
		Heading:     "QR code generator",
		Accent:      "create a free PNG QR code",
		Intro:       "Enter a URL or text, choose foreground and background colors, then download a PNG QR code for print, campaigns, menus and social profiles.",
		Kind:        "utility",
		PageTool:    "compress",
		PageUtility: "qr",
	},
	{
		Path:        "/social-card-maker",
		Title:       "Social Card Maker | Create Social Sharing Images Online",
		Description: "Create a simple social sharing card online. Add a title, subtitle and accent color, then download a 1200x630 image.",
		Heading:     "Social card maker",
		Accent:      "create share images online",
		Intro:       "Create a clean 1200x630 social sharing image from a title, subtitle and accent color. Useful for blog posts, product updates and launch notes.",
		Kind:        "utility",
		PageTool:    "compress",
		PageUtility: "social",
	},
	{
		Path:        "/gradient-generator",
		Title:       "Gradient Generator | Generate CSS Gradients Online",
		Description: "Generate random CSS gradients and color combinations for web backgrounds, social assets, landing pages and quick design exploration.",
		Heading:     "Gradient generator",
		Accent:      "generate CSS gradients online",
		Intro:       "Generate a CSS gradient, preview the result and copy the background code for websites, cards, banners and design experiments.",
		Kind:        "utility",
		PageTool:    "compress",
		PageUtility: "gradient",
	},
	{
		Path:        "/folder-to-zip",
		Title:       "Folder to ZIP | Compress Folders Online Free - OnlineBox",
		Description: "Compress a folder into a ZIP file in your browser. No upload, no server — create ZIP files online for free.",
		Heading:     "Folder to ZIP",
		Accent:      "Compress Folders Online in Your Browser",
		Intro:       "Create ZIP files from folders or multiple files directly in your browser. Choose a compression level, keep folder structure and download the ZIP without uploading files.",
		Kind:        "utility",
		PageTool:    "compress",
		PageUtility: "folder-zip",
	},
	{
		Path:        "/csv-to-json",
		Title:       "CSV to JSON Converter | Free Online CSV JSON Tool",
		Description: "Convert CSV text to JSON online. Paste CSV with headers and get formatted JSON for API testing, imports, data cleanup and mock data.",
		Heading:     "CSV to JSON converter",
		Accent:      "convert CSV text into JSON",
		Intro:       "Paste CSV with a header row and convert it into formatted JSON. Useful for API testing, import preparation, data cleanup and front-end mock data.",
		Kind:        "utility",
		PageTool:    "compress",
		PageUtility: "csv",
	},
	{
		Path:        "/json-formatter",
		Title:       "JSON Formatter & Validator | Free Online JSON Beautifier - OnlineBox",
		Description: "Format, validate and minify JSON instantly in your browser. Free online JSON beautifier with error detection — no uploads, no server.",
		Heading:     "JSON Formatter & Validator",
		Accent:      "Free Online JSON Beautifier",
		Intro:       "Paste your JSON to format, validate and minify instantly. Runs in your browser — nothing is sent to a server.",
		Kind:        "utility",
		PageTool:    "compress",
		PageUtility: "json",
	},
	{
		Path:        "/markdown-to-pdf",
		Title:       "Markdown to PDF | Free Markdown Preview and PDF Export",
		Description: "Convert Markdown to a printable preview and export it as PDF from your browser. Useful for notes, drafts and lightweight documentation.",
		Heading:     "Markdown to PDF",
		Accent:      "preview Markdown and print PDF",
		Intro:       "Paste Markdown, preview the formatted result and use your browser print dialog to save it as PDF for notes, drafts and lightweight documents.",
		Kind:        "utility",
		PageTool:    "compress",
		PageUtility: "markdown",
	},
	{
		Path:        "/pdf-unlocker",
		Title:       "PDF Unlocker | Remove PDF Restrictions Online Free - OnlineBox",
		Description: "Remove copy, print and edit restrictions from PDF files free in your browser. No upload, no server — your PDF stays on your device.",
		Heading:     "PDF Unlocker",
		Accent:      "Remove PDF Restrictions in Your Browser",
		Intro:       "Remove copy, print and edit restrictions from PDF files instantly. Everything runs locally — your file never leaves your device.",
		Kind:        "utility",
		PageTool:    "compress",
		PageUtility: "pdf-unlocker",
	},
	{
		Path:        "/m3u8-player",
		Title:       "M3U8 Player | Free Online HLS Stream Player - OnlineBox",
		Description: "Play any M3U8 or HLS stream directly in your browser for free. No software, no uploads — just paste the URL and watch.",
		Heading:     "M3U8 Player",
		Accent:      "Free Online HLS Stream Player",
		Intro:       "Paste any M3U8 URL and play it directly in your browser. No software needed, no uploads.",
		Kind:        "utility",
		PageTool:    "compress",
		PageUtility: "m3u8",
	},
	{
		Path:        "/privacy-policy",
		Title:       "Privacy Policy | OnlineBox",
		Description: "Privacy Policy for OnlineBox browser tools, including local processing, analytics, cookies and advertising disclosures.",
		Heading:     "Privacy Policy",
		Accent:      "how OnlineBox handles data",
		Intro:       "This policy explains what information OnlineBox collects, how browser-based tools process files, and how analytics and advertising services may use cookies.",
		Kind:        "legal",
	},
	{
		Path:        "/about",
		Title:       "About OnlineBox | Free Browser Tools",
		Description: "Learn about OnlineBox, a collection of free browser-based tools for images, data formatting, QR codes, Markdown, video streams and creator workflows.",
		Heading:     "About OnlineBox",
		Accent:      "free browser tools with local processing",
		Intro:       "OnlineBox is built around small, focused tools that help people finish everyday image, data and creator tasks directly in the browser.",
		Kind:        "trust",
	},
	{
		Path:        "/contact",
		Title:       "Contact OnlineBox | Feedback and Tool Requests",
		Description: "Contact OnlineBox for feedback, bug reports, tool requests, privacy questions and browser tool support.",
		Heading:     "Contact OnlineBox",
		Accent:      "feedback, bugs and tool requests",
		Intro:       "Use this page to find the best way to send feedback, report a broken tool, request a new utility or ask a privacy-related question.",
		Kind:        "trust",
	},
	{
		Path:        "/terms",
		Title:       "Terms of Use | OnlineBox",
		Description: "Terms of Use for OnlineBox free browser tools, including acceptable use, local processing, third-party services and service availability.",
		Heading:     "Terms of Use",
		Accent:      "rules for using OnlineBox",
		Intro:       "These terms explain the basic rules for using OnlineBox browser tools and the limits of the service.",
		Kind:        "trust",
	},
}

var publicPageByPath = map[string]publicPage{}

type blogPost struct {
	Slug        string
	Title       string
	Date        string
	Description string
	ContentHTML string
	dateValue   time.Time
}

var (
	blogContentDir      = filepath.Join("content", "blog")
	inlineCodePattern   = regexp.MustCompile("`([^`]+)`")
	inlineStrongPattern = regexp.MustCompile(`\*\*(.+?)\*\*`)
	inlineEmPattern     = regexp.MustCompile(`\*(.+?)\*`)
	inlineLinkPattern   = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
)

func main() {
	db, err := openDB()
	if err != nil {
		log.Fatalf("database init failed: %v", err)
	}
	appDB = db
	defer appDB.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/blog", handleBlogIndex)
	mux.HandleFunc("/blog/", handleBlogPost)
	mux.HandleFunc("/api/register", handleRegister)
	mux.HandleFunc("/api/login", handleLogin)
	mux.HandleFunc("/api/logout", handleLogout)
	mux.HandleFunc("/api/me", handleMe)
	mux.HandleFunc("/api/paypal-webhook", handlePayPalWebhook)
	mux.HandleFunc("/ads.txt", handleAdsTXT)
	mux.HandleFunc("/robots.txt", handleRobots)
	mux.HandleFunc("/sitemap.xml", handleSitemap)
	mux.HandleFunc("/favicon.ico", handleFavicon)
	mux.HandleFunc("/favicon.svg", handleFavicon)
	mux.HandleFunc("/google6409d0c57bc30ecb.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("google-site-verification: google6409d0c57bc30ecb.html"))
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	for _, fallbackPort := range []string{"8080", "8081"} {
		if fallbackPort == port {
			continue
		}
		go listenAndServe(mux, fallbackPort, false)
	}

	listenAndServe(mux, port, true)
}

func listenAndServe(handler http.Handler, port string, fatal bool) {
	log.Printf("Server listening on 0.0.0.0:%s", port)
	err := http.ListenAndServe(":"+port, handler)
	if fatal {
		log.Fatal(err)
	}
	log.Printf("Could not listen on fallback port %s: %v", port, err)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	page, ok := publicPageByPath[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(renderIndexHTML(page)))
}

func renderIndexHTML(page publicPage) string {
	if page.Kind == "legal" {
		return renderPrivacyHTML(page)
	}
	if page.Kind == "trust" {
		return renderTrustHTML(page)
	}
	if page.Path != "/" {
		return renderLandingHTML(page)
	}
	return renderHomeHTML(page)
}

func renderHomeHTML(page publicPage) string {
	return strings.NewReplacer(
		"__PAGE_TITLE__", html.EscapeString(page.Title),
		"__PAGE_DESCRIPTION__", html.EscapeString(page.Description),
		"__CANONICAL_URL__", html.EscapeString(siteURL()+"/"),
		"__PAGE_HEADING__", html.EscapeString(page.Heading),
		"__PAGE_ACCENT__", html.EscapeString(page.Accent),
		"__PAGE_INTRO__", html.EscapeString(page.Intro),
		"__SEO_META__", seoMetaTags(page.Title, page.Description, siteURL()+"/", "website"),
		"__JSON_LD__", schemaScript(homeSchema()),
		"__IMAGE_TOOL_LINKS__", homeToolCardsHTML("image"),
		"__UTILITY_TOOL_LINKS__", homeToolCardsHTML("utility"),
		"__GOOGLE_ANALYTICS__", googleAnalyticsTag,
	).Replace(homeHTML)
}

func homeToolCardsHTML(kind string) string {
	var cards []string
	for _, page := range publicPages {
		if page.Path == "/" || page.Kind != kind {
			continue
		}
		cards = append(cards, fmt.Sprintf(
			`<a class="tool-card" href="%s"><span>%s</span><strong>%s</strong><small>%s</small></a>`,
			html.EscapeString(page.Path),
			html.EscapeString(toolLabel(page)),
			html.EscapeString(page.Heading),
			html.EscapeString(page.Intro),
		))
	}
	return strings.Join(cards, "")
}

func toolLabel(page publicPage) string {
	if page.Kind == "image" {
		return "Image"
	}
	if page.PageUtility == "csv" || page.PageUtility == "json" || page.PageUtility == "markdown" {
		return "Data"
	}
	if page.PageUtility == "m3u8" {
		return "Video"
	}
	if page.PageUtility == "pdf-unlocker" {
		return "PDF"
	}
	if page.PageUtility == "folder-zip" {
		return "Files"
	}
	return "Creator"
}

func seoMetaTags(title, description, canonical, ogType string) string {
	return fmt.Sprintf(`<meta property="og:title" content="%s">
<meta property="og:description" content="%s">
<meta property="og:url" content="%s">
<meta property="og:type" content="%s">
<meta property="og:site_name" content="OnlineBox">`,
		html.EscapeString(title),
		html.EscapeString(description),
		html.EscapeString(canonical),
		html.EscapeString(ogType),
	)
}

func schemaScript(items []map[string]any) string {
	payload := map[string]any{
		"@context": "https://schema.org",
		"@graph":   items,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return `<script type="application/ld+json">` + string(encoded) + `</script>`
}

func homeSchema() []map[string]any {
	base := siteURL()
	return []map[string]any{
		{
			"@type":       "WebSite",
			"@id":         base + "/#website",
			"url":         base + "/",
			"name":        "OnlineBox",
			"description": publicPageByPath["/"].Description,
			"inLanguage":  "en",
		},
		{
			"@type":       "Organization",
			"@id":         base + "/#organization",
			"name":        "OnlineBox",
			"url":         base + "/",
			"description": "Free browser-based tools for images, data and creator workflows.",
		},
	}
}

func toolPageSchema(page publicPage, canonical string) []map[string]any {
	return []map[string]any{
		{
			"@type":               "WebApplication",
			"@id":                 canonical + "#app",
			"name":                page.Heading,
			"url":                 canonical,
			"description":         page.Description,
			"applicationCategory": applicationCategory(page),
			"operatingSystem":     "Any",
			"isAccessibleForFree": true,
			"inLanguage":          "en",
			"publisher": map[string]any{
				"@type": "Organization",
				"name":  "OnlineBox",
				"url":   siteURL() + "/",
			},
		},
		breadcrumbSchema(canonical, page.Heading),
	}
}

func webPageSchema(page publicPage, canonical string) []map[string]any {
	return []map[string]any{
		{
			"@type":       "WebPage",
			"@id":         canonical + "#webpage",
			"name":        page.Title,
			"url":         canonical,
			"description": page.Description,
			"inLanguage":  "en",
			"isPartOf": map[string]any{
				"@type": "WebSite",
				"name":  "OnlineBox",
				"url":   siteURL() + "/",
			},
		},
		breadcrumbSchema(canonical, page.Heading),
	}
}

func blogPostSchema(post blogPost) []map[string]any {
	canonical := siteURL() + "/blog/" + post.Slug
	return []map[string]any{
		{
			"@type":         "BlogPosting",
			"@id":           canonical + "#blogposting",
			"headline":      post.Title,
			"description":   post.Description,
			"url":           canonical,
			"datePublished": post.Date,
			"dateModified":  post.Date,
			"inLanguage":    "en",
			"author": map[string]any{
				"@type": "Organization",
				"name":  "OnlineBox",
				"url":   siteURL() + "/",
			},
			"publisher": map[string]any{
				"@type": "Organization",
				"name":  "OnlineBox",
				"url":   siteURL() + "/",
			},
		},
		breadcrumbSchema(canonical, post.Title),
	}
}

func breadcrumbSchema(canonical, currentName string) map[string]any {
	return map[string]any{
		"@type": "BreadcrumbList",
		"itemListElement": []map[string]any{
			{
				"@type":    "ListItem",
				"position": 1,
				"name":     "Free browser tools",
				"item":     siteURL() + "/",
			},
			{
				"@type":    "ListItem",
				"position": 2,
				"name":     currentName,
				"item":     canonical,
			},
		},
	}
}

func applicationCategory(page publicPage) string {
	if page.Kind == "image" {
		return "MultimediaApplication"
	}
	if page.PageUtility == "csv" || page.PageUtility == "json" || page.PageUtility == "markdown" {
		return "DeveloperApplication"
	}
	return "UtilitiesApplication"
}

func renderLandingHTML(page publicPage) string {
	canonical := siteURL() + page.Path
	if page.Path == "/" {
		canonical = siteURL() + "/"
	}
	return strings.NewReplacer(
		"__PAGE_TITLE__", html.EscapeString(page.Title),
		"__PAGE_DESCRIPTION__", html.EscapeString(page.Description),
		"__CANONICAL_URL__", html.EscapeString(canonical),
		"__PAGE_HEADING__", html.EscapeString(page.Heading),
		"__PAGE_ACCENT__", html.EscapeString(page.Accent),
		"__PAGE_INTRO__", html.EscapeString(page.Intro),
		"__SEO_META__", seoMetaTags(page.Title, page.Description, canonical, "website"),
		"__JSON_LD__", schemaScript(toolPageSchema(page, canonical)),
		"__PRIMARY_TOOL__", landingToolHTML(page),
		"__GUIDE_CONTENT__", landingGuideHTML(page),
		"__RELATED_LINKS__", relatedLinksHTML(page),
		"__QR_SCRIPT__", qrScriptTag(page),
		"__MARKDOWN_CSS__", markdownCSSLink(page),
		"__LANDING_SCRIPT__", landingScript(page),
		"__GOOGLE_ANALYTICS__", googleAnalyticsTag,
	).Replace(landingHTML)
}

func renderPrivacyHTML(page publicPage) string {
	return strings.NewReplacer(
		"__PAGE_TITLE__", html.EscapeString(page.Title),
		"__PAGE_DESCRIPTION__", html.EscapeString(page.Description),
		"__CANONICAL_URL__", html.EscapeString(siteURL()+page.Path),
		"__PAGE_HEADING__", html.EscapeString(page.Heading),
		"__PAGE_ACCENT__", html.EscapeString(page.Accent),
		"__PAGE_INTRO__", html.EscapeString(page.Intro),
		"__SEO_META__", seoMetaTags(page.Title, page.Description, siteURL()+page.Path, "website"),
		"__JSON_LD__", schemaScript(webPageSchema(page, siteURL()+page.Path)),
		"__GOOGLE_ANALYTICS__", googleAnalyticsTag,
	).Replace(privacyHTML)
}

func renderTrustHTML(page publicPage) string {
	canonical := siteURL() + page.Path
	return strings.NewReplacer(
		"__PAGE_TITLE__", html.EscapeString(page.Title),
		"__PAGE_DESCRIPTION__", html.EscapeString(page.Description),
		"__CANONICAL_URL__", html.EscapeString(canonical),
		"__PAGE_HEADING__", html.EscapeString(page.Heading),
		"__PAGE_ACCENT__", html.EscapeString(page.Accent),
		"__PAGE_INTRO__", html.EscapeString(page.Intro),
		"__SEO_META__", seoMetaTags(page.Title, page.Description, canonical, "website"),
		"__JSON_LD__", schemaScript(webPageSchema(page, canonical)),
		"__TRUST_CONTENT__", trustContentHTML(page.Path),
		"__GOOGLE_ANALYTICS__", googleAnalyticsTag,
	).Replace(trustHTML)
}

func handleBlogIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/blog" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	posts, err := loadBlogPosts()
	if err != nil {
		log.Printf("blog index load error: %v", err)
		http.Error(w, "blog unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(renderBlogIndexHTML(posts)))
}

func handleBlogPost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	slug := strings.Trim(strings.TrimPrefix(r.URL.Path, "/blog/"), "/")
	if slug == "" || strings.Contains(slug, "/") || strings.Contains(slug, "..") {
		http.NotFound(w, r)
		return
	}
	post, err := loadBlogPost(slug)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		log.Printf("blog post load error: %v", err)
		http.Error(w, "blog unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(renderBlogPostHTML(post)))
}

func loadBlogPosts() ([]blogPost, error) {
	entries, err := os.ReadDir(blogContentDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	posts := make([]blogPost, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		slug := strings.TrimSuffix(entry.Name(), ".md")
		post, err := loadBlogPost(slug)
		if err != nil {
			return nil, err
		}
		posts = append(posts, post)
	}
	sort.Slice(posts, func(i, j int) bool {
		if posts[i].dateValue.Equal(posts[j].dateValue) {
			return posts[i].Slug < posts[j].Slug
		}
		return posts[i].dateValue.After(posts[j].dateValue)
	})
	return posts, nil
}

func loadBlogPost(slug string) (blogPost, error) {
	if slug == "" || strings.Contains(slug, "/") || strings.Contains(slug, "..") {
		return blogPost{}, os.ErrNotExist
	}
	raw, err := os.ReadFile(filepath.Join(blogContentDir, slug+".md"))
	if err != nil {
		return blogPost{}, err
	}
	post, err := parseBlogPost(slug, string(raw))
	if err != nil {
		return blogPost{}, fmt.Errorf("%s.md: %w", slug, err)
	}
	return post, nil
}

func parseBlogPost(slug, raw string) (blogPost, error) {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return blogPost{}, errors.New("missing frontmatter")
	}
	parts := strings.SplitN(strings.TrimPrefix(normalized, "---\n"), "\n---\n", 2)
	if len(parts) != 2 {
		return blogPost{}, errors.New("unterminated frontmatter")
	}
	meta := map[string]string{}
	for _, line := range strings.Split(parts[0], "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return blogPost{}, fmt.Errorf("invalid frontmatter line %q", line)
		}
		meta[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	title := meta["title"]
	date := meta["date"]
	description := meta["description"]
	if title == "" || date == "" || description == "" {
		return blogPost{}, errors.New("frontmatter requires title, date and description")
	}
	dateValue, err := time.Parse("2006-01-02", date)
	if err != nil {
		return blogPost{}, fmt.Errorf("invalid date %q", date)
	}
	return blogPost{
		Slug:        slug,
		Title:       title,
		Date:        date,
		Description: description,
		ContentHTML: renderMarkdownHTML(parts[1]),
		dateValue:   dateValue,
	}, nil
}

type markdownRenderState struct {
	html    strings.Builder
	inList  bool
	listTag string
	inQuote bool
}

func renderMarkdownHTML(markdown string) string {
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	state := markdownRenderState{}
	inCode := false
	code := strings.Builder{}
	codeLang := ""
	for index := 0; index < len(lines); index++ {
		raw := lines[index]
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "```") {
			if inCode {
				state.html.WriteString(`<pre><code class="language-`)
				state.html.WriteString(html.EscapeString(codeLang))
				state.html.WriteString(`">`)
				state.html.WriteString(html.EscapeString(strings.TrimSuffix(code.String(), "\n")))
				state.html.WriteString(`</code></pre>`)
				inCode = false
				code.Reset()
				codeLang = ""
			} else {
				closeMarkdownBlocks(&state)
				inCode = true
				codeLang = strings.TrimSpace(strings.TrimPrefix(line, "```"))
			}
			continue
		}
		if inCode {
			code.WriteString(raw)
			code.WriteByte('\n')
			continue
		}
		if line == "" {
			closeMarkdownBlocks(&state)
			continue
		}
		if index+1 < len(lines) && strings.Contains(line, "|") && isMarkdownTableDivider(lines[index+1]) {
			index = renderMarkdownTable(lines, index, &state)
			continue
		}
		switch {
		case line == "---" || line == "***":
			closeMarkdownBlocks(&state)
			state.html.WriteString("<hr>")
		case strings.HasPrefix(line, "> "):
			if state.inList {
				state.html.WriteString("</" + state.listTag + ">")
				state.inList = false
				state.listTag = ""
			}
			if !state.inQuote {
				state.html.WriteString("<blockquote>")
				state.inQuote = true
			}
			state.html.WriteString("<p>" + inlineMarkdownHTML(strings.TrimSpace(strings.TrimPrefix(line, "> "))) + "</p>")
		case strings.HasPrefix(line, "### "):
			closeMarkdownBlocks(&state)
			state.html.WriteString("<h3>" + inlineMarkdownHTML(line[4:]) + "</h3>")
		case strings.HasPrefix(line, "## "):
			closeMarkdownBlocks(&state)
			state.html.WriteString("<h2>" + inlineMarkdownHTML(line[3:]) + "</h2>")
		case strings.HasPrefix(line, "# "):
			closeMarkdownBlocks(&state)
			state.html.WriteString("<h1>" + inlineMarkdownHTML(line[2:]) + "</h1>")
		case isUnorderedMarkdownItem(line):
			addMarkdownListItem(&state, "ul", strings.TrimSpace(line[2:]))
		case isOrderedMarkdownItem(line):
			addMarkdownListItem(&state, "ol", strings.TrimSpace(line[strings.Index(line, ".")+1:]))
		default:
			closeMarkdownBlocks(&state)
			state.html.WriteString("<p>" + inlineMarkdownHTML(line) + "</p>")
		}
	}
	if inCode {
		state.html.WriteString("<pre><code>" + html.EscapeString(strings.TrimSuffix(code.String(), "\n")) + "</code></pre>")
	}
	closeMarkdownBlocks(&state)
	return state.html.String()
}

func closeMarkdownBlocks(state *markdownRenderState) {
	if state.inList {
		state.html.WriteString("</" + state.listTag + ">")
		state.inList = false
		state.listTag = ""
	}
	if state.inQuote {
		state.html.WriteString("</blockquote>")
		state.inQuote = false
	}
}

func inlineMarkdownHTML(text string) string {
	escaped := html.EscapeString(text)
	escaped = inlineLinkPattern.ReplaceAllStringFunc(escaped, func(match string) string {
		parts := inlineLinkPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		href := sanitizeMarkdownHref(html.UnescapeString(parts[2]))
		if href == "" {
			return parts[1]
		}
		return `<a href="` + html.EscapeString(href) + `">` + parts[1] + `</a>`
	})
	escaped = inlineCodePattern.ReplaceAllString(escaped, "<code>$1</code>")
	escaped = inlineStrongPattern.ReplaceAllString(escaped, "<strong>$1</strong>")
	escaped = inlineEmPattern.ReplaceAllString(escaped, "<em>$1</em>")
	return escaped
}

func sanitizeMarkdownHref(href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	lower := strings.ToLower(href)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "mailto:") || strings.HasPrefix(href, "/") || strings.HasPrefix(href, "#") {
		return href
	}
	return ""
}

func isUnorderedMarkdownItem(line string) bool {
	return strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ")
}

func isOrderedMarkdownItem(line string) bool {
	dot := strings.Index(line, ".")
	if dot <= 0 || dot+1 >= len(line) || line[dot+1] != ' ' {
		return false
	}
	for _, ch := range line[:dot] {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func addMarkdownListItem(state *markdownRenderState, tag, value string) {
	if state.inQuote {
		state.html.WriteString("</blockquote>")
		state.inQuote = false
	}
	if state.inList && state.listTag != tag {
		state.html.WriteString("</" + state.listTag + ">")
		state.inList = false
		state.listTag = ""
	}
	if !state.inList {
		state.html.WriteString("<" + tag + ">")
		state.inList = true
		state.listTag = tag
	}
	state.html.WriteString("<li>" + inlineMarkdownHTML(value) + "</li>")
}

func splitMarkdownTableRow(line string) []string {
	value := strings.TrimSpace(line)
	value = strings.TrimPrefix(value, "|")
	value = strings.TrimSuffix(value, "|")
	parts := strings.Split(value, "|")
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}
	return parts
}

func isMarkdownTableDivider(line string) bool {
	cells := splitMarkdownTableRow(line)
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		cell = strings.Trim(cell, " :-")
		if cell != "" {
			return false
		}
	}
	return true
}

func renderMarkdownTable(lines []string, start int, state *markdownRenderState) int {
	headers := splitMarkdownTableRow(lines[start])
	index := start + 2
	var body strings.Builder
	for index < len(lines) {
		line := strings.TrimSpace(lines[index])
		if line == "" || !strings.Contains(line, "|") || isMarkdownTableDivider(line) {
			break
		}
		body.WriteString("<tr>")
		for _, cell := range splitMarkdownTableRow(line) {
			body.WriteString("<td>" + inlineMarkdownHTML(cell) + "</td>")
		}
		body.WriteString("</tr>")
		index++
	}
	closeMarkdownBlocks(state)
	state.html.WriteString(`<div class="table-wrap"><table><thead><tr>`)
	for _, cell := range headers {
		state.html.WriteString("<th>" + inlineMarkdownHTML(cell) + "</th>")
	}
	state.html.WriteString("</tr></thead><tbody>")
	state.html.WriteString(body.String())
	state.html.WriteString("</tbody></table></div>")
	return index - 1
}

func renderBlogIndexHTML(posts []blogPost) string {
	canonical := siteURL() + "/blog"
	title := "Blog | OnlineBox"
	description := "Updates and guides for OnlineBox browser tools."
	return strings.NewReplacer(
		"__PAGE_TITLE__", title,
		"__PAGE_DESCRIPTION__", description,
		"__CANONICAL_URL__", html.EscapeString(canonical),
		"__SEO_META__", seoMetaTags(title, description, canonical, "blog"),
		"__JSON_LD__", schemaScript([]map[string]any{
			{
				"@type":       "Blog",
				"name":        "OnlineBox Blog",
				"url":         canonical,
				"description": description,
				"inLanguage":  "en",
			},
			breadcrumbSchema(canonical, "Blog"),
		}),
		"__BLOG_CONTENT__", blogListHTML(posts),
		"__GOOGLE_ANALYTICS__", googleAnalyticsTag,
	).Replace(blogIndexHTML)
}

func blogListHTML(posts []blogPost) string {
	if len(posts) == 0 {
		return `<p class="empty">No blog posts yet.</p>`
	}
	items := make([]string, 0, len(posts))
	for _, post := range posts {
		items = append(items, fmt.Sprintf(
			`<article class="post-card"><time datetime="%s">%s</time><h2><a href="/blog/%s">%s</a></h2><p>%s</p></article>`,
			html.EscapeString(post.Date),
			html.EscapeString(post.Date),
			html.EscapeString(post.Slug),
			html.EscapeString(post.Title),
			html.EscapeString(post.Description),
		))
	}
	return strings.Join(items, "")
}

func renderBlogPostHTML(post blogPost) string {
	canonical := siteURL() + "/blog/" + post.Slug
	title := post.Title + " | OnlineBox Blog"
	return strings.NewReplacer(
		"__PAGE_TITLE__", html.EscapeString(title),
		"__PAGE_DESCRIPTION__", html.EscapeString(post.Description),
		"__CANONICAL_URL__", html.EscapeString(canonical),
		"__SEO_META__", seoMetaTags(title, post.Description, canonical, "article"),
		"__JSON_LD__", schemaScript(blogPostSchema(post)),
		"__POST_TITLE__", html.EscapeString(post.Title),
		"__POST_DATE__", html.EscapeString(post.Date),
		"__POST_DESCRIPTION__", html.EscapeString(post.Description),
		"__POST_CONTENT__", post.ContentHTML,
		"__GOOGLE_ANALYTICS__", googleAnalyticsTag,
	).Replace(blogPostHTML)
}

func qrScriptTag(page publicPage) string {
	if page.PageUtility == "qr" {
		return `<script src="https://unpkg.com/qrcode-generator@1.4.4/qrcode.js"></script>`
	}
	if page.PageUtility == "m3u8" {
		return `<script src="https://cdn.jsdelivr.net/npm/hls.js@latest"></script>`
	}
	if page.PageUtility == "pdf-unlocker" {
		return `<script src="https://cdn.jsdelivr.net/npm/pdf-lib@latest/dist/pdf-lib.min.js"></script>`
	}
	if page.PageUtility == "folder-zip" {
		return `<script src="https://cdn.jsdelivr.net/npm/fflate@0.8.2/umd/index.js"></script>`
	}
	if page.PageTool == "exif" {
		return `<script src="https://cdn.jsdelivr.net/npm/heic2any@0.0.4/dist/heic2any.min.js"></script>
<script src="https://cdn.jsdelivr.net/npm/exifr/dist/lite.umd.js"></script>`
	}
	if page.PageTool == "compress" || page.PageTool == "convert" || page.PageTool == "resize" || page.PageTool == "watermark" {
		return `<script src="https://cdn.jsdelivr.net/npm/heic2any@0.0.4/dist/heic2any.min.js"></script>`
	}
	return ""
}

func markdownCSSLink(page publicPage) string {
	if page.PageUtility == "markdown" {
		return `<link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/github-markdown-css/5.5.1/github-markdown.min.css">
<link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/KaTeX/0.16.9/katex.min.css">
<link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/styles/github-dark.min.css">
<script defer src="https://cdnjs.cloudflare.com/ajax/libs/KaTeX/0.16.9/katex.min.js"></script>
<script defer src="https://cdnjs.cloudflare.com/ajax/libs/KaTeX/0.16.9/contrib/auto-render.min.js"></script>
<script defer src="https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/highlight.min.js"></script>`
	}
	return ""
}

const googleAnalyticsTag = `<!-- Google tag (gtag.js) -->
<script async src="https://www.googletagmanager.com/gtag/js?id=G-GRDT3349BV"></script>
<script>
  window.dataLayer = window.dataLayer || [];
  function gtag(){dataLayer.push(arguments);}
  gtag('js', new Date());

  gtag('config', 'G-GRDT3349BV');
</script>`

const adsTXT = "google.com, pub-1902780696242483, DIRECT, f08c47fec0942fa0\n"

const faviconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">
<rect width="64" height="64" rx="14" fill="#101113"/>
<circle cx="32" cy="32" r="22" fill="#d4ff57"/>
<circle cx="32" cy="32" r="13" fill="#101113"/>
<rect x="31" y="12" width="20" height="40" rx="10" fill="#101113"/>
<circle cx="42" cy="32" r="8" fill="#d4ff57"/>
</svg>`

const homeHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>__PAGE_TITLE__</title>
<meta name="description" content="__PAGE_DESCRIPTION__">
__SEO_META__
<link rel="canonical" href="__CANONICAL_URL__">
<link rel="icon" href="/favicon.svg" type="image/svg+xml">
<link rel="shortcut icon" href="/favicon.ico">
<link href="https://fonts.googleapis.com/css2?family=Syne:wght@500;700;800&family=DM+Sans:wght@400;500;700&display=swap" rel="stylesheet">
__GOOGLE_ANALYTICS__
__JSON_LD__
<style>
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
:root{--bg:#101113;--panel:#18191d;--panel2:#202126;--line:rgba(255,255,255,.09);--text:#f4f1e8;--muted:#9b988e;--accent:#d4ff57}
body{font-family:'DM Sans',sans-serif;background:var(--bg);color:var(--text);min-height:100vh}
body::before{content:'';position:fixed;inset:0;background:linear-gradient(90deg,rgba(212,255,87,.08),transparent 38%),linear-gradient(rgba(255,255,255,.025) 1px,transparent 1px),linear-gradient(90deg,rgba(255,255,255,.025) 1px,transparent 1px);background-size:auto,46px 46px,46px 46px;pointer-events:none}
.wrap{position:relative;z-index:1;max-width:1060px;margin:0 auto;padding:38px 22px 76px}
.nav{display:flex;justify-content:space-between;align-items:center;margin-bottom:64px}
.brand{font-family:'Syne',sans-serif;font-weight:800;color:var(--accent);text-decoration:none;font-size:19px}
.nav-links{display:flex;gap:16px;flex-wrap:wrap}.nav-links a{color:var(--muted);text-decoration:none;font-size:13px;font-weight:700}
.hero{display:grid;grid-template-columns:minmax(0,1.1fr) minmax(280px,.9fr);gap:42px;align-items:end;margin-bottom:56px}
.kicker{display:inline-flex;color:var(--accent);border:1px solid rgba(212,255,87,.28);background:rgba(212,255,87,.09);border-radius:999px;padding:6px 11px;font-size:11px;font-weight:800;letter-spacing:.08em;text-transform:uppercase;margin-bottom:18px}
h1{font-family:'Syne',sans-serif;font-size:clamp(42px,7vw,78px);line-height:.98;letter-spacing:0;margin-bottom:18px}h1 em{display:block;color:var(--accent);font-style:normal}
.lead{color:var(--muted);font-size:17px;line-height:1.75;max-width:650px}.hero-panel{background:var(--panel);border:1px solid var(--line);border-radius:16px;padding:22px}
.hero-panel strong{display:block;font-family:'Syne',sans-serif;font-size:22px;margin-bottom:12px}.hero-panel p{color:var(--muted);line-height:1.65}
.quick{display:grid;grid-template-columns:repeat(3,1fr);gap:10px;margin-top:18px}.quick a{border:1px solid var(--line);background:var(--panel2);border-radius:10px;color:var(--text);text-decoration:none;padding:12px;font-size:13px;font-weight:800}
.section{margin-top:44px}.section-head{display:flex;justify-content:space-between;gap:24px;align-items:end;margin-bottom:16px}.section h2{font-family:'Syne',sans-serif;font-size:30px}.section p{color:var(--muted);line-height:1.65;max-width:520px}
.grid{display:grid;grid-template-columns:repeat(3,1fr);gap:12px}.tool-card{min-height:188px;background:var(--panel);border:1px solid var(--line);border-radius:14px;padding:18px;text-decoration:none;color:var(--text);display:flex;flex-direction:column;gap:10px;transition:transform .16s,border-color .16s}
.tool-card:hover{transform:translateY(-2px);border-color:rgba(212,255,87,.4)}.tool-card span{align-self:flex-start;color:var(--accent);background:rgba(212,255,87,.09);border:1px solid rgba(212,255,87,.22);border-radius:999px;padding:4px 8px;font-size:10px;font-weight:800;text-transform:uppercase}
.tool-card strong{font-family:'Syne',sans-serif;font-size:19px}.tool-card small{color:var(--muted);line-height:1.55;font-size:13px}
.notes{display:grid;grid-template-columns:repeat(3,1fr);gap:12px;margin-top:18px}.note{background:var(--panel);border:1px solid var(--line);border-radius:14px;padding:18px}.note b{display:block;margin-bottom:8px}.note p{font-size:13px;color:var(--muted);line-height:1.6}
.faq{margin-top:54px;display:grid;grid-template-columns:.7fr 1fr;gap:26px}.faq h2{font-family:'Syne',sans-serif;font-size:30px}.faq details{border-top:1px solid var(--line);padding:16px 0}.faq summary{cursor:pointer;font-weight:800}.faq p{color:var(--muted);line-height:1.65;margin-top:10px}
.site-footer{border-top:1px solid var(--line);margin-top:54px;padding-top:18px;display:flex;justify-content:space-between;gap:12px;flex-wrap:wrap;color:var(--muted);font-size:13px}.site-footer a{color:var(--text);text-decoration:none;font-weight:800}
@media(max-width:820px){.hero,.faq{grid-template-columns:1fr}.grid,.notes,.quick{grid-template-columns:1fr}.nav{align-items:flex-start;gap:16px;flex-direction:column}.wrap{padding-top:26px}}
</style>
</head>
<body>
<main class="wrap">
<nav class="nav">
<a class="brand" href="/">OnlineBox</a>
<div class="nav-links"><a href="#image-tools">Image tools</a><a href="#data-tools">Data tools</a><a href="#creator-tools">Creator tools</a><a href="/blog">Blog</a></div>
</nav>
<section class="hero">
<div>
<div class="kicker">Browser-first tool directory</div>
<h1>__PAGE_HEADING__<em>__PAGE_ACCENT__</em></h1>
<p class="lead">__PAGE_INTRO__</p>
</div>
<aside class="hero-panel">
<strong>Start with a focused tool</strong>
<p>The homepage is only a directory. Each tool has its own page, keyword, instructions and FAQ.</p>
<div class="quick"><a href="/json-formatter">JSON formatter</a><a href="/csv-to-json">CSV to JSON</a><a href="/image-compressor">Compress image</a></div>
</aside>
</section>
<section class="section" id="image-tools">
<div class="section-head"><h2>Image tools</h2><p>Compress, convert and resize images for uploads, product listings, forms and social media.</p></div>
<div class="grid">__IMAGE_TOOL_LINKS__</div>
</section>
<section class="section" id="data-tools">
<div class="section-head"><h2>Data and creator tools</h2><p>Small utilities for developers, operators and creators who need quick browser-based helpers.</p></div>
<div class="grid">__UTILITY_TOOL_LINKS__</div>
</section>
<section class="section">
<div class="section-head"><h2>Why this site is simple</h2><p>Low-cost utility sites work best when each page solves one job clearly.</p></div>
<div class="notes">
<div class="note"><b>Focused pages</b><p>Every tool page targets one search intent instead of mixing unrelated tools into the same content.</p></div>
<div class="note"><b>Browser local</b><p>Most tools run in the browser, keeping server cost low and making the experience fast for small files.</p></div>
<div class="note"><b>Internal links</b><p>The homepage links to every tool page so search engines and users can discover the full toolkit.</p></div>
</div>
</section>
<section class="faq">
<h2>FAQ</h2>
<div>
<details open><summary>Is OnlineBox free?</summary><p>Yes. The current tools are free to use, including batch image compression. Advanced templates or heavier workflows may be added later, but the core browser tools stay easy to try.</p></details>
<details><summary>Do files upload to a server?</summary><p>Most tools are designed to run locally in your browser. The server mainly delivers the page.</p></details>
<details><summary>Why are tools on separate pages?</summary><p>Separate pages are better for users and search engines because each page can focus on one task, one title and one set of instructions.</p></details>
</div>
</section>
<footer class="site-footer"><span>OnlineBox</span><a href="/about">About</a><a href="/contact">Contact</a><a href="/terms">Terms</a><a href="/privacy-policy">Privacy Policy</a></footer>
</main>
</body>
</html>`

func landingToolHTML(page publicPage) string {
	switch page.PageUtility {
	case "csv":
		return `<section class="tool-panel">
<label for="csvInput">CSV input</label>
<textarea id="csvInput" spellcheck="false">name,email,plan
Alice,alice@example.com,free
Bob,bob@example.com,pro</textarea>
<button class="btn" onclick="convertCSV()">Convert to JSON</button>
<pre id="jsonOutput" class="output"></pre>
</section>`
	case "json":
		return `<section class="tool-panel json-tool">
<div class="json-toolbar" aria-label="JSON actions">
<button class="btn compact" onclick="formatJSON()">Format</button>
<button class="ghost compact" onclick="minifyJSON()">Minify</button>
<button class="ghost compact" onclick="copyJSONOutput()">Copy result</button>
<button class="ghost compact" onclick="clearJSONTool()">Clear</button>
</div>
<div id="jsonStatus" class="json-status valid" aria-live="polite">Valid JSON</div>
<div class="json-columns">
<div>
<label for="jsonInput">Raw JSON</label>
<textarea id="jsonInput" class="json-area" spellcheck="false">{"name":"OnlineBox","tools":["formatter","validator","minifier"],"local":true}</textarea>
</div>
<div>
<label for="jsonFormattedOutput">Formatted result</label>
<textarea id="jsonFormattedOutput" class="json-area output-area" readonly spellcheck="false"></textarea>
</div>
</div>
</section>`
	case "markdown":
		return `<section class="tool-panel markdown-tool">
<div class="markdown-toolbar">
<div class="toolbar-group">
<label for="markdownViewMode">View</label>
<select id="markdownViewMode" onchange="setMarkdownViewMode(this.value)"><option value="split" selected>Split</option><option value="editor">Editor only</option><option value="preview">Preview only</option></select>
</div>
<div class="toolbar-group">
<label for="markdownStyle">Style</label>
<select id="markdownStyle" onchange="setMarkdownStyle(this.value)"><option value="Default" selected>Default</option><option value="Academic">Academic</option><option value="Minimal">Minimal</option><option value="Custom">Custom</option></select>
</div>
<div class="toolbar-group title-field">
<label for="markdownTitle">PDF title</label>
<input id="markdownTitle" value="Markdown PDF" oninput="renderMarkdown(false)">
</div>
<button class="btn compact" onclick="renderMarkdown(true)">Export PDF</button>
</div>
<div id="markdownCustomPanel" class="markdown-custom-panel" hidden>
<label>Body size <span id="customFontSizeValue">14px</span><input id="customFontSize" type="range" min="12" max="18" step="1" value="14" oninput="updateCustomMarkdownStyle()"></label>
<label>Line height <span id="customLineHeightValue">1.6</span><input id="customLineHeight" type="range" min="1.2" max="2" step="0.1" value="1.6" oninput="updateCustomMarkdownStyle()"></label>
<label>Page margin <span id="customMarginValue">16mm</span><input id="customMargin" type="range" min="10" max="30" step="1" value="16" oninput="updateCustomMarkdownStyle()"></label>
<label>Title color <input id="customTitleColor" type="color" value="#1f2328" oninput="updateCustomMarkdownStyle()"></label>
<label>Font <select id="customFontFamily" onchange="updateCustomMarkdownStyle()"><option value="sans" selected>Sans-serif</option><option value="serif">Serif</option><option value="mono">Monospace</option></select></label>
</div>
<div class="markdown-actions">
<input id="markdownFileInput" class="file-input-hidden" type="file" accept=".md,.markdown,text/markdown,text/plain" onchange="loadMarkdownFile(this.files[0])">
<button class="ghost compact" onclick="document.getElementById('markdownFileInput').click()">Open .md</button>
<button class="ghost compact" onclick="saveMarkdownFile()">Save .md</button>
<span id="markdownStats" class="markdown-stat">0 words</span>
<span id="markdownDraftStatus" class="markdown-stat">Draft saved locally</span>
</div>
<div id="markdownWorkbench" class="markdown-workbench split" data-style="Default">
<div class="markdown-pane editor-pane">
<label for="markdownInput">Markdown input</label>
<textarea id="markdownInput" spellcheck="false" oninput="renderMarkdown(false)"># Weekly Project Report

## Completed This Week

### Feature Development
- Completed user login with JWT authentication
- Fixed occasional 500 errors during image upload
- Added CSV export

### Performance
| Metric | This Week | Last Week | Change |
|---|---|---|---|
| DAU | 1,200 | 980 | +22% |
| Error Count | 12 | 34 | -65% |

> Memory leak investigation continues next week.

&#96;&#96;&#96;go
func handleUpload(w http.ResponseWriter, r *http.Request) {
    file, _, err := r.FormFile("image")
    if err != nil {
        http.Error(w, "upload failed", 400)
        return
    }
    defer file.Close()
}
&#96;&#96;&#96;</textarea>
</div>
<div class="markdown-pane preview-pane">
<div class="pane-label">Live preview</div>
<div id="markdownPreview" class="markdown-preview output preview markdown-body" data-style="Default"></div>
</div>
</div>
</section>`
	case "pdf-unlocker":
		return `<section class="tool-panel pdf-tool">
<input id="pdfUnlockInput" class="file-input-hidden" type="file" accept="application/pdf,.pdf" onchange="unlockPDFFile(this.files[0])">
<div id="pdfUnlockDrop" class="file-drop" role="button" tabindex="0" onclick="document.getElementById('pdfUnlockInput').click()" onkeydown="handlePDFUnlockDropKey(event)">
<div class="file-drop-icon">PDF</div>
<strong>Drop your PDF here</strong>
<span>Choose a PDF and the unlocked copy downloads automatically. Processing stays in this browser.</span>
</div>
<button id="pdfUnlockButton" class="btn" onclick="document.getElementById('pdfUnlockInput').click()">Choose PDF</button>
<div id="pdfUnlockStatus" class="pdf-status" aria-live="polite">No PDF selected yet.</div>
</section>`
	case "folder-zip":
		return `<section class="tool-panel zip-tool">
<input id="zipFolderInput" class="file-input-hidden" type="file" webkitdirectory multiple onchange="loadZipFiles(this.files)">
<input id="zipFilesInput" class="file-input-hidden" type="file" multiple onchange="loadZipFiles(this.files)">
<div id="zipDrop" class="file-drop zip-drop" role="button" tabindex="0" onclick="document.getElementById('zipFolderInput').click()" onkeydown="handleZipDropKey(event)">
<div class="file-drop-icon">ZIP</div>
<strong>Drop files here or choose a folder</strong>
<span>Create a ZIP locally in your browser. Folder structure is kept by default.</span>
</div>
<div class="zip-actions">
<button class="btn compact" onclick="document.getElementById('zipFolderInput').click()">Choose folder</button>
<button class="ghost compact" onclick="document.getElementById('zipFilesInput').click()">Choose files</button>
</div>
<div class="zip-options">
<label>Compression level<select id="zipLevel"><option value="1">Fast</option><option value="6" selected>Balanced</option><option value="9">Maximum</option></select></label>
<label>ZIP file name<input id="zipName" value="onlinebox-files.zip"></label>
<label class="check-row"><input id="zipKeepPaths" type="checkbox" checked> Keep folder structure</label>
<label class="check-row"><input id="zipSkipJunk" type="checkbox" checked> Skip .DS_Store, Thumbs.db and hidden files</label>
</div>
<button id="zipCreateButton" class="btn" onclick="createFolderZip()">Create ZIP</button>
<div id="zipSummary" class="zip-summary">No files selected yet.</div>
<div id="zipAnalysis" class="zip-analysis"></div>
<div id="zipFileList" class="file-list"></div>
</section>`
	case "qr":
		return `<section class="tool-panel">
<label for="qrText">QR code content</label>
<textarea id="qrText">https://onlinebox.site/</textarea>
<div class="grid two">
<label>Foreground color<input id="qrDark" type="color" value="#0e0e11"></label>
<label>Background color<input id="qrLight" type="color" value="#ffffff"></label>
</div>
<button class="btn" onclick="generateQR()">Generate QR code</button>
<button class="ghost" onclick="downloadCanvas('qrCanvas','qrcode.png')">Download PNG</button>
<div class="canvas-wrap"><canvas id="qrCanvas" width="260" height="260"></canvas></div>
</section>`
	case "social":
		return `<section class="tool-panel">
<label for="cardTitle">Title</label>
<input id="cardTitle" value="Browser Tool Suite">
<label for="cardSubtitle">Subtitle</label>
<input id="cardSubtitle" value="Compress, convert, resize and create useful assets locally.">
<label for="cardAccent">Accent color</label>
<input id="cardAccent" type="color" value="#d4ff57">
<button class="btn" onclick="renderSocialCard(true)">Generate and download card</button>
<div class="canvas-wrap wide"><canvas id="socialCanvas" width="1200" height="630"></canvas></div>
</section>`
	case "gradient":
		return `<section class="tool-panel">
<label for="gradientDirection">Direction</label>
<select id="gradientDirection"><option value="135deg">Diagonal</option><option value="90deg">Horizontal</option><option value="180deg">Vertical</option><option value="45deg">Soft angle</option></select>
<button class="btn" onclick="generateGradient()">Generate gradient</button>
<div id="gradientPreview" class="gradient-preview"></div>
<pre id="gradientCode" class="output"></pre>
</section>`
	case "m3u8":
		return `<section class="tool-panel m3u8-tool">
<div class="m3u8-input-row">
<input id="m3u8Url" type="url" placeholder="https://example.com/live/playlist.m3u8" aria-label="M3U8 URL">
<button class="btn m3u8-play-btn" onclick="loadM3U8()">Play</button>
</div>
<div id="m3u8Status" class="hint">Enter an HLS stream URL, then click Play.</div>
<div class="m3u8-player" id="m3u8Player">
<video id="m3u8Video" playsinline></video>
<div class="m3u8-controls" id="m3u8Controls">
<button class="player-btn" id="m3u8Toggle" onclick="toggleM3U8Playback()" aria-label="Play or pause">Play</button>
<span class="m3u8-time" id="m3u8Current">0:00</span>
<input id="m3u8Progress" class="m3u8-progress" type="range" min="0" max="1000" value="0" aria-label="Playback progress">
<span class="m3u8-time" id="m3u8Duration">0:00</span>
<input id="m3u8Volume" class="m3u8-volume" type="range" min="0" max="1" step="0.01" value="1" aria-label="Volume">
<div class="speed-group" aria-label="Playback speed">
<button class="speed-btn" onclick="setM3U8Speed(0.5,this)">0.5x</button>
<button class="speed-btn" onclick="setM3U8Speed(0.75,this)">0.75x</button>
<button class="speed-btn on" onclick="setM3U8Speed(1,this)">1x</button>
<button class="speed-btn" onclick="setM3U8Speed(1.25,this)">1.25x</button>
<button class="speed-btn" onclick="setM3U8Speed(1.5,this)">1.5x</button>
<button class="speed-btn" onclick="setM3U8Speed(2,this)">2x</button>
</div>
<button class="player-btn" onclick="toggleM3U8Fullscreen()" aria-label="Fullscreen">Full</button>
</div>
</div>
</section>`
	}
	switch page.PageTool {
	case "convert":
		return imageToolHTML("Output format", `<div class="choices"><button class="chip on" onclick="setFormat('image/jpeg','JPG',this)">JPG</button><button class="chip" onclick="setFormat('image/png','PNG',this)">PNG</button><button class="chip" onclick="setFormat('image/webp','WebP',this)">WebP</button></div>`, "Convert image", "convertImage()")
	case "resize":
		return imageToolHTML("Output size", `<div class="grid two"><input id="resizeW" type="number" value="200" min="1" max="12000" aria-label="Width"><input id="resizeH" type="number" value="200" min="1" max="12000" aria-label="Height"></div><div class="choices"><button class="chip on" onclick="setResizeMode('contain',this)">Contain</button><button class="chip" onclick="setResizeMode('cover',this)">Cover</button><button class="chip" onclick="setResizeMode('stretch',this)">Stretch</button></div>`, "Resize image", "resizeImage()")
	case "exif":
		return imageToolHTML("Metadata actions", `<div class="choices"><button class="chip on" onclick="loadEXIFMetadata()">View EXIF</button><button class="chip" onclick="downloadWithoutMetadata()">Remove metadata</button></div><pre id="exifOutput" class="output">Choose a photo to inspect EXIF metadata.</pre>`, "Download clean JPG", "downloadWithoutMetadata()")
	case "watermark":
		return imageToolHTML("Watermark settings", `<label for="watermarkText">Watermark text</label><input id="watermarkText" value="onlinebox.site" oninput="renderWatermarkPreview()"><div class="grid two"><label for="watermarkSize">Text size<input id="watermarkSize" type="number" value="42" min="10" max="220" oninput="renderWatermarkPreview()"></label><label for="watermarkOpacity">Opacity<input id="watermarkOpacity" type="range" value="0.55" min="0.1" max="1" step="0.05" oninput="renderWatermarkPreview()"></label></div><label for="watermarkPosition">Position</label><select id="watermarkPosition" onchange="renderWatermarkPreview()"><option value="bottom-right">Bottom right</option><option value="bottom-left">Bottom left</option><option value="top-right">Top right</option><option value="top-left">Top left</option><option value="center">Center</option></select>`, "Download watermarked JPG", "downloadWatermarkedImage()")
	case "batch":
		return `<section class="tool-panel">
<label for="batchInput">Choose images</label>
<div id="batchDrop" class="file-drop" role="button" tabindex="0" onclick="document.getElementById('batchInput').click()" onkeydown="handleBatchDropKey(event)">
<input id="batchInput" class="file-input-hidden" type="file" accept="image/jpeg,image/png,image/webp" multiple onchange="loadBatchFiles(this.files)">
<div class="file-drop-icon">+</div>
<strong>Choose multiple images</strong>
<span>Drag a group of JPG, PNG or WebP images here, or click and use Shift, Cmd or Ctrl to select more than one file.</span>
</div>
<div id="batchInfo" class="hint">No images selected yet. Files are compressed locally in your browser.</div>
<div id="batchList" class="file-list" aria-live="polite"></div>
<label for="batchTargetKB">Target file size per image</label>
<div class="grid unit"><input id="batchTargetKB" type="number" value="200" min="10" max="20000"><span>KB</span></div>
<div class="choices"><button class="chip" onclick="setBatchTarget(100)">100 KB</button><button class="chip on" onclick="setBatchTarget(200)">200 KB</button><button class="chip" onclick="setBatchTarget(500)">500 KB</button><button class="chip" onclick="setBatchTarget(1024)">1 MB</button></div>
<button class="btn" onclick="compressBatchImages()">Compress batch</button>
<div id="batchStatus" class="hint"></div>
<pre id="batchOutput" class="output"></pre>
</section>`
	default:
		return imageToolHTML("Target file size", `<div class="grid unit"><input id="targetKB" type="number" value="200" min="10" max="20000"><span>KB</span></div><div class="choices"><button class="chip" onclick="setTarget(100)">100 KB</button><button class="chip on" onclick="setTarget(200)">200 KB</button><button class="chip" onclick="setTarget(500)">500 KB</button><button class="chip" onclick="setTarget(1024)">1 MB</button></div>`, "Compress image", "compressImage()")
	}
}

func imageToolHTML(label, controls, actionLabel, action string) string {
	return `<section class="tool-panel">
<label for="imageInput">Choose image</label>
<div id="imageDrop" class="file-drop image-drop" role="button" tabindex="0" onclick="document.getElementById('imageInput').click()" onkeydown="handleImageDropKey(event)">
<input id="imageInput" class="file-input-hidden" type="file" accept="image/jpeg,image/png,image/webp,image/heic,image/heif,.heic,.heif" onchange="loadImageFile(this.files[0])">
<div class="file-drop-icon">+</div>
<strong>Drop an image here or click to browse</strong>
<span>JPG, PNG, WebP, HEIC and HEIF · processed locally in your browser</span>
</div>
<div id="imageInfo" class="hint">Supports HEIC, HEIF, JPG, PNG and WebP. The image is processed locally in your browser.</div>
<label>` + label + `</label>
` + controls + `
<button class="btn" onclick="` + action + `">` + actionLabel + `</button>
<div class="canvas-wrap"><canvas id="imageCanvas"></canvas></div>
<div id="status" class="hint"></div>
</section>`
}

func relatedLinksHTML(page publicPage) string {
	anchors := map[string][][2]string{
		"/image-compressor":       {{"/batch-image-compressor", "Batch image compressor"}, {"/image-resizer", "Image resizer for exact dimensions"}, {"/image-converter", "HEIC to JPG converter"}, {"/blog/compress-image-to-200kb-online", "How to compress an image to 200KB"}},
		"/batch-image-compressor": {{"/image-compressor", "Compress one image to a target KB"}, {"/image-resizer", "Resize images online"}, {"/image-converter", "Convert HEIC, JPG, PNG and WebP"}},
		"/image-converter":        {{"/image-compressor", "Image compressor to 200KB"}, {"/image-resizer", "Resize converted images"}, {"/blog/convert-heic-to-jpg-online", "How to convert HEIC to JPG"}},
		"/image-resizer":          {{"/image-compressor", "Compress resized images"}, {"/batch-image-compressor", "Batch image compressor"}, {"/image-converter", "Image format converter"}},
		"/json-formatter":         {{"/csv-to-json", "CSV to JSON converter"}, {"/blog/format-and-validate-json-online", "How to format and validate JSON"}, {"/markdown-to-pdf", "Markdown to PDF converter"}},
		"/csv-to-json":            {{"/json-formatter", "JSON Formatter and Validator"}, {"/blog/format-and-validate-json-online", "JSON validation guide"}, {"/markdown-to-pdf", "Markdown to PDF converter"}},
		"/markdown-to-pdf":        {{"/pdf-unlocker", "PDF Unlocker for copy and print restrictions"}, {"/json-formatter", "JSON Formatter for code snippets"}, {"/blog/convert-markdown-to-pdf-browser", "How to convert Markdown to PDF"}},
		"/pdf-unlocker":           {{"/markdown-to-pdf", "Markdown to PDF converter"}, {"/blog/remove-pdf-copy-print-restrictions", "How to remove PDF copy and print restrictions"}, {"/json-formatter", "JSON Formatter and Validator"}},
		"/qr-code-generator":      {{"/social-card-maker", "Social card maker"}, {"/gradient-generator", "CSS gradient generator"}, {"/image-compressor", "Compress QR images"}},
		"/social-card-maker":      {{"/gradient-generator", "CSS gradient generator"}, {"/image-compressor", "Image compressor"}, {"/qr-code-generator", "QR code generator"}},
		"/gradient-generator":     {{"/social-card-maker", "Social card maker"}, {"/image-watermark", "Image watermark tool"}, {"/qr-code-generator", "QR code generator"}},
		"/folder-to-zip":          {{"/image-compressor", "Image compressor to reduce files before ZIP"}, {"/pdf-unlocker", "PDF Unlocker for PDF files"}, {"/json-formatter", "JSON Formatter before sharing data files"}},
	}
	selected := anchors[page.Path]
	if len(selected) == 0 {
		selected = [][2]string{{"/image-compressor", "Image compressor"}, {"/json-formatter", "JSON Formatter and Validator"}, {"/markdown-to-pdf", "Markdown to PDF converter"}, {"/pdf-unlocker", "PDF Unlocker"}}
	}
	links := make([]string, 0, len(selected))
	for _, item := range selected {
		links = append(links, fmt.Sprintf(`<a href="%s">%s</a>`, html.EscapeString(item[0]), html.EscapeString(item[1])))
	}
	return strings.Join(links, "")
}

func landingGuideHTML(page publicPage) string {
	switch page.Path {
	case "/csv-to-json":
		return `<section class="guide">
<h2>How to use the CSV to JSON converter</h2>
<ol>
<li>Paste CSV text with a header row, for example name,email,plan.</li>
<li>Click “Convert to JSON”. The first row becomes the field names and each following row becomes a JSON object.</li>
<li>Check the formatted output before using it in an API request, import job or mock data file.</li>
</ol>
<h2>When should you convert CSV to JSON?</h2>
<p>CSV to JSON conversion is useful for API testing, preparing import data, cleaning small spreadsheets, building low-code configuration and creating front-end mock data. This tool is designed for simple CSV tables where the first row contains field names and every row after that contains values.</p>
<h2>What should you check before converting?</h2>
<p>Make sure the first row contains clear field names such as title, email, price or status. If a field name is empty, the converter uses a fallback name such as field_0. If a value contains a comma, wrap it in double quotes so the parser can keep it as one cell.</p>
<section class="faq-block">
<h2>CSV to JSON FAQ</h2>
<details open><summary>Does the first CSV row need to be a header?</summary><p>Yes, that is recommended. The converter uses the first row as JSON keys, so a header row makes the output readable.</p></details>
<details><summary>Does the data upload to a server?</summary><p>No. The conversion runs in your browser and the pasted CSV is not sent to the server for conversion.</p></details>
<details><summary>Can it handle quoted values and commas?</summary><p>It supports common double-quoted CSV values. For example, "New York, USA" is treated as one cell.</p></details>
<details><summary>Can I convert an Excel file?</summary><p>This page accepts pasted CSV text. If you have an Excel file, export or save it as CSV first, then paste the CSV content here.</p></details>
</section>
</section>`
	case "/json-formatter":
		return `<section class="guide">
<h2>How to use the JSON Formatter & Validator</h2>
<ol>
<li>Paste raw JSON into the left input box. Validation runs as you type.</li>
<li>Click Format to beautify the JSON with indentation, or Minify to compress it into a single line.</li>
<li>Use Copy result when the output is ready, or Clear to reset both panels.</li>
</ol>
<h2>Best use cases for JSON formatting</h2>
<p>This tool is useful for reading API responses, checking configuration files, cleaning webhook payloads, preparing mock data, debugging front-end state and sharing compact JSON snippets. Everything runs locally in your browser for the core formatting workflow, so pasted JSON is not uploaded for formatting.</p>
<h2>What the validator checks</h2>
<p>The validator uses the browser JSON parser and reports syntax problems such as trailing commas, missing quotes, unclosed objects, invalid escape sequences and unexpected tokens. When the browser exposes a character position, the error message includes the line and column.</p>
<h2>Common JSON formatting tasks</h2>
<p>Use this page when you need to format JSON online, validate a JSON error line, beautify API responses, minify JSON for storage, clean webhook payloads or check whether a configuration file is valid JSON. The formatter is useful for small debugging tasks because it does not require installing a desktop app or uploading private API data.</p>
<section class="faq-block">
<h2>JSON Formatter FAQ</h2>
<details open><summary>Is this JSON formatter free?</summary><p>Yes. You can format, validate, minify, copy and clear JSON directly in the browser.</p></details>
<details><summary>Does my JSON upload to a server?</summary><p>No. The formatting and validation logic runs locally in your browser, and the server only delivers the page.</p></details>
<details><summary>Why does valid JavaScript object syntax fail?</summary><p>JSON is stricter than JavaScript object literals. Keys and string values must use double quotes, and comments or trailing commas are not allowed.</p></details>
<details><summary>Can it show where a JSON error happened?</summary><p>Yes when the browser provides the character offset or line and column. The tool converts that into an easy-to-read position next to the parser message.</p></details>
<details><summary>What is the difference between format and minify?</summary><p>Format adds indentation and line breaks for readability. Minify removes unnecessary whitespace to make JSON smaller for storage, URLs or API payloads.</p></details>
</section>
</section>`
	case "/image-compressor":
		return `<section class="guide">
<h2>How to use the image compressor</h2>
<ol><li>Upload an image, enter a target size such as 200KB, 500KB or 1MB, then click Compress image.</li><li>The compressor exports a smaller JPG locally in your browser while trying to stay close to your target file size.</li><li>If the result is not small enough, lower the target KB value and compress the image again.</li></ol>
<h2>Compress image to 200KB, 500KB or 1MB</h2>
<p>This image compressor is useful when a form, job portal, profile page or ecommerce platform asks for a photo under a specific size limit. You can compress JPG, PNG, WebP, HEIC and HEIF images to common targets such as 200KB, 500KB or 1MB without uploading the original file to a server.</p>
<h2>When should you reduce image size for upload?</h2>
<p>Use it for application forms, avatars, document photos, marketplace images, product photos and social media uploads. If you also need exact width and height, use the related image resizer after compression.</p>
<section class="faq-block">
<h2>Image Compressor FAQ</h2>
<details open><summary>Does the image upload to a server?</summary><p>No. The current tool uses browser features to process the image locally. The server mainly delivers the page.</p></details>
<details><summary>Can I compress an image to exactly 200KB?</summary><p>The tool tries to get close to the target size. The exact output depends on image dimensions, detail and browser encoding support.</p></details>
<details><summary>Which image formats are supported?</summary><p>Common JPG, PNG and WebP files are supported, plus HEIC and HEIF preview conversion in supported browsers.</p></details>
<details><summary>Where is the compressed image saved?</summary><p>The compressed image downloads to your browser's default download folder.</p></details>
</section>
</section>`
	case "/image-converter":
		return `<section class="guide">
<h2>How to use the HEIC to JPG image converter</h2>
<ol><li>Choose JPG, PNG or WebP as the output format.</li><li>Upload a HEIC, HEIF, JPG, PNG or WebP image and click Convert image.</li><li>The converted image downloads automatically after browser-local processing.</li></ol>
<h2>Convert iPhone HEIC photos to JPG online</h2>
<p>Many websites and older apps do not accept HEIC photos from iPhone. This converter helps turn iPhone HEIC photos into JPG, PNG or WebP in your browser, so you can upload photos to forms, marketplaces, CMS editors and social media tools without installing image software.</p>
<h2>When is the HEIC to JPG image converter useful?</h2>
<p>Use it to convert PNG to JPG for smaller uploads, create WebP assets for web pages, turn screenshots into JPG or fix an incompatible image format before sending a file to another service.</p>
<section class="faq-block">
<h2>HEIC to JPG FAQ</h2>
<details open><summary>Does the image upload to a server?</summary><p>No. Conversion runs locally in your browser for the core workflow.</p></details>
<details><summary>Can I convert iPhone photos?</summary><p>Yes. HEIC and HEIF files from iPhone can be converted in browsers that support the local decoder library.</p></details>
<details><summary>Which output formats are supported?</summary><p>You can export JPG, PNG or WebP depending on browser support.</p></details>
</section>
</section>`
	case "/image-resizer":
		return imageGuideHTML("image resizer", "Enter the target width and height, choose contain, cover or stretch mode, then upload an image and resize it.", "It is useful for avatars, product photos, social covers, form uploads and fixed-ratio design assets.")
	case "/batch-image-compressor":
		return imageGuideHTML("batch image compressor", "Select multiple JPG, PNG or WebP images, choose a target size per image, then click Compress batch. Each compressed file downloads separately as it finishes.", "It is useful when you need to prepare product photos, document scans, marketplace images or social media assets with the same file-size limit.")
	case "/exif-viewer":
		return imageGuideHTML("EXIF viewer and remover", "Upload a photo to view EXIF metadata. If you want a privacy-safe copy, click Remove metadata or Download clean JPG to export a new JPG without embedded metadata.", "It is useful before sharing iPhone or camera photos online because EXIF can include device, capture date, lens settings and sometimes GPS location data.")
	case "/image-watermark":
		return imageGuideHTML("image watermark tool", "Upload an image, enter watermark text, choose position, size and opacity, then download a watermarked JPG.", "It is useful for creator previews, marketplace images, portfolio samples and quick brand marks before posting images publicly.")
	case "/qr-code-generator":
		return utilityGuideHTML("QR code generator", "Enter a link or text, choose foreground and background colors, generate the QR code and download it as PNG.", "It is useful for campaign links, menus, social profiles, business cards and printed materials.")
	case "/markdown-to-pdf":
		return `<section class="guide">
<h2>How to convert Markdown to PDF</h2>
<ol>
<li>Paste Markdown text into the editor or open a local .md file.</li>
<li>Check the live preview with tables, code blocks, syntax highlighting and LaTeX math rendering.</li>
<li>Choose a style preset or custom PDF settings, then export with your browser print dialog.</li>
</ol>
<h2>Markdown to PDF with live preview</h2>
<p>This tool is useful when you need to export Markdown to PDF in the browser with a live preview, code highlighting, math formulas, custom title, page numbers and academic or minimal formatting. It is a practical option for project notes, documentation drafts, meeting notes, lightweight reports and README-style documents.</p>
<h2>When browser-based PDF export helps</h2>
<p>Use it when you want a quick Markdown PDF without installing a desktop editor. Your draft is saved locally in the browser, and the Markdown content is not uploaded to a server for rendering.</p>
<section class="faq-block">
<h2>Markdown to PDF FAQ</h2>
<details open><summary>Does my Markdown upload to a server?</summary><p>No. Editing, preview and export run in your browser.</p></details>
<details><summary>Does it support code highlighting?</summary><p>Yes. Code blocks can be highlighted in the preview and printed PDF.</p></details>
<details><summary>Can I customize PDF style?</summary><p>Yes. Choose Default, Academic, Minimal or Custom style settings before exporting.</p></details>
<details><summary>Does it support math formulas?</summary><p>Yes. The page uses KaTeX for LaTeX-style math rendering.</p></details>
</section>
</section>`
	case "/pdf-unlocker":
		return `<section class="guide">
<h2>How to use PDF Unlocker</h2>
<ol>
<li>Drop a PDF into the upload area or click Choose PDF.</li>
<li>The tool rebuilds the PDF locally in your browser with permission restrictions removed.</li>
<li>The unlocked PDF downloads automatically when processing finishes.</li>
</ol>
<h2>Best use cases</h2>
<p>PDF Unlocker is useful when you own the document or have permission to work with it and need to copy text, print a file, annotate pages, merge documents or edit a PDF in another app. Everything runs locally in your browser, so the PDF is not uploaded to OnlineBox.</p>
<h2>Remove PDF copy, print and edit restrictions</h2>
<p>Use this page to remove PDF copy restrictions, unlock PDF printing restrictions or remove edit restrictions from a PDF that you can already open. It does not remove open passwords. If a file asks for a password before it can be viewed, the browser cannot rebuild it without that password.</p>
<section class="faq-block">
<h2>PDF Unlocker FAQ</h2>
<details open><summary>Does this upload my PDF?</summary><p>No. The PDF is processed in your browser with pdf-lib. The server only delivers the page and JavaScript library.</p></details>
<details><summary>Can it remove an open password?</summary><p>No. This tool removes permission restrictions only, not open passwords.</p></details>
<details><summary>What restrictions can it remove?</summary><p>It is designed for copy, print and edit permission flags on PDFs that can already be opened in the browser.</p></details>
<details><summary>Why did my PDF fail?</summary><p>The file may require an open password, use unsupported encryption or be damaged. Try opening it locally first to confirm the PDF itself works.</p></details>
<details><summary>Is it okay to unlock any PDF?</summary><p>Use it only for files you own or have permission to modify. Some documents may be protected by legal or workplace rules.</p></details>
</section>
</section>`
	case "/m3u8-player":
		return `<section class="guide">
<h2>How to use the M3U8 Player</h2>
<ol>
<li>Paste a direct .m3u8 or HLS playlist URL into the input field.</li>
<li>Click Play. Safari uses native HLS playback, while other modern browsers use hls.js.</li>
<li>Use the custom controls to pause, seek, adjust volume, change speed or enter fullscreen.</li>
</ol>
<h2>Best use cases for M3U8 playback</h2>
<p>This online HLS player is useful for testing live streams, previewing adaptive bitrate video, checking VOD playlists, verifying CDN delivery and opening M3U8 links without installing desktop media software.</p>
<section class="faq-block">
<h2>M3U8 FAQ</h2>
<details open><summary>What is an M3U8 file?</summary><p>An M3U8 file is a playlist used by HLS video streams. It points the player to media segments and, in adaptive streams, alternate quality levels.</p></details>
<details><summary>Does this upload my video anywhere?</summary><p>No. The page loads the stream URL in your browser. OnlineBox does not upload or re-host the video content.</p></details>
<details><summary>Why does an M3U8 URL fail to play?</summary><p>The stream may block cross-origin browser playback, require authentication, be offline, use unsupported codecs or only allow specific domains.</p></details>
<details><summary>Does it work with live HLS streams?</summary><p>Yes. It can play both live and video-on-demand HLS playlists when the stream server allows browser playback.</p></details>
<details><summary>Which browsers support M3U8 playback?</summary><p>Safari supports HLS natively. Chrome, Edge and Firefox can play many HLS streams through hls.js when Media Source Extensions are available.</p></details>
</section>
</section>`
	case "/social-card-maker":
		return utilityGuideHTML("social card maker", "Enter a title, subtitle and accent color, then generate a 1200x630 sharing image.", "It is useful for blog covers, product updates, social posts and launch announcements.")
	case "/gradient-generator":
		return utilityGuideHTML("gradient generator", "Choose a direction, generate a random gradient and copy the CSS background code.", "It is useful for website backgrounds, cards, banners, posters and visual exploration.")
	case "/folder-to-zip":
		return `<section class="guide">
<h2>How to compress a folder to ZIP online</h2>
<ol>
<li>Click Choose folder or drag multiple files into the upload area.</li>
<li>Choose Fast, Balanced or Maximum compression. Keep folder structure if you want the ZIP to preserve subfolders.</li>
<li>Click Create ZIP and download the ZIP file generated locally in your browser.</li>
</ol>
<h2>Folder to ZIP in your browser</h2>
<p>This tool creates ZIP files online without uploading your folder to a server. It is useful when you need to send multiple files, archive a project folder, package CSV and JSON data files, collect documents or share a folder with its structure preserved.</p>
<h2>What compresses well?</h2>
<p>Text, code, CSV, JSON, HTML, CSS, logs and similar files usually compress well. JPG, PNG, WebP, PDF, MP4, MP3 and existing ZIP or RAR archives are already compressed, so the ZIP may not become much smaller. For images, compress them first with the image compressor if file size matters.</p>
<section class="faq-block">
<h2>Folder to ZIP FAQ</h2>
<details open><summary>Does the folder upload to a server?</summary><p>No. Files are read by your browser and compressed locally with fflate.</p></details>
<details><summary>Can it keep folder structure?</summary><p>Yes. Keep folder structure is enabled by default when your browser provides relative folder paths.</p></details>
<details><summary>Does it support many file types?</summary><p>Yes. ZIP can contain almost any file extension. Some file types simply do not shrink much because they are already compressed.</p></details>
<details><summary>Is Maximum compression the same as 7-Zip?</summary><p>No. This browser tool creates standard ZIP files. 7-Zip can be smaller for text-heavy folders, but ZIP is widely supported and works directly in the browser.</p></details>
<details><summary>Which browsers support folder upload?</summary><p>Chrome and Edge have the best folder upload support. Other browsers may support selecting multiple files instead.</p></details>
</section>
</section>`
	default:
		return utilityGuideHTML(page.Heading, page.Intro, "It is useful for quick browser-based file and content tasks.")
	}
}

func imageGuideHTML(name, how, scenario string) string {
	return fmt.Sprintf(`<section class="guide">
<h2>How to use the %s</h2>
<ol><li>%s</li><li>After processing, check whether the output file size, format or dimensions match your target platform.</li><li>If the result is not right, adjust the settings and process the image again.</li></ol>
<h2>When is the %s useful?</h2>
<p>%s Everything runs locally in your browser for the core image workflow, so the image does not need to be uploaded to a server for the core operation.</p>
<section class="faq-block">
<h2>FAQ</h2>
<details open><summary>Does the image upload to a server?</summary><p>No. The current tool uses browser features to process the image locally. The server mainly delivers the page.</p></details>
<details><summary>Which image formats are supported?</summary><p>Common JPG, PNG and WebP files are supported. Output support can vary slightly by browser.</p></details>
<details><summary>Where is the processed image saved?</summary><p>After conversion, compression or resizing, the result downloads to your browser's default download folder.</p></details>
</section>
</section>`, html.EscapeString(name), html.EscapeString(how), html.EscapeString(name), html.EscapeString(scenario))
}

func utilityGuideHTML(name, how, scenario string) string {
	return fmt.Sprintf(`<section class="guide">
<h2>How to use the %s</h2>
<p>%s</p>
<h2>When is the %s useful?</h2>
<p>%s Everything runs locally in your browser for the core workflow. This tool is designed for quick, lightweight browser-based tasks.</p>
<section class="faq-block">
<h2>FAQ</h2>
<details open><summary>Is this tool free?</summary><p>Yes, the basic tool is available for free. Future batch workflows, templates or high-resolution exports may become Pro features.</p></details>
<details><summary>Does my data upload to a server?</summary><p>The core processing logic runs in your browser, which is useful for small content you do not want to upload.</p></details>
<details><summary>Does it work on mobile?</summary><p>Yes. The page uses a responsive layout and can be opened in modern mobile browsers.</p></details>
</section>
</section>`, html.EscapeString(name), html.EscapeString(how), html.EscapeString(name), html.EscapeString(scenario))
}

func landingScript(page publicPage) string {
	if page.PageUtility != "" {
		return utilityLandingScript(page.PageUtility)
	}
	if page.PageTool == "batch" {
		return batchImageLandingScript()
	}
	if page.PageTool == "exif" {
		return imageLandingScript() + "\n" + exifLandingScript()
	}
	if page.PageTool == "watermark" {
		return imageLandingScript() + "\n" + watermarkLandingScript()
	}
	return imageLandingScript()
}

func utilityLandingScript(tool string) string {
	switch tool {
	case "csv":
		return `function parseCSV(text){const rows=[];let row=[],cell='',quote=false;for(let i=0;i<text.length;i++){const ch=text[i],next=text[i+1];if(ch==='"'&&quote&&next==='"'){cell+='"';i++;}else if(ch==='"'){quote=!quote;}else if(ch===','&&!quote){row.push(cell.trim());cell='';}else if((ch==='\n'||ch==='\r')&&!quote){if(ch==='\r'&&next==='\n')i++;row.push(cell.trim());if(row.some(v=>v!==''))rows.push(row);row=[];cell='';}else{cell+=ch;}}row.push(cell.trim());if(row.some(v=>v!==''))rows.push(row);return rows;}
function convertCSV(){const rows=parseCSV(document.getElementById('csvInput').value);const out=document.getElementById('jsonOutput');if(rows.length<2){out.textContent='Paste CSV content with a header row.';return;}const headers=rows[0];const data=rows.slice(1).map(row=>{const item={};headers.forEach((header,index)=>{item[header||('field_'+index)]=row[index]||'';});return item;});out.textContent=JSON.stringify(data,null,2);}
convertCSV();`
	case "json":
		return `function jsonPositionFromMessage(message,text){let match=message.match(/position\s+(\d+)/i);if(match){const pos=parseInt(match[1],10);let line=1,column=1;for(let i=0;i<Math.min(pos,text.length);i++){if(text[i]==='\n'){line++;column=1;}else{column++;}}return {pos,line,column};}match=message.match(/line\s+(\d+)\s+column\s+(\d+)/i);if(match){return {line:parseInt(match[1],10),column:parseInt(match[2],10)};}return null;}
function setJSONStatus(message,valid){const status=document.getElementById('jsonStatus');status.textContent=message;status.classList.toggle('valid',valid);status.classList.toggle('invalid',!valid);}
function parseJSONInput(){const input=document.getElementById('jsonInput').value;if(!input.trim()){setJSONStatus('Paste JSON to validate and format.',true);return {ok:false,empty:true};}try{return {ok:true,value:JSON.parse(input),text:input};}catch(error){const position=jsonPositionFromMessage(error.message,input);let detail=error.message;if(position&&!/line\s+\d+\s+column\s+\d+/i.test(detail)){detail+=' at line '+position.line+', column '+position.column;if(Number.isFinite(position.pos))detail+=' (position '+position.pos+')';}setJSONStatus('Invalid JSON: '+detail,false);return {ok:false,error};}}
function validateJSONLive(){const parsed=parseJSONInput();if(parsed.ok){setJSONStatus('Valid JSON',true);}return parsed;}
function formatJSON(){const parsed=parseJSONInput();if(!parsed.ok)return;document.getElementById('jsonFormattedOutput').value=JSON.stringify(parsed.value,null,2);setJSONStatus('Valid JSON · formatted',true);}
function minifyJSON(){const parsed=parseJSONInput();if(!parsed.ok)return;document.getElementById('jsonFormattedOutput').value=JSON.stringify(parsed.value);setJSONStatus('Valid JSON · minified',true);}
async function copyJSONOutput(){const output=document.getElementById('jsonFormattedOutput');if(!output.value){setJSONStatus('Nothing to copy yet. Format or minify first.',false);return;}try{await navigator.clipboard.writeText(output.value);setJSONStatus('Copied formatted result to clipboard.',true);}catch(error){output.focus();output.select();document.execCommand('copy');setJSONStatus('Copied formatted result to clipboard.',true);}}
function clearJSONTool(){document.getElementById('jsonInput').value='';document.getElementById('jsonFormattedOutput').value='';setJSONStatus('Paste JSON to validate and format.',true);}
document.getElementById('jsonInput').addEventListener('input',()=>{const parsed=validateJSONLive();if(parsed.ok)document.getElementById('jsonFormattedOutput').value=JSON.stringify(parsed.value,null,2);});
formatJSON();`
	case "markdown":
		return `function escapeHTML(value){return value.replace(/[&<>"']/g,ch=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[ch]));}
const MARKDOWN_STORAGE_KEY='onlinebox.markdownToPdf.draft.v2';
function inlineMarkdown(text){return escapeHTML(text).replace(/\[([^\]]+)\]\((https?:\/\/[^)]+)\)/g,'<a href="$2">$1</a>').replace(/\x60([^\x60]+)\x60/g,'<code>$1</code>').replace(/\*\*(.*?)\*\*/g,'<strong>$1</strong>').replace(/\*(.*?)\*/g,'<em>$1</em>');}
function splitTableRow(line){let value=line.trim();if(value.startsWith('|'))value=value.slice(1);if(value.endsWith('|'))value=value.slice(0,-1);return value.split('|').map(cell=>cell.trim());}
function isTableDivider(line){return /^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$/.test(line);}
function closeBlocks(state){if(state.inList){state.html+='</'+state.listTag+'>';state.inList=false;state.listTag='';}if(state.inQuote){state.html+='</blockquote>';state.inQuote=false;}}
function addListItem(state,tag,text){if(state.inQuote){state.html+='</blockquote>';state.inQuote=false;}if(state.inList&&state.listTag!==tag){state.html+='</'+state.listTag+'>';state.inList=false;state.listTag='';}if(!state.inList){state.html+='<'+tag+'>';state.inList=true;state.listTag=tag;}state.html+='<li>'+inlineMarkdown(text)+'</li>';}
function renderTable(lines,start,state){const headers=splitTableRow(lines[start]);let index=start+2;let body='';while(index<lines.length&&lines[index].includes('|')&&lines[index].trim()&&!isTableDivider(lines[index])){body+='<tr>'+splitTableRow(lines[index]).map(cell=>'<td>'+inlineMarkdown(cell)+'</td>').join('')+'</tr>';index++;}closeBlocks(state);state.html+='<div class="table-wrap"><table><thead><tr>'+headers.map(cell=>'<th>'+inlineMarkdown(cell)+'</th>').join('')+'</tr></thead><tbody>'+body+'</tbody></table></div>';return index-1;}
function markdownToHTML(markdown){const lines=markdown.split(/\r?\n/);const state={html:'',inList:false,inQuote:false,listTag:''};let inCode=false,code='',codeLang='';for(let i=0;i<lines.length;i++){const raw=lines[i],line=raw.trim();if(line.startsWith('\x60\x60\x60')){if(inCode){state.html+='<pre><code class="language-'+escapeHTML(codeLang)+'">'+escapeHTML(code.replace(/\n$/,''))+'</code></pre>';inCode=false;code='';codeLang='';}else{closeBlocks(state);inCode=true;codeLang=line.slice(3).trim();}continue;}if(inCode){code+=raw+'\n';continue;}if(!line){closeBlocks(state);continue;}if(i+1<lines.length&&line.includes('|')&&isTableDivider(lines[i+1])){i=renderTable(lines,i,state);continue;}if(line==='---'||line==='***'){closeBlocks(state);state.html+='<hr>';continue;}if(line.startsWith('> ')){if(state.inList){state.html+='</'+state.listTag+'>';state.inList=false;state.listTag='';}if(!state.inQuote){state.html+='<blockquote>';state.inQuote=true;}state.html+='<p>'+inlineMarkdown(line.slice(2))+'</p>';continue;}if(line.startsWith('### ')){closeBlocks(state);state.html+='<h3>'+inlineMarkdown(line.slice(4))+'</h3>';}else if(line.startsWith('## ')){closeBlocks(state);state.html+='<h2>'+inlineMarkdown(line.slice(3))+'</h2>';}else if(line.startsWith('# ')){closeBlocks(state);state.html+='<h1>'+inlineMarkdown(line.slice(2))+'</h1>';}else if(/^[-*]\s+/.test(line)){addListItem(state,'ul',line.replace(/^[-*]\s+/,''));}else if(/^\d+\.\s+/.test(line)){addListItem(state,'ol',line.replace(/^\d+\.\s+/,''));}else{closeBlocks(state);state.html+='<p>'+inlineMarkdown(line)+'</p>';}}if(inCode){state.html+='<pre><code>'+escapeHTML(code.replace(/\n$/,''))+'</code></pre>';}closeBlocks(state);return state.html;}
const markdownCSSHref='https://cdnjs.cloudflare.com/ajax/libs/github-markdown-css/5.5.1/github-markdown.min.css';
const markdownKatexCSS='https://cdnjs.cloudflare.com/ajax/libs/KaTeX/0.16.9/katex.min.css';
const markdownHighlightCSS='https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/styles/github.min.css';
const markdownPrintBodyStyle='body.markdown-body{box-sizing:border-box;max-width:860px;margin:40px auto;padding:0 32px;color:#1f2328;background:#fff;color-scheme:light;--color-canvas-default:#fff;--color-canvas-subtle:#f6f8fa;--color-fg-default:#1f2328;--color-fg-muted:#59636e;--color-border-default:#d0d7de;--color-border-muted:#d8dee4}body.academic{font-family:Georgia,"Times New Roman",serif;line-height:1.8;max-width:780px}body.academic h1,body.academic h2,body.academic h3{font-family:Georgia,"Times New Roman",serif}body.minimal{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;max-width:820px}body.minimal h1,body.minimal h2{border-bottom:0}body.minimal blockquote{border-left:0;background:transparent;padding-left:0}body.minimal table,body.minimal th,body.minimal td{border-color:transparent}body.custom{font-family:var(--md-font-family);font-size:var(--md-font-size);line-height:var(--md-line-height);max-width:860px}body.custom h1,body.custom h2,body.custom h3{font-family:inherit;color:var(--md-title-color)}table{display:table;width:100%;color:#1f2328;background:#fff}th{background:#f6f8fa;color:#1f2328}td{color:#1f2328;background:#fff}pre{white-space:pre-wrap;word-break:break-word;background:#f6f8fa;color:#1f2328}pre code{color:#1f2328}code{color:#1f2328}pre,table,blockquote{break-inside:avoid;page-break-inside:avoid}h1,h2,h3{break-after:avoid;page-break-after:avoid}p,li{orphans:3;widows:3}@media print{body.markdown-body{max-width:none;margin:0;padding:0}pre{overflow:visible}}';
function markdownPrintStyle(settings){const margin=settings?settings.margin:16;return '@page{margin:'+margin+'mm;@bottom-center{content:counter(page) " / " counter(pages);font-size:10px;color:#6b7280}}'+markdownPrintBodyStyle;}
function markdownDocumentTitle(){return (document.getElementById('markdownTitle').value||'Markdown PDF').trim()||'Markdown PDF';}
function updateMarkdownStats(){const text=document.getElementById('markdownInput').value;const words=(text.trim().match(/\S+/g)||[]).length;const chars=text.length;document.getElementById('markdownStats').textContent=words+' words · '+chars+' chars';}
const markdownFontMap={sans:'-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif',serif:'Georgia,"Times New Roman",serif',mono:'ui-monospace,SFMono-Regular,Menlo,monospace'};
function clampNumber(value,min,max,fallback){const next=parseFloat(value);if(!Number.isFinite(next))return fallback;return Math.min(max,Math.max(min,next));}
function customMarkdownSettings(){return {fontSize:clampNumber(document.getElementById('customFontSize').value,12,18,14),lineHeight:clampNumber(document.getElementById('customLineHeight').value,1.2,2,1.6),margin:clampNumber(document.getElementById('customMargin').value,10,30,16),titleColor:/^#[0-9a-f]{6}$/i.test(document.getElementById('customTitleColor').value)?document.getElementById('customTitleColor').value:'#1f2328',font:markdownFontMap[document.getElementById('customFontFamily').value]?document.getElementById('customFontFamily').value:'sans'};}
function applyCustomMarkdownStyle(){const settings=customMarkdownSettings();document.getElementById('customFontSizeValue').textContent=settings.fontSize+'px';document.getElementById('customLineHeightValue').textContent=settings.lineHeight.toFixed(1);document.getElementById('customMarginValue').textContent=settings.margin+'mm';const preview=document.getElementById('markdownPreview');preview.style.setProperty('--md-font-size',settings.fontSize+'px');preview.style.setProperty('--md-line-height',settings.lineHeight);preview.style.setProperty('--md-title-color',settings.titleColor);preview.style.setProperty('--md-font-family',markdownFontMap[settings.font]);return settings;}
function updateCustomMarkdownStyle(){applyCustomMarkdownStyle();if(document.getElementById('markdownStyle').value==='Custom')renderMarkdown(false);else saveMarkdownDraft();}
function customMarkdownStyleAttribute(settings){return ('--md-font-size:'+settings.fontSize+'px;--md-line-height:'+settings.lineHeight+';--md-title-color:'+settings.titleColor+';--md-font-family:'+markdownFontMap[settings.font]).replace(/"/g,'&quot;');}
function saveMarkdownDraft(){const payload={title:document.getElementById('markdownTitle').value,markdown:document.getElementById('markdownInput').value,style:document.getElementById('markdownStyle').value,view:document.getElementById('markdownViewMode').value,custom:customMarkdownSettings(),updatedAt:Date.now()};localStorage.setItem(MARKDOWN_STORAGE_KEY,JSON.stringify(payload));document.getElementById('markdownDraftStatus').textContent='Draft saved locally';}
function loadMarkdownDraft(){try{const raw=localStorage.getItem(MARKDOWN_STORAGE_KEY);if(!raw)return;const draft=JSON.parse(raw);if(draft.markdown)document.getElementById('markdownInput').value=draft.markdown;if(draft.title)document.getElementById('markdownTitle').value=draft.title;if(draft.style)document.getElementById('markdownStyle').value=draft.style;if(draft.view)document.getElementById('markdownViewMode').value=draft.view;if(draft.custom){document.getElementById('customFontSize').value=clampNumber(draft.custom.fontSize,12,18,14);document.getElementById('customLineHeight').value=clampNumber(draft.custom.lineHeight,1.2,2,1.6);document.getElementById('customMargin').value=clampNumber(draft.custom.margin,10,30,16);document.getElementById('customTitleColor').value=/^#[0-9a-f]{6}$/i.test(draft.custom.titleColor)?draft.custom.titleColor:'#1f2328';document.getElementById('customFontFamily').value=markdownFontMap[draft.custom.font]?draft.custom.font:'sans';}}catch(error){}}
function setMarkdownViewMode(mode){const safe=['split','editor','preview'].includes(mode)?mode:'split';const workbench=document.getElementById('markdownWorkbench');workbench.classList.remove('split','editor','preview');workbench.classList.add(safe);saveMarkdownDraft();}
function setMarkdownStyle(style){const safe=['Default','Academic','Minimal','Custom'].includes(style)?style:'Default';document.getElementById('markdownWorkbench').dataset.style=safe;document.getElementById('markdownPreview').dataset.style=safe;document.getElementById('markdownCustomPanel').hidden=safe!=='Custom';applyCustomMarkdownStyle();saveMarkdownDraft();renderMarkdown(false);}
function applyMarkdownEnhancements(target){if(window.hljs){target.querySelectorAll('pre code').forEach(block=>hljs.highlightElement(block));}if(window.renderMathInElement){renderMathInElement(target,{delimiters:[{left:'$$',right:'$$',display:true},{left:'$',right:'$',display:false},{left:'\\\\(',right:'\\\\)',display:false},{left:'\\\\[',right:'\\\\]',display:true}],throwOnError:false});}}
function renderMarkdown(print){const preview=document.getElementById('markdownPreview');const settings=applyCustomMarkdownStyle();preview.innerHTML=markdownToHTML(document.getElementById('markdownInput').value);applyMarkdownEnhancements(preview);updateMarkdownStats();if(!print){saveMarkdownDraft();return;}const style=document.getElementById('markdownStyle').value;const bodyClass=style==='Academic'?'academic':style==='Minimal'?'minimal':style==='Custom'?'custom':'';const bodyStyle=style==='Custom'?' style="'+customMarkdownStyleAttribute(settings)+'"':'';const html=preview.innerHTML;const w=window.open('','_blank');w.document.write('<!doctype html><html><head><title>'+escapeHTML(markdownDocumentTitle())+'</title><link rel="stylesheet" href="'+markdownCSSHref+'"><link rel="stylesheet" href="'+markdownKatexCSS+'"><link rel="stylesheet" href="'+markdownHighlightCSS+'"><style>'+markdownPrintStyle(settings)+'</style></head><body class="markdown-body '+bodyClass+'"'+bodyStyle+'>'+html+'<script>let printed=false;function printWhenReady(){if(printed)return;printed=true;setTimeout(()=>window.print(),120)}window.addEventListener(\"load\",printWhenReady);setTimeout(printWhenReady,1500);<\/script></body></html>');w.document.close();}
function loadMarkdownFile(file){if(!file)return;const reader=new FileReader();reader.onload=()=>{document.getElementById('markdownInput').value=reader.result||'';if(file.name)document.getElementById('markdownTitle').value=file.name.replace(/\.(md|markdown|txt)$/i,'');renderMarkdown(false);};reader.readAsText(file);}
function saveMarkdownFile(){const blob=new Blob([document.getElementById('markdownInput').value],{type:'text/markdown'});const url=URL.createObjectURL(blob);const a=document.createElement('a');a.href=url;a.download=(markdownDocumentTitle().toLowerCase().replace(/[^a-z0-9]+/g,'-').replace(/^-|-$/g,'')||'markdown-document')+'.md';a.click();URL.revokeObjectURL(url);}
loadMarkdownDraft();setMarkdownViewMode(document.getElementById('markdownViewMode').value);setMarkdownStyle(document.getElementById('markdownStyle').value);renderMarkdown(false);`
	case "pdf-unlocker":
		return `function setPDFUnlockStatus(message,state){const el=document.getElementById('pdfUnlockStatus');el.textContent=message;el.classList.remove('loading','success','error');if(state)el.classList.add(state);}
function setPDFUnlockBusy(busy){const button=document.getElementById('pdfUnlockButton');button.disabled=busy;button.textContent=busy?'Processing...':'Choose PDF';}
function pdfUnlockDownload(blob,filename){const url=URL.createObjectURL(blob);const a=document.createElement('a');a.href=url;a.download=filename;a.click();URL.revokeObjectURL(url);}
function pdfUnlockName(file){return 'unlocked_'+(file.name||'document.pdf').replace(/\.pdf$/i,'')+'.pdf';}
function isOpenPasswordError(error){const message=String(error&&error.message||error||'');return /password|decrypt|encrypted|encryption/i.test(message);}
async function unlockPDFFile(file){if(!file)return;if(!/\.pdf$/i.test(file.name||'')&&file.type!=='application/pdf'){setPDFUnlockStatus('Please choose a PDF file.','error');return;}if(!window.PDFLib){setPDFUnlockStatus('PDF library is still loading. Try again in a moment.','error');return;}setPDFUnlockBusy(true);setPDFUnlockStatus('Removing PDF permission restrictions locally...','loading');try{const bytes=await file.arrayBuffer();let source;try{source=await PDFLib.PDFDocument.load(bytes,{ignoreEncryption:true});}catch(error){if(isOpenPasswordError(error)){setPDFUnlockStatus('This tool removes permission restrictions only, not open passwords','error');}else{setPDFUnlockStatus('Could not read this PDF file.','error');}return;}const unlocked=await PDFLib.PDFDocument.create();const pages=await unlocked.copyPages(source,source.getPageIndices());pages.forEach(page=>unlocked.addPage(page));const pdfBytes=await unlocked.save();pdfUnlockDownload(new Blob([pdfBytes],{type:'application/pdf'}),pdfUnlockName(file));setPDFUnlockStatus('Unlocked PDF downloaded. Your file never left this browser.','success');}catch(error){if(isOpenPasswordError(error)){setPDFUnlockStatus('This tool removes permission restrictions only, not open passwords','error');}else{setPDFUnlockStatus('Could not unlock this PDF. It may be damaged or use unsupported encryption.','error');}}finally{setPDFUnlockBusy(false);document.getElementById('pdfUnlockInput').value='';}}
function handlePDFUnlockDropKey(event){if(event.key==='Enter'||event.key===' '){event.preventDefault();document.getElementById('pdfUnlockInput').click();}}
function setupPDFUnlockDrop(){const drop=document.getElementById('pdfUnlockDrop');if(!drop)return;['dragenter','dragover'].forEach(name=>drop.addEventListener(name,event=>{event.preventDefault();drop.classList.add('over');}));['dragleave','drop'].forEach(name=>drop.addEventListener(name,event=>{event.preventDefault();drop.classList.remove('over');}));drop.addEventListener('drop',event=>{const file=event.dataTransfer.files&&event.dataTransfer.files[0];unlockPDFFile(file);});}
setupPDFUnlockDrop();`
	case "folder-zip":
		return `let zipFiles=[];
function formatZipBytes(bytes){if(!bytes)return '0 B';const units=['B','KB','MB','GB'];let value=bytes,index=0;while(value>=1024&&index<units.length-1){value/=1024;index++;}return value.toFixed(value>=10||index===0?0:1)+' '+units[index];}
function zipPath(file){return file.webkitRelativePath||file.name;}
function zipBaseName(path){return path.split('/').pop()||path;}
function isZipJunk(path){const parts=path.split('/');const name=zipBaseName(path);return name==='.DS_Store'||name==='Thumbs.db'||parts.some(part=>part.startsWith('.')&&part!=='.');}
function zipKind(file){const name=(file.name||'').toLowerCase();if(/\.(txt|md|csv|json|xml|html|css|js|ts|tsx|jsx|log|sql|yaml|yml)$/i.test(name))return 'text';if(/\.(jpg|jpeg|png|webp|gif|heic|heif)$/i.test(name))return 'image';if(/\.(mp4|mov|mp3|wav|m4a|webm)$/i.test(name))return 'media';if(/\.(pdf|docx|xlsx|pptx)$/i.test(name))return 'document';if(/\.(zip|rar|7z|gz|tar)$/i.test(name))return 'archive';return 'other';}
function zipEstimateNote(counts){const text=counts.text||0,compressed=(counts.image||0)+(counts.media||0)+(counts.archive||0)+(counts.document||0);if(text>compressed)return 'High compression expected for text, code, CSV, JSON and logs.';if(compressed>text)return 'Many selected files are already compressed, so ZIP may not become much smaller.';return 'Compression depends on file types. Text shrinks more than images, video, PDF or existing archives.';}
function setZipSummary(message){document.getElementById('zipSummary').textContent=message;}
function renderZipSelection(){const skip=document.getElementById('zipSkipJunk').checked;const files=zipFiles.filter(file=>!skip||!isZipJunk(zipPath(file)));const total=files.reduce((sum,file)=>sum+file.size,0);const counts={};files.forEach(file=>{const kind=zipKind(file);counts[kind]=(counts[kind]||0)+1;});setZipSummary(files.length?files.length+' file(s) selected · '+formatZipBytes(total)+' total':'No files selected yet.');document.getElementById('zipAnalysis').innerHTML=files.length?'<span>Text/code: '+(counts.text||0)+'</span><span>Images: '+(counts.image||0)+'</span><span>PDF/Office: '+(counts.document||0)+'</span><span>Media/archives: '+((counts.media||0)+(counts.archive||0))+'</span><small>'+zipEstimateNote(counts)+'</small>':'';const list=document.getElementById('zipFileList');list.textContent='';files.slice(0,10).forEach(file=>{const row=document.createElement('div');row.className='file-pill';const name=document.createElement('span');name.textContent=zipPath(file);const size=document.createElement('span');size.textContent=formatZipBytes(file.size);row.append(name,size);list.appendChild(row);});if(files.length>10){const more=document.createElement('div');more.className='file-pill';more.textContent='+'+(files.length-10)+' more file(s)';list.appendChild(more);}}
function loadZipFiles(files){zipFiles=Array.from(files||[]);renderZipSelection();}
function handleZipDropKey(event){if(event.key==='Enter'||event.key===' '){event.preventDefault();document.getElementById('zipFolderInput').click();}}
function setupZipDrop(){const drop=document.getElementById('zipDrop');if(!drop)return;['dragenter','dragover'].forEach(name=>drop.addEventListener(name,event=>{event.preventDefault();drop.classList.add('over');}));['dragleave','drop'].forEach(name=>drop.addEventListener(name,event=>{event.preventDefault();drop.classList.remove('over');}));drop.addEventListener('drop',event=>loadZipFiles(event.dataTransfer.files));document.getElementById('zipSkipJunk').addEventListener('change',renderZipSelection);}
function zipDownload(blob,name){const url=URL.createObjectURL(blob);const a=document.createElement('a');a.href=url;a.download=name.endsWith('.zip')?name:name+'.zip';a.click();URL.revokeObjectURL(url);}
async function createFolderZip(){const skip=document.getElementById('zipSkipJunk').checked;const keep=document.getElementById('zipKeepPaths').checked;const files=zipFiles.filter(file=>!skip||!isZipJunk(zipPath(file)));if(!files.length){setZipSummary('Choose files or a folder first.');return;}if(!window.fflate){setZipSummary('ZIP library is still loading. Try again in a moment.');return;}const button=document.getElementById('zipCreateButton');button.disabled=true;button.textContent='Creating ZIP...';try{const entries={};for(const file of files){const path=keep?zipPath(file):zipBaseName(zipPath(file));entries[path]=new Uint8Array(await file.arrayBuffer());}const level=parseInt(document.getElementById('zipLevel').value,10)||6;const zipped=fflate.zipSync(entries,{level});zipDownload(new Blob([zipped],{type:'application/zip'}),document.getElementById('zipName').value.trim()||'onlinebox-files.zip');const original=files.reduce((sum,file)=>sum+file.size,0);const saved=original-zipped.length;setZipSummary('ZIP created · '+formatZipBytes(original)+' to '+formatZipBytes(zipped.length)+' · '+(saved>0?formatZipBytes(saved)+' saved':'already compressed files may not shrink'));}catch(error){setZipSummary('Could not create ZIP. Try fewer files or a smaller folder.');}finally{button.disabled=false;button.textContent='Create ZIP';}}
setupZipDrop();`
	case "qr":
		return `function downloadBlob(blob,filename){const url=URL.createObjectURL(blob);const a=document.createElement('a');a.download=filename;a.href=url;a.click();URL.revokeObjectURL(url);}
function downloadCanvas(id,filename){document.getElementById(id).toBlob(blob=>{if(blob)downloadBlob(blob,filename);},'image/png');}
function generateQR(){const text=document.getElementById('qrText').value.trim();const canvas=document.getElementById('qrCanvas');const ctx=canvas.getContext('2d');if(!text)return;if(!window.qrcode){ctx.fillStyle='#fff';ctx.fillRect(0,0,canvas.width,canvas.height);ctx.fillStyle='#111';ctx.fillText('QR library failed to load',56,128);return;}const qr=qrcode(0,'M');qr.addData(text);qr.make();const count=qr.getModuleCount();const margin=16;const cell=Math.floor((canvas.width-margin*2)/count);const size=cell*count;const offset=Math.floor((canvas.width-size)/2);ctx.fillStyle=document.getElementById('qrLight').value;ctx.fillRect(0,0,canvas.width,canvas.height);ctx.fillStyle=document.getElementById('qrDark').value;for(let row=0;row<count;row++){for(let col=0;col<count;col++){if(qr.isDark(row,col))ctx.fillRect(offset+col*cell,offset+row*cell,cell,cell);}}}
generateQR();`
	case "social":
		return `function downloadBlob(blob,filename){const url=URL.createObjectURL(blob);const a=document.createElement('a');a.download=filename;a.href=url;a.click();URL.revokeObjectURL(url);}
function wrapCanvasText(ctx,text,x,y,maxWidth,lineHeight){const words=text.split(/\s+/);let line='';for(const word of words){const test=line?line+' '+word:word;if(ctx.measureText(test).width>maxWidth&&line){ctx.fillText(line,x,y);line=word;y+=lineHeight;}else{line=test;}}if(line)ctx.fillText(line,x,y);}
function renderSocialCard(download){const canvas=document.getElementById('socialCanvas');const ctx=canvas.getContext('2d');const accent=document.getElementById('cardAccent').value||'#d4ff57';ctx.fillStyle='#0e0e11';ctx.fillRect(0,0,canvas.width,canvas.height);ctx.fillStyle=accent;ctx.fillRect(0,0,26,canvas.height);ctx.fillStyle='#fff';ctx.font='800 72px sans-serif';wrapCanvasText(ctx,document.getElementById('cardTitle').value||'Browser Tool Suite',95,230,960,82);ctx.fillStyle='#b7b7b7';ctx.font='400 30px sans-serif';wrapCanvasText(ctx,document.getElementById('cardSubtitle').value||'Useful tools that run locally in your browser.',100,400,920,42);if(download)canvas.toBlob(blob=>downloadBlob(blob,'social-card.png'),'image/png');}
renderSocialCard(false);`
	case "gradient":
		return `function randomColor(){return '#'+Math.floor(Math.random()*16777215).toString(16).padStart(6,'0');}
function generateGradient(){const colors=[randomColor(),randomColor(),randomColor()];const direction=document.getElementById('gradientDirection').value;const css='linear-gradient('+direction+', '+colors.join(', ')+')';document.getElementById('gradientPreview').style.background=css;document.getElementById('gradientCode').textContent='background: '+css+';';}
generateGradient();`
	case "m3u8":
		return `let m3u8Hls=null,m3u8HideTimer=null,m3u8Seeking=false;
const m3u8Video=document.getElementById('m3u8Video');
const m3u8Player=document.getElementById('m3u8Player');
const m3u8Progress=document.getElementById('m3u8Progress');
const m3u8Status=document.getElementById('m3u8Status');
function setM3U8Status(message){m3u8Status.textContent=message;}
function formatM3U8Time(value){if(!Number.isFinite(value)||value<0)return '0:00';const total=Math.floor(value);const h=Math.floor(total/3600);const m=Math.floor((total%3600)/60);const s=String(total%60).padStart(2,'0');return h? h+':'+String(m).padStart(2,'0')+':'+s : m+':'+s;}
function showM3U8Controls(){m3u8Player.classList.add('controls-visible');clearTimeout(m3u8HideTimer);if(!m3u8Video.paused){m3u8HideTimer=setTimeout(()=>m3u8Player.classList.remove('controls-visible'),2000);}}
function updateM3U8Button(){document.getElementById('m3u8Toggle').textContent=m3u8Video.paused?'Play':'Pause';}
function updateM3U8Progress(){document.getElementById('m3u8Current').textContent=formatM3U8Time(m3u8Video.currentTime);document.getElementById('m3u8Duration').textContent=formatM3U8Time(m3u8Video.duration);if(!m3u8Seeking&&Number.isFinite(m3u8Video.duration)&&m3u8Video.duration>0){m3u8Progress.value=Math.round((m3u8Video.currentTime/m3u8Video.duration)*1000);}}
function loadM3U8(){const url=document.getElementById('m3u8Url').value.trim();if(!url){setM3U8Status('Paste an M3U8 URL first.');return;}if(m3u8Hls){m3u8Hls.destroy();m3u8Hls=null;}m3u8Video.pause();m3u8Video.removeAttribute('src');m3u8Video.load();setM3U8Status('Loading stream...');if(m3u8Video.canPlayType('application/vnd.apple.mpegurl')){m3u8Video.src=url;m3u8Video.play().then(()=>setM3U8Status('Playing with native HLS support.')).catch(()=>setM3U8Status('Stream loaded. Press Play to start.'));return;}if(window.Hls&&Hls.isSupported()){m3u8Hls=new Hls();m3u8Hls.loadSource(url);m3u8Hls.attachMedia(m3u8Video);m3u8Hls.on(Hls.Events.MANIFEST_PARSED,()=>{m3u8Video.play().then(()=>setM3U8Status('Playing with hls.js.')).catch(()=>setM3U8Status('Stream loaded. Press Play to start.'));});m3u8Hls.on(Hls.Events.ERROR,(event,data)=>{if(data&&data.fatal){setM3U8Status('Playback error: '+data.type+'. Check the URL, CORS settings or stream availability.');}});return;}setM3U8Status('This browser does not support HLS playback.');}
function toggleM3U8Playback(){if(!m3u8Video.src&&!m3u8Hls){loadM3U8();return;}if(m3u8Video.paused){m3u8Video.play().catch(()=>setM3U8Status('Playback was blocked. Check the stream URL.'));}else{m3u8Video.pause();}showM3U8Controls();}
function setM3U8Speed(speed,button){m3u8Video.playbackRate=speed;document.querySelectorAll('.speed-btn').forEach(item=>item.classList.remove('on'));button.classList.add('on');showM3U8Controls();}
function toggleM3U8Fullscreen(){if(document.fullscreenElement){document.exitFullscreen();return;}if(m3u8Player.requestFullscreen)m3u8Player.requestFullscreen();}
m3u8Player.addEventListener('mousemove',showM3U8Controls);
m3u8Player.addEventListener('mouseenter',showM3U8Controls);
m3u8Player.addEventListener('mouseleave',()=>{clearTimeout(m3u8HideTimer);m3u8HideTimer=setTimeout(()=>m3u8Player.classList.remove('controls-visible'),2000);});
m3u8Video.addEventListener('play',()=>{updateM3U8Button();showM3U8Controls();});
m3u8Video.addEventListener('pause',()=>{updateM3U8Button();showM3U8Controls();});
m3u8Video.addEventListener('timeupdate',updateM3U8Progress);
m3u8Video.addEventListener('durationchange',updateM3U8Progress);
m3u8Video.addEventListener('loadedmetadata',updateM3U8Progress);
m3u8Video.addEventListener('click',toggleM3U8Playback);
m3u8Progress.addEventListener('input',()=>{m3u8Seeking=true;if(Number.isFinite(m3u8Video.duration)&&m3u8Video.duration>0){document.getElementById('m3u8Current').textContent=formatM3U8Time((m3u8Progress.value/1000)*m3u8Video.duration);}});
m3u8Progress.addEventListener('change',()=>{if(Number.isFinite(m3u8Video.duration)&&m3u8Video.duration>0){m3u8Video.currentTime=(m3u8Progress.value/1000)*m3u8Video.duration;}m3u8Seeking=false;showM3U8Controls();});
document.getElementById('m3u8Volume').addEventListener('input',event=>{m3u8Video.volume=parseFloat(event.target.value);showM3U8Controls();});
document.getElementById('m3u8Url').addEventListener('keydown',event=>{if(event.key==='Enter')loadM3U8();});
showM3U8Controls();`
	default:
		return ""
	}
}

func imageLandingScript() string {
	return `let selectedFile=null,selectedImage=null,outputType='image/jpeg',outputLabel='JPG',resizeMode='contain';
function setStatus(msg){const el=document.getElementById('status');if(el)el.textContent=msg;}
function downloadBlob(blob,filename){const url=URL.createObjectURL(blob);const a=document.createElement('a');a.download=filename;a.href=url;a.click();URL.revokeObjectURL(url);}
function isHEICFile(file){const name=(file&&file.name||'').toLowerCase();const type=(file&&file.type||'').toLowerCase();return type==='image/heic'||type==='image/heif'||name.endsWith('.heic')||name.endsWith('.heif');}
function canLoadImageFile(file){return !!file&&(file.type.startsWith('image/')||isHEICFile(file));}
function drawLoadedImage(file,img,previewURL,note){selectedImage=img;const canvas=document.getElementById('imageCanvas');canvas.width=img.naturalWidth;canvas.height=img.naturalHeight;canvas.getContext('2d').drawImage(img,0,0);document.getElementById('imageInfo').textContent=file.name+' · '+(file.size/1024).toFixed(1)+' KB'+(note? ' · '+note:'');if(previewURL)URL.revokeObjectURL(previewURL);}
async function loadImageFile(file){if(!canLoadImageFile(file)){setStatus('Please choose a HEIC, HEIF, JPG, PNG or WebP image');return false;}selectedFile=file;selectedImage=null;setStatus('Loading image locally...');try{let sourceBlob=file,note='';if(isHEICFile(file)){if(!window.heic2any){setStatus('HEIC support is still loading. Try again in a moment.');return false;}setStatus('Converting HEIC preview locally...');sourceBlob=await heic2any({blob:file,toType:'image/jpeg',quality:.92});if(Array.isArray(sourceBlob))sourceBlob=sourceBlob[0];note='HEIC decoded locally';}const img=new Image();const previewURL=URL.createObjectURL(sourceBlob);return await new Promise(resolve=>{img.onload=()=>{drawLoadedImage(file,img,previewURL,note);setStatus(isHEICFile(file)?'HEIC loaded. Choose JPG and click Convert image.':'Image loaded locally.');resolve(true);};img.onerror=()=>{URL.revokeObjectURL(previewURL);setStatus('This image could not be loaded in the browser.');resolve(false);};img.src=previewURL;});}catch(error){setStatus('Could not decode this image. Try another file or export it from Photos first.');return false;}}
function setTarget(kb){const input=document.getElementById('targetKB');if(input)input.value=kb;}
function handleImageDropKey(event){if(event.key==='Enter'||event.key===' '){event.preventDefault();document.getElementById('imageInput').click();}}
function setupImageDrop(){const drop=document.getElementById('imageDrop');if(!drop)return;['dragenter','dragover'].forEach(name=>drop.addEventListener(name,event=>{event.preventDefault();drop.classList.add('over');}));['dragleave','drop'].forEach(name=>drop.addEventListener(name,event=>{event.preventDefault();drop.classList.remove('over');}));drop.addEventListener('drop',event=>loadImageFile(event.dataTransfer.files&&event.dataTransfer.files[0]));}
function setFormat(type,label,el){outputType=type;outputLabel=label;document.querySelectorAll('.chip').forEach(b=>b.classList.remove('on'));el.classList.add('on');}
function setResizeMode(mode,el){resizeMode=mode;document.querySelectorAll('.chip').forEach(b=>b.classList.remove('on'));el.classList.add('on');}
function imageToBlob(canvas,type,quality){return new Promise(resolve=>canvas.toBlob(resolve,type,quality));}
async function compressImage(){if(!selectedFile||!selectedImage){setStatus('Please choose an image first');return;}const targetBytes=(parseFloat(document.getElementById('targetKB').value)||200)*1024;const canvas=document.getElementById('imageCanvas');let low=.02,high=.95,best=null;for(let i=0;i<10;i++){const q=(low+high)/2;const blob=await imageToBlob(canvas,'image/jpeg',q);if(blob.size<=targetBytes){best=blob;low=q;}else{high=q;}}if(!best)best=await imageToBlob(canvas,'image/jpeg',.02);downloadBlob(best,'compressed_'+selectedFile.name.replace(/\.[^.]+$/,'')+'.jpg');setStatus('Compressed to '+(best.size/1024).toFixed(1)+' KB');}
async function convertImage(){if(!selectedFile||!selectedImage){setStatus('Please choose an image first');return;}const canvas=document.getElementById('imageCanvas');const ctx=canvas.getContext('2d');if(outputType==='image/jpeg'){ctx.globalCompositeOperation='destination-over';ctx.fillStyle='#fff';ctx.fillRect(0,0,canvas.width,canvas.height);ctx.globalCompositeOperation='source-over';}const blob=await imageToBlob(canvas,outputType,.92);const ext=outputType==='image/png'?'.png':outputType==='image/webp'?'.webp':'.jpg';downloadBlob(blob,'converted_'+selectedFile.name.replace(/\.[^.]+$/,'')+ext);setStatus('Converted to '+outputLabel+' · '+(blob.size/1024).toFixed(1)+' KB');}
async function resizeImage(){if(!selectedFile||!selectedImage){setStatus('Please choose an image first');return;}const width=parseInt(document.getElementById('resizeW').value,10),height=parseInt(document.getElementById('resizeH').value,10);if(!width||!height){setStatus('Enter a valid width and height');return;}const canvas=document.getElementById('imageCanvas');canvas.width=width;canvas.height=height;const ctx=canvas.getContext('2d');ctx.fillStyle='#fff';ctx.fillRect(0,0,width,height);let sx=0,sy=0,sw=selectedImage.naturalWidth,sh=selectedImage.naturalHeight,dx=0,dy=0,dw=width,dh=height;if(resizeMode==='contain'){const scale=Math.min(width/sw,height/sh);dw=sw*scale;dh=sh*scale;dx=(width-dw)/2;dy=(height-dh)/2;}else if(resizeMode==='cover'){const target=width/height,ratio=sw/sh;if(ratio>target){sw=sh*target;sx=(selectedImage.naturalWidth-sw)/2;}else{sh=sw/target;sy=(selectedImage.naturalHeight-sh)/2;}}ctx.drawImage(selectedImage,sx,sy,sw,sh,dx,dy,dw,dh);const blob=await imageToBlob(canvas,'image/jpeg',.92);downloadBlob(blob,'resized_'+selectedFile.name.replace(/\.[^.]+$/,'')+'_'+width+'x'+height+'.jpg');setStatus('Resized to '+width+'x'+height+' · '+(blob.size/1024).toFixed(1)+' KB');}
setupImageDrop();`
}

func exifLandingScript() string {
	return `const originalLoadImageFile=loadImageFile;
loadImageFile=async function(file){const loaded=await originalLoadImageFile(file);if(loaded)loadEXIFMetadata();return loaded;};
function formatEXIFValue(value){if(value===null||value===undefined)return '';if(value instanceof Date)return value.toISOString();if(Array.isArray(value))return value.map(formatEXIFValue).join(', ');if(typeof value==='object'){if('description' in value)return value.description;try{return JSON.stringify(value);}catch(error){return String(value);}}return String(value);}
async function loadEXIFMetadata(){const output=document.getElementById('exifOutput');if(!selectedFile){output.textContent='Choose a photo to inspect EXIF metadata.';return;}if(isHEICFile(selectedFile)){output.textContent='HEIC preview can be converted locally. EXIF reading depends on browser support for the original file.';}if(!window.exifr){output.textContent='EXIF library is still loading. Try again in a moment.';return;}try{const data=await exifr.parse(selectedFile,{tiff:true,ifd0:true,exif:true,gps:true,interop:true,iptc:true,xmp:true});if(!data||!Object.keys(data).length){output.textContent='No readable EXIF metadata found. Some apps remove metadata before export.';return;}const preferred=['Make','Model','LensModel','DateTimeOriginal','CreateDate','ModifyDate','ExposureTime','FNumber','ISO','FocalLength','GPSLatitude','GPSLongitude','Software','Artist','Copyright'];const rows=[];preferred.forEach(key=>{if(data[key]!==undefined)rows.push(key+': '+formatEXIFValue(data[key]));});Object.keys(data).sort().forEach(key=>{if(!preferred.includes(key))rows.push(key+': '+formatEXIFValue(data[key]));});output.textContent=rows.join('\n');setStatus('EXIF metadata loaded locally.');}catch(error){output.textContent='Could not read EXIF metadata from this file.';}}
async function downloadWithoutMetadata(){if(!selectedFile||!selectedImage){setStatus('Choose a photo first.');return;}const canvas=document.getElementById('imageCanvas');const ctx=canvas.getContext('2d');ctx.globalCompositeOperation='destination-over';ctx.fillStyle='#fff';ctx.fillRect(0,0,canvas.width,canvas.height);ctx.globalCompositeOperation='source-over';const blob=await imageToBlob(canvas,'image/jpeg',.92);downloadBlob(blob,'metadata_removed_'+selectedFile.name.replace(/\.[^.]+$/,'')+'.jpg');setStatus('Downloaded a clean JPG. Canvas export removes embedded EXIF metadata.');}`
}

func watermarkLandingScript() string {
	return `const originalLoadImageFile=loadImageFile;
loadImageFile=async function(file){const loaded=await originalLoadImageFile(file);if(loaded)renderWatermarkPreview();return loaded;};
function watermarkPoint(canvas,textWidth,size){const pos=document.getElementById('watermarkPosition').value;const pad=Math.max(18,Math.round(size*.6));if(pos==='top-left')return {x:pad,y:pad+size};if(pos==='top-right')return {x:canvas.width-pad-textWidth,y:pad+size};if(pos==='bottom-left')return {x:pad,y:canvas.height-pad};if(pos==='center')return {x:(canvas.width-textWidth)/2,y:canvas.height/2};return {x:canvas.width-pad-textWidth,y:canvas.height-pad};}
function renderWatermarkPreview(){if(!selectedImage)return;const canvas=document.getElementById('imageCanvas');canvas.width=selectedImage.naturalWidth;canvas.height=selectedImage.naturalHeight;const ctx=canvas.getContext('2d');ctx.drawImage(selectedImage,0,0);const text=(document.getElementById('watermarkText').value||'onlinebox.site').trim();if(!text)return;const size=parseInt(document.getElementById('watermarkSize').value,10)||42;const opacity=parseFloat(document.getElementById('watermarkOpacity').value)||.55;ctx.save();ctx.globalAlpha=opacity;ctx.font='800 '+size+'px sans-serif';ctx.textBaseline='alphabetic';const width=ctx.measureText(text).width;const point=watermarkPoint(canvas,width,size);ctx.lineWidth=Math.max(3,Math.round(size/12));ctx.strokeStyle='rgba(0,0,0,.72)';ctx.fillStyle='#ffffff';ctx.strokeText(text,point.x,point.y);ctx.fillText(text,point.x,point.y);ctx.restore();setStatus('Watermark preview updated locally.');}
async function downloadWatermarkedImage(){if(!selectedFile||!selectedImage){setStatus('Choose an image first.');return;}renderWatermarkPreview();const canvas=document.getElementById('imageCanvas');const blob=await imageToBlob(canvas,'image/jpeg',.92);downloadBlob(blob,'watermarked_'+selectedFile.name.replace(/\.[^.]+$/,'')+'.jpg');setStatus('Downloaded watermarked JPG.');}`
}

func batchImageLandingScript() string {
	return `let batchFiles=[];
function setBatchStatus(msg){document.getElementById('batchStatus').textContent=msg;}
function downloadBlob(blob,filename){const url=URL.createObjectURL(blob);const a=document.createElement('a');a.download=filename;a.href=url;a.click();URL.revokeObjectURL(url);}
function imageToBlob(canvas,type,quality){return new Promise(resolve=>canvas.toBlob(resolve,type,quality));}
function renderBatchList(){const list=document.getElementById('batchList');list.textContent='';batchFiles.slice(0,12).forEach(file=>{const row=document.createElement('div');row.className='file-pill';const name=document.createElement('span');name.textContent=file.name;const size=document.createElement('span');size.textContent=(file.size/1024).toFixed(1)+' KB';row.append(name,size);list.appendChild(row);});if(batchFiles.length>12){const more=document.createElement('div');more.className='file-pill';more.textContent='+'+(batchFiles.length-12)+' more image(s) selected';list.appendChild(more);}}
function loadBatchFiles(files){batchFiles=Array.from(files||[]).filter(file=>file.type&&file.type.startsWith('image/'));document.getElementById('batchInfo').textContent=batchFiles.length?batchFiles.length+' image(s) selected. They will be processed locally.':'No images selected yet. Choose multiple JPG, PNG or WebP images.';document.getElementById('batchOutput').textContent='';setBatchStatus('');renderBatchList();}
function handleBatchDropKey(event){if(event.key==='Enter'||event.key===' '){event.preventDefault();document.getElementById('batchInput').click();}}
function setupBatchDrop(){const drop=document.getElementById('batchDrop');if(!drop)return;['dragenter','dragover'].forEach(name=>drop.addEventListener(name,event=>{event.preventDefault();drop.classList.add('over');}));['dragleave','drop'].forEach(name=>drop.addEventListener(name,event=>{event.preventDefault();drop.classList.remove('over');}));drop.addEventListener('drop',event=>loadBatchFiles(event.dataTransfer.files));}
function setBatchTarget(kb){document.getElementById('batchTargetKB').value=kb;}
function loadImage(file){return new Promise((resolve,reject)=>{const img=new Image();const url=URL.createObjectURL(file);img.onload=()=>{URL.revokeObjectURL(url);resolve(img);};img.onerror=()=>{URL.revokeObjectURL(url);reject(new Error('Could not load '+file.name));};img.src=url;});}
async function compressFileToTarget(file,targetBytes){const img=await loadImage(file);const canvas=document.createElement('canvas');canvas.width=img.naturalWidth;canvas.height=img.naturalHeight;const ctx=canvas.getContext('2d');ctx.drawImage(img,0,0);let low=.02,high=.95,best=null;for(let i=0;i<10;i++){const q=(low+high)/2;const blob=await imageToBlob(canvas,'image/jpeg',q);if(blob.size<=targetBytes){best=blob;low=q;}else{high=q;}}if(!best)best=await imageToBlob(canvas,'image/jpeg',.02);return best;}
async function compressBatchImages(){if(!batchFiles.length){setBatchStatus('Choose images first');return;}const targetKB=parseFloat(document.getElementById('batchTargetKB').value)||200;if(targetKB<=0){setBatchStatus('Enter a valid target size');return;}const targetBytes=targetKB*1024;const output=document.getElementById('batchOutput');output.textContent='';let done=0;for(const file of batchFiles){setBatchStatus('Compressing '+(done+1)+' of '+batchFiles.length+'...');try{const blob=await compressFileToTarget(file,targetBytes);const base=file.name.replace(/\.[^.]+$/,'');downloadBlob(blob,'compressed_'+base+'.jpg');done++;output.textContent+=file.name+' -> '+(blob.size/1024).toFixed(1)+' KB\n';}catch(err){output.textContent+=file.name+' -> failed\n';}}setBatchStatus('Finished '+done+' of '+batchFiles.length+' image(s).');}
setupBatchDrop();`
}

func trustContentHTML(path string) string {
	switch path {
	case "/about":
		return `<h2>What OnlineBox does</h2>
<p>OnlineBox collects practical browser tools for everyday image, data and creator work. The goal is simple: open a page, complete one task, download or copy the result, and move on.</p>
<h2>Why local browser processing matters</h2>
<p>Many OnlineBox tools use browser APIs for the core operation, including image compression, image conversion, resizing, JSON formatting, CSV conversion and Markdown preview. That keeps small tasks fast and reduces unnecessary file uploads.</p>
<h2>Who it is for</h2>
<p>The tools are built for developers, operators, creators, students, small business owners and anyone who needs a lightweight utility without installing software.</p>
<h2>Editorial approach</h2>
<p>Each tool page focuses on one job, explains the expected input and output, and links to related tools when another workflow is a better fit.</p>`
	case "/contact":
		return `<h2>Feedback and bug reports</h2>
<p>If a tool does not behave as expected, include the page URL, browser name, operating system, what you tried to do and what happened. Do not send sensitive files or private data when reporting a problem.</p>
<h2>Tool requests</h2>
<p>OnlineBox is focused on small browser-first utilities. Good requests usually describe the input, the output and the situation where the tool would save time.</p>
<h2>Privacy questions</h2>
<p>For questions about local processing, analytics, advertising or cookies, start with the Privacy Policy and include the specific page or feature you are asking about.</p>
<h2>Contact channel</h2>
<p>Email: <a href="mailto:hello@onlinebox.site">hello@onlinebox.site</a></p>`
	case "/terms":
		return `<h2>Using the tools</h2>
<p>OnlineBox provides browser-based utilities for general productivity tasks. You are responsible for checking that each output is suitable for your use case before submitting it to another service, publishing it or relying on it.</p>
<h2>Acceptable use</h2>
<p>Do not use OnlineBox to attack, overload, reverse engineer or interfere with the service. Do not use the tools to process content that you do not have the right to use.</p>
<h2>Local processing and third parties</h2>
<p>Many tools run locally in your browser, but pages may still load third-party libraries, analytics, fonts or advertising scripts. See the Privacy Policy for more detail.</p>
<h2>No professional advice</h2>
<p>OnlineBox is a utility site. It does not provide legal, financial, medical or professional advice.</p>
<h2>Availability</h2>
<p>The site may change, add or remove tools at any time. Free tools are provided as available and may have browser-specific limitations.</p>`
	default:
		return `<p>OnlineBox provides free browser tools for images, data and creators.</p>`
	}
}

const landingHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>__PAGE_TITLE__</title>
<meta name="description" content="__PAGE_DESCRIPTION__">
__SEO_META__
<link rel="canonical" href="__CANONICAL_URL__">
<link rel="icon" href="/favicon.svg" type="image/svg+xml">
<link rel="shortcut icon" href="/favicon.ico">
<link href="https://fonts.googleapis.com/css2?family=Syne:wght@400;700;800&family=DM+Sans:wght@400;500;700&display=swap" rel="stylesheet">
__GOOGLE_ANALYTICS__
__JSON_LD__
__QR_SCRIPT__
__MARKDOWN_CSS__
<style>
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
:root{--bg:#0e0e11;--surface:#18181d;--surface2:#222228;--border:rgba(255,255,255,.08);--accent:#d4ff57;--accent-dim:rgba(212,255,87,.1);--text:#f2f2f2;--muted:#8c8c8c}
body{font-family:'DM Sans',sans-serif;background:var(--bg);color:var(--text);min-height:100vh}
body::before{content:'';position:fixed;inset:0;background-image:linear-gradient(rgba(255,255,255,.025) 1px,transparent 1px),linear-gradient(90deg,rgba(255,255,255,.025) 1px,transparent 1px);background-size:48px 48px;pointer-events:none}
.wrap{position:relative;z-index:1;max-width:760px;margin:0 auto;padding:48px 22px 72px}
.top{display:flex;justify-content:space-between;gap:16px;align-items:center;margin-bottom:44px}
.brand{color:var(--accent);font-family:'Syne',sans-serif;font-weight:800;text-decoration:none}
.home{color:var(--muted);font-size:13px;text-decoration:none}
.badge{display:inline-flex;color:var(--accent);background:var(--accent-dim);border:1px solid rgba(212,255,87,.24);border-radius:999px;padding:6px 11px;font-size:11px;font-weight:800;letter-spacing:.08em;text-transform:uppercase;margin-bottom:18px}
h1{font-family:'Syne',sans-serif;font-size:clamp(34px,7vw,58px);line-height:1.04;margin-bottom:16px}
h1 em{display:block;color:var(--accent);font-style:normal}
.intro{color:var(--muted);font-size:16px;line-height:1.7;max-width:620px;margin-bottom:26px}
.tool-panel{background:var(--surface);border:1px solid var(--border);border-radius:16px;padding:24px;margin:26px 0}
label{display:grid;gap:8px;color:var(--muted);font-size:12px;font-weight:800;letter-spacing:.06em;text-transform:uppercase;margin:12px 0}
textarea,input,select{width:100%;background:var(--surface2);border:1px solid var(--border);border-radius:10px;color:var(--text);padding:13px 14px;font:inherit;outline:none}
textarea{min-height:170px;resize:vertical;line-height:1.55}
textarea:focus,input:focus,select:focus{border-color:var(--accent)}
.grid{display:grid;gap:12px}.two{grid-template-columns:1fr 1fr}.unit{grid-template-columns:1fr auto;align-items:center}
.file-input-hidden{display:none}
.file-drop{border:1.5px dashed rgba(255,255,255,.16);background:linear-gradient(135deg,rgba(255,255,255,.04),rgba(255,255,255,.02));border-radius:14px;padding:22px;margin:10px 0 12px;cursor:pointer;display:grid;grid-template-columns:auto 1fr;gap:8px 14px;align-items:center;transition:border-color .16s,background .16s,transform .16s}
.file-drop:hover,.file-drop:focus-visible,.file-drop.over{border-color:var(--accent);background:var(--accent-dim);outline:0;transform:translateY(-1px)}
.file-drop-icon{grid-row:1/3;width:44px;height:44px;border-radius:12px;background:var(--accent);color:#0e0e11;display:flex;align-items:center;justify-content:center;font-family:'Syne',sans-serif;font-size:28px;font-weight:800;line-height:1}
.file-drop strong{font-family:'Syne',sans-serif;color:var(--text);font-size:19px;line-height:1.2}
.file-drop span{color:var(--muted);line-height:1.55;font-size:13px}
.file-list{display:grid;gap:8px;margin-top:10px}
.file-pill{display:flex;justify-content:space-between;gap:12px;background:var(--surface2);border:1px solid var(--border);border-radius:10px;padding:10px 12px;color:var(--text);font-size:13px;line-height:1.4}
.file-pill span:first-child{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.file-pill span:last-child{color:var(--muted);white-space:nowrap}
.btn{display:inline-flex;justify-content:center;width:100%;background:var(--accent);color:#0e0e11;border:0;border-radius:12px;padding:15px 18px;margin-top:14px;font-family:'Syne',sans-serif;font-weight:800;cursor:pointer;text-decoration:none}
.btn.compact,.ghost.compact{width:auto;margin-top:0;min-height:42px;align-items:center}
.ghost,.chip{border:1px solid var(--border);background:transparent;color:var(--text);border-radius:10px;padding:10px 12px;font-weight:800;cursor:pointer;margin-top:10px}
.choices{display:flex;gap:8px;flex-wrap:wrap}.chip.on{border-color:var(--accent);color:var(--accent);background:var(--accent-dim)}
.output{background:var(--surface2);border:1px solid var(--border);border-radius:12px;min-height:170px;padding:16px;margin-top:16px;white-space:pre-wrap;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:13px;line-height:1.55}
.markdown-tool{width:min(1180px,calc(100vw - 44px));margin-left:50%;transform:translateX(-50%)}
.markdown-toolbar{display:grid;grid-template-columns:140px 150px minmax(180px,1fr) auto;gap:12px;align-items:end;margin-bottom:12px}
.toolbar-group{display:grid;gap:6px}.toolbar-group label,.markdown-pane label,.pane-label{margin:0;color:var(--muted);font-size:11px;font-weight:800;letter-spacing:.08em;text-transform:uppercase}.title-field input{height:42px}
.markdown-custom-panel{display:grid;grid-template-columns:repeat(auto-fit,minmax(118px,1fr));gap:10px;align-items:end;margin:-2px 0 12px;padding:10px;border:1px solid var(--border);border-radius:12px;background:rgba(255,255,255,.035)}
.markdown-custom-panel[hidden]{display:none}.markdown-custom-panel label{display:grid;gap:6px;margin:0;color:var(--muted);font-size:11px;font-weight:800;letter-spacing:.06em;text-transform:uppercase}.markdown-custom-panel span{color:var(--accent);font-size:12px}.markdown-custom-panel input[type=range]{width:100%;accent-color:var(--accent)}.markdown-custom-panel input[type=color]{width:100%;height:38px;padding:4px;border-radius:10px;background:var(--surface2);border:1px solid var(--border)}
.markdown-actions{display:flex;gap:10px;align-items:center;flex-wrap:wrap;margin-bottom:14px}.markdown-stat{color:var(--muted);font-size:12px;font-weight:800;margin-left:auto}.markdown-stat+ .markdown-stat{margin-left:0}
.markdown-workbench{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:14px;min-height:calc(100vh - 360px)}
.markdown-workbench.editor{grid-template-columns:1fr}.markdown-workbench.preview{grid-template-columns:1fr}.markdown-workbench.editor .preview-pane,.markdown-workbench.preview .editor-pane{display:none}
.markdown-pane{min-width:0;display:flex;flex-direction:column;gap:8px}.markdown-pane textarea{min-height:560px;height:calc(100vh - 430px);font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:13px;line-height:1.62}
.markdown-preview{min-height:560px;height:calc(100vh - 430px);overflow:auto;background:#fff;color:#1f2328;color-scheme:light;white-space:normal;margin-top:0}.preview.markdown-body[data-style="Academic"]{font-family:Georgia,"Times New Roman",serif;line-height:1.8}.preview.markdown-body[data-style="Academic"] h1,.preview.markdown-body[data-style="Academic"] h2,.preview.markdown-body[data-style="Academic"] h3{font-family:Georgia,"Times New Roman",serif}.preview.markdown-body[data-style="Minimal"]{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;border-color:#e5e7eb;box-shadow:none}.preview.markdown-body[data-style="Minimal"] h1,.preview.markdown-body[data-style="Minimal"] h2{border-bottom:0}.preview.markdown-body[data-style="Minimal"] blockquote{border-left:0;background:transparent;padding-left:0}.preview.markdown-body[data-style="Minimal"] table,.preview.markdown-body[data-style="Minimal"] th,.preview.markdown-body[data-style="Minimal"] td{border-color:transparent}.preview.markdown-body[data-style="Custom"]{font-family:var(--md-font-family);font-size:var(--md-font-size);line-height:var(--md-line-height)}.preview.markdown-body[data-style="Custom"] h1,.preview.markdown-body[data-style="Custom"] h2,.preview.markdown-body[data-style="Custom"] h3{font-family:inherit;color:var(--md-title-color)}
.json-tool{width:min(1120px,calc(100vw - 44px));margin-left:50%;transform:translateX(-50%)}
.json-toolbar{display:flex;gap:10px;flex-wrap:wrap;align-items:center;margin-bottom:12px}
.json-status{border:1px solid var(--border);background:var(--surface2);border-radius:10px;padding:11px 12px;color:var(--muted);font-size:13px;line-height:1.5;margin-bottom:14px}
.json-status.valid{border-color:rgba(212,255,87,.26);color:var(--accent);background:var(--accent-dim)}.json-status.invalid{border-color:rgba(255,105,105,.42);color:#ff9a9a;background:rgba(255,105,105,.08)}
.json-columns{display:grid;grid-template-columns:1fr 1fr;gap:16px}.json-area{min-height:430px;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:13px;line-height:1.58;tab-size:2}.output-area{background:#141418;color:#f4f4f4}
.pdf-tool .file-drop-icon{font-size:13px;letter-spacing:.04em}.pdf-status{margin-top:12px;border:1px solid var(--border);background:var(--surface2);border-radius:12px;padding:12px 14px;color:var(--muted);font-size:13px;line-height:1.5}.pdf-status.loading{border-color:rgba(212,255,87,.3);color:var(--accent);background:var(--accent-dim)}.pdf-status.success{border-color:rgba(212,255,87,.3);color:var(--accent)}.pdf-status.error{border-color:rgba(255,105,105,.42);color:#ff9a9a;background:rgba(255,105,105,.08)}.pdf-tool button:disabled{cursor:wait;opacity:.72}
.zip-tool .file-drop-icon{font-size:13px;letter-spacing:.04em}.zip-actions{display:flex;gap:10px;flex-wrap:wrap;margin:12px 0}.zip-options{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px;margin-top:8px}.zip-options label{margin:0}.check-row{display:flex;align-items:center;gap:10px;background:var(--surface2);border:1px solid var(--border);border-radius:10px;padding:12px 14px;color:var(--text);letter-spacing:0;text-transform:none}.check-row input{width:auto}.zip-summary{border:1px solid var(--border);background:var(--surface2);border-radius:12px;padding:12px 14px;color:var(--muted);font-size:13px;line-height:1.5;margin-top:14px}.zip-analysis{display:grid;grid-template-columns:repeat(4,1fr);gap:8px;margin-top:10px}.zip-analysis span,.zip-analysis small{background:rgba(255,255,255,.04);border:1px solid var(--border);border-radius:10px;padding:9px 10px;color:var(--muted);font-size:12px;line-height:1.4}.zip-analysis small{grid-column:1/-1}.zip-tool button:disabled{cursor:wait;opacity:.72}
.preview{font-family:'DM Sans',sans-serif}.preview h1,.preview h2,.preview h3{font-family:'Syne',sans-serif;margin:0 0 10px}.preview h3{font-size:19px;color:var(--accent)}.preview p,.preview li{line-height:1.65;margin-bottom:8px}.preview ul,.preview ol{padding-left:22px;margin:10px 0 14px}.preview blockquote{border-left:3px solid var(--accent);background:rgba(212,255,87,.08);margin:14px 0;padding:10px 14px;color:var(--text)}.preview pre{background:#101114;border:1px solid var(--border);border-radius:10px;padding:14px;overflow:auto;margin:14px 0}.preview code{background:rgba(255,255,255,.08);border-radius:5px;padding:2px 5px}.preview pre code{background:transparent;padding:0}.preview hr{border:0;border-top:1px solid var(--border);margin:20px 0}.table-wrap{overflow:auto;margin:14px 0}.preview table{width:100%;border-collapse:collapse;min-width:520px}.preview th,.preview td{border:1px solid var(--border);padding:9px 10px;text-align:left}.preview th{color:var(--text);background:rgba(255,255,255,.05)}
.preview.markdown-body{background:#fff;color:#1f2328;color-scheme:light;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;white-space:normal;--color-canvas-default:#fff;--color-canvas-subtle:#f6f8fa;--color-fg-default:#1f2328;--color-fg-muted:#59636e;--color-border-default:#d0d7de;--color-border-muted:#d8dee4}.preview.markdown-body h1,.preview.markdown-body h2,.preview.markdown-body h3{font-family:inherit;color:#1f2328}.preview.markdown-body h3{font-size:1.25em}.preview.markdown-body blockquote{background:transparent;color:#59636e;border-left-color:#d0d7de}.preview.markdown-body pre{background:#f6f8fa;border:0;color:#1f2328}.preview.markdown-body code{background:rgba(175,184,193,.2);color:#1f2328}.preview.markdown-body pre code{background:transparent;color:#1f2328}.preview.markdown-body table{display:table;width:100%;background:#fff;color:#1f2328}.preview.markdown-body th{background:#f6f8fa;color:#1f2328}.preview.markdown-body td{background:#fff;color:#1f2328}
.canvas-wrap{display:flex;justify-content:center;background:var(--surface2);border:1px solid var(--border);border-radius:12px;padding:16px;margin-top:16px;overflow:auto}
.canvas-wrap canvas{max-width:100%;height:auto}.wide canvas{width:100%;max-width:520px}.gradient-preview{height:210px;border-radius:12px;border:1px solid var(--border);margin-top:16px}
.m3u8-tool{display:grid;gap:14px}.m3u8-input-row{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:10px;align-items:stretch}.m3u8-input-row input{min-width:0}.m3u8-play-btn{width:auto;margin-top:0;min-width:112px;align-items:center}
.m3u8-player{position:relative;width:100%;aspect-ratio:16/9;background:#000;border-radius:8px;box-shadow:0 16px 38px rgba(0,0,0,.38);overflow:hidden;border:1px solid var(--border)}
.m3u8-player video{width:100%;height:100%;display:block;background:#000;object-fit:contain}
.m3u8-controls{position:absolute;left:0;right:0;bottom:0;display:grid;grid-template-columns:auto auto minmax(90px,1fr) auto minmax(70px,90px) auto auto;gap:8px;align-items:center;padding:28px 12px 12px;background:linear-gradient(to top,rgba(0,0,0,.88),rgba(0,0,0,.58) 58%,transparent);opacity:0;transform:translateY(8px);pointer-events:none;transition:opacity .18s,transform .18s}
.m3u8-player.controls-visible .m3u8-controls,.m3u8-player:hover .m3u8-controls{opacity:1;transform:translateY(0);pointer-events:auto}
.player-btn,.speed-btn{border:1px solid rgba(255,255,255,.14);background:rgba(255,255,255,.08);color:#fff;border-radius:8px;padding:8px 10px;font-size:12px;font-weight:800;cursor:pointer;white-space:nowrap}
.player-btn:hover,.speed-btn:hover,.speed-btn.on{border-color:var(--accent);color:var(--accent);background:rgba(212,255,87,.12)}
.m3u8-time{color:#d8d8d8;font-size:12px;font-weight:800;min-width:42px;text-align:center}.m3u8-progress,.m3u8-volume{accent-color:var(--accent);padding:0;border:0;background:transparent}.m3u8-progress{width:100%}.m3u8-volume{width:90px}
.speed-group{display:flex;gap:5px;flex-wrap:wrap;justify-content:flex-end}.speed-btn{padding:7px 8px}
.hint{color:var(--muted);font-size:13px;line-height:1.6;margin-top:10px}.pro-kicker{display:inline-flex;color:var(--accent);background:var(--accent-dim);border:1px solid rgba(212,255,87,.24);border-radius:999px;padding:6px 10px;font-size:11px;font-weight:800}
.guide{color:var(--muted);line-height:1.75;margin:30px 0}.guide h2{font-family:'Syne',sans-serif;color:var(--text);font-size:22px;margin:26px 0 10px}.guide p{margin-bottom:12px}.guide ol{padding-left:22px;margin:10px 0 18px}.guide li{margin-bottom:8px}.faq-block{margin-top:24px}.faq-block details{border-top:1px solid var(--border);padding:14px 0}.faq-block summary{color:var(--text);font-weight:800;cursor:pointer}.faq-block p{margin:10px 0 0}
.related{display:grid;grid-template-columns:repeat(2,1fr);gap:10px;margin-top:30px}.related a{background:var(--surface);border:1px solid var(--border);border-radius:10px;color:var(--text);text-decoration:none;padding:12px;font-weight:800}
.site-footer{border-top:1px solid var(--border);margin-top:34px;padding-top:18px;display:flex;justify-content:space-between;gap:12px;flex-wrap:wrap;color:var(--muted);font-size:13px}.site-footer a{color:var(--text);text-decoration:none;font-weight:800}
@media(max-width:760px){.json-columns{grid-template-columns:1fr}.json-area{min-height:300px}.json-toolbar .compact{flex:1 1 calc(50% - 8px)}}
@media(max-width:760px){.markdown-toolbar,.markdown-workbench{grid-template-columns:1fr}.markdown-workbench{min-height:auto}.markdown-pane textarea,.markdown-preview{height:auto;min-height:420px}.markdown-stat{margin-left:0}}
@media(max-width:620px){.two,.related,.zip-options,.zip-analysis{grid-template-columns:1fr}.wrap{padding-top:30px}.m3u8-input-row{grid-template-columns:1fr}.m3u8-play-btn{width:100%}.m3u8-controls{grid-template-columns:auto 1fr auto;gap:7px}.m3u8-progress{grid-column:1/-1;grid-row:1}#m3u8Current{display:none}.m3u8-volume{width:76px}.speed-group{grid-column:1/-1;justify-content:flex-start}.m3u8-time{min-width:36px}}
</style>
</head>
<body>
<main class="wrap">
<nav class="top"><a class="brand" href="/">OnlineBox</a><a class="home" href="/">All tools</a></nav>
<div class="badge">Focused browser tool</div>
<h1>__PAGE_HEADING__<em>__PAGE_ACCENT__</em></h1>
<p class="intro">__PAGE_INTRO__</p>
__PRIMARY_TOOL__
__GUIDE_CONTENT__
<section class="related" aria-label="Related tools">__RELATED_LINKS__</section>
<footer class="site-footer"><span>OnlineBox</span><a href="/about">About</a><a href="/contact">Contact</a><a href="/terms">Terms</a><a href="/privacy-policy">Privacy Policy</a></footer>
</main>
<script>
__LANDING_SCRIPT__
</script>
</body>
</html>`

const privacyHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>__PAGE_TITLE__</title>
<meta name="description" content="__PAGE_DESCRIPTION__">
__SEO_META__
<link rel="canonical" href="__CANONICAL_URL__">
<link rel="icon" href="/favicon.svg" type="image/svg+xml">
<link rel="shortcut icon" href="/favicon.ico">
<link href="https://fonts.googleapis.com/css2?family=Syne:wght@400;700;800&family=DM+Sans:wght@400;500;700&display=swap" rel="stylesheet">
__GOOGLE_ANALYTICS__
__JSON_LD__
<style>
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
:root{--bg:#0e0e11;--surface:#18181d;--border:rgba(255,255,255,.09);--accent:#d4ff57;--text:#f2f2f2;--muted:#9b9b9b}
body{font-family:'DM Sans',sans-serif;background:var(--bg);color:var(--text);min-height:100vh}
body::before{content:'';position:fixed;inset:0;background-image:linear-gradient(rgba(255,255,255,.025) 1px,transparent 1px),linear-gradient(90deg,rgba(255,255,255,.025) 1px,transparent 1px);background-size:48px 48px;pointer-events:none}
.wrap{position:relative;z-index:1;max-width:820px;margin:0 auto;padding:48px 22px 72px}
.top{display:flex;justify-content:space-between;gap:16px;align-items:center;margin-bottom:44px}.brand{color:var(--accent);font-family:'Syne',sans-serif;font-weight:800;text-decoration:none}.home{color:var(--muted);font-size:13px;text-decoration:none}
.badge{display:inline-flex;color:var(--accent);background:rgba(212,255,87,.1);border:1px solid rgba(212,255,87,.24);border-radius:999px;padding:6px 11px;font-size:11px;font-weight:800;letter-spacing:.08em;text-transform:uppercase;margin-bottom:18px}
h1{font-family:'Syne',sans-serif;font-size:clamp(36px,7vw,62px);line-height:1.04;margin-bottom:16px}h1 em{display:block;color:var(--accent);font-style:normal}
.intro{color:var(--muted);font-size:16px;line-height:1.7;max-width:680px;margin-bottom:28px}
.policy{background:var(--surface);border:1px solid var(--border);border-radius:16px;padding:24px;color:var(--muted);line-height:1.75}.policy h2{font-family:'Syne',sans-serif;color:var(--text);font-size:23px;margin:24px 0 8px}.policy h2:first-child{margin-top:0}.policy p{margin-bottom:12px}.policy ul{padding-left:22px;margin:8px 0 14px}.policy li{margin-bottom:8px}.policy a{color:var(--accent);font-weight:800;text-decoration:none}
.site-footer{border-top:1px solid var(--border);margin-top:34px;padding-top:18px;display:flex;justify-content:space-between;gap:12px;flex-wrap:wrap;color:var(--muted);font-size:13px}.site-footer a{color:var(--text);text-decoration:none;font-weight:800}
</style>
</head>
<body>
<main class="wrap">
<nav class="top"><a class="brand" href="/">OnlineBox</a><a class="home" href="/">All tools</a></nav>
<div class="badge">Legal</div>
<h1>__PAGE_HEADING__<em>__PAGE_ACCENT__</em></h1>
<p class="intro">__PAGE_INTRO__</p>
<section class="policy">
<h2>Overview</h2>
<p>OnlineBox provides browser-based tools for images, data and creator workflows. Many tools are designed to process files locally in your browser, which means the file content is handled by your device for the core operation instead of being uploaded to our server.</p>
<p>Last updated: May 9, 2026.</p>
<h2>Information We Process</h2>
<ul>
<li>Tool inputs such as images, CSV text, Markdown text or QR code content may be processed in your browser to produce the requested output.</li>
<li>Basic technical information such as page views, browser type, approximate region and device information may be collected through analytics tools.</li>
<li>If account, payment or contact features are used in the future, information you submit for those features may be processed to provide the requested service.</li>
</ul>
<h2>Local File Processing</h2>
<p>Image compression, image conversion, resizing, batch compression and similar tools use browser features whenever possible. The server mainly delivers the web page. You should still avoid using any online tool for highly sensitive files unless you are comfortable with the processing environment.</p>
<h2>Analytics</h2>
<p>OnlineBox uses Google Analytics to understand traffic, popular pages and general usage patterns. Google Analytics may use cookies or similar technologies to collect aggregated reporting data. You can learn more from Google's page about <a href="https://policies.google.com/technologies/partner-sites">how Google uses information from sites or apps that use its services</a>.</p>
<h2>Advertising and Cookies</h2>
<p>OnlineBox may display ads served by Google or other advertising partners. These partners may use cookies, advertising identifiers or similar technologies to serve ads, measure ad performance and help prevent fraud.</p>
<p>Google's use of advertising cookies enables it and its partners to serve ads based on visits to OnlineBox and other sites. Users may opt out of personalized advertising by visiting <a href="https://adssettings.google.com/">Google Ads Settings</a>. You may also manage cookies in your browser settings.</p>
<h2>Third-Party Services</h2>
<p>Some pages may load third-party scripts, fonts, analytics, advertising or utility libraries. These third parties may process data according to their own privacy policies.</p>
<h2>Data Retention</h2>
<p>We keep analytics and operational records only as long as needed to understand site performance, maintain the service, comply with obligations and improve the tools.</p>
<h2>Changes to This Policy</h2>
<p>We may update this Privacy Policy when tools, analytics, advertising or legal requirements change. The updated version will be posted on this page.</p>
</section>
<footer class="site-footer"><span>OnlineBox</span><a href="/about">About</a><a href="/contact">Contact</a><a href="/terms">Terms</a><a href="/privacy-policy">Privacy Policy</a></footer>
</main>
</body>
</html>`

const trustHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>__PAGE_TITLE__</title>
<meta name="description" content="__PAGE_DESCRIPTION__">
__SEO_META__
<link rel="canonical" href="__CANONICAL_URL__">
<link rel="icon" href="/favicon.svg" type="image/svg+xml">
<link rel="shortcut icon" href="/favicon.ico">
<link href="https://fonts.googleapis.com/css2?family=Syne:wght@400;700;800&family=DM+Sans:wght@400;500;700&display=swap" rel="stylesheet">
__GOOGLE_ANALYTICS__
__JSON_LD__
<style>
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
:root{--bg:#0e0e11;--surface:#18181d;--border:rgba(255,255,255,.09);--accent:#d4ff57;--text:#f2f2f2;--muted:#9b9b9b}
body{font-family:'DM Sans',sans-serif;background:var(--bg);color:var(--text);min-height:100vh}
body::before{content:'';position:fixed;inset:0;background-image:linear-gradient(rgba(255,255,255,.025) 1px,transparent 1px),linear-gradient(90deg,rgba(255,255,255,.025) 1px,transparent 1px);background-size:48px 48px;pointer-events:none}
.wrap{position:relative;z-index:1;max-width:820px;margin:0 auto;padding:48px 22px 72px}
.top{display:flex;justify-content:space-between;gap:16px;align-items:center;margin-bottom:44px}.brand{color:var(--accent);font-family:'Syne',sans-serif;font-weight:800;text-decoration:none}.home{color:var(--muted);font-size:13px;text-decoration:none}
.badge{display:inline-flex;color:var(--accent);background:rgba(212,255,87,.1);border:1px solid rgba(212,255,87,.24);border-radius:999px;padding:6px 11px;font-size:11px;font-weight:800;letter-spacing:.08em;text-transform:uppercase;margin-bottom:18px}
h1{font-family:'Syne',sans-serif;font-size:clamp(36px,7vw,62px);line-height:1.04;margin-bottom:16px}h1 em{display:block;color:var(--accent);font-style:normal}
.intro{color:var(--muted);font-size:16px;line-height:1.7;max-width:680px;margin-bottom:28px}
.content{background:var(--surface);border:1px solid var(--border);border-radius:16px;padding:24px;color:var(--muted);line-height:1.75}.content h2{font-family:'Syne',sans-serif;color:var(--text);font-size:23px;margin:24px 0 8px}.content h2:first-child{margin-top:0}.content p{margin-bottom:12px}.content a{color:var(--accent);font-weight:800;text-decoration:none}
.site-footer{border-top:1px solid var(--border);margin-top:34px;padding-top:18px;display:flex;justify-content:space-between;gap:12px;flex-wrap:wrap;color:var(--muted);font-size:13px}.site-footer a{color:var(--text);text-decoration:none;font-weight:800}
</style>
</head>
<body>
<main class="wrap">
<nav class="top"><a class="brand" href="/">OnlineBox</a><a class="home" href="/">All tools</a></nav>
<div class="badge">OnlineBox</div>
<h1>__PAGE_HEADING__<em>__PAGE_ACCENT__</em></h1>
<p class="intro">__PAGE_INTRO__</p>
<section class="content">__TRUST_CONTENT__</section>
<footer class="site-footer"><span>OnlineBox</span><a href="/about">About</a><a href="/contact">Contact</a><a href="/terms">Terms</a><a href="/privacy-policy">Privacy Policy</a></footer>
</main>
</body>
</html>`

const blogIndexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>__PAGE_TITLE__</title>
<meta name="description" content="__PAGE_DESCRIPTION__">
__SEO_META__
<link rel="canonical" href="__CANONICAL_URL__">
<link rel="icon" href="/favicon.svg" type="image/svg+xml">
<link rel="shortcut icon" href="/favicon.ico">
<link href="https://fonts.googleapis.com/css2?family=Syne:wght@400;700;800&family=DM+Sans:wght@400;500;700&display=swap" rel="stylesheet">
__GOOGLE_ANALYTICS__
__JSON_LD__
<style>
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
:root{--bg:#0e0e11;--surface:#18181d;--surface2:#202126;--border:rgba(255,255,255,.09);--accent:#d4ff57;--text:#f2f2f2;--muted:#9b9b9b}
body{font-family:'DM Sans',sans-serif;background:var(--bg);color:var(--text);min-height:100vh}
body::before{content:'';position:fixed;inset:0;background-image:linear-gradient(rgba(255,255,255,.025) 1px,transparent 1px),linear-gradient(90deg,rgba(255,255,255,.025) 1px,transparent 1px);background-size:48px 48px;pointer-events:none}
.wrap{position:relative;z-index:1;max-width:900px;margin:0 auto;padding:48px 22px 72px}
.top{display:flex;justify-content:space-between;gap:16px;align-items:center;margin-bottom:46px}.brand{color:var(--accent);font-family:'Syne',sans-serif;font-weight:800;text-decoration:none}.home{color:var(--muted);font-size:13px;text-decoration:none;font-weight:800}
.badge{display:inline-flex;color:var(--accent);background:rgba(212,255,87,.1);border:1px solid rgba(212,255,87,.24);border-radius:999px;padding:6px 11px;font-size:11px;font-weight:800;letter-spacing:.08em;text-transform:uppercase;margin-bottom:18px}
h1{font-family:'Syne',sans-serif;font-size:clamp(42px,8vw,72px);line-height:1.02;margin-bottom:14px}
.intro{color:var(--muted);font-size:16px;line-height:1.7;max-width:620px;margin-bottom:30px}
.post-list{display:grid;gap:14px}.post-card{background:var(--surface);border:1px solid var(--border);border-radius:12px;padding:20px}.post-card time{display:block;color:var(--accent);font-size:12px;font-weight:800;margin-bottom:8px}.post-card h2{font-family:'Syne',sans-serif;font-size:24px;margin-bottom:9px}.post-card a{color:var(--text);text-decoration:none}.post-card a:hover{color:var(--accent)}.post-card p,.empty{color:var(--muted);line-height:1.65}.empty{background:var(--surface);border:1px solid var(--border);border-radius:12px;padding:20px}
.site-footer{border-top:1px solid var(--border);margin-top:34px;padding-top:18px;display:flex;justify-content:space-between;gap:12px;flex-wrap:wrap;color:var(--muted);font-size:13px}.site-footer a{color:var(--text);text-decoration:none;font-weight:800}
</style>
</head>
<body>
<main class="wrap">
<nav class="top"><a class="brand" href="/">OnlineBox</a><a class="home" href="/">All tools</a></nav>
<div class="badge">OnlineBox notes</div>
<h1>Blog</h1>
<p class="intro">Short guides, updates and practical notes for free browser tools.</p>
<section class="post-list">__BLOG_CONTENT__</section>
<footer class="site-footer"><span>OnlineBox</span><a href="/about">About</a><a href="/contact">Contact</a><a href="/terms">Terms</a><a href="/privacy-policy">Privacy Policy</a></footer>
</main>
</body>
</html>`

const blogPostHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>__PAGE_TITLE__</title>
<meta name="description" content="__PAGE_DESCRIPTION__">
__SEO_META__
<link rel="canonical" href="__CANONICAL_URL__">
<link rel="icon" href="/favicon.svg" type="image/svg+xml">
<link rel="shortcut icon" href="/favicon.ico">
<link href="https://fonts.googleapis.com/css2?family=Syne:wght@400;700;800&family=DM+Sans:wght@400;500;700&display=swap" rel="stylesheet">
__GOOGLE_ANALYTICS__
__JSON_LD__
<style>
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
:root{--bg:#0e0e11;--surface:#18181d;--surface2:#202126;--border:rgba(255,255,255,.09);--accent:#d4ff57;--text:#f2f2f2;--muted:#9b9b9b}
body{font-family:'DM Sans',sans-serif;background:var(--bg);color:var(--text);min-height:100vh}
body::before{content:'';position:fixed;inset:0;background-image:linear-gradient(rgba(255,255,255,.025) 1px,transparent 1px),linear-gradient(90deg,rgba(255,255,255,.025) 1px,transparent 1px);background-size:48px 48px;pointer-events:none}
.wrap{position:relative;z-index:1;max-width:820px;margin:0 auto;padding:48px 22px 72px}
.top{display:flex;justify-content:space-between;gap:16px;align-items:center;margin-bottom:44px}.brand{color:var(--accent);font-family:'Syne',sans-serif;font-weight:800;text-decoration:none}.home{color:var(--muted);font-size:13px;text-decoration:none;font-weight:800}
.badge{display:inline-flex;color:var(--accent);background:rgba(212,255,87,.1);border:1px solid rgba(212,255,87,.24);border-radius:999px;padding:6px 11px;font-size:11px;font-weight:800;letter-spacing:.08em;text-transform:uppercase;margin-bottom:18px}
h1{font-family:'Syne',sans-serif;font-size:clamp(36px,7vw,64px);line-height:1.04;margin-bottom:12px}.date{color:var(--accent);font-size:13px;font-weight:800;margin-bottom:20px}.dek{color:var(--muted);font-size:17px;line-height:1.72;max-width:680px;margin-bottom:28px}
.article{background:var(--surface);border:1px solid var(--border);border-radius:12px;padding:24px;color:var(--muted);line-height:1.75}.article h1,.article h2,.article h3{font-family:'Syne',sans-serif;color:var(--text);line-height:1.2;margin:28px 0 10px}.article h1:first-child,.article h2:first-child,.article h3:first-child{margin-top:0}.article h1{font-size:32px}.article h2{font-size:26px}.article h3{font-size:21px}.article p{margin-bottom:14px}.article ul,.article ol{padding-left:22px;margin:10px 0 16px}.article li{margin-bottom:8px}.article blockquote{border-left:3px solid var(--accent);background:rgba(212,255,87,.08);margin:18px 0;padding:12px 15px;color:var(--text)}.article pre{background:#101114;border:1px solid var(--border);border-radius:10px;padding:14px;overflow:auto;margin:16px 0}.article code{background:rgba(255,255,255,.08);border-radius:5px;padding:2px 5px;color:var(--text)}.article pre code{background:transparent;padding:0}.article hr{border:0;border-top:1px solid var(--border);margin:22px 0}.article a{color:var(--accent);font-weight:800;text-decoration:none}.table-wrap{overflow:auto;margin:16px 0}.article table{width:100%;border-collapse:collapse;min-width:520px}.article th,.article td{border:1px solid var(--border);padding:9px 10px;text-align:left}.article th{color:var(--text);background:rgba(255,255,255,.05)}
.more-tools{border-top:1px solid var(--border);margin-top:24px;padding-top:18px;color:var(--muted);font-weight:800}.more-tools a{color:var(--accent);text-decoration:none}
.site-footer{border-top:1px solid var(--border);margin-top:34px;padding-top:18px;display:flex;justify-content:space-between;gap:12px;flex-wrap:wrap;color:var(--muted);font-size:13px}.site-footer a{color:var(--text);text-decoration:none;font-weight:800}
@media(max-width:620px){.article{padding:18px}.article table{min-width:460px}}
</style>
</head>
<body>
<main class="wrap">
<nav class="top"><a class="brand" href="/">OnlineBox</a><a class="home" href="/blog">Blog</a></nav>
<div class="badge">OnlineBox Blog</div>
<article>
<h1>__POST_TITLE__</h1>
<div class="date">__POST_DATE__</div>
<p class="dek">__POST_DESCRIPTION__</p>
<section class="article">__POST_CONTENT__</section>
<p class="more-tools">More free browser tools &rarr; <a href="/">onlinebox.site</a></p>
</article>
<footer class="site-footer"><span>OnlineBox</span><a href="/about">About</a><a href="/contact">Contact</a><a href="/terms">Terms</a><a href="/privacy-policy">Privacy Policy</a></footer>
</main>
</body>
</html>`

func jsString(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(encoded)
}

func handleRobots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "User-agent: *\nAllow: /\n\nSitemap: %s/sitemap.xml\n", siteURL())
}

func handleAdsTXT(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(adsTXT))
}

func handleFavicon(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write([]byte(faviconSVG))
}

type sitemapURLSet struct {
	XMLName xml.Name     `xml:"urlset"`
	Xmlns   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Loc        string `xml:"loc"`
	LastMod    string `xml:"lastmod,omitempty"`
	ChangeFreq string `xml:"changefreq,omitempty"`
	Priority   string `xml:"priority,omitempty"`
}

func handleSitemap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Write([]byte(xml.Header))
	if err := xml.NewEncoder(w).Encode(sitemapURLSet{
		Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  sitemapURLs(),
	}); err != nil {
		log.Printf("sitemap encode error: %v", err)
	}
}

func sitemapURLs() []sitemapURL {
	posts, err := loadBlogPosts()
	if err != nil {
		log.Printf("sitemap blog load error: %v", err)
	}
	urls := make([]sitemapURL, 0, len(publicPages)+1+len(posts))
	lastMod := time.Now().UTC().Format("2006-01-02")
	for _, page := range publicPages {
		priority := "0.8"
		if page.Path == "/" {
			priority = "1.0"
		}
		urls = append(urls, sitemapURL{
			Loc:        siteURL() + page.Path,
			LastMod:    lastMod,
			ChangeFreq: "weekly",
			Priority:   priority,
		})
	}
	urls = append(urls, sitemapURL{
		Loc:        siteURL() + "/blog",
		LastMod:    lastMod,
		ChangeFreq: "weekly",
		Priority:   "0.7",
	})
	for _, post := range posts {
		urls = append(urls, sitemapURL{
			Loc:        siteURL() + "/blog/" + post.Slug,
			LastMod:    post.Date,
			ChangeFreq: "monthly",
			Priority:   "0.6",
		})
	}
	return urls
}

func siteURL() string {
	raw := strings.TrimSpace(os.Getenv("SITE_URL"))
	if raw == "" {
		raw = "https://onlinebox.site"
	}
	return strings.TrimRight(raw, "/")
}

func openDB() (*sql.DB, error) {
	path := os.Getenv("DB_PATH")
	if path == "" {
		path = "data/imgtools.db"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;`); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateDB(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrateDB(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	email TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS licenses (
	email TEXT PRIMARY KEY,
	paid INTEGER NOT NULL DEFAULT 0,
	source TEXT NOT NULL DEFAULT '',
	event_type TEXT NOT NULL DEFAULT '',
	resource_id TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
`)
	return err
}

type authRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type sessionClaims struct {
	UserID int64  `json:"uid"`
	Email  string `json:"email"`
	Iat    int64  `json:"iat"`
	Exp    int64  `json:"exp"`
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method_not_allowed"})
		return
	}
	if !allowAuthRequest(r) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "error": "too_many_requests"})
		return
	}
	req, ok := readAuthRequest(w, r)
	if !ok {
		return
	}
	email := normalizeEmail(req.Email)
	if email == "" || len(email) > 254 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_email"})
		return
	}
	if len(req.Password) < 8 || len(req.Password) > 128 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "weak_password"})
		return
	}

	hash, err := hashPassword(req.Password)
	if err != nil {
		log.Printf("password hash error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "server_error"})
		return
	}
	res, err := appDB.Exec(
		`INSERT INTO users (email, password_hash, created_at) VALUES (?, ?, ?)`,
		email, hash, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "constraint") {
			writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "email_exists"})
			return
		}
		log.Printf("register insert error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "server_error"})
		return
	}
	userID, _ := res.LastInsertId()
	setSessionCookie(w, r, userID, email)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "email": email, "paid": isPaidEmail(email)})
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method_not_allowed"})
		return
	}
	if !allowAuthRequest(r) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "error": "too_many_requests"})
		return
	}
	req, ok := readAuthRequest(w, r)
	if !ok {
		return
	}
	email := normalizeEmail(req.Email)
	if email == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "invalid_credentials"})
		return
	}

	var userID int64
	var storedHash string
	err := appDB.QueryRow(`SELECT id, password_hash FROM users WHERE email = ?`, email).Scan(&userID, &storedHash)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "invalid_credentials"})
		return
	}
	if err != nil {
		log.Printf("login query error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "server_error"})
		return
	}
	if !verifyPassword(req.Password, storedHash) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "invalid_credentials"})
		return
	}

	setSessionCookie(w, r, userID, email)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "email": email, "paid": isPaidEmail(email)})
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method_not_allowed"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method_not_allowed"})
		return
	}
	claims, ok := currentSession(r)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "authenticated": false, "paid": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"authenticated": true,
		"email":         claims.Email,
		"paid":          isPaidEmail(claims.Email),
	})
}

func readAuthRequest(w http.ResponseWriter, r *http.Request) (authRequest, bool) {
	defer r.Body.Close()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxJSONBody))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_body"})
		return authRequest{}, false
	}
	var req authRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_json"})
		return authRequest{}, false
	}
	return req, true
}

func allowAuthRequest(r *http.Request) bool {
	key := clientIP(r)
	now := time.Now()
	cutoff := now.Add(-authWindow)

	authMu.Lock()
	defer authMu.Unlock()

	hits := authHits[key]
	kept := hits[:0]
	for _, hit := range hits {
		if hit.After(cutoff) {
			kept = append(kept, hit)
		}
	}
	if len(kept) >= authMaxHits {
		authHits[key] = kept
		return false
	}
	authHits[key] = append(kept, now)
	return true
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		first := strings.TrimSpace(strings.Split(forwarded, ",")[0])
		if first != "" {
			return first
		}
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	host := r.RemoteAddr
	if idx := strings.LastIndex(host, ":"); idx > -1 {
		return host[:idx]
	}
	return host
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := pbkdf2SHA256([]byte(password), salt, passwordIter, 32)
	return fmt.Sprintf(
		"pbkdf2_sha256$%d$%s$%s",
		passwordIter,
		base64.RawURLEncoding.EncodeToString(salt),
		base64.RawURLEncoding.EncodeToString(hash),
	), nil
}

func verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2_sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 10000 {
		return false
	}
	salt, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	expected, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	actual := pbkdf2SHA256([]byte(password), salt, iter, len(expected))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func pbkdf2SHA256(password, salt []byte, iter, keyLen int) []byte {
	hashLen := sha256.Size
	blocks := (keyLen + hashLen - 1) / hashLen
	out := make([]byte, 0, blocks*hashLen)
	for block := 1; block <= blocks; block++ {
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := make([]byte, len(u))
		copy(t, u)
		for i := 1; i < iter; i++ {
			mac = hmac.New(sha256.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, userID int64, email string) {
	token, err := signSession(sessionClaims{
		UserID: userID,
		Email:  email,
		Iat:    time.Now().Unix(),
		Exp:    time.Now().Add(180 * 24 * time.Hour).Unix(),
	})
	if err != nil {
		log.Printf("session sign error: %v", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   180 * 24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
	})
}

func currentSession(r *http.Request) (sessionClaims, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return sessionClaims{}, false
	}
	claims, err := verifySession(cookie.Value)
	if err != nil || claims.Exp < time.Now().Unix() || normalizeEmail(claims.Email) == "" {
		return sessionClaims{}, false
	}
	return claims, true
}

func signSession(claims sessionClaims) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signature := signValue(encodedPayload)
	return encodedPayload + "." + signature, nil
}

func verifySession(token string) (sessionClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return sessionClaims{}, errors.New("invalid token")
	}
	expected := signValue(parts[0])
	if subtle.ConstantTimeCompare([]byte(expected), []byte(parts[1])) != 1 {
		return sessionClaims{}, errors.New("invalid signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return sessionClaims{}, err
	}
	var claims sessionClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return sessionClaims{}, err
	}
	return claims, nil
}

func signValue(value string) string {
	mac := hmac.New(sha256.New, []byte(sessionSecret()))
	mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func sessionSecret() string {
	if secret := os.Getenv("SESSION_SECRET"); secret != "" {
		return secret
	}
	if secret := os.Getenv("JWT_SECRET"); secret != "" {
		return secret
	}
	secretOnce.Do(func() {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			log.Printf("session secret fallback random error: %v", err)
			secretKey = "temporary-dev-session-secret"
			return
		}
		secretKey = base64.RawURLEncoding.EncodeToString(buf)
		log.Print("SESSION_SECRET/JWT_SECRET not set; generated temporary session secret for this process")
	})
	return secretKey
}

func isSecureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func isPaidEmail(email string) bool {
	email = normalizeEmail(email)
	if email == "" {
		return false
	}
	if envPaidEmail(email) {
		return true
	}
	if appDB == nil {
		paidMu.RLock()
		_, ok := paidEmails[email]
		paidMu.RUnlock()
		return ok
	}
	var paid int
	err := appDB.QueryRow(`SELECT paid FROM licenses WHERE email = ?`, email).Scan(&paid)
	if err != nil {
		return false
	}
	return paid == 1
}

func envPaidEmail(email string) bool {
	for _, raw := range strings.Split(os.Getenv("PAID_EMAILS"), ",") {
		if normalizeEmail(raw) == email {
			return true
		}
	}
	return false
}

func setLicense(email, eventType, resourceID string, paid bool) {
	email = normalizeEmail(email)
	if email == "" || appDB == nil {
		return
	}
	paidValue := 0
	if paid {
		paidValue = 1
	}
	_, err := appDB.Exec(
		`INSERT INTO licenses (email, paid, source, event_type, resource_id, updated_at)
		 VALUES (?, ?, 'paypal', ?, ?, ?)
		 ON CONFLICT(email) DO UPDATE SET
		 paid = excluded.paid,
		 source = excluded.source,
		 event_type = excluded.event_type,
		 resource_id = excluded.resource_id,
		 updated_at = excluded.updated_at`,
		email,
		paidValue,
		eventType,
		resourceID,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		log.Printf("license upsert error email=%q event=%q: %v", email, eventType, err)
	}
}

func handlePayPalWebhook(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"message": "paypal webhook endpoint ready",
			"path":    "/api/paypal-webhook",
		})
		return
	}

	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method_not_allowed"})
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		log.Printf("paypal webhook read error: %v", err)
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_body"})
		return
	}
	defer r.Body.Close()

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("paypal webhook invalid json: %v body=%s", err, truncateForLog(string(body), 1200))
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_json"})
		return
	}

	eventType, _ := payload["event_type"].(string)
	resourceID := findResourceID(payload)
	emails := extractEmails(payload)
	grantPaid := eventType == "PAYMENT.CAPTURE.COMPLETED"
	revokePaid := eventType == "PAYMENT.CAPTURE.REFUNDED" || eventType == "PAYMENT.CAPTURE.REVERSED" || eventType == "PAYMENT.CAPTURE.DENIED"

	for _, email := range emails {
		if grantPaid {
			paidMu.Lock()
			paidEmails[email] = paidRecord{
				Email:      email,
				EventType:  eventType,
				ResourceID: resourceID,
				ReceivedAt: time.Now().UTC(),
			}
			paidMu.Unlock()
			setLicense(email, eventType, resourceID, true)
		}
		if revokePaid {
			paidMu.Lock()
			delete(paidEmails, email)
			paidMu.Unlock()
			setLicense(email, eventType, resourceID, false)
		}
	}

	log.Printf(
		"paypal webhook received event=%q resource_id=%q emails=%v transmission_id=%q",
		eventType,
		resourceID,
		emails,
		r.Header.Get("Paypal-Transmission-Id"),
	)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"event_type":  eventType,
		"resource_id": resourceID,
		"emails":      emails,
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("json response error: %v", err)
	}
}

func extractEmails(value any) []string {
	seen := map[string]bool{}
	var emails []string

	var walk func(any)
	walk = func(v any) {
		switch typed := v.(type) {
		case map[string]any:
			for key, item := range typed {
				if strings.Contains(strings.ToLower(key), "email") {
					if raw, ok := item.(string); ok {
						if email := normalizeEmail(raw); email != "" && !seen[email] {
							seen[email] = true
							emails = append(emails, email)
						}
					}
				}
				walk(item)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		}
	}

	walk(value)
	sort.Strings(emails)
	return emails
}

func normalizeEmail(value string) string {
	email := strings.ToLower(strings.TrimSpace(value))
	if !strings.Contains(email, "@") || strings.ContainsAny(email, " \t\r\n") {
		return ""
	}
	return email
}

func findResourceID(payload map[string]any) string {
	if resource, ok := payload["resource"]; ok {
		return findStringByKey(resource, "id")
	}
	return findStringByKey(payload, "id")
}

func findStringByKey(value any, key string) string {
	switch typed := value.(type) {
	case map[string]any:
		if raw, ok := typed[key].(string); ok {
			return raw
		}
		for _, item := range typed {
			if found := findStringByKey(item, key); found != "" {
				return found
			}
		}
	case []any:
		for _, item := range typed {
			if found := findStringByKey(item, key); found != "" {
				return found
			}
		}
	}
	return ""
}

func truncateForLog(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "...(truncated)"
}

const indexHTML = `<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>__PAGE_TITLE__</title>
<meta name="description" content="__PAGE_DESCRIPTION__">
<link rel="canonical" href="__CANONICAL_URL__">
<link rel="icon" href="/favicon.svg" type="image/svg+xml">
<link rel="shortcut icon" href="/favicon.ico">
<link href="https://fonts.googleapis.com/css2?family=Syne:wght@400;700;800&family=DM+Sans:wght@400;500&display=swap" rel="stylesheet">
<script async src="https://pagead2.googlesyndication.com/pagead/js/adsbygoogle.js?client=ca-pub-1902780696242483" crossorigin="anonymous"></script>
<script src="https://unpkg.com/qrcode-generator@1.4.4/qrcode.js"></script>
<style>
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
:root{
  --bg:#0e0e11;--surface:#18181d;--surface2:#222228;
  --border:rgba(255,255,255,0.07);--accent:#d4ff57;
  --accent-dim:rgba(212,255,87,0.1);--text:#f0f0f0;
  --muted:#777;--success:#57ff9a;--danger:#ff6b6b;
}
body{font-family:'DM Sans',sans-serif;background:var(--bg);color:var(--text);min-height:100vh;overflow-x:hidden}
body::before{content:'';position:fixed;inset:0;background-image:linear-gradient(rgba(255,255,255,0.02) 1px,transparent 1px),linear-gradient(90deg,rgba(255,255,255,0.02) 1px,transparent 1px);background-size:48px 48px;pointer-events:none;z-index:0}
.wrap{position:relative;z-index:1;max-width:700px;margin:0 auto;padding:64px 24px 80px;display:flex;flex-direction:column}
.header{order:1}
.quota-bar{order:2}
.card{order:3}
.features{order:4}
.section{order:5}
.utility-lab{order:6}
.pro-strip{order:7}
.faq{order:8}
body.route-utility .utility-lab{order:2;margin-top:0;margin-bottom:20px}
body.route-utility .quota-bar{order:3}
body.route-utility .card{order:4}
body.route-utility .features{order:5}
body.route-utility .section{order:6}
body.route-utility .pro-strip{order:7}
body.route-utility .faq{order:8}
body.route-utility .utility-lab .section-head{align-items:flex-start}
body.route-utility .utility-lab .section-title{font-size:18px;color:var(--muted)}
body.route-utility .utility-lab .section-desc{display:none}
.badge{display:inline-flex;align-items:center;gap:6px;background:var(--accent-dim);border:1px solid rgba(212,255,87,0.25);color:var(--accent);font-size:11px;font-weight:500;letter-spacing:.1em;text-transform:uppercase;padding:5px 12px;border-radius:20px;margin-bottom:20px}
.badge-dot{width:6px;height:6px;border-radius:50%;background:var(--accent);animation:pulse 2s ease-in-out infinite}
@keyframes pulse{0%,100%{opacity:1;transform:scale(1)}50%{opacity:.4;transform:scale(.7)}}
h1{font-family:'Syne',sans-serif;font-size:clamp(34px,6vw,54px);font-weight:800;line-height:1.05;letter-spacing:-.02em;margin-bottom:14px}
h1 em{color:var(--accent);font-style:normal}
.desc{color:var(--muted);font-size:15px;line-height:1.65;max-width:460px;margin-bottom:48px}

/* 免费次数提示 */
.quota-bar{display:flex;align-items:center;justify-content:space-between;background:var(--surface2);border:1px solid var(--border);border-radius:12px;padding:12px 16px;margin-bottom:20px;gap:12px}
.quota-left{font-size:13px;color:var(--muted)}
.quota-left strong{color:var(--text);font-family:'Syne',sans-serif}
.quota-track{flex:1;height:4px;background:rgba(255,255,255,0.06);border-radius:2px;overflow:hidden}
.quota-fill{height:100%;background:var(--accent);border-radius:2px;transition:width .4s}
.quota-fill.low{background:var(--danger)}
.quota-unlock{background:transparent;border:1px solid rgba(212,255,87,0.35);color:var(--accent);border-radius:8px;padding:7px 12px;font-size:12px;font-weight:700;cursor:pointer;white-space:nowrap}
.quota-unlock:hover{background:var(--accent-dim)}

.card{background:var(--surface);border:1px solid var(--border);border-radius:20px;padding:32px;margin-bottom:20px}
.tool-tabs{display:grid;grid-template-columns:repeat(4,1fr);gap:8px;background:var(--surface2);border:1px solid var(--border);border-radius:12px;padding:6px;margin-bottom:24px}
.tool-tab{border:0;background:transparent;color:var(--muted);border-radius:8px;padding:11px 8px;font-size:13px;font-weight:700;cursor:pointer;transition:all .18s;white-space:nowrap}
.tool-tab:hover{color:var(--text);background:rgba(255,255,255,0.04)}
.tool-tab.on{background:var(--accent);color:#0e0e11}
.tool-tab .pro{display:inline-block;margin-left:5px;font-size:10px;color:inherit;opacity:.72}
.tool-box.hidden,.tool-controls.hidden{display:none}
@media(max-width:620px){.tool-tabs{grid-template-columns:repeat(2,1fr)}}
.drop-zone{border:1.5px dashed rgba(255,255,255,0.12);border-radius:14px;padding:52px 24px;text-align:center;cursor:pointer;transition:all .2s;background:var(--surface2);margin-bottom:24px}
.drop-zone:hover,.drop-zone.over{border-color:var(--accent);background:var(--accent-dim)}
.drop-zone.over{transform:scale(1.01)}
.drop-zone input{display:none}
.drop-icon{width:52px;height:52px;background:var(--surface);border:1px solid var(--border);border-radius:12px;display:flex;align-items:center;justify-content:center;margin:0 auto 14px;font-size:22px;transition:all .2s}
.drop-zone:hover .drop-icon{background:var(--accent-dim);border-color:rgba(212,255,87,0.3)}
.drop-title{font-family:'Syne',sans-serif;font-size:15px;font-weight:700;margin-bottom:5px}
.drop-title span{color:var(--accent)}
.drop-hint{font-size:12px;color:var(--muted)}
.preview{display:none;background:var(--surface2);border:1px solid var(--border);border-radius:12px;padding:14px 16px;margin-bottom:24px;align-items:center;gap:14px}
.preview.show{display:flex}
.prev-img{width:56px;height:56px;border-radius:8px;object-fit:cover;border:1px solid var(--border);flex-shrink:0}
.prev-info{flex:1;min-width:0}
.prev-name{font-size:13px;font-weight:500;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;margin-bottom:3px}
.prev-size{font-size:12px;color:var(--muted)}
.prev-change{font-size:12px;color:var(--muted);cursor:pointer;padding:6px 12px;border:1px solid var(--border);border-radius:8px;transition:all .15s;flex-shrink:0;white-space:nowrap}
.prev-change:hover{border-color:var(--accent);color:var(--accent)}
.field-label{font-size:11px;font-weight:500;color:var(--muted);text-transform:uppercase;letter-spacing:.08em;margin-bottom:10px}
.size-row{display:flex;align-items:center;gap:10px;margin-bottom:12px}
.size-input{flex:1;background:var(--surface2);border:1.5px solid var(--border);border-radius:10px;padding:12px 16px;font-size:26px;font-family:'Syne',sans-serif;font-weight:700;color:var(--text);outline:none;transition:border-color .2s;-moz-appearance:textfield}
.size-input::-webkit-outer-spin-button,.size-input::-webkit-inner-spin-button{-webkit-appearance:none}
.size-input:focus{border-color:var(--accent)}
.size-unit{font-family:'Syne',sans-serif;font-size:26px;font-weight:700;color:var(--muted)}
.presets{display:flex;gap:8px;flex-wrap:wrap;margin-bottom:28px}
.preset{padding:6px 14px;background:var(--surface2);border:1px solid var(--border);border-radius:8px;color:var(--muted);font-size:13px;font-weight:500;cursor:pointer;transition:all .15s}
.preset:hover,.preset.on{border-color:var(--accent);color:var(--accent);background:var(--accent-dim)}
.format-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:8px;margin-bottom:28px}
.format-option{padding:13px 10px;background:var(--surface2);border:1px solid var(--border);border-radius:10px;color:var(--muted);font-size:13px;font-weight:700;cursor:pointer;transition:all .15s}
.format-option:hover,.format-option.on{border-color:var(--accent);color:var(--accent);background:var(--accent-dim)}
.dimension-grid{display:grid;grid-template-columns:1fr 1fr;gap:10px;margin-bottom:12px}
.dim-input{width:100%;background:var(--surface2);border:1.5px solid var(--border);border-radius:10px;padding:12px 14px;font-size:20px;font-family:'Syne',sans-serif;font-weight:700;color:var(--text);outline:none;-moz-appearance:textfield}
.dim-input::-webkit-outer-spin-button,.dim-input::-webkit-inner-spin-button{-webkit-appearance:none}
.dim-input:focus{border-color:var(--accent)}
.resize-presets{display:flex;gap:8px;flex-wrap:wrap;margin-bottom:16px}
.resize-preset{padding:6px 12px;background:var(--surface2);border:1px solid var(--border);border-radius:8px;color:var(--muted);font-size:12px;font-weight:700;cursor:pointer;transition:all .15s}
.resize-preset:hover,.resize-preset.on{border-color:var(--accent);color:var(--accent);background:var(--accent-dim)}
.mode-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:8px;margin-bottom:28px}
.pro-panel{display:none;background:var(--surface2);border:1px solid rgba(212,255,87,0.24);border-radius:14px;padding:28px;text-align:left}
.pro-panel.show{display:block}
.pro-kicker{display:inline-flex;align-items:center;background:var(--accent-dim);border:1px solid rgba(212,255,87,0.25);color:var(--accent);border-radius:999px;padding:5px 10px;font-size:11px;font-weight:800;margin-bottom:16px}
.pro-title{font-family:'Syne',sans-serif;font-size:24px;font-weight:800;margin-bottom:10px}
.pro-desc{font-size:14px;color:var(--muted);line-height:1.65;margin-bottom:18px}
.pro-list{display:grid;gap:8px;margin-bottom:22px}
.pro-item{font-size:13px;color:var(--text);display:flex;gap:10px;align-items:center}
.pro-item::before{content:'✓';color:var(--accent);font-weight:800}
.btn{width:100%;padding:16px;background:var(--accent);color:#0e0e11;border:none;border-radius:12px;font-size:16px;font-family:'Syne',sans-serif;font-weight:700;cursor:pointer;transition:all .2s}
.btn:hover:not(:disabled){background:#c8f53f;transform:translateY(-1px)}
.btn:active:not(:disabled){transform:translateY(0)}
.btn:disabled{background:var(--surface2);color:var(--muted);cursor:not-allowed;border:1px solid var(--border)}
.btn.pay{background:linear-gradient(135deg,#f59e0b,#ef4444);color:#fff}
.btn.pay:hover:not(:disabled){background:linear-gradient(135deg,#d97706,#dc2626);transform:translateY(-1px)}
.status{display:none;margin-top:14px;padding:13px 16px;border-radius:10px;font-size:13px;font-weight:500;align-items:center;gap:10px}
.status.show{display:flex}
.status.loading{background:rgba(255,255,255,0.03);border:1px solid var(--border);color:var(--muted)}
.status.ok{background:rgba(87,255,154,0.07);border:1px solid rgba(87,255,154,0.25);color:var(--success)}
.status.err{background:rgba(255,107,107,0.07);border:1px solid rgba(255,107,107,0.25);color:var(--danger)}
.sdot{width:7px;height:7px;border-radius:50%;flex-shrink:0}
.loading .sdot{background:var(--muted);animation:pulse 1s ease-in-out infinite}
.ok .sdot{background:var(--success)}
.err .sdot{background:var(--danger)}
@keyframes spin{to{transform:rotate(360deg)}}
.spinner{width:14px;height:14px;border:2px solid rgba(255,255,255,0.08);border-top-color:var(--muted);border-radius:50%;animation:spin .7s linear infinite;flex-shrink:0}

/* paywall modal */
.overlay{display:none;position:fixed;inset:0;background:rgba(0,0,0,0.8);z-index:100;align-items:center;justify-content:center;padding:24px}
.overlay.show{display:flex}
.modal{background:var(--surface);border:1px solid var(--border);border-radius:20px;padding:40px 32px;max-width:420px;width:100%;text-align:center}
.modal-icon{font-size:48px;margin-bottom:16px}
.modal-title{font-family:'Syne',sans-serif;font-size:24px;font-weight:800;margin-bottom:8px}
.modal-desc{color:var(--muted);font-size:14px;line-height:1.6;margin-bottom:28px}
.modal-price{font-family:'Syne',sans-serif;font-size:42px;font-weight:800;color:var(--accent);margin-bottom:4px}
.modal-price-desc{font-size:13px;color:var(--muted);margin-bottom:28px}
.modal-features{text-align:left;background:var(--surface2);border-radius:12px;padding:16px 20px;margin-bottom:24px}
.modal-feature{font-size:13px;color:var(--text);padding:5px 0;display:flex;align-items:center;gap:10px}
.modal-feature::before{content:'✓';color:var(--accent);font-weight:700;flex-shrink:0}
.modal-close{margin-top:16px;font-size:13px;color:var(--muted);cursor:pointer;text-decoration:underline}
.modal-close:hover{color:var(--text)}

.features{display:grid;grid-template-columns:repeat(4,1fr);gap:12px}
@media(max-width:650px){.features{grid-template-columns:repeat(2,1fr)}}
@media(max-width:500px){.features{grid-template-columns:1fr}}
.feat{background:var(--surface);border:1px solid var(--border);border-radius:14px;padding:20px}
.feat-icon{font-size:20px;margin-bottom:10px}
.feat-title{font-family:'Syne',sans-serif;font-size:13px;font-weight:700;margin-bottom:4px}
.feat-desc{font-size:12px;color:var(--muted);line-height:1.55}
.section{margin-top:52px}
.section-head{display:flex;align-items:flex-end;justify-content:space-between;gap:18px;margin-bottom:18px}
.section-title{font-family:'Syne',sans-serif;font-size:24px;font-weight:800;letter-spacing:-.01em}
.section-desc{font-size:13px;color:var(--muted);line-height:1.6;max-width:360px}
.tool-matrix{display:grid;grid-template-columns:repeat(3,1fr);gap:12px}
@media(max-width:720px){.tool-matrix{grid-template-columns:1fr}.section-head{display:block}.section-desc{margin-top:8px}}
.tool-card{background:var(--surface);border:1px solid var(--border);border-radius:14px;padding:20px;min-height:156px;display:flex;flex-direction:column;gap:10px}
.tool-card.pro{border-color:rgba(212,255,87,0.28)}
.tool-card.coming{opacity:.78}
.tool-top{display:flex;align-items:center;justify-content:space-between;gap:10px}
.tool-icon{font-size:22px}
.tool-tag{font-size:10px;font-weight:800;letter-spacing:.06em;color:var(--accent);background:var(--accent-dim);border:1px solid rgba(212,255,87,0.2);border-radius:999px;padding:4px 8px;white-space:nowrap}
.tool-name{font-family:'Syne',sans-serif;font-size:16px;font-weight:800}
.tool-copy{font-size:12px;color:var(--muted);line-height:1.55;flex:1}
.tool-action{align-self:flex-start;border:1px solid var(--border);background:transparent;color:var(--text);border-radius:8px;padding:8px 11px;font-size:12px;font-weight:800;cursor:pointer;transition:all .15s;text-decoration:none}
.tool-action:hover{border-color:var(--accent);color:var(--accent);background:var(--accent-dim)}
.pro-strip{background:linear-gradient(135deg,rgba(212,255,87,0.13),rgba(255,255,255,0.03));border:1px solid rgba(212,255,87,0.25);border-radius:18px;padding:28px;margin-top:52px}
.pro-strip-title{font-family:'Syne',sans-serif;font-size:28px;font-weight:800;margin-bottom:8px}
.pro-strip-copy{color:var(--muted);font-size:14px;line-height:1.65;margin-bottom:18px;max-width:560px}
.pro-points{display:grid;grid-template-columns:repeat(3,1fr);gap:10px;margin-bottom:20px}
@media(max-width:620px){.pro-points{grid-template-columns:1fr}}
.pro-point{background:rgba(255,255,255,0.04);border:1px solid var(--border);border-radius:10px;padding:11px 12px;font-size:12px;color:var(--text);font-weight:700}
.utility-lab{background:var(--surface);border:1px solid var(--border);border-radius:20px;padding:28px;margin-top:52px}
.utility-tabs{display:flex;gap:8px;flex-wrap:wrap;margin-bottom:20px}
.utility-tab{background:var(--surface2);border:1px solid var(--border);color:var(--muted);border-radius:8px;padding:9px 12px;font-size:12px;font-weight:800;cursor:pointer;transition:all .15s}
.utility-tab:hover,.utility-tab.on{border-color:var(--accent);color:var(--accent);background:var(--accent-dim)}
.utility-panel{display:none}
.utility-panel.show{display:block}
.utility-grid{display:grid;grid-template-columns:1fr 1fr;gap:18px;align-items:start}
@media(max-width:720px){.utility-grid{grid-template-columns:1fr}}
.utility-field{display:grid;gap:8px;margin-bottom:12px}
.utility-label{font-size:11px;color:var(--muted);font-weight:800;letter-spacing:.08em;text-transform:uppercase}
.utility-input,.utility-textarea,.utility-select{width:100%;background:var(--surface2);border:1px solid var(--border);border-radius:10px;color:var(--text);padding:12px 13px;font:inherit;outline:none}
.utility-input:focus,.utility-textarea:focus,.utility-select:focus{border-color:var(--accent)}
.utility-textarea{min-height:150px;resize:vertical;line-height:1.55}
.utility-output{background:var(--surface2);border:1px solid var(--border);border-radius:12px;min-height:180px;padding:16px;overflow:auto}
.qr-box{display:flex;align-items:center;justify-content:center;min-height:240px}
.gradient-preview{height:190px;border-radius:12px;border:1px solid var(--border);margin-bottom:12px}
.swatch-row{display:flex;gap:8px;flex-wrap:wrap}
.swatch{height:34px;min-width:86px;border-radius:8px;border:1px solid var(--border)}
.social-canvas{width:100%;max-width:420px;border:1px solid var(--border);border-radius:12px;background:#111}
.json-output{white-space:pre-wrap;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px;line-height:1.55}
.markdown-preview h1,.markdown-preview h2,.markdown-preview h3{font-family:'Syne',sans-serif;margin:0 0 10px}
.markdown-preview p,.markdown-preview li{color:var(--text);line-height:1.65;margin-bottom:8px}
.markdown-preview code{background:rgba(255,255,255,0.08);border-radius:5px;padding:2px 5px}
.faq{margin-top:48px}
.faq-title{font-family:'Syne',sans-serif;font-size:22px;font-weight:800;margin-bottom:24px;letter-spacing:-.01em}
.faq-item{border-bottom:1px solid var(--border);padding:20px 0;cursor:pointer}
.faq-item:last-child{border-bottom:none}
.faq-q{font-family:'Syne',sans-serif;font-size:15px;font-weight:700;display:flex;justify-content:space-between;align-items:center;gap:16px}
.faq-arrow{color:var(--muted);font-size:18px;flex-shrink:0;transition:transform .2s}
.faq-item.open .faq-arrow{transform:rotate(45deg)}
.faq-a{font-size:14px;color:var(--muted);line-height:1.75;max-height:0;overflow:hidden;transition:max-height .3s ease,padding .3s}
.faq-item.open .faq-a{max-height:300px;padding-top:12px}
@keyframes up{from{opacity:0;transform:translateY(18px)}to{opacity:1;transform:translateY(0)}}
.header{animation:up .5s ease both}
.card{animation:up .5s ease .08s both}
.features{animation:up .5s ease .16s both}
.account-bar{display:flex;justify-content:flex-end;margin-bottom:20px}
.account-pill{display:inline-flex;align-items:center;gap:8px;background:var(--surface);border:1px solid var(--border);border-radius:999px;color:var(--text);padding:8px 12px;font-size:12px;font-weight:800;cursor:pointer;max-width:100%;transition:all .15s}
.account-pill:hover{border-color:rgba(212,255,87,0.35);color:var(--accent);background:var(--accent-dim)}
.account-pill.pro{border-color:rgba(212,255,87,0.35);color:var(--accent)}
.account-email{max-width:220px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.auth-switch{display:grid;grid-template-columns:1fr 1fr;gap:8px;background:var(--surface2);border:1px solid var(--border);border-radius:12px;padding:6px;margin-bottom:18px}
.auth-switch button{border:0;background:transparent;color:var(--muted);border-radius:8px;padding:10px;font-size:13px;font-weight:800;cursor:pointer}
.auth-switch button.on{background:var(--accent);color:#0e0e11}
.auth-form{display:grid;gap:10px;text-align:left}
.auth-input{width:100%;background:var(--surface2);border:1px solid var(--border);border-radius:10px;color:var(--text);padding:13px 14px;font:inherit;outline:none}
.auth-input:focus{border-color:var(--accent)}
.auth-hint{font-size:12px;color:var(--muted);line-height:1.55;text-align:left;margin-top:12px}
.auth-msg{min-height:18px;font-size:12px;color:var(--muted);text-align:left}
.auth-msg.err{color:var(--danger)}
.auth-msg.ok{color:var(--success)}
</style>
</head>
<body class="route-__PAGE_KIND__">
<div class="wrap">
  <div class="header">
    <div class="account-bar">
      <button class="account-pill" id="accountPill" onclick="showAuthModal()">登录 / 注册</button>
    </div>
    <div class="badge"><span class="badge-dot"></span>Browser Tool Suite</div>
    <h1>__PAGE_HEADING__<br><em>__PAGE_ACCENT__</em></h1>
    <p class="desc">__PAGE_INTRO__</p>
  </div>

  <!-- 免费次数进度条 -->
  <div class="quota-bar" id="quotaBar">
    <div class="quota-left">免费次数：<strong id="quotaText">100 / 100</strong></div>
    <div class="quota-track"><div class="quota-fill" id="quotaFill" style="width:100%"></div></div>
    <button class="quota-unlock" onclick="showPaywall()">PayPal 解锁</button>
  </div>

  <div class="card">
    <div class="tool-tabs">
      <button class="tool-tab on" id="tab-compress" onclick="switchTool('compress')">单张压缩</button>
      <button class="tool-tab" id="tab-convert" onclick="switchTool('convert')">格式转换</button>
      <button class="tool-tab" id="tab-resize" onclick="switchTool('resize')">尺寸转换</button>
      <button class="tool-tab" id="tab-batch" onclick="switchTool('batch')">批量压缩 <span class="pro">PRO</span></button>
    </div>

    <div class="tool-box" id="singleTool">
      <div class="drop-zone" id="dz">
        <input type="file" id="fi" accept="image/jpeg,image/png,image/webp">
        <div class="drop-icon">🖼</div>
        <div class="drop-title">拖拽图片到这里，或<span>点击选择</span></div>
        <div class="drop-hint" id="dropHint">JPG · PNG · WebP &nbsp;·&nbsp; 本地处理，不上传服务器</div>
      </div>

      <div class="preview" id="prev">
        <img class="prev-img" id="pimg" src="" alt="">
        <div class="prev-info">
          <div class="prev-name" id="pname"></div>
          <div class="prev-size" id="psize"></div>
        </div>
        <div class="prev-change" onclick="document.getElementById('fi').click()">换一张</div>
      </div>

      <div class="tool-controls" id="compressControls">
        <div class="field-label">目标文件大小</div>
        <div class="size-row">
          <input class="size-input" type="number" id="tgt" value="200" min="10" max="20000">
          <span class="size-unit">KB</span>
        </div>
        <div class="presets">
          <button class="preset" onclick="setTarget(100)">100 KB</button>
          <button class="preset on" onclick="setTarget(200)">200 KB</button>
          <button class="preset" onclick="setTarget(500)">500 KB</button>
          <button class="preset" onclick="setTarget(1024)">1 MB</button>
          <button class="preset" onclick="setTarget(2048)">2 MB</button>
        </div>
      </div>

      <div class="tool-controls hidden" id="convertControls">
        <div class="field-label">输出格式</div>
        <div class="format-grid">
          <button class="format-option on" onclick="setFormat('image/jpeg', 'JPG')">JPG</button>
          <button class="format-option" onclick="setFormat('image/png', 'PNG')">PNG</button>
          <button class="format-option" onclick="setFormat('image/webp', 'WebP')">WebP</button>
        </div>
      </div>

      <div class="tool-controls hidden" id="resizeControls">
        <div class="field-label">输出尺寸</div>
        <div class="dimension-grid">
          <input class="dim-input" type="number" id="resizeW" value="200" min="1" max="12000" aria-label="宽度">
          <input class="dim-input" type="number" id="resizeH" value="200" min="1" max="12000" aria-label="高度">
        </div>
        <div class="resize-presets">
          <button class="resize-preset on" onclick="setResizePreset(200, 200)">200x200</button>
          <button class="resize-preset" onclick="setResizePreset(300, 300)">300x300</button>
          <button class="resize-preset" onclick="setResizePreset(512, 512)">512x512</button>
          <button class="resize-preset" onclick="setResizePreset(1080, 1080)">1080x1080</button>
          <button class="resize-preset" onclick="setResizePreset(1920, 1080)">1920x1080</button>
        </div>
        <div class="field-label">适配方式</div>
        <div class="mode-grid">
          <button class="format-option on" onclick="setResizeMode('contain', this)">留白适配</button>
          <button class="format-option" onclick="setResizeMode('cover', this)">裁剪填满</button>
          <button class="format-option" onclick="setResizeMode('stretch', this)">拉伸</button>
        </div>
      </div>

      <button class="btn" id="btn" onclick="runTool()" disabled>选择图片后开始</button>
      <div class="status" id="st"></div>
    </div>

    <div class="pro-panel" id="batchPanel">
      <div class="pro-kicker">PRO TOOL</div>
      <div class="pro-title">批量图片压缩</div>
      <div class="pro-desc">一次选择多张图片，统一压缩到指定大小，适合电商图、证件资料、社媒素材和表单上传前的批处理。</div>
      <div class="pro-list">
        <div class="pro-item">批量处理 JPG / PNG / WebP</div>
        <div class="pro-item">统一目标大小，减少重复操作</div>
        <div class="pro-item">浏览器本地处理，图片不上传服务器</div>
      </div>
      <button class="btn pay" onclick="showPaywall()">使用 PayPal 解锁批量压缩 →</button>
    </div>
  </div>

  <div class="features">
    <div class="feat"><div class="feat-icon">⚡</div><div class="feat-title">浏览器本地处理</div><div class="feat-desc">图片不上传服务器，速度快，完全私密</div></div>
    <div class="feat"><div class="feat-icon">🎯</div><div class="feat-title">精确压缩</div><div class="feat-desc">二分法算法，紧贴目标大小，不会超限</div></div>
    <div class="feat"><div class="feat-icon">📐</div><div class="feat-title">尺寸转换</div><div class="feat-desc">快速生成200x200、头像和平台上传尺寸</div></div>
    <div class="feat"><div class="feat-icon">🆓</div><div class="feat-title">免费格式转换</div><div class="feat-desc">JPG、PNG、WebP互转，免费使用</div></div>
  </div>

  <section class="section">
    <div class="section-head">
      <div class="section-title">Image Tools</div>
      <p class="section-desc">围绕上传限制、头像尺寸、电商素材和批量处理设计，免费工具引流，Pro 工具节省重复操作。</p>
    </div>
    <div class="tool-matrix">
      <div class="tool-card">
        <div class="tool-top"><span class="tool-icon">🎯</span><span class="tool-tag">FREE</span></div>
        <div class="tool-name">图片压缩到指定KB</div>
        <div class="tool-copy">把 JPG、PNG、WebP 压缩到 200KB、500KB、1MB 等目标大小，适合表单、招聘网站和证件材料上传。</div>
        <a class="tool-action" href="/image-compressor">开始压缩</a>
      </div>
      <div class="tool-card">
        <div class="tool-top"><span class="tool-icon">🔁</span><span class="tool-tag">FREE</span></div>
        <div class="tool-name">图片格式转换</div>
        <div class="tool-copy">免费将图片转换为 JPG、PNG 或 WebP，全部在浏览器中完成，不需要上传服务器。</div>
        <a class="tool-action" href="/image-converter">转换格式</a>
      </div>
      <div class="tool-card">
        <div class="tool-top"><span class="tool-icon">📐</span><span class="tool-tag">FREE</span></div>
        <div class="tool-name">图片尺寸转换</div>
        <div class="tool-copy">自定义宽高，支持留白适配、裁剪填满和拉伸，快速生成头像、社媒和平台上传尺寸。</div>
        <a class="tool-action" href="/image-resizer">调整尺寸</a>
      </div>
      <div class="tool-card pro">
        <div class="tool-top"><span class="tool-icon">📦</span><span class="tool-tag">PRO</span></div>
        <div class="tool-name">批量图片压缩</div>
        <div class="tool-copy">一次处理多张图片，统一压缩设置，适合电商图、资料图和社媒素材批处理。</div>
        <a class="tool-action" href="/batch-image-compressor">解锁批量</a>
      </div>
      <div class="tool-card pro coming">
        <div class="tool-top"><span class="tool-icon">✂️</span><span class="tool-tag">PRO SOON</span></div>
        <div class="tool-name">背景移除</div>
        <div class="tool-copy">面向头像、电商图和社媒配图，计划支持本地模型移除背景，Pro 输出高清无水印。</div>
        <button class="tool-action" onclick="showPaywall()">查看 Pro</button>
      </div>
      <div class="tool-card pro coming">
        <div class="tool-top"><span class="tool-icon">🧰</span><span class="tool-tag">PRO</span></div>
        <div class="tool-name">批量尺寸与格式处理</div>
        <div class="tool-copy">把批量压缩、尺寸转换和格式转换组合成一个流水线，后续作为 Pro 核心能力。</div>
        <button class="tool-action" onclick="showPaywall()">解锁 Pro</button>
      </div>
    </div>
  </section>

  <section class="section">
    <div class="section-head">
      <div class="section-title">Creator Tools</div>
      <p class="section-desc">为运营、博主和小商家准备的轻量创作工具，基础功能免费，高级样式和高清导出进入 Pro。</p>
    </div>
    <div class="tool-matrix">
      <div class="tool-card">
        <div class="tool-top"><span class="tool-icon">▦</span><span class="tool-tag">FREE</span></div>
        <div class="tool-name">二维码生成器</div>
        <div class="tool-copy">生成基础二维码，可自定义前景色和背景色，适合链接、活动页和社媒运营。</div>
        <a class="tool-action" href="/qr-code-generator">生成二维码</a>
      </div>
      <div class="tool-card">
        <div class="tool-top"><span class="tool-icon">🖼</span><span class="tool-tag">FREE</span></div>
        <div class="tool-name">社交媒体卡片制作</div>
        <div class="tool-copy">输入标题和副标题，本地生成一张适合社媒分享的卡片图，后续 Pro 解锁更多模板。</div>
        <a class="tool-action" href="/social-card-maker">制作卡片</a>
      </div>
      <div class="tool-card">
        <div class="tool-top"><span class="tool-icon">🎨</span><span class="tool-tag">FREE</span></div>
        <div class="tool-name">配色与渐变生成器</div>
        <div class="tool-copy">随机生成配色和 CSS 渐变，快速复制到设计稿、落地页或社媒素材里。</div>
        <a class="tool-action" href="/gradient-generator">生成配色</a>
      </div>
    </div>
  </section>

  <section class="section">
    <div class="section-head">
      <div class="section-title">Data Tools</div>
      <p class="section-desc">面向开发者、运营和数据整理场景，主打浏览器本地解析，减少上传敏感文件的顾虑。</p>
    </div>
    <div class="tool-matrix">
      <div class="tool-card">
        <div class="tool-top"><span class="tool-icon">{} </span><span class="tool-tag">FREE</span></div>
        <div class="tool-name">CSV / Excel 转 JSON</div>
        <div class="tool-copy">粘贴 CSV 内容，在浏览器本地转换成 JSON。Pro 方向是字段映射、批量文件和保存转换规则。</div>
        <a class="tool-action" href="/csv-to-json">转换 JSON</a>
      </div>
      <div class="tool-card">
        <div class="tool-top"><span class="tool-icon">📄</span><span class="tool-tag">FREE</span></div>
        <div class="tool-name">Markdown 导出 PDF</div>
        <div class="tool-copy">把 Markdown 转成可打印预览，直接使用浏览器打印为 PDF。后续 Pro 支持高级模板。</div>
        <a class="tool-action" href="/markdown-to-pdf">打开编辑器</a>
      </div>
      <div class="tool-card pro coming">
        <div class="tool-top"><span class="tool-icon">⚙️</span><span class="tool-tag">PRO SOON</span></div>
        <div class="tool-name">批量数据转换规则</div>
        <div class="tool-copy">为重复导入、清洗和转换任务保存规则，作为 Data Tools 的付费增强点。</div>
        <button class="tool-action" onclick="showPaywall()">查看 Pro</button>
      </div>
    </div>
  </section>

  <section class="utility-lab" id="utilityLab">
    <div class="section-head">
      <div class="section-title">Live Browser Tools</div>
      <p class="section-desc">这些工具已经可以直接使用，处理过程在浏览器本地完成，适合作为 SEO 引流和 AdSense 展示入口。</p>
    </div>
    <div class="utility-tabs">
      <button class="utility-tab on" id="util-tab-qr" onclick="openUtility('qr')">二维码</button>
      <button class="utility-tab" id="util-tab-social" onclick="openUtility('social')">社交卡片</button>
      <button class="utility-tab" id="util-tab-gradient" onclick="openUtility('gradient')">配色渐变</button>
      <button class="utility-tab" id="util-tab-csv" onclick="openUtility('csv')">CSV转JSON</button>
      <button class="utility-tab" id="util-tab-markdown" onclick="openUtility('markdown')">Markdown转PDF</button>
    </div>

    <div class="utility-panel show" id="util-qr">
      <div class="utility-grid">
        <div>
          <div class="utility-field">
            <label class="utility-label">二维码内容</label>
            <textarea class="utility-textarea" id="qrText">https://img-production-b10c.up.railway.app</textarea>
          </div>
          <div class="dimension-grid">
            <div class="utility-field">
              <label class="utility-label">前景色</label>
              <input class="utility-input" id="qrDark" type="color" value="#0e0e11">
            </div>
            <div class="utility-field">
              <label class="utility-label">背景色</label>
              <input class="utility-input" id="qrLight" type="color" value="#ffffff">
            </div>
          </div>
          <button class="btn" onclick="generateQR()">生成二维码</button>
          <button class="tool-action" onclick="downloadCanvas('qrCanvas', 'qrcode.png')">下载 PNG</button>
        </div>
        <div class="utility-output qr-box">
          <canvas id="qrCanvas" width="260" height="260"></canvas>
        </div>
      </div>
    </div>

    <div class="utility-panel" id="util-social">
      <div class="utility-grid">
        <div>
          <div class="utility-field">
            <label class="utility-label">标题</label>
            <input class="utility-input" id="cardTitle" value="Browser Tool Suite">
          </div>
          <div class="utility-field">
            <label class="utility-label">副标题</label>
            <input class="utility-input" id="cardSubtitle" value="Compress, convert, resize and create useful assets locally.">
          </div>
          <div class="dimension-grid">
            <div class="utility-field">
              <label class="utility-label">强调色</label>
              <input class="utility-input" id="cardAccent" type="color" value="#d4ff57">
            </div>
            <div class="utility-field">
              <label class="utility-label">版式</label>
              <select class="utility-select" id="cardTemplate">
                <option value="dark">Dark tool card</option>
                <option value="light">Clean light card</option>
              </select>
            </div>
          </div>
          <button class="btn" onclick="renderSocialCard()">生成并下载卡片</button>
        </div>
        <div class="utility-output">
          <canvas class="social-canvas" id="socialCanvas" width="1200" height="630"></canvas>
        </div>
      </div>
    </div>

    <div class="utility-panel" id="util-gradient">
      <div class="utility-grid">
        <div>
          <div class="utility-field">
            <label class="utility-label">方向</label>
            <select class="utility-select" id="gradientDirection">
              <option value="135deg">Diagonal</option>
              <option value="90deg">Horizontal</option>
              <option value="180deg">Vertical</option>
              <option value="45deg">Soft angle</option>
            </select>
          </div>
          <button class="btn" onclick="generateGradient()">随机生成配色</button>
        </div>
        <div class="utility-output">
          <div class="gradient-preview" id="gradientPreview"></div>
          <div class="swatch-row" id="swatchRow"></div>
          <pre class="json-output" id="gradientCode"></pre>
        </div>
      </div>
    </div>

    <div class="utility-panel" id="util-csv">
      <div class="utility-grid">
        <div>
          <div class="utility-field">
            <label class="utility-label">CSV内容</label>
            <textarea class="utility-textarea" id="csvInput">name,email,plan
Alice,alice@example.com,free
Bob,bob@example.com,pro</textarea>
          </div>
          <button class="btn" onclick="convertCSV()">转换为 JSON</button>
        </div>
        <div class="utility-output">
          <pre class="json-output" id="jsonOutput"></pre>
        </div>
      </div>
    </div>

    <div class="utility-panel" id="util-markdown">
      <div class="utility-grid">
        <div>
          <div class="utility-field">
            <label class="utility-label">Markdown</label>
            <textarea class="utility-textarea" id="markdownInput"># Project Notes

## Tools
- Image compression
- Format conversion
- CSV to JSON

**Export this page with browser print.**</textarea>
          </div>
          <button class="btn" onclick="renderMarkdown(true)">预览并打印PDF</button>
        </div>
        <div class="utility-output markdown-preview" id="markdownPreview"></div>
      </div>
    </div>
  </section>

  <section class="pro-strip">
    <div class="pro-strip-title">Unlock Pro Tools — $10 one-time</div>
    <p class="pro-strip-copy">Pro 不是单独买一个按钮，而是解锁整个工具包里的高级能力：批量图片处理、背景移除、高清无水印导出、更多模板，以及未来新增的 Pro 工具。</p>
    <div class="pro-points">
      <div class="pro-point">Batch image compression</div>
      <div class="pro-point">Background remover access</div>
      <div class="pro-point">Future Pro tools included</div>
    </div>
    <button class="btn pay" onclick="showPaywall()">使用 PayPal 解锁 $10 →</button>
  </section>

  <div class="faq">
    <div class="faq-title">常见问题</div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">如何把图片压缩到200KB以内？<span class="faq-arrow">+</span></div>
      <div class="faq-a">上传图片后，在目标大小输入框填写"200"，点击"开始压缩"，工具会自动将图片压缩到200KB以内并下载。适合政府表单、招聘网站等对图片大小有严格限制的场景。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">图片压缩后会不会很模糊？<span class="faq-arrow">+</span></div>
      <div class="faq-a">我们使用二分法算法，在满足目标大小的前提下尽量保留最高画质。目标大小设置越接近原图大小，画质损失越小。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">支持压缩到1MB、500KB、100KB吗？<span class="faq-arrow">+</span></div>
      <div class="faq-a">支持任意目标大小，可以直接点击预设按钮，也可以手动输入任意数值。无论压缩到1MB还是50KB都可以处理。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">图片会上传到服务器保存吗？<span class="faq-arrow">+</span></div>
      <div class="faq-a">不会。所有压缩操作完全在你的浏览器本地完成，图片数据从不离开你的设备，完全私密安全。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">支持哪些图片格式？PNG能压缩吗？<span class="faq-arrow">+</span></div>
      <div class="faq-a">支持 JPG、PNG、WebP 格式。PNG 图片会自动转换为 JPG 格式进行压缩，压缩效果更好，文件体积更小。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">微信发图片太模糊，怎么办？<span class="faq-arrow">+</span></div>
      <div class="faq-a">微信会对超过5MB的图片进行二次压缩。建议将图片压缩到4MB以内再发送，在目标大小输入"4096"即可有效避免微信自动压缩。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">免费次数用完了怎么办？<span class="faq-arrow">+</span></div>
      <div class="faq-a">免费额度用完后，支付$10即可解锁无限次使用，一次付费永久有效，不收月费。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">这个网站只有图片压缩工具吗？<span class="faq-arrow">+</span></div>
      <div class="faq-a">不止。现在已经支持图片压缩、格式转换和尺寸转换，后续会逐步加入批量图片处理、背景移除、二维码生成、社交媒体卡片、CSV转JSON和Markdown转PDF等浏览器本地工具。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">尺寸转换支持自定义宽高吗？<span class="faq-arrow">+</span></div>
      <div class="faq-a">支持。你可以输入任意宽度和高度，也可以使用200x200、300x300、512x512、1080x1080等预设。适配方式支持留白适配、裁剪填满和拉伸。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">哪些功能是免费的，哪些是Pro？<span class="faq-arrow">+</span></div>
      <div class="faq-a">单张图片压缩、格式转换和尺寸转换是免费入口。批量压缩、背景移除、批量尺寸和格式处理、高清无水印导出以及未来高级模板会放入Pro工具包。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">CSV转JSON和Markdown转PDF会上传文件吗？<span class="faq-arrow">+</span></div>
      <div class="faq-a">规划方向是浏览器本地解析和导出，尽量不把文件上传到服务器。这样既节省服务器成本，也更适合处理个人资料、运营表格和技术文档。</div>
    </div>
  </div>
</div>

<!-- Paywall Modal -->
<div class="overlay" id="overlay">
  <div class="modal">
    <div class="modal-icon">🎉</div>
    <div class="modal-title">解锁 Pro 工具</div>
    <div class="modal-desc">解锁批量图片压缩和无限次单张压缩。支付一次，永久使用。</div>
    <div class="modal-price">$10</div>
    <div class="modal-price-desc">一次付费 · 永久有效 · 无月费</div>
    <div class="modal-features">
      <div class="modal-feature">批量图片压缩</div>
      <div class="modal-feature">背景移除与未来Pro工具</div>
      <div class="modal-feature">高清无水印导出</div>
      <div class="modal-feature">一次付费，解锁工具包</div>
    </div>
    <button class="btn pay" onclick="goPay()">使用 PayPal 解锁 $10 →</button>
    <div class="auth-hint">已经付款？用付款时的 PayPal 邮箱登录或注册，系统会自动识别 Pro 状态。</div>
    <button class="quota-unlock" style="margin-top:14px" onclick="showAuthModal()">登录 / 注册激活</button>
    <div class="modal-close" onclick="closeModal()">暂时不需要</div>
  </div>
</div>

<!-- Auth Modal -->
<div class="overlay" id="authOverlay">
  <div class="modal">
    <div class="modal-title" id="authTitle">登录</div>
    <div class="modal-desc">只用邮箱和密码，服务器只负责账号与 Pro 状态。</div>
    <div class="auth-switch">
      <button id="authLoginTab" class="on" onclick="setAuthMode('login')">登录</button>
      <button id="authRegisterTab" onclick="setAuthMode('register')">注册</button>
    </div>
    <div class="auth-form">
      <input class="auth-input" id="authEmail" type="email" autocomplete="email" placeholder="PayPal 邮箱">
      <input class="auth-input" id="authPassword" type="password" autocomplete="current-password" placeholder="密码，至少 8 位">
      <button class="btn" id="authSubmit" onclick="submitAuth()">继续</button>
      <div class="auth-msg" id="authMsg"></div>
    </div>
    <div class="auth-hint">付款后请使用 PayPal 付款邮箱登录。没有付款也可以先注册，免费工具照常使用。</div>
    <div class="modal-close" onclick="closeAuthModal()">关闭</div>
  </div>
</div>

<script>
const FREE_LIMIT = 100;
const STORAGE_KEY = 'img_compress_count';
const PAYPAL_PAYMENT_LINK = 'https://www.paypal.com/ncp/payment/LN55VSJYNE252';
const INITIAL_PAGE_TOOL = __PAGE_TOOL__;
const INITIAL_PAGE_UTILITY = __PAGE_UTILITY__;

let f = null;
let usedCount = parseInt(localStorage.getItem(STORAGE_KEY) || '0');
let account = { authenticated: false, paid: false, email: '' };
let authMode = 'login';
let activeTool = 'compress';
let outputType = 'image/jpeg';
let outputLabel = 'JPG';
let resizeMode = 'contain';

const dz = document.getElementById('dz');
const fi = document.getElementById('fi');
const prev = document.getElementById('prev');
const pimg = document.getElementById('pimg');
const pname = document.getElementById('pname');
const psize = document.getElementById('psize');
const btn = document.getElementById('btn');
const st = document.getElementById('st');
const dropHint = document.getElementById('dropHint');
const singleTool = document.getElementById('singleTool');
const batchPanel = document.getElementById('batchPanel');
const compressControls = document.getElementById('compressControls');
const convertControls = document.getElementById('convertControls');
const resizeControls = document.getElementById('resizeControls');
const resizeW = document.getElementById('resizeW');
const resizeH = document.getElementById('resizeH');

initAccount();
updateQuota();
initRoutePage();

dz.onclick = () => fi.click();
dz.ondragover = e => { e.preventDefault(); dz.classList.add('over'); };
dz.ondragleave = () => dz.classList.remove('over');
dz.ondrop = e => { e.preventDefault(); dz.classList.remove('over'); if (e.dataTransfer.files[0]) load(e.dataTransfer.files[0]); };
fi.onchange = e => { if (e.target.files[0]) load(e.target.files[0]); };

function switchTool(tool) {
  activeTool = tool;
  document.querySelectorAll('.tool-tab').forEach(tab => tab.classList.remove('on'));
  document.getElementById('tab-' + tool).classList.add('on');
  singleTool.classList.toggle('hidden', tool === 'batch');
  batchPanel.classList.toggle('show', tool === 'batch');

  if (tool === 'batch') {
    st.className = 'status';
    return;
  }

  compressControls.classList.toggle('hidden', tool !== 'compress');
  convertControls.classList.toggle('hidden', tool !== 'convert');
  resizeControls.classList.toggle('hidden', tool !== 'resize');
  if (tool === 'convert') {
    dropHint.textContent = '免费转换 JPG · PNG · WebP · 图片不上传服务器';
  } else if (tool === 'resize') {
    dropHint.textContent = '自定义宽高 · 头像尺寸 · 平台上传尺寸 · 本地处理';
  } else {
    dropHint.textContent = 'JPG · PNG · WebP · 本地处理，不上传服务器';
  }
  st.className = 'status';
  updateButtonText();
}

function runTool() {
  if (activeTool === 'convert') {
    convertImage();
    return;
  }
  if (activeTool === 'resize') {
    resizeImage();
    return;
  }
  compress();
}

function load(file) {
  if (!file.type.startsWith('image/')) { showStatus('err', '请选择图片文件'); return; }
  f = file;
  const r = new FileReader();
  r.onload = e => {
    pimg.src = e.target.result;
    pname.textContent = file.name;
    psize.textContent = '原始大小：' + (file.size / 1024).toFixed(1) + ' KB';
    prev.classList.add('show');
    dz.style.display = 'none';
  };
  r.readAsDataURL(file);
  btn.disabled = false;
  updateButtonText();
  st.className = 'status';
}

function updateButtonText() {
  if (!f) {
    btn.textContent = '选择图片后开始';
    btn.disabled = true;
    return;
  }
  btn.disabled = false;
  if (activeTool === 'convert') {
    btn.textContent = '免费转换为 ' + outputLabel + ' →';
  } else if (activeTool === 'resize') {
    btn.textContent = '转换尺寸 →';
  } else {
    btn.textContent = '开始压缩 →';
  }
}

function setTarget(kb) {
  document.getElementById('tgt').value = kb;
  document.querySelectorAll('.preset').forEach(b => {
    const val = parseInt(b.textContent);
    b.classList.toggle('on', val === kb || (kb === 1024 && b.textContent.includes('1 MB')) || (kb === 2048 && b.textContent.includes('2 MB')));
  });
}

function setFormat(type, label) {
  outputType = type;
  outputLabel = label;
  document.querySelectorAll('.format-option').forEach(b => {
    b.classList.toggle('on', b.textContent.trim() === label);
  });
  updateButtonText();
}

function setResizePreset(w, h) {
  resizeW.value = w;
  resizeH.value = h;
  document.querySelectorAll('.resize-preset').forEach(b => {
    b.classList.toggle('on', b.textContent.trim() === w + 'x' + h);
  });
}

function setResizeMode(mode, el) {
  resizeMode = mode;
  document.querySelectorAll('.mode-grid .format-option').forEach(b => b.classList.remove('on'));
  el.classList.add('on');
}

function updateQuota() {
  if (isPaid()) {
    document.getElementById('quotaText').textContent = 'PRO';
    const fill = document.getElementById('quotaFill');
    fill.style.width = '100%';
    fill.className = 'quota-fill';
    return;
  }
  const remaining = Math.max(0, FREE_LIMIT - usedCount);
  const pct = (remaining / FREE_LIMIT) * 100;
  document.getElementById('quotaText').textContent = remaining + ' / ' + FREE_LIMIT;
  const fill = document.getElementById('quotaFill');
  fill.style.width = pct + '%';
  fill.className = 'quota-fill' + (remaining <= 10 ? ' low' : '');
}

async function compress() {
  if (!f) return;

  // 检查免费次数
  if (!isPaid() && usedCount >= FREE_LIMIT) {
    showPaywall();
    return;
  }

  const targetKB = parseFloat(document.getElementById('tgt').value);
  if (!targetKB || targetKB <= 0) { showStatus('err', '请输入有效的目标大小'); return; }
  const targetBytes = targetKB * 1024;

  btn.disabled = true;
  btn.textContent = '压缩中...';
  showStatus('loading', '正在压缩，请稍候...');

  try {
    const result = await compressInBrowser(f, targetBytes);
    const origKB = (f.size / 1024).toFixed(1);
    const resultKB = (result.size / 1024).toFixed(1);
    const saved = (((f.size - result.size) / f.size) * 100).toFixed(0);

    // 下载
    const url = URL.createObjectURL(result);
    const a = document.createElement('a');
    const ext = result.type === 'image/png' ? '.png' : '.jpg';
    const base = f.name.replace(/\.[^.]+$/, '');
    a.download = 'compressed_' + base + ext;
    a.href = url;
    a.click();
    URL.revokeObjectURL(url);

    // 记录次数
    if (!isPaid()) {
      usedCount++;
      localStorage.setItem(STORAGE_KEY, usedCount);
      updateQuota();
    }

    showStatus('ok', origKB + ' KB → ' + resultKB + ' KB · 减少 ' + saved + '% · 已自动下载');
    psize.textContent = '原始：' + origKB + ' KB → 压缩后：' + resultKB + ' KB';
  } catch (e) {
    showStatus('err', '压缩失败，请重试');
  } finally {
    btn.disabled = false;
    btn.textContent = '再次压缩 →';
  }
}

async function convertImage() {
  if (!f) return;

  btn.disabled = true;
  btn.textContent = '转换中...';
  showStatus('loading', '正在转换格式，请稍候...');

  try {
    const result = await convertInBrowser(f, outputType);
    const resultKB = (result.size / 1024).toFixed(1);
    const ext = outputType === 'image/png' ? '.png' : outputType === 'image/webp' ? '.webp' : '.jpg';
    const base = f.name.replace(/\.[^.]+$/, '');
    downloadBlob(result, 'converted_' + base + ext);
    showStatus('ok', '已转换为 ' + outputLabel + ' · ' + resultKB + ' KB · 已自动下载');
    psize.textContent = '已转换为：' + outputLabel + ' · ' + resultKB + ' KB';
  } catch (e) {
    showStatus('err', '转换失败，请重试');
  } finally {
    btn.disabled = false;
    updateButtonText();
  }
}

async function resizeImage() {
  if (!f) return;

  const width = parseInt(resizeW.value, 10);
  const height = parseInt(resizeH.value, 10);
  if (!width || !height || width <= 0 || height <= 0) {
    showStatus('err', '请输入有效的宽度和高度');
    return;
  }
  if (width > 12000 || height > 12000) {
    showStatus('err', '尺寸过大，请输入12000以内的宽高');
    return;
  }

  btn.disabled = true;
  btn.textContent = '处理中...';
  showStatus('loading', '正在转换图片尺寸...');

  try {
    const result = await resizeInBrowser(f, width, height, resizeMode);
    const resultKB = (result.size / 1024).toFixed(1);
    const base = f.name.replace(/\.[^.]+$/, '');
    downloadBlob(result, 'resized_' + base + '_' + width + 'x' + height + '.jpg');
    showStatus('ok', '已转换为 ' + width + 'x' + height + ' · ' + resultKB + ' KB · 已自动下载');
    psize.textContent = '已转换尺寸：' + width + 'x' + height + ' · ' + resultKB + ' KB';
  } catch (e) {
    showStatus('err', '尺寸转换失败，请重试');
  } finally {
    btn.disabled = false;
    updateButtonText();
  }
}

// 浏览器端二分法压缩
function compressInBrowser(file, targetBytes) {
  return new Promise((resolve, reject) => {
    const img = new Image();
    const url = URL.createObjectURL(file);
    img.onload = () => {
      URL.revokeObjectURL(url);
      const canvas = document.createElement('canvas');
      canvas.width = img.naturalWidth;
      canvas.height = img.naturalHeight;
      const ctx = canvas.getContext('2d');
      ctx.drawImage(img, 0, 0);

      // 如果原图已经小于目标，直接返回
      if (file.size <= targetBytes) {
        canvas.toBlob(blob => resolve(blob), 'image/jpeg', 0.95);
        return;
      }

      // 二分法找最优 quality
      let low = 0.01, high = 0.95, bestBlob = null, attempts = 0;

      function tryQuality(q) {
        canvas.toBlob(blob => {
          attempts++;
          if (!blob) { reject(new Error('Failed')); return; }

          if (blob.size <= targetBytes) {
            bestBlob = blob;
            low = q;
          } else {
            high = q;
          }

          if (attempts >= 10 || (high - low) < 0.01) {
            if (bestBlob) {
              resolve(bestBlob);
            } else {
              // 还是太大，用最低质量
              canvas.toBlob(b => resolve(b), 'image/jpeg', 0.01);
            }
            return;
          }
          tryQuality((low + high) / 2);
        }, 'image/jpeg', q);
      }

      tryQuality((low + high) / 2);
    };
    img.onerror = reject;
    img.src = url;
  });
}

function convertInBrowser(file, type) {
  return new Promise((resolve, reject) => {
    const img = new Image();
    const url = URL.createObjectURL(file);
    img.onload = () => {
      URL.revokeObjectURL(url);
      const canvas = document.createElement('canvas');
      canvas.width = img.naturalWidth;
      canvas.height = img.naturalHeight;
      const ctx = canvas.getContext('2d');
      if (type === 'image/jpeg') {
        ctx.fillStyle = '#ffffff';
        ctx.fillRect(0, 0, canvas.width, canvas.height);
      }
      ctx.drawImage(img, 0, 0);
      canvas.toBlob(blob => {
        if (!blob) { reject(new Error('Failed')); return; }
        resolve(blob);
      }, type, 0.92);
    };
    img.onerror = reject;
    img.src = url;
  });
}

function resizeInBrowser(file, width, height, mode) {
  return new Promise((resolve, reject) => {
    const img = new Image();
    const url = URL.createObjectURL(file);
    img.onload = () => {
      URL.revokeObjectURL(url);
      const canvas = document.createElement('canvas');
      canvas.width = width;
      canvas.height = height;
      const ctx = canvas.getContext('2d');
      ctx.fillStyle = '#ffffff';
      ctx.fillRect(0, 0, width, height);

      let sx = 0, sy = 0, sw = img.naturalWidth, sh = img.naturalHeight;
      let dx = 0, dy = 0, dw = width, dh = height;

      if (mode === 'contain') {
        const scale = Math.min(width / img.naturalWidth, height / img.naturalHeight);
        dw = img.naturalWidth * scale;
        dh = img.naturalHeight * scale;
        dx = (width - dw) / 2;
        dy = (height - dh) / 2;
      } else if (mode === 'cover') {
        const targetRatio = width / height;
        const imageRatio = img.naturalWidth / img.naturalHeight;
        if (imageRatio > targetRatio) {
          sw = img.naturalHeight * targetRatio;
          sx = (img.naturalWidth - sw) / 2;
        } else {
          sh = img.naturalWidth / targetRatio;
          sy = (img.naturalHeight - sh) / 2;
        }
      }

      ctx.drawImage(img, sx, sy, sw, sh, dx, dy, dw, dh);
      canvas.toBlob(blob => {
        if (!blob) { reject(new Error('Failed')); return; }
        resolve(blob);
      }, 'image/jpeg', 0.92);
    };
    img.onerror = reject;
    img.src = url;
  });
}

function downloadBlob(blob, filename) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.download = filename;
  a.href = url;
  a.click();
  URL.revokeObjectURL(url);
}

function openUtility(tool, shouldScroll = true) {
  document.querySelectorAll('.utility-tab').forEach(tab => tab.classList.remove('on'));
  document.querySelectorAll('.utility-panel').forEach(panel => panel.classList.remove('show'));
  document.getElementById('util-tab-' + tool).classList.add('on');
  document.getElementById('util-' + tool).classList.add('show');
  if (shouldScroll) {
    document.getElementById('utilityLab').scrollIntoView({ behavior: 'smooth', block: 'start' });
  }
  if (tool === 'qr') generateQR();
  if (tool === 'gradient') generateGradient();
  if (tool === 'social') renderSocialCard(false);
  if (tool === 'csv') convertCSV();
  if (tool === 'markdown') renderMarkdown(false);
}

function generateQR() {
  const text = document.getElementById('qrText').value.trim();
  const canvas = document.getElementById('qrCanvas');
  if (!text) return;
  if (!window.qrcode) {
    const ctx = canvas.getContext('2d');
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.fillStyle = '#ffffff';
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    ctx.fillStyle = '#0e0e11';
    ctx.font = '14px sans-serif';
    ctx.fillText('二维码库加载失败', 72, 128);
    return;
  }
  const qr = qrcode(0, 'M');
  qr.addData(text);
  qr.make();
  const ctx = canvas.getContext('2d');
  const count = qr.getModuleCount();
  const margin = 16;
  const cell = Math.floor((canvas.width - margin * 2) / count);
  const size = cell * count;
  const offset = Math.floor((canvas.width - size) / 2);
  const dark = document.getElementById('qrDark').value;
  const light = document.getElementById('qrLight').value;
  ctx.fillStyle = light;
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  ctx.fillStyle = dark;
  for (let row = 0; row < count; row++) {
    for (let col = 0; col < count; col++) {
      if (qr.isDark(row, col)) {
        ctx.fillRect(offset + col * cell, offset + row * cell, cell, cell);
      }
    }
  }
}

function downloadCanvas(id, filename) {
  const canvas = document.getElementById(id);
  canvas.toBlob(blob => {
    if (blob) downloadBlob(blob, filename);
  }, 'image/png');
}

function renderSocialCard(shouldDownload = true) {
  const canvas = document.getElementById('socialCanvas');
  const ctx = canvas.getContext('2d');
  const title = document.getElementById('cardTitle').value || 'Browser Tool Suite';
  const subtitle = document.getElementById('cardSubtitle').value || 'Useful tools that run locally in your browser.';
  const accent = document.getElementById('cardAccent').value || '#d4ff57';
  const template = document.getElementById('cardTemplate').value;
  const light = template === 'light';
  ctx.fillStyle = light ? '#f7f7f2' : '#0e0e11';
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  ctx.fillStyle = accent;
  ctx.fillRect(0, 0, 26, canvas.height);
  ctx.globalAlpha = 0.15;
  for (let x = 90; x < canvas.width; x += 90) {
    ctx.fillRect(x, 0, 1, canvas.height);
  }
  for (let y = 90; y < canvas.height; y += 90) {
    ctx.fillRect(0, y, canvas.width, 1);
  }
  ctx.globalAlpha = 1;
  ctx.fillStyle = light ? '#111111' : '#ffffff';
  ctx.font = '800 72px Syne, sans-serif';
  wrapCanvasText(ctx, title, 95, 230, 960, 82);
  ctx.fillStyle = light ? '#4a4a4a' : '#b7b7b7';
  ctx.font = '400 30px DM Sans, sans-serif';
  wrapCanvasText(ctx, subtitle, 100, 400, 920, 42);
  ctx.fillStyle = accent;
  ctx.font = '800 24px Syne, sans-serif';
  ctx.fillText('IMGTOOLS · BROWSER LOCAL', 100, 545);
  if (shouldDownload) downloadCanvas('socialCanvas', 'social-card.png');
}

function wrapCanvasText(ctx, text, x, y, maxWidth, lineHeight) {
  const words = text.split(/\s+/);
  let line = '';
  for (const word of words) {
    const test = line ? line + ' ' + word : word;
    if (ctx.measureText(test).width > maxWidth && line) {
      ctx.fillText(line, x, y);
      line = word;
      y += lineHeight;
    } else {
      line = test;
    }
  }
  if (line) ctx.fillText(line, x, y);
}

function randomColor() {
  return '#' + Math.floor(Math.random() * 16777215).toString(16).padStart(6, '0');
}

function generateGradient() {
  const colors = [randomColor(), randomColor(), randomColor()];
  const direction = document.getElementById('gradientDirection').value;
  const css = 'linear-gradient(' + direction + ', ' + colors.join(', ') + ')';
  document.getElementById('gradientPreview').style.background = css;
  document.getElementById('swatchRow').innerHTML = colors.map(c => '<div class="swatch" title="' + c + '" style="background:' + c + '"></div>').join('');
  document.getElementById('gradientCode').textContent = 'background: ' + css + ';';
}

function parseCSV(text) {
  const rows = [];
  let row = [], cell = '', quote = false;
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    const next = text[i + 1];
    if (ch === '"' && quote && next === '"') {
      cell += '"';
      i++;
    } else if (ch === '"') {
      quote = !quote;
    } else if (ch === ',' && !quote) {
      row.push(cell.trim());
      cell = '';
    } else if ((ch === '\n' || ch === '\r') && !quote) {
      if (ch === '\r' && next === '\n') i++;
      row.push(cell.trim());
      if (row.some(v => v !== '')) rows.push(row);
      row = [];
      cell = '';
    } else {
      cell += ch;
    }
  }
  row.push(cell.trim());
  if (row.some(v => v !== '')) rows.push(row);
  return rows;
}

function convertCSV() {
  const rows = parseCSV(document.getElementById('csvInput').value);
  if (rows.length < 2) {
    document.getElementById('jsonOutput').textContent = '请粘贴带表头的CSV内容。';
    return;
  }
  const headers = rows[0];
  const data = rows.slice(1).map(row => {
    const item = {};
    headers.forEach((header, index) => {
      item[header || 'field_' + index] = row[index] || '';
    });
    return item;
  });
  document.getElementById('jsonOutput').textContent = JSON.stringify(data, null, 2);
}

function escapeHTML(value) {
  return value.replace(/[&<>"']/g, ch => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[ch]));
}

function markdownToHTML(markdown) {
  const lines = markdown.split(/\r?\n/);
  let html = '', inList = false;
  for (const raw of lines) {
    const line = raw.trim();
    if (!line) {
      if (inList) { html += '</ul>'; inList = false; }
      continue;
    }
    if (line.startsWith('# ')) {
      if (inList) { html += '</ul>'; inList = false; }
      html += '<h1>' + inlineMarkdown(line.slice(2)) + '</h1>';
    } else if (line.startsWith('## ')) {
      if (inList) { html += '</ul>'; inList = false; }
      html += '<h2>' + inlineMarkdown(line.slice(3)) + '</h2>';
    } else if (line.startsWith('### ')) {
      if (inList) { html += '</ul>'; inList = false; }
      html += '<h3>' + inlineMarkdown(line.slice(4)) + '</h3>';
    } else if (line.startsWith('- ')) {
      if (!inList) { html += '<ul>'; inList = true; }
      html += '<li>' + inlineMarkdown(line.slice(2)) + '</li>';
    } else {
      if (inList) { html += '</ul>'; inList = false; }
      html += '<p>' + inlineMarkdown(line) + '</p>';
    }
  }
  if (inList) html += '</ul>';
  return html;
}

function inlineMarkdown(text) {
  return escapeHTML(text)
    .replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>');
}

function renderMarkdown(shouldPrint) {
  const html = markdownToHTML(document.getElementById('markdownInput').value);
  document.getElementById('markdownPreview').innerHTML = html;
  if (shouldPrint) {
    const w = window.open('', '_blank');
    w.document.write('<!doctype html><html><head><title>Markdown PDF</title><style>body{font-family:Arial,sans-serif;max-width:760px;margin:40px auto;line-height:1.65;color:#111}code{background:#eee;padding:2px 5px;border-radius:4px}</style></head><body>' + html + '<script>window.print()<\/script></body></html>');
    w.document.close();
  }
}

function jumpTool(tool) {
  switchTool(tool);
  document.querySelector('.card').scrollIntoView({ behavior: 'smooth', block: 'start' });
}

function initRoutePage() {
  if (INITIAL_PAGE_TOOL) {
    switchTool(INITIAL_PAGE_TOOL);
  }
  if (INITIAL_PAGE_UTILITY) {
    openUtility(INITIAL_PAGE_UTILITY, false);
  }
}

function showSoon(name) {
  closeModal();
  st.className = 'status show loading';
  st.innerHTML = '<div class="sdot"></div><span>' + name + ' 即将上线，先试试当前可用工具。</span>';
  document.querySelector('.card').scrollIntoView({ behavior: 'smooth', block: 'start' });
}

function showPaywall() {
  document.getElementById('overlay').classList.add('show');
}

function closeModal() {
  document.getElementById('overlay').classList.remove('show');
}

function isPaid() {
  return account && account.paid === true;
}

async function initAccount() {
  try {
    const res = await fetch('/api/me', { credentials: 'same-origin' });
    const data = await res.json();
    if (data && data.ok) {
      account = {
        authenticated: data.authenticated === true,
        paid: data.paid === true,
        email: data.email || ''
      };
      renderAccount();
      updateQuota();
    }
  } catch (e) {
    renderAccount();
  }
}

function renderAccount() {
  const pill = document.getElementById('accountPill');
  if (!pill) return;
  if (!account.authenticated) {
    pill.className = 'account-pill';
    pill.innerHTML = '登录 / 注册';
    pill.onclick = showAuthModal;
    return;
  }
  pill.className = 'account-pill' + (account.paid ? ' pro' : '');
  pill.innerHTML = '<span class="account-email">' + escapeHTML(account.email) + '</span><span>' + (account.paid ? 'PRO' : 'FREE') + '</span>';
  pill.onclick = showAccountMenu;
}

function showAccountMenu() {
  if (!account.authenticated) {
    showAuthModal();
    return;
  }
  const action = confirm((account.paid ? 'Pro 已激活' : '当前是免费账号') + '\n\n点击确定退出登录，取消保留登录。');
  if (action) logout();
}

function showAuthModal(mode) {
  closeModal();
  setAuthMode(mode || authMode || 'login');
  document.getElementById('authOverlay').classList.add('show');
  setTimeout(() => document.getElementById('authEmail').focus(), 50);
}

function closeAuthModal() {
  document.getElementById('authOverlay').classList.remove('show');
}

function setAuthMode(mode) {
  authMode = mode === 'register' ? 'register' : 'login';
  document.getElementById('authTitle').textContent = authMode === 'register' ? '注册' : '登录';
  document.getElementById('authLoginTab').classList.toggle('on', authMode === 'login');
  document.getElementById('authRegisterTab').classList.toggle('on', authMode === 'register');
  document.getElementById('authSubmit').textContent = authMode === 'register' ? '注册并登录' : '登录';
  document.getElementById('authMsg').textContent = '';
  document.getElementById('authMsg').className = 'auth-msg';
}

async function submitAuth() {
  const email = document.getElementById('authEmail').value.trim();
  const password = document.getElementById('authPassword').value;
  const msg = document.getElementById('authMsg');
  const submit = document.getElementById('authSubmit');
  msg.className = 'auth-msg';
  msg.textContent = '';
  if (!email || !email.includes('@')) {
    msg.className = 'auth-msg err';
    msg.textContent = '请输入有效邮箱。';
    return;
  }
  if (password.length < 8) {
    msg.className = 'auth-msg err';
    msg.textContent = '密码至少 8 位。';
    return;
  }
  submit.disabled = true;
  submit.textContent = '处理中...';
  try {
    const res = await fetch('/api/' + authMode, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, password })
    });
    const data = await res.json();
    if (!res.ok || !data.ok) {
      msg.className = 'auth-msg err';
      msg.textContent = authErrorText(data.error);
      return;
    }
    account = { authenticated: true, paid: data.paid === true, email: data.email || email };
    renderAccount();
    updateQuota();
    msg.className = 'auth-msg ok';
    msg.textContent = account.paid ? '已登录，Pro 已激活。' : '已登录，当前是免费账号。';
    setTimeout(closeAuthModal, 700);
  } catch (e) {
    msg.className = 'auth-msg err';
    msg.textContent = '网络异常，请稍后重试。';
  } finally {
    submit.disabled = false;
    submit.textContent = authMode === 'register' ? '注册并登录' : '登录';
  }
}

function authErrorText(code) {
  if (code === 'email_exists') return '这个邮箱已经注册，请切换到登录。';
  if (code === 'weak_password') return '密码至少 8 位。';
  if (code === 'invalid_credentials') return '邮箱或密码不正确。';
  if (code === 'invalid_email') return '邮箱格式不正确。';
  return '操作失败，请稍后重试。';
}

async function logout() {
  try {
    await fetch('/api/logout', { method: 'POST', credentials: 'same-origin' });
  } catch (e) {}
  account = { authenticated: false, paid: false, email: '' };
  renderAccount();
  updateQuota();
}

function goPay() {
  window.location.href = PAYPAL_PAYMENT_LINK;
}

function showStatus(type, msg) {
  st.className = 'status show ' + type;
  if (type === 'loading') {
    st.innerHTML = '<div class="spinner"></div><span>' + msg + '</span>';
  } else {
    st.innerHTML = '<div class="sdot"></div><span>' + msg + '</span>';
  }
}

generateQR();
renderSocialCard(false);
generateGradient();
convertCSV();
renderMarkdown(false);
</script>
</body>
</html>`
