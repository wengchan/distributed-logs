output "cluster_name" {
  value = aws_eks_cluster.main.name
}

output "aws_region" {
  value = var.aws_region
}

output "cluster_endpoint" {
  value = aws_eks_cluster.main.endpoint
}

output "rds_endpoint" {
  value = aws_db_instance.postgres.address
}

output "ecr_registry" {
  value = "${data.aws_caller_identity.current.account_id}.dkr.ecr.${var.aws_region}.amazonaws.com"
}

output "ecr_urls" {
  value = { for k, v in aws_ecr_repository.services : k => v.repository_url }
}

output "kubeconfig_command" {
  value = "aws eks update-kubeconfig --region ${var.aws_region} --name ${var.cluster_name}"
}

output "log_archive_bucket" {
  value = aws_s3_bucket.logs.bucket
}

output "log_archive_role_arn" {
  description = "Annotate the index-service ServiceAccount with this (eks.amazonaws.com/role-arn) for S3 access via IRSA"
  value       = aws_iam_role.logs_archive.arn
}
