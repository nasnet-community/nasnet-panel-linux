# Security Policy

## Supported versions

Security fixes are applied on the latest released `v*` tag and on `main`. Older minor releases are not backported unless a release note says otherwise.

## Reporting a vulnerability

Please **do not** open a public GitHub issue for security problems.

1. Prefer [GitHub Private Vulnerability Reporting](https://github.com/nasnet-community/nasnet-panel-linux/security/advisories/new) on this repository (Security → Advisories → Report a vulnerability), if enabled for the org.
2. If that form is unavailable, open a **private** channel with the maintainers via the [NasNet Community](https://github.com/nasnet-community) contact paths listed on the org profile — still without posting exploit details publicly.

Include:

- Affected version or commit
- Impact (auth bypass, RCE, data leak, …)
- Steps to reproduce or a proof of concept
- Any workaround you already know

We will acknowledge the report when we can and coordinate a fix and disclosure timeline. Please give us a reasonable window before public disclosure.

## Scope notes

- This repository is the **Linux** panel (`nasnet-panel-linux`). The MikroTik product lives in [nasnet-panel](https://github.com/nasnet-community/nasnet-panel); report router-side issues there.
- Misconfiguration of production deployments (open admin ports, weak admin passwords, exposed SQLite paths) is out of scope for advisory credit, but tips that harden defaults are welcome as normal PRs.
