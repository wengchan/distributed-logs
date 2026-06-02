#!/usr/bin/env bash
# Build all service images and push them to ECR.
#
#   ./build-and-push.sh [IMAGE_TAG]
#
# IMAGE_TAG defaults to the short git SHA. Reads ECR_REGISTRY/AWS_REGION from
# the environment, otherwise pulls them from `terraform output`.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TF_DIR="$REPO_ROOT/deploy/aws/terraform"
DOCKERFILE="$REPO_ROOT/deploy/aws/Dockerfile"

IMAGE_TAG="${1:-$(git -C "$REPO_ROOT" rev-parse --short HEAD)}"
AWS_REGION="${AWS_REGION:-$(terraform -chdir="$TF_DIR" output -raw aws_region)}"
ECR_REGISTRY="${ECR_REGISTRY:-$(terraform -chdir="$TF_DIR" output -raw ecr_registry)}"

SERVICES=(index-service log-client query-service summarize-service monitor-service)

echo "==> Logging in to ECR ($ECR_REGISTRY)"
aws ecr get-login-password --region "$AWS_REGION" \
  | docker login --username AWS --password-stdin "$ECR_REGISTRY"

for svc in "${SERVICES[@]}"; do
  image="$ECR_REGISTRY/distributed-logs/$svc:$IMAGE_TAG"
  echo "==> Building $svc -> $image"
  docker build \
    --platform linux/amd64 \
    -f "$DOCKERFILE" \
    --build-arg "SERVICE=$svc" \
    -t "$image" \
    "$REPO_ROOT"
  echo "==> Pushing $image"
  docker push "$image"
done

echo
echo "Pushed all images with tag: $IMAGE_TAG"
