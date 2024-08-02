terraform {
  required_providers {
    awsapigateway = {
      source  = "Traceableai/awsapigateway"
      version = "0.3.0"
    }
  }
}

# For a list of accounts
resource "awsapigateway_resource" "traceable-example-1" {
  identifier                 = uuid()
  ignore_access_log_settings = false
  dynamic "accounts" {
    for_each = var.accounts
    content {
      region                 = accounts.value["region"]
      api_list               = accounts.value["api_list"]
      cross_account_role_arn = accounts.value["cross_account_role_arn"]
      exclude                = accounts.value["exclude"]
    }
  }
  timeout = "10s"
}

# For a single account
resource "awsapigateway_resource" "traceable-example-2" {
  identifier                 = uuid()
  ignore_access_log_settings = false
  accounts {
    region                 = "us-east-1"
    api_list               = ["api1", "api2"]
    cross_account_role_arn = ""
    exclude                = false
  }
  timeout = "1s"
}