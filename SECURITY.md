# Security Policy

groundtruth is designed to run `terraform`/`tofu` against real infrastructure and to be reachable by credentials you provide — security issues here are taken seriously.

## Reporting a vulnerability

Please **do not** open a public GitHub issue for a security vulnerability.

Instead, use GitHub's private reporting feature: go to the [Security tab](../../security) of this repository and click **"Report a vulnerability"**. This opens a private advisory visible only to you and the maintainers, so the issue can be discussed and fixed before any public disclosure.

Please include:

- A description of the vulnerability and its potential impact.
- Steps to reproduce it (a minimal example, if possible).
- The version/commit you tested against.

## What to expect

- An acknowledgement as soon as reasonably possible.
- An assessment of the report and, if confirmed, a plan for a fix.
- Credit in the advisory and release notes, unless you'd prefer to stay anonymous.

## Scope

In scope: the groundtruth server and its official Docker image. Out of scope: vulnerabilities in Terraform, OpenTofu, or cloud provider tooling itself — please report those upstream.

groundtruth is a single-operator admin tool and makes a few assumptions on purpose (for example, that the Terraform/OpenTofu binaries and providers it runs are trusted, and that first-run setup is open until the first admin exists). The [threat model](docs/ARCHITECTURE.md#threat-model) lists them, so you can see what groundtruth does and doesn't defend against before you report.

## Supported versions

Only the most recent tagged release (and `main`) is supported with security fixes. Upgrade to the latest release before reporting an issue you found in an older one.
