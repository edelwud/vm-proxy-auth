package tenant

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/edelwud/vm-proxy-auth/internal/config"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// mockLogger implements domain.Logger for testing.
type mockLogger struct{}

func (m *mockLogger) Debug(msg string, fields ...domain.Field)  {}
func (m *mockLogger) Info(msg string, fields ...domain.Field)   {}
func (m *mockLogger) Warn(msg string, fields ...domain.Field)   {}
func (m *mockLogger) Error(msg string, fields ...domain.Field)  {}
func (m *mockLogger) With(fields ...domain.Field) domain.Logger { return m }

func TestPromQLTenantInjector_SingleTenant(t *testing.T) {
	logger := &mockLogger{}
	injector := NewPromQLTenantInjector(logger)

	cfg := &config.UpstreamConfig{
		TenantLabel:  "vm_account_id",
		ProjectLabel: "vm_project_id",
		UseProjectID: false,
	}

	vmTenants := []domain.VMTenant{
		{AccountID: "1000"},
	}

	query := "up"
	result, err := injector.InjectTenantLabels(query, vmTenants, cfg)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	expected := `up{vm_account_id="1000"}`
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestPromQLTenantInjector_SingleTenantWithProject(t *testing.T) {
	logger := &mockLogger{}
	injector := NewPromQLTenantInjector(logger)

	cfg := &config.UpstreamConfig{
		TenantLabel:  "vm_account_id",
		ProjectLabel: "vm_project_id",
		UseProjectID: true,
	}

	vmTenants := []domain.VMTenant{
		{AccountID: "1000", ProjectID: "proj1"},
	}

	query := "up"
	result, err := injector.InjectTenantLabels(query, vmTenants, cfg)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	expected := `up{vm_account_id="1000",vm_project_id="proj1"}`
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestPromQLTenantInjector_MultipleTenants(t *testing.T) {
	logger := &mockLogger{}
	injector := NewPromQLTenantInjector(logger)

	cfg := &config.UpstreamConfig{
		TenantLabel:  "vm_account_id",
		ProjectLabel: "vm_project_id",
		UseProjectID: false,
	}

	vmTenants := []domain.VMTenant{
		{AccountID: "1000"},
		{AccountID: "2000"},
		{AccountID: "3000"},
	}

	query := "up"
	result, err := injector.InjectTenantLabels(query, vmTenants, cfg)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Check that result contains regex pattern and all IDs (order may vary)
	if !strings.Contains(result, "vm_account_id=~") {
		t.Errorf("Expected result to contain regex pattern, got %q", result)
	}
	
	expectedIDs := []string{"1000", "2000", "3000"}
	for _, id := range expectedIDs {
		if !strings.Contains(result, id) {
			t.Errorf("Expected result to contain ID %s, got %q", id, result)
		}
	}
}

func TestPromQLTenantInjector_MultipleTenantsWithProjects(t *testing.T) {
	logger := &mockLogger{}
	injector := NewPromQLTenantInjector(logger)

	cfg := &config.UpstreamConfig{
		TenantLabel:  "vm_account_id",
		ProjectLabel: "vm_project_id",
		UseProjectID: true,
	}

	vmTenants := []domain.VMTenant{
		{AccountID: "1000", ProjectID: "proj1"},
		{AccountID: "2000", ProjectID: "proj2"},
		{AccountID: "3000"}, // No project ID
	}

	query := "up"
	result, err := injector.InjectTenantLabels(query, vmTenants, cfg)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should create regex patterns for both account and project IDs
	// Account IDs: all three accounts (order may vary)
	// Project IDs: only proj1 and proj2 (3000 has no project)
	
	// Check that result contains account filter pattern
	if !strings.Contains(result, "vm_account_id=~") {
		t.Errorf("Expected result to contain account ID filter, got %q", result)
	}
	
	// Check all account IDs are present
	expectedAccountIDs := []string{"1000", "2000", "3000"}
	for _, id := range expectedAccountIDs {
		if !strings.Contains(result, id) {
			t.Errorf("Expected result to contain account ID %s, got %q", id, result)
		}
	}

	// Check that result contains project filter pattern
	if !strings.Contains(result, "vm_project_id=~") {
		t.Errorf("Expected result to contain project ID filter, got %q", result)
	}
	
	// Check expected project IDs are present (only proj1 and proj2)
	expectedProjectIDs := []string{"proj1", "proj2"}
	for _, id := range expectedProjectIDs {
		if !strings.Contains(result, id) {
			t.Errorf("Expected result to contain project ID %s, got %q", id, result)
		}
	}
}

func TestPromQLTenantInjector_ComplexQuery(t *testing.T) {
	logger := &mockLogger{}
	injector := NewPromQLTenantInjector(logger)

	cfg := &config.UpstreamConfig{
		TenantLabel:  "vm_account_id",
		ProjectLabel: "vm_project_id",
		UseProjectID: false,
	}

	vmTenants := []domain.VMTenant{
		{AccountID: "1000"},
		{AccountID: "2000"},
	}

	query := `rate(http_requests_total{status="200"}[5m]) + rate(http_requests_total{status="500"}[5m])`
	result, err := injector.InjectTenantLabels(query, vmTenants, cfg)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Both metrics should have tenant filters (but the exact order may vary)
	// Count how many times vm_account_id appears
	accountFilterCount := strings.Count(result, "vm_account_id=~")

	if accountFilterCount != 2 {
		t.Errorf("Expected account filter to appear 2 times in complex query, got %d times. Result: %q", accountFilterCount, result)
	}

	// Verify both account IDs are present in the result
	expectedIDs := []string{"1000", "2000"}
	for _, id := range expectedIDs {
		if !strings.Contains(result, id) {
			t.Errorf("Expected result to contain account ID %s, got %q", id, result)
		}
	}
}

func TestPromQLTenantInjector_ExistingLabels(t *testing.T) {
	logger := &mockLogger{}
	injector := NewPromQLTenantInjector(logger)

	cfg := &config.UpstreamConfig{
		TenantLabel:  "vm_account_id",
		ProjectLabel: "vm_project_id",
		UseProjectID: false,
	}

	vmTenants := []domain.VMTenant{
		{AccountID: "1000"},
		{AccountID: "2000"},
	}

	// Query already has vm_account_id - should not be modified
	query := `up{vm_account_id="1500",job="prometheus"}`
	result, err := injector.InjectTenantLabels(query, vmTenants, cfg)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should remain unchanged since tenant label already exists
	// Check that both labels are present (order might vary)
	if !strings.Contains(result, `vm_account_id="1500"`) {
		t.Errorf("Expected vm_account_id label to remain unchanged, got %q", result)
	}
	if !strings.Contains(result, `job="prometheus"`) {
		t.Errorf("Expected job label to remain, got %q", result)
	}
}

func TestPromQLTenantInjector_MixedExistingLabels(t *testing.T) {
	logger := &mockLogger{}
	injector := NewPromQLTenantInjector(logger)

	cfg := &config.UpstreamConfig{
		TenantLabel:  "vm_account_id",
		ProjectLabel: "vm_project_id",
		UseProjectID: false,
	}

	vmTenants := []domain.VMTenant{
		{AccountID: "1000"},
		{AccountID: "2000"},
	}

	// First metric has tenant label, second doesn't
	query := `up{vm_account_id="1500"} + down`
	result, err := injector.InjectTenantLabels(query, vmTenants, cfg)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// First metric should remain unchanged, second should get tenant filter
	if !strings.Contains(result, `up{vm_account_id="1500"}`) {
		t.Errorf("Expected first metric to remain unchanged, got: %q", result)
	}

	// Check that second metric gets tenant filter (order may vary)
	if !strings.Contains(result, "down{vm_account_id=~") {
		t.Errorf("Expected second metric to get tenant filter, got: %q", result)
	}
	
	// Check that both expected IDs are in the result
	expectedIDs := []string{"1000", "2000"}
	downPartStart := strings.Index(result, "down{")
	if downPartStart == -1 {
		t.Errorf("Expected to find 'down{' in result, got: %q", result)
	} else {
		downPart := result[downPartStart:]
		for _, id := range expectedIDs {
			if !strings.Contains(downPart, id) {
				t.Errorf("Expected down metric to contain ID %s, got: %q", id, result)
			}
		}
	}
}

func TestPromQLTenantInjector_NoTenants(t *testing.T) {
	logger := &mockLogger{}
	injector := NewPromQLTenantInjector(logger)

	cfg := &config.UpstreamConfig{
		TenantLabel:  "vm_account_id",
		ProjectLabel: "vm_project_id",
		UseProjectID: false,
	}

	vmTenants := []domain.VMTenant{}

	query := "up"
	_, err := injector.InjectTenantLabels(query, vmTenants, cfg)
	if err == nil {
		t.Fatal("Expected error when no tenants provided")
	}

	if !errors.Is(err, domain.ErrNoVMTenantsForFiltering) {
		t.Errorf("Expected domain.ErrNoVMTenantsForFiltering, got: %v", err)
	}
}

func TestPromQLTenantInjector_InvalidQuery(t *testing.T) {
	logger := &mockLogger{}
	injector := NewPromQLTenantInjector(logger)

	cfg := &config.UpstreamConfig{
		TenantLabel:  "vm_account_id",
		ProjectLabel: "vm_project_id",
		UseProjectID: false,
	}

	vmTenants := []domain.VMTenant{
		{AccountID: "1000"},
	}

	// Invalid PromQL query
	query := "up{invalid=syntax"
	_, err := injector.InjectTenantLabels(query, vmTenants, cfg)
	if err == nil {
		t.Fatal("Expected error for invalid PromQL query")
	}

	if !strings.Contains(err.Error(), "failed to parse PromQL query") {
		t.Errorf("Expected parse error, got: %v", err)
	}
}

func TestPromQLTenantInjector_SingleTenantRegexEscape(t *testing.T) {
	logger := &mockLogger{}
	injector := NewPromQLTenantInjector(logger)

	cfg := &config.UpstreamConfig{
		TenantLabel:  "vm_account_id",
		ProjectLabel: "vm_project_id",
		UseProjectID: false,
	}

	// Test account ID that could be problematic in regex
	vmTenants := []domain.VMTenant{
		{AccountID: "1000.test"},
	}

	query := "up"
	result, err := injector.InjectTenantLabels(query, vmTenants, cfg)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Single tenant should use exact match, not regex
	expected := `up{vm_account_id="1000.test"}`
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestPromQLTenantInjector_ProjectIDOnlyMode(t *testing.T) {
	logger := &mockLogger{}
	injector := NewPromQLTenantInjector(logger)

	cfg := &config.UpstreamConfig{
		TenantLabel:  "vm_account_id",
		ProjectLabel: "vm_project_id",
		UseProjectID: true,
	}

	// All tenants have both account and project IDs
	vmTenants := []domain.VMTenant{
		{AccountID: "1000", ProjectID: "proj1"},
		{AccountID: "2000", ProjectID: "proj2"},
	}

	query := "up"
	result, err := injector.InjectTenantLabels(query, vmTenants, cfg)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should have both account and project filters (order may vary)
	// Check for account filter
	if !strings.Contains(result, "vm_account_id=~") {
		t.Errorf("Expected result to contain account filter, got %q", result)
	}

	// Check for project filter
	if !strings.Contains(result, "vm_project_id=~") {
		t.Errorf("Expected result to contain project filter, got %q", result)
	}

	// Check all expected IDs are present
	expectedAccountIDs := []string{"1000", "2000"}
	expectedProjectIDs := []string{"proj1", "proj2"}

	for _, id := range expectedAccountIDs {
		if !strings.Contains(result, id) {
			t.Errorf("Expected result to contain account ID %s, got %q", id, result)
		}
	}

	for _, id := range expectedProjectIDs {
		if !strings.Contains(result, id) {
			t.Errorf("Expected result to contain project ID %s, got %q", id, result)
		}
	}
}

// Benchmark tests
func BenchmarkPromQLTenantInjector_SingleTenant(b *testing.B) {
	logger := &mockLogger{}
	injector := NewPromQLTenantInjector(logger)

	cfg := &config.UpstreamConfig{
		TenantLabel:  "vm_account_id",
		ProjectLabel: "vm_project_id",
		UseProjectID: false,
	}

	vmTenants := []domain.VMTenant{
		{AccountID: "1000"},
	}

	query := "up"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := injector.InjectTenantLabels(query, vmTenants, cfg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestPromQLTenantInjector_DuplicateDeduplication(t *testing.T) {
	logger := &mockLogger{}
	injector := NewPromQLTenantInjector(logger)

	cfg := &config.UpstreamConfig{
		TenantLabel:  "vm_account_id",
		ProjectLabel: "vm_project_id",
		UseProjectID: false,
	}

	// Create tenants with duplicate account IDs
	vmTenants := []domain.VMTenant{
		{AccountID: "1000"},
		{AccountID: "2000"},
		{AccountID: "1000"}, // Duplicate
		{AccountID: "3000"},
		{AccountID: "2000"}, // Duplicate
		{AccountID: "1000"}, // Another duplicate
	}

	query := "up"
	result, err := injector.InjectTenantLabels(query, vmTenants, cfg)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should only contain unique account IDs
	// The exact order might vary due to map iteration, so we check for presence
	uniqueIDs := []string{"1000", "2000", "3000"}
	
	for _, id := range uniqueIDs {
		if !strings.Contains(result, id) {
			t.Errorf("Expected result to contain account ID %s, got %q", id, result)
		}
	}

	// Should not contain extra duplicates - count occurrences of each ID
	for _, id := range uniqueIDs {
		count := strings.Count(result, id)
		if count != 1 {
			t.Errorf("Expected account ID %s to appear exactly once, appeared %d times in %q", id, count, result)
		}
	}
}

func TestPromQLTenantInjector_DuplicateDeduplicationWithProjects(t *testing.T) {
	logger := &mockLogger{}
	injector := NewPromQLTenantInjector(logger)

	cfg := &config.UpstreamConfig{
		TenantLabel:  "vm_account_id",
		ProjectLabel: "vm_project_id",
		UseProjectID: true,
	}

	// Create tenants with duplicate account and project IDs
	vmTenants := []domain.VMTenant{
		{AccountID: "1000", ProjectID: "proj1"},
		{AccountID: "2000", ProjectID: "proj2"},
		{AccountID: "1000", ProjectID: "proj1"}, // Complete duplicate
		{AccountID: "3000", ProjectID: "proj1"}, // Same project, different account
		{AccountID: "2000", ProjectID: "proj3"}, // Same account, different project
		{AccountID: "1000", ProjectID: "proj2"}, // Mixed duplicates
	}

	query := "up"
	result, err := injector.InjectTenantLabels(query, vmTenants, cfg)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Check unique account IDs
	uniqueAccountIDs := []string{"1000", "2000", "3000"}
	for _, id := range uniqueAccountIDs {
		if !strings.Contains(result, id) {
			t.Errorf("Expected result to contain account ID %s, got %q", id, result)
		}
	}

	// Check unique project IDs  
	uniqueProjectIDs := []string{"proj1", "proj2", "proj3"}
	for _, id := range uniqueProjectIDs {
		if !strings.Contains(result, id) {
			t.Errorf("Expected result to contain project ID %s, got %q", id, result)
		}
	}

	// Verify no duplicates by counting pattern occurrences
	// Each unique ID should appear exactly once in the regex pattern
	for _, id := range uniqueAccountIDs {
		count := strings.Count(result, id)
		if count != 1 {
			t.Errorf("Expected account ID %s to appear exactly once, appeared %d times in %q", id, count, result)
		}
	}

	for _, id := range uniqueProjectIDs {
		count := strings.Count(result, id)
		if count != 1 {
			t.Errorf("Expected project ID %s to appear exactly once, appeared %d times in %q", id, count, result)
		}
	}
}

func TestPromQLTenantInjector_EmptyAndDuplicateHandling(t *testing.T) {
	logger := &mockLogger{}
	injector := NewPromQLTenantInjector(logger)

	cfg := &config.UpstreamConfig{
		TenantLabel:  "vm_account_id",
		ProjectLabel: "vm_project_id",
		UseProjectID: true,
	}

	// Mix of valid, empty, and duplicate values
	vmTenants := []domain.VMTenant{
		{AccountID: "1000", ProjectID: "proj1"},
		{AccountID: "", ProjectID: "proj2"},     // Empty account
		{AccountID: "2000", ProjectID: ""},      // Empty project
		{AccountID: "1000", ProjectID: "proj1"}, // Exact duplicate
		{AccountID: "3000", ProjectID: "proj2"}, // Project duplicate
		{AccountID: "", ProjectID: ""},          // Both empty
	}

	query := "up"
	result, err := injector.InjectTenantLabels(query, vmTenants, cfg)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should only contain non-empty, unique IDs
	expectedAccountIDs := []string{"1000", "2000", "3000"}
	expectedProjectIDs := []string{"proj1", "proj2"}

	for _, id := range expectedAccountIDs {
		if !strings.Contains(result, id) {
			t.Errorf("Expected result to contain account ID %s, got %q", id, result)
		}
	}

	for _, id := range expectedProjectIDs {
		if !strings.Contains(result, id) {
			t.Errorf("Expected result to contain project ID %s, got %q", id, result)
		}
	}

	// Verify no empty strings made it through
	if strings.Contains(result, `""`) {
		t.Errorf("Result should not contain empty strings, got %q", result)
	}
}

func BenchmarkPromQLTenantInjector_MultipleTenants(b *testing.B) {
	logger := &mockLogger{}
	injector := NewPromQLTenantInjector(logger)

	cfg := &config.UpstreamConfig{
		TenantLabel:  "vm_account_id",
		ProjectLabel: "vm_project_id",
		UseProjectID: true,
	}

	// Create many tenants to test performance
	vmTenants := make([]domain.VMTenant, 10)
	for i := 0; i < 10; i++ {
		vmTenants[i] = domain.VMTenant{
			AccountID: fmt.Sprintf("acc%d", i+1000),
			ProjectID: fmt.Sprintf("proj%d", i),
		}
	}

	query := `rate(http_requests_total{status="200"}[5m])`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := injector.InjectTenantLabels(query, vmTenants, cfg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPromQLTenantInjector_ManyDuplicateTenants(b *testing.B) {
	logger := &mockLogger{}
	injector := NewPromQLTenantInjector(logger)

	cfg := &config.UpstreamConfig{
		TenantLabel:  "vm_account_id",
		ProjectLabel: "vm_project_id",
		UseProjectID: true,
	}

	// Create many tenants with lots of duplicates (simulate multiple tenant mappings)
	vmTenants := make([]domain.VMTenant, 100)
	for i := 0; i < 100; i++ {
		// Only 5 unique account IDs, but 100 tenant entries
		accountID := fmt.Sprintf("acc%d", i%5+1000)
		projectID := fmt.Sprintf("proj%d", i%3) // Only 3 unique project IDs
		vmTenants[i] = domain.VMTenant{
			AccountID: accountID,
			ProjectID: projectID,
		}
	}

	query := `rate(http_requests_total{status="200"}[5m])`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := injector.InjectTenantLabels(query, vmTenants, cfg)
		if err != nil {
			b.Fatal(err)
		}
	}
}