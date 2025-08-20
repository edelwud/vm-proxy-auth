package proxy

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"
)

// WriteProcessor handles tenant injection for write operations
type WriteProcessor struct {
	logger       *logrus.Logger
	tenantLabel  string
}

// NewWriteProcessor creates a new write processor
func NewWriteProcessor(logger *logrus.Logger, tenantLabel string) *WriteProcessor {
	return &WriteProcessor{
		logger:      logger,
		tenantLabel: tenantLabel,
	}
}

// ProcessWriteData injects tenant labels into write data
func (wp *WriteProcessor) ProcessWriteData(data []byte, tenantID string, format string) ([]byte, error) {
	if tenantID == "" || tenantID == "*" {
		return data, nil // No tenant injection needed
	}

	switch format {
	case "prometheus":
		return wp.processPrometheusFormat(data, tenantID)
	case "csv":
		return wp.processCSVFormat(data, tenantID)
	case "native":
		return wp.processNativeFormat(data, tenantID)
	case "remote_write":
		return wp.processRemoteWriteFormat(data, tenantID)
	default:
		// Default to Prometheus format
		return wp.processPrometheusFormat(data, tenantID)
	}
}

// processPrometheusFormat injects tenant labels into Prometheus exposition format
func (wp *WriteProcessor) processPrometheusFormat(data []byte, tenantID string) ([]byte, error) {
	// Don't process if tenant is empty or wildcard
	if tenantID == "" || tenantID == "*" {
		return data, nil
	}
	
	var result bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(data))
	
	tenantLabelPair := fmt.Sprintf(`%s="%s"`, wp.tenantLabel, tenantID)
	
	// Regex to match metric lines (not comments or empty lines)
	metricRegex := regexp.MustCompile(`^([a-zA-Z_:][a-zA-Z0-9_:]*(?:\{[^}]*\})?\s+[^\s]+(?:\s+\d+)?)$`)
	
	for scanner.Scan() {
		line := scanner.Text()
		
		// Skip comments and empty lines
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			result.WriteString(line + "\n")
			continue
		}
		
		// Check if this is a metric line
		if metricRegex.MatchString(line) {
			modifiedLine := wp.injectTenantIntoMetricLine(line, tenantLabelPair)
			result.WriteString(modifiedLine + "\n")
		} else {
			result.WriteString(line + "\n")
		}
	}
	
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error processing Prometheus format data: %w", err)
	}
	
	return result.Bytes(), nil
}

// injectTenantIntoMetricLine injects tenant label into a single metric line
func (wp *WriteProcessor) injectTenantIntoMetricLine(line, tenantLabelPair string) string {
	// Parse metric line: metric_name{labels} value timestamp
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return line
	}
	
	metricPart := parts[0]
	rest := strings.Join(parts[1:], " ")
	
	// Check if metric already has labels
	if strings.Contains(metricPart, "{") {
		// Metric has existing labels: metric_name{existing_labels}
		closeBraceIndex := strings.LastIndex(metricPart, "}")
		if closeBraceIndex == -1 {
			// Malformed metric, return as-is
			return line
		}
		
		beforeBrace := metricPart[:closeBraceIndex]
		afterBrace := metricPart[closeBraceIndex:]
		
		// Check if tenant label already exists
		if strings.Contains(beforeBrace, wp.tenantLabel+"=") {
			// Tenant label already exists, don't modify
			return line
		}
		
		// Add comma if there are existing labels
		separator := ""
		if !strings.HasSuffix(beforeBrace, "{") {
			separator = ","
		}
		
		return beforeBrace + separator + tenantLabelPair + afterBrace + " " + rest
	} else {
		// Metric has no labels: metric_name
		return metricPart + "{" + tenantLabelPair + "} " + rest
	}
}

// processCSVFormat injects tenant labels into CSV format
func (wp *WriteProcessor) processCSVFormat(data []byte, tenantID string) ([]byte, error) {
	// CSV format: metric_name,label1=value1;label2=value2,timestamp,value
	var result bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(data))
	
	for scanner.Scan() {
		line := scanner.Text()
		
		if strings.TrimSpace(line) == "" {
			result.WriteString(line + "\n")
			continue
		}
		
		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			// Invalid CSV line, keep as-is
			result.WriteString(line + "\n")
			continue
		}
		
		metricName := parts[0]
		labels := parts[1]
		rest := strings.Join(parts[2:], ",")
		
		// Inject tenant label into labels
		tenantLabel := wp.tenantLabel + "=" + tenantID
		if strings.TrimSpace(labels) == "" {
			labels = tenantLabel
		} else {
			// Check if tenant label already exists
			if !strings.Contains(labels, wp.tenantLabel+"=") {
				labels = labels + ";" + tenantLabel
			}
		}
		
		result.WriteString(metricName + "," + labels + "," + rest + "\n")
	}
	
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error processing CSV format data: %w", err)
	}
	
	return result.Bytes(), nil
}

// processNativeFormat handles VictoriaMetrics native format
func (wp *WriteProcessor) processNativeFormat(data []byte, tenantID string) ([]byte, error) {
	// VictoriaMetrics native format is binary, so we can't easily inject labels
	// For now, we'll assume the client should handle tenant injection
	// or we'd need to implement binary protocol parsing
	
	wp.logger.WithFields(logrus.Fields{
		"tenant_id": tenantID,
		"format":    "native",
	}).Warn("Native format write detected - tenant injection not implemented for binary format")
	
	return data, nil
}

// processRemoteWriteFormat handles Prometheus remote write format
func (wp *WriteProcessor) processRemoteWriteFormat(data []byte, tenantID string) ([]byte, error) {
	// Remote write format is protobuf-based, which would require protobuf parsing
	// For now, we'll log and return data as-is
	// In production, you'd want to implement proper protobuf handling
	
	wp.logger.WithFields(logrus.Fields{
		"tenant_id": tenantID,
		"format":    "remote_write",
	}).Warn("Remote write format detected - tenant injection not implemented for protobuf format")
	
	return data, nil
}

// DetectWriteFormat attempts to detect the format of write data
func (wp *WriteProcessor) DetectWriteFormat(data []byte, contentType string) string {
	// Check content type first
	switch {
	case strings.Contains(contentType, "application/x-protobuf"):
		return "remote_write"
	case strings.Contains(contentType, "text/csv"):
		return "csv"
	case strings.Contains(contentType, "application/x-vm-native"):
		return "native"
	default:
		// Analyze data content
		dataStr := string(data[:min(len(data), 1024)]) // Look at first 1KB
		
		// CSV format detection
		if strings.Contains(dataStr, ",") && strings.Count(dataStr, ",") >= 2 {
			lines := strings.Split(dataStr, "\n")
			for _, line := range lines {
				if strings.TrimSpace(line) != "" {
					parts := strings.Split(line, ",")
					if len(parts) >= 3 {
						return "csv"
					}
					break
				}
			}
		}
		
		// Default to Prometheus exposition format
		return "prometheus"
	}
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}