package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	latitude "github.com/latitudesh/terraform-provider-latitudesh/v2/latitudesh"
)

// version is set via ldflags during build
var version = "dev"

// Generate the Terraform provider documentation using `tfplugindocs`.
//
// Both names are pinned on purpose. tfplugindocs otherwise derives them from the
// checkout directory name, so generation only worked in a directory called exactly
// "terraform-provider-latitudesh" — anywhere else it looked up a provider that does
// not exist and deleted docs/ before failing. --provider-name is the name used in
// Terraform configurations; --rendered-provider-name is what appears in page titles.
//
// Both are "latitudesh" because the two ways a page title gets written have to agree.
// Our own templates interpolate .ProviderName, while the built-in fallback template —
// used by the plan, region and role data sources, which have no template of their own —
// switched to .RenderedProviderName in tfplugindocs 0.23. Setting them to different
// values silently retitles exactly those three pages.
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-name latitudesh --rendered-provider-name latitudesh

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
