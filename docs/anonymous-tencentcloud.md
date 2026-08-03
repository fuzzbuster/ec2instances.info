# Tencent Cloud CVM without credentials

Without a complete credential pair, Tencent Cloud CVM scraping uses the
purchase workbench endpoints and does not require a Secret ID or Secret Key.

## Data sources

- Region discovery:
  `https://workbench.cloud.tencent.com/cgi/area/queryCvmRegion`.
- Per-region and per-zone configuration:
  `https://workbench.cloud.tencent.com/cgi/api?i=cvm/DescribeZoneInstanceConfigInfos&uin=&region=<region>`.

The scraper requests Linux configurations for prepaid, hourly and Spot billing.
It aggregates instance specifications, CPU model, local disks, prices,
sellable availability zones and regional coverage. Duplicate zone records use
the lowest observed price for each price type.

## Optional credentials

When both `TENCENTCLOUD_SECRET_ID` and `TENCENTCLOUD_SECRET_KEY` are set, the
scraper switches to the official `DescribeInstanceTypeConfigs` path. This path
provides official specifications but does not merge the anonymous prices,
sellable availability zones, CPU model or local disk fields.

## Fallback and risk

The workbench endpoints are public but are not a documented stable API
contract. If they fail or return no instances, the scraper falls back to its
embedded seed specifications. Seed output is intentionally less complete and
does not represent real-time inventory.
