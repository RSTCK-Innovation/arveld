import { expect, test } from 'bun:test';
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { agentRelease, installationScript } from '../src/data/installation';

const input = {
  method: 'docker' as const,
  // biome-ignore lint/suspicious/noTemplateCurlyInString: Exercise literal shell expansion syntax in the supplied URL.
  endpoint: "https://arveld.example.com/team's/$(touch${IFS}injected)",
};

test.each(['docker', 'linux'] as const)(
  '%s starts with the two connection variables and no Supervisor file',
  (method) => {
    const directory = mkdtempSync(join(tmpdir(), 'arveld-install-'));
    try {
      // Capture the process boundary without starting containers or contacting Arveld.
      writeFileSync(
        join(directory, 'docker'),
        '#!/bin/sh\nprintf "%s\\n" "$ARVELD_AGENT_TOKEN" "$ARVELD_URL" "$@"\n',
        { mode: 0o700 },
      );
      const endpoint = input.endpoint.replace('https:', 'HtTpS:');
      const script = installationScript({ ...input, method, endpoint }, 'v0.1.0-rc.3');
      // Stop native installation at the platform boundary, before any host mutation.
      writeFileSync(
        join(directory, 'uname'),
        '#!/bin/sh\nprintf "%s\\n" "$ARVELD_AGENT_TOKEN" "$ARVELD_URL" "$ARVELD_INSTALL_COMPONENT" "$ARVELD_VERSION" > connection\necho Unsupported\n',
        { mode: 0o700 },
      );
      const env = { PATH: `${directory}:${process.env.PATH}`, ARVELD_AGENT_TOKEN: 'test-key' };
      const execute = (token: string) =>
        Bun.spawnSync(['sh', '-c', script], {
          cwd: directory,
          env: { ...env, ARVELD_AGENT_TOKEN: token },
        });
      const missing = execute('');
      expect(missing.exitCode).toBe(1);
      expect(missing.stderr.toString()).toContain('ARVELD_AGENT_TOKEN is required');
      expect(missing.stdout.toString()).toBe('');

      const result = execute(env.ARVELD_AGENT_TOKEN);
      expect(result.exitCode).toBe(method === 'docker' ? 0 : 1);
      const output =
        method === 'docker'
          ? result.stdout.toString()
          : readFileSync(join(directory, 'connection'), 'utf8');
      const [token, url, ...args] = output.trimEnd().split('\n');
      expect(token).toBe(env.ARVELD_AGENT_TOKEN);
      expect(url).toBe(input.endpoint);
      expect(script).not.toContain(env.ARVELD_AGENT_TOKEN);
      expect(existsSync(join(directory, 'injected'))).toBe(false);
      if (method === 'docker') {
        expect(args).toContain('run');
        expect(args).toContain('ARVELD_AGENT_TOKEN');
        expect(args).toContain('ARVELD_URL');
        expect(args).toContain('arveld-agent-data:/var/lib/arveld-agent');
        expect(args).toContain('/:/hostfs:ro');
        expect(args.at(-1)).toBe('ghcr.io/rstck-innovation/arveld-agent:v0.1.0-rc.3');
      } else {
        expect(args).toEqual(['arveld-agent', 'v0.1.0-rc.3']);
        expect(result.stderr.toString()).toContain('This installer requires Linux.');
        expect(script).not.toContain('docker compose');
      }
    } finally {
      rmSync(directory, { recursive: true, force: true });
    }
  },
);

test('installation rejects unsupported URLs and release overrides', () => {
  for (const endpoint of [
    'wss://arveld.example.com/v1/opamp',
    'https://user:password@arveld.example.com',
    'https://arveld.example.com/?token=secret',
    'https://arveld.example.com/#fragment',
    'https://arveld.example.com/white space',
  ]) {
    expect(() => installationScript({ ...input, endpoint }, 'v0.1.0-rc.3')).toThrow();
  }
  for (const override of [{ image: 'custom-agent:v9.9.9' }, { version: 'v9.9.9' }]) {
    expect(() => installationScript({ ...input, ...override }, 'v0.1.0-rc.3')).toThrow();
  }
});

test('installation follows the controller release for both methods and rejects unknown versions', () => {
  expect(agentRelease('v0.1.0-rc.3')).toEqual({
    version: 'v0.1.0-rc.3',
    image: 'ghcr.io/rstck-innovation/arveld-agent:v0.1.0-rc.3',
  });
  for (const version of [undefined, 'dev', 'latest', '0.1.0']) {
    expect(agentRelease(version)).toBeNull();
  }
  for (const method of ['docker', 'linux'] as const) {
    for (const version of [
      undefined,
      '',
      'dev',
      'latest',
      'v1.2.3; touch /tmp/bad',
      'v1.2.3\n',
      '../main',
    ]) {
      expect(() => installationScript({ ...input, method }, version)).toThrow();
    }
    const script = installationScript({ ...input, method }, 'v2.3.4');
    expect(script).toContain(
      method === 'linux'
        ? "ARVELD_VERSION='v2.3.4'"
        : 'ghcr.io/rstck-innovation/arveld-agent:v2.3.4',
    );
  }
  expect(() =>
    installationScript({ ...input, method: 'compose' } as never, 'v0.1.0-rc.3'),
  ).toThrow();
});
