package proxy

import (
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestWriteProcessor_ProcessPrometheusFormat(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	
	wp := NewWriteProcessor(logger, "tenant_id")

	tests := []struct {
		name     string
		input    string
		tenantID string
		want     string
	}{
		{
			name: "simple metric without labels",
			input: `http_requests_total 100 1234567890`,
			tenantID: "alpha",
			want: `http_requests_total{tenant_id="alpha"} 100 1234567890`,
		},
		{
			name: "metric with existing labels",
			input: `http_requests_total{method="GET",status="200"} 100 1234567890`,
			tenantID: "beta",
			want: `http_requests_total{method="GET",status="200",tenant_id="beta"} 100 1234567890`,
		},
		{
			name: "metric with tenant label already exists",
			input: `http_requests_total{tenant_id="existing",method="GET"} 100`,
			tenantID: "alpha",
			want: `http_requests_total{tenant_id="existing",method="GET"} 100`,
		},
		{
			name: "multiple metrics",
			input: `# HELP http_requests_total Total HTTP requests
# TYPE http_requests_total counter
http_requests_total{method="GET"} 100 1234567890
http_requests_total{method="POST"} 50 1234567890`,
			tenantID: "gamma",
			want: `# HELP http_requests_total Total HTTP requests
# TYPE http_requests_total counter
http_requests_total{method="GET",tenant_id="gamma"} 100 1234567890
http_requests_total{method="POST",tenant_id="gamma"} 50 1234567890`,
		},
		{
			name: "empty tenant should not modify",
			input: `http_requests_total 100`,
			tenantID: "",
			want: `http_requests_total 100`,
		},
		{
			name: "wildcard tenant should not modify",
			input: `http_requests_total 100`,
			tenantID: "*",
			want: `http_requests_total 100`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := wp.processPrometheusFormat([]byte(tt.input), tt.tenantID)
			if err != nil {
				t.Errorf("processPrometheusFormat() error = %v", err)
				return
			}

			resultStr := strings.TrimSpace(string(result))
			wantStr := strings.TrimSpace(tt.want)

			if resultStr != wantStr {
				t.Errorf("processPrometheusFormat() = %q, want %q", resultStr, wantStr)
			}
		})
	}
}

func TestWriteProcessor_InjectTenantIntoMetricLine(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	
	wp := NewWriteProcessor(logger, "tenant_id")
	tenantLabelPair := `tenant_id="test"`

	tests := []struct {
		name string
		line string
		want string
	}{
		{
			name: "metric without labels",
			line: "up 1 1234567890",
			want: "up{tenant_id=\"test\"} 1 1234567890",
		},
		{
			name: "metric with empty labels",
			line: "up{} 1",
			want: "up{tenant_id=\"test\"} 1",
		},
		{
			name: "metric with existing labels",
			line: "up{instance=\"localhost:9090\",job=\"prometheus\"} 1",
			want: "up{instance=\"localhost:9090\",job=\"prometheus\",tenant_id=\"test\"} 1",
		},
		{
			name: "metric with tenant label already exists",
			line: "up{tenant_id=\"existing\",job=\"test\"} 1",
			want: "up{tenant_id=\"existing\",job=\"test\"} 1",
		},
		{
			name: "malformed metric",
			line: "up{broken 1",
			want: "up{broken 1", // Should return as-is
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wp.injectTenantIntoMetricLine(tt.line, tenantLabelPair)
			if got != tt.want {
				t.Errorf("injectTenantIntoMetricLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWriteProcessor_ProcessCSVFormat(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	
	wp := NewWriteProcessor(logger, "tenant_id")

	tests := []struct {
		name     string
		input    string
		tenantID string
		want     string
	}{
		{
			name: "CSV without existing labels",
			input: "http_requests_total,,1234567890,100",
			tenantID: "alpha",
			want: "http_requests_total,tenant_id=alpha,1234567890,100",
		},
		{
			name: "CSV with existing labels",
			input: "http_requests_total,method=GET;status=200,1234567890,100",
			tenantID: "beta",
			want: "http_requests_total,method=GET;status=200;tenant_id=beta,1234567890,100",
		},
		{
			name: "CSV with tenant label already exists",
			input: "http_requests_total,tenant_id=existing;method=GET,1234567890,100",
			tenantID: "alpha",
			want: "http_requests_total,tenant_id=existing;method=GET,1234567890,100",
		},
		{
			name: "multiple CSV lines",
			input: `http_requests_total,method=GET,1234567890,100
http_requests_total,method=POST,1234567890,50`,
			tenantID: "gamma",
			want: `http_requests_total,method=GET;tenant_id=gamma,1234567890,100
http_requests_total,method=POST;tenant_id=gamma,1234567890,50`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := wp.processCSVFormat([]byte(tt.input), tt.tenantID)
			if err != nil {
				t.Errorf("processCSVFormat() error = %v", err)
				return
			}

			resultStr := strings.TrimSpace(string(result))
			wantStr := strings.TrimSpace(tt.want)

			if resultStr != wantStr {
				t.Errorf("processCSVFormat() = %q, want %q", resultStr, wantStr)
			}
		})
	}
}

func TestWriteProcessor_DetectWriteFormat(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	
	wp := NewWriteProcessor(logger, "tenant_id")

	tests := []struct {
		name        string
		data        string
		contentType string
		want        string
	}{
		{
			name:        "protobuf content type",
			data:        "binary data",
			contentType: "application/x-protobuf",
			want:        "remote_write",
		},
		{
			name:        "CSV content type",
			data:        "metric,labels,timestamp,value",
			contentType: "text/csv",
			want:        "csv",
		},
		{
			name:        "native content type",
			data:        "binary data",
			contentType: "application/x-vm-native",
			want:        "native",
		},
		{
			name:        "CSV data detection",
			data:        "http_requests_total,method=GET,1234567890,100",
			contentType: "text/plain",
			want:        "csv",
		},
		{
			name:        "Prometheus format default",
			data:        "http_requests_total{method=\"GET\"} 100 1234567890",
			contentType: "text/plain",
			want:        "prometheus",
		},
		{
			name:        "empty data defaults to prometheus",
			data:        "",
			contentType: "",
			want:        "prometheus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wp.DetectWriteFormat([]byte(tt.data), tt.contentType)
			if got != tt.want {
				t.Errorf("DetectWriteFormat() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWriteProcessor_ProcessWriteData(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	
	wp := NewWriteProcessor(logger, "tenant_id")

	tests := []struct {
		name     string
		data     string
		tenantID string
		format   string
		want     string
		wantErr  bool
	}{
		{
			name:     "prometheus format processing",
			data:     "http_requests_total 100",
			tenantID: "alpha",
			format:   "prometheus",
			want:     "http_requests_total{tenant_id=\"alpha\"} 100",
			wantErr:  false,
		},
		{
			name:     "CSV format processing",
			data:     "http_requests_total,,1234567890,100",
			tenantID: "beta",
			format:   "csv",
			want:     "http_requests_total,tenant_id=beta,1234567890,100",
			wantErr:  false,
		},
		{
			name:     "native format - no processing",
			data:     "binary data",
			tenantID: "gamma",
			format:   "native",
			want:     "binary data",
			wantErr:  false,
		},
		{
			name:     "remote write format - no processing",
			data:     "protobuf data",
			tenantID: "delta",
			format:   "remote_write",
			want:     "protobuf data",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := wp.ProcessWriteData([]byte(tt.data), tt.tenantID, tt.format)
			
			if (err != nil) != tt.wantErr {
				t.Errorf("ProcessWriteData() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			resultStr := strings.TrimSpace(string(result))
			wantStr := strings.TrimSpace(tt.want)

			if resultStr != wantStr {
				t.Errorf("ProcessWriteData() = %q, want %q", resultStr, wantStr)
			}
		})
	}
}