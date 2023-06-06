terraform {
  required_providers {
	awsapigateway = {
	  source  = "Traceableai/awsapigateway"
	  version = "0.1.0"
	}
  }
}

resource "awsapigateway_resource" "test" {
  api_gateways = []
  action       = "exclude"
}
