#!/usr/bin/env bash

set -euo pipefail

project_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
dist_dir="$project_root/dist"
cert_dir="${SPEED_PROBE_TLS_OUTPUT_DIR:-$dist_dir/certs}"
cert_file="$cert_dir/speed-probe.crt"
key_file="$cert_dir/speed-probe.key"

fail() {
    printf 'error: %s\n' "$*" >&2
    exit 1
}

validate_tls_pair() {
    local cert_public_key
    local private_public_key

    openssl x509 -in "$cert_file" -noout >/dev/null 2>&1 || fail "invalid TLS certificate: $cert_file"
    openssl pkey -in "$key_file" -noout >/dev/null 2>&1 || fail "invalid TLS private key: $key_file"

    cert_public_key="$(openssl x509 -in "$cert_file" -pubkey -noout)"
    private_public_key="$(openssl pkey -in "$key_file" -pubout 2>/dev/null)"
    [[ "$cert_public_key" == "$private_public_key" ]] || fail "TLS certificate and private key do not match"
}

prepare_tls_pair() {
    local source_cert="${SPEED_PROBE_TLS_CERT_FILE:-}"
    local source_key="${SPEED_PROBE_TLS_KEY_FILE:-}"

    mkdir -p "$cert_dir"

    if [[ -n "$source_cert" || -n "$source_key" ]]; then
        [[ -n "$source_cert" && -n "$source_key" ]] || fail "set both SPEED_PROBE_TLS_CERT_FILE and SPEED_PROBE_TLS_KEY_FILE"
        [[ -f "$source_cert" ]] || fail "TLS certificate not found: $source_cert"
        [[ -f "$source_key" ]] || fail "TLS private key not found: $source_key"
        cp "$source_cert" "$cert_file"
        cp "$source_key" "$key_file"
    elif [[ -f "$cert_file" && -f "$key_file" ]]; then
        :
    elif [[ -f "$cert_file" || -f "$key_file" ]]; then
        fail "incomplete TLS pair in $cert_dir; remove it or provide both files"
    elif command -v openssl >/dev/null 2>&1; then
        printf 'Generating a development self-signed TLS certificate...\n'
        openssl req -x509 -newkey rsa:2048 -sha256 -days 365 -nodes \
            -subj "/CN=speed-probe.local" \
            -keyout "$key_file" \
            -out "$cert_file" >/dev/null 2>&1
    else
        printf 'warning: OpenSSL is unavailable; TLS development assets were not generated\n' >&2
        return
    fi

    chmod 600 "$key_file"
    validate_tls_pair
}

command -v go >/dev/null 2>&1 || fail "Go 1.25.5 or newer is required"

mkdir -p "$dist_dir"
prepare_tls_pair

commit="$(git -C "$project_root" rev-parse --short HEAD 2>/dev/null || printf unknown)"
compilation_time="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
ldflags="-s -w -X main.COMMIT=$commit -X main.COMPILATIONTIME=$compilation_time"

printf 'Building speed-probe with Mihomo and sing-box support...\n'
cd "$project_root"
go build -tags with_utls -trimpath -ldflags "$ldflags" -o "$dist_dir/speed-probe" .

printf 'Built %s\n' "$dist_dir/speed-probe"
if [[ -f "$cert_file" && -f "$key_file" ]]; then
    printf 'TLS assets: %s\n' "$cert_dir"
fi
