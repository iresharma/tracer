package podwatcher

import (
	"os"
	"path/filepath"
	"testing"
)

// withFakeServiceAccount points the package-level token/CA/namespace path
// vars at temp-dir fixtures for the duration of one test, restoring the
// originals afterward.
func withFakeServiceAccount(t *testing.T, token, ca, namespace string) {
	t.Helper()
	dir := t.TempDir()

	writeIfSet := func(name, content string) string {
		if content == "" {
			return filepath.Join(dir, name+"-missing")
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatalf("write fake %s: %v", name, err)
		}
		return p
	}

	origToken, origCA, origNS := tokenPath, caCertPath, namespacePath
	tokenPath = writeIfSet("token", token)
	caCertPath = writeIfSet("ca.crt", ca)
	namespacePath = writeIfSet("namespace", namespace)
	t.Cleanup(func() {
		tokenPath, caCertPath, namespacePath = origToken, origCA, origNS
	})
}

func TestLoadInClusterConfig(t *testing.T) {
	withFakeServiceAccount(t, "fake-token", "", "demo")
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.96.0.1")
	t.Setenv("KUBERNETES_SERVICE_PORT", "443")

	cfg, err := LoadInClusterConfig()
	if err != nil {
		t.Fatalf("LoadInClusterConfig: %v", err)
	}
	if cfg.BaseURL != "https://10.96.0.1:443" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.DefaultNamespace != "demo" {
		t.Errorf("DefaultNamespace = %q", cfg.DefaultNamespace)
	}
}

func TestLoadInClusterConfigMissingToken(t *testing.T) {
	withFakeServiceAccount(t, "", "", "demo")
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.96.0.1")
	t.Setenv("KUBERNETES_SERVICE_PORT", "443")

	if _, err := LoadInClusterConfig(); err == nil {
		t.Fatal("expected an error when the token file is missing")
	}
}

func TestLoadInClusterConfigMissingServiceEnv(t *testing.T) {
	withFakeServiceAccount(t, "fake-token", "", "demo")
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")

	if _, err := LoadInClusterConfig(); err == nil {
		t.Fatal("expected an error when KUBERNETES_SERVICE_HOST/PORT aren't set")
	}
}

func TestNewRequestAttachesCurrentToken(t *testing.T) {
	withFakeServiceAccount(t, "token-v1", "", "demo")
	cfg := &InClusterConfig{BaseURL: "https://example.invalid"}

	req, err := cfg.NewRequest("/api/v1/namespaces/demo/pods")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer token-v1" {
		t.Errorf("Authorization header = %q", got)
	}

	// Simulate kubelet rotating the token on disk; NewRequest must pick up
	// the new value, not something cached from the first call.
	if err := os.WriteFile(tokenPath, []byte("token-v2"), 0o600); err != nil {
		t.Fatalf("rewrite token: %v", err)
	}
	req2, err := cfg.NewRequest("/api/v1/namespaces/demo/pods")
	if err != nil {
		t.Fatalf("NewRequest (2nd): %v", err)
	}
	if got := req2.Header.Get("Authorization"); got != "Bearer token-v2" {
		t.Errorf("Authorization header after rotation = %q, want Bearer token-v2", got)
	}
}
