import { useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api, type Monitor } from '../data/api';
import { monitorOptions, monitorsOptions } from '../data/monitors';
import { useSubmit } from '../data/use-submit';
import { ButtonLink } from './links';
import { useNotify } from './notifications';
import { Button, Confirm } from './ui';

export function MonitorActions({ monitor }: { monitor: Monitor }) {
  const { t } = useTranslation();
  const client = useQueryClient();
  const navigate = useNavigate();
  const notify = useNotify();
  const [confirm, setConfirm] = useState(false);
  const action = useSubmit();
  return (
    <>
      <ButtonLink
        to="/monitors/$monitorId/edit"
        params={{ monitorId: monitor.id }}
        disabled={action.pending}
      >
        {t('Edit monitor')}
      </ButtonLink>
      <Button
        variant="danger"
        onClick={() => {
          action.reset();
          setConfirm(true);
        }}
      >
        {t('Delete monitor')}
      </Button>
      <Confirm
        open={confirm}
        onOpenChange={(open) => {
          if (!action.pending) setConfirm(open);
        }}
        title={t('Delete {{value1}}?', { value1: monitor.name })}
        description={t(
          'The monitor will stop when its agent receives the change. Existing measurements are kept according to retention.',
        )}
        label="Delete monitor"
        busy={action.pending}
        error={action.error}
        onConfirm={() => {
          void action.submit(async () => {
            await api.deleteMonitor(monitor.id);
            await client.cancelQueries({ queryKey: monitorOptions(monitor.id).queryKey });
            client.setQueryData(monitorOptions(monitor.id).queryKey, null);
            void client.invalidateQueries({ queryKey: monitorsOptions.queryKey });
            void client.invalidateQueries({
              queryKey: ['agent-config', monitor.agent_instance_uid],
            });
            void client.invalidateQueries({
              queryKey: ['agent-config-revisions', monitor.agent_instance_uid],
            });
            notify('Monitor deleted');
            await navigate({ to: '/monitors', search: { q: '', status: 'all', create: false } });
          });
        }}
      />
    </>
  );
}
