package domain

// Core constants that are not specific to configuration modules.
const (
	// Test loop counts.
	DefaultTestRetries = 5
	DefaultTestCount   = 3
	DefaultBenchCount  = 10

	// Encryption constants.
	AESKeySize = 32

	// HTTP status codes.
	StatusInternalServerError = 500
	StatusBadRequest          = 400

	// Log parsing constants.
	DefaultFieldsPerKeyValue      = 2
	DefaultComponentNameMaxLength = 20
	DefaultColonSeparatorOffset   = 2

	// Commonly used log field keys.
	LogFieldComponent = "component"
	LogFieldError     = "error"
	LogFieldUserID    = "user_id"
	LogFieldRequestID = "request_id"
	LogFieldTenantID  = "tenant_id"

	// Memberlist roles.
	PeerRole = "peer"
)
