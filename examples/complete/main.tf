
module "flux2-ecr-webhook2" {
  source = "../../"

  app_name = "flux2-ecr-webhook2"
  repo_mapping = {
    "test/my-ecr-repo" = {
      production = {
        webhook = ["https://gitops.domain.com/hook/11111111111"]
      }
    }
  }
  webhook_token = "WEBHOOK-TOKEN" # Keep this token safe, you can use sops (mozilla/sops).
}

module "flux2-ecr-webhook3" {
  source = "../../"

  app_name = "flux2-ecr-webhook3"
  repo_mapping = {
    my-ecr-repo = {
      prod = {
        webhook = ["https://gitops.domain.com/hook/11111111111"]
      }
    }
    my-ecr-repo2 = {
      prod = {
        webhook = ["https://gitops.domain.com/hook/11111111111"]
        regex   = "prod-(?P<version>.*)" # Regex for ECR image tag
      }
    }
    my-ecr-repo3 = {
      prod = {
        webhook = ["https://gitops.domain.com/hook/11111111111"]
        token   = "WEBHOOK-TOKEN" # Custom token (you can use mozilla/sops).
      }
    }
  }
  webhook_token = "WEBHOOK-TOKEN" # Webhook token (you can use mozilla/sops).
}
