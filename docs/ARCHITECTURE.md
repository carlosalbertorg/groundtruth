# Architecture

groundtruth runs `terraform`/`tofu plan -refresh-only` against infrastructure you already manage, and tells you what changed outside of Terraform. It's designed to be trustworthy to run against production: this document explains the specific decisions that make that true, not just the ones that make it work.

## What it does, in one pass

For each enabled workspace, on a schedule or on demand:

1. Copy the workspace's module source into a fresh, disposable directory.
2. Run `init` then `plan -refresh-only` there, with the operator's credentials injected only into that one subprocess.
3. Parse the resulting plan, redact anything Terraform marked sensitive or not-yet-known, and discard the disposable directory.
4. Persist the redacted result and, if the drift status changed, notify.

Nothing in that pipeline writes anything but the redacted result to disk permanently. The sections below justify each step.

## Threat model

groundtruth is a **single-operator, self-hosted admin tool**, not a multi-tenant service. The dashboard is behind authentication, and whoever can reach it already has the ability to configure a workspace pointed at real infrastructure. The security decisions here are about limiting what a *bug* in groundtruth, or a *compromise of groundtruth's own database*, can expose - not about defending against the operator themselves.

## Execution: isolation, not sandboxing

Each check gets its own `os.MkdirTemp` directory, used for exactly one `init`/`plan`/`show` cycle and removed via `defer` before the check returns - including on a panic, a timeout, or the process receiving SIGTERM. The directory can contain the real plan file, which can itself contain unredacted sensitive values; it never outlives the check that created it.

This is isolation between checks, not a security sandbox around the `terraform`/`tofu` binary itself. Those binaries and the providers they load are trusted code, the same way they would be if you ran them yourself from a terminal. groundtruth's job is to make sure *its own* mistakes can't leak something that belonged to one specific check.

No shell is ever invoked. Both `hashicorp/terraform-exec` and `opentofu/tofu-exec` build `exec.Command` argv arrays internally; groundtruth never constructs a command line by concatenating strings.

## Credentials: never stored

A workspace's `credential_env_file` is a path to a dotenv-format file the **operator** mounts into the container (a Kubernetes Secret volume, a Docker secret, a SOPS-decrypted file, a Vault Agent template sink - whatever secret pipeline the operator already runs). groundtruth:

- Reads it fresh from disk on every single check. Nothing from it is cached in memory between checks, logged, or written to groundtruth's own database.
- Merges it into exactly one subprocess's environment via `tfexec`/`tofuexec`'s `SetEnv`, which **replaces** the subprocess environment rather than merging with groundtruth's own - confirmed against the library source, not assumed.
- Never echoes its contents. See "A specific leak this caught," below - this rule is enforced deliberately, not by accident.

**Why not encrypt credentials and store them in the database instead** (a "home lab" mode without an external secrets pipeline)? Because "we never hold your cloud credentials" is a stronger, simpler-to-verify trust story than "we hold them, encrypted, trust our crypto." A subtle bug in hand-rolled key derivation or nonce handling is a realistic risk for a tool built by a small team, and a severe credibility hit if ever found. This mirrors Atlantis' own precedent - the closest comparable "wraps terraform as a service" tool doesn't store cloud credentials either.

If demand for a no-external-secrets-manager mode materializes, here is the scheme that would be used, specified now so it's reviewed as a decision rather than improvised under pressure:

- A required `GROUNDTRUTH_MASTER_KEY` (32 bytes, base64), never written to disk by groundtruth itself.
- Per-record keys derived via HKDF-SHA256 from the master key, a purpose string, and a random per-record salt - never using the raw master key directly.
- AES-256-GCM, a fresh `crypto/rand` 12-byte nonce per encryption call, stored as `nonce || ciphertext || tag`.
- Decryption only in-memory, immediately before injecting into the subprocess environment.

This is unbuilt. It is not a half-finished feature hidden somewhere; it's a specification waiting for a real reason to implement it.

## Redaction: before anything is persisted

Terraform's plan JSON marks sensitive and not-yet-known values with a parallel tree of `true`/`false` markers (`BeforeSensitive`, `AfterSensitive`, `AfterUnknown` on each `ResourceChange`), matching the shape of the real value - sometimes at a leaf, sometimes marking a whole subtree at once. `internal/drift` walks that tree and replaces every marked value with a fixed placeholder (`"(sensitive value)"` or `"(known after apply)"`) before the result is turned into anything that gets stored or returned by the API.

This conversion happens exactly once, in one place, and the *only* representation of a plan that leaves that function is the redacted one. Fixture tests cover a plain update, a wholly-sensitive attribute, a sensitive value nested inside an otherwise-visible map, and an unknown-until-apply attribute - the shapes that are easy to get subtly wrong.

A second thing is worth stating plainly: groundtruth reads `plan.ResourceDrift`, not `plan.ResourceChanges`. Terraform documents `resource_drift` as "the changes detected when it compared the most recent state to the prior saved state" - exactly what this tool means by drift. `resource_changes` describes what Terraform would do to match *configuration*, a different question groundtruth never asks, since it never runs anything but a refresh-only plan.

## A specific leak this caught

During a hardening pass, we found that `godotenv`'s own parse errors can embed the offending source line verbatim - an "unterminated quoted value" error includes the raw text, quote and all. For most files that's a reasonable debugging aid. For a *credentials* file, that line is exactly the one piece of text that must never reach a log, an HTTP response, or a stored `error_message`.

The fix: a credential file that can't be *opened* (not found, permission denied) surfaces the real OS error, which is just a path and an error code - safe. A file that opens but fails to *parse* surfaces a fixed, generic message instead of whatever the parser produced. This is tested by planting a secret-looking value in a malformed file and asserting it never appears in the resulting error.

The broader rule this represents: **any error that passed through code touching credential content gets treated as untrusted text**, not assumed safe just because it came from a well-regarded library. It's worth rechecking this assumption again if the credential-reading code ever changes.

## What groundtruth deliberately doesn't manage

- **The module source.** A workspace points at a filesystem path the operator keeps current via their own existing pipeline (a volume mount, a git-sync sidecar, whatever GitOps they already run). groundtruth never clones a repository and never holds VCS credentials.
- **The backend.** The module's own `backend` block (S3, GCS, Azure Blob, Terraform Cloud, or - as in groundtruth's own test fixtures - a local backend pointed at a path outside the module) is what `init` connects to. groundtruth doesn't configure, migrate, or lock state.
- **Remediation.** groundtruth never runs `apply`. It is read-only by design: it can tell you what changed, never change anything itself. This keeps the trust bar for adopting it low and the blast radius of a bug in it bounded to "wrong information displayed," never "infrastructure modified."

## Concurrency: a scheduler, not a queue

A single poll loop (`internal/scheduler`) wakes every 30 seconds, asks the database which enabled workspaces are due (never checked, or their last check started longer ago than their own `check_interval_minutes`), and runs each due workspace through the same `internal/checks.Service` the manual "check now" endpoint uses - one shared code path, so a check's outcome is recorded identically regardless of what triggered it.

A semaphore bounds how many checks run at once (`GROUNDTRUTH_MAX_CONCURRENT_CHECKS`, default 3), and an in-memory set prevents a workspace already running from being started a second time by the next tick. On shutdown (SIGTERM), the scheduler's context is cancelled - in-flight `terraform` subprocesses are killed via that same cancellation (standard `exec.CommandContext` behavior), and their `defer`-based temp-directory cleanup still runs before the process exits, because Go's `defer` runs regardless of *why* a function returned.

This is a single-process design. Running multiple groundtruth instances against the same database is a real architectural fork point (coordinating which instance owns which workspace), not something this scheduler attempts - a stated non-goal for v1, not a silent gap.

## Alerting: synchronous, and silent on repeats

`internal/alerting` is called inline, after a check is persisted, with the workspace's previous and new status. It fires exactly on a *transition* - clean-or-never-checked into drifted, drifted back to clean, anything into failed - and is deliberately silent when the status doesn't change, including a workspace that's been drifted for days: it alerted once, and re-alerting on every single check would train operators to ignore it. A time-based "still unresolved" re-notification is a plausible follow-up; it isn't built, because guessing at the right interval without real usage data would be worse than not having it.

Delivery is synchronous (blocking the check's completion by at most a few seconds) rather than fire-and-forget, deliberately: checks already run minutes apart, and a synchronous call is far simpler to reason about and test than managing background goroutine lifecycles correctly through a shutdown. A webhook's shared secret, if set, HMAC-signs the outgoing body (`X-Groundtruth-Signature: sha256=...`), mirroring the convention GitHub and Stripe use.

## Authentication

Dashboard sessions are opaque random tokens (32 bytes from `crypto/rand`); only a SHA-256 hash is ever stored, so a database read alone can't be replayed as a live session. There is no session-signing secret anywhere in the system - one fewer secret for groundtruth itself to protect. Passwords are bcrypt-hashed at cost 12 (above bcrypt's own default of 10: this dashboard reaches real cloud credentials). CSRF protection uses Go's standard-library `http.CrossOriginProtection` rather than a third-party library, checking `Sec-Fetch-Site`/`Origin` against `Host` - this is origin-based, not session-based, so it covers every mutating route uniformly, including setup and login before any session cookie exists.

API tokens (for CI-triggered checks) follow the identical pattern - opaque value, hashed at rest, scoped to the user who created them for revocation.

## What a reviewer should check first

If you're auditing this project, the highest-value places to look are:

1. `internal/drift/redact.go` - does the redaction walker actually cover every shape Terraform can mark sensitive?
2. `internal/terraform/executor.go` and `credentials.go` - does anything on the path from "read the credential file" to "subprocess exits" risk writing or echoing a credential value?
3. `internal/checks/service.go` - is the redacted result really the only form of a check that reaches the database?

Everything else follows from those three files being correct.
