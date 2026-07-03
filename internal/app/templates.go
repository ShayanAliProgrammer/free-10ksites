package app

import (
        "embed"
        "fmt"
        "html/template"
        "io/fs"
        "strings"
        "time"

        assets "10ksites"
)

// templatesFS is the embedded templates directory.
var templatesFS embed.FS = assets.TemplatesFS

// Static assets embedded at compile time.
var (
        staticCSS        = assets.CSS
        staticHTMX       = assets.HTMX
        staticAppJS      = assets.AppJS
        staticSecurityJS = assets.SecurityJS
)

// ============================================================================
// Data types
// ============================================================================

type SiteType string

const (
        SiteTypeLanding   SiteType = "landing"
        SiteTypePortfolio SiteType = "portfolio"
        SiteTypeSaaS      SiteType = "saas"
        SiteTypeEcommerce SiteType = "ecommerce"
        SiteTypeBlog      SiteType = "blog"
        SiteTypeOther     SiteType = "other"
)

type RequestStatus string

const (
        StatusReceived   RequestStatus = "received"
        StatusQueued     RequestStatus = "queued"
        StatusInProgress RequestStatus = "in_progress"
        StatusReview     RequestStatus = "review"
        StatusDelivered  RequestStatus = "delivered"
)

type WebsiteRequest struct {
        ID                   string        `json:"id"`
        TrackingID           string        `json:"trackingId"`
        RequesterName        string        `json:"requesterName"`
        RequesterEmail       string        `json:"requesterEmail"`
        SiteType             SiteType      `json:"siteType"`
        SiteDescription      string        `json:"siteDescription"`
        InspirationURL       string        `json:"inspirationUrl,omitempty"`
        Status               RequestStatus `json:"status"`
        Progress             int           `json:"progress"`
        EstimatedDelivery    string        `json:"estimatedDelivery,omitempty"`
        DeliveredURL         string        `json:"deliveredUrl,omitempty"`
        AdminNotes           string        `json:"adminNotes,omitempty"`
        QueuePosition        *int          `json:"queuePosition,omitempty"`
        CreatedAt            time.Time     `json:"createdAt"`
        UpdatedAt            time.Time     `json:"updatedAt"`
}

type StatusInfo struct {
        Key         RequestStatus
        Label       string
        Description string
        Step        int
}

var StatusFlow = []StatusInfo{
        {StatusReceived, "Request Received", "We've got your request and added it to the queue.", 0},
        {StatusQueued, "In Queue", "Your site is queued and waiting for a build slot.", 1},
        {StatusInProgress, "In Progress", "Our team is actively designing and building your site.", 2},
        {StatusReview, "In Review", "Final polish, QA, and your approval pending.", 3},
        {StatusDelivered, "Delivered", "Your site is live! Check the delivery link below.", 4},
}

type SiteTypeMeta struct {
        Value       SiteType
        Label       string
        Emoji       string
        Description string
}

var SiteTypes = []SiteTypeMeta{
        {SiteTypeLanding, "Landing Page", "🚀", "A single high-conversion page for a product, event, or idea."},
        {SiteTypePortfolio, "Portfolio", "🎨", "Showcase your work, case studies, and contact info."},
        {SiteTypeSaaS, "SaaS Waitlist", "⚡", "Capture early sign-ups with a sleek waitlist page."},
        {SiteTypeEcommerce, "Simple E-commerce", "🛍️", "A small storefront with products and checkout."},
        {SiteTypeBlog, "Blog", "✍️", "A clean, fast blog with posts and RSS."},
        {SiteTypeOther, "Other", "✨", "Something custom — tell us what you need."},
}

type StatsResponse struct {
        Goal       int            `json:"goal"`
        Total      int            `json:"total"`
        Delivered  int            `json:"delivered"`
        InProgress int            `json:"inProgress"`
        Received   int            `json:"received"`
        Remaining  int            `json:"remaining"`
        ByType     map[string]int `json:"byType"`
}

// ============================================================================
// Template helpers
// ============================================================================

type HowItWorksStep struct {
        Icon  string
        Title string
        Body  string
        Color string
}

type FAQItem struct {
        Q string
        A string
}

var howItWorksSteps = []HowItWorksStep{
        {"💬", "1 · Reply to claim", "Comment or DM with the kind of site you need. We confirm and assign your tracking ID.", "bg-stone-100 text-stone-700"},
        {"🔨", "2 · We build", "Design, copy, and dev happen in 2–3 working days. Watch your tracker for live progress.", "bg-amber-50 text-amber-700"},
        {"👁️", "3 · Review", "You get a preview link. We polish based on your feedback — no extra rounds of fees.", "bg-violet-50 text-violet-700"},
        {"🚀", "4 · Launch", "Final site delivered. Share it, ship it, scale it. It's yours, free.", "bg-emerald-50 text-emerald-700"},
}

var faqs = []FAQItem{
        {"Is it really free? What's the catch?", "Yes, 100% free. No hidden fees, no upsells. The catch is simple: this is a public credibility-building exercise. I want to showcase speed and help thousands of people launch something without money being a barrier. Your site is yours — code, design, the works."},
        {"How long does delivery take?", "2–3 working days from when your build slot opens. Most simple sites (landing pages, portfolios, waitlists) ship in 2. E-commerce and blog builds occasionally take the full 3. You'll see the estimated delivery date in your tracker."},
        {"How do I get a tracking ID?", "Reply or comment on the original post with the kind of site you want. Once we confirm your slot, you'll receive a tracking ID (looks like 10K-ABC123). Bookmark this page and use the tracker above anytime to see live progress."},
        {"What if I already have a site?", "Drop your current link or an inspiration site in the replies. We'll review it and ship a faster, cleaner version. The tracker still applies — you'll see your rebuild move from received → in progress → delivered."},
        {"Can I request changes after delivery?", "Yes. Every site includes one round of polish during the review stage, plus one post-delivery tweak round. If you need ongoing work after that, we can talk about a paid retainer — but the initial site is fully yours, free."},
        {"Who owns the code and content?", "You do. Once delivered, the site, the code, and any placeholder copy are yours to use, modify, host, and share however you like. We retain the right to feature the work in our portfolio unless you ask us not to."},
        {"What happens to the first 10,000?", "First come, first served. Once 10,000 sites have been claimed, the free offer closes. Your tracker will always show your queue position so you know exactly when your build starts."},
}

func getStatusInfo(s RequestStatus) StatusInfo {
        for _, info := range StatusFlow {
                if info.Key == s {
                        return info
                }
        }
        return StatusFlow[0]
}

func getProgressForStatus(s RequestStatus) int {
        switch s {
        case StatusReceived:
                return 5
        case StatusQueued:
                return 20
        case StatusInProgress:
                return 55
        case StatusReview:
                return 85
        case StatusDelivered:
                return 100
        }
        return 0
}

func getSiteTypeMeta(t SiteType) SiteTypeMeta {
        for _, m := range SiteTypes {
                if m.Value == t {
                        return m
                }
        }
        return SiteTypes[len(SiteTypes)-1]
}

func pct(total, goal int) float64 {
        if goal == 0 {
                return 0
        }
        return float64(total) / float64(goal) * 100
}

func statusBadgeClass(s RequestStatus) string {
        switch s {
        case StatusReceived:
                return "bg-stone-100 text-stone-700"
        case StatusQueued:
                return "bg-teal-50 text-teal-700"
        case StatusInProgress:
                return "bg-amber-50 text-amber-700"
        case StatusReview:
                return "bg-violet-50 text-violet-700"
        case StatusDelivered:
                return "bg-emerald-50 text-emerald-700"
        }
        return "bg-stone-100 text-stone-700"
}

func statusDotClass(s RequestStatus) string {
        switch s {
        case StatusReceived:
                return "bg-stone-400"
        case StatusQueued:
                return "bg-teal-500"
        case StatusInProgress:
                return "bg-amber-500"
        case StatusReview:
                return "bg-violet-500"
        case StatusDelivered:
                return "bg-emerald-500"
        }
        return "bg-stone-400"
}

func timelineRing(s RequestStatus) string {
        switch s {
        case StatusReceived:
                return "ring-stone-300 text-stone-500"
        case StatusQueued:
                return "ring-teal-300 text-teal-500"
        case StatusInProgress:
                return "ring-amber-300 text-amber-500"
        case StatusReview:
                return "ring-violet-300 text-violet-500"
        case StatusDelivered:
                return "ring-emerald-300 text-emerald-500"
        }
        return "ring-stone-300 text-stone-500"
}

func timelineText(s RequestStatus) string {
        switch s {
        case StatusQueued:
                return "text-teal-500"
        case StatusInProgress:
                return "text-amber-500"
        case StatusReview:
                return "text-violet-500"
        case StatusDelivered:
                return "text-emerald-500"
        }
        return "text-stone-500"
}

func formatDate(t time.Time) string {
        if t.IsZero() {
                return "—"
        }
        return t.Format("Mon, Jan 2")
}

func formatDateStr(s string) string {
        if s == "" {
                return "—"
        }
        t, err := time.Parse("2006-01-02", s)
        if err != nil {
                return s
        }
        return t.Format("Mon, Jan 2")
}

func lowerStr(s string) string      { return strings.ToLower(s) }
func upperStr(s string) string      { return strings.ToUpper(s) }
func hasPrefixStr(s, p string) bool { return strings.HasPrefix(s, p) }

func dict(values ...interface{}) (map[string]interface{}, error) {
        if len(values)%2 != 0 {
                return nil, fmt.Errorf("dict: odd number of args")
        }
        m := make(map[string]interface{}, len(values)/2)
        for i := 0; i < len(values); i += 2 {
                s, ok := values[i].(string)
                if !ok {
                        return nil, fmt.Errorf("dict: key %d not string", i)
                }
                m[s] = values[i+1]
        }
        return m, nil
}

// loadTemplates parses all embedded HTML templates with helper functions.
func loadTemplates() (*template.Template, error) {
        sub, err := fs.Sub(templatesFS, "templates")
        if err != nil {
                return nil, err
        }
        funcs := template.FuncMap{
                "formatDate":       formatDate,
                "formatDateStr":    formatDateStr,
                "statusInfo":       getStatusInfo,
                "siteTypeMeta":     getSiteTypeMeta,
                "siteTypesList":    func() []SiteTypeMeta { return SiteTypes },
                "statusFlowList":   func() []StatusInfo { return StatusFlow },
                "howItWorksSteps":  func() []HowItWorksStep { return howItWorksSteps },
                "faqs":             func() []FAQItem { return faqs },
                "pct":              pct,
                "statusBadgeClass": statusBadgeClass,
                "statusDotClass":   statusDotClass,
                "timelineRing":     timelineRing,
                "timelineText":     timelineText,
                "lower":            lowerStr,
                "upper":            upperStr,
                "hasPrefix":        hasPrefixStr,
                "sub":              func(a, b int) int { return a - b },
                "add":              func(a, b int) int { return a + b },
                "dict":             dict,
        }
        return template.New("").Funcs(funcs).ParseFS(sub, "*.html")
}
