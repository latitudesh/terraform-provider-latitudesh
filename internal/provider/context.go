package latitudesh

import latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"

type ProviderContext struct {
	Client  *latitudeshgosdk.Latitudesh
	Project string
}
