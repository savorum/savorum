package app

import "testing"

func TestHostingSupportURLUsesOnlySafeConfiguredOrigin(t *testing.T) {
	for _, issuer := range []string{"http://cloud.example", "https://user@cloud.example", "https://cloud.example/path", "https://cloud.example/?next=bad", "https://cloud.example/#bad", "//cloud.example"} {
		if got := hostingSupportURL(issuer); got != "" {
			t.Fatalf("issuer %q produced %q", issuer, got)
		}
	}

	if got := hostingSupportURL("https://cloud.example/"); got != "https://cloud.example/cloud/account/manage" {
		t.Fatal(got)
	}
}

func TestSelfHostedPhysicalWarningIsVisibleWithoutHostedCeiling(t *testing.T) {
	if got := combineStorageState("unknown", "warning", true); got != "warning" {
		t.Fatal(got)
	}

	if got := combineStorageState("unknown", "normal", true); got != "normal" {
		t.Fatal(got)
	}

	if got := combineStorageState("unknown", "warning", false); got != "unknown" {
		t.Fatal(got)
	}

	if got := combineStorageState("normal", "limited", true); got != "limited" {
		t.Fatal(got)
	}
}
