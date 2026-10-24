# asbx

A small utility to isolate your (at the moment) claude instances from
your local machine.

## Requirements

- [Orbstack](https://orbstack.dev)
  - You can technically use Docker for Mac, but your container will not
    have access to services running your mac (which may or may not be
    acceptable!)
- Optional: [1Password CLI](https://developer.1password.com/docs/cli/get-started/):
  Used to source tokens passed through to the agent, and by `op` in the
  container.

## Features

- Agent has access to your current `pwd`, and only that directory.
- Git commit signing works via ssh-agent (e.g., 1Password).
- Forgejo authentication is passed through via `fj auth token` if
  enabled.
- `op read` and `op run` work in the container through your host's
  `op`, limited to the secrets you allow if enabled.

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
- `OP_ALLOWED_SECRET_REFS`: comma-separated `op://` refs that `op` in
  the container may read through your host's `op`. See [1Password
  CLI](#1password-cli).
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

The 1Password App's desktop integration is unable to be forwarded like
the SSH agent due to more strict validation. To work around this, we run
a container only proxy with a shim inside the container that allows
specific key paths to be read using the host `op`. This requires `op` to
be installed on the host.

To allow specific secret refs, use the `op://` syntax like so, with
wildcards to signify wider access:

```bash
OP_ALLOWED_SECRET_REFS="op://Dev/github/token,op://Dev/aws/*,op://Agent/**"
```

Query params such as `?attribute=otp` are supported.

> [!WARNING]
> Only `op read` and `op run` are supported in the container.

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
  agent can read every secret `OP_ALLOWED_SECRET_REFS` allows, so keep
  it as minimal as possible.
- **Project config**: a repo's or work dir `.asbx` configuration can
  grant more permissions, mitigate this by validating before running.
  When a configuration file already exists, the agent cannot modify it,
  but it could create one.
- **`CLAUDE_MIRROR_PROJECTS`**: when enabled, sessions and memory
  written in the container are read by Claude on the host, so they can
  carry prompt injection.

## License

GPL-3.0
