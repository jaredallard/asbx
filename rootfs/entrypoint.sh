#!/usr/bin/env bash
# Prepares the asbx container from the host's read-only mounts and starts
# claude in the working directory (the repository, at its host path).

set -euo pipefail

host_mnt=/mnt/readonly_host_mnt

# Copy the host's Claude configuration. asbx doesn't mount projects, the
# current project's sessions and memory are bind mounted read-write over
# projects/<slug> when mirrored.
shopt -s dotglob nullglob
for f in "$host_mnt/.claude"/*; do
	cp -r "$f" "$HOME/.claude/"
done
shopt -u dotglob nullglob

if [[ -n ${ASBX_TRUSTED_DIR:-} ]]; then
	jq --arg dir "$ASBX_TRUSTED_DIR" \
		'.projects[$dir].hasTrustDialogAccepted = true' \
		"$host_mnt/.claude.json" >"$HOME/.claude.json"
else
	cp "$host_mnt/.claude.json" "$HOME/"
fi

# Only mounted when Forgejo is enabled. The token comes from FORGEJO_TOKEN.
if [[ -f "$host_mnt/fj-config.json" ]]; then
	mkdir -p "$HOME/.config/fj"
	jq "del(.token)" "$host_mnt/fj-config.json" >"$HOME/.config/fj/config.json"
fi

# Podman's Docker API socket, for tools that use DOCKER_HOST rather than
# the docker command.
if [[ ${ASBX_PODMAN:-} == 1 ]]; then
	podman system service --time=0 >"$XDG_RUNTIME_DIR/podman-service.log" 2>&1 &
fi

if [[ -e ".mise.toml" ]] || [ -e "mise.toml" ]; then
	install_args=()
	if [[ -e "mise.lock" ]]; then
		install_args+=("--locked")
	fi

	mise install "${install_args[@]}"
fi

# Every tool in the image's and the project's mise config goes on PATH, not
# just claude, so hooks and the agent can run them (e.g., rtk).
exec mise exec -- claude "$@"
