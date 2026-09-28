import { z } from 'zod';
import linuxInstaller from '../../../scripts/install-linux.sh?raw';

const releaseVersion = z
  .string()
  .regex(/^v\d+\.\d+\.\d+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$/)
  .refine((value) => !/\s/.test(value));
const endpoint = z
  .url()
  .refine((value) => {
    const url = new URL(value);
    return ['http:', 'https:'].includes(url.protocol) && !/[\s@?#]/.test(value);
  }, 'Use an http:// or https:// URL without credentials, whitespace, a query or a fragment.')
  .transform((value) => value.replace(/^https?:/i, (scheme) => scheme.toLowerCase()));
const inputSchema = z.object({ method: z.enum(['docker', 'linux']), endpoint }).strict();
export type InstallInput = z.infer<typeof inputSchema>;

export function agentRelease(version: string | undefined) {
  const parsed = releaseVersion.safeParse(version);
  if (!parsed.success) return null;
  return { version: parsed.data, image: `ghcr.io/rstck-innovation/arveld-agent:${parsed.data}` };
}

// Single quotes keep URL characters literal; escape embedded quotes by closing and reopening them.
function shellQuote(value: string) {
  return `'${value.replaceAll("'", "'\\''")}'`;
}

export function installationScript(input: InstallInput, controllerVersion: string | undefined) {
  const value = inputSchema.parse(input);
  const release = agentRelease(controllerVersion);
  if (!release) throw new Error('Automatic installation requires an Arveld release version.');
  const header = [
    '#!/bin/sh',
    'set -eu',
    '',
    // biome-ignore lint/suspicious/noTemplateCurlyInString: Expanded by the generated shell script.
    'if [ -z "${ARVELD_AGENT_TOKEN:-}" ]; then',
    "  printf '%s\\n' 'ARVELD_AGENT_TOKEN is required' >&2",
    '  exit 1',
    'fi',
    '',
    `export ARVELD_URL=${shellQuote(value.endpoint)}`,
    '',
  ].join('\n');
  if (value.method === 'linux') {
    return (
      header +
      `export ARVELD_INSTALL_COMPONENT=arveld-agent\nexport ARVELD_VERSION=${shellQuote(release.version)}\n\n` +
      linuxInstaller.replace(/^#![^\n]*\n/, '')
    );
  }
  return (
    header +
    [
      'docker run --detach',
      '  --name arveld-agent',
      '  --restart unless-stopped',
      '  --add-host host.docker.internal:host-gateway',
      '  --env ARVELD_AGENT_TOKEN',
      '  --env ARVELD_URL',
      '  --volume arveld-agent-data:/var/lib/arveld-agent',
      '  --volume /:/hostfs:ro',
      `  ${shellQuote(release.image)}`,
    ].join(' \\\n') +
    '\n'
  );
}
