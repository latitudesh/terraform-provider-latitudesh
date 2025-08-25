package validators

import (
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

const (
	MaxHostnameLength = 32
)

var hostnameRegex = regexp.MustCompile(
	`^[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$`,
)

// Hostname returns validators that enforce a stricter than RFC1123 allowed hostname:
// - max 32 characters
// - must start and end with a letter or digit
// - may contain letters, digits, hyphens (-) and dots (.)
// - underscores (_) are not allowed
func Hostname() []validator.String {
	return []validator.String{
		stringvalidator.LengthAtMost(MaxHostnameLength),
		stringvalidator.RegexMatches(
			hostnameRegex,
			"must contain only letters, digits, hyphens (-), and dots (.). Cannot start or end with a hyphen or dot; underscores (_) are not allowed",
		),
	}
}
