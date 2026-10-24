# asbx

A small utility to isolate your (at the moment) claude instances from
your local machine.

## Requirements

- [Orbstack](https://orbstack.dev)
  - You can technically use Docker for Mac, but your container will not
    have access to services running your mac (which may or may not be
    acceptable!)
- 1Password: Used to source tokens passed through to the agent.

## Features

- Agent has access to your current `pwd`, and only that directory.
- Git commit signing works via ssh-agent (e.g., 1Password).
- Forgejo authentication is passed through via `fj auth token` if
  enabled.
- `op` (1Password CLI) works in the container through a service
  account, if configured.

## Installation

Install the latest release via [mise](https://mise.jdx.dev):

```bash
mise use -g 'forgejo:jaredallard/asbx[api_url=https://git.rgst.io/api/v1,asset_pattern=asbx]'
```

Or add it to your mise config:

```toml
[tools]
"forgejo:jaredallard/asbx" = { version = "latest", api_url = "https://git.rgst.io/api/v1", asset_pattern = "asbx" }
```

## Configuration

asbx reads `~/.config/asbx/config` for configuration values. Note that
it is NOT sourced, so shell expansion is not supported. Options are
only read from config files, never from environment variables.

To get started, copy [config.example](config.example):

```bash
mkdir -p ~/.config/asbx
cp config.example ~/.config/asbx/config
```

Repositories can also have their own `.asbx` file at its git root (or
the working directory outside of a git repo). It takes the same options,
is read after `~/.config/asbx/config` so it overrides it, and is
mounted read-only into the container. See [Security](#security) before
running asbx in a repo you don't trust.

- `FORGEJO_ENABLED`: `true` passes `FORGEJO_TOKEN` (from `fj auth token`)
  and your host's fj config (without its token) into the container.
- `CLAUDE_CODE_OAUTH_TOKEN_SECRET_REF`: 1Password `op://` ref to read
  `CLAUDE_CODE_OAUTH_TOKEN` from.
- `LITELLM_TOKEN_SECRET_REF`: 1Password `op://` ref to read
  `LITELLM_API_KEY` from.
- `OP_SERVICE_ACCOUNT_TOKEN_SECRET_REF`: 1Password `op://` ref to a
  service account token, so `op` works in the container. See
  [1Password CLI](#1password-cli).
- `EXTRA_SECRET_ENV_VARS`: comma-separated `NAME:op://ref` entries; each
  1Password secret is passed into the container as the environment
  variable `NAME`.
- `CLAUDE_MIRROR_MANAGED_SETTINGS`: `true` mounts your OS specific
  managed settings into the container.
- `CLAUDE_MIRROR_PROJECTS`: `true` mounts this directory's Claude
  sessions and memory read-write, so the host and container can resume
  each other's sessions.
- `NETWORK_HOST`: `true` runs the container on the host's network, so it
  can reach every service listening on localhost. Otherwise host services
  are only reachable via `host.docker.internal`.
- `EXTRA_MOUNTS`: comma-separated absolute paths to mount read-only at
  the same path inside the container.

### 1Password CLI

The 1Password app's CLI integration can't be passed into the container.
On macOS, `op` talks to the app over XPC and both sides verify each
other's code signature, which can't cross into the container's VM. On
Linux, the app trusts any process in the `onepassword-cli` group, which
an agent with `sudo` could fake to reach your whole account.

Instead, `op` in the container uses a [service
account](https://www.1password.dev/service-accounts/get-started/):

1. Create a service account with access to only the vaults the agent
   needs, read-only if it doesn't need to write.
2. Store its token in 1Password.
3. Set `OP_SERVICE_ACCOUNT_TOKEN_SECRET_REF` to that token's `op://` ref.

## Security

asbx limits agent access, but it's not perfect. There's still a few
limitations:

- Refuses to run in `/`, your home directory or its parents, or a
  dot-directory in your home (e.g., `~/.config`).
- Mounts some files read-only to the host (namely: git hooks and git
  config) to prevent automatic execution of commands on the host.
- Doesn't mount other projects' sessions, memory or prompt history from
  `~/.claude`.
- If project memory is symlinked, it will be pinned when first read to
  prevent the agent from mutating it and asbx feed it through on next
  run.

What it doesn't protect against:

- **sudo on Linux**: the agent can become root in the container, and root
  can create setuid-root files in the working directory. Running one on
  the host gives host root. Use rootless Docker or `userns-remap` to
  close this. macOS docker prevents this.
- **ssh-agent**: the agent can use every key in your agent against any
  host, not just for commit signing. Limit which keys are offered in
  1Password's `agent.toml`, or if using the Desktop integration, simply
  deny the request.
- **Tokens**: every token passed in, including `EXTRA_SECRET_ENV_VARS`,
  is readable by the agent. Mitigate this with scoped credentials. The
  agent can read everything the 1Password service account can, so keep
  its vaults minimal.
- **Project config**: a repo's `.asbx` is parsed rather than run, but
  it can still enable host networking, extra mounts or secrets, so check
  it before running asbx in a repo you don't trust. An existing
  `.asbx` is mounted read-only, but the agent can create one (or a nested
  `.git` with its own `.asbx`) when there isn't one, and it applies on
  the next run.
- **`CLAUDE_MIRROR_PROJECTS`**: when enabled, sessions and memory
  written in the container are read by Claude on the host, so they can
  carry prompt injection.

## License

GPL-3.0
