package config

import (
	"testing"
	"time"
)

func TestResyncDuration(t *testing.T) {
	tests := []struct {
		name     string
		interval string
		want     time.Duration
	}{
		{name: "valid 5m", interval: "5m", want: 5 * time.Minute},
		{name: "valid 1h", interval: "1h", want: time.Hour},
		{name: "valid 30s", interval: "30s", want: 30 * time.Second},
		{name: "invalid falls back to 5m", interval: "invalid", want: 5 * time.Minute},
		{name: "empty falls back to 5m", interval: "", want: 5 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Config{ResyncInterval: tt.interval}
			if got := c.ResyncDuration(); got != tt.want {
				t.Errorf("ResyncDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSplitCSV(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want []string
	}{
		{name: "empty", s: "", want: nil},
		{name: "single", s: "ns1", want: []string{"ns1"}},
		{name: "multiple", s: "ns1,ns2,ns3", want: []string{"ns1", "ns2", "ns3"}},
		{name: "with spaces", s: " ns1 , ns2 , ns3 ", want: []string{"ns1", "ns2", "ns3"}},
		{name: "trailing comma", s: "ns1,ns2,", want: []string{"ns1", "ns2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitCSV(tt.s)
			if len(got) != len(tt.want) {
				t.Fatalf("splitCSV(%q) = %v (len %d), want %v (len %d)", tt.s, got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitCSV(%q)[%d] = %q, want %q", tt.s, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestFromEnv(t *testing.T) {
	// Test defaults
	cfg := FromEnv()
	if cfg.FluxNamespace != "flux-system" {
		t.Errorf("FluxNamespace default = %q, want %q", cfg.FluxNamespace, "flux-system")
	}
	if !cfg.ScanAllNamespaces {
		t.Error("ScanAllNamespaces default should be true")
	}
	if cfg.ExcludeAnnotation != "ecr-webhook.io/skip" {
		t.Errorf("ExcludeAnnotation default = %q, want %q", cfg.ExcludeAnnotation, "ecr-webhook.io/skip")
	}
	if cfg.ResyncInterval != "5m" {
		t.Errorf("ResyncInterval default = %q, want %q", cfg.ResyncInterval, "5m")
	}

	// Test with env override
	t.Setenv("FLUX_NAMESPACE", "custom-ns")
	t.Setenv("SCAN_ALL_NAMESPACES", "false")
	cfg = FromEnv()
	if cfg.FluxNamespace != "custom-ns" {
		t.Errorf("FluxNamespace = %q, want %q", cfg.FluxNamespace, "custom-ns")
	}
	if cfg.ScanAllNamespaces {
		t.Error("ScanAllNamespaces should be false")
	}
}
