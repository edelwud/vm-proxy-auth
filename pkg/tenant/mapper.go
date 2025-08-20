package tenant

import (
	"fmt"
	"slices"
	"strings"
)

type Mapping struct {
	Groups   []string
	Tenants  []string
	ReadOnly bool
}

type Mapper struct {
	mappings      []Mapping
	defaultAccess []string
}

type AccessInfo struct {
	Tenants  []string
	ReadOnly bool
}

func NewMapper(mappings []Mapping) *Mapper {
	return &Mapper{
		mappings: mappings,
	}
}

func (m *Mapper) GetUserAccess(userGroups []string) (*AccessInfo, error) {
	if len(userGroups) == 0 {
		return &AccessInfo{
			Tenants:  []string{},
			ReadOnly: true,
		}, nil
	}

	var allTenants []string
	readOnly := true

	for _, mapping := range m.mappings {
		if hasAnyGroup(userGroups, mapping.Groups) {
			allTenants = append(allTenants, mapping.Tenants...)
			if !mapping.ReadOnly {
				readOnly = false
			}
		}
	}

	if len(allTenants) == 0 {
		return &AccessInfo{
			Tenants:  []string{},
			ReadOnly: true,
		}, nil
	}

	uniqueTenants := removeDuplicates(allTenants)

	return &AccessInfo{
		Tenants:  uniqueTenants,
		ReadOnly: readOnly,
	}, nil
}

func (m *Mapper) CanAccessTenant(userGroups []string, tenantID string) (bool, error) {
	access, err := m.GetUserAccess(userGroups)
	if err != nil {
		return false, err
	}

	if contains(access.Tenants, "*") {
		return true, nil
	}

	return contains(access.Tenants, tenantID), nil
}

func (m *Mapper) FilterTenantsInQuery(userGroups []string, query string) (string, error) {
	if strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("empty query provided")
	}

	access, err := m.GetUserAccess(userGroups)
	if err != nil {
		return "", fmt.Errorf("failed to get user access: %w", err)
	}

	if len(access.Tenants) == 0 {
		return "", fmt.Errorf("user has no access to any tenants")
	}

	if contains(access.Tenants, "*") {
		return query, nil
	}

	return addTenantFilter(query, access.Tenants), nil
}

func addTenantFilter(query string, allowedTenants []string) string {
	if len(allowedTenants) == 0 {
		return query
	}

	tenantFilter := buildTenantRegex(allowedTenants)

	// Build VictoriaMetrics tenant filter
	// For now, we only use vm_account_id. vm_project_id support can be added later if needed.
	vmFilter := fmt.Sprintf("vm_account_id=~\"%s\"", tenantFilter)

	if strings.Contains(query, "{") {
		return strings.Replace(query, "{", fmt.Sprintf("{%s,", vmFilter), 1)
	}

	metricNames := extractMetricNames(query)
	for _, metric := range metricNames {
		replacement := fmt.Sprintf("%s{%s}", metric, vmFilter)
		query = strings.Replace(query, metric, replacement, -1)
	}

	return query
}

func buildTenantRegex(tenants []string) string {
	if len(tenants) == 1 {
		return tenants[0]
	}
	return strings.Join(tenants, "|")
}

func extractMetricNames(query string) []string {
	var metrics []string

	// Simple regex to find metric names in PromQL queries
	// This looks for words that could be metric names, excluding operators and functions
	words := strings.FieldsFunc(query, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '(' || r == ')' || r == '[' || r == ']' || r == '{' || r == '}' || r == ',' || r == '+' || r == '-' || r == '*' || r == '/' || r == '%'
	})

	for _, word := range words {
		word = strings.TrimSpace(word)
		if isMetricName(word) && !contains(metrics, word) {
			metrics = append(metrics, word)
		}
	}

	return metrics
}

func isMetricName(word string) bool {
	if len(word) == 0 {
		return false
	}

	if strings.ContainsAny(word, "()[]{}=!<>~+*/-% ") {
		return false
	}

	if word[0] >= '0' && word[0] <= '9' {
		return false
	}

	reservedWords := []string{
		"and", "or", "unless", "by", "without", "on", "ignoring",
		"group_left", "group_right", "offset", "bool",
		"sum", "min", "max", "avg", "count", "stddev", "stdvar",
		"rate", "irate", "increase", "delta", "idelta",
	}

	return !contains(reservedWords, strings.ToLower(word))
}

func hasAnyGroup(userGroups, requiredGroups []string) bool {
	for _, userGroup := range userGroups {
		if slices.Contains(requiredGroups, userGroup) {
			return true
		}
	}
	return false
}

func contains(slice []string, item string) bool {
	return slices.Contains(slice, item)
}

func removeDuplicates(slice []string) []string {
	if len(slice) == 0 {
		return []string{}
	}

	keys := make(map[string]bool)
	var result []string

	for _, item := range slice {
		if !keys[item] {
			keys[item] = true
			result = append(result, item)
		}
	}

	return result
}
