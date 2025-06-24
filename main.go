package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	latitude "github.com/latitudesh/terraform-provider-latitudesh/latitudesh"
)

// version is set via ldflags during build
var version = "dev"

// Generate the Terraform provider documentation using `tfplugindocs`:
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs

func main() {
	var debugMode bool

	flag.BoolVar(&debugMode, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(
		context.Background(),
		latitude.New(version),
		providerserver.ServeOpts{
			Address: "registry.terraform.io/latitudesh/latitudesh",
			Debug:   debugMode,
		},
	)

	if err != nil {
		log.Fatal(err)
	}
}
