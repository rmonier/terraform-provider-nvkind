# NVKind Provider

The NVKind provider is used to interact with [Kubernetes IN Docker with NVIDIA GPUs access
(nvkind)](https://github.com/NVIDIA/nvkind) to provision local
[Kubernetes](https://kubernetes.io) clusters.

> **Note**
> 
> For the `runtimeConfig` field there's special behaviour for options containing a `/` character. Since this is not allowed in HCL you can just use `_` which is internally replaced with a `/` for generating the nvkind config. E.g. for the option `api/alpha` you'd name the field `api_alpha` and it will set it to `api/alpha` when creating the corresponding nvkind config.

## Example Usage

```hcl
# Configure the NVKind Provider
provider "nvkind" {}

# Create a cluster
resource "nvkind_cluster" "default" {
    name = "test-cluster"
}
```
