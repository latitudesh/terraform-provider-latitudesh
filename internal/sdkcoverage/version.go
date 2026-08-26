package sdkcoverage

import (
	"path/filepath"
	"strings"
)

// VersionFromDir derives the version string reports display from the SDK
// source directory. Module-cache directories are named module@version, so the
// suffix after "@" is kept only when it actually looks like one — Go module
// versions always start with "v" followed by a digit. Any other basename,
// including a hand-picked -sdk-dir that happens to contain "@" (or end with
// it), passes through whole: truncating it would report something that was
// never a version, and a trailing "@" would erase the metadata entirely.
func VersionFromDir(dir string) string {
	base := filepath.Base(dir)
	if at := strings.LastIndex(base, "@"); at >= 0 {
		if v := base[at+1:]; len(v) >= 2 && v[0] == 'v' && v[1] >= '0' && v[1] <= '9' {
			return v
		}
	}
	return base
}
