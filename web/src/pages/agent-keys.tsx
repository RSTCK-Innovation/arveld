import { Box, TextField } from '@mui/material';
import { queryOptions, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { KeySecret } from '../components/key-secret';
import { useNotify } from '../components/notifications';
import { RecordsTable } from '../components/records-table';
import { SettingsNavigation } from '../components/settings-navigation';
import {
  Badge,
  Button,
  Confirm,
  Empty,
  ErrorState,
  FormError,
  Loading,
  Modal,
  PageHeading,
  Panel,
} from '../components/ui';
import { type AgentKey, api } from '../data/api';
import { useSubmit } from '../data/use-submit';
import { formatDate } from '../i18n/format';

const agentKeysOptions = queryOptions({
  queryKey: ['agentkeys'],
  queryFn: ({ signal }) => api.listAgentKeys(signal),
  retry: false,
  refetchOnWindowFocus: true,
  refetchInterval: 60_000,
});

export function AgentKeysPage() {
  const { t } = useTranslation();
  const client = useQueryClient();
  const notify = useNotify();
  const query = useQuery(agentKeysOptions);
  const [open, setOpen] = useState(false);
  const [revoking, setRevoking] = useState<AgentKey | null>(null);
  const revoke = useSubmit();
  const create = useSubmit();
  const keys = query.data ?? [];
  return (
    <>
      <PageHeading
        eyebrow={t('ACCESS AND SECURITY')}
        title={t('Agent keys')}
        description={t(
          'Agent keys have no expiration. Revoke a key to stop its agents from connecting.',
        )}
        actions={
          <Button
            variant="primary"
            disabled={query.isPending}
            onClick={() => {
              create.reset();
              setOpen(true);
            }}
          >
            {t('Create key')}
          </Button>
        }
      />
      <SettingsNavigation />
      <Panel>
        {query.isPending ? (
          <Loading />
        ) : query.isError ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : keys.length ? (
          <RecordsTable
            label={t('Agent keys')}
            columns={[t('Name'), t('Prefix'), t('Created'), t('Status'), t('Actions')]}
            rows={keys.map((key) => ({
              id: key.id,
              cells: [
                key.name,
                <code key="prefix">{key.prefix}…</code>,
                formatDate(key.createdAt, { dateStyle: 'medium', timeStyle: 'short' }),
                <Badge key="status" tone={key.revokedAt ? 'neutral' : 'good'}>
                  {key.revokedAt ? t('Revoked') : t('Active')}
                </Badge>,
                <Button
                  key="revoke"
                  variant="danger"
                  disabled={!!key.revokedAt}
                  onClick={() => {
                    revoke.reset();
                    setRevoking(key);
                  }}
                >
                  {t('Revoke')}
                </Button>,
              ],
            }))}
          />
        ) : (
          <Empty
            title={t('No keys')}
            description={t('Create a named key to connect your agents.')}
          />
        )}
      </Panel>
      <Modal
        open={open}
        onOpenChange={(value) => {
          if (!create.pending) setOpen(value);
        }}
        title={t('Create key')}
        description={t('Choose a name to identify where this agent key is used.')}
      >
        {open && <AgentKeyForm create={create} onDone={() => setOpen(false)} />}
      </Modal>
      <Confirm
        open={!!revoking}
        onOpenChange={(value) => {
          if (!value && !revoke.pending) setRevoking(null);
        }}
        title={t('Revoke this key?')}
        description={t(
          'Agents using “{{name}}” will be rejected on their next request or message.',
          { name: revoking?.name ?? '' },
        )}
        busy={revoke.pending}
        error={revoke.error}
        onConfirm={() => {
          if (revoking)
            void revoke.submit(async () => {
              await api.revokeAgentKey(revoking.id);
              setRevoking(null);
              await client.invalidateQueries({ queryKey: agentKeysOptions.queryKey });
              notify('Key revoked');
            });
        }}
      />
    </>
  );
}

function AgentKeyForm({
  create,
  onDone,
}: {
  create: ReturnType<typeof useSubmit>;
  onDone: () => void;
}) {
  const { t } = useTranslation();
  const client = useQueryClient();
  const notify = useNotify();
  const [name, setName] = useState('');
  const [token, setToken] = useState('');
  if (token) return <KeySecret token={token} onDone={onDone} />;
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        void create.submit(async () => {
          const result = await api.createAgentKey({ name });
          setToken(result.token);
          await client.invalidateQueries({ queryKey: agentKeysOptions.queryKey });
          notify('Key created');
        });
      }}
    >
      <Box sx={{ p: 3, display: 'flex', flexDirection: 'column', gap: 3 }}>
        <TextField
          fullWidth
          label={t('Key name')}
          required
          value={name}
          onChange={(event) => setName(event.target.value)}
        />
        <FormError error={create.error} />
      </Box>
      <Box sx={{ p: 3, display: 'flex', flexWrap: 'wrap', gap: 2, justifyContent: 'flex-end' }}>
        <Button disabled={create.pending} onClick={onDone}>
          {t('Cancel')}
        </Button>
        <Button type="submit" variant="primary" busy={create.pending}>
          {t('Create key')}
        </Button>
      </Box>
    </form>
  );
}
