# Terraform AWS-API-Gateway-resource Provider

The AWS-API-Gateway-Resource Provider is a plugin for Terraform that allows working with AWS API Gateways. This provider is maintained by Traceableai.

For a more comprehensive explanation see [awsapigateway_resource](./docs/resources/awsapigateway_resource.md) documentation.

## Usage
The first example will track all apis defined by the cross account role arn `test-arn-1` except for the api with id `api1`.
The second example will only track apis with id `api1` and `api2` from the account where the deployment is made
```hcl
terraform {
  required_providers {
    awsapigateway = {
      source  = "Traceableai/awsapigateway"
      version = "0.3.0"
    }
  }
}

resource "awsapigateway_resouce" "traceable-example-1" {
  identifier                 = uuid()
  ignore_access_log_settings = false
  accounts {
    region                 = "us-east-2"
    api_list               = ["api1"]
    cross_account_role_arn = "test-arn-1"
    exclude                = true
  }
}

resource "awsapigateway_resouce" "traceable-example-2" {
  identifier                 = uuid()
  ignore_access_log_settings = false
  accounts {
    region                 = "us-east-1"
    api_list               = ["api1", "api2"]
    cross_account_role_arn = ""
    exclude                = false
  }
}
```

See the complete example [here](./examples/default)

## Dev Testing
Terraform providers local build can be used for terraform deployments instead of the published ones. Need to update 
the local `~/.terraformrc` file with the location of the build. 
ref: https://developer.hashicorp.com/terraform/cli/config/config-file#development-overrides-for-provider-developers

### Steps
1. Install go releaser for creating the build
```shell
brew install goreleaser
```
2. Make dev build
```shell
goreleaser build --snapshot
```
3. Update `~/.terraformrc` with the location of the build
GoReleaser will publish artifacts for all the different runtimes. Find the correct build and add it's path in the above 
file.

For example, on M1 Mac, the corresponding build will be inside folder `dist/terraform-provider-awsapigateway_darwin_arm64/`.
In that case, the '~/.terraformrc' file contents will be
```
provider_installation {

  # Use /home/developer/tmp/terraform-null as an overridden package directory
  # for the hashicorp/null provider. This disables the version and checksum
  # verifications for this provider and forces Terraform to look for the
  # null provider plugin in the given directory.
  dev_overrides {
    "Traceableai/awsapigateway" = "/Users/varkeychanjacob/Projects/terraform-provider-awsapigateway/dist/terraform-provider-awsapigateway_darwin_arm64"
  }

  # For all other providers, install them directly from their origin provider
  # registries as normal. If you omit this, Terraform will _only_ use
  # the dev_overrides block, and so no other providers will be available.
  direct {}
}

```

## Testing

```shell
make test
```
