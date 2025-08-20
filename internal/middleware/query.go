package middleware

import (
	"net/http"
	"strings"

	"github.com/finlego/prometheus-oauth-gateway/pkg/tenant"
	"github.com/sirupsen/logrus"
)

type QueryFilterMiddleware struct {
	tenantMapper *tenant.Mapper
	logger       *logrus.Logger
}

func NewQueryFilterMiddleware(tenantMapper *tenant.Mapper, logger *logrus.Logger) *QueryFilterMiddleware {
	return &QueryFilterMiddleware{
		tenantMapper: tenantMapper,
		logger:       logger,
	}
}

func (m *QueryFilterMiddleware) FilterQuery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userCtx := GetUserContext(r)
		if userCtx == nil {
			writeErrorResponse(w, http.StatusUnauthorized, "User context not found")
			return
		}

		if !m.isQueryEndpoint(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		originalQuery := r.URL.Query().Get("query")
		if originalQuery == "" {
			originalQuery = m.extractQueryFromBody(r)
		}

		if originalQuery == "" {
			next.ServeHTTP(w, r)
			return
		}

		if len(userCtx.AllowedTenants) == 0 {
			m.logger.WithField("user_id", userCtx.UserID).Warn("User has no access to any tenants")
			writeErrorResponse(w, http.StatusForbidden, "No access to any tenants")
			return
		}

		filteredQuery, err := m.tenantMapper.FilterTenantsInQuery(userCtx.Groups, originalQuery)
		if err != nil {
			m.logger.WithError(err).WithFields(logrus.Fields{
				"user_id":        userCtx.UserID,
				"original_query": originalQuery,
			}).Error("Failed to filter query")
			writeErrorResponse(w, http.StatusForbidden, "Access denied")
			return
		}

		if filteredQuery != originalQuery {
			m.logger.WithFields(logrus.Fields{
				"user_id":         userCtx.UserID,
				"original_query":  originalQuery,
				"filtered_query":  filteredQuery,
				"allowed_tenants": userCtx.AllowedTenants,
			}).Debug("Query filtered for tenant access")

			r = m.updateRequestQuery(r, filteredQuery)
		}

		next.ServeHTTP(w, r)
	})
}

func (m *QueryFilterMiddleware) isQueryEndpoint(path string) bool {
	queryEndpoints := []string{
		"/api/v1/query",
		"/api/v1/query_range",
		"/api/v1/query_exemplars",
	}

	for _, endpoint := range queryEndpoints {
		if strings.HasPrefix(path, endpoint) {
			return true
		}
	}

	return false
}

func (m *QueryFilterMiddleware) extractQueryFromBody(r *http.Request) string {
	if r.Method != "POST" {
		return ""
	}

	contentType := r.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/x-www-form-urlencoded") {
		return ""
	}

	if err := r.ParseForm(); err != nil {
		m.logger.WithError(err).Warn("Failed to parse form data")
		return ""
	}

	return r.PostForm.Get("query")
}

func (m *QueryFilterMiddleware) updateRequestQuery(r *http.Request, newQuery string) *http.Request {
	if r.Method == "GET" {
		q := r.URL.Query()
		q.Set("query", newQuery)

		newURL := *r.URL
		newURL.RawQuery = q.Encode()

		newReq := *r
		newReq.URL = &newURL
		return &newReq
	}

	if r.Method == "POST" {
		if err := r.ParseForm(); err == nil {
			r.PostForm.Set("query", newQuery)
		}
	}

	return r
}
