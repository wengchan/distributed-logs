variable "aws_region" {
  default = "us-west-2"
}

variable "cluster_name" {
  default = "distributed-logs"
}

variable "db_password" {
  description = "Postgres master password"
  sensitive   = true
}

variable "anthropic_api_key" {
  description = "Anthropic API key for summarize service"
  sensitive   = true
}
