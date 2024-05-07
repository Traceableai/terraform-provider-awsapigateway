variable "accounts" {
  type = list(object({
    region                 = string
    api_list               = list(string)
    cross_account_role_arn = string
    exclude                = bool
  }))

}