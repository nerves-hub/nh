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

Signing and uploading an fwup (`.fw`) image shells out to
[`fwup`](https://github.com/fwup-home/fwup), which must be on your `PATH`. It is
not needed for other firmware formats, for any other command, or when uploading
with `--skip-signing`.

Signing an ESP-IDF image shells out to `espsecure.py` (or `espsecure`) from
[esptool](https://github.com/espressif/esptool) (`pip install esptool`). AtomVM
packbeams are signed by `nh` itself, and RAUC bundles are signed when they are
built, so neither needs anything extra.

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

Check that `nh` can reach it with `nh status`, which needs no sign-in. It prints
the configured URI and whether the server answered its health check, and exits
non-zero when the server is unreachable or reports a problem.

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
- `list`, `show <name>` — list products or show one, with their settings
  (accepted firmware formats, unique-version and unsigned-firmware rules, and
  the default device-managed updates grant)
- `create <name>`, `delete <name>`
- `update <name>` — change a product's settings; only the flags you pass are
  sent, and a product cannot be renamed:
  - `--allow-tool <tool>` / `--disallow-tool <tool>` add or remove a firmware
    format (`fwup`, `esp-idf`, `atomvm`, `rauc`); repeatable or comma-separated
  - `--tools <list>` sets the whole list instead, so `--tools fwup` removes
    every other format. fwup is always kept and cannot be disallowed
  - `--allow-unsigned-esp-idf`, `--allow-unsigned-atomvm`,
    `--require-unique-firmware-version`, `--default-managed-updates-allowed`
    take `=true` or `=false` (on its own, the flag means true)

  A format other than fwup must also be enabled on the NervesHub instance
  (`ESP_IDF_FIRMWARE_ENABLED`, `ATOMVM_FIRMWARE_ENABLED` or
  `RAUC_FIRMWARE_ENABLED`) before a product can accept it.

  ```sh
  nh product update thermostat --allow-tool esp-idf --allow-unsigned-esp-idf
  nh product update thermostat --disallow-tool atomvm
  ```

**`nh device`** — manage devices
- `list` — list devices in a product (`--filter key:value`, `--sort`, `--page`);
  `-q`/`--query` takes the web UI search bar's advanced query language,
  including custom health metrics (needs NervesHub v2.5.0 or later; see
  `nh device list --help` for each column's operators):

  ```sh
  # Devices whose latest cpu_temp reading is over 70
  nh device list --query 'metric:cpu_temp > 70'

  # Connected production devices
  nh device list --query 'connection = "connected" and tags contains "prod"'

  # Devices that need attention
  nh device list --query 'health_status != "healthy" or alarm_status = "with"'

  # Low battery on devices that would accept an update
  nh device list --query 'metric:battery_soc < 20 and updates = "enabled"'
  ```
- `show <identifier>` — show details for a device
- `create <identifier>`, `update <identifier>` — set `--description`, `--tag`
  (repeatable), `--update-mode off|automatic|device_managed`,
  `--managed-updates-allowed` and `--connecting-code` (needs NervesHub v2.5.3 or
  later). The API can't set a device's deployment group: NervesHub assigns one
  whose version and tag conditions the device matches when it connects
- `delete <identifier>`
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
- `metrics <identifier>` — show the metric readings a device has reported, grouped
  by metric name (`--key`, `--since`, `--before`, `--limit`, `--order`);
  `--list-keys` lists the metric names in the fetched reports, and
  `-f`/`--follow` tails new readings by polling (`--interval`)
- `run-code <identifier> <code>` — run Elixir code on a device
- `scripts <identifier>`, `run-script <identifier> <name-or-id>` — support scripts
- `network-identities <identifier>` — list the keys a device reports holding on
  other networks (iroh/NetBird/Tailscale/WireGuard); `--service`/`--instance` filters
- `certificates` — manage a device's certificates
  - `list <identifier>`, `show <identifier> <serial>`, `delete <identifier> <serial>`
  - `generate <identifier>` — generate a device key and CSR or signed certificate locally
  - `upload <identifier> <certificate path>` — upload a certificate for a device
- `shared-secrets` — manage a device's own shared secrets (`nhd_` keys)
  - `list <identifier>` — list the device's keys, deactivated ones included
    (secrets are never listed)
  - `create <identifier>` — create a key and secret; the secret is shown only
    once and cannot be fetched again
  - `delete <identifier> <key>` (alias `deactivate`) — deactivate a key so the
    device can no longer connect with it (`--yes` skips the prompt)

**`nh firmware`** — manage firmware
- `list`, `show <uuid>`, `download <uuid>`, `delete <uuid>`
- `upload [path]` (alias `publish`) — sign and upload a firmware file
  (`--skip-signing` to skip). Inside a Nerves project the path is optional and the
  built image is detected. `--deploy <group>` (repeatable) points deployment groups
  at the new firmware once the upload succeeds. The format is detected from the
  file's contents, the image's own metadata is previewed, and an image built for
  another product is refused:

  | Format | File | Preview read from | Signed with |
  |---|---|---|---|
  | `fwup` | `.fw` | `fwup -m` | `fwup --sign`, Ed25519 key from `--key <name>`, `--key-file` or `NERVES_HUB_PRIVATE_KEY` |
  | `atomvm` | `.avm` | the packbeam's `application.bin` | `nh` itself, with the same Ed25519 keys as fwup (an existing signature is replaced) |
  | `esp-idf` | `.bin` | the image header and app descriptor | `espsecure.py sign_data --version 2`, RSA-3072 key from `--key <name>` (`<data-dir>/keys/<org>/<name>.rsa.pem`) or `--key-file <pem>`; an already signed image is uploaded as is |
  | `rauc` | `.raucb` | the manifest's `[update]` and `[meta.nerveshub]` | nothing: bundles are signed when built and uploaded as is |

  A product takes only the formats in its `allowed_update_tools` (`fwup` by
  default; `nh product update <product> --allow-tool <format>`), and may accept
  unsigned ESP-IDF or AtomVM images (`--allow-unsigned-esp-idf` /
  `--allow-unsigned-atomvm`). `list` and `show` report each firmware's format
  (`tool`), and `show` its size

**`nh deployment`** — manage deployment groups
- `list`, `show <name>`, `create <name>`, `update <name>`, `delete <name>`

**`nh key`** — manage firmware signing keys (see [Signing key schemes](#signing-key-schemes))
- `list`, `show <name>` — include each key's `scheme`; for an RSA key the Secure
  Boot v2 eFuse digest, for a certificate its SHA-256 fingerprint
- `create <name>` — generate a keypair locally and register its public key;
  `--scheme ed25519` (default, `--password` optional) or `--scheme secure_boot_v2_rsa`
- `add <name> --public-key <file>` — register an existing public key or
  certificate without generating anything; `--scheme` is detected from the file
  when omitted, and the key is checked locally before it is sent
- `delete <name>` — delete the key from the server; local key files are kept and listed

**`nh ca`** — manage organization CA certificates
- `generate` — generate a CA certificate locally (`--upload` registers it, and
  takes the `--jitp-*` flags below)
- `upload [name]` — register a CA certificate with the organization.
  `--jitp-product <name>` turns on just-in-time provisioning: a device whose
  certificate the CA signed is created in that product on its first connection,
  with `--jitp-tag` (repeatable or comma-separated) and `--jitp-description`.
  Naming the product needs NervesHub v2.5.0 or later
- `list`, `show <serial>`, `delete <serial>` — `list` and `show` include each
  CA's JITP product, tags and description

**`nh status`** — check that the configured server is up (`GET /status/alive`,
no sign-in needed); `--timeout` sets how long to wait

**`nh iroh-endpoint`** (alias `iroh`) — manage an organization's iroh endpoint ids
- `list` — list registered endpoints; `--owner device|user|none`, `--search <text>`
- `register <identifier>` — register an endpoint id; `--instance`, `--user-email`, `--detail key=value`
- `show <identifier>`, `delete <identifier>`

**`nh script`** — manage support scripts
- `list`, `show <id>`, `create`, `update <id>`, `delete <id>`

**`nh config`** — view and change persisted CLI defaults (see below)

**`nh migrate`** — import settings and signing keys from the old Elixir CLI (see
[Migrating from the Elixir CLI](#migrating-from-the-elixir-cli))

### Signing key schemes

Every signing key has a `scheme`, and a key only verifies firmware of its own
scheme. Which one you need depends on the firmware format you upload:

| Firmware format | Scheme | Key string | Create or register |
| --- | --- | --- | --- |
| fwup `.fw` (Nerves) | `ed25519` | base64 Ed25519 public key (an fwup `.pub`) | `nh key create <name>`, or `nh key add <name> --public-key fwup-key.pub` |
| ESP-IDF `.bin` (Secure Boot v2) | `secure_boot_v2_rsa` | PEM RSA-3072 public key | `nh key create <name> --scheme secure_boot_v2_rsa`, or `nh key add <name> --public-key key.pem` |
| RAUC bundle | `x509_certificate` | PEM X.509 certificate (optionally followed by its issuers) | `nh key add <name> --scheme x509_certificate --public-key cert.pem` |

Keys `nh key create` generates live in `<data-dir>/keys/<org>/`:

- `ed25519`: `<name>.priv` (encrypted with an optional password) and `<name>.pub`.
- `secure_boot_v2_rsa`: `<name>.rsa.pem`, an **unencrypted** PKCS#1 PEM private
  key (mode 0600), and `<name>.rsa.pub.pem`. It is the same format
  `espsecure.py generate_signing_key --version 2 --scheme rsa3072` writes, so
  `espsecure.py sign_data --version 2 --keyfile <name>.rsa.pem` can use it
  directly. It is left unencrypted because espsecure needs a plain key file;
  protect it accordingly.

`nh key add` accepts an espsecure PEM private key too, and sends only its public
half. It never accepts a certificate file that also holds a private key.

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
nh status   # check that nh can reach it
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
| `NERVES_HUB_PRIVATE_KEY` | Unencrypted base64 Ed25519 signing key for `firmware upload` of fwup and AtomVM images (alternative to `--key`) | — |
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
- `internal/pki/` — X.509 certificate handling and signing keys (Ed25519,
  Secure Boot v2 RSA-3072, RAUC certificates)
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
