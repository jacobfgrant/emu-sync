package update

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"v0.3.0", "v0.3.1", -1},
		{"v0.3.1", "v0.3.1", 0},
		{"v1.0.0", "v0.9.9", 1},
		{"v0.10.0", "v0.9.0", 1},
		{"v1.0", "v1.0.0", 0},
		{"v2.0.0", "v1.99.99", 1},
		{"v0.0.1", "v0.0.2", -1},
	}
	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			got := CompareVersions(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestIsUpdateAvailable(t *testing.T) {
	tests := []struct {
		current, latest string
		want            bool
	}{
		{"v0.3.0", "v0.3.1", true},
		{"v0.3.1", "v0.3.1", false},
		{"v1.0.0", "v0.9.0", false},
		{"dev", "v99.0.0", false},
	}
	for _, tt := range tests {
		t.Run(tt.current+"_to_"+tt.latest, func(t *testing.T) {
			got := IsUpdateAvailable(tt.current, tt.latest)
			if got != tt.want {
				t.Errorf("IsUpdateAvailable(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
			}
		})
	}
}

func TestCheckLatestVersion(t *testing.T) {
	t.Run("valid redirect", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "https://github.com/jacobfgrant/emu-sync/releases/tag/v0.5.0")
			w.WriteHeader(http.StatusFound)
		}))
		defer srv.Close()

		origURL := latestReleaseURL
		latestReleaseURL = srv.URL
		defer func() { latestReleaseURL = origURL }()

		got, err := CheckLatestVersion()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "v0.5.0" {
			t.Errorf("got %q, want %q", got, "v0.5.0")
		}
	})

	t.Run("non-302 response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		origURL := latestReleaseURL
		latestReleaseURL = srv.URL
		defer func() { latestReleaseURL = origURL }()

		_, err := CheckLatestVersion()
		if err == nil {
			t.Fatal("expected error for non-302 response")
		}
	})

	t.Run("missing location header", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusFound)
		}))
		defer srv.Close()

		origURL := latestReleaseURL
		latestReleaseURL = srv.URL
		defer func() { latestReleaseURL = origURL }()

		_, err := CheckLatestVersion()
		if err == nil {
			t.Fatal("expected error for missing Location header")
		}
	})

	t.Run("bad tag format", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "https://github.com/jacobfgrant/emu-sync/releases/tag/latest")
			w.WriteHeader(http.StatusFound)
		}))
		defer srv.Close()

		origURL := latestReleaseURL
		latestReleaseURL = srv.URL
		defer func() { latestReleaseURL = origURL }()

		_, err := CheckLatestVersion()
		if err == nil {
			t.Fatal("expected error for bad tag format")
		}
	})
}
