package app

import (
	"database/sql"
	"log"
	"time"

	"github.com/google/uuid"
)

// ============================================================================
// Database query methods (all parameterized — SQL injection proof)
// ============================================================================

func (app *App) findRequestByTrackingID(id string) (*WebsiteRequest, error) {
	row := app.db.QueryRow(`
		SELECT id, tracking_id, requester_name, requester_email, site_type,
		       site_description, inspiration_url, status, progress,
		       estimated_delivery, delivered_url, admin_notes, queue_position,
		       created_at, updated_at
		FROM website_requests WHERE UPPER(tracking_id) = ?`, id)
	return scanRequest(row)
}

func (app *App) findRequestByID(id string) (*WebsiteRequest, error) {
	row := app.db.QueryRow(`
		SELECT id, tracking_id, requester_name, requester_email, site_type,
		       site_description, inspiration_url, status, progress,
		       estimated_delivery, delivered_url, admin_notes, queue_position,
		       created_at, updated_at
		FROM website_requests WHERE id = ?`, id)
	return scanRequest(row)
}

func (app *App) listAllRequests() ([]WebsiteRequest, error) {
	rows, err := app.db.Query(`
		SELECT id, tracking_id, requester_name, requester_email, site_type,
		       site_description, inspiration_url, status, progress,
		       estimated_delivery, delivered_url, admin_notes, queue_position,
		       created_at, updated_at
		FROM website_requests ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRequests(rows)
}

func (app *App) insertRequest(req *WebsiteRequest) error {
	_, err := app.db.Exec(`
		INSERT INTO website_requests
		(id, tracking_id, requester_name, requester_email, site_type, site_description,
		 inspiration_url, status, progress, estimated_delivery, delivered_url,
		 admin_notes, queue_position, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.ID, req.TrackingID, req.RequesterName, req.RequesterEmail,
		string(req.SiteType), req.SiteDescription, nullableString(req.InspirationURL),
		string(req.Status), req.Progress, nullableTime(req.EstimatedDelivery),
		nullableString(req.DeliveredURL), nullableString(req.AdminNotes),
		nullableInt(req.QueuePosition), req.CreatedAt, req.UpdatedAt,
	)
	return err
}

func (app *App) updateRequest(req *WebsiteRequest) error {
	_, err := app.db.Exec(`
		UPDATE website_requests SET
			requester_name = ?, requester_email = ?, site_type = ?, site_description = ?,
			inspiration_url = ?, status = ?, progress = ?, estimated_delivery = ?,
			delivered_url = ?, admin_notes = ?, queue_position = ?, updated_at = ?
		WHERE id = ?`,
		req.RequesterName, req.RequesterEmail, string(req.SiteType), req.SiteDescription,
		nullableString(req.InspirationURL), string(req.Status), req.Progress,
		nullableTime(req.EstimatedDelivery), nullableString(req.DeliveredURL),
		nullableString(req.AdminNotes), nullableInt(req.QueuePosition), time.Now(), req.ID,
	)
	return err
}

func (app *App) deleteRequest(id string) error {
	_, err := app.db.Exec("DELETE FROM website_requests WHERE id = ?", id)
	return err
}

func (app *App) computeStats() (*StatsResponse, error) {
	stats := &StatsResponse{Goal: 10000, ByType: map[string]int{}}
	if err := app.db.QueryRow("SELECT COUNT(*) FROM website_requests").Scan(&stats.Total); err != nil {
		return nil, err
	}
	app.db.QueryRow("SELECT COUNT(*) FROM website_requests WHERE status = 'delivered'").Scan(&stats.Delivered)
	app.db.QueryRow("SELECT COUNT(*) FROM website_requests WHERE status IN ('queued','in_progress','review')").Scan(&stats.InProgress)
	app.db.QueryRow("SELECT COUNT(*) FROM website_requests WHERE status = 'received'").Scan(&stats.Received)
	stats.Remaining = stats.Goal - stats.Total
	if stats.Remaining < 0 {
		stats.Remaining = 0
	}
	rows, err := app.db.Query("SELECT site_type, COUNT(*) FROM website_requests GROUP BY site_type")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var t string
			var c int
			rows.Scan(&t, &c)
			stats.ByType[t] = c
		}
	}
	return stats, nil
}

func (app *App) countQueued() int {
	var count int
	app.db.QueryRow("SELECT COUNT(*) FROM website_requests WHERE status IN ('received', 'queued')").Scan(&count)
	return count
}

// ============================================================================
// Scanning helpers
// ============================================================================

func scanRequest(row *sql.Row) (*WebsiteRequest, error) {
	var req WebsiteRequest
	var insURL, delURL, notes sql.NullString
	var estDel sql.NullTime
	var qPos sql.NullInt64
	err := row.Scan(
		&req.ID, &req.TrackingID, &req.RequesterName, &req.RequesterEmail,
		&req.SiteType, &req.SiteDescription, &insURL,
		&req.Status, &req.Progress, &estDel,
		&delURL, &notes, &qPos,
		&req.CreatedAt, &req.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if insURL.Valid {
		req.InspirationURL = insURL.String
	}
	if delURL.Valid {
		req.DeliveredURL = delURL.String
	}
	if notes.Valid {
		req.AdminNotes = notes.String
	}
	if estDel.Valid {
		req.EstimatedDelivery = estDel.Time.Format("2006-01-02")
	}
	if qPos.Valid {
		n := int(qPos.Int64)
		req.QueuePosition = &n
	}
	return &req, nil
}

func scanRequests(rows *sql.Rows) ([]WebsiteRequest, error) {
	var list []WebsiteRequest
	for rows.Next() {
		var req WebsiteRequest
		var insURL, delURL, notes sql.NullString
		var estDel sql.NullTime
		var qPos sql.NullInt64
		if err := rows.Scan(
			&req.ID, &req.TrackingID, &req.RequesterName, &req.RequesterEmail,
			&req.SiteType, &req.SiteDescription, &insURL,
			&req.Status, &req.Progress, &estDel,
			&delURL, &notes, &qPos,
			&req.CreatedAt, &req.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if insURL.Valid {
			req.InspirationURL = insURL.String
		}
		if delURL.Valid {
			req.DeliveredURL = delURL.String
		}
		if notes.Valid {
			req.AdminNotes = notes.String
		}
		if estDel.Valid {
			req.EstimatedDelivery = estDel.Time.Format("2006-01-02")
		}
		if qPos.Valid {
			n := int(qPos.Int64)
			req.QueuePosition = &n
		}
		list = append(list, req)
	}
	return list, rows.Err()
}

func nullableString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullableTime(dateStr string) sql.NullTime {
	if dateStr == "" {
		return sql.NullTime{}
	}
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t, Valid: true}
}

func nullableInt(n *int) sql.NullInt64 {
	if n == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*n), Valid: true}
}

// ============================================================================
// ID generation (no confusing characters: no 0/O, 1/I/L)
// ============================================================================

const idAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func generateTrackingID() string {
	b := make([]byte, 6)
	now := time.Now().UnixNano()
	for i := range b {
		b[i] = idAlphabet[(now>>uint(i*7))%int64(len(idAlphabet))]
	}
	for i := range b {
		b[i] = idAlphabet[(int64(b[i])^now>>uint(i+3))%int64(len(idAlphabet))]
		if b[i] < 0 {
			b[i] = idAlphabet[(int64(b[i])*-1)%int64(len(idAlphabet))]
		}
	}
	return "10K-" + string(b)
}

func (app *App) uniqueTrackingID() string {
	for attempts := 0; attempts < 5; attempts++ {
		id := generateTrackingID()
		var exists string
		err := app.db.QueryRow("SELECT tracking_id FROM website_requests WHERE tracking_id = ?", id).Scan(&exists)
		if err == sql.ErrNoRows {
			return id
		}
	}
	return generateTrackingID()
}

// ============================================================================
// Seed data
// ============================================================================

type demoSeed struct {
	TrackingID    string
	Name          string
	Email         string
	SiteType      SiteType
	Description   string
	Inspiration   string
	Status        RequestStatus
	Progress      int
	QueuePosition *int
	AdminNotes    string
	DeliveredURL  string
}

var demoSeeds = []demoSeed{
	{"10K-DEMO1", "Maya Chen", "maya@example.com", SiteTypePortfolio, "Personal portfolio for a freelance brand designer. Needs case study pages, about, and contact form.", "https://pentagram.com", StatusInProgress, 55, nil, "Started wireframes. Hero treatment approved. Building case study grid next.", ""},
	{"10K-DEMO2", "Jordan Patel", "jordan@example.com", SiteTypeSaaS, "Waitlist landing page for an AI scheduling assistant. Email capture + early-access benefits.", "https://linear.app", StatusQueued, 20, intPtr(3), "Slotted for build after current two landing pages.", ""},
	{"10K-DEMO3", "Sara Okafor", "sara@example.com", SiteTypeBlog, "Minimal blog about slow living and cooking. Categories, RSS, dark mode.", "https://maggieappleton.com", StatusDelivered, 100, nil, "Delivered ahead of schedule. Two rounds of edits applied.", "https://example.com/sara-blog"},
	{"10K-DEMO4", "Liam Rivera", "liam@example.com", SiteTypeEcommerce, "Small storefront for handmade ceramics, ~12 products. Stripe checkout, shipping calculator.", "https://fawnshoppe.com", StatusReview, 85, nil, "Awaiting client approval on product photography placement and copy.", ""},
	{"10K-DEMO5", "Priya Singh", "priya@example.com", SiteTypeLanding, "One-page landing for a fitness bootcamp launching next month. Lead form + schedule.", "https://stripe.com", StatusReceived, 5, intPtr(7), "", ""},
}

func intPtr(n int) *int { return &n }

func (app *App) ensureSeedData() {
	var count int
	app.db.QueryRow("SELECT COUNT(*) FROM website_requests").Scan(&count)
	if count > 0 {
		return
	}
	log.Println("Seeding demo data…")
	if err := app.seedDemoData(); err != nil {
		log.Printf("seed error: %v", err)
	}
}

func (app *App) seedDemoData() error {
	// Delete existing demo entries first
	for _, d := range demoSeeds {
		app.db.Exec("DELETE FROM website_requests WHERE tracking_id = ?", d.TrackingID)
	}

	now := time.Now()
	for i, d := range demoSeeds {
		created := now.AddDate(0, 0, -(i + 1))
		delivery := created.AddDate(0, 0, 3)
		req := &WebsiteRequest{
			ID:                uuid.NewString(),
			TrackingID:        d.TrackingID,
			RequesterName:     d.Name,
			RequesterEmail:    d.Email,
			SiteType:          d.SiteType,
			SiteDescription:   d.Description,
			Status:            d.Status,
			Progress:          d.Progress,
			QueuePosition:     d.QueuePosition,
			CreatedAt:         created,
			UpdatedAt:         now,
			EstimatedDelivery: delivery.Format("2006-01-02"),
			InspirationURL:    d.Inspiration,
			AdminNotes:        d.AdminNotes,
			DeliveredURL:      d.DeliveredURL,
		}
		if err := app.insertRequest(req); err != nil {
			return err
		}
	}
	return nil
}
