package main

import (
	"flag"

	"github.com/capturealpha/terraform-provider-latitude/latitude"
	"github.com/hashicorp/terraform-plugin-sdk/v2/plugin"
)

func main() {
	var debugMode bool

	flag.BoolVar(&debugMode, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()
	opts := &plugin.ServeOpts{
		Debug:        debugMode,
		ProviderFunc: latitude.Provider,
	}

	plugin.Serve(opts)
}
