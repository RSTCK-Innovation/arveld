import Copy from '@mui/icons-material/ContentCopyOutlined';
import { Alert, Box, Button, List, ListItemButton, Paper, Typography } from '@mui/material';
import { useQuery } from '@tanstack/react-query';
import Prism from 'prismjs';
import 'prismjs/components/prism-yaml';
import { type ReactNode, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api } from '../data/api';
import { formatDate } from '../i18n/format';
import { Badge, Empty, ErrorState, Loading } from './ui';

export function AgentConfigHistory({
  instanceUID,
  desiredRevision,
}: {
  instanceUID: string;
  desiredRevision: number;
}) {
  const { t } = useTranslation();
  const [selected, setSelected] = useState<number | null>(null);
  const history = useQuery({
    queryKey: ['agent-config-revisions', instanceUID],
    queryFn: ({ signal }) => api.agentConfigRevisions(instanceUID, signal),
    retry: false,
    refetchInterval: 10_000,
  });
  if (history.isPending) return <Loading />;
  if (history.isError)
    return <ErrorState error={history.error} retry={() => void history.refetch()} />;
  if (history.data.length === 0)
    return (
      <Empty
        title={t('No configuration revisions')}
        description={t('Arveld has not assigned a configuration to this agent yet.')}
      />
    );
  const active =
    history.data.find((revision) => revision.revision === (selected ?? desiredRevision)) ??
    history.data[0];
  return (
    <Paper
      variant="outlined"
      sx={{
        display: 'grid',
        gridTemplateColumns: { xs: 'minmax(0,1fr)', md: '280px minmax(0,1fr)' },
        overflow: 'hidden',
        minWidth: 0,
      }}
    >
      <Box
        component="nav"
        aria-label={t('Configuration revisions')}
        sx={(theme) => ({
          minWidth: 0,
          borderRight: { md: `1px solid ${theme.palette.divider}` },
          borderBottom: { xs: `1px solid ${theme.palette.divider}`, md: 0 },
          bgcolor: 'background.default',
        })}
      >
        <Typography component="h2" variant="subtitle2" sx={{ p: 2.5 }}>
          {t('Revision history')}
        </Typography>
        <List
          component="div"
          disablePadding
          sx={{
            display: 'flex',
            flexDirection: { xs: 'row', md: 'column' },
            overflow: 'auto',
            maxHeight: { md: '70vh' },
            px: 1,
            pb: 1,
            gap: 0.5,
          }}
        >
          {history.data.map((revision) => (
            <ListItemButton
              component="button"
              type="button"
              key={revision.revision}
              selected={active.revision === revision.revision}
              aria-current={active.revision === revision.revision ? 'true' : undefined}
              aria-label={t('Revision {{revision}}', { revision: revision.revision })}
              onClick={() => setSelected(revision.revision)}
              sx={{
                display: 'block',
                textAlign: 'left',
                borderRadius: 1,
                flexShrink: 0,
                minWidth: { xs: 190, md: 0 },
                width: { md: '100%' },
                mx: 0,
                px: 1.5,
                py: 1.5,
              }}
            >
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 0.75 }}>
                <Typography component="span" variant="body2" sx={{ fontWeight: 600 }}>
                  {t('Revision {{revision}}', { revision: revision.revision })}
                </Typography>
                {revision.revision === desiredRevision && (
                  <Badge tone="info">{t('Requested')}</Badge>
                )}
              </Box>
              <Typography
                component={revision.created_at ? 'time' : 'span'}
                dateTime={revision.created_at ?? undefined}
                variant="caption"
                color="text.secondary"
              >
                {revision.created_at
                  ? formatDate(revision.created_at, { dateStyle: 'medium', timeStyle: 'short' })
                  : t('Date unavailable')}
              </Typography>
            </ListItemButton>
          ))}
        </List>
      </Box>
      <RevisionYAML
        key={`${instanceUID}:${active.revision}`}
        instanceUID={instanceUID}
        revision={active.revision}
        createdAt={active.created_at}
      />
    </Paper>
  );
}

function RevisionYAML({
  instanceUID,
  revision,
  createdAt,
}: {
  instanceUID: string;
  revision: number;
  createdAt: string | null;
}) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);
  const [copyError, setCopyError] = useState(false);
  const yaml = useQuery({
    queryKey: ['agent-config-yaml', instanceUID, revision],
    queryFn: ({ signal }) => api.agentConfigYAML(instanceUID, revision, signal),
    retry: false,
    staleTime: Infinity,
  });
  const highlighted = useMemo(
    () =>
      yaml.data === undefined ? null : tokenNodes(Prism.tokenize(yaml.data, Prism.languages.yaml)),
    [yaml.data],
  );
  async function copy() {
    if (yaml.data === undefined) return;
    setCopyError(false);
    try {
      await navigator.clipboard.writeText(yaml.data);
      setCopied(true);
    } catch {
      setCopyError(true);
    }
  }
  return (
    <Box sx={{ minWidth: 0 }}>
      <Box
        sx={{
          p: 2.5,
          borderBottom: 1,
          borderColor: 'divider',
          display: 'flex',
          alignItems: 'center',
          gap: 2,
          flexWrap: 'wrap',
        }}
      >
        <Box sx={{ flex: 1 }}>
          <Typography component="h2" variant="subtitle2">
            collector.yaml
          </Typography>
          <Typography variant="caption" color="text.secondary">
            {t('Revision {{revision}}', { revision })}
            {' · '}
            {createdAt
              ? formatDate(createdAt, { dateStyle: 'medium', timeStyle: 'short' })
              : t('Date unavailable')}
          </Typography>
        </Box>
        <Button
          size="small"
          startIcon={<Copy />}
          disabled={yaml.isPending || yaml.isError}
          onClick={() => void copy()}
        >
          {copied ? t('Copied') : t('Copy YAML')}
        </Button>
      </Box>
      {copyError && (
        <Alert severity="error" sx={{ m: 2 }}>
          {t('Unable to copy. Select and copy the text manually.')}
        </Alert>
      )}
      {yaml.isPending ? (
        <Loading />
      ) : yaml.isError ? (
        <ErrorState error={yaml.error} retry={() => void yaml.refetch()} />
      ) : (
        <Box
          component="pre"
          role="region"
          aria-label={t('YAML configuration')}
          tabIndex={0}
          sx={(theme) => ({
            m: 0,
            p: 3,
            overflow: 'auto',
            maxHeight: '70vh',
            minHeight: 420,
            fontSize: 13,
            lineHeight: 1.75,
            tabSize: 2,
            bgcolor: 'background.paper',
            color: 'text.primary',
            fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
            '& .token.comment': {
              color: theme.palette.mode === 'dark' ? '#94a3b8' : '#64748b',
              fontStyle: 'italic',
            },
            '& .token.key, & .token.atrule': {
              color: theme.palette.mode === 'dark' ? '#93c5fd' : '#075985',
            },
            '& .token.string, & .token.scalar': {
              color: theme.palette.mode === 'dark' ? '#a7d9a0' : '#27632b',
            },
            '& .token.number, & .token.datetime': {
              color: theme.palette.mode === 'dark' ? '#fdba74' : '#a33b10',
            },
            '& .token.boolean, & .token.null, & .token.important, & .token.tag': {
              color: theme.palette.mode === 'dark' ? '#d8b4fe' : '#7e22ce',
            },
            '& .token.punctuation': { color: 'text.secondary' },
          })}
        >
          <code className="language-yaml">{highlighted}</code>
        </Box>
      )}
    </Box>
  );
}

// Render Prism tokens as React text nodes so YAML is never interpreted as HTML.
function tokenNodes(tokens: Array<string | Prism.Token>): ReactNode[] {
  let offset = 0;
  return tokens.map((token) => {
    const start = offset;
    offset += token.length;
    if (typeof token === 'string') return token;
    const aliases = Array.isArray(token.alias) ? token.alias.join(' ') : (token.alias ?? '');
    return (
      <span key={start} className={`token ${token.type} ${aliases}`}>
        {tokenNodes(Array.isArray(token.content) ? token.content : [token.content])}
      </span>
    );
  });
}
