package main

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/plugin"
	"github.com/rmonier/terraform-provider-nvkind/nvkind"
)

func main() {
	plugin.Serve(&plugin.ServeOpts{
		ProviderFunc: nvkind.Provider})
}
