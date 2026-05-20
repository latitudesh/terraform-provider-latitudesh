package latitudesh

import (
	"sync"

	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
)

type ProviderContext struct {
	Client  *latitudeshgosdk.Latitudesh
	Project string

	// UserDataHashCache stores the planned content hash for each user_data
	// resource managed in the current Terraform run, keyed by user_data ID.
	// Populated by latitudesh_user_data ModifyPlan and consumed by
	// latitudesh_server ModifyPlan so that in-Terraform content changes
	// cascade to dependent servers in a single apply.
	UserDataHashCache *sync.Map
}
