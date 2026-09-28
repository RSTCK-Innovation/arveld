import { Alert, MenuItem, Step, StepLabel, Stepper, TextField } from '@mui/material';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ButtonLink } from '../components/links';
import {
  Button,
  download,
  ErrorState,
  FormError,
  Loading,
  PageHeading,
  Panel,
} from '../components/ui';
import { agentRelease, type InstallInput, installationScript } from '../data/installation';
import { readinessOptions } from '../data/instance-health';

export function AgentInstallPage() {
  const { t } = useTranslation();
  const [step, setStep] = useState(0);
  const readiness = useQuery({ ...readinessOptions, refetchInterval: false });
  const release = agentRelease(readiness.data?.version);
  const [input, setInput] = useState<InstallInput>({
    method: 'linux',
    endpoint: window.location.origin,
  });
  const [files, setFiles] = useState<Record<string, string>>({});
  const [error, setError] = useState<Error | null>(null);
  return (
    <>
      <ButtonLink to="/agents" search={{ q: '', status: 'all' }} color="inherit">
        ← {t('Agents')}
      </ButtonLink>
      <PageHeading
        eyebrow={t('CONNECT YOUR INFRASTRUCTURE')}
        title={t('Install agent')}
        description={t('Install an agent with a Linux convenience script or Docker run.')}
        actions={
          <ButtonLink to="/settings/agent-keys" color="inherit">
            {t('Agent keys')}
          </ButtonLink>
        }
      />
      <Panel>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (!release || readiness.isError) return;
            setError(null);
            try {
              if (step === 1) {
                setFiles({
                  [`install-arveld-agent-${input.method}.sh`]: installationScript(
                    input,
                    readiness.data?.version,
                  ),
                });
              }
              setStep(Math.min(step + 1, 2));
            } catch (caught) {
              setError(caught instanceof Error ? caught : new Error(t('Monitor the form.')));
            }
          }}
        >
          <Box
            component="div"
            sx={{ padding: 3, display: 'flex', flexDirection: 'column', gap: 3 }}
          >
            <Stepper activeStep={step} alternativeLabel>
              {['Environment', 'Connection', 'Installation'].map((label) => (
                <Step key={label}>
                  <StepLabel>{t(label)}</StepLabel>
                </Step>
              ))}
            </Stepper>
            {step === 0 && (
              <>
                <TextField
                  fullWidth
                  select
                  label={t('Installation method')}
                  value={input.method}
                  onChange={(e) =>
                    setInput({ ...input, method: e.target.value as InstallInput['method'] })
                  }
                >
                  <MenuItem value="linux">{t('Linux convenience script')}</MenuItem>
                  <MenuItem value="docker">Docker run</MenuItem>
                </TextField>
                {input.method === 'linux' && (
                  <Alert severity="info">
                    {t(
                      'Supports Ubuntu, Debian, AlmaLinux, Rocky Linux, Fedora, Red Hat, Amazon Linux, openSUSE and Arch Linux with systemd, on x86_64 and ARM64. Installs and starts a system service.',
                    )}
                  </Alert>
                )}
              </>
            )}
            {step === 1 && (
              <>
                <TextField
                  fullWidth
                  label={t('Arveld URL')}
                  type="url"
                  helperText={t(
                    'Use the HTTP(S) base URL, for example https://arveld.example.com.',
                  )}
                  required
                  value={input.endpoint}
                  onChange={(e) => setInput({ ...input, endpoint: e.target.value })}
                />
                <Alert severity="info">
                  {t(
                    input.method === 'linux'
                      ? 'Use an address reachable from the Linux host where the agent will run.'
                      : 'Use an address reachable from the agent. localhost refers to the container itself.',
                  )}
                </Alert>
                <Typography component="p" variant="body2">
                  {t(
                    'Before running the script, export your agent key as ARVELD_AGENT_TOKEN in your terminal.',
                  )}
                </Typography>
              </>
            )}
            {step === 2 && (
              <>
                <Alert severity="info">
                  {t(
                    input.method === 'linux'
                      ? 'Run this script on the Linux host you want to connect. It preserves the agent identity in /var/lib/arveld-agent and stores the key in a root-only environment file.'
                      : 'Run this script on the machine you want to connect. It starts the agent and preserves its identity in a Docker volume.',
                  )}
                </Alert>
                <Box component="pre" sx={{ p: 2, overflow: 'auto', bgcolor: 'action.hover' }}>
                  {input.method === 'linux'
                    ? 'sudo --preserve-env=ARVELD_AGENT_TOKEN sh install-arveld-agent-linux.sh'
                    : 'sh install-arveld-agent-docker.sh'}
                </Box>
                {Object.entries(files).map(([path, content]) => (
                  <div key={path}>
                    <Box
                      component="div"
                      sx={{
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'space-between',
                        gap: 2,
                        flexWrap: 'wrap',
                      }}
                    >
                      <Typography component="h3" variant="subtitle1" sx={{ mb: 2 }}>
                        {path}
                      </Typography>
                      <Button
                        onClick={() =>
                          download(
                            path,
                            content,
                            path.endsWith('.sh') ? 'text/x-shellscript' : 'text/yaml',
                          )
                        }
                      >
                        {path.endsWith('.sh') ? t('Download installation script') : t('Download')}
                      </Button>
                    </Box>
                    <Box
                      component="pre"
                      sx={{
                        my: 2,
                        p: 2,
                        maxHeight: 400,
                        overflow: 'auto',
                        whiteSpace: 'pre',
                        bgcolor: 'action.hover',
                      }}
                    >
                      {content}
                    </Box>
                  </div>
                ))}
                <Typography component="p" variant="body2" sx={{ mb: 2 }}>
                  {t('After installation, verify the connection in the agents list.')}
                </Typography>
                <ButtonLink to="/agents" search={{ q: '', status: 'all' }}>
                  {t('View agents')}
                </ButtonLink>
              </>
            )}
            {step < 2 &&
              (readiness.isPending ? (
                <Loading />
              ) : readiness.isError ? (
                <ErrorState error={readiness.error} retry={() => void readiness.refetch()} />
              ) : release ? (
                <Typography variant="body2" color="text.secondary">
                  {t('The agent automatically uses Arveld {{version}}.', {
                    version: release.version,
                  })}
                </Typography>
              ) : (
                <Alert severity="warning">
                  {t('Automatic installation requires an Arveld release version.')}
                </Alert>
              ))}
            <FormError error={error} />
          </Box>
          <Box
            component="div"
            sx={{
              display: 'flex',
              alignItems: 'center',
              flexWrap: 'wrap',
              gap: 2,
              p: 3,
              justifyContent: 'flex-end',
            }}
          >
            {step > 0 && <Button onClick={() => setStep(step - 1)}>{t('Previous')}</Button>}
            {step < 2 && (
              <Button type="submit" variant="primary" disabled={!release || readiness.isError}>
                {step === 0 ? t('Continue') : t('Prepare installation')}
              </Button>
            )}
          </Box>
        </form>
      </Panel>
    </>
  );
}
