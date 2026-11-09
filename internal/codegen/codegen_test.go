package codegen

import (
	"regexp"
	"strings"
	"testing"
)

var (
	organizationPattern = regexp.MustCompile(`^ORG-[A-HJ-NP-Z2-9]{6}$`)
	packagePattern      = regexp.MustCompile(`^PKG-[A-HJ-NP-Z2-9]{6}$`)
	participantPattern  = regexp.MustCompile(`^PSN-[A-HJ-NP-Z2-9]{8}$`)
	certificatePattern  = regexp.MustCompile(`^EDU-[A-HJ-NP-Z2-9]{12}$`)
	tokenPattern        = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

func TestAccessCodeFormats(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		pattern *regexp.Regexp
	}{
		{"organization", OrganizationCode(), organizationPattern},
		{"package", PackageCode(), packagePattern},
		{"participant", ParticipantCode(), participantPattern},
		{"certificate", CertificateCode(), certificatePattern},
	}
	for _, testCase := range cases {
		if !testCase.pattern.MatchString(testCase.value) {
			t.Errorf("%s code %q does not match %s", testCase.name, testCase.value, testCase.pattern)
		}
	}
}

func TestCertificateCodeIsOpaqueAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for index := 0; index < 100; index++ {
		code := CertificateCode()
		if seen[code] {
			t.Fatalf("duplicate certificate code generated: %s", code)
		}
		seen[code] = true
		if strings.Contains(code, "EDU-0") {
			t.Fatalf("certificate alphabet must not contain ambiguous characters: %s", code)
		}
	}
}

func TestRandomTokenAndHash(t *testing.T) {
	token := RandomToken()
	if !tokenPattern.MatchString(token) {
		t.Fatalf("token %q is not 64 lowercase hex characters", token)
	}
	hash := HashToken(token)
	if !tokenPattern.MatchString(hash) {
		t.Fatalf("hash %q is not 64 lowercase hex characters", hash)
	}
	if hash != HashToken(token) {
		t.Fatal("hash must be deterministic")
	}
	if hash == HashToken(RandomToken()) {
		t.Fatal("different tokens must not produce the same hash")
	}
}

func TestNormalizeAccessCode(t *testing.T) {
	if NormalizeAccessCode("  psn-abcd2345 ") != "PSN-ABCD2345" {
		t.Fatalf("unexpected normalized code %q", NormalizeAccessCode("  psn-abcd2345 "))
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Tips Lulus SKD 2026!":       "tips-lulus-skd-2026",
		"  Ujian   & Pembahasan  ":   "ujian-pembahasan",
		"Bahasa Indonesia -- Dasar ": "bahasa-indonesia-dasar",
	}
	for input, expected := range cases {
		if actual := Slugify(input); actual != expected {
			t.Errorf("Slugify(%q) = %q, want %q", input, actual, expected)
		}
	}
}
