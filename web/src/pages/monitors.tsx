import Plus from '@mui/icons-material/Add';
import { Divider } from '@mui/material';
import Box from '@mui/material/Box';
import { Navigate, useNavigate, useSearch } from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';
import { ButtonLink } from '../components/links';
import { MonitorList } from '../components/monitor-list';
import { PageHeading, Panel, SearchField } from '../components/ui';

export function MonitorsPage() {
  const { t } = useTranslation();
  const search = useSearch({ from: '/monitors' });
  const navigate = useNavigate({ from: '/monitors' });
  if (search.create) return <Navigate to="/monitors/new" replace />;
  return (
    <>
      <PageHeading
        eyebrow={t('AVAILABILITY')}
        title={t('Keep an eye on your services.')}
        description={t('Monitors saved in Arveld and assigned to your agents.')}
        actions={
          <ButtonLink to="/monitors/new" variant="contained" startIcon={<Plus />}>
            {t('Create a monitor')}
          </ButtonLink>
        }
      />
      <Panel>
        <Box sx={{ p: 3 }}>
          <SearchField
            value={search.q}
            placeholder={t('Search for a service…')}
            onChange={(q) =>
              void navigate({ search: (previous) => ({ ...previous, q }), replace: true })
            }
          />
        </Box>
        <Divider />
        <MonitorList q={search.q} />
      </Panel>
    </>
  );
}
