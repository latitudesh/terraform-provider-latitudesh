package latitudesh

import (
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
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

func optionalString(ptr *string) types.String {
	if ptr == nil {
		return types.StringNull()
	}
	return types.StringValue(*ptr)
}
