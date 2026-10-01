# groundtruth

[![CI](https://github.com/carlosalbertorg/groundtruth/actions/workflows/ci.yml/badge.svg)](https://github.com/carlosalbertorg/groundtruth/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/carlosalbertorg/groundtruth)](https://github.com/carlosalbertorg/groundtruth/releases/latest)
[![License](https://img.shields.io/github/license/carlosalbertorg/groundtruth)](LICENSE)

**Self-hosted drift detection for Terraform and OpenTofu.** Someone edits a config file by hand, rotates a secret outside your pipeline, or changes a managed resource in a cloud console — groundtruth notices before it becomes an incident, and tells you exactly what changed. It watches what Terraform already manages; it doesn't go looking for resources created outside of it (see [Limitations](#limitations)).

`driftctl`, previously the leading open-source tool for this, has been in maintenance mode since 2023. What's left is either paid SaaS (Spacelift, env0, Firefly, Scalr) or small, unmaintained projects. groundtruth is a free, self-hosted alternative focused on doing one thing well: periodically running `terraform plan -refresh-only` against your existing workspaces, showing you precisely what changed outside of Terraform, and alerting you when it does.

![Drift detected: a credentials file and a feature-flag file were changed outside Terraform](docs/images/drift-diff.png)

In that screenshot, two files no longer match what Terraform recorded. Both show as `Deleted`: the `local` provider reports a file whose content differs from the state as removed, and groundtruth shows exactly what Terraform and the provider report (see [Limitations](#limitations)). The content of `db_credentials.json` is redacted — it's marked sensitive in the Terraform state, so groundtruth never stores or displays it. The content of `feature-flags.json` isn't sensitive, so it's shown in full. That's the entire trust model: redact what Terraform tells you is sensitive, show the rest plainly.

## What it does

- **Read-only.** groundtruth detects and reports drift; it never runs `apply` and cannot modify your infrastructure. It doesn't take your backend's state lock either, so it can't block your pipeline, or leave a stale lock behind if a check is killed.
- **No cloud credentials stored.** Credentials are supplied by the operator at runtime (an env file you mount yourself). They're never written to groundtruth's database, and their values are scrubbed from error messages before those are stored. (An alert destination's webhook secret is different: it has to be stored — see [what groundtruth does store](docs/ARCHITECTURE.md#what-groundtruth-does-store).)
- **No shell, no hand-rolled parsing.** Terraform/OpenTofu are invoked via their official Go libraries (`terraform-exec`/`tofu-exec`), and plan output is parsed with HashiCorp's own `terraform-json`.
- **Sensitive values are redacted before they're ever stored**, not just before they're displayed — see the screenshot above.
- **Single binary, single process.** An embedded SQLite database and an embedded frontend — nothing else to run to self-host this.

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full security model and the reasoning behind these decisions.

## How it works

Your existing pipeline keeps applying Terraform however it already does — groundtruth never touches that. It just watches:

1. On a schedule (or on demand, via API), groundtruth runs `terraform plan -refresh-only` against a workspace.
2. That's a real Terraform/OpenTofu process, refreshing state against actual infrastructure — no config changes are proposed, it only asks "does the state still match reality?"
3. Any difference is drift. Values Terraform marks sensitive are redacted before the result is ever written to disk.
4. A real state transition — clean → drifted, drifted → clean, anything → failed — fires an alert. Staying drifted doesn't page anyone twice.

Each check runs in an isolated, throwaway directory, with provider plugins taken from a cache shared by all checks. Credentials are read fresh from the file you point a workspace at and injected only into that one subprocess — never logged, never persisted, gone when the check ends.

### Acting on drift

groundtruth tells you; what to do about it stays with you. If the change was intentional (an emergency fix made in the console, say), update your Terraform code to match and let your pipeline `apply` it — or run `terraform apply -refresh-only` to accept the change into the state. If it wasn't, re-apply your existing code to put the infrastructure back. Either way, the next check sees the state and reality agree and sends a "resolved" alert.

Two things to know. groundtruth compares the *state* with reality, so editing the code alone doesn't clear drift — the state has to be reconciled by an `apply`. And there's no "acknowledge" or "mute": a workspace stays `drifted` until the state and reality agree again, and you're alerted once per transition, not on every check.

![Two workspaces: one clean, one drifted](docs/images/workspaces.png)

## Example: catching this from CI

API tokens let CI trigger a check without a browser session — useful right after a deploy, or on a schedule independent of groundtruth's own scheduler:

```sh
curl --fail-with-body -X POST https://groundtruth.internal/api/workspaces/$WORKSPACE_ID/check \
  -H "Authorization: Bearer $GROUNDTRUTH_TOKEN"
```

The response is the check itself, and its `status` is `clean`, `drifted` or `failed` — a drifted workspace is still a `200`, so pipe it to `jq -e '.status == "clean"'` if the job should fail on drift. `--fail-with-body` makes curl exit non-zero on an error status, such as the `409` you get when a check is already running for that workspace; without it, curl treats any HTTP response as success. A check started with a token is recorded in the history as triggered by `api`.

Every check is kept in history, so you can see exactly when a workspace went from clean to drifted:

![Check history for a workspace, showing a transition from clean to drifted](docs/images/workspace-history.png)

Point an alert destination at a Slack webhook or your own HTTP endpoint and you'll hear about it the moment it happens — not when someone notices the dashboard:

![Alert destinations and API tokens configured in Settings](docs/images/settings.png)

Generic webhooks are signed (`X-Groundtruth-Signature: sha256=...`, HMAC over the raw body) so your receiver can verify the request actually came from groundtruth:

```json
{
  "event": "drift_detected",
  "workspace": { "id": "8dc9a7c8-...", "name": "platform-config" },
  "check": {
    "id": "ee71937a-...",
    "status": "drifted",
    "started_at": "2026-10-01T09:04:39Z",
    "summary": { "added": 0, "changed": 0, "destroyed": 2 }
  },
  "dashboard_url": "https://groundtruth.internal/workspaces/8dc9a7c8-...",
  "timestamp": "2026-10-01T09:04:39Z"
}
```

## groundtruth vs. the alternatives

| | groundtruth | driftctl | Paid SaaS |
|---|---|---|---|
| Cost | Free | Free | Per-resource or per-month |
| Maintained | Actively | No (since 2023) | Yes |
| Self-hosted | Yes | Yes | Rarely, or enterprise-only |
| Your credentials | Read once per check, never stored | Held by driftctl to scan cloud APIs directly | Stored on the vendor's infrastructure |
| How it detects drift | Real `terraform plan -refresh-only` | Direct cloud API enumeration | Varies by vendor |
| Finds resources created outside Terraform | No | Yes | Varies by vendor |

## Limitations

groundtruth delegates detection to `terraform plan -refresh-only`. That keeps it accurate to how Terraform itself sees your infrastructure, and it's also what bounds it:

- **Only resources Terraform manages.** A resource created by hand in a console and never imported isn't drift to Terraform, so it never shows up. (This is what driftctl's API scan covered and groundtruth deliberately doesn't. As a consequence the `added` count is always 0.)
- **State against reality, not code against reality.** An unapplied change to your `.tf` files isn't drift here; `terraform plan` is the tool for that.
- **The provider decides what's reported, and how.** A provider only reports attributes it reads back, and some report an edited resource as deleted — that's why the screenshot above says `Deleted`. Read the real change from the before/after values.
- **A check can overlap your `apply`.** It takes no state lock, so it neither blocks your pipeline nor is blocked by it, but a check that runs while an `apply` is mid-flight may report drift that is gone by the next check.
- **Pin your provider versions.** Every check starts in a fresh directory, so without a committed `.terraform.lock.hcl`, `init` picks the newest provider versions your constraints allow, and a provider release can change what a check reports. Commit the lock file.
- **Redaction follows Terraform's own markers.** A value the provider doesn't mark sensitive is shown in full, even if it holds a secret.

See [Detection model and its limits](docs/ARCHITECTURE.md#detection-model-and-its-limits) for the reasoning.

## Quickstart

```sh
mkdir groundtruth && cd groundtruth
curl -O https://raw.githubusercontent.com/carlosalbertorg/groundtruth/main/deploy/docker-compose.yml
mkdir modules
# Put a Terraform or OpenTofu root module in ./modules - its own backend
# block controls where its real state lives (S3, GCS, Terraform Cloud,
# a local backend, ...). groundtruth only reads it. Commit the module's
# .terraform.lock.hcl too, so provider versions stay pinned.
docker compose up -d
```

Then open `http://localhost:8080`, create the first admin account, and add a workspace pointing at `/modules/<your-module>` (the path *inside* the container — see the volume mount in `docker-compose.yml`).

**Create the admin account right away.** Until it exists, anyone who can reach the port can create it. That's why `docker-compose.yml` publishes port 8080 on `127.0.0.1` only; to reach groundtruth from elsewhere, put a TLS-terminating reverse proxy in front of it (and set `GROUNDTRUTH_BASE_URL`) rather than publishing the port on every interface.

If your module's provider needs credentials that aren't already available to it (an attached IAM role, ambient environment variables, etc.), uncomment the `credentials.env` volume in `docker-compose.yml`, create that dotenv-format file, and point the workspace's "credential env file" at `/secrets/credentials.env`. It's read fresh on every check and never stored — see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md#credentials-never-stored).

## Configuration

Environment variables, all optional:

| Variable | Default | Meaning |
|---|---|---|
| `GROUNDTRUTH_ADDR` | `:8080` | HTTP listen address |
| `GROUNDTRUTH_DATA_DIR` | `./data` | Where the SQLite database, plugin cache, and check scratch space live |
| `GROUNDTRUTH_BASE_URL` | unset | groundtruth's externally-visible URL. Set this (with `https://`) once behind TLS, so the session cookie is marked `Secure`, and so alert payloads can include a dashboard link |
| `GROUNDTRUTH_MAX_CONCURRENT_CHECKS` | `3` | How many drift checks the scheduler runs at once, across all workspaces. Manual and API-triggered checks aren't counted against this, but only one check runs per workspace at a time (a second request gets `409`) |

Behind a reverse proxy, groundtruth sees the proxy's address for every client, so its login rate limit (10 attempts a minute, burst of 5) becomes one budget shared by everyone. See [Authentication](docs/ARCHITECTURE.md#authentication).

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
