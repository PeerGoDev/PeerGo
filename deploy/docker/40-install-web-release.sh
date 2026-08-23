#!/bin/sh

set -eu

release_root=/opt/peergo-web-release
serve_root=/usr/share/nginx/html

if [ ! -f "${release_root}/index.html" ]; then
    printf 'PeerGo Web release is missing index.html\n' >&2
    exit 1
fi

mkdir -p "${serve_root}" "${serve_root}/assets"

# Root documents describe only the current release and must be replaced. The
# hashed asset directory is merged so browser tabs opened before a deploy can
# finish their next navigation without receiving HTML in place of JavaScript.
find "${serve_root}" -mindepth 1 -maxdepth 1 ! -name assets -exec rm -rf -- {} +
find "${release_root}" -mindepth 1 -maxdepth 1 ! -name assets -exec cp -Rf -- {} "${serve_root}/" \;
if [ -d "${release_root}/assets" ]; then
    cp -Rf "${release_root}/assets/." "${serve_root}/assets/"
fi

# BusyBox find measures whole days. +2 keeps previous releases for roughly
# three days while bounding the persistent volume instead of growing forever.
find "${serve_root}/assets" -type f -mtime +2 -delete
find "${serve_root}/assets" -depth -type d -empty -delete
