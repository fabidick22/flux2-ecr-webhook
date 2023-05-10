
module "flux2-ecr-webhook" {
  source = "../../"

  app_name          = "flux2-ecr-webhook"
  repo_mapping_file = "repos.yml" # Deprecated
  webhook_token     = "WEBHOOK-TOKEN" # Keep this token safe, you can use sops (mozilla/sops).
  cw_logs_retention = 7
}

module "flux2-ecr-webhook2" {
  source = "../../"

  app_name = "flux2-ecr-webhook2"
  repo_mapping = {
    test/my-ecr-repo = {
      webhook = "https://gitops.domain.com/hook/11111111111"
    }
  }
  webhook_token = "WEBHOOK-TOKEN" # Keep this token safe, you can use sops (mozilla/sops).
}

module "flux2-ecr-webhook3" {
  source = "../../"

  app_name = "flux2-ecr-webhook3"
  repo_mapping = {
    my-ecr-repo = {
      webhook = "https://gitops.domain.com/hook/11111111111"
      token = "WEBHOOK-TOKEN" # Keep this token safe, you can use sops (mozilla/sops).
    }
    my-ecr-repo2 = {
      webhook = "https://gitops.domain.com/hook/11111111111"
    }
    my-ecr-repo3 = {
      webhook = "https://gitops.domain.com/hook/11111111111"
    }
  }
  webhook_token = "WEBHOOK-TOKEN" # Keep this token safe, you can use sops (mozilla/sops).
}