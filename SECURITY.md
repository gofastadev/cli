# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| latest  | Yes       |

The full set of currently-supported `(cli, gofasta)` pairs is declared in [`COMPATIBILITY.md`](https://github.com/gofastadev/release/blob/main/COMPATIBILITY.md). A pair leaves support 90 days after a newer compatible row publishes — see [`CADENCE.md`](https://github.com/gofastadev/release/blob/main/CADENCE.md) for the support-window policy.

## Reporting a Security Issue

**Please do not report security issues through public GitHub issues.**

Instead, use [GitHub's private vulnerability reporting](https://github.com/gofastadev/cli/security/advisories/new) to submit a report. Include:

- A description of the issue and its potential impact
- Steps to reproduce or a proof-of-concept
- Any suggested fix if you have one

## Response timelines

We commit to the following service-level objectives (SLOs) for any privately-reported vulnerability. The same SLOs are mirrored in [`CADENCE.md`](https://github.com/gofastadev/release/blob/main/CADENCE.md) — this section and that document are kept in sync.

| Phase | SLO |
|-------|-----|
| Acknowledgment of your report | ≤ 72 hours from receipt |
| Initial triage and severity assignment | ≤ 7 days from receipt |
| Patch released — Critical (CVSS ≥ 9.0) | ≤ 7 days from confirmation |
| Patch released — High (CVSS 7.0–8.9) | ≤ 14 days from confirmation |
| Patch released — Moderate (CVSS 4.0–6.9) | ≤ 30 days from confirmation |
| Patch released — Low (CVSS < 4.0) | Bundled with the next scheduled minor release |
| Reporter credit in release notes | At publication, unless you request anonymity |

Severity follows the [CVSS v3.1](https://www.first.org/cvss/v3.1/specification-document) framework. If we expect to miss an SLO, we post an explanation on the advisory thread before the deadline.

## Coordination

We use [GitHub Security Advisories](https://docs.github.com/en/code-security/security-advisories) for coordination. Advisories become public once a patch has shipped and adopters have had a reasonable window to upgrade. We will credit reporters in the advisory and the release notes unless anonymity is requested.

## Scope

This policy covers the `github.com/gofastadev/cli` tool. Vulnerabilities in third-party dependencies should be reported to their respective maintainers; if a dependency advisory affects gofasta users, we will issue a coordinated patch under the SLOs above.
