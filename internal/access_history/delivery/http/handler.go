// Package http exposes the access-history usecase over the admin REST API.
package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/access_history/usecase"
	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
)

// Per-request caps on caller-supplied lists. Prevents an oversized
// emails / subscription_ids parameter from ballooning the SQL IN
// clause or triggering pathological query plans.
const (
	maxEmailsPerSearch          = 50
	maxSubscriptionIDsPerSearch = 50
)

type Handler struct {
	uc      usecase.Usecase
	auditUC auditDomain.AuditLogUsecase // optional; nil disables audit
}

func NewHandler(uc usecase.Usecase) *Handler {
	return &Handler{uc: uc}
}

// SetAuditUC wires audit logging on after construction. Call before the
// admin route is registered if you want every admin query logged.
func (h *Handler) SetAuditUC(au auditDomain.AuditLogUsecase) {
	h.auditUC = au
}

// RegisterAdminRoutes: per-sub GET .../:id/access-history[/search] +
// cross-sub /access-history/search.
func (h *Handler) RegisterAdminRoutes(rg *gin.RouterGroup) {
	rg.GET("/subscriptions/:id/access-history", h.GetSubscriptionAccessHistory)
	rg.GET("/subscriptions/:id/access-history/search", h.SearchSubscriptionAccessHistory)
	rg.GET("/access-history/search", h.SearchGlobalAccessHistory)
}

func (h *Handler) GetSubscriptionAccessHistory(c *gin.Context) {
	subID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		respondErr(c, http.StatusBadRequest, "invalid subscription id")
		return
	}

	req, parseErr := parseHistoryRequest(c, uint(subID))
	if parseErr != nil {
		respondErr(c, http.StatusBadRequest, parseErr.Error())
		return
	}

	resp, err := h.uc.GetSubscriptionAccessHistory(c.Request.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, usecase.ErrInvalidRange), errors.Is(err, usecase.ErrRangeOutsideRetention):
			respondErr(c, http.StatusBadRequest, err.Error())
		case errors.Is(err, usecase.ErrSubscriptionEmpty):
			// Treat as success-with-empty so the panel can render an
			// empty state instead of an error banner.
			c.JSON(http.StatusOK, gin.H{"success": true, "data": emptyResponse(req)})
		default:
			respondErr(c, http.StatusInternalServerError, err.Error())
		}
		return
	}

	h.recordAudit(c, req)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": resp})
}

// recordAudit best-effort writes an audit row capturing who looked at which
// subscription's history and the requested window. Non-blocking — audit
// failures never affect the response. Skips entirely when no audit usecase
// is wired in (tests, lightweight deployments).
func (h *Handler) recordAudit(c *gin.Context, req usecase.Request) {
	if h.auditUC == nil {
		return
	}
	rawActorID, _ := c.Get("user_id")
	rawActorName, _ := c.Get("username")
	payload := map[string]any{
		"from":               req.From,
		"to":                 req.To,
		"granularity":        req.Granularity,
		"include_source_ips": req.IncludeSourceIPs,
		"node_ids":           req.NodeIDs,
	}
	rawJSON, _ := json.Marshal(payload)
	h.auditUC.Log(c.Request.Context(), &auditDomain.AuditLog{
		Action:     string(auditDomain.AuditSubViewAccessHistory),
		ActorID:    actorIDFromContext(rawActorID),
		ActorName:  stringFromContext(rawActorName),
		EntityType: "subscription",
		EntityID:   req.SubscriptionID,
		NewValues:  string(rawJSON),
		IPAddress:  c.ClientIP(),
		RequestID:  c.GetHeader("X-Request-ID"),
		Source:     "panel",
	})
}

// SearchSubscriptionAccessHistory exposes the global substring search across
// the subscription's hourly summaries. Same auth + retention semantics as
// GetSubscriptionAccessHistory; the only extra inputs are q + kinds + limit.
func (h *Handler) SearchSubscriptionAccessHistory(c *gin.Context) {
	subID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		respondErr(c, http.StatusBadRequest, "invalid subscription id")
		return
	}
	req, parseErr := parseSearchRequest(c, uint(subID))
	if parseErr != nil {
		respondErr(c, http.StatusBadRequest, parseErr.Error())
		return
	}

	resp, err := h.uc.SearchSubscriptionAccessLog(c.Request.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, usecase.ErrInvalidRange),
			errors.Is(err, usecase.ErrRangeOutsideRetention),
			errors.Is(err, usecase.ErrInvalidQuery):
			respondErr(c, http.StatusBadRequest, err.Error())
		case errors.Is(err, usecase.ErrSubscriptionEmpty):
			c.JSON(http.StatusOK, gin.H{"success": true, "data": emptySearchResponse(req)})
		default:
			respondErr(c, http.StatusInternalServerError, err.Error())
		}
		return
	}

	h.recordSearchAudit(c, req)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": resp})
}

func (h *Handler) recordSearchAudit(c *gin.Context, req usecase.SearchRequest) {
	if h.auditUC == nil {
		return
	}
	rawActorID, _ := c.Get("user_id")
	rawActorName, _ := c.Get("username")
	payload := map[string]any{
		"from":               req.From,
		"to":                 req.To,
		"query":              req.Query,
		"kinds":              req.Kinds,
		"include_source_ips": req.IncludeSourceIPs,
		"node_ids":           req.NodeIDs,
		"limit":              req.Limit,
	}
	rawJSON, _ := json.Marshal(payload)
	h.auditUC.Log(c.Request.Context(), &auditDomain.AuditLog{
		Action:     string(auditDomain.AuditSubSearchAccessHistory),
		ActorID:    actorIDFromContext(rawActorID),
		ActorName:  stringFromContext(rawActorName),
		EntityType: "subscription",
		EntityID:   req.SubscriptionID,
		NewValues:  string(rawJSON),
		IPAddress:  c.ClientIP(),
		RequestID:  c.GetHeader("X-Request-ID"),
		Source:     "panel",
	})
}

// SearchGlobalAccessHistory exposes the cross-subscription substring search.
// Always audits when the audit usecase is wired (the action reveals what
// admins are mining the request log for, and "I just looked everywhere"
// shouldn't be invisible).
func (h *Handler) SearchGlobalAccessHistory(c *gin.Context) {
	req, parseErr := parseGlobalSearchRequest(c)
	if parseErr != nil {
		respondErr(c, http.StatusBadRequest, parseErr.Error())
		return
	}

	resp, err := h.uc.SearchGlobalAccessLog(c.Request.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, usecase.ErrInvalidRange),
			errors.Is(err, usecase.ErrRangeOutsideRetention),
			errors.Is(err, usecase.ErrInvalidQuery):
			respondErr(c, http.StatusBadRequest, err.Error())
		default:
			respondErr(c, http.StatusInternalServerError, err.Error())
		}
		return
	}

	h.recordGlobalSearchAudit(c, req)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": resp})
}

func (h *Handler) recordGlobalSearchAudit(c *gin.Context, req usecase.GlobalSearchRequest) {
	if h.auditUC == nil {
		return
	}
	rawActorID, _ := c.Get("user_id")
	rawActorName, _ := c.Get("username")
	payload := map[string]any{
		"from":               req.From,
		"to":                 req.To,
		"query":              req.Query,
		"kinds":              req.Kinds,
		"include_source_ips": req.IncludeSourceIPs,
		"node_ids":           req.NodeIDs,
		"subscription_ids":   req.SubscriptionIDs,
		"emails":             req.Emails,
		"limit":              req.Limit,
	}
	rawJSON, _ := json.Marshal(payload)
	h.auditUC.Log(c.Request.Context(), &auditDomain.AuditLog{
		Action:     string(auditDomain.AuditAccessHistoryGlobalSearch),
		ActorID:    actorIDFromContext(rawActorID),
		ActorName:  stringFromContext(rawActorName),
		EntityType: "access_history",
		NewValues:  string(rawJSON),
		IPAddress:  c.ClientIP(),
		RequestID:  c.GetHeader("X-Request-ID"),
		Source:     "panel",
	})
}

func parseGlobalSearchRequest(c *gin.Context) (usecase.GlobalSearchRequest, error) {
	from, err := parseTimeRequired(c.Query("from"), "from")
	if err != nil {
		return usecase.GlobalSearchRequest{}, err
	}
	to, err := parseTimeRequired(c.Query("to"), "to")
	if err != nil {
		return usecase.GlobalSearchRequest{}, err
	}
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		return usecase.GlobalSearchRequest{}, errors.New("q is required")
	}

	req := usecase.GlobalSearchRequest{
		From:  from,
		To:    to,
		Query: q,
	}
	if v := c.Query("node_ids"); v != "" {
		ids, err := parseNodeIDs(v)
		if err != nil {
			return req, err
		}
		req.NodeIDs = ids
	}
	if v := c.Query("subscription_ids"); v != "" {
		ids, err := parseUintList(v, "subscription_ids")
		if err != nil {
			return req, err
		}
		if len(ids) > maxSubscriptionIDsPerSearch {
			return req, errors.New("subscription_ids exceeds limit of " + strconv.Itoa(maxSubscriptionIDsPerSearch))
		}
		req.SubscriptionIDs = ids
	}
	if v := c.Query("emails"); v != "" {
		emails := parseStringList(v)
		if len(emails) > maxEmailsPerSearch {
			return req, errors.New("emails exceeds limit of " + strconv.Itoa(maxEmailsPerSearch))
		}
		req.Emails = emails
	}
	if v := c.Query("kinds"); v != "" {
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			out = append(out, p)
		}
		req.Kinds = out
	}
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return req, errors.New("invalid limit")
		}
		req.Limit = n
	}
	if v := c.Query("include_ips"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return req, errors.New("invalid include_ips")
		}
		req.IncludeSourceIPs = b
	}
	return req, nil
}

// parseUintList parses a comma-separated list of unsigned ints. Empty entries
// skipped. fieldName is used in error messages so callers can disambiguate
// which param was bad.
func parseUintList(raw, fieldName string) ([]uint, error) {
	parts := strings.Split(raw, ",")
	out := make([]uint, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			return nil, errors.New("invalid " + fieldName + " entry: " + p)
		}
		out = append(out, uint(n))
	}
	return out, nil
}

// parseStringList parses a comma-separated list of strings. Whitespace is
// trimmed; empty entries dropped. No format validation — repo uses exact match.
func parseStringList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func actorIDFromContext(v any) uint {
	switch val := v.(type) {
	case uint:
		return val
	case int:
		return uint(val)
	case float64:
		return uint(val)
	}
	return 0
}

func stringFromContext(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// parseHistoryRequest converts the gin query string into a usecase.Request
// and surfaces any malformed inputs as 400-class errors via the returned
// error. Validation of the resolved range itself stays in the usecase.
func parseHistoryRequest(c *gin.Context, subID uint) (usecase.Request, error) {
	from, err := parseTimeRequired(c.Query("from"), "from")
	if err != nil {
		return usecase.Request{}, err
	}
	to, err := parseTimeRequired(c.Query("to"), "to")
	if err != nil {
		return usecase.Request{}, err
	}

	req := usecase.Request{
		SubscriptionID: subID,
		From:           from,
		To:             to,
		Granularity:    c.Query("granularity"),
	}

	if v := c.Query("node_ids"); v != "" {
		ids, err := parseNodeIDs(v)
		if err != nil {
			return req, err
		}
		req.NodeIDs = ids
	}

	if v := c.Query("top_n"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return req, errors.New("invalid top_n")
		}
		req.TopN = n
	}

	if v := c.Query("include_ips"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return req, errors.New("invalid include_ips")
		}
		req.IncludeSourceIPs = b
	}

	return req, nil
}

func parseTimeRequired(s, field string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New(field + " is required")
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, errors.New("invalid " + field + " (expected RFC3339)")
	}
	return t.UTC(), nil
}

func parseNodeIDs(raw string) ([]uint, error) {
	parts := strings.Split(raw, ",")
	out := make([]uint, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			return nil, errors.New("invalid node_ids entry: " + p)
		}
		out = append(out, uint(n))
	}
	return out, nil
}

func respondErr(c *gin.Context, code int, msg string) {
	c.JSON(code, gin.H{"success": false, "error": msg})
}

// emptyResponse mirrors the shape of usecase.Response for the
// no-accounts-yet case, so the frontend can treat it identically to a
// happy-path empty range.
func emptyResponse(req usecase.Request) usecase.Response {
	gran := req.Granularity
	if gran == "" {
		gran = "hour"
	}
	return usecase.Response{
		From:        req.From,
		To:          req.To,
		Granularity: gran,
	}
}

// emptySearchResponse mirrors usecase.SearchResponse for the no-accounts-yet
// case so the panel can render the standard empty state.
func emptySearchResponse(req usecase.SearchRequest) usecase.SearchResponse {
	return usecase.SearchResponse{
		From:  req.From,
		To:    req.To,
		Query: req.Query,
		Kinds: req.Kinds,
	}
}

// parseSearchRequest mirrors parseHistoryRequest for the search endpoint.
func parseSearchRequest(c *gin.Context, subID uint) (usecase.SearchRequest, error) {
	from, err := parseTimeRequired(c.Query("from"), "from")
	if err != nil {
		return usecase.SearchRequest{}, err
	}
	to, err := parseTimeRequired(c.Query("to"), "to")
	if err != nil {
		return usecase.SearchRequest{}, err
	}
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		return usecase.SearchRequest{}, errors.New("q is required")
	}

	req := usecase.SearchRequest{
		SubscriptionID: subID,
		From:           from,
		To:             to,
		Query:          q,
	}
	if v := c.Query("node_ids"); v != "" {
		ids, err := parseNodeIDs(v)
		if err != nil {
			return req, err
		}
		req.NodeIDs = ids
	}
	if v := c.Query("kinds"); v != "" {
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			out = append(out, p)
		}
		req.Kinds = out
	}
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return req, errors.New("invalid limit")
		}
		req.Limit = n
	}
	if v := c.Query("include_ips"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return req, errors.New("invalid include_ips")
		}
		req.IncludeSourceIPs = b
	}
	return req, nil
}
