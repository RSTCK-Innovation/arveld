import { Alert, MenuItem, Step, StepLabel, Stepper, TextField } from '@mui/material';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { agentsOptions } from '../data/agents';
import { api, type Monitor, type MonitorInput } from '../data/api';
import { monitorOptions, monitorsOptions } from '../data/monitors';
import { useSubmit } from '../data/use-submit';
import { HTTPMonitorSettings, HTTPOptionsSummary } from './http-monitor-settings';
import { ButtonLink } from './links';
import { Button, ErrorState, FormError, Loading } from './ui';

export function MonitorForm({ monitor }: { monitor?: Monitor }) {
  const query = useQuery(agentsOptions);
  if (query.isPending) return <Loading />;
  if (query.isError) return <ErrorState error={query.error} retry={() => void query.refetch()} />;
  return <MonitorFields key={monitor?.id ?? 'new'} agents={query.data} monitor={monitor} />;
}

function MonitorFields({
  agents,
  monitor,
}: {
  agents: Awaited<ReturnType<typeof api.listAgents>>;
  monitor?: Monitor;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const client = useQueryClient();
  const action = useSubmit();
  const [step, setStep] = useState(0);
  const [input, setInput] = useState<MonitorInput>(() => {
    if (monitor) {
      const { id: _id, ...definition } = monitor;
      return definition;
    }
    return {
      protocol: 'http',
      name: '',
      endpoint: '',
      method: 'GET',
      interval_seconds: 60,
      timeout_seconds: 5,
      agent_instance_uid: agents[0]?.instance_uid ?? '',
    };
  });
  const selectedAgent = agents.find((agent) => agent.instance_uid === input.agent_instance_uid);
  function selectProtocol(protocol: string) {
    const common = {
      name: input.name,
      endpoint: '',
      agent_instance_uid: input.agent_instance_uid,
      interval_seconds: input.interval_seconds,
      timeout_seconds: input.timeout_seconds,
    };
    switch (protocol) {
      case 'http':
        setInput({ ...common, protocol, method: 'GET' });
        break;
      case 'tcp':
        setInput({ ...common, protocol });
        break;
      case 'icmp':
        setInput({ ...common, protocol, ping_count: Math.min(3, common.timeout_seconds) });
        break;
      case 'dns':
        setInput({
          ...common,
          protocol,
          dns_server: '1.1.1.1:53',
          record_type: 'A',
          transport: 'udp',
        });
        break;
    }
  }
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        if (step < 2) setStep(step + 1);
        else
          void action.submit(async () => {
            const saved = monitor
              ? await api.updateMonitor(monitor.id, input)
              : await api.createMonitor(input);
            await client.cancelQueries({ queryKey: monitorOptions(saved.id).queryKey });
            client.setQueryData(monitorOptions(saved.id).queryKey, saved);
            void client.invalidateQueries({ queryKey: monitorsOptions.queryKey });
            for (const uid of new Set([saved.agent_instance_uid, monitor?.agent_instance_uid])) {
              if (uid) {
                void client.invalidateQueries({ queryKey: ['agent-config', uid] });
                void client.invalidateQueries({ queryKey: ['agent-config-revisions', uid] });
              }
            }
            await navigate({ to: '/monitors/$monitorId', params: { monitorId: saved.id } });
          });
      }}
    >
      <Box
        component="fieldset"
        disabled={action.pending}
        sx={{ border: 0, m: 0, p: 3, minWidth: 0, display: 'grid', gap: 3 }}
      >
        <Stepper activeStep={step} alternativeLabel>
          {['Service', 'Settings', 'Review summary'].map((label) => (
            <Step key={label}>
              <StepLabel>{t(label)}</StepLabel>
            </Step>
          ))}
        </Stepper>
        {!selectedAgent && (
          <Alert severity="warning">
            {t('Add an agent before creating a monitor.')}{' '}
            <ButtonLink to="/agents/install">{t('Add an agent')}</ButtonLink>
          </Alert>
        )}
        {step === 0 && (
          <>
            <TextField
              fullWidth
              label={t('Monitor name')}
              required
              value={input.name}
              onChange={(e) => setInput({ ...input, name: e.target.value })}
            />
            <TextField
              fullWidth
              select
              label={t('Protocol')}
              disabled={!!monitor}
              helperText={monitor ? t('The protocol cannot be changed after creation.') : undefined}
              value={input.protocol}
              onChange={(event) => selectProtocol(event.target.value)}
            >
              {['http', 'tcp', 'icmp', 'dns'].map((protocol) => (
                <MenuItem key={protocol} value={protocol}>
                  {protocol.toUpperCase()}
                </MenuItem>
              ))}
            </TextField>
            <TextField
              fullWidth
              type={input.protocol === 'http' ? 'url' : 'text'}
              label={t('Target')}
              required
              value={input.endpoint}
              placeholder={
                input.protocol === 'http'
                  ? 'https://service.example.com/health'
                  : input.protocol === 'tcp'
                    ? 'service.example.com:443'
                    : input.protocol === 'icmp'
                      ? 'router.example.com'
                      : 'example.com'
              }
              onChange={(e) => setInput({ ...input, endpoint: e.target.value })}
            />
            <TextField
              fullWidth
              select
              required
              label={t('Executor agent')}
              value={input.agent_instance_uid}
              onChange={(e) => setInput({ ...input, agent_instance_uid: e.target.value })}
            >
              {agents.map((agent) => (
                <MenuItem
                  key={agent.instance_uid}
                  value={agent.instance_uid}
                  sx={{ whiteSpace: 'normal', overflowWrap: 'anywhere' }}
                >
                  {agent.hostname ?? agent.instance_uid}
                  {!agent.connected ? t(' (offline)') : ''}
                </MenuItem>
              ))}
            </TextField>
            {selectedAgent && !selectedAgent.connected && (
              <Alert severity="info">
                {t('This agent is offline. The Monitor will be delivered when it reconnects.')}
              </Alert>
            )}
          </>
        )}
        {step === 1 && (
          <>
            <Box
              sx={{
                display: 'grid',
                gap: 3,
                gridTemplateColumns: { xs: 'minmax(0,1fr)', sm: 'repeat(2,minmax(0,1fr))' },
              }}
            >
              <TextField
                fullWidth
                type="number"
                label={t('Interval (seconds)')}
                required
                value={input.interval_seconds}
                onChange={(e) => setInput({ ...input, interval_seconds: +e.target.value })}
                slotProps={{ htmlInput: { min: 10, max: 3600, step: 1 } }}
              />
              <TextField
                fullWidth
                type="number"
                label={t('Timeout (seconds)')}
                required
                value={input.timeout_seconds}
                onChange={(e) => setInput({ ...input, timeout_seconds: +e.target.value })}
                slotProps={{
                  htmlInput: { min: 1, max: Math.min(60, input.interval_seconds), step: 1 },
                }}
              />
            </Box>
            {input.protocol === 'http' && <HTTPMonitorSettings input={input} onChange={setInput} />}
            {input.protocol === 'icmp' && (
              <TextField
                fullWidth
                type="number"
                required
                label={t('Ping count')}
                value={input.ping_count}
                onChange={(event) => setInput({ ...input, ping_count: +event.target.value })}
                slotProps={{
                  htmlInput: { min: 1, max: Math.min(10, input.timeout_seconds), step: 1 },
                }}
                helperText={t('One ping per second. The count must not exceed the timeout.')}
              />
            )}
            {input.protocol === 'dns' && (
              <>
                <TextField
                  fullWidth
                  required
                  label={t('DNS server')}
                  value={input.dns_server}
                  placeholder="1.1.1.1:53"
                  onChange={(event) => setInput({ ...input, dns_server: event.target.value })}
                />
                <TextField
                  fullWidth
                  select
                  label={t('Record type')}
                  value={input.record_type}
                  onChange={(event) =>
                    setInput({
                      ...input,
                      record_type: event.target.value as typeof input.record_type,
                    })
                  }
                >
                  {['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS'].map((type) => (
                    <MenuItem key={type} value={type}>
                      {type}
                    </MenuItem>
                  ))}
                </TextField>
                <TextField
                  fullWidth
                  select
                  label={t('DNS transport')}
                  value={input.transport}
                  onChange={(event) =>
                    setInput({ ...input, transport: event.target.value as typeof input.transport })
                  }
                >
                  {['udp', 'tcp', 'tcp-tls'].map((transport) => (
                    <MenuItem key={transport} value={transport}>
                      {transport.toUpperCase()}
                    </MenuItem>
                  ))}
                </TextField>
              </>
            )}
          </>
        )}
        {step === 2 && (
          <>
            <Typography component="h2" variant="h6">
              {input.name}
            </Typography>
            <Box
              component="dl"
              sx={{
                display: 'grid',
                gap: 3,
                gridTemplateColumns: { xs: 'minmax(0,1fr)', sm: 'repeat(2,minmax(0,1fr))' },
                m: 0,
                overflowWrap: 'anywhere',
              }}
            >
              {[
                [t('Target'), input.endpoint],
                [t('Protocol'), input.protocol.toUpperCase()],
                [t('Executor agent'), selectedAgent?.hostname ?? input.agent_instance_uid],
                ...(input.protocol === 'http' ? [[t('HTTP method'), input.method]] : []),
                ...(input.protocol === 'icmp' ? [[t('Ping count'), input.ping_count]] : []),
                ...(input.protocol === 'dns'
                  ? [
                      [t('DNS server'), input.dns_server],
                      [t('Record type'), input.record_type],
                      [t('DNS transport'), input.transport],
                    ]
                  : []),
                [t('Interval (seconds)'), input.interval_seconds],
                [t('Timeout (seconds)'), input.timeout_seconds],
              ].map(([label, value]) => (
                <div key={label}>
                  <Typography component="dt" variant="body2" color="text.secondary">
                    {label}
                  </Typography>
                  <Typography component="dd" variant="body2" sx={{ m: 0 }}>
                    {value}
                  </Typography>
                </div>
              ))}
              {input.protocol === 'http' && <HTTPOptionsSummary monitor={input} />}
            </Box>
          </>
        )}
        <FormError error={action.error} />
        <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 2, justifyContent: 'flex-end' }}>
          {monitor ? (
            <ButtonLink
              to="/monitors/$monitorId"
              params={{ monitorId: monitor.id }}
              disabled={action.pending}
            >
              {t('Cancel')}
            </ButtonLink>
          ) : (
            <ButtonLink
              to="/monitors"
              search={{ q: '', status: 'all', create: false }}
              disabled={action.pending}
            >
              {t('Cancel')}
            </ButtonLink>
          )}
          {step > 0 && <Button onClick={() => setStep(step - 1)}>{t('Previous')}</Button>}
          <Button type="submit" variant="primary" busy={action.pending} disabled={!selectedAgent}>
            {step < 2 ? t('Continue') : t('Save monitor')}
          </Button>
        </Box>
      </Box>
    </form>
  );
}
