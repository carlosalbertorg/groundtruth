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

Three assumptions follow from that, stated so they can be checked rather than discovered:

- **First-run setup is unauthenticated by design.** Until the first admin account exists, whoever reaches the instance first can create it. The example `docker-compose.yml` therefore publishes the port on `127.0.0.1` only. Create the account before you expose the service, and keep it off untrusted networks until you have.
- **Terraform, OpenTofu and the providers are trusted code.** They run with the credentials you give them, exactly as they would from your own terminal.
- **Any reverse proxy is yours.** groundtruth serves plain HTTP and expects TLS to be terminated in front of it. [Authentication](#authentication) covers what that changes for rate limiting.

## Execution: isolation, not sandboxing

Each check gets its own `os.MkdirTemp` directory, used for exactly one `init`/`plan`/`show` cycle and removed via `defer` before the check returns - including on a panic or a timeout. On SIGTERM the signal context is cancelled; the scheduler runs under it and every HTTP request's context derives from it, so a check running inside a "check now" request is cancelled exactly like a scheduled one, and runs its cleanup before the process exits.

Cancelling is bounded. Terraform is asked to stop with SIGINT and killed if it hasn't within 5 seconds (OpenTofu is killed at once). Left to its default, `terraform-exec` waits up to a minute, and a provider stuck in a slow API call would then hold a timed-out check, and a shutdown, well past their limits. Docker's default stop timeout is 10 seconds; stopping groundtruth with checks running takes about 5.

A check never takes the backend's state lock: the plan runs with `-lock=false`. The libraries' default writes a lock record to the backend for the whole check, so an `apply` in the operator's pipeline that overlapped one would fail to get the lock, and a check killed by a timeout or at shutdown would leave a stale lock that blocks the pipeline until someone force-unlocks it. Neither fits a tool that only reads. The cost is that a check overlapping an `apply` can read the state halfway through it and report drift the next check no longer sees.

The directory can contain the real plan file, which can itself contain unredacted sensitive values. The one case a `defer` can't cover is the process being killed outright (SIGKILL, the OOM killer, a power cut), so that case is handled at the next start: before serving anything, groundtruth removes every `check-*` directory it finds in its scratch space (`Executor.SweepStale`; this assumes one groundtruth process per data directory, the only supported deployment). Between a hard kill and the next start, a plan file can sit on disk, in a directory only groundtruth's user can read.

The module source is copied into that directory without any `terraform.tfstate*` file, `.git`, or `.terraform`. A workspace whose source path contains groundtruth's own scratch directory (`/`, say, or the data directory itself) is rejected before copying: such a copy would recurse into the directory it is filling.

All checks share one provider plugin cache (`TF_PLUGIN_CACHE_DIR`, under the data directory, owner-only) so providers are downloaded once rather than on every scheduled run. It is shared storage, not per-check isolation, and sharing has two hazards that are handled explicitly:

- Terraform overwrites a cached provider binary whenever the module's lock file doesn't vouch for it, which fails with "text file busy" if another check is executing that very binary. Checks therefore run with `TF_PLUGIN_CACHE_MAY_BREAK_DEPENDENCY_LOCK_FILE=true`, which uses a cached package as it is: it was verified against the registry when first downloaded, and the cache is owner-only.
- Two checks installing the same new provider at the same moment collide. `init` therefore runs one check at a time, and a queued check waits under its own timeout. Once a provider is cached `init` only links it, so this costs next to nothing.

What keeps provider *versions* stable between checks is still a lock file, covered in [Detection model and its limits](#detection-model-and-its-limits).

This is isolation between checks, not a security sandbox around the `terraform`/`tofu` binary itself. Those binaries and the providers they load are trusted code, the same way they would be if you ran them yourself from a terminal. groundtruth's job is to make sure *its own* mistakes can't leak something that belonged to one specific check.

No shell is ever invoked. Both `hashicorp/terraform-exec` and `opentofu/tofu-exec` build `exec.Command` argv arrays internally; groundtruth never constructs a command line by concatenating strings.

## Credentials: never stored

A workspace's `credential_env_file` is a path to a dotenv-format file the **operator** mounts into the container (a Kubernetes Secret volume, a Docker secret, a SOPS-decrypted file, a Vault Agent template sink - whatever secret pipeline the operator already runs). groundtruth:

- Reads it fresh from disk on every single check. Nothing from it is cached in memory between checks, logged, or written to groundtruth's own database.
- Merges it into exactly one subprocess's environment via `tfexec`/`tofuexec`'s `SetEnv`, which **replaces** the subprocess environment rather than merging with groundtruth's own - confirmed against the library source, not assumed.
- Never echoes its contents. See "A specific leak this caught," below - this rule is enforced deliberately, not by accident.
- Removes those values from any error before it is stored or logged. The `terraform`/`tofu` subprocess is the largest consumer of the credentials, so its stderr is treated as untrusted text: a provider or backend can echo a key it was handed (in an HTTP error, in a rejected URL), and a check's `error_message` is persisted, returned by the API and shown in the dashboard. Every value of four or more characters in the file is replaced with `[redacted]` wherever it appears - deliberately including non-secret values such as a region, because telling the two apart by name would be a guess, and a wrong guess leaks a secret.

**Why not encrypt credentials and store them in the database instead** (a "home lab" mode without an external secrets pipeline)? Because "we never hold your cloud credentials" is a stronger, simpler-to-verify trust story than "we hold them, encrypted, trust our crypto." A subtle bug in hand-rolled key derivation or nonce handling is a realistic risk for a tool built by a small team, and a severe credibility hit if ever found. This mirrors Atlantis' own precedent - the closest comparable "wraps terraform as a service" tool doesn't store cloud credentials either.

If demand for a no-external-secrets-manager mode materializes, here is the scheme that would be used, specified now so it's reviewed as a decision rather than improvised under pressure:

- A required `GROUNDTRUTH_MASTER_KEY` (32 bytes, base64), never written to disk by groundtruth itself.
- Per-record keys derived via HKDF-SHA256 from the master key, a purpose string, and a random per-record salt - never using the raw master key directly.
- AES-256-GCM, a fresh `crypto/rand` 12-byte nonce per encryption call, stored as `nonce || ciphertext || tag`.
- Decryption only in-memory, immediately before injecting into the subprocess environment.

This is unbuilt. It is not a half-finished feature hidden somewhere; it's a specification waiting for a real reason to implement it.

### What groundtruth does store

"Never stored" is about *cloud credentials*. A few other secrets do have to live in the database, and it is better to name them than to let the claim above imply there are none:

- **Alert-destination secrets.** A webhook's shared secret is stored as entered, because signing each request (HMAC) needs the raw key; it is write-only, and the API reports only whether one is set. A destination's URL is returned and shown as entered, and for Slack the URL *is* the credential - anyone holding it can post to the channel.
- **Password hashes** (bcrypt, cost 12) and **session and API tokens** (a SHA-256 hash of each, never the token). A read of the database can't be replayed as a login.
- **Drift results**, already redacted (see below), and failed checks' error messages, scrubbed of credential values.

The data directory and the database file are created readable by groundtruth's user only (0700 and 0600), and tightened on startup if an earlier version created them more openly. That is the protection for the plaintext items above. If the database file is ever read by someone who shouldn't have, assume the webhook secrets and Slack URLs in it are exposed and rotate them.

## Redaction: before anything is persisted

Terraform's plan JSON marks sensitive and not-yet-known values with a parallel tree of `true`/`false` markers (`BeforeSensitive`, `AfterSensitive`, `AfterUnknown` on each `ResourceChange`), matching the shape of the real value - sometimes at a leaf, sometimes marking a whole subtree at once. `internal/drift` walks that tree and replaces every marked value with a fixed placeholder (`"(sensitive value)"` or `"(known after apply)"`) before the result is turned into anything that gets stored or returned by the API.

This conversion happens exactly once, in one place, and the *only* representation of a plan that leaves that function is the redacted one. Fixture tests cover a plain update, a wholly-sensitive attribute, a sensitive value nested inside an otherwise-visible map, and an unknown-until-apply attribute - the shapes that are easy to get subtly wrong.

A second thing is worth stating plainly: groundtruth reads `plan.ResourceDrift`, not `plan.ResourceChanges`. Terraform documents `resource_drift` as "the changes detected when it compared the most recent state to the prior saved state" - exactly what this tool means by drift. `resource_changes` describes what Terraform would do to match *configuration*, a different question groundtruth never asks, since it never runs anything but a refresh-only plan.

## A specific leak this caught

During a hardening pass, we found that `godotenv`'s own parse errors can embed the offending source line verbatim - an "unterminated quoted value" error includes the raw text, quote and all. For most files that's a reasonable debugging aid. For a *credentials* file, that line is exactly the one piece of text that must never reach a log, an HTTP response, or a stored `error_message`.

The fix: a credential file that can't be *opened* (not found, permission denied) surfaces the real OS error, which is just a path and an error code - safe. A file that opens but fails to *parse* surfaces a fixed, generic message instead of whatever the parser produced. This is tested by planting a secret-looking value in a malformed file and asserting it never appears in the resulting error.

The broader rule this represents: **any error that passed through code touching credential content gets treated as untrusted text**, not assumed safe just because it came from a well-regarded library. It's worth rechecking this assumption again if the credential-reading code ever changes.

## Detection model and its limits

groundtruth doesn't implement drift detection; it asks Terraform/OpenTofu to. `plan -refresh-only` compares the **state** Terraform last saved with what the providers read back from the real infrastructure, and reports the differences (`resource_drift` in the plan JSON). That keeps groundtruth accurate to how Terraform itself sees your infrastructure - same providers, same authentication, same understanding of each resource - and it is also what bounds it. Several consequences follow, none of which groundtruth can paper over:

- **Only resources in the state.** A resource created outside Terraform and never imported isn't drift to Terraform, so it never appears. Terraform walks the prior state and classifies each resource as updated, deleted, or merely moved; it never reports a *new* one. That is why the `added` count is zero for every refresh-only plan (the field stays in the API for completeness). Finding unmanaged resources means enumerating cloud APIs directly, which is what `driftctl` did and what groundtruth deliberately doesn't.
- **State against reality, not code against reality.** Editing a `.tf` file without applying it isn't drift here (`terraform plan` is the tool for that). And fixing drift in code doesn't clear it: groundtruth keeps reporting until the *state* matches reality again, which takes an `apply` - the pipeline's normal one, or `apply -refresh-only` to accept the change into state.
- **The provider decides what is reported, and how.** A provider can only report attributes it reads back, and it chooses how to represent a change. The `local` provider, for example, reports an *edited* `local_file` as deleted (its refresh drops the resource when the file's checksum no longer matches the state), so the dashboard says `Deleted` for what a person would call a modification. groundtruth shows what Terraform and the provider say; read the real change from the before/after values.
- **Provider versions are pinned only by a lock file.** Every check runs in a fresh directory, so without a committed `.terraform.lock.hcl`, `init` selects the newest provider versions the module's constraints allow, and a provider release can change what a check reports. groundtruth never passes `-upgrade`; commit the lock file and versions move only when you move them.
- **A check can overlap one of your applies.** It takes no state lock (see [Execution](#execution-isolation-not-sandboxing)), so it can neither block your pipeline nor be blocked by it - but a check that runs while an `apply` is mid-flight may report drift that is gone by the next check.
- **A failed refresh is a failed check, never a clean one.** If a provider can't read the infrastructure (expired credentials, an API outage), `plan` fails and the check is recorded, and alerted, as `failed`.

### Deciding what to do about drift

groundtruth reports drift; it never resolves it, and it has no "acknowledge" or "mute". That decision is the operator's. If the change was intentional, update the code and let the pipeline apply it (or accept it with `apply -refresh-only`). If it wasn't, re-apply the existing code to put the infrastructure back. Either way, the next check sees state and reality agree and sends a "resolved" alert.

Because alerts fire on transitions only (see [Alerting](#alerting-synchronous-and-silent-on-repeats)), drift that nobody acts on produces one alert and then silence, while the workspace stays `drifted` in the dashboard. A periodic re-notification is the obvious follow-up, and isn't built.

## What groundtruth deliberately doesn't manage

- **The module source.** A workspace points at a filesystem path the operator keeps current via their own existing pipeline (a volume mount, a git-sync sidecar, whatever GitOps they already run). groundtruth never clones a repository and never holds VCS credentials.
- **The backend.** The module's own `backend` block (S3, GCS, Azure Blob, Terraform Cloud, or - as in groundtruth's own test fixtures - a local backend pointed at a path outside the module) is what `init` connects to. groundtruth doesn't configure, migrate, or lock state.
- **Remediation.** groundtruth never runs `apply`. It is read-only by design: it can tell you what changed, never change anything itself. This keeps the trust bar for adopting it low and the blast radius of a bug in it bounded to "wrong information displayed," never "infrastructure modified."

## Concurrency: a scheduler, not a queue

A single poll loop (`internal/scheduler`) wakes every 30 seconds, asks the database which enabled workspaces are due (never checked, or their last check started longer ago than their own `check_interval_minutes`), and runs each due workspace through the same `internal/checks.Service` the manual "check now" endpoint uses - one shared code path, so a check's outcome is recorded identically regardless of what triggered it.

A semaphore bounds how many *scheduled* checks run at once (`GROUNDTRUTH_MAX_CONCURRENT_CHECKS`, default 3). Beyond that, `checks.Service` itself allows only one check per workspace at a time, whoever starts it: a "check now" request or an API call that arrives while that workspace is running gets `409 check_in_progress`, and the scheduler quietly skips it. Two overlapping checks would repeat the same expensive work and, worse, each judge "did the status change?" against the same previous status and alert twice for one change - so the previous status is also read from the database inside that guard, rather than taken from the caller's possibly stale copy. Manual and API-triggered checks therefore aren't counted against the semaphore, but they are bounded by the number of workspaces.

On shutdown (SIGTERM), the signal context is cancelled - the scheduler runs under it and every HTTP request's context derives from it - so in-flight `terraform`/`tofu` subprocesses are interrupted via that same cancellation (see [Execution](#execution-isolation-not-sandboxing) for how long they get to stop), and their `defer`-based temp-directory cleanup still runs before the process exits, because Go's `defer` runs regardless of *why* a function returned.

This is a single-process design. Running multiple groundtruth instances against the same database is a real architectural fork point (coordinating which instance owns which workspace), not something this scheduler attempts - a stated non-goal for v1, not a silent gap.

## Alerting: synchronous, and silent on repeats

`internal/alerting` is called inline, after a check is persisted, with the workspace's previous and new status. It fires exactly on a *transition* - clean-or-never-checked into drifted, drifted back to clean, anything into failed - and is deliberately silent when the status doesn't change, including a workspace that's been drifted for days: it alerted once, and re-alerting on every single check would train operators to ignore it. A time-based "still unresolved" re-notification is a plausible follow-up; it isn't built, because guessing at the right interval without real usage data would be worse than not having it.

Delivery is synchronous rather than fire-and-forget, deliberately: checks already run minutes apart, and a synchronous call is far simpler to reason about and test than managing background goroutine lifecycles correctly through a shutdown. It isn't instant, though. A destination that answers takes milliseconds, but one that is down costs three attempts of up to 10 seconds each plus the 2s and 4s backoffs between them - about 36 seconds, during which the check that triggered it (and, for a scheduled check, its concurrency slot) waits. Destinations are attempted in parallel, so that worst case doesn't grow with how many are configured, and the backoff gives up at once if the process is shutting down.

A redirect is never followed: Go would re-send a redirected POST as a body-less GET, and the final `200` would be logged as a delivery the receiver never got. A `3xx` is a failed attempt. Each delivery ends in one `alert_log` row and, on failure, a warning in groundtruth's own log; there is no UI for the log yet, so a destination that never works is visible only there. A webhook's shared secret, if set, HMAC-signs the outgoing body (`X-Groundtruth-Signature: sha256=...`), mirroring the convention GitHub and Stripe use. Destination URLs must be absolute `http(s)` URLs.

## Authentication

Dashboard sessions are opaque random tokens (32 bytes from `crypto/rand`); only a SHA-256 hash is ever stored, so a database read alone can't be replayed as a live session. There is no session-signing secret anywhere in the system - one fewer secret for groundtruth itself to protect. Passwords are bcrypt-hashed at cost 12 (above bcrypt's own default of 10: this dashboard reaches real cloud credentials). CSRF protection uses Go's standard-library `http.CrossOriginProtection` rather than a third-party library, checking `Sec-Fetch-Site`/`Origin` against `Host` - this is origin-based, not session-based, so it covers every mutating route uniformly, including setup and login before any session cookie exists.

Sessions expire after 7 days idle or 30 days absolute, and an expired one is deleted when it is next presented rather than swept in the background. The cookie is `HttpOnly` and `SameSite=Lax`, and `Secure` whenever `GROUNDTRUTH_BASE_URL` is `https://` - which is why that variable should be set when running behind TLS. Passwords must be 12 to 72 bytes: 72 is bcrypt's own limit, and a longer one is rejected up front instead of being silently truncated. Login answers an unknown email and a wrong password identically, and with comparable *timing*: for an unknown email it spends a bcrypt comparison against a throwaway hash, so response time doesn't reveal which emails have accounts.

Login and first-run setup are rate-limited to 10 attempts a minute per client, with a burst of 5. A client is identified by the connection's source address, never `X-Forwarded-For` (any client could set that to dodge the limit); for IPv6 that means the /64, since one subscriber controls a whole /64 and per-address limiting would be trivially bypassed. The limiter forgets a client after 15 idle minutes, so memory doesn't grow with every address ever seen. The consequence to know about: behind a reverse proxy every client arrives from the proxy's address, so the limit becomes one budget shared by everyone - enough to lock the operator out of login for a minute by hammering it, not enough to guess a password. A configurable list of trusted proxies is the fix, and isn't built.

Every response carries a `Content-Security-Policy` (scripts and connections only from the same origin, no framing, no plugins), `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY` and `Referrer-Policy: no-referrer`, and everything under `/api` is `Cache-Control: no-store`. `style-src` allows inline styles because the dialog library injects `<style>` elements at runtime; scripts are never inline. An unknown `/api/...` path is a JSON `404`, never the dashboard's HTML, which would answer `200` to a mistyped URL in a CI script. The server bounds how long it waits for request headers and bodies and for idle connections; there is deliberately no write timeout, because a "check now" request can run as long as its workspace's check timeout.

API tokens (for CI-triggered checks) follow the identical pattern - opaque value, hashed at rest, scoped to the user who created them for revocation. A check started with one is recorded as `triggered_by: api`, to tell it apart from a click on "Check now" (`manual`) and from the scheduler (`schedule`).

## The release image and its build

The Docker image is built from base images pinned by digest as well as tag, so a moved or tampered tag can't change what ships; Dependabot proposes bumps of both together.

Terraform and OpenTofu are downloaded at *build* time, never at runtime, and verified before they are used. A checksum fetched from the same place as the archive would only prove the download wasn't corrupted, so what is verified is the signature on the checksum file: HashiCorp's PGP signature for Terraform (it only counts if made by the key with the fingerprint recorded in the `Dockerfile`, the one HashiCorp publishes), and OpenTofu's keyless cosign signature from its release workflow. A tampered checksum file, or a signature by any other key, fails the build; both were checked.

The frontend is built with `npm ci --ignore-scripts`, so no dependency's install script runs in the build (the only dependency that declares one, `fsevents`, is macOS-only). CI builds the image on every push and pull request and starts it, so a change that would break a release fails there rather than at tag time. The release's archives and checksums are signed with cosign (keyless, GitHub OIDC); the release notes show how to verify them.

To bump Terraform or OpenTofu, change `TERRAFORM_VERSION` or `TOFU_VERSION` in the `Dockerfile`: the new files are verified by the same steps.

## What a reviewer should check first

If you're auditing this project, the highest-value places to look are:

1. `internal/drift/redact.go` - does the redaction walker actually cover every shape Terraform can mark sensitive?
2. `internal/terraform/executor.go` and `credentials.go` - does anything on the path from "read the credential file" to "subprocess exits" risk writing or echoing a credential value?
3. `internal/checks/service.go` - is the redacted result really the only form of a check that reaches the database?
4. `internal/httpapi/router.go` and `security.go` - is every route behind the right middleware, and does an unknown path fail closed?
5. `internal/auth` and `internal/alerting/dispatcher.go` - what proves a caller is the operator, and what leaves the process.
6. `Dockerfile` - is every binary that ends up in the image verified against its publisher, not just against a checksum from the same download?

Everything else follows from those files being correct.
