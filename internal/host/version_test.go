package host

import "testing"

// A release build stamps -X today/internal/host.Version, and that stamp has to
// win over whatever the bundle says.
func TestAppVersionPrefersStampedVersion(t *testing.T) {
	defer func(v string) { Version = v }(Version)
	Version = "1.2.3"
	if got := AppVersion(); got != "1.2.3" {
		t.Fatalf("AppVersion() = %q, want the stamped 1.2.3", got)
	}
}

// Under `go test` there is no .app bundle, so the placeholder is all that is
// left. The point of the check is that AppVersion does not invent a version
// and does not panic before Run.
func TestAppVersionFallsBackToPlaceholder(t *testing.T) {
	defer func(v string) { Version = v }(Version)
	Version = devVersion
	if got := AppVersion(); got != devVersion {
		t.Fatalf("AppVersion() = %q, want %q without a bundle", got, devVersion)
	}
}
