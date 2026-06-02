# ── Security group — allow EKS nodes → RDS ───────────────────────────────────
resource "aws_security_group" "rds" {
  name   = "${var.cluster_name}-rds"
  vpc_id = aws_vpc.main.id

  ingress {
    from_port   = 5432
    to_port     = 5432
    protocol    = "tcp"
    cidr_blocks = ["10.0.0.0/16"]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
  tags = { Name = "${var.cluster_name}-rds-sg" }
}

resource "aws_db_subnet_group" "main" {
  name       = "${var.cluster_name}-db-subnet"
  subnet_ids = aws_subnet.private[*].id
}

# ── RDS Postgres (Multi-AZ for HA) ────────────────────────────────────────────
resource "aws_db_instance" "postgres" {
  identifier        = "${var.cluster_name}-postgres"
  engine            = "postgres"
  engine_version    = "16"
  instance_class    = "db.t3.micro"
  allocated_storage = 20
  storage_type      = "gp3"

  db_name  = "logs"
  username = "postgres"
  password = var.db_password

  db_subnet_group_name   = aws_db_subnet_group.main.name
  vpc_security_group_ids = [aws_security_group.rds.id]

  multi_az            = true   # HA — automatic failover to standby
  skip_final_snapshot = true
  deletion_protection = false  # set true in production

  tags = { Name = "${var.cluster_name}-postgres" }
}

# ── Store DB credentials in Secrets Manager ───────────────────────────────────
resource "aws_secretsmanager_secret" "db" {
  name = "${var.cluster_name}/db"
}

resource "aws_secretsmanager_secret_version" "db" {
  secret_id = aws_secretsmanager_secret.db.id
  secret_string = jsonencode({
    username = "postgres"
    password = var.db_password
    host     = aws_db_instance.postgres.address
    port     = 5432
    dbname   = "logs"
    url      = "postgres://postgres:${var.db_password}@${aws_db_instance.postgres.address}:5432/logs?sslmode=require"
  })
}

resource "aws_secretsmanager_secret" "anthropic" {
  name = "${var.cluster_name}/anthropic"
}

resource "aws_secretsmanager_secret_version" "anthropic" {
  secret_id     = aws_secretsmanager_secret.anthropic.id
  secret_string = jsonencode({ api_key = var.anthropic_api_key })
}
