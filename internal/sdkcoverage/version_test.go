package sdkcoverage

import "testing"

func TestVersionFromDir(t *testing.T) {
	cases := []struct {
		dir  string
		want string
	}{
		// Module-cache entries normalize to the bare version go list prints.
		{"/home/u/go/pkg/mod/github.com/latitudesh/latitudesh-go-sdk@v1.19.12", "v1.19.12"},
		{"latitudesh-go-sdk@v1.19.14-rc.1", "v1.19.14-rc.1"},
		{"sdk@v2.0.0+incompatible", "v2.0.0+incompatible"},

		// Hand-picked -sdk-dir basenames are not module-cache entries: never
		// truncate them into something that was never a version, and never let a
		// trailing "@" blank the field (markdown/text suppress an empty version
		// and JSON omits sdk_version entirely).
		{"/tmp/sdk@work", "sdk@work"},
		{"/tmp/sdk@vendored", "sdk@vendored"},
		{"/tmp/sdk@", "sdk@"},
		{"/tmp/checkout", "checkout"},
	}
	for _, tc := range cases {
		if got := VersionFromDir(tc.dir); got != tc.want {
			t.Errorf("VersionFromDir(%q) = %q, want %q", tc.dir, got, tc.want)
		}
	}
}
