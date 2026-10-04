"""Run release installers against real systemd on disposable GitHub Linux runners."""

import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.error
import urllib.request


def run(*args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def wait_for(check):
    for _ in range(90):
        try:
            result = check()
            if result:
                return result
        except (urllib.error.URLError, TimeoutError):
            pass
        time.sleep(2)
    raise AssertionError("Timed out waiting for the installed services")


assert os.environ.get("GITHUB_ACTIONS") == "true" and os.geteuid() == 0, "Disposable CI runner only"
assets = Path(sys.argv[1]).resolve()
version = sys.argv[2]
print(f"[CONTEXT] Native systemd test using real controller and Agent binaries for {version}.", flush=True)
print("[CHECK] Require an empty disposable runner before installing either service.", flush=True)
for component in ("arveld", "arveld-agent"):
    for path in (f"/etc/{component}", f"/var/lib/{component}", f"/usr/local/bin/{component}",
                 f"/etc/systemd/system/{component}.service",
                 f"/usr/local/share/licenses/{component}"):
        assert not Path(path).exists(), f"Refusing to overwrite {path}"

client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
origin = "http://127.0.0.1:8080"


def request(path, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(origin + path, data=data,
                                 headers={"Content-Type": "application/json", "Origin": origin})
    with client.open(req, timeout=5) as response:
        content = response.read()
        return json.loads(content) if content else None


with tempfile.TemporaryDirectory(prefix="arveld-systemd-") as directory:
    release = Path(directory)
    for path in assets.iterdir():
        if path.name.endswith((".tar.gz", ".sh")):
            shutil.copy(path, release / path.name)
    checksums = [f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}\n"
                 for path in release.iterdir()]
    (release / "SHA256SUMS").write_text("".join(checksums))
    env = {**os.environ, "ARVELD_RELEASE_DIR": str(release)}

    def install(component, connection=None):
        run("sh", str(release / f"install-{component}.sh"), env={**env, **(connection or {})})
        arch = {"x86_64": "amd64", "aarch64": "arm64"}[os.uname().machine]
        with tarfile.open(release / f"{component}-{version}_linux_{arch}.tar.gz") as archive:
            for filename in ("LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.txt"):
                installed = Path(f"/usr/local/share/licenses/{component}/{filename}")
                source = archive.extractfile(f"./{filename}")
                assert source is not None
                assert installed.read_bytes() == source.read()
                assert installed.stat().st_mode & 0o777 == 0o644
        run("systemctl", "is-active", "--quiet", component)
        run("systemctl", "is-enabled", "--quiet", component)

    try:
        print("[TEST] Install the controller, enable/start its service and verify the ready endpoint version.", flush=True)
        install("arveld")
        assert wait_for(lambda: request("/readyz"))["version"] == version
        print("[PASS] Controller service and readiness version checks passed.", flush=True)
        # This CI-only loopback session uses HTTP; production keeps secure cookies.
        config = Path("/etc/arveld/arveld.yml")
        config.write_text(config.read_text().replace("session_cookie_secure: true", "session_cookie_secure: false"))
        run("systemctl", "restart", "arveld")
        wait_for(lambda: request("/readyz"))
        print("[SETUP] Create a temporary administrator and Agent key for the loopback integration test.", flush=True)
        account = {"email": "installer@example.com", "password": "installer integration test password"}
        request("/api/v1/auth/setup", {**account, "name": "Installer test"})
        request("/api/v1/auth/login", account)
        key = request("/api/v1/agentkeys", {"name": "Native installer test"})
        print("[TEST] Install the Agent, enable/start its service and wait for an authenticated connection.", flush=True)
        install("arveld-agent", {"ARVELD_URL": origin, "ARVELD_AGENT_TOKEN": key["token"]})

        def connected_agent():
            agents = request("/api/v1/agents")["agents"]
            assert len(agents) <= 1, "Reinstallation created a new Agent identity"
            return agents[0] if agents and agents[0]["connected"] else None

        identity = wait_for(connected_agent)["instance_uid"]
        print("[PASS] The Agent connected successfully.", flush=True)
        saved_config = config.read_bytes()
        credentials = Path("/etc/arveld-agent/agent.env").read_bytes()
        print("[TEST] Stop the Agent, observe disconnection and reinstall both services without new credentials.", flush=True)
        run("systemctl", "stop", "arveld-agent")
        wait_for(lambda: not connected_agent())
        install("arveld")
        wait_for(lambda: request("/readyz"))
        install("arveld-agent")
        assert wait_for(connected_agent)["instance_uid"] == identity
        assert config.read_bytes() == saved_config
        assert Path("/etc/arveld-agent/agent.env").read_bytes() == credentials
        print("[PASS] Both services restarted; Agent identity, controller configuration and credentials were preserved.", flush=True)
    finally:
        print("[CLEANUP] Collect service logs and remove only resources created by this test.", flush=True)
        # Only paths created after the empty-host assertions above are removed.
        for component in ("arveld-agent", "arveld"):
            subprocess.run(["journalctl", "--no-pager", "-u", component, "-n", "30"], check=False)
            subprocess.run(["systemctl", "disable", "--now", component], check=False)
            for path in (f"/etc/{component}", f"/var/lib/{component}",
                         f"/usr/local/share/licenses/{component}"):
                shutil.rmtree(path, ignore_errors=True)
            Path(f"/usr/local/bin/{component}").unlink(missing_ok=True)
            Path(f"/etc/systemd/system/{component}.service").unlink(missing_ok=True)
        run("systemctl", "daemon-reload")
