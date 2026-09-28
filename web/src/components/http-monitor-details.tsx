import { Box, Tooltip, Typography } from '@mui/material';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import type { Monitor } from '../data/api';
import { type HTTPDetail, httpDetailsOptions } from '../data/monitor-results';
import { formatNumber } from '../i18n/format';
import { ErrorState, Panel } from './ui';

const details: { field: HTTPDetail; label: string; explanation: string }[] = [
  {
    field: 'dns',
    label: 'DNS lookup',
    explanation: 'Last DNS lookup observed during this check; zero when no lookup event occurs.',
  },
  {
    field: 'connection',
    label: 'TCP connection',
    explanation:
      'Last TCP connection attempt observed during this check; zero when the connection is reused.',
  },
  {
    field: 'tls',
    label: 'TLS handshake',
    explanation: 'Last TLS handshake observed during this check; zero when no handshake occurs.',
  },
  {
    field: 'request',
    label: 'Request completion',
    explanation:
      'Time from request start until it is written, including DNS, connection and TLS setup.',
  },
  {
    field: 'headers',
    label: 'Response headers',
    explanation:
      'Time from the first response byte until headers are available. This excludes body transfer.',
  },
  {
    field: 'size',
    label: 'Response body size',
    explanation:
      'Bytes in a fully read, nonempty response body. Empty or incomplete bodies have no size metric.',
  },
  {
    field: 'certificate',
    label: 'Certificate expires in',
    explanation:
      'Remaining lifetime of the response TLS certificate. Failed TLS verification may prevent this metric.',
  },
];

export function HTTPMonitorDetails({ monitor }: { monitor: Monitor }) {
  const { t } = useTranslation();
  const query = useQuery(httpDetailsOptions(monitor));
  return (
    <Panel title={t('HTTP measurements')}>
      <Box sx={{ p: 3 }} aria-busy={query.isPending}>
        {query.isError ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : (
          <>
            <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
              {t(
                'These timings may overlap and reflect the last events when redirects occur. They do not form a transfer waterfall.',
              )}
            </Typography>
            <Box
              component="dl"
              sx={{
                m: 0,
                display: 'grid',
                gap: 3,
                gridTemplateColumns: {
                  xs: 'minmax(0,1fr)',
                  sm: 'repeat(2,minmax(0,1fr))',
                  lg: 'repeat(3,minmax(0,1fr))',
                },
              }}
            >
              {details.map(({ field, label, explanation }) => {
                const value = query.data?.[field];
                return (
                  <div key={field}>
                    <Tooltip title={t(explanation)}>
                      <Typography
                        component="dt"
                        variant="body2"
                        color="text.secondary"
                        tabIndex={0}
                      >
                        {t(label)}
                      </Typography>
                    </Tooltip>
                    <Typography
                      component="dd"
                      variant="h6"
                      sx={{ m: 0, fontVariantNumeric: 'tabular-nums' }}
                    >
                      {value === null || value === undefined
                        ? '—'
                        : field === 'certificate'
                          ? t('{{count}} days', { count: Number((value / 86400).toFixed(1)) })
                          : `${formatNumber(value)} ${field === 'size' ? t('bytes') : 'ms'}`}
                    </Typography>
                  </div>
                );
              })}
            </Box>
          </>
        )}
      </Box>
    </Panel>
  );
}
