import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import {
  createRootRoute,
  createRoute,
  createRouter,
  lazyRouteComponent,
  redirect,
} from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';
import { ButtonLink } from './components/links';
import { Shell } from './components/shell';
import { ErrorState } from './components/ui';
import { OverviewPage } from './pages/overview';

const rootRoute = createRootRoute({
  component: Shell,
});
const overview = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  component: OverviewPage,
});
const agents = createRoute({
  getParentRoute: () => rootRoute,
  path: '/agents',
  validateSearch: (search: Record<string, unknown>) => ({
    q: typeof search.q === 'string' ? search.q : '',
    status: ['all', 'online', 'offline'].includes(String(search.status))
      ? String(search.status)
      : 'all',
  }),
  component: lazyRouteComponent(() => import('./pages/agents'), 'AgentsPage'),
});
const agent = createRoute({
  getParentRoute: () => rootRoute,
  path: '/agents/$agentId',
  validateSearch: (
    search: Record<string, unknown>,
  ): {
    tab?: 'overview' | 'configuration' | 'alerts' | 'activity';
  } => {
    switch (search.tab) {
      case 'overview':
      case 'configuration':
      case 'alerts':
      case 'activity':
        return { tab: search.tab };
      default:
        return { tab: undefined };
    }
  },
  component: lazyRouteComponent(() => import('./pages/agent-detail'), 'AgentDetailPage'),
});
const monitors = createRoute({
  getParentRoute: () => rootRoute,
  path: '/monitors',
  validateSearch: (search: Record<string, unknown>) => ({
    q: typeof search.q === 'string' ? search.q : '',
    status: ['all', 'healthy', 'failing'].includes(String(search.status))
      ? String(search.status)
      : 'all',
    create: search.create === true,
  }),
  component: lazyRouteComponent(() => import('./pages/monitors'), 'MonitorsPage'),
});
const monitorCreate = createRoute({
  getParentRoute: () => rootRoute,
  path: '/monitors/new',
  component: lazyRouteComponent(() => import('./pages/monitor-create'), 'MonitorCreatePage'),
});
const alerts = createRoute({
  getParentRoute: () => rootRoute,
  path: '/alerts',
  component: lazyRouteComponent(() => import('./pages/alerts'), 'AlertsPage'),
});
const metrics = createRoute({
  getParentRoute: () => rootRoute,
  path: '/metrics',
  component: lazyRouteComponent(() => import('./pages/metrics'), 'MetricsPage'),
});
const settings = createRoute({
  getParentRoute: () => rootRoute,
  path: '/settings',
  component: lazyRouteComponent(() => import('./pages/settings'), 'SettingsPage'),
});
const signup = createRoute({
  getParentRoute: () => rootRoute,
  path: '/signup',
  component: lazyRouteComponent(() => import('./pages/signup'), 'SignupPage'),
});
const account = createRoute({
  getParentRoute: () => rootRoute,
  path: '/account',
  component: lazyRouteComponent(() => import('./pages/account'), 'AccountPage'),
});

const login = createRoute({
  getParentRoute: () => rootRoute,
  path: '/login',
  component: lazyRouteComponent(() => import('./pages/login'), 'LoginPage'),
});
const monitorDetail = createRoute({
  getParentRoute: () => rootRoute,
  path: '/monitors/$monitorId',
  component: lazyRouteComponent(() => import('./pages/monitor-detail'), 'MonitorDetailPage'),
});
const monitorEdit = createRoute({
  getParentRoute: () => rootRoute,
  path: '/monitors/$monitorId/edit',
  component: lazyRouteComponent(() => import('./pages/monitor-edit'), 'MonitorEditPage'),
});
const alertDetail = createRoute({
  getParentRoute: () => rootRoute,
  path: '/alerts/$alertId',
  component: lazyRouteComponent(() => import('./pages/alert-detail'), 'AlertDetailPage'),
});
const notifications = createRoute({
  getParentRoute: () => rootRoute,
  path: '/notifications',
  component: lazyRouteComponent(() => import('./pages/notifications'), 'NotificationsPage'),
});
const maintenance = createRoute({
  getParentRoute: () => rootRoute,
  path: '/maintenance',
  component: lazyRouteComponent(() => import('./pages/maintenance'), 'MaintenancePage'),
});
const agentKeys = createRoute({
  getParentRoute: () => rootRoute,
  path: '/settings/agent-keys',
  component: lazyRouteComponent(() => import('./pages/agent-keys'), 'AgentKeysPage'),
});
const apiKeys = createRoute({
  getParentRoute: () => rootRoute,
  path: '/settings/apikeys',
  component: lazyRouteComponent(() => import('./pages/access-keys'), 'ApiKeysPage'),
});
// Preserve existing bookmarks while keeping all key management under instance settings.
const legacyAgentKeys = createRoute({
  getParentRoute: () => rootRoute,
  path: '/agents/keys',
  beforeLoad: () => {
    throw redirect({ to: '/settings/agent-keys', replace: true });
  },
});
const legacyApiKeys = createRoute({
  getParentRoute: () => rootRoute,
  path: '/account/apikeys',
  beforeLoad: () => {
    throw redirect({ to: '/settings/apikeys', replace: true });
  },
});
const agentInstall = createRoute({
  getParentRoute: () => rootRoute,
  path: '/agents/install',
  component: lazyRouteComponent(() => import('./pages/agent-install'), 'AgentInstallPage'),
});
const instanceHealth = createRoute({
  getParentRoute: () => rootRoute,
  path: '/settings/instance',
  component: lazyRouteComponent(() => import('./pages/instance-health'), 'InstanceHealthPage'),
});
const notificationCreate = createRoute({
  getParentRoute: () => rootRoute,
  path: '/notifications/new',
  component: lazyRouteComponent(
    () => import('./pages/notification-edit'),
    'NotificationCreatePage',
  ),
});
const notificationEdit = createRoute({
  getParentRoute: () => rootRoute,
  path: '/notifications/$notificationId',
  component: lazyRouteComponent(() => import('./pages/notification-edit'), 'NotificationEditPage'),
});
const alertRuleCreate = createRoute({
  getParentRoute: () => rootRoute,
  path: '/monitors/$monitorId/alert-rules/new',
  component: lazyRouteComponent(() => import('./pages/alert-rule-edit'), 'AlertRuleCreatePage'),
});
const agentAlertRuleCreate = createRoute({
  getParentRoute: () => rootRoute,
  path: '/agents/$agentId/alert-rules/new',
  component: lazyRouteComponent(
    () => import('./pages/alert-rule-edit'),
    'AgentAlertRuleCreatePage',
  ),
});
const alertRuleEdit = createRoute({
  getParentRoute: () => rootRoute,
  path: '/alert-rules/$ruleId',
  component: lazyRouteComponent(() => import('./pages/alert-rule-edit'), 'AlertRuleEditPage'),
});
const monitorSilenceCreate = createRoute({
  getParentRoute: () => rootRoute,
  path: '/monitors/$monitorId/silences/new',
  component: lazyRouteComponent(() => import('./pages/silence-create'), 'MonitorSilenceCreatePage'),
});
const agentSilenceCreate = createRoute({
  getParentRoute: () => rootRoute,
  path: '/agents/$agentId/silences/new',
  component: lazyRouteComponent(() => import('./pages/silence-create'), 'AgentSilenceCreatePage'),
});
export const router = createRouter({
  routeTree: rootRoute.addChildren([
    overview,
    agents,
    agent,
    monitors,
    monitorCreate,
    monitorDetail,
    monitorEdit,
    alerts,
    alertDetail,
    notifications,
    notificationCreate,
    notificationEdit,
    alertRuleCreate,
    agentAlertRuleCreate,
    alertRuleEdit,
    monitorSilenceCreate,
    agentSilenceCreate,
    maintenance,
    metrics,
    settings,
    signup,
    login,
    account,
    legacyAgentKeys,
    legacyApiKeys,
    agentKeys,
    apiKeys,
    agentInstall,
    instanceHealth,
  ]),
  defaultPreload: 'intent',
  defaultErrorComponent: ({ error, reset }) => (
    <ErrorState error={error instanceof Error ? error : new Error(String(error))} retry={reset} />
  ),
  defaultNotFoundComponent: () => {
    const { t } = useTranslation();
    return (
      <Box
        component="div"
        sx={{
          display: 'flex',
          flexDirection: 'column',
          gap: 2,
          alignItems: 'center',
          p: 4,
          textAlign: 'center',
        }}
      >
        <span>404</span>
        <Typography component="h1" variant="h4" sx={{ mb: 2 }}>
          {t('This page has wandered off.')}
        </Typography>
        <Typography component="p" variant="body2" sx={{ mb: 2 }}>
          {t('Find your machines from the overview.')}
        </Typography>
        <ButtonLink variant="contained" color="primary" to="/">
          {t('Back to services')}
        </ButtonLink>
      </Box>
    );
  },
});
declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}
