# Custom domain for the Cloud Run service

## Context

The site lives at the assigned gc-app-aauuwonajq-uc.a.run.app URL. A real
domain needs Cloud Run domain mapping (or a load balancer if/when more is
needed).

## Work

- Pick/buy the domain; add `google_cloud_run_domain_mapping` (or v2
  equivalent) + DNS records to infra/.
- Verify domain ownership in Google Search Console (manual step).

## Done when

The site serves on the custom domain with managed TLS.
