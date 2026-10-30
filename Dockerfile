FROM ubuntu:resolute

SHELL [ "/usr/bin/env", "bash", "-c" ]

# Install mise
RUN <<EOF
	set -euo pipefail

	apt-get update
	apt-get install -y --no-install-recommends software-properties-common

	apt-get install -y --no-install-recommends extrepo
	extrepo enable mise
	apt-get update -y
	apt-get install -y --no-install-recommends mise
EOF

# Install system packages
RUN <<EOF
	set -euo pipefail

	apt-get install -y sudo jq curl wget net-tools dnsutils git openssh-client build-essential \
		zip unzip 7zip
EOF

# Where Podman keeps its runtime state and API socket.
ENV XDG_RUNTIME_DIR=/run/user/1000

# Install rootless Podman, for podman_enabled. newuidmap and newgidmap
# get file capabilities rather than setuid root. As root, writing a user
# namespace's ID maps needs CAP_SYS_ADMIN, which the container doesn't
# have.
RUN <<EOF
	set -euo pipefail

	apt-get install -y --no-install-recommends podman podman-docker uidmap passt libcap2-bin

	# Silences podman-docker's notice on every docker command.
	touch /etc/containers/nodocker

	chmod u-s /usr/bin/newuidmap /usr/bin/newgidmap
	setcap cap_setuid=ep /usr/bin/newuidmap
	setcap cap_setgid=ep /usr/bin/newgidmap

	# Every ID below 65536 but ubuntu's own, so they fit in rootless
	# Docker's user namespace.
	printf 'ubuntu:1:999\nubuntu:1001:64535\n' | tee /etc/subuid >/etc/subgid

	# podman-docker links /var/run/docker.sock to rootful Podman's
	# socket, which never exists here; use rootless Podman's instead, for
	# tools that ignore DOCKER_HOST.
	ln -sfn "$XDG_RUNTIME_DIR/podman/podman.sock" /var/run/docker.sock
EOF

COPY rootfs/containers.conf /etc/containers/containers.conf

# Stands in for podman and docker when podman_enabled is off, so the
# agent is told how to turn it on rather than seeing Podman fail. docker
# needs its own, podman-docker's runs /usr/bin/podman.
COPY --chmod=0755 rootfs/podman-disabled.sh /opt/asbx/podman-disabled/podman
RUN ln -s podman /opt/asbx/podman-disabled/docker

# Install Google Chrome for agents to use for browser based testing.
RUN <<EOF
	set -euo pipefail

	file_name="google-chrome-stable_current_$(dpkg --print-architecture).deb"
	wget "https://dl.google.com/linux/direct/$file_name"
	apt install -y "./$file_name"
	rm "$file_name"
EOF

# Assign the existing ubuntu user to the sudoers
RUN <<EOF
	set -euo pipefail

	echo "ubuntu ALL=(ALL) NOPASSWD:ALL" >/etc/sudoers.d/user
	chmod 0440 /etc/sudoers.d/user
EOF


# The image only ships the C locales; use the UTF-8 one so the terminal
# renders non-ASCII output.
ENV LANG=C.UTF-8

# Use the host's gitconfig (mounted read-only by asbx), overriding the parts
# that only work on the macOS host: credentials come from fj instead of the
# keychain, and SSH signing goes through the forwarded agent.
COPY rootfs/gitconfig /etc/gitconfig

COPY --chmod=0755 rootfs/entrypoint.sh /opt/asbx/entrypoint.sh

ARG TARGETPLATFORM
COPY --chmod=0755 $TARGETPLATFORM/asbx-op /usr/local/bin/asbx-op
RUN <<EOF
	set -euo pipefail

	ln -s asbx-op /usr/local/bin/op
	install -d -o ubuntu -g ubuntu /run/asbx
EOF

# Stands in for the clipboard tools, which can't reach a display here,
# and forwards them to the host's clipboard when clipboard is enabled.
COPY --chmod=0755 $TARGETPLATFORM/asbx-clip /usr/local/bin/asbx-clip
RUN <<EOF
	set -euo pipefail

	for tool in wl-copy wl-paste xclip xsel pbcopy pbpaste; do
		ln -s asbx-clip "/usr/local/bin/$tool"
	done
EOF

COPY --chmod=0755 $TARGETPLATFORM/asbx-open /usr/local/bin/asbx-open
RUN ln -s asbx-open /usr/local/bin/xdg-open
ENV BROWSER=xdg-open

USER ubuntu
WORKDIR /home/ubuntu

# Ensure perms + dirs are created and exist. We pre-create some dirs so
# that the bind mount doesn't mess up permissions.
RUN <<EOF
	set -euo pipefail

	mkdir -p /home/ubuntu/{.claude/projects,.codex,.cache/mise,.local/share/mise,.local/state/mise,.local/share/containers}
	sudo chown -R 1000:1000 /home/ubuntu
	sudo install -d -o ubuntu -g ubuntu -m 0700 "$XDG_RUNTIME_DIR"
EOF

COPY --chown=1000:1000 rootfs/mise.toml /home/ubuntu/.config/mise/config.toml
COPY --chown=1000:1000 rootfs/mise.lock /home/ubuntu/.config/mise/mise.lock
