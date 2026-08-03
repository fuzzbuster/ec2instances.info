package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestProvidersJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runCLI([]string{"--json", "providers"}, &stdout, &stderr); code != exitSuccess {
		t.Fatalf("runCLI() code = %d, stderr = %q", code, stderr.String())
	}

	var result providersResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if len(result.Providers) != 11 {
		t.Fatalf("provider count = %d, want 11", len(result.Providers))
	}
	if result.Providers[0].Name != "aws" || result.Providers[10].Name != "hetzner" {
		t.Fatalf("unexpected provider order: %#v", result.Providers)
	}
	if result.Providers[7].RequiredEnv == nil || result.Providers[7].OptionalEnv == nil {
		t.Fatal("provider environment fields must encode as arrays")
	}
}

func TestScrapeRequiresProviders(t *testing.T) {
	t.Setenv("EC2INSTANCES_PROVIDERS", "")
	t.Setenv("ALLOWED_SERVICES", "")

	var stdout, stderr bytes.Buffer
	code := runCLI([]string{"--json", "scrape"}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("runCLI() code = %d, want %d", code, exitUsage)
	}

	var result scrapeResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if result.Status != "error" || result.Partial {
		t.Fatalf("unexpected result: %#v", result)
	}
	if !strings.Contains(result.Error, "at least one provider") {
		t.Fatalf("error = %q", result.Error)
	}
}

func TestSelectProvidersIsStableAndRejectsUnknown(t *testing.T) {
	selected, err := selectProviders("hetzner,aws,hetzner")
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 2 || selected[0].Name != "aws" || selected[1].Name != "hetzner" {
		t.Fatalf("unexpected selection: %#v", selected)
	}

	_, err = selectProviders("zeta,alpha")
	if err == nil || err.Error() != "unknown provider: alpha,zeta" {
		t.Fatalf("error = %v", err)
	}
}

func TestRunProvidersCollectsPartialFailure(t *testing.T) {
	selected := []provider{
		{Name: "ok", Run: func() error { return nil }},
		{Name: "bad", Run: func() error { return errors.New("failed") }},
	}
	succeeded, failed := runProviders(selected)
	if len(succeeded) != 1 || succeeded[0] != "ok" {
		t.Fatalf("succeeded = %#v", succeeded)
	}
	if len(failed) != 1 || failed[0].Name != "bad" || failed[0].Error != "failed" {
		t.Fatalf("failed = %#v", failed)
	}
}
