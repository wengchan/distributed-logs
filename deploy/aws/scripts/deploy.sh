#!/usr/bin/env bash
# End-to-end deploy to EKS:
#   1. terraform apply (VPC, EKS, RDS, ECR, S3)
#   2. build + push images
#   3. wire kubeconfig
#   4. create namespace, secrets, configmaps (from RDS/Secrets Manager + ./testlogs + ./migrations)
#   5. run DB migration Job
#   6. apply service manifests
#
#   ./deploy.sh [IMAGE_TAG]
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
AWS_DIR="$REPO_ROOT/deploy/aws"
TF_DIR="$AWS_DIR/terraform"
K8S_DIR="$AWS_DIR/k8s"

IMAGE_TAG="${1:-$(git -C "$REPO_ROOT" rev-parse --short HEAD)}"

command -v terraform >/dev/null || { echo "terraform not found"; exit 1; }
command -v kubectl   >/dev/null || { echo "kubectl not found"; exit 1; }
command -v envsubst  >/dev/null || { echo "envsubst (gettext) not found"; exit 1; }

# ── 1. Provision infrastructure ──────────────────────────────────────────────
echo "==> terraform apply"
terraform -chdir="$TF_DIR" init -input=false
terraform -chdir="$TF_DIR" apply -auto-approve

export AWS_REGION="$(terraform -chdir="$TF_DIR" output -raw aws_region)"
export ECR_REGISTRY="$(terraform -chdir="$TF_DIR" output -raw ecr_registry)"
export IMAGE_TAG
export LOG_ARCHIVE_BUCKET="$(terraform -chdir="$TF_DIR" output -raw log_archive_bucket)"
export LOG_ARCHIVE_ROLE_ARN="$(terraform -chdir="$TF_DIR" output -raw log_archive_role_arn)"
CLUSTER_NAME="$(terraform -chdir="$TF_DIR" output -raw cluster_name)"
RDS_ENDPOINT="$(terraform -chdir="$TF_DIR" output -raw rds_endpoint)"

# ── 2. Build + push images ───────────────────────────────────────────────────
"$AWS_DIR/scripts/build-and-push.sh" "$IMAGE_TAG"

# ── 3. kubeconfig ────────────────────────────────────────────────────────────
echo "==> updating kubeconfig"
aws eks update-kubeconfig --region "$AWS_REGION" --name "$CLUSTER_NAME"

# ── 4. namespace + secrets + configmaps ──────────────────────────────────────
echo "==> namespace + config"
kubectl apply -f "$K8S_DIR/namespace.yaml"
envsubst < "$K8S_DIR/01-serviceaccount.yaml" | kubectl apply -f -
envsubst < "$K8S_DIR/02-config.yaml"          | kubectl apply -f -

# DATABASE_URL + ANTHROPIC_API_KEY are read from Secrets Manager (populated by
# Terraform) so they never touch git or the shell history.
DB_URL="$(aws secretsmanager get-secret-value --region "$AWS_REGION" \
  --secret-id "$CLUSTER_NAME/db" --query SecretString --output text | python3 -c 'import sys,json;print(json.load(sys.stdin)["url"])')"
ANTHROPIC_KEY="$(aws secretsmanager get-secret-value --region "$AWS_REGION" \
  --secret-id "$CLUSTER_NAME/anthropic" --query SecretString --output text | python3 -c 'import sys,json;print(json.load(sys.stdin)["api_key"])')"

kubectl -n distributed-logs create secret generic app-secrets \
  --from-literal=DATABASE_URL="$DB_URL" \
  --from-literal=ANTHROPIC_API_KEY="$ANTHROPIC_KEY" \
  --dry-run=client -o yaml | kubectl apply -f -

# Migrations + sample logs as ConfigMaps.
kubectl -n distributed-logs create configmap migrations \
  --from-file="$REPO_ROOT/migrations" \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl -n distributed-logs create configmap sample-logs \
  --from-file="$REPO_ROOT/testlogs" \
  --dry-run=client -o yaml | kubectl apply -f -

# ── 5. migrate ───────────────────────────────────────────────────────────────
echo "==> running DB migrations (RDS @ $RDS_ENDPOINT)"
kubectl -n distributed-logs delete job db-migrate --ignore-not-found
kubectl apply -f "$K8S_DIR/00-migrate-job.yaml"
kubectl -n distributed-logs wait --for=condition=complete job/db-migrate --timeout=180s

# ── 6. services ──────────────────────────────────────────────────────────────
echo "==> deploying services"
for f in 10-index-service 11-summarize-service 12-query-service 13-log-client; do
  envsubst < "$K8S_DIR/$f.yaml" | kubectl apply -f -
done

kubectl -n distributed-logs rollout status deploy/index-service
kubectl -n distributed-logs rollout status deploy/query-service

echo
echo "Deploy complete. Public endpoint:"
echo "  kubectl -n distributed-logs get svc query-service -o jsonpath='{.status.loadBalancer.ingress[0].hostname}'"
