#!/bin/sh
set -eu
[ "${ARVELD_INSTALLER_TEST:-}" = 1 ] && [ -f /.dockerenv ] || exit 1

expect_rejection() {
  description=$1
  expected=$2
  shift 2
  printf '[TEST] %s — rejection is expected.\n' "$description"
  if output=$("$@" 2>&1); then
    printf '%s\n' "$output" >&2
    printf '[FAIL] %s was unexpectedly accepted.\n' "$description" >&2
    exit 1
  fi
  if ! printf '%s\n' "$output" | grep -Fq -- "$expected"; then
    printf '%s\n' "$output" >&2
    printf '[FAIL] %s failed for an unexpected reason.\n' "$description" >&2
    exit 1
  fi
  printf '%s\n' "$output" | sed 's/^/[EXPECTED REJECTION] /'
  printf '[PASS] %s was rejected for the expected reason.\n' "$description"
}

# The container has no PID 1 systemd. Record that boundary while exercising real
# distro package managers, accounts, permissions, extraction and persisted state.
mkdir -p /run/systemd/system /etc/systemd/system /test-bin
cat > /test-bin/systemctl <<'SH'
#!/bin/sh
printf '%s\n' "$*" >> /systemctl-calls
SH
chmod +x /test-bin/systemctl
export PATH=/test-bin:$PATH ARVELD_RELEASE_DIR=/release
export ARVELD_URL='https://arveld.example.com/team\path/"quote"/literal$HOME'
export ARVELD_AGENT_TOKEN=arv_agent_test_key

for component in arveld arveld-agent; do
  printf '[TEST] Fresh %s installation — check version, non-root account, permissions and service commands.\n' "$component"
  sh "/release/install-$component.sh"
  [ "$("/usr/local/bin/$component" --version)" = "$component v0.0.0-rc.0" ]
  [ "$(id -u "$component")" != 0 ]
  [ "$(stat -c '%U:%G:%a' "/var/lib/$component")" = "$component:$component:700" ]
  grep -Fx "User=$component" "/etc/systemd/system/$component.service"
  grep -Fx "enable $component.service" /systemctl-calls
  grep -Fx "restart $component.service" /systemctl-calls
  printf '%s\n' 'preserved identity' > "/var/lib/$component/identity"
  printf '[PASS] Fresh %s installation checks passed.\n' "$component"
done
printf '%s\n' '[TEST] Check secure controller defaults, escaped Agent connection values, private credentials and network capability.'
[ "$(stat -c '%U:%G:%a' /etc/arveld-agent/agent.env)" = 'root:root:600' ]
grep -Fx 'session_cookie_secure: true' /etc/arveld/arveld.yml
grep -Fx 'ARVELD_URL="https://arveld.example.com/team\\path/\"quote\"/literal$HOME"' /etc/arveld-agent/agent.env
grep -Fx 'ARVELD_AGENT_TOKEN="arv_agent_test_key"' /etc/arveld-agent/agent.env
grep -Fx 'AmbientCapabilities=CAP_NET_RAW' /etc/systemd/system/arveld-agent.service
printf '%s\n' '[PASS] Configuration and permission checks passed.'

# Reinstallation must preserve local configuration, credentials and Agent identity.
printf '%s\n' '[TEST] Reinstall both components — preserve local configuration, Agent credentials and identity.'
printf '%s\n' '# local retention override' >> /etc/arveld/arveld.yml
sha256sum /etc/arveld/arveld.yml /etc/arveld-agent/agent.env > /config.sha256
unset ARVELD_URL ARVELD_AGENT_TOKEN
for component in arveld arveld-agent; do
  sh "/release/install-$component.sh"
  [ "$(cat "/var/lib/$component/identity")" = 'preserved identity' ]
done
sha256sum --check /config.sha256
printf '%s\n' '[PASS] Reinstallation preserved configuration, credentials and identity.'

# Reject corruption or a missing checksum before replacing a binary or restarting.
mkdir /bad-release
cp /release/* /bad-release/
export ARVELD_RELEASE_DIR=/bad-release
sha256sum /usr/local/bin/arveld /systemctl-calls > /before.sha256
printf '\ncorrupted' >> "/bad-release/arveld-v0.0.0-rc.0_linux_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz"
expect_rejection 'Intentionally corrupted archive' ': FAILED' sh /release/install-arveld.sh
printf '%s\n' '[CHECK] A rejected archive must not replace the installed binary or call systemctl.'
sha256sum --check /before.sha256
cp /release/* /bad-release/
printf '' > /bad-release/SHA256SUMS
expect_rejection 'Intentionally missing checksum entry' 'SHA256SUMS must contain exactly one entry' sh /release/install-arveld.sh
printf '%s\n' '[CHECK] A missing checksum must not replace the installed binary or call systemctl.'
sha256sum --check /before.sha256

# An invalid connection must not alter a working Agent installation.
export ARVELD_RELEASE_DIR=/release ARVELD_AGENT_TOKEN=arv_agent_test_key
ARVELD_URL=$(printf 'https://arveld.example.com\nINJECTED=value')
export ARVELD_URL
expect_rejection 'Intentionally invalid multiline Agent URL' 'ARVELD_URL must not contain whitespace' sh /release/install-arveld-agent.sh
printf '%s\n' '[CHECK] A rejected Agent URL must preserve the existing configuration and credentials.'
sha256sum --check /config.sha256
printf '%s\n' '[PASS] All Linux installer checks passed, including every expected rejection.'
