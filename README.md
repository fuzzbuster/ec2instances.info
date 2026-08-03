# ec2instances CLI

`ec2instances` collects cloud virtual-machine specifications, regional
availability, and pricing as JSON. This `scraper-cli` branch is intended for AI
agents and automation. It does not include the original website, database
services, deployment code, or container images.

This repository is a CLI-focused fork of
[vantage-sh/ec2instances.info](https://github.com/vantage-sh/ec2instances.info).

## Install

Download the archive for Linux or macOS from
[GitHub Releases](https://github.com/fuzzbuster/ec2instances.info/releases), or
build from source with Go 1.26:

```sh
go build -trimpath -o ec2instances .
```

Release tags use the `scraper-v<semver>` format. Published targets are:

- Linux amd64 and arm64
- macOS amd64 and arm64

Each release includes `checksums.txt`.

## Usage

```text
ec2instances [--json] providers
ec2instances [--json] version
ec2instances [--json] scrape --providers <names> [--output-dir <path>]
```

Always select providers explicitly. The CLI does not start an implicit
full-cloud scrape.

```sh
ec2instances --json providers
ec2instances --json scrape \
  --providers tencentcloud,volcengine \
  --output-dir ./output
```

Provider selection uses this precedence:

1. `--providers`
2. `EC2INSTANCES_PROVIDERS`
3. `ALLOWED_SERVICES` for compatibility with the upstream scraper

The output directory uses this precedence:

1. `--output-dir`
2. `EC2INSTANCES_OUTPUT_DIR`
3. `./output`

Each dataset is written as JSON and as matching `.gz` and `.br` files.

## Providers

| Provider | Name | Required environment |
| --- | --- | --- |
| Amazon EC2 | `aws` | `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`; optional `AWS_SESSION_TOKEN` |
| Azure Virtual Machines | `azure` | `AZURE_TENANT_ID`, `AZURE_CLIENT_ID`, `AZURE_CLIENT_SECRET`, `AZURE_SUBSCRIPTION_ID` |
| Google Compute Engine | `gcp` | `GCP_PROJECT_ID`, `GCP_CLIENT_EMAIL`, `GCP_PRIVATE_KEY` |
| Alibaba Cloud ECS | `alicloud` | Optional: `ALICLOUD_ACCESS_KEY`, `ALICLOUD_SECRET_KEY` |
| Tencent Cloud CVM | `tencentcloud` | Optional: `TENCENTCLOUD_SECRET_ID`, `TENCENTCLOUD_SECRET_KEY` |
| Volcengine ECS | `volcengine` | Optional: `VOLCENGINE_ACCESS_KEY`, `VOLCENGINE_SECRET_KEY` |
| Huawei Cloud ECS | `huaweicloud` | Optional Huawei access keys, project ID, and region |
| Vultr | `vultr` | None |
| Linode | `linode` | None |
| DigitalOcean | `digitalocean` | None |
| Hetzner Cloud | `hetzner` | None |

Optional credentials enrich providers that also contain static seed data.

AWS credentials only need the `ec2:DescribeInstanceTypes` IAM permission. AWS
output includes instance specifications and non-Spot pricing, but does not
include Spot prices, interruption rates, or Spot savings estimates. The EC2 API
request uses the built-in SigV4 Query client and does not depend on the AWS SDK.

See [Azure setup](docs/setting-up-azure.md) and
[GCP setup](docs/setting-up-gcp.md) for credential creation.

## Agent Contract

With `--json`, stdout contains exactly one JSON object. Progress logs go to
stderr.

Successful scrape:

```json
{
  "status": "ok",
  "command": "scrape",
  "output_dir": "/absolute/path/output",
  "succeeded": ["tencentcloud"],
  "failed": [],
  "partial": false
}
```

Providers run independently. If one fails, the others finish and successful
files remain in the output directory:

```json
{
  "status": "error",
  "command": "scrape",
  "output_dir": "/absolute/path/output",
  "succeeded": ["tencentcloud"],
  "failed": [{"name": "aws", "error": "request failed"}],
  "partial": true,
  "error": "one or more providers failed"
}
```

Exit codes:

- `0`: success
- `1`: provider, network, parsing, or write failure
- `2`: invalid command, provider, or credential configuration

Treat any result with `partial: true` as an incomplete snapshot.

## Development

```sh
test -z "$(gofmt -l .)"
go vet ./...
go test ./...
go build ./...
```

The CLI uses only the Go standard library for command parsing. Scraper output
schemas remain compatible with the original compute-instance datasets.

## License

MIT. See [LICENSE](LICENSE). Original project copyright and attribution are
preserved.
