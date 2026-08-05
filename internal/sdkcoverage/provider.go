package sdkcoverage

import (
	"context"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// ShippedTypeNames returns the Terraform type names a provider registers, asking
// each registered constructor for its own Metadata.
//
// Reading the registration at runtime rather than grepping the source keeps the
// reconciliation honest: Resources()/DataSources()/Actions() is what Terraform
// actually serves, so a resource that exists as a file but was never registered
// does not count as shipped, and a rename cannot slip past.
//
// The parameter is the framework's provider.Provider interface, so this package
// still never imports the provider implementation — the gate test lives in that
// package and would otherwise form an import cycle.
func ShippedTypeNames(ctx context.Context, p provider.Provider, providerTypeName string) []string {
	seen := map[string]bool{}
	var names []string

	add := func(name string) {
		// A type name can legitimately appear twice: ssh_key and tag each ship as
		// both a resource and a data source.
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	}

	for _, newResource := range p.Resources(ctx) {
		var resp resource.MetadataResponse
		newResource().Metadata(ctx, resource.MetadataRequest{ProviderTypeName: providerTypeName}, &resp)
		add(resp.TypeName)
	}

	for _, newDataSource := range p.DataSources(ctx) {
		var resp datasource.MetadataResponse
		newDataSource().Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: providerTypeName}, &resp)
		add(resp.TypeName)
	}

	// Actions are optional on the provider interface, so a provider that
	// registers none is not a failure to introspect.
	if withActions, ok := p.(provider.ProviderWithActions); ok {
		for _, newAction := range withActions.Actions(ctx) {
			var resp action.MetadataResponse
			newAction().Metadata(ctx, action.MetadataRequest{ProviderTypeName: providerTypeName}, &resp)
			add(resp.TypeName)
		}
	}

	sort.Strings(names)
	return names
}
