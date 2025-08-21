package latitudesh

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
)

type ProviderDeps struct {
	Client         *latitudeshgosdk.Latitudesh
	DefaultProject string
}

func resolveProviderDeps(pd any) (ProviderDeps, error) {
	switch v := pd.(type) {
	case *ProviderContext:
		if v == nil || v.Client == nil {
			return ProviderDeps{}, fmt.Errorf("nil ProviderContext or Client")
		}
		return ProviderDeps{
			Client:         v.Client,
			DefaultProject: v.Project,
		}, nil
	case *latitudeshgosdk.Latitudesh:
		if v == nil {
			return ProviderDeps{}, fmt.Errorf("nil *latitudeshgosdk.Latitudesh")
		}
		return ProviderDeps{Client: v}, nil
	case nil:
		return ProviderDeps{}, nil
	default:
		return ProviderDeps{}, fmt.Errorf("unexpected ProviderData type: %T", v)
	}
}

func ConfigureFromProviderData(providerData any, diags *diag.Diagnostics) ProviderDeps {
	deps, err := resolveProviderDeps(providerData)
	if err != nil {
		diags.AddError("Unexpected Configure Type", err.Error())
	}
	return deps
}
