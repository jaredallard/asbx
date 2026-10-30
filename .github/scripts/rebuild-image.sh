#!/usr/bin/env bash
# Rebuilds a release's image from its source, as goreleaser built it but
# with the latest base image and packages, pushes it under the release's
# tag and latest, and signs it.
#
# Usage: rebuild-image.sh <release source dir> <tag>
set -euo pipefail

src="$1"
tag="$2"

readonly image=ghcr.io/jaredallard/asbx
readonly platforms=(linux/amd64 linux/arm64)

ctx=$(mktemp -d)
trap 'rm -rf "$ctx"' EXIT

# The Dockerfile copies asbx-op, asbx-clip and asbx-open from
# $TARGETPLATFORM/, where goreleaser puts them. It's built with goreleaser's flags.
for platform in "${platforms[@]}"; do
	mkdir -p "$ctx/$platform"
	(
		cd "$src"
		CGO_ENABLED=0 GOOS="${platform%/*}" GOARCH="${platform#*/}" \
			go build -trimpath -ldflags "-s -w" -o "$ctx/$platform/asbx-op" ./cmd/asbx-op
		CGO_ENABLED=0 GOOS="${platform%/*}" GOARCH="${platform#*/}" \
			go build -trimpath -ldflags "-s -w" -o "$ctx/$platform/asbx-clip" ./cmd/asbx-clip
		CGO_ENABLED=0 GOOS="${platform%/*}" GOARCH="${platform#*/}" \
			go build -trimpath -ldflags "-s -w" -o "$ctx/$platform/asbx-open" ./cmd/asbx-open
	)
done

cp "$src/Dockerfile" "$ctx/"
cp -R "$src/rootfs" "$ctx/"

# goreleaser's labels, see .goreleaser.yaml.
labels=(
	"org.opencontainers.image.title=asbx"
	"org.opencontainers.image.description=asbx"
	"org.opencontainers.image.source=$(git -C "$src" remote get-url origin)"
	"org.opencontainers.image.version=${tag#v}"
	"org.opencontainers.image.created=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
	"org.opencontainers.image.revision=$(git -C "$src" rev-parse HEAD)"
	"org.opencontainers.image.licenses=GPL-3.0"
)

args=()
for label in "${labels[@]}"; do
	args+=(--label "$label")
done

# Nothing is reused from earlier builds, since getting the latest base
# image and packages is the point.
docker buildx build \
	--pull \
	--no-cache \
	--platform "$(IFS=, && echo "${platforms[*]}")" \
	"${args[@]}" \
	--tag "$image:$tag" \
	--tag "$image:latest" \
	--push \
	"$ctx"

digest=$(docker buildx imagetools inspect "$image:$tag" --format '{{.Manifest.Digest}}')
"$(dirname "$0")/sign-image.sh" "$image@$digest"
