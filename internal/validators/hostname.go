package validators

import (
	"context"
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
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

// ValidateHostname runs the Hostname validators against the given string
func ValidateHostname(s string) error {
	ctx := context.Background()

	req := validator.StringRequest{
		Path:        path.Root("hostname"),
		ConfigValue: types.StringValue(s),
	}
	var resp validator.StringResponse

	for _, v := range Hostname() {
		v.ValidateString(ctx, req, &resp)
	}

	if resp.Diagnostics.HasError() {
		d := resp.Diagnostics[0]
		return fmt.Errorf("%s: %s", d.Summary(), d.Detail())
	}

	return nil
}
