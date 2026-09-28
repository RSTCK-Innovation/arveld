import CheckCircle from '@mui/icons-material/CheckCircleOutlined';
import ErrorOutline from '@mui/icons-material/ErrorOutlined';
import Refresh from '@mui/icons-material/Refresh';
import Sync from '@mui/icons-material/Sync';
import { Alert, Divider, IconButton, Paper, Tooltip } from '@mui/material';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { agentConfigOptions } from '../data/agents';
import type { ConfigApplyStatus } from '../data/api';
import { formatDate } from '../i18n/format';
import { AgentConfigHistory } from './agent-config-history';
import { Badge, Empty, ErrorState, Loading, Panel, relativeTime } from './ui';

const states = {
  applying: {
    title: 'Applying configuration',
    icon: Sync,
    color: 'info',
    label: 'Applying',
    tone: 'info',
    description: 'Arveld is waiting for confirmation of the desired revision.',
  },
  applied: {
    title: 'Configuration up to date',
    icon: CheckCircle,
    color: 'success',
    label: 'Applied',
    tone: 'good',
    description: 'The agent confirmed the desired revision.',
  },
  failed: {
    title: 'Configuration failed',
    icon: ErrorOutline,
    color: 'error',
    label: 'Failed',
    tone: 'danger',
    description: 'The agent could not apply the desired revision.',
  },
} as const satisfies Record<
  ConfigApplyStatus,
  {
    title: string;
    icon: typeof Sync;
    color: string;
    label: string;
    tone: string;
    description: string;
  }
>;

export function AgentConfiguration({ instanceUID }: { instanceUID: string }) {
  const { t } = useTranslation();
  const query = useQuery(agentConfigOptions(instanceUID));
  if (query.isPending) return <Loading />;
  if (query.isError) return <ErrorState error={query.error} retry={() => void query.refetch()} />;
  if (query.data === null)
    return (
      <Panel title={t('Agent configuration')}>
        <Empty
          title={t('No configuration assigned')}
          description={t('Arveld has not assigned a configuration to this agent yet.')}
        />
      </Panel>
    );

  const { state, desired, reported, last_failure } = query.data;
  const current = states[state];
  const failure = last_failure ?? (reported?.status === 'failed' ? reported : null);
  const failureDetails = failure && (
    <Box sx={{ overflowWrap: 'anywhere' }}>
      <Typography variant="body2" color="text.secondary">
        {failure.revision === null
          ? t('Unknown revision')
          : t('Revision {{revision}}', { revision: failure.revision })}
        {' · '}
        <time dateTime={failure.reported_at}>
          {formatDate(failure.reported_at, { dateStyle: 'medium', timeStyle: 'short' })}
        </time>
      </Typography>
      {failure.error_message && (
        <Typography variant="body2" sx={{ mt: 1, whiteSpace: 'pre-wrap' }}>
          {failure.error_message}
        </Typography>
      )}
    </Box>
  );
  return (
    <Box sx={{ display: 'grid', gap: 3, minWidth: 0 }}>
      <Paper
        component="section"
        aria-label={t('Configuration status')}
        variant="outlined"
        sx={{ minWidth: 0 }}
      >
        <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 2, p: 3 }}>
          <current.icon sx={{ color: `${current.color}.main`, fontSize: 28, mt: 0.25 }} />
          <Box sx={{ flex: 1, minWidth: 0 }}>
            <Typography component="h2" variant="h6">
              {t(current.title)}
            </Typography>
            <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
              {t(current.description)}
            </Typography>
          </Box>
          <Tooltip title={t('Refresh')}>
            <IconButton
              aria-label={t('Refresh')}
              loading={query.isFetching}
              onClick={() => void query.refetch()}
              size="small"
            >
              <Refresh fontSize="small" />
            </IconButton>
          </Tooltip>
        </Box>
        <Divider />
        <Box
          component="dl"
          sx={{
            display: 'grid',
            gridTemplateColumns: { xs: 'minmax(0,1fr)', sm: 'repeat(3,minmax(0,1fr))' },
            gap: 3,
            m: 0,
            p: 3,
          }}
        >
          <div>
            <Typography component="dt" variant="body2" color="text.secondary">
              {t('Requested revision')}
            </Typography>
            <Typography component="dd" variant="subtitle1" sx={{ m: 0, mt: 1, fontWeight: 600 }}>
              {t('Revision {{revision}}', { revision: desired.revision })}
            </Typography>
          </div>
          <div>
            <Typography component="dt" variant="body2" color="text.secondary">
              {t('Reported revision')}
            </Typography>
            <Box component="dd" sx={{ m: 0, mt: 1 }}>
              <Typography component="p" variant="subtitle1" sx={{ fontWeight: 600 }}>
                {!reported
                  ? '—'
                  : reported.revision === null
                    ? t('Unknown revision')
                    : t('Revision {{revision}}', { revision: reported.revision })}
              </Typography>
              {reported && state !== reported.status && (
                <Box sx={{ mt: 1 }}>
                  <Badge tone={states[reported.status].tone}>
                    {t(states[reported.status].label)}
                  </Badge>
                </Box>
              )}
            </Box>
          </div>
          <div>
            <Typography component="dt" variant="body2" color="text.secondary">
              {t('Last report')}
            </Typography>
            <Box component="dd" sx={{ m: 0, mt: 1 }}>
              {reported ? (
                <>
                  <Typography component="p" variant="subtitle1" sx={{ fontWeight: 600 }}>
                    {relativeTime(reported.reported_at)}
                  </Typography>
                  <Typography
                    component="time"
                    dateTime={reported.reported_at}
                    variant="caption"
                    color="text.secondary"
                  >
                    {formatDate(reported.reported_at, { dateStyle: 'medium', timeStyle: 'short' })}
                  </Typography>
                </>
              ) : (
                <Typography variant="body2" color="text.secondary">
                  {t('Waiting for the first agent report.')}
                </Typography>
              )}
            </Box>
          </div>
        </Box>
        {failure &&
          (state === 'failed' ? (
            <Alert severity="error" sx={{ mx: 3, mb: 3 }}>
              {failureDetails}
            </Alert>
          ) : (
            <Box
              component="details"
              key={failure.reported_at}
              sx={{ borderTop: 1, borderColor: 'divider' }}
            >
              <Box component="summary" sx={{ cursor: 'pointer', px: 3, py: 2 }}>
                <Typography component="span" variant="body2" sx={{ fontWeight: 600 }}>
                  {t('Last failure')}
                </Typography>
              </Box>
              <Box sx={{ px: 3, pb: 3 }}>{failureDetails}</Box>
            </Box>
          ))}
      </Paper>
      <AgentConfigHistory instanceUID={instanceUID} desiredRevision={desired.revision} />
    </Box>
  );
}
