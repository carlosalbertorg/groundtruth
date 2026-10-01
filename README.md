# groundtruth

Self-hosted, open-source drift detection for Terraform and OpenTofu.

`driftctl`, previously the leading open-source tool for this, has been in
maintenance mode since 2023. The alternatives that exist today are either
paid SaaS platforms or early/unmaintained projects. groundtruth is a free,
self-hosted replacement focused on one thing: periodically running
`terraform plan -refresh-only` against your existing workspaces, showing
you exactly what changed outside of Terraform, and alerting you when it
does.

**Status: early development, not yet usable.** This README will grow a
proper quickstart, screenshots, and architecture docs as the project
reaches a working v1. See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)
(coming soon) for the security model.

## Design principles

- **Read-only.** groundtruth detects and reports drift; it never runs
  `apply` and cannot modify your infrastructure.
- **No cloud credentials stored.** Credentials are supplied by the
  operator at runtime (an env file you mount yourself) and are never
  written to groundtruth's database.
- **No shell, no hand-rolled parsing.** Terraform/OpenTofu are invoked via
  their official Go libraries (`terraform-exec`/`tofu-exec`), and plan
  output is parsed with HashiCorp's own `terraform-json`.
- **Single binary, single process.** An embedded SQLite database and an
  embedded frontend — nothing else to run to self-host this.

## Contributing

Contributions are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request — in particular, open an issue first for anything beyond a small fix. This project follows the [Code of Conduct](CODE_OF_CONDUCT.md). Security issues should be reported privately per [SECURITY.md](SECURITY.md), not as a public issue.

## License

[Apache-2.0](LICENSE)
