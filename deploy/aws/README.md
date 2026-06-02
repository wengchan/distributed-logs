# AWS deployment (EKS + RDS + S3 + ECR)

Deploys the distributed-logs stack to AWS:

| Component          | AWS service              | Notes                                            |
| ------------------ | ------------------------ | ------------------------------------------------ |
| Container runtime  | **EKS** (managed K8s)    | 2× `t3.medium` nodes in private subnets          |
| AI monitoring      | **monitor-service**      | Agentic tool-use loop over the logs, reports via HTTP |
| Index database     | **RDS** Postgres 16      | Multi-AZ for HA, in private subnets              |
| Raw-log archive    | **S3**                   | Versioned, encrypted, tiers to Glacier @ 90d     |
| Image registry     | **ECR**                  | One repo per service, scan-on-push               |
| Secrets            | **Secrets Manager**      | DB URL + Anthropic key, injected as K8s secrets  |
| Public entrypoint  | **NLB**                  | Fronts `query-service` (HTTP API)                |
| Networking         | **VPC**                  | Public + private subnets across 2 AZs, NAT GW    |

```
                    Internet
                       │
                   [ NLB ]  ← query-service Service (LoadBalancer)
                       │
   ┌───────────────────┼─────────────────────────── EKS (private subnets) ──┐
   │   query-service ──┼── index-service ──(gRPC)        log-client          │
   │        │          └── summarize-service                  │             │
   │        │                                                 │             │
   └────────┼─────────────────────────────────────────────────┼────────────┘
            │                                                  │
        [ RDS Postgres ]                                  [ S3 log archive ]
```

## Layout

```
deploy/aws/
├── Dockerfile              # generic build, parameterized by --build-arg SERVICE
├── terraform/              # all infrastructure as code
│   ├── main.tf  vpc.tf  eks.tf  rds.tf  ecr.tf  s3.tf
│   ├── variables.tf  outputs.tf
│   └── terraform.tfvars.example
├── k8s/                    # manifests (envsubst placeholders filled by deploy.sh)
│   ├── namespace.yaml  01-serviceaccount.yaml  02-config.yaml
│   ├── 00-migrate-job.yaml
│   └── 10-index 11-summarize 12-query 13-log-client 14-monitor
└── scripts/
    ├── build-and-push.sh   # build all images → ECR
    ├── deploy.sh           # full provision + deploy
    └── destroy.sh          # tear down
```

## Prerequisites

- `awscli` v2 (authenticated: `aws sts get-caller-identity` works)
- `terraform` ≥ 1.6
- `kubectl`
- `docker`
- `envsubst` (`brew install gettext`)
- `python3` (used to parse Secrets Manager JSON)

## Deploy

```bash
cd deploy/aws/terraform
cp terraform.tfvars.example terraform.tfvars
$EDITOR terraform.tfvars        # set db_password + anthropic_api_key

cd ../..                        # back to repo root
./deploy/aws/scripts/deploy.sh
```

`deploy.sh` runs the whole pipeline: `terraform apply` → build & push images →
update kubeconfig → create secrets/configmaps → run the migration Job → roll out
the services. First run takes ~15–20 min (EKS + RDS provisioning dominates).

Get the public URL once the NLB is ready (~2 min after deploy):

```bash
LB=$(kubectl -n distributed-logs get svc query-service \
       -o jsonpath='{.status.loadBalancer.ingress[0].hostname}')
curl "http://$LB/api/v1/logs/count"
curl "http://$LB/api/v1/logs"
```

## How config flows

- **Secrets** (`DATABASE_URL`, `ANTHROPIC_API_KEY`) live in Secrets Manager,
  written by Terraform. `deploy.sh` reads them and creates the `app-secrets`
  K8s Secret — nothing sensitive is committed or passed on the CLI.
- **S3 access** uses IRSA: Terraform creates an IAM role scoped to the
  `index-service` ServiceAccount via the cluster's OIDC provider. No static
  AWS keys in the pods. The bucket name + region are in the `app-config`
  ConfigMap (`LOG_ARCHIVE_BUCKET`, `AWS_REGION`).
- **Migrations** and **sample logs** are mounted from ConfigMaps generated at
  deploy time from `./migrations` and `./testlogs`.

## Iterating on code

After the infra exists, redeploy just the app:

```bash
./deploy/aws/scripts/build-and-push.sh v2     # push new images tagged v2
./deploy/aws/scripts/deploy.sh v2             # terraform is a no-op; rolls out v2
```

## Tear down

```bash
./deploy/aws/scripts/destroy.sh
```

Deletes the namespace first (releasing the NLB) then `terraform destroy`. If the
S3 bucket has versioned objects, empty it manually before re-running.

## Notes / production hardening

- `db_password` in `terraform.tfvars` is convenient for a demo; prefer a
  Terraform-generated random password or rotation via Secrets Manager.
- Set `deletion_protection = true` and `skip_final_snapshot = false` on RDS,
  and enable `backend "s3"` state in `main.tf` for team use.
- `query-service` has no `/health` route, so its probes are TCP. `index-service`
  uses native gRPC probes; `summarize-service` uses `GET /health`.
- The S3 bucket + IRSA role are provisioned and wired, ready for the
  index-service to archive raw logs; the app reads `LOG_ARCHIVE_BUCKET` from its
  environment.
