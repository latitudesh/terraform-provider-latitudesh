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
