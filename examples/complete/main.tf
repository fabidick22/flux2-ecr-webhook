
module "flux2-ecr-webhook" {
  source = "../../"

  repo_mapping_file = "repos.yml"
  app_name = "flux2-ecr-webhook"
  webhook_token = "420a1600df316540afe3391c740c0d24ea6e9922"
  cw_logs_retention = 7
}