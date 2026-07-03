package app

import (
        "database/sql"
        "encoding/json"
        "fmt"
        "html/template"
        "log"
        "net/http"
        "strings"
        "time"

        "github.com/go-chi/chi/v5"
        "github.com/google/uuid"
        "golang.org/x/time/rate"
)

// ============================================================================
// Render helpers
// ============================================================================

func (app *App) render(w http.ResponseWriter, name string, data interface{}) {
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        if err := app.tmpl.ExecuteTemplate(w, name, data); err != nil {
                log.Printf("template error %s: %v", name, err)
                http.Error(w, "render error", http.StatusInternalServerError)
        }
}

func (app *App) renderFragment(w http.ResponseWriter, name string, data interface{}) {
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        if err := app.tmpl.ExecuteTemplate(w, name, data); err != nil {
                log.Printf("fragment template error %s: %v", name, err)
                http.Error(w, "render error", http.StatusInternalServerError)
        }
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(status)
        json.NewEncoder(w).Encode(v)
}

// ============================================================================
// Public handlers
// ============================================================================

func (app *App) indexHandler(w http.ResponseWriter, r *http.Request) {
        // Stats from cache (1 DB query per 60s, not per page load)
        stats := app.getCachedStats()
        data := map[string]interface{}{
                "Stats":        stats,
                "InitialQuery": "",
        }

        // Deep-link: ?id=10K-XXX
        if id := r.URL.Query().Get("id"); id != "" {
                id = strings.ToUpper(strings.TrimSpace(id))
                if validateTrackingID(id) {
                        req, err := app.getCachedRequest(id)
                        if err == nil {
                                data["InitialRequest"] = req
                        }
                }
        }
        app.render(w, "index.html", data)
}

func (app *App) trackSearchHandler(w http.ResponseWriter, r *http.Request) {
        var id string
        if r.Method == http.MethodPost {
                r.ParseForm()
                id = strings.ToUpper(strings.TrimSpace(r.FormValue("trackingId")))
        } else {
                id = strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("id")))
        }

        if !validateTrackingID(id) {
                app.renderFragment(w, "fragment_error", map[string]string{
                        "Error": "Enter a valid tracking ID (e.g., 10K-ABC123).",
                })
                return
        }

        req, err := app.getCachedRequest(id)
        if err != nil {
                app.renderFragment(w, "fragment_error", map[string]string{
                        "Error": "No site found with that tracking ID. Double-check the code we sent you.",
                })
                return
        }

        app.renderFragment(w, "fragment_result", map[string]interface{}{
                "Request": req,
        })
}

func (app *App) trackHandler(w http.ResponseWriter, r *http.Request) {
        id := strings.ToUpper(strings.TrimSpace(chi.URLParam(r, "id")))
        if !validateTrackingID(id) {
                app.renderFragment(w, "fragment_error", map[string]string{
                        "Error": "Invalid tracking ID format.",
                })
                return
        }
        req, err := app.getCachedRequest(id)
        if err != nil {
                app.renderFragment(w, "fragment_error", map[string]string{
                        "Error": "No site found with that tracking ID.",
                })
                return
        }
        app.renderFragment(w, "fragment_result", map[string]interface{}{
                "Request": req,
        })
}

func (app *App) resetHandler(w http.ResponseWriter, r *http.Request) {
        app.renderFragment(w, "fragment_search", nil)
}

func (app *App) statsHandler(w http.ResponseWriter, r *http.Request) {
        stats := app.getCachedStats()
        app.renderFragment(w, "fragment_stats", map[string]interface{}{
                "Stats": stats,
        })
}

// ============================================================================
// Admin handlers (HTML)
// ============================================================================

func (app *App) adminLoginHandler(w http.ResponseWriter, r *http.Request) {
        // If already logged in, redirect to panel
        if cookie, err := r.Cookie("session"); err == nil && app.sessions.Valid(cookie.Value) {
                http.Redirect(w, r, "/admin/panel", http.StatusSeeOther)
                return
        }
        app.render(w, "admin_login.html", nil)
}

func (app *App) adminLoginPostHandler(w http.ResponseWriter, r *http.Request) {
        r.ParseForm()
        pw := r.FormValue("password")

        if !app.auth.VerifyPassword(pw) {
                w.WriteHeader(http.StatusUnauthorized)
                app.render(w, "admin_login.html", map[string]string{
                        "Error": "Wrong password.",
                })
                return
        }

        token, err := app.sessions.Create()
        if err != nil {
                http.Error(w, "Session creation failed", http.StatusInternalServerError)
                return
        }
        setSessionCookie(w, token, app.config.CookieSecure)
        http.Redirect(w, r, "/admin/panel", http.StatusSeeOther)
}

func (app *App) adminLogoutHandler(w http.ResponseWriter, r *http.Request) {
        if cookie, err := r.Cookie("session"); err == nil {
                app.sessions.Delete(cookie.Value)
        }
        clearSessionCookie(w)
        http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (app *App) adminPanelHandler(w http.ResponseWriter, r *http.Request) {
        requests, _ := app.listAllRequests()
        stats := app.getCachedStats()
        app.render(w, "admin_panel.html", map[string]interface{}{
                "Requests": requests,
                "Stats":    stats,
        })
}

func (app *App) adminListHandler(w http.ResponseWriter, r *http.Request) {
        requests, _ := app.listAllRequests()
        stats := app.getCachedStats()
        app.renderFragment(w, "fragment_admin_list", map[string]interface{}{
                "Requests": requests,
                "Stats":    stats,
        })
}

func (app *App) adminNewFormHandler(w http.ResponseWriter, r *http.Request) {
        app.renderFragment(w, "fragment_admin_form", map[string]interface{}{
                "Mode": "create",
                "Req":  &WebsiteRequest{},
        })
}

func (app *App) adminCreateHandler(w http.ResponseWriter, r *http.Request) {
        r.ParseForm()
        req, validationErr := app.buildRequestFromForm(r)
        if validationErr != nil {
                app.renderFragment(w, "fragment_admin_form", map[string]interface{}{
                        "Mode":  "create",
                        "Req":   req,
                        "Error": validationErr.Error(),
                })
                return
        }

        req.TrackingID = app.uniqueTrackingID()
        req.ID = uuid.NewString()
        req.CreatedAt = time.Now()
        req.UpdatedAt = time.Now()

        if req.Status == StatusReceived || req.Status == StatusQueued {
                n := app.countQueued() + 1
                req.QueuePosition = &n
        }
        // Use the admin-specified progress, or fall back to status-based default
        if req.Progress == 0 {
                req.Progress = getProgressForStatus(req.Status)
        }

        if err := app.insertRequest(req); err != nil {
                log.Printf("insert error: %v", err)
                app.renderFragment(w, "fragment_admin_form", map[string]interface{}{
                        "Mode":  "create",
                        "Req":   req,
                        "Error": "Failed to create request.",
                })
                return
        }

        // Invalidate cache + broadcast to all connected clients
        app.cache.InvalidateAll()
        app.hub.Broadcast(WSMessage{Type: "stats_updated"})
        app.hub.Broadcast(WSMessage{Type: "request_created", TrackingID: req.TrackingID})

        requests, _ := app.listAllRequests()
        stats := app.getCachedStats()
        app.renderFragment(w, "fragment_admin_list", map[string]interface{}{
                "Requests": requests,
                "Stats":    stats,
                "Flash":    fmt.Sprintf("Created %s", req.TrackingID),
        })
}

func (app *App) adminEditFormHandler(w http.ResponseWriter, r *http.Request) {
        id := chi.URLParam(r, "id")
        req, err := app.findRequestByID(id)
        if err != nil {
                http.Error(w, "not found", http.StatusNotFound)
                return
        }
        app.renderFragment(w, "fragment_admin_form", map[string]interface{}{
                "Mode": "edit",
                "Req":  req,
        })
}

func (app *App) adminUpdateHandler(w http.ResponseWriter, r *http.Request) {
        id := chi.URLParam(r, "id")
        existing, err := app.findRequestByID(id)
        if err != nil {
                http.Error(w, "not found", http.StatusNotFound)
                return
        }
        r.ParseForm()
        updates, validationErr := app.buildRequestFromForm(r)
        if validationErr != nil {
                app.renderFragment(w, "fragment_admin_form", map[string]interface{}{
                        "Mode":  "edit",
                        "Req":   existing,
                        "Error": validationErr.Error(),
                })
                return
        }

        existing.RequesterName = updates.RequesterName
        existing.RequesterEmail = updates.RequesterEmail
        existing.SiteType = updates.SiteType
        existing.SiteDescription = updates.SiteDescription
        existing.InspirationURL = updates.InspirationURL
        existing.Status = updates.Status
        if r.FormValue("progress") != "" {
                existing.Progress = updates.Progress
        } else {
                existing.Progress = getProgressForStatus(updates.Status)
        }
        existing.EstimatedDelivery = updates.EstimatedDelivery
        existing.DeliveredURL = updates.DeliveredURL
        existing.AdminNotes = updates.AdminNotes
        existing.UpdatedAt = time.Now()

        if existing.Status == StatusDelivered {
                existing.QueuePosition = nil
        }

        if err := app.updateRequest(existing); err != nil {
                log.Printf("update error: %v", err)
        }

        // Invalidate cache + broadcast
        app.cache.InvalidateStats()
        app.cache.InvalidateRequest(existing.TrackingID)
        app.hub.Broadcast(WSMessage{Type: "stats_updated"})
        app.hub.Broadcast(WSMessage{
                Type:       "request_updated",
                TrackingID: existing.TrackingID,
                Status:     string(existing.Status),
        })

        requests, _ := app.listAllRequests()
        stats := app.getCachedStats()
        app.renderFragment(w, "fragment_admin_list", map[string]interface{}{
                "Requests": requests,
                "Stats":    stats,
                "Flash":    fmt.Sprintf("Saved %s", existing.TrackingID),
        })
}

func (app *App) adminDeleteHandler(w http.ResponseWriter, r *http.Request) {
        id := chi.URLParam(r, "id")
        req, err := app.findRequestByID(id)
        if err != nil {
                http.Error(w, "not found", http.StatusNotFound)
                return
        }
        trackingID := req.TrackingID
        if err := app.deleteRequest(id); err != nil {
                log.Printf("delete error: %v", err)
        }

        app.cache.InvalidateAll()
        app.hub.Broadcast(WSMessage{Type: "stats_updated"})
        app.hub.Broadcast(WSMessage{Type: "request_deleted", TrackingID: trackingID})

        requests, _ := app.listAllRequests()
        stats := app.getCachedStats()
        app.renderFragment(w, "fragment_admin_list", map[string]interface{}{
                "Requests": requests,
                "Stats":    stats,
                "Flash":    "Deleted",
        })
}

// ============================================================================
// JSON API handlers
// ============================================================================

func (app *App) apiTrackHandler(w http.ResponseWriter, r *http.Request) {
        id := strings.ToUpper(strings.TrimSpace(chi.URLParam(r, "id")))
        if !validateTrackingID(id) {
                writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid tracking ID"})
                return
        }
        req, err := app.getCachedRequest(id)
        if err != nil {
                writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
                return
        }
        writeJSON(w, http.StatusOK, map[string]interface{}{"request": req})
}

func (app *App) apiStatsHandler(w http.ResponseWriter, r *http.Request) {
        stats := app.getCachedStats()
        writeJSON(w, http.StatusOK, stats)
}

func (app *App) apiListRequestsHandler(w http.ResponseWriter, r *http.Request) {
        requests, _ := app.listAllRequests()
        writeJSON(w, http.StatusOK, map[string]interface{}{"requests": requests})
}

func (app *App) apiCreateRequestHandler(w http.ResponseWriter, r *http.Request) {
        var body struct {
                RequesterName     string `json:"requesterName"`
                RequesterEmail    string `json:"requesterEmail"`
                SiteType          string `json:"siteType"`
                SiteDescription   string `json:"siteDescription"`
                InspirationURL    string `json:"inspirationUrl"`
                Status            string `json:"status"`
                EstimatedDelivery string `json:"estimatedDelivery"`
                AdminNotes        string `json:"adminNotes"`
        }
        if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
                writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad body"})
                return
        }
        // Validate
        if body.RequesterName == "" || body.RequesterEmail == "" || body.SiteDescription == "" {
                writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing required fields"})
                return
        }
        if !validateEmail(body.RequesterEmail) {
                writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid email"})
                return
        }
        if !validateSiteType(body.SiteType) {
                writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid site type"})
                return
        }
        status := RequestStatus(body.Status)
        if status == "" {
                status = StatusReceived
        }
        if !validateStatus(string(status)) {
                writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status"})
                return
        }

        req := &WebsiteRequest{
                ID:                uuid.NewString(),
                TrackingID:        app.uniqueTrackingID(),
                RequesterName:     truncateLen(body.RequesterName, maxNameLen),
                RequesterEmail:    truncateLen(strings.ToLower(body.RequesterEmail), maxEmailLen),
                SiteType:          SiteType(body.SiteType),
                SiteDescription:   truncateLen(body.SiteDescription, maxDescriptionLen),
                Status:            status,
                Progress:          getProgressForStatus(status),
                EstimatedDelivery: body.EstimatedDelivery,
                InspirationURL:    truncateLen(body.InspirationURL, maxURLLen),
                AdminNotes:        truncateLen(body.AdminNotes, maxNotesLen),
                CreatedAt:         time.Now(),
                UpdatedAt:         time.Now(),
        }
        if body.InspirationURL != "" && !validateURL(body.InspirationURL) {
                req.InspirationURL = ""
        }

        if req.Status == StatusReceived || req.Status == StatusQueued {
                n := app.countQueued() + 1
                req.QueuePosition = &n
        }

        if err := app.insertRequest(req); err != nil {
                writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
                return
        }

        app.cache.InvalidateAll()
        app.hub.Broadcast(WSMessage{Type: "stats_updated"})
        app.hub.Broadcast(WSMessage{Type: "request_created", TrackingID: req.TrackingID})

        writeJSON(w, http.StatusCreated, map[string]interface{}{"request": req})
}

func (app *App) apiUpdateRequestHandler(w http.ResponseWriter, r *http.Request) {
        id := chi.URLParam(r, "id")
        existing, err := app.findRequestByID(id)
        if err != nil {
                writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
                return
        }
        var body struct {
                Status            *string `json:"status"`
                Progress          *int    `json:"progress"`
                EstimatedDelivery *string `json:"estimatedDelivery"`
                DeliveredURL      *string `json:"deliveredUrl"`
                AdminNotes        *string `json:"adminNotes"`
        }
        if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
                writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad body"})
                return
        }
        if body.Status != nil && validateStatus(*body.Status) {
                existing.Status = RequestStatus(*body.Status)
                existing.Progress = getProgressForStatus(existing.Status)
                if existing.Status == StatusDelivered {
                        existing.QueuePosition = nil
                }
        }
        if body.Progress != nil {
                existing.Progress = *body.Progress
                if *body.Progress < 0 {
                        existing.Progress = 0
                }
                if *body.Progress > 100 {
                        existing.Progress = 100
                }
        }
        if body.EstimatedDelivery != nil {
                existing.EstimatedDelivery = *body.EstimatedDelivery
        }
        if body.DeliveredURL != nil {
                existing.DeliveredURL = truncateLen(*body.DeliveredURL, maxURLLen)
        }
        if body.AdminNotes != nil {
                existing.AdminNotes = truncateLen(*body.AdminNotes, maxNotesLen)
        }
        existing.UpdatedAt = time.Now()

        if err := app.updateRequest(existing); err != nil {
                writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
                return
        }

        app.cache.InvalidateStats()
        app.cache.InvalidateRequest(existing.TrackingID)
        app.hub.Broadcast(WSMessage{Type: "stats_updated"})
        app.hub.Broadcast(WSMessage{
                Type:       "request_updated",
                TrackingID: existing.TrackingID,
                Status:     string(existing.Status),
        })

        writeJSON(w, http.StatusOK, map[string]interface{}{"request": existing})
}

func (app *App) apiDeleteRequestHandler(w http.ResponseWriter, r *http.Request) {
        id := chi.URLParam(r, "id")
        req, err := app.findRequestByID(id)
        if err != nil {
                writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
                return
        }
        trackingID := req.TrackingID
        if err := app.deleteRequest(id); err != nil {
                writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
                return
        }

        app.cache.InvalidateAll()
        app.hub.Broadcast(WSMessage{Type: "stats_updated"})
        app.hub.Broadcast(WSMessage{Type: "request_deleted", TrackingID: trackingID})

        writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (app *App) apiSeedHandler(w http.ResponseWriter, r *http.Request) {
        if err := app.seedDemoData(); err != nil {
                writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
                return
        }
        app.cache.InvalidateAll()
        app.hub.Broadcast(WSMessage{Type: "stats_updated"})
        requests, _ := app.listAllRequests()
        writeJSON(w, http.StatusOK, map[string]interface{}{
                "seeded":   len(requests),
                "requests": requests,
        })
}

// ============================================================================
// Cache-aware data access (minimizes DB queries)
// ============================================================================

// getCachedStats returns stats from cache or fetches from DB.
// With 10,000 concurrent users, this results in 1 DB query per 60s.
func (app *App) getCachedStats() *StatsResponse {
        if s := app.cache.GetStats(); s != nil {
                return s
        }
        stats, err := app.computeStats()
        if err != nil {
                return &StatsResponse{Goal: 10000, ByType: map[string]int{}}
        }
        app.cache.SetStats(stats)
        return stats
}

// getCachedRequest returns a request from cache or fetches from DB.
// TTL is 10s — invalidated immediately on mutation via WebSocket broadcast.
func (app *App) getCachedRequest(trackingID string) (*WebsiteRequest, error) {
        if req := app.cache.GetRequest(trackingID); req != nil {
                return req, nil
        }
        req, err := app.findRequestByTrackingID(trackingID)
        if err != nil {
                return nil, err
        }
        app.cache.SetRequest(req)
        return req, nil
}

// ============================================================================
// Form parsing + validation
// ============================================================================

func (app *App) buildRequestFromForm(r *http.Request) (*WebsiteRequest, error) {
        req := &WebsiteRequest{
                RequesterName:     truncateLen(strings.TrimSpace(r.FormValue("requesterName")), maxNameLen),
                RequesterEmail:    truncateLen(strings.ToLower(strings.TrimSpace(r.FormValue("requesterEmail"))), maxEmailLen),
                SiteType:          SiteType(r.FormValue("siteType")),
                SiteDescription:   truncateLen(strings.TrimSpace(r.FormValue("siteDescription")), maxDescriptionLen),
                InspirationURL:    truncateLen(strings.TrimSpace(r.FormValue("inspirationUrl")), maxURLLen),
                Status:            RequestStatus(r.FormValue("status")),
                EstimatedDelivery: strings.TrimSpace(r.FormValue("estimatedDelivery")),
                DeliveredURL:      truncateLen(strings.TrimSpace(r.FormValue("deliveredUrl")), maxURLLen),
                AdminNotes:        truncateLen(strings.TrimSpace(r.FormValue("adminNotes")), maxNotesLen),
        }

        if req.Status == "" {
                req.Status = StatusReceived
        }
        if p := r.FormValue("progress"); p != "" {
                fmt.Sscanf(p, "%d", &req.Progress)
        }

        // Validation
        if req.RequesterName == "" {
                return req, fmt.Errorf("Name is required")
        }
        if !validateEmail(req.RequesterEmail) {
                return req, fmt.Errorf("Valid email is required")
        }
        if req.SiteDescription == "" {
                return req, fmt.Errorf("Description is required")
        }
        if !validateSiteType(string(req.SiteType)) {
                return req, fmt.Errorf("Invalid site type")
        }
        if !validateStatus(string(req.Status)) {
                return req, fmt.Errorf("Invalid status")
        }
        if req.InspirationURL != "" && !validateURL(req.InspirationURL) {
                req.InspirationURL = ""
        }

        return req, nil
}

// Suppress unused import warning
var _ = sql.ErrNoRows
var _ = rate.Limit(0)
var _ = template.HTML("")
