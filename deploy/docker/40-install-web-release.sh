#!/bin/sh

set -eu

release_root=/opt/peergo-web-release
serve_root=/usr/share/nginx/html

if [ ! -f "${release_root}/index.html" ]; then
    printf 'PeerGo Web release is missing index.html\n' >&2
    exit 1
fi

mkdir -p "${serve_root}" "${serve_root}/assets"

# Publish hashed assets first. The old index remains readable until the new
# release is complete, so a deployment never exposes a half-written SPA.
if [ -d "${release_root}/assets" ]; then
    cp -Rf "${release_root}/assets/." "${serve_root}/assets/"
fi

find "${serve_root}" -mindepth 1 -maxdepth 1 ! -name assets ! -name index.html -exec rm -rf -- {} +
find "${release_root}" -mindepth 1 -maxdepth 1 ! -name assets ! -name index.html -exec cp -Rf -- {} "${serve_root}/" \;

temporary_index="$(mktemp "${serve_root}/.peergo-index.XXXXXX")"
trap '[ -z "${temporary_index:-}" ] || rm -f "${temporary_index}"' EXIT
cp "${release_root}/index.html" "${temporary_index}"
chmod 0644 "${temporary_index}"
mv -f "${temporary_index}" "${serve_root}/index.html"
temporary_index=

# BusyBox find measures whole days. +2 keeps previous releases for roughly
# three days while bounding the persistent volume instead of growing forever.
find "${serve_root}/assets" -type f -mtime +2 -delete
find "${serve_root}/assets" -depth -type d -empty -delete
