package sdkcoverage

import (
	"context"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// ShippedTypes is a provider's registered Terraform type names, split by kind.
// The arrays are always non-nil so the JSON rendering never emits null.
type ShippedTypes struct {
	Resources   []string `json:"resources"`
	DataSources []string `json:"datasources"`
	Actions     []string `json:"actions"`
}

// ShippedByKind returns the Terraform type names a provider registers, asking
// each registered constructor for its own Metadata and keeping the kinds apart.
//
// Reading the registration at runtime rather than grepping the source keeps the
// reconciliation honest: Resources()/DataSources()/Actions() is what Terraform
// actually serves, so a resource that exists as a file but was never registered
// does not count as shipped, and a rename cannot slip past.
//
// The split matters to the scaffold gate: a type name can legitimately serve
// several kinds (ssh_key ships as both a resource and a data source), so once
// one kind claims the name, a merged view cannot tell whether the other kind
// was ever registered at all.
//
// The parameter is the framework's provider.Provider interface, so this package
// still never imports the provider implementation — the gate test lives in that
// package and would otherwise form an import cycle.
func ShippedByKind(ctx context.Context, p provider.Provider, providerTypeName string) ShippedTypes {
	shipped := ShippedTypes{
		Resources:   []string{},
		DataSources: []string{},
		Actions:     []string{},
	}

	for _, newResource := range p.Resources(ctx) {
		var resp resource.MetadataResponse
		newResource().Metadata(ctx, resource.MetadataRequest{ProviderTypeName: providerTypeName}, &resp)
		if resp.TypeName != "" {
			shipped.Resources = append(shipped.Resources, resp.TypeName)
		}
	}

	for _, newDataSource := range p.DataSources(ctx) {
		var resp datasource.MetadataResponse
		newDataSource().Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: providerTypeName}, &resp)
		if resp.TypeName != "" {
			shipped.DataSources = append(shipped.DataSources, resp.TypeName)
		}
	}

	// Actions are optional on the provider interface, so a provider that
	// registers none is not a failure to introspect.
	if withActions, ok := p.(provider.ProviderWithActions); ok {
		for _, newAction := range withActions.Actions(ctx) {
			var resp action.MetadataResponse
			newAction().Metadata(ctx, action.MetadataRequest{ProviderTypeName: providerTypeName}, &resp)
			if resp.TypeName != "" {
				shipped.Actions = append(shipped.Actions, resp.TypeName)
			}
		}
	}

	sort.Strings(shipped.Resources)
	sort.Strings(shipped.DataSources)
	sort.Strings(shipped.Actions)
	return shipped
}

// ShippedTypeNames returns the registered type names merged across kinds, which
// is the view coverage reconciliation wants: there, a type name is one fact
// however many kinds serve it.
func ShippedTypeNames(ctx context.Context, p provider.Provider, providerTypeName string) []string {
	byKind := ShippedByKind(ctx, p, providerTypeName)

	seen := map[string]bool{}
	var names []string
	for _, list := range [][]string{byKind.Resources, byKind.DataSources, byKind.Actions} {
		for _, name := range list {
			if seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}

	sort.Strings(names)
	return names
}
