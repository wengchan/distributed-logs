#!/usr/bin/env bash
# Tear everything down. Deletes the LoadBalancer Service first so the NLB is
# released before Terraform tries to destroy the VPC it lives in.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TF_DIR="$REPO_ROOT/deploy/aws/terraform"

echo "==> deleting in-cluster resources (releases the NLB)"
kubectl delete namespace distributed-logs --ignore-not-found --wait=true || true

echo "==> terraform destroy"
terraform -chdir="$TF_DIR" destroy -auto-approve

echo "Done. Note: the S3 log-archive bucket may need manual emptying if versioned objects remain."
