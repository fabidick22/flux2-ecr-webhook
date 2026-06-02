package main

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestManagerOptions_DisablesSecretCache(t *testing.T) {
	opts := managerOptions(":8080", ":8081", false)

	if opts.Client.Cache == nil {
		t.Fatal("Client.Cache must not be nil — Secret cache bypass is required to prevent OOM")
	}

	var found bool
	for _, obj := range opts.Client.Cache.DisableFor {
		if _, ok := obj.(*corev1.Secret); ok {
			found = true
			break
		}
	}
	if !found {
		t.Error("corev1.Secret must be in Client.Cache.DisableFor to avoid caching all cluster secrets")
	}
}

func TestManagerOptions_SetsScheme(t *testing.T) {
	opts := managerOptions(":8080", ":8081", false)

	if opts.Scheme == nil {
		t.Fatal("Scheme must not be nil")
	}
	if opts.Scheme != scheme {
		t.Error("expected managerOptions to use the package-level scheme")
	}
}

func TestManagerOptions_LeaderElection(t *testing.T) {
	opts := managerOptions(":8080", ":8081", true)
	if !opts.LeaderElection {
		t.Error("expected LeaderElection to be true when passed as true")
	}

	opts = managerOptions(":8080", ":8081", false)
	if opts.LeaderElection {
		t.Error("expected LeaderElection to be false when passed as false")
	}
}
