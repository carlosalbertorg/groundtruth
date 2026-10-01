# groundtruth

Self-hosted, open-source drift detection for Terraform and OpenTofu.

`driftctl`, previously the leading open-source tool for this, has been in maintenance mode since 2023. The alternatives that exist today are either paid SaaS platforms or early/unmaintained projects. groundtruth is a free, self-hosted replacement focused on one thing: periodically running `terraform plan -refresh-only` against your existing workspaces, showing you exactly what changed outside of Terraform, and alerting you when it does.

- **Read-only.** groundtruth detects and reports drift; it never runs `apply` and cannot modify your infrastructure.
- **No cloud credentials stored.** Credentials are supplied by the operator at runtime (an env file you mount yourself) and are never written to groundtruth's database.
- **No shell, no hand-rolled parsing.** Terraform/OpenTofu are invoked via their official Go libraries (`terraform-exec`/`tofu-exec`), and plan output is parsed with HashiCorp's own `terraform-json`.
- **Single binary, single process.** An embedded SQLite database and an embedded frontend — nothing else to run to self-host this.

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full security model and the reasoning behind these decisions.

## Quickstart

```sh
mkdir groundtruth && cd groundtruth
curl -O https://raw.githubusercontent.com/carlosalbertorg/groundtruth/main/deploy/docker-compose.yml
mkdir modules
# Put a Terraform or OpenTofu root module in ./modules - its own backend
# block controls where its real state lives (S3, GCS, Terraform Cloud,
# a local backend, ...). groundtruth only reads it.
docker compose up -d
```

Then open `http://localhost:8080`, create the first admin account, and add a workspace pointing at `/modules/<your-module>` (the path *inside* the container — see the volume mount in `docker-compose.yml`).

If your module's provider needs credentials that aren't already available to it (an attached IAM role, ambient environment variables, etc.), mount a dotenv-format file and point the workspace's "credential env file" at its in-container path. It's read fresh on every check and never stored — see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md#credentials-never-stored).

## Configuration

Environment variables, all optional:

| Variable | Default | Meaning |
|---|---|---|
| `GROUNDTRUTH_ADDR` | `:8080` | HTTP listen address |
| `GROUNDTRUTH_DATA_DIR` | `./data` | Where the SQLite database, plugin cache, and check scratch space live |
| `GROUNDTRUTH_BASE_URL` | unset | groundtruth's externally-visible URL. Set this (with `https://`) once behind TLS, so the session cookie is marked `Secure`, and so alert payloads can include a dashboard link |
| `GROUNDTRUTH_MAX_CONCURRENT_CHECKS` | `3` | How many drift checks the scheduler runs at once, across all workspaces |

## Building from source

```sh
make build-frontend   # builds web/ and embeds it into internal/webassets
go build ./cmd/groundtruth
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full development setup, including running the test suite.

## Contributing

Contributions are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request — in particular, open an issue first for anything beyond a small fix. This project follows the [Code of Conduct](CODE_OF_CONDUCT.md). Security issues should be reported privately per [SECURITY.md](SECURITY.md), not as a public issue.

## License

[Apache-2.0](LICENSE)
