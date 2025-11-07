package validators

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	BillingHourly  = "hourly"
	BillingMonthly = "monthly"
	BillingYearly  = "yearly"
)

var validBillingValues = []string{BillingHourly, BillingMonthly, BillingYearly}

var billingHierarchy = map[string]int{
	BillingHourly:  0,
	BillingMonthly: 1,
	BillingYearly:  2,
}

// Billing returns validators that enforce valid billing values:
// - must be one of: hourly, monthly, yearly
func Billing() []validator.String {
	return []validator.String{
		stringvalidator.OneOf(validBillingValues...),
	}
}

// ValidateBilling runs the Billing validators against the given string
func ValidateBilling(s string) error {
	ctx := context.Background()

	req := validator.StringRequest{
		Path:        path.Root("billing"),
		ConfigValue: types.StringValue(s),
	}
	var resp validator.StringResponse

	for _, v := range Billing() {
		v.ValidateString(ctx, req, &resp)
	}

	if resp.Diagnostics.HasError() {
		d := resp.Diagnostics[0]
		return fmt.Errorf("%s: %s", d.Summary(), d.Detail())
	}

	return nil
}

// ValidateBillingChange validates if a billing change is allowed.
// Only allows upgrades: hourly -> monthly -> yearly
// Blocks all downgrades: yearly -> monthly, yearly -> hourly, monthly -> hourly
func ValidateBillingChange(current, newBilling string) error {
	current = strings.ToLower(strings.TrimSpace(current))
	newBilling = strings.ToLower(strings.TrimSpace(newBilling))

	// Validate new billing value
	if err := ValidateBilling(newBilling); err != nil {
		return err
	}

	// If no current value, allow any valid billing value
	if current == "" {
		return nil
	}

	// Validate current billing value
	currentLevel, currentOk := billingHierarchy[current]
	if !currentOk {
		return fmt.Errorf("invalid current billing value: %s", current)
	}

	// Same value, no change needed
	if current == newBilling {
		return nil
	}

	// Check if change is allowed
	newLevel, newOk := billingHierarchy[newBilling]
	if !newOk {
		return fmt.Errorf("invalid billing value: %s", newBilling)
	}

	if newLevel <= currentLevel {
		return fmt.Errorf(
			"cannot change billing from '%s' to '%s'. The API only allows billing upgrades (hourly -> monthly -> yearly). Downgrades are not permitted",
			current, newBilling,
		)
	}

	return nil
}
