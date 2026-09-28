import Server from '@mui/icons-material/DnsOutlined';
import {
  Divider,
  ListItemText,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  ToggleButton,
} from '@mui/material';
import Box from '@mui/material/Box';
import Table from '@mui/material/Table';
import { useQuery } from '@tanstack/react-query';
import { useNavigate, useSearch } from '@tanstack/react-router';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { AppLink, ButtonLink } from '../components/links';
import {
  Badge,
  Button,
  Empty,
  ErrorState,
  Loading,
  PageHeading,
  Panel,
  relativeTime,
  SearchField,
} from '../components/ui';
import { agentMetricOptions } from '../data/agent-metrics';
import { agentsOptions } from '../data/agents';
import { formatPercent } from '../i18n/format';

export function AgentsPage() {
  const { t } = useTranslation();
  const query = useQuery(agentsOptions);
  const search = useSearch({
    from: '/agents',
  });
  const navigate = useNavigate({
    from: '/agents',
  });
  const [page, setPage] = useState(1);
  if (query.isPending) return <Loading />;
  if (query.isError) return <ErrorState error={query.error} retry={() => void query.refetch()} />;
  const agents = query.data;
  const online = agents.filter((a) => a.connected).length;
  const filtered = agents
    .filter(
      (a) =>
        `${a.hostname ?? ''} ${a.instance_uid}`.toLowerCase().includes(search.q.toLowerCase()) &&
        (search.status === 'all' || a.connected === (search.status === 'online')),
    )
    .sort((a, b) => (a.hostname ?? a.instance_uid).localeCompare(b.hostname ?? b.instance_uid));
  const pages = Math.max(1, Math.ceil(filtered.length / 10));
  const current = Math.min(page, pages);
  return (
    <>
      <PageHeading
        eyebrow={t('YOUR INFRASTRUCTURE')}
        title={t('Every machine matters.')}
        description={t('See your agents, their connection status and their last contact.')}
        actions={
          <ButtonLink to="/agents/install" variant="contained" color="primary">
            {t('Install agent')}
          </ButtonLink>
        }
      />
      <Panel>
        <Box
          component="div"
          sx={{
            display: 'flex',
            alignItems: 'center',
            flexWrap: 'wrap',
            gap: 2,
            p: 3,
            justifyContent: 'space-between',
          }}
        >
          <Box
            component="fieldset"
            className="filter-tabs"
            aria-label={t('Filter agents')}
            sx={{
              display: 'flex',
              alignItems: 'center',
              flexWrap: 'wrap',
              gap: 1,
              border: 0,
              p: 0,
              m: 0,
              minWidth: 0,
            }}
          >
            {[
              ['all', t('All'), agents.length],
              ['online', t('Connected (plural)'), online],
              ['offline', t('Offline'), agents.length - online],
            ].map(([value, label, count]) => (
              <ToggleButton
                type="button"
                key={value}
                selected={search.status === value}
                value={value}
                onClick={() => {
                  setPage(1);
                  void navigate({
                    search: (prev) => ({
                      ...prev,
                      status: String(value),
                    }),
                  });
                }}
              >
                {label}
                <Box component="span" className="filter-count" sx={{ ml: 1 }}>
                  {count}
                </Box>
              </ToggleButton>
            ))}
          </Box>
          <Box
            component="div"
            sx={{
              display: 'flex',
              alignItems: 'center',
              flexWrap: 'wrap',
              gap: 2,
              width: { xs: '100%', sm: 'auto' },
            }}
          >
            <SearchField
              value={search.q}
              onChange={(q) => {
                setPage(1);
                void navigate({
                  search: (prev) => ({
                    ...prev,
                    q,
                  }),
                  replace: true,
                });
              }}
              placeholder={t('Search for an agent…')}
            />
          </Box>
        </Box>
        <Divider />
        {filtered.length ? (
          <>
            <Box component="div" sx={{ width: '100%', overflowX: 'auto' }}>
              <Table>
                <TableHead>
                  <TableRow>
                    <TableCell>{t('Agent')}</TableCell>
                    <TableCell>{t('Status')}</TableCell>
                    <TableCell>{t('CPU')}</TableCell>
                    <TableCell>{t('Memory')}</TableCell>
                    <TableCell>{t('Version')}</TableCell>
                    <TableCell>{t('Last seen')}</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {filtered.slice((current - 1) * 10, current * 10).map((a) => (
                    <TableRow key={a.instance_uid}>
                      <TableCell>
                        <AppLink
                          to="/agents/$agentId"
                          params={{ agentId: a.instance_uid }}
                          sx={{ display: 'flex', alignItems: 'center', gap: 2, minWidth: 200 }}
                        >
                          <Box component="span" sx={{ display: 'inline-flex' }}>
                            <Server />
                          </Box>
                          <ListItemText
                            primary={a.hostname ?? a.instance_uid}
                            secondary={a.hostname ? a.instance_uid : undefined}
                            sx={{ overflowWrap: 'anywhere' }}
                          />
                        </AppLink>
                      </TableCell>
                      <TableCell>
                        <Badge tone={a.connected ? 'good' : 'neutral'}>
                          {a.connected ? t('Connected') : t('Offline')}
                        </Badge>
                      </TableCell>
                      <AgentMetricCell metric="cpu" instanceUID={a.instance_uid} />
                      <AgentMetricCell metric="memory" instanceUID={a.instance_uid} />
                      <TableCell>
                        {a.version ? <code>{a.version}</code> : t('Unavailable')}
                      </TableCell>
                      <TableCell>
                        {a.last_seen_at ? relativeTime(a.last_seen_at) : t('Unavailable')}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </Box>
            <Box
              component="div"
              sx={{
                display: 'flex',
                alignItems: 'center',
                flexWrap: 'wrap',
                gap: 2,
                p: 2,
                justifyContent: 'space-between',
              }}
            >
              <span>
                {t('{{count}} agent', {
                  count: filtered.length,
                })}
              </span>
              <Box
                component="div"
                sx={{ display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: 2 }}
              >
                <Button disabled={current === 1} onClick={() => setPage(current - 1)}>
                  {t('Previous')}
                </Button>
                <span>
                  {current} / {pages}
                </span>
                <Button disabled={current === pages} onClick={() => setPage(current + 1)}>
                  {t('Next')}
                </Button>
              </Box>
            </Box>
          </>
        ) : agents.length ? (
          <Empty
            title={t('No agents found')}
            description={t('Try a different hostname, identifier or filter.')}
            action={
              <Button
                onClick={() => {
                  setPage(1);
                  void navigate({
                    search: {
                      q: '',
                      status: 'all',
                    },
                  });
                }}
              >
                {t('Clear filters')}
              </Button>
            }
          />
        ) : (
          <Empty
            title={t('No agents connected yet')}
            description={t(
              'Install an agent with an Agent Key. It will appear here when it connects.',
            )}
            action={
              <ButtonLink to="/agents/install" variant="contained" color="primary">
                {t('Install agent')}
              </ButtonLink>
            }
          />
        )}
      </Panel>
    </>
  );
}

function AgentMetricCell({
  metric,
  instanceUID,
}: {
  metric: 'cpu' | 'memory';
  instanceUID: string;
}) {
  const { t } = useTranslation();
  const query = useQuery(agentMetricOptions(metric, instanceUID));
  return (
    <TableCell aria-busy={query.isPending}>
      {query.isError ? (
        <Button onClick={() => void query.refetch()}>{t('Retry')}</Button>
      ) : query.isPending ? (
        t('Loading data')
      ) : query.data === null ? (
        t('Unavailable')
      ) : (
        formatPercent(query.data.value)
      )}
    </TableCell>
  );
}
