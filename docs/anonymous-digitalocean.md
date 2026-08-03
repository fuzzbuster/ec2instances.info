# DigitalOcean without credentials

DigitalOcean's authenticated Droplet API is not used. The scraper combines two
public website sources instead.

## Data sources

- Droplet pricing page:
  `https://www.digitalocean.com/pricing/droplets`.
- Regional availability Markdown:
  `https://docs.digitalocean.com/platform/regional-availability/index.html.md`.

Droplet plan objects are extracted from JSON embedded in pricing-page script
tags. The Markdown document supplies region names and a plan-family by region
availability matrix. The scraper joins both sources to emit vCPU, memory, disk,
network, transfer and hourly pricing.

## Credentials and limits

No DigitalOcean token is read or required. The output covers Droplet plans
published in the pricing page, not every product available through the
authenticated API. Architecture is currently reported as `x86_64`.

## Failure behavior and risk

Both website documents are required. Missing plan objects, region rows or
availability rows fail the scrape. HTML and Markdown layout changes can require
parser updates; there is no static fallback.
