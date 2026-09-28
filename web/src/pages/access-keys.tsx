import { MenuItem, TextField } from '@mui/material';
import Box from '@mui/material/Box';
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
import { type ApiKey, type ApiKeyInput, api, apiKeyInputSchema } from '../data/api';
import { useSubmit } from '../data/use-submit';
import { formatDate } from '../i18n/format';

const keysOptions = queryOptions({
  queryKey: ['apikeys'],
  queryFn: ({ signal }) => api.listKeys(signal),
  retry: false,
  refetchOnWindowFocus: true,
  refetchInterval: (query) => {
    const now = Date.now();
    const next = Math.min(
      ...(query.state.data ?? [])
        .filter((key) => !key.revokedAt && key.expiresAt)
        .map((key) => Date.parse(key.expiresAt ?? ''))
        .filter((expiry) => expiry > now),
    );
    return Math.min(60_000, Math.max(100, next - now));
  },
});

export function ApiKeysPage() {
  const { t } = useTranslation();
  const client = useQueryClient();
  const notify = useNotify();
  const query = useQuery(keysOptions);
  const [open, setOpen] = useState(false);
  const [revoking, setRevoking] = useState<ApiKey | null>(null);
  const revoke = useSubmit();
  const create = useSubmit();
  const keys = query.data ?? [];
  return (
    <>
      <PageHeading
        eyebrow={t('ACCESS AND SECURITY')}
        title={t('API keys')}
        description={t('Manage programmatic access to your Arveld instance.')}
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
            label={t('Access keys')}
            columns={[
              t('Name'),
              t('Prefix'),
              t('Permission'),
              t('Expiration'),
              t('Status'),
              t('Actions'),
            ]}
            rows={keys.map((key) => {
              const expired = key.expiresAt !== null && Date.parse(key.expiresAt) <= Date.now();
              return {
                id: key.id,
                cells: [
                  key.name,
                  <code key="prefix">{key.prefix}…</code>,
                  key.permission === 'read' ? t('Read only') : t('Read and write'),
                  key.expiresAt === null
                    ? t('No expiration')
                    : formatDate(key.expiresAt, { dateStyle: 'medium', timeStyle: 'short' }),
                  <Badge key="status" tone={key.revokedAt || expired ? 'neutral' : 'good'}>
                    {key.revokedAt ? t('Revoked') : expired ? t('Expired') : t('Active')}
                  </Badge>,
                  <Button
                    key="revoke"
                    variant="danger"
                    disabled={!!key.revokedAt}
                    onClick={() => setRevoking(key)}
                  >
                    {t('Revoke')}
                  </Button>,
                ],
              };
            })}
          />
        ) : (
          <Empty
            title={t('No keys')}
            description={t('Create a key with a clear name and expiration.')}
          />
        )}
      </Panel>

      <Modal
        open={open}
        onOpenChange={(value) => {
          if (!create.pending) setOpen(value);
        }}
        title={t('Create key')}
        description={t('Choose its purpose and expiration.')}
      >
        {open && <KeyForm create={create} onDone={() => setOpen(false)} />}
      </Modal>
      <FormError error={revoke.error} />
      <Confirm
        open={!!revoking}
        onOpenChange={(value) => {
          if (!value && !revoke.pending) setRevoking(null);
        }}
        title={t('Revoke this key?')}
        description={t('This key will be marked as revoked. Create a new key to replace it.')}
        busy={revoke.pending}
        onConfirm={() => {
          if (revoking)
            void revoke.submit(async () => {
              await api.revokeKey(revoking.id);
              setRevoking(null);
              await client.invalidateQueries({ queryKey: keysOptions.queryKey });
              notify('Key revoked');
            });
        }}
      />
    </>
  );
}
function KeyForm({ create, onDone }: { create: ReturnType<typeof useSubmit>; onDone: () => void }) {
  const { t } = useTranslation();
  const client = useQueryClient();
  const notify = useNotify();
  const [input, setInput] = useState<ApiKeyInput>({
    name: '',
    expiresDays: 30,
    permission: 'read',
  });
  const [token, setToken] = useState('');
  if (token) return <KeySecret token={token} onDone={onDone} />;
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        void create.submit(async () => {
          const result = await api.createKey(apiKeyInputSchema.parse(input));
          setToken(result.token);
          await client.invalidateQueries({ queryKey: keysOptions.queryKey });
          notify('Key created');
        });
      }}
    >
      <Box component="div" sx={{ p: 3, display: 'flex', flexDirection: 'column', gap: 3 }}>
        <TextField
          fullWidth
          label={t('Key name')}
          required
          value={input.name}
          onChange={(e) => setInput({ ...input, name: e.target.value })}
        />
        <TextField
          fullWidth
          select
          label={t('Validity')}
          value={input.expiresDays ?? 'never'}
          onChange={(e) =>
            setInput({
              ...input,
              expiresDays:
                e.target.value === 'never' ? null : (+e.target.value as ApiKeyInput['expiresDays']),
            })
          }
        >
          {[1, 7, 30, 90, 365].map((days) => (
            <MenuItem key={days} value={days}>
              {t('{{count}} day', { count: days })}
            </MenuItem>
          ))}
          <MenuItem value="never">{t('No expiration')}</MenuItem>
        </TextField>

        <TextField
          fullWidth
          select
          label={t('Permission')}
          value={input.permission}
          onChange={(e) => setInput({ ...input, permission: e.target.value as 'read' | 'write' })}
        >
          <MenuItem value="read">{t('Read only')}</MenuItem>
          <MenuItem value="write">{t('Read and write')}</MenuItem>
        </TextField>
        <FormError error={create.error} />
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
