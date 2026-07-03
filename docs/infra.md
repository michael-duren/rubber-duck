# Infra

## Initial Setup

### CI/CD (GitHub Actions)

`.github/workflows/`: `test.yml` (reusable — vet, templ staleness check,
unit + store-integration tests against a Postgres service container) is
called by both `ci.yml` (every PR and push to `main`) and `cd.yml` (push
to `main`: re-runs `test.yml`, then builds/pushes images and runs
`tofu apply` against `getcracked-touch-grass`).

CD's `deploy` job auto-approves `tofu apply` (no TTY on a runner), so the
human-in-the-loop check for infra changes has to happen _before_ apply
runs, not during it. That's a GitHub **environment** with required
reviewers — a merge to `main` builds and pushes images immediately, but
`tofu apply` waits for someone to click approve on the run. **This isn't
optional**: `infra/network.tf` is already on `main`, deliberately un-applied
pending manual review (`issues/ops/03-job-egress-lockdown.md`) — without
the gate, the first CD run applies it unreviewed.

One-time setup, in addition to the steps under "One-time setup" above:

```sh
PROJECT_ID=getcracked-touch-grass
REPO=michael-duren/rubber-duck

# Remote Terraform state (a GitHub runner has no local state to apply
# against). Versioning protects against a bad apply corrupting state.
gcloud storage buckets create "gs://${PROJECT_ID}-tfstate" \
  --project="$PROJECT_ID" --location=us-central1 --uniform-bucket-level-access
gcloud storage buckets update "gs://${PROJECT_ID}-tfstate" --versioning

# Migrate existing local state (infra/terraform.tfstate) into the bucket —
# run once, from a checkout that already has real state, before the first
# CD run. Confirm "yes" to copy state into the new gcs backend.
(cd infra && tofu init -migrate-state)

# Workload Identity Federation: lets Actions authenticate as a GCP service
# account with no long-lived key stored in GitHub.
PROJECT_NUMBER=$(gcloud projects describe "$PROJECT_ID" --format='value(projectNumber)')

gcloud iam workload-identity-pools create github \
  --project="$PROJECT_ID" --location=global --display-name="GitHub Actions"

gcloud iam workload-identity-pools providers create-oidc github-actions \
  --project="$PROJECT_ID" --location=global --workload-identity-pool=github \
  --display-name="GitHub Actions" \
  --attribute-mapping="google.subject=assertion.sub,attribute.repository=assertion.repository" \
  --attribute-condition="assertion.repository=='${REPO}'" \
  --issuer-uri="https://token.actions.githubusercontent.com"

gcloud iam service-accounts create gh-deployer \
  --project="$PROJECT_ID" --display-name="GitHub Actions CD deployer"

gcloud iam service-accounts add-iam-policy-binding \
  "gh-deployer@${PROJECT_ID}.iam.gserviceaccount.com" \
  --project="$PROJECT_ID" --role=roles/iam.workloadIdentityUser \
  --member="principalSet://iam.googleapis.com/projects/${PROJECT_NUMBER}/locations/global/workloadIdentityPools/github/attribute.repository/${REPO}"

# Terraform manages IAM bindings and service accounts (internal/iam.tf), so
# roles/editor alone isn't enough — it excludes IAM/security-sensitive
# permissions by design. It also doesn't cover Artifact Registry pushes
# (confirmed: `docker push` fails with `artifactregistry.repositories
# .uploadArtifacts denied` under editor alone) — hence the explicit
# artifactregistry.writer grant below.
for role in roles/editor roles/resourcemanager.projectIamAdmin roles/iam.serviceAccountAdmin roles/artifactregistry.writer; do
  gcloud projects add-iam-policy-binding "$PROJECT_ID" \
    --member="serviceAccount:gh-deployer@${PROJECT_ID}.iam.gserviceaccount.com" --role="$role"
done
```



Then in the GitHub repo settings:

- **Settings > Environments**: create `production`, add yourself (or the
  team) as a required reviewer.
- **Settings > Secrets and variables > Actions > Variables**: set
  `GCP_PROJECT_ID=getcracked-touch-grass`,
  `GCP_WORKLOAD_IDENTITY_PROVIDER=projects/${PROJECT_NUMBER}/locations/global/workloadIdentityPools/github/providers/github-actions`,
  `GCP_DEPLOYER_SA=gh-deployer@getcracked-touch-grass.iam.gserviceaccount.com`.
  No secrets needed — WIF is keyless.
