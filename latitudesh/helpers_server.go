package latitudesh

import (
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

var ifaceType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"name":        types.StringType,
		"mac_address": types.StringType,
		"description": types.StringType,
	},
}

func emptyIfaces() types.List {
	list, _ := types.ListValue(ifaceType, []attr.Value{})
	return list
}

func listIfaces(vals []attr.Value) (types.List, diag.Diagnostics) {
	return types.ListValue(ifaceType, vals)
}

// buildInterfacesList converts the API interface slice into a Terraform list
// with a canonical, deterministic order. The API does not guarantee a stable
// ordering between reads, and `interfaces` is a Computed list, so an unsorted
// list makes index positions meaningless: on the next apply Terraform compares
// interfaces[N] from state against a possibly-reshuffled read and raises
// "Provider produced inconsistent result after apply" on interfaces[N].mac_address.
// Sorting by (name, mac_address, description) pins each interface to a stable
// position regardless of how the API returns them.
func buildInterfacesList(ifaces []components.Interfaces) (types.List, diag.Diagnostics) {
	if len(ifaces) == 0 {
		return emptyIfaces(), nil
	}

	sorted := make([]components.Interfaces, len(ifaces))
	copy(sorted, ifaces)
	sort.SliceStable(sorted, func(i, j int) bool {
		return ifaceSortKey(sorted[i]) < ifaceSortKey(sorted[j])
	})

	objs := make([]attr.Value, 0, len(sorted))
	for _, iface := range sorted {
		obj, diags := types.ObjectValue(ifaceType.AttrTypes, map[string]attr.Value{
			"name":        optionalString(iface.Name),
			"mac_address": optionalString(iface.MacAddress),
			"description": optionalString(iface.Description),
		})
		if diags.HasError() {
			return types.ListNull(ifaceType), diags
		}
		objs = append(objs, obj)
	}

	return listIfaces(objs)
}

// ifaceSortKey builds a total-order key from the interface fields. The NUL
// separator keeps the fields from bleeding into each other (e.g. so "ab"+"c"
// and "a"+"bc" sort distinctly).
func ifaceSortKey(iface components.Interfaces) string {
	return derefString(iface.Name) + "\x00" + derefString(iface.MacAddress) + "\x00" + derefString(iface.Description)
}

func derefString(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

func optionalString(ptr *string) types.String {
	if ptr == nil {
		return types.StringNull()
	}
	return types.StringValue(*ptr)
}
