package browser

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateTargetURL(t *testing.T) {
	valid := []string{
		"https://one.swjtu.edu.cn/",
		"http://localhost:8080/page",
	}
	for _, raw := range valid {
		if err := ValidateTargetURL(raw); err != nil {
			t.Errorf("ValidateTargetURL(%q) returned error: %v", raw, err)
		}
	}

	invalid := []string{
		"",
		"javascript:alert(1)",
		"file:///tmp/page.html",
		"https:///missing-host",
	}
	for _, raw := range invalid {
		if err := ValidateTargetURL(raw); err == nil {
			t.Errorf("ValidateTargetURL(%q) accepted invalid URL", raw)
		}
	}
}

func TestValidateSWJTUTargetURL(t *testing.T) {
	for _, raw := range []string{
		"https://one.swjtu.edu.cn/",
		"https://swjtu.edu.cn/",
		"http://localhost:8080/test",
	} {
		if err := ValidateSWJTUTargetURL(raw); err != nil {
			t.Errorf("ValidateSWJTUTargetURL(%q) returned error: %v", raw, err)
		}
	}
	for _, raw := range []string{
		"https://swjtu.edu.cn.evil.example/",
		"https://example.com/",
	} {
		if err := ValidateSWJTUTargetURL(raw); err == nil {
			t.Errorf("ValidateSWJTUTargetURL(%q) accepted unrelated host", raw)
		}
	}
}

func TestLooksLikeCASLogin(t *testing.T) {
	if !LooksLikeCASLogin("https://cas.swjtu.edu.cn/authserver/login?service=x") {
		t.Error("expected SWJTU CAS login URL to be detected")
	}
	if !LooksLikeCASLogin("https://identity.swjtu.edu.cn/login") {
		t.Error("expected identity.swjtu.edu.cn login URL to be detected")
	}
	for _, raw := range []string{
		"https://one.swjtu.edu.cn/login",
		"https://example.com/authserver/login",
		"not a URL",
	} {
		if LooksLikeCASLogin(raw) {
			t.Errorf("unexpected CAS login detection for %q", raw)
		}
	}
}

func TestDefaultProfileDir(t *testing.T) {
	dir, err := DefaultProfileDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(filepath.ToSlash(dir), "/swjtu-cli/browser-profile") {
		t.Fatalf("unexpected profile directory: %q", dir)
	}
}
