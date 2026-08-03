---
name: "ec2instances"
description: "Scrapes cloud VM specifications and pricing with structured JSON. Invoke when an agent needs current compute-instance data from supported providers."
---

# ec2instances

Use the `ec2instances` CLI to collect cloud virtual-machine specifications,
availability, and pricing. This skill covers compute instances only. Do not use
it for RDS, managed databases, caches, data warehouses, or search services.

## Workflow

1. Confirm the binary is available:

   ```sh
   ec2instances --json version
   ```

2. Inspect supported providers and credential requirements:

   ```sh
   ec2instances --json providers
   ```

3. Select only the providers required for the task. Never start an implicit
   full-provider scrape.

4. Run the scrape with an explicit output directory:

   ```sh
   ec2instances --json scrape \
     --providers tencentcloud,volcengine \
     --output-dir ./output
   ```

5. Parse the single JSON object from stdout. Treat stderr as diagnostic logs.

6. Accept the snapshot only when the exit code is `0`, `status` is `ok`, and
   `partial` is `false`.

## Configuration

Provider precedence:

1. `--providers`
2. `EC2INSTANCES_PROVIDERS`
3. `ALLOWED_SERVICES`

Output directory precedence:

1. `--output-dir`
2. `EC2INSTANCES_OUTPUT_DIR`
3. `./output`

Required credentials:

- `aws`: `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`
- `azure`: `AZURE_TENANT_ID`, `AZURE_CLIENT_ID`,
  `AZURE_CLIENT_SECRET`, `AZURE_SUBSCRIPTION_ID`
- `gcp`: `GCP_PROJECT_ID`, `GCP_CLIENT_EMAIL`, `GCP_PRIVATE_KEY`

Alibaba Cloud, Tencent Cloud, Volcengine, and Huawei Cloud can use optional
credentials to enrich their static seed data. Vultr, Linode, DigitalOcean, and
Hetzner need no credentials.

Never print credentials or place them in command arguments. Pass them through
the process environment.

## Results

Every dataset has three files:

```text
instances.json
instances.json.gz
instances.json.br
```

The exact relative directory depends on the provider. Use the absolute
`output_dir` returned by the command as the root.

Scrape result fields:

- `succeeded`: providers that completed
- `failed`: provider name and error message
- `partial`: true when execution produced an incomplete snapshot
- `error`: top-level failure summary

Exit codes:

- `0`: complete success
- `1`: runtime/provider failure; files may be partial
- `2`: invalid arguments, provider selection, or credentials

Do not silently consume files from a failed or partial run. Report the failed
providers and let the caller decide whether successful provider files are
usable.

## Installation

Use a matching archive from:

```text
https://github.com/fuzzbuster/ec2instances.info/releases
```

Verify the archive against `checksums.txt`. Supported release targets are Linux
and macOS on amd64 and arm64.
