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

	apt-get install -y sudo jq curl wget net-tools dnsutils git openssh-client build-essential
EOF

# The image only ships the C locales; use the UTF-8 one so the terminal
# renders non-ASCII output.
ENV LANG=C.UTF-8

# Use the host's gitconfig (mounted read-only by asbx), overriding the parts
# that only work on the macOS host: credentials come from fj instead of the
# keychain, and SSH signing goes through the forwarded agent.
COPY rootfs/gitconfig /etc/gitconfig

COPY --chmod=0755 rootfs/entrypoint.sh /opt/asbx/entrypoint.sh

# Assign the existing ubuntu user to the sudoers
RUN <<EOF
	set -euo pipefail

	echo "ubuntu ALL=(ALL) NOPASSWD:ALL" >/etc/sudoers.d/user
	chmod 0440 /etc/sudoers.d/user
EOF

USER ubuntu
WORKDIR /home/ubuntu

# Install Google Chrome for agents to use for browser based testing.
RUN <<EOF
	set -euo pipefail

	file_name="google-chrome-stable_current_$(dpkg --print-architecture).deb"
	wget "https://dl.google.com/linux/direct/$file_name"
	sudo apt install -y "./$file_name"
	rm "$file_name"
EOF

# Ensure perms + dirs are created and exist. We pre-create some dirs so
# that the bind mount doesn't mess up permissions.
RUN <<EOF
	set -euo pipefail

	mkdir -p /home/ubuntu/{.claude/projects,.cache/mise,.local/share/mise}
	chmod -R 1000:1000 /home/ubuntu
EOF

COPY --chown=1000:1000 rootfs/mise.toml /home/ubuntu/.config/mise/config.toml
COPY --chown=1000:1000 rootfs/mise.lock /home/ubuntu/.config/mise/mise.lock
