#!/bin/sh
# Shared by the release installers and the Agent installation wizard.
set -eu

component=${ARVELD_INSTALL_COMPONENT:-arveld}
version=${ARVELD_VERSION:-}
repository=RSTCK-Innovation/arveld

fail() { printf '%s\n' "$*" >&2; exit 1; }

case "$version" in *[!0-9A-Za-z.-]*) fail 'Invalid release version.' ;; esac

case "$component" in
  arveld|arveld-agent) ;;
  *) fail 'ARVELD_INSTALL_COMPONENT must be arveld or arveld-agent.' ;;
esac
printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$' ||
  fail 'Set ARVELD_VERSION to an exact release tag.'
[ "$(uname -s)" = Linux ] || fail 'This installer requires Linux.'
[ "$(id -u)" = 0 ] || fail 'Run this script as root (sudo); preserve ARVELD_AGENT_TOKEN when installing an Agent.'
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) fail 'Supported architectures: x86_64 (amd64) and aarch64 (arm64).' ;;
esac
[ -r /etc/os-release ] || fail 'Cannot identify this Linux distribution: /etc/os-release is missing.'
. /etc/os-release
family=
for distro in "${ID:-}" ${ID_LIKE:-}; do
  case "$distro" in
    ubuntu|debian) family=debian; break ;;
    rhel|fedora|centos|almalinux|rocky|ol|amzn) family=redhat; break ;;
    suse|opensuse*|sles) family=suse; break ;;
    arch|manjaro) family=arch; break ;;
  esac
done
[ -n "$family" ] || fail "Unsupported distribution: ${ID:-unknown}. Install the release binary manually."
command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ] ||
  fail 'A running systemd system is required. For containers, use the Docker installation.'

config_dir=/etc/$component
data_dir=/var/lib/$component
binary=/usr/local/bin/$component
environment=$config_dir/agent.env
write_connection=false
if [ "$component" = arveld-agent ]; then
  if [ -n "${ARVELD_URL:-}" ] || [ -n "${ARVELD_AGENT_TOKEN:-}" ] || [ ! -f "$environment" ]; then
    [ -n "${ARVELD_AGENT_TOKEN:-}" ] || fail 'ARVELD_AGENT_TOKEN is required.'
    # Keys are generated from letters, digits and underscores; reject control characters.
    case "$ARVELD_AGENT_TOKEN" in *[!a-zA-Z0-9_-]*) fail 'Invalid ARVELD_AGENT_TOKEN.' ;; esac
    case "${ARVELD_URL:-}" in *[[:space:]]*) fail 'ARVELD_URL must not contain whitespace.' ;; esac
    printf '%s\n' "${ARVELD_URL:-}" | grep -Eq '^https?://[^/?#@[:space:]]+(/[^?#@[:space:]]*)?$' ||
      fail 'ARVELD_URL must be an HTTP(S) base URL without credentials, whitespace, query or fragment.'
    write_connection=true
  fi
fi

# Only install prerequisites when needed; do not upgrade the operating system.
missing=false
for tool in curl tar gzip sha256sum install getent useradd groupadd; do
  command -v "$tool" >/dev/null 2>&1 || missing=true
done
if [ ! -s /etc/ssl/certs/ca-certificates.crt ] && [ ! -s /etc/pki/tls/certs/ca-bundle.crt ] && [ ! -s /etc/ssl/ca-bundle.pem ]; then
  missing=true
fi
if [ "$missing" = true ]; then
  case "$family" in
    debian)
      apt-get update
      DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates curl tar gzip coreutils passwd
      ;;
    redhat)
      if command -v dnf >/dev/null 2>&1; then manager=dnf; else manager=yum; fi
      packages=ca-certificates
      # Keep compatible minimal variants (curl-minimal, coreutils-single) intact.
      for dependency in curl:curl tar:tar gzip:gzip sha256sum:coreutils install:coreutils useradd:shadow-utils groupadd:shadow-utils getent:glibc-common; do
        command -v "${dependency%%:*}" >/dev/null 2>&1 || packages="$packages ${dependency#*:}"
      done
      "$manager" install -y $packages
      ;;
    suse) zypper --non-interactive install ca-certificates curl tar gzip coreutils shadow ;;
    arch) pacman -S --noconfirm --needed ca-certificates curl tar gzip coreutils shadow ;;
  esac
fi

umask 077
work=$(mktemp -d)
staged_binary=
cleanup() {
  rm -rf "$work"
  [ -z "$staged_binary" ] || rm -f "$staged_binary"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
archive=$component-${version}_linux_$arch.tar.gz
download() {
  if [ -n "${ARVELD_RELEASE_DIR:-}" ]; then
    cp "$ARVELD_RELEASE_DIR/$1" "$work/$1"
  else
    curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
      --retry 3 --connect-timeout 15 --max-time 300 \
      "https://github.com/$repository/releases/download/$version/$1" -o "$work/$1"
  fi
}
download "$archive"
download SHA256SUMS
# Verify this exact file, including that its checksum exists, before extraction.
awk -v file="$archive" '$2 == file || $2 == "./" file { print $1 "  " file; count++ }
  END { if (count != 1) exit 1 }' "$work/SHA256SUMS" > "$work/selected.sha256" ||
  fail "SHA256SUMS must contain exactly one entry for $archive."
(cd "$work" && sha256sum --check selected.sha256)
mkdir "$work/unpacked"
tar -xzf "$work/$archive" -C "$work/unpacked" --no-same-owner "./$component"
[ -f "$work/unpacked/$component" ] && [ ! -L "$work/unpacked/$component" ] || fail 'Missing release executable.'
chmod 0755 "$work/unpacked/$component"
[ "$("$work/unpacked/$component" --version)" = "$component $version" ] || fail 'Release executable version mismatch.'

if ! getent group "$component" >/dev/null; then groupadd --system "$component"; fi
if ! id "$component" >/dev/null 2>&1; then
  nologin=$(command -v nologin || printf /sbin/nologin)
  useradd --system --gid "$component" --home-dir "$data_dir" --no-create-home --shell "$nologin" "$component"
fi
[ "$(id -u "$component")" != 0 ] || fail 'The service account must not be root.'
install -d -o "$component" -g "$component" -m 0700 "$data_dir"
install -d -o root -g "$component" -m 0750 "$config_dir"
install -d -m 0755 /usr/local/bin
# Rename on the same filesystem so a running executable can be replaced safely.
staged_binary=$(mktemp /usr/local/bin/.$component.XXXXXX)
install -o root -g root -m 0755 "$work/unpacked/$component" "$staged_binary"
mv -f "$staged_binary" "$binary"
staged_binary=

if [ "$component" = arveld ]; then
  if [ ! -e "$config_dir/arveld.yml" ]; then
    cat > "$work/arveld.yml" <<'CONFIG'
http_address: 127.0.0.1:8080
session_cookie_secure: true
database_path: /var/lib/arveld/arveld.db
prometheus_retention_time: 15d
CONFIG
    install -o root -g "$component" -m 0640 "$work/arveld.yml" "$config_dir/arveld.yml"
  fi
  command_line="$binary --config $config_dir/arveld.yml"
  service_options=
else
  if [ "$write_connection" = true ]; then
    # systemd EnvironmentFile double quotes only require escaping backslash and quote.
    escaped_url=$(printf '%s' "$ARVELD_URL" | sed 's/\\/\\\\/g; s/"/\\"/g')
    printf 'ARVELD_URL="%s"\nARVELD_AGENT_TOKEN="%s"\n' "$escaped_url" "$ARVELD_AGENT_TOKEN" > "$work/agent.env"
    install -o root -g root -m 0600 "$work/agent.env" "$environment"
  fi
  command_line="$binary --storage-directory=$data_dir"
  service_options="EnvironmentFile=$environment
AmbientCapabilities=CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_RAW"
fi

cat > "$work/$component.service" <<UNIT
[Unit]
Description=$component
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=$component
Group=$component
WorkingDirectory=$data_dir
ExecStart=$command_line
$service_options
Restart=on-failure
RestartSec=5
TimeoutStopSec=90
UMask=0077
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
UNIT
install -m 0644 "$work/$component.service" "/etc/systemd/system/$component.service"
# Keep the distribution's SELinux policy and restore the installed files' labels.
if command -v restorecon >/dev/null 2>&1; then
  restorecon -R "$binary" "$config_dir" "$data_dir" "/etc/systemd/system/$component.service"
fi
systemctl daemon-reload
systemctl enable "$component.service"
systemctl restart "$component.service"
systemctl is-active --quiet "$component.service" || fail "Service failed; inspect journalctl -u $component."
printf '%s\n' "$component $version installed. Logs: journalctl -u $component -f"
if [ "$component" = arveld ]; then
  printf '%s\n' 'Configure an HTTPS reverse proxy to 127.0.0.1:8080, then create your administrator.' \
    'Guide: https://github.com/RSTCK-Innovation/arveld/blob/main/docs/guides/https.md'
else
  printf '%s\n' 'Confirm the Agent connection and fresh measurements in Arveld.'
fi
