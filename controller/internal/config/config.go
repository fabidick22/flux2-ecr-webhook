package config

import (
	"os"
	"strings"
	"time"
)

// Config holds all controller configuration, populated from environment variables
// injected by the Helm chart's ConfigMap.
type Config struct {
	FluxNamespace     string
	WebhookBaseURL    string
	ScanAllNamespaces bool
	IncludeNamespaces []string
	ExcludeNamespaces []string
	ExcludeAnnotation string
	AWSRegion         string
	AWSLambdaName     string
	AWSSQSName        string
	AWSAppName        string
	ResyncInterval    string
}

// FromEnv builds a Config by reading environment variables.
// All variables are set by the Helm chart ConfigMap via envFrom.
func FromEnv() Config {
	return Config{
		FluxNamespace:     getEnv("FLUX_NAMESPACE", "flux-system"),
		WebhookBaseURL:    getEnv("FLUX_WEBHOOK_BASE_URL", ""),
		ScanAllNamespaces: getEnv("SCAN_ALL_NAMESPACES", "true") == "true",
		IncludeNamespaces: splitCSV(getEnv("SCAN_INCLUDE_NAMESPACES", "")),
		ExcludeNamespaces: splitCSV(getEnv("SCAN_EXCLUDE_NAMESPACES", "")),
		ExcludeAnnotation: getEnv("EXCLUDE_ANNOTATION", "ecr-webhook.io/skip"),
		AWSRegion:         getEnv("AWS_REGION", ""),
		AWSLambdaName:     getEnv("AWS_LAMBDA_NAME", "flux2-ecr-webhook"),
		AWSSQSName:        getEnv("AWS_SQS_NAME", "flux2-ecr-webhook-push-events"),
		AWSAppName:        getEnv("AWS_APP_NAME", "flux2-ecr-webhook"),
		ResyncInterval:    getEnv("RESYNC_INTERVAL", "5m"),
	}
}

// ResyncDuration parses the ResyncInterval string to time.Duration.
// Falls back to 5 minutes if the value is invalid.
func (c Config) ResyncDuration() time.Duration {
	d, err := time.ParseDuration(c.ResyncInterval)
	if err != nil {
		return 5 * time.Minute
	}
	return d
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	for _, part := range strings.Split(s, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
