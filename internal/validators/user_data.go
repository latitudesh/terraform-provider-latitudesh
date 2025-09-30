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

var userDataPrefixRegex = regexp.MustCompile(`^ud_`)

// UserData returns validators that enforce user_data format:
// - must start with 'ud_' prefix
func UserData() []validator.String {
	return []validator.String{
		stringvalidator.RegexMatches(
			userDataPrefixRegex,
			"user_data must start with 'ud_' prefix",
		),
	}
}

// ValidateUserData runs the UserData validators against the given string
func ValidateUserData(s string) error {
	ctx := context.Background()

	req := validator.StringRequest{
		Path:        path.Root("user_data"),
		ConfigValue: types.StringValue(s),
	}
	var resp validator.StringResponse

	for _, v := range UserData() {
		v.ValidateString(ctx, req, &resp)
	}

	if resp.Diagnostics.HasError() {
		d := resp.Diagnostics[0]
		return fmt.Errorf("%s: %s", d.Summary(), d.Detail())
	}

	return nil
}
