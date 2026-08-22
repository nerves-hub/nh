# nh

[![CI](https://github.com/nerves-hub/nh/actions/workflows/ci.yml/badge.svg)](https://github.com/nerves-hub/nh/actions/workflows/ci.yml)

`nh` is the command-line interface for [NervesCloud](https://nervescloud.com) and [NervesHub](https://nerves-hub.org/). It manages organizations, products, devices, firmware, deployments, firmware signing keys, and generates the X.509 certificates devices use to authenticate — all from your terminal.

## Installation

### Homebrew (macOS and Linux)

```sh
brew install nerves-hub/tap/nh-cli
```

If you have the old Elixir CLI installed, it provides the same `nh` command;
remove it first to avoid a conflict:

```sh
brew uninstall nerves-hub/tap/nh
```

### Prebuilt binaries

Download the archive for your OS and architecture from the
[latest release](https://github.com/nerves-hub/nh/releases/latest), extract it,
and put the `nh` binary on your `PATH`.

### From source

Requires [Go](https://go.dev) 1.26.4 or newer (the version is pinned in
`mise.toml`; if you use [mise](https://mise.jdx.dev), run `mise install`).

```sh
git clone https://github.com/nerves-hub/nh.git
cd nh
go build -o nh .
# optionally move it onto your PATH
mv nh /usr/local/bin/
```

Or install straight from the module path:

```sh
go install github.com/nerves-hub/nh@latest
```

Check your install with `nh --version`.

### Runtime dependency

Signing and uploading firmware shells out to [`fwup`](https://github.com/fwup-home/fwup),
which must be on your `PATH`. It is not needed for any other command, or when
uploading firmware with `--skip-signing`.

## Getting started

If you are using NervesCloud, all you need to do is authenticate and then point `nh` at the organization and product you work with most:

```sh
# Sign in by confirming in your browser (recommended)
nh user login

# ...or sign in with email and password
nh user auth

# Confirm who you are
nh user whoami

# Save defaults so you can omit --org / --product on every command
nh config set org my-org
nh config set product my-product

# List your devices
nh device list
```

If you are using a self-hosted NervesHub installation, you will need to tell `nh` where to find your installation before authenticating:

```
nh config set uri https://my.nerves-hub.installation
```

Your token and defaults are saved to `~/.nh/settings.json`. See
[Configuration](#configuration) below. Migrating from the old Elixir CLI? See
[Migrating from the Elixir CLI](#migrating-from-the-elixir-cli).

## Usage

Every command accepts `--help`. Output defaults to a human-readable table; pass
`-o json` (`--output json`) on any command for machine-readable JSON.

### Global flags

These persistent flags apply to every command (each also has an environment
variable — see [Configuration](#configuration)):

| Flag | Description |
|------|-------------|
| `--uri` | NervesHub/NervesCloud API base URI |
| `--token` | Personal access token |
| `--org` | Organization scope |
| `--product` | Product scope |
| `--data-dir` | Directory for local state |
| `--non-interactive` | Never prompt; treat missing input as an error |
| `-o, --output` | Output format: `table` (default) or `json` |

### Command reference

**`nh user`** — authenticate and inspect the current user
- `login` — authenticate by confirming in your browser
- `auth` — sign in with email and password and save an API token
- `logout` — remove the saved API token
- `whoami` — show the currently authenticated user

**`nh org`** — manage organizations
- `list` — list organizations you belong to
- `show [name]` — show details for an organization
- `members [name]` — list members of an organization
- `member <email>` — show an organization member
- `invite <email> <role>` — invite a user to an organization
- `set-role <email> <role>` — change a member's role
- `remove-member <email>` — remove a member from an organization

**`nh product`** — manage products
- `list`, `show <name>`, `create <name>`, `delete <name>`

**`nh device`** — manage devices
- `list` — list devices in a product
- `show <identifier>` — show details for a device
- `create <identifier>`, `update <identifier>`, `delete <identifier>`
- `upgrade <identifier> <firmware-uuid>` — upgrade a device to a firmware
- `move <identifier>` — move a device to another product
- `reboot <identifier>`, `reconnect <identifier>`
- `clear-penalty <identifier>` — clear a device's penalty box
- `console <identifier>`, `shell <identifier>` — connect to a device's console/shell
- `iroh-console <identifier>` — open a remote IEx console over a peer-to-peer iroh
  connection (`--auth`, `--instance`; Ctrl-] to detach)
- `logs <identifier>` — show the log lines a device has sent
  (`--level`, `--search`, `--since`, `--before`, `--limit`, `--order`, `--meta`);
  `-f`/`--follow` tails new lines by polling (`--interval`)
- `run-code <identifier> <code>` — run Elixir code on a device
- `scripts <identifier>`, `run-script <identifier> <name-or-id>` — support scripts
- `network-identities <identifier>` — list the keys a device reports holding on
  other networks (iroh/NetBird/Tailscale/WireGuard); `--service`/`--instance` filters
- `certificates` — manage a device's certificates
  - `list <identifier>`, `show <identifier> <serial>`, `delete <identifier> <serial>`
  - `generate <identifier>` — generate a device key and CSR or signed certificate locally
  - `upload <identifier> <certificate path>` — upload a certificate for a device

**`nh firmware`** — manage firmware
- `list`, `show <uuid>`, `download <uuid>`, `delete <uuid>`
- `upload [path]` (alias `publish`) — sign (via `fwup`) and upload a firmware file
  (`--skip-signing` to skip). Inside a Nerves project the path is optional and the
  built image is detected. `--deploy <group>` (repeatable) points deployment groups
  at the new firmware once the upload succeeds

**`nh deployment`** — manage deployment groups
- `list`, `show <name>`, `create <name>`, `update <name>`, `delete <name>`

**`nh key`** — manage firmware signing keys
- `list`, `show <name>`, `create <name>`, `delete <name>`

**`nh ca`** — manage organization CA certificates
- `generate` — generate a CA certificate locally
- `upload [name]` — register a CA certificate with the organization
- `list`, `show <serial>`, `delete <serial>`

**`nh iroh-endpoint`** (alias `iroh`) — manage an organization's iroh endpoint ids
- `list` — list registered endpoints; `--owner device|user|none`, `--search <text>`
- `register <identifier>` — register an endpoint id; `--instance`, `--user-email`, `--detail key=value`
- `show <identifier>`, `delete <identifier>`

**`nh script`** — manage support scripts
- `list`, `show <id>`, `create`, `update <id>`, `delete <id>`

**`nh config`** — view and change persisted CLI defaults (see below)

**`nh migrate`** — import settings and signing keys from the old Elixir CLI (see
[Migrating from the Elixir CLI](#migrating-from-the-elixir-cli))

## Configuration

`nh` resolves each setting from the following sources, highest precedence first:

1. **Command-line flag** (e.g. `--org`)
2. **Environment variable** (`NERVES_HUB_*`, then the also-supported `NERVES_CLOUD_*`)
3. **Saved settings file** (`~/.nh/settings.json`, managed by `nh config`)
4. **Built-in default**

### Persisted defaults

Store the org and product you use most so you can omit them on every command:

```sh
nh config set org my-org
nh config set product my-product
nh config get            # show all persisted defaults
nh config unset product  # clear one
```

The API base URI is stored the same way, under the `uri` key. It defaults to
`https://manage.nervescloud.com`, so you only need to set it to point `nh` at a
self-hosted NervesHub (or a staging environment):

```sh
nh config set uri https://nerves-hub.your-company.com
```

Like org and product, a saved `uri` is the lowest-precedence source: the `--uri`
flag and the `NERVES_HUB_URI` environment variable still override it for an
individual command.

You can also keep multiple named configuration profiles and switch between them:

```sh
nh config save staging   # snapshot the active config as "staging"
nh config profiles       # list saved profiles
nh config load staging   # switch the active config to a profile
```

### Environment variables

Every variable is read with the `NERVES_HUB_` prefix and, equivalently, the
`NERVES_CLOUD_` prefix. When both are set, `NERVES_HUB_` wins.

| Variable | Purpose | Default |
|----------|---------|---------|
| `NERVES_HUB_URI` | NervesCloud API base URL | `https://manage.nervescloud.com` |
| `NERVES_HUB_TOKEN` | Personal access token (falls back to the saved settings file) | — |
| `NERVES_HUB_ORG` | Default organization scope | saved settings |
| `NERVES_HUB_PRODUCT` | Default product scope | saved settings |
| `NERVES_HUB_DATA_DIR` | Directory for local state (settings, keys) | `~/.nh` |
| `NERVES_HUB_NON_INTERACTIVE` | Disable all prompts; missing input is an error (useful in CI) | `false` |
| `NERVES_HUB_PRIVATE_KEY` | Unencrypted base64 signing key for `firmware upload` (alternative to `--key`) | — |
| `NERVES_HUB_DEPLOYMENT_NAME` | Deployment group `firmware upload` updates when `--deploy` is not given | — |

Boolean variables accept `1`, `true`, `yes`, `y`, or `on` (case-insensitive).

## Migrating from the Elixir CLI

If you previously used the old Elixir `nerves_hub_cli`, `nh migrate` imports your
existing setup:

```sh
nh migrate --dry-run   # preview what would be imported
nh migrate             # import it
```

It reads the old CLI's data directory (default `~/.nerves-hub`, or
`$NERVES_HUB_HOME`) and copies into `~/.nh`:

- your saved defaults (API base URI, org, product) and API token, and
- your signing keys, from `~/.nerves-hub/keys/<org>/` into `~/.nh/keys/<org>/`.

Notes:

- The old directory is only **read**, never modified.
- Signing keys are **re-encoded, not decrypted** — no key password is needed, and
  password-protected keys keep their password.
- It is **non-destructive and repeatable**: existing `nh` settings and keys are
  left alone unless you pass `--force`.
- Point it elsewhere with `--from <dir>`.

After migrating, run `nh user whoami` to confirm your token still works; if it
has expired, sign in again with `nh user login`.

## Project layout

The codebase deliberately keeps `cmd/` thin — flag parsing only, with all
behavior in `internal/`:

- `cmd/` — Cobra command definitions and flag parsing; no business logic
- `internal/api/` — HTTP client and API request/response types
- `internal/config/` — URI / token / data-dir handling and environment binding
- `internal/pki/` — X.509 certificate and Ed25519 signing-key handling
- `internal/phoenix/` — Phoenix Channels client (device console / shell)
- `internal/mix/` — Mix / Nerves project integration helpers

## Contributing

Contributions are welcome — Go was chosen in part to keep the CLI approachable
for the Elixir-first Nerves community.

1. Fork and clone the repository.
2. Install the pinned toolchain: `mise install` (or install Go 1.26.4 yourself).
3. Make your change, keeping `cmd/` thin and business logic in `internal/`.
4. Run the test suite and formatters:

   ```sh
   go test ./...
   go vet ./...
   gofmt -l .
   ```

5. Open a pull request describing the change.

Guidelines:

- Match the surrounding style and the existing command structure (nouns as
  parent commands, verbs as subcommands).
- Treat the security-sensitive crypto/cert/signing paths carefully; lean on Go's
  standard `crypto/*` packages rather than hand-rolling.
- Add or update tests alongside code — most commands have a matching `_test.go`.

## Releasing

Releases are automated with [GoReleaser](https://goreleaser.com) via the
`.github/workflows/release.yml` workflow. To cut a release, push a semver tag:

```sh
git tag v0.1.0
git push origin v0.1.0
```

The workflow cross-compiles binaries (macOS/Linux/Windows × amd64/arm64), creates
the GitHub release with archives and checksums, and updates the Homebrew cask.

Preview a release locally without publishing:

```sh
goreleaser release --snapshot --clean --skip=publish
```

## License

Released under the [MIT License](LICENSE). Copyright © 2026 NervesHub.
