import { MenuItem, TextField } from '@mui/material';
import Box from '@mui/material/Box';
import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ButtonLink } from '../components/links';
import { NotificationSilences } from '../components/notification-silences';
import { Empty, ErrorState, Loading, PageHeading, Panel } from '../components/ui';
import { agentsOptions } from '../data/agents';
import { monitorsOptions } from '../data/monitors';

export function MaintenancePage() {
  const { t } = useTranslation();
  const monitors = useQuery(monitorsOptions);
  const agents = useQuery(agentsOptions);
  const [scope, setScope] = useState<'monitor' | 'agent'>('monitor');
  const [ownerId, setOwnerId] = useState('');
  if (monitors.isPending || agents.isPending) return <Loading />;
  if (monitors.isError)
    return <ErrorState error={monitors.error} retry={() => void monitors.refetch()} />;
  if (agents.isError)
    return <ErrorState error={agents.error} retry={() => void agents.refetch()} />;
  const owners =
    scope === 'agent'
      ? agents.data.map((agent) => ({
          id: agent.instance_uid,
          name: agent.hostname ?? agent.instance_uid,
        }))
      : monitors.data;
  const selected = owners.find((owner) => owner.id === ownerId);
  return (
    <>
      <PageHeading
        eyebrow={t('PLAN AHEAD FOR DOWNTIME')}
        title={t('Maintenance')}
        description={t(
          'View immediate and scheduled notification silences for your Agents and Monitors.',
        )}
      />
      <Panel title={t('Create silence')}>
        <Box sx={{ p: 3, display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 2 }}>
          <TextField
            select
            label={t('Target')}
            value={scope}
            onChange={(event) => {
              setScope(event.target.value === 'agent' ? 'agent' : 'monitor');
              setOwnerId('');
            }}
            sx={{ flex: '0 1 180px', minWidth: 0 }}
          >
            <MenuItem value="monitor">{t('Monitor')}</MenuItem>
            <MenuItem value="agent">{t('Agent')}</MenuItem>
          </TextField>
          {owners.length > 0 && (
            <>
              <TextField
                select
                label={t(scope === 'agent' ? 'Agent' : 'Monitor')}
                value={selected?.id ?? ''}
                onChange={(event) => setOwnerId(event.target.value)}
                sx={{ flex: '1 1 240px', minWidth: 0 }}
              >
                {owners.map((owner) => (
                  <MenuItem key={owner.id} value={owner.id}>
                    {owner.name}
                  </MenuItem>
                ))}
              </TextField>
              {selected &&
                (scope === 'agent' ? (
                  <ButtonLink
                    to="/agents/$agentId/silences/new"
                    params={{ agentId: selected.id }}
                    variant="contained"
                  >
                    {t('Create silence')}
                  </ButtonLink>
                ) : (
                  <ButtonLink
                    to="/monitors/$monitorId/silences/new"
                    params={{ monitorId: selected.id }}
                    variant="contained"
                  >
                    {t('Create silence')}
                  </ButtonLink>
                ))}
            </>
          )}
        </Box>
        {owners.length === 0 && (
          <Empty
            title={t(scope === 'agent' ? 'No agents found' : 'No Monitors')}
            description={t(
              scope === 'agent'
                ? 'Connect an Agent to create an Agent silence.'
                : 'Create a Monitor to schedule a Monitor silence.',
            )}
          />
        )}
      </Panel>
      <NotificationSilences monitors={monitors.data} agents={agents.data} />
    </>
  );
}
