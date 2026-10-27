#!/usr/bin/env bash
# Signs an image, by digest, with the cosign key in COSIGN_PRIVATE_KEY,
# whose password is in COSIGN_PASSWORD. The signature is attached to the
# image as a Sigstore bundle, without a transparency log entry or
# timestamp, since asbx verifies it with only the public key in
# internal/image/embed/cosign.pub.
#
# Usage: sign-image.sh <image>@<digest>
set -euo pipefail

image="$1"

# A tag can be moved after it's signed, a digest can't.
if [[ "$image" != *@sha256:* ]]; then
	echo "error: $image isn't by digest" >&2
	exit 1
fi

# Without a signing config, cosign signs with only the key, and without
# --tlog-upload=false it would still upload to the public Rekor.
cosign sign --yes \
	--key env://COSIGN_PRIVATE_KEY \
	--use-signing-config=false \
	--tlog-upload=false \
	"$image"
