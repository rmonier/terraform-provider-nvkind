# Terraform Provider for nvkind


## Overview

The Terraform Provider for nvkind enables [Terraform](https://www.terraform.io) to provision local [Kubernetes](https://kubernetes.io) clusters on base of [Kubernetes IN Docker with NVIDIA GPUs access (nvkind)](https://github.com/NVIDIA/nvkind).

Based on the [Terraform Provider for kind](https://github.com/tehcyx/terraform-provider-kind) by [Daniel Roth](https://github.com/tehcyx), licensed under Apache 2.0 — see LICENSE and NOTICE.md for details.

## Quick Starts
- [Using the provider](./docs/USAGE.md)
- [Provider development](./docs/DEVELOPMENT.md)

> **Note**
> 
> For the `runtimeConfig` field there's special behaviour for options containing a `/` character. Since this is not allowed in HCL you can just use `_` which is internally replaced with a `/` for generating the nvkind config. E.g. for the option `api/alpha` you'd name the field `api_alpha` and it will set it to `api/alpha` when creating the corresponding nvkind config.

## Example Usage

Copy the following code into a file with the extension `.tf` to create a nvkind cluster with only default values.
```hcl
provider "nvkind" {}

resource "kind_cluster" "default" {
    name = "test-cluster"
}
```

Then run `terraform init`, `terraform plan` & `terraform apply` and follow the on screen instructions. For more details on how to influence creation of the nvkind resource check out the Quick Start section above.
