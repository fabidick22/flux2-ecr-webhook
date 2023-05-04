locals {
  repo_mapping = yamldecode(file(var.repo_mapping_file))
}