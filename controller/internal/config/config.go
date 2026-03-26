package config

import (
	"os"
	"strconv"
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
	CloudProvider     string
	AWSRegion         string
	AWSLambdaName     string
	AWSLambdaRuntime  string
	AWSLambdaTimeout  int32
	AWSSQSName        string
	AWSAppName                   string
	AWSManageInfra               bool
	AWSExistingRepoMappingSecret string
	AWSExistingSQSQueue          string
	AWSExistingEventRule         string
	ResyncInterval               string
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
		CloudProvider:     getEnv("CLOUD_PROVIDER", "aws"),
		AWSRegion:         getEnv("AWS_REGION", ""),
		AWSLambdaName:     getEnv("AWS_LAMBDA_NAME", "flux-webhook"),
		AWSLambdaRuntime:  getEnv("AWS_LAMBDA_RUNTIME", "python3.12"),
		AWSLambdaTimeout:  parseInt32(getEnv("AWS_LAMBDA_TIMEOUT", "30")),
		AWSSQSName:        getEnv("AWS_SQS_NAME", "flux-webhook-push-events"),
		AWSAppName:                   getEnv("AWS_APP_NAME", "flux-webhook"),
		AWSManageInfra:               getEnv("AWS_MANAGE_INFRASTRUCTURE", "true") == "true",
		AWSExistingRepoMappingSecret: getEnv("AWS_EXISTING_REPO_MAPPING_SECRET", ""),
		AWSExistingSQSQueue:          getEnv("AWS_EXISTING_SQS_QUEUE", ""),
		AWSExistingEventRule:         getEnv("AWS_EXISTING_EVENT_RULE", ""),
		ResyncInterval:               getEnv("RESYNC_INTERVAL", "5m"),
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

func parseInt32(s string) int32 {
	v, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return 30
	}
	return int32(v)
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
