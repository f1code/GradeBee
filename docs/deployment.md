# Deployment

GradeBee deploys to Scaleway: serverless function (Go backend) + Object Storage (frontend SPA).

## Prerequisites

- [Terraform](https://terraform.io) ≥ 1.0
- [AWS CLI](https://aws.amazon.com/cli/) (for S3-compatible frontend upload)
- Scaleway account with API keys
- Go, Node.js

## AWS CLI Configuration for Scaleway

Create a profile for Scaleway S3:

```bash
aws configure --profile scaleway
# Access Key: your Scaleway access key
# Secret Key: your Scaleway secret key
# Region: fr-par
```

Then either `export AWS_PROFILE=scaleway` or add it to your `.env` file.

## Setup

1. Copy and fill in Terraform variables:

```bash
cp infra/terraform.tfvars.example infra/terraform.tfvars
# Edit infra/terraform.tfvars with your values
```

2. Initialize Terraform:

```bash
cd infra && terraform init
```

## Deploy Everything

```bash
make deploy
```

This runs:
1. `make build` — vendors Go deps, builds & zips backend
2. `make terraform` — applies Terraform (creates/updates function + bucket)
3. `make deploy-frontend` — builds frontend with API URL from Terraform outputs, syncs to S3 bucket

## Individual Targets

| Command | Description |
|---------|-------------|
| `make build` | Build backend zip |
| `make terraform` | Apply Terraform |
| `make build-frontend` | Build frontend (needs Terraform outputs) |
| `make deploy-frontend` | Build + upload frontend to S3 |
| `make dev` | Local frontend dev server |
| `make clean` | Remove dist/ |

## Environment Variables

Configured via `infra/terraform.tfvars`:

| Variable | Description |
|----------|-------------|
| `project_id` | Scaleway project ID |
| `region` | Scaleway region (default: `fr-par`) |
| `clerk_secret_key` | Clerk backend secret key |
| `clerk_publishable_key` | Clerk frontend publishable key |
| `openai_api_key` | OpenAI API key (Whisper + GPT) |
