locals {
  repo_mapping = var.repo_mapping == null ? yamldecode(file(var.repo_mapping_file)) : var.repo_mapping
}