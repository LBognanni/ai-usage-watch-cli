#!/bin/sh
# Install a checksum-verified Linux release without sudo. No client credentials
# are accessed. This script is intentionally compatible with `curl | sh`.
set -eu

fail() {
    printf 'aiu: %s\n' "$*" >&2
    exit 1
}

main() {
    for tool in uname curl tar sha256sum awk grep mktemp mkdir cp chmod mv rm; do
        command -v "$tool" >/dev/null 2>&1 || fail "Required command not found: $tool"
    done

    [ "$(uname -s)" = Linux ] || fail 'Only Linux is supported.'
    case "$(uname -m)" in
        x86_64|amd64) arch=amd64 ;;
        aarch64|arm64) arch=arm64 ;;
        *) fail 'Supported architectures: Linux x86_64 and arm64.' ;;
    esac

    version=${AIU_VERSION:-latest}
    repository=https://github.com/LBognanni/ai-usage-watch-cli
    if [ "$version" = latest ]; then
        base=$repository/releases/latest/download
    else
        printf '%s\n' "$version" | grep -Eq '^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$' || fail 'AIU_VERSION must be a stable version such as v0.1.0.'
        version=v${version#v}
        base=$repository/releases/download/$version
    fi

    install_dir=${AIU_INSTALL_DIR:-${HOME:?HOME must be set}/.local/bin}
    tmp=$(mktemp -d "${TMPDIR:-/tmp}/aiu-download.XXXXXX")
    stage=
    # Only remove directories this invocation created using mktemp.
    trap 'rm -rf "$tmp"; if [ -n "$stage" ]; then rm -rf "$stage"; fi' 0
    trap 'exit 1' HUP INT TERM
    asset=aiu_linux_$arch.tar.gz

    printf 'Downloading aiu (%s, Linux %s)...\n' "$version" "$arch"
    curl --proto '=https' --proto-redir '=https' --tlsv1.2 --fail --silent --show-error --location \
        --retry 3 --connect-timeout 10 --max-time 120 -o "$tmp/$asset" "$base/$asset" || fail 'Download failed; check that a release exists.'
    curl --proto '=https' --proto-redir '=https' --tlsv1.2 --fail --silent --show-error --location \
        --retry 3 --connect-timeout 10 --max-time 120 -o "$tmp/SHA256SUMS" "$base/SHA256SUMS" || fail 'Checksum download failed.'

    checksum=$(awk -v asset="$asset" '$2 == asset { print $1 }' "$tmp/SHA256SUMS")
    printf '%s\n' "$checksum" | grep -Eq '^[0-9a-fA-F]{64}$' || fail 'Missing or invalid release checksum.'
    # A manifest with duplicate entries is also invalid.
    [ "$(printf '%s\n' "$checksum" | awk 'END { print NR }')" = 1 ] || fail 'Duplicate release checksums.'
    printf '%s  %s\n' "$checksum" "$asset" > "$tmp/verify.sha256"
    (cd "$tmp" && sha256sum -c verify.sha256 >/dev/null) || fail 'Checksum mismatch; nothing was installed. Try again or select AIU_VERSION explicitly.'

    tar -xzf "$tmp/$asset" -C "$tmp" aiu || fail 'Invalid release archive.'
    if [ ! -f "$tmp/aiu" ] || [ -L "$tmp/aiu" ]; then
        fail 'Release must contain a regular aiu executable.'
    fi
    mkdir -p "$install_dir"
    stage=$(mktemp -d "$install_dir/.aiu-install.XXXXXX")
    cp "$tmp/aiu" "$stage/aiu"
    chmod 755 "$stage/aiu"
    mv -f "$stage/aiu" "$install_dir/aiu"
    printf 'Installed aiu to %s/aiu\n' "$install_dir"
    case ":${PATH:-}:" in
        *:"$install_dir":*) ;;
        *) printf 'Add %s to your PATH, then run aiu --help.\n' "$install_dir" ;;
    esac
}

main "$@"
