import ChevronRight from '@mui/icons-material/ChevronRight';
import Close from '@mui/icons-material/Close';
import LayoutDashboard from '@mui/icons-material/DashboardOutlined';
import Server from '@mui/icons-material/DnsOutlined';
import Command from '@mui/icons-material/KeyboardCommandKey';
import Menu from '@mui/icons-material/Menu';
import Activity from '@mui/icons-material/MonitorHeartOutlined';
import Bell from '@mui/icons-material/NotificationsNone';
import UserRound from '@mui/icons-material/PersonOutlined';
import Search from '@mui/icons-material/Search';
import Settings from '@mui/icons-material/SettingsOutlined';
import SlidersHorizontal from '@mui/icons-material/Tune';
import ShieldCheck from '@mui/icons-material/VerifiedUserOutlined';
import {
  Alert,
  AppBar,
  Avatar,
  Chip,
  Divider,
  Drawer,
  IconButton,
  List,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  ListSubheader,
  Badge as MuiBadge,
  Paper,
  Stack,
  Toolbar,
  Typography,
  useMediaQuery,
  useTheme,
} from '@mui/material';
import Box from '@mui/material/Box';
import { useQuery } from '@tanstack/react-query';
import { Navigate, Outlet, useLocation, useNavigate } from '@tanstack/react-router';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { initials } from '../data/account';
import { agentsOptions } from '../data/agents';
import { incidentsOptions } from '../data/incidents';
import { browserPreferences, usePreferences } from '../data/preferences-query';
import { useSession } from '../data/session';
import { AppLink, NavLink } from './links';
import { Badge, Button, ErrorState, Loading, Modal, SearchField } from './ui';

const navigation = [
  {
    to: '/',
    label: 'Overview',
    icon: LayoutDashboard,
  },
  {
    to: '/agents',
    label: 'Agents',
    icon: Server,
  },
  {
    to: '/monitors',
    label: 'Monitors',
    icon: Activity,
  },
  {
    to: '/alerts',
    label: 'Alerts',
    icon: Bell,
  },
  {
    to: '/metrics',
    label: 'Explorer',
    icon: SlidersHorizontal,
  },
  { to: '/notifications', label: 'Notifications', icon: Bell },
  { to: '/maintenance', label: 'Maintenance', icon: Settings },
  { to: '/settings/apikeys', label: 'API keys', icon: Settings },
  { to: '/settings/agent-keys', label: 'Agent keys', icon: Settings },
  { to: '/settings/instance', label: 'Arveld health', icon: Activity },
  {
    to: '/settings',
    label: 'Settings',
    icon: Settings,
  },
  {
    to: '/account',
    label: 'My account',
    icon: UserRound,
  },
] as const;
export function Brand() {
  const { t } = useTranslation();
  return (
    <AppLink
      to="/"
      underline="none"
      aria-label={t('Arveld, home')}
      sx={{
        display: 'flex',
        alignItems: 'center',
        flexWrap: 'wrap',
        gap: 2,
        p: 2,
      }}
    >
      <svg width="31" height="31" viewBox="0 0 40 40" aria-hidden="true">
        <rect width="40" height="40" rx="11" fill="currentColor" />
        <path d="m9 28 10-18h4L13 28zm11-10 7 10h5L22 10z" fill="white" />
      </svg>
      <Typography component="span" variant="h5" color="text.primary">
        {t('arveld')}.
      </Typography>
      <Chip label={t('BETA')} size="small" />
    </AppLink>
  );
}
export function Shell() {
  const location = useLocation();
  const query = useSession();
  if (query.isPending)
    return (
      <Box component="main" sx={{ maxWidth: 1200, mx: 'auto', p: { xs: 2, sm: 3, lg: 4 } }}>
        <Loading form={location.pathname === '/login' || location.pathname === '/signup'} />
      </Box>
    );
  if (query.isError) return <ErrorState error={query.error} retry={() => void query.refetch()} />;
  if (query.data.status === 'setup' && location.pathname !== '/signup')
    return <Navigate to="/signup" replace />;
  if (query.data.status === 'anonymous' && location.pathname !== '/login')
    return <Navigate to="/login" replace />;
  if (
    query.data.status === 'authenticated' &&
    (location.pathname === '/signup' || location.pathname === '/login')
  )
    return <Navigate to="/" replace />;
  if (query.data.status !== 'authenticated')
    return (
      <Box component="main" id="main" sx={{ minHeight: '100vh' }}>
        <Outlet />
      </Box>
    );
  return <WorkspaceShell />;
}
function WorkspaceShell() {
  const { t } = useTranslation();
  const { data } = usePreferences();
  const incidents = useQuery(incidentsOptions({ status: 'open', limit: 1 }));
  const session = useSession();
  const account = session.data?.status === 'authenticated' ? session.data.account : null;
  const location = useLocation();
  const navigate = useNavigate();
  const desktop = useMediaQuery(useTheme().breakpoints.up('md'));
  const [searchOpen, setSearchOpen] = useState(false);
  const [mobileOpen, setMobileOpen] = useState(false);
  const [search, setSearch] = useState('');
  const agentsQuery = useQuery({ ...agentsOptions, enabled: searchOpen });
  useEffect(() => {
    function keyboard(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault();
        setSearchOpen((open) => !open);
      }
    }
    window.addEventListener('keydown', keyboard);
    return () => window.removeEventListener('keydown', keyboard);
  }, []);
  const current =
    navigation.find((n) =>
      n.to === '/' ? location.pathname === '/' : location.pathname.startsWith(n.to),
    )?.label ?? (location.pathname.startsWith('/alert-rules/') ? 'Monitors' : t('Page not found'));
  const matches =
    agentsQuery.data?.filter((a) =>
      `${a.hostname ?? ''} ${a.instance_uid}`.toLowerCase().includes(search.toLowerCase()),
    ) ?? [];
  const firing = incidents.data?.total ?? 0;
  return (
    <Box component="div" sx={{ display: 'flex', minHeight: '100vh' }}>
      <Box
        component="a"
        href="#main"
        sx={{
          position: 'absolute',
          top: -100,
          left: 8,
          zIndex: 1500,
          '&:focus': { top: 8, bgcolor: 'background.paper', p: 2 },
        }}
      >
        {t('Skip to content')}
      </Box>
      <Drawer
        variant={desktop ? 'permanent' : 'temporary'}
        open={desktop || mobileOpen}
        onClose={() => setMobileOpen(false)}
        sx={{ width: '0rem', flexShrink: 0 }}
        slotProps={{
          paper: {
            sx: {
              width: 256,
            },
            'aria-label': t('Workspace navigation'),
          },
        }}
      >
        <Box component="div" sx={{ display: 'flex', alignItems: 'center' }}>
          <Brand />
          {!desktop && (
            <IconButton aria-label={t('Close navigation')} onClick={() => setMobileOpen(false)}>
              <Close />
            </IconButton>
          )}
        </Box>
        <Paper
          variant="soft"
          sx={{ display: 'flex', alignItems: 'center', gap: 1.5, p: 1.5, mx: 2, my: 2 }}
        >
          <Avatar variant="rounded">{t('H')}</Avatar>
          <ListItemText
            primary={data?.name ?? t('My services')}
            secondary={t('Personal workspace')}
          />
        </Paper>
        {[
          {
            title: t('WORKSPACE'),
            name: t('Main navigation'),
            paths: ['/', '/agents', '/monitors', '/metrics'],
          },
          {
            title: t('Alerting'),
            name: t('Alerting'),
            paths: ['/alerts', '/notifications', '/maintenance'],
          },
        ].map(({ title, name, paths }) => (
          <div key={name}>
            <List
              component="nav"
              aria-label={name}
              subheader={<ListSubheader>{title}</ListSubheader>}
            >
              {navigation
                .filter((n) => paths.includes(n.to))
                .map(({ to, label, icon: Icon }) => (
                  <NavLink
                    key={to}
                    to={to}
                    activeOptions={{ exact: to === '/' }}
                    selected={current === label}
                    onClick={() => setMobileOpen(false)}
                  >
                    <ListItemIcon>
                      <Icon />
                    </ListItemIcon>
                    <ListItemText primary={t(label)} />
                    {to === '/alerts' && firing > 0 && <Chip label={firing} color="warning" />}
                  </NavLink>
                ))}
            </List>
          </div>
        ))}
        <Box component="div" sx={{ mt: 'auto' }}>
          <Box component="div" sx={{ display: 'flex', alignItems: 'center', gap: 2, p: 2 }}>
            <ShieldCheck />
            <div>
              <Typography component="strong" variant="subtitle2">
                {t('Your infra. Your data.')}
              </Typography>
              <Typography component="p" variant="body2" sx={{ mb: 2 }}>
                {t('Everything stays with you.')}
              </Typography>
            </div>
          </Box>
          <NavLink
            to="/settings"
            selected={location.pathname.startsWith('/settings')}
            onClick={() => setMobileOpen(false)}
          >
            <ListItemIcon>
              <Settings />
            </ListItemIcon>
            <ListItemText primary={t('Settings')} />
          </NavLink>
          <NavLink
            to="/account"
            aria-label={t('My account')}
            onClick={() => setMobileOpen(false)}
            sx={{ gap: 2 }}
          >
            <Avatar>{initials(account?.name ?? t('Administrator'))}</Avatar>
            <ListItemText
              primary={account?.name ?? t('My account')}
              secondary={t('Profile and security')}
            />
            <ChevronRight />
          </NavLink>
        </Box>
      </Drawer>
      <Box
        component="div"
        sx={{
          display: 'flex',
          flexDirection: 'column',
          minHeight: '100dvh',
          flex: 1,
          minWidth: 0,
          ml: { xs: 0, md: '256px' },
        }}
      >
        <AppBar position="sticky" color="inherit" elevation={0}>
          <Toolbar
            sx={{
              minHeight: 64,
              justifyContent: 'space-between',
              gap: 2,
              px: { xs: 2, sm: 3, lg: 4 },
            }}
          >
            <Box
              component="div"
              sx={{ display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: 2 }}
            >
              {!desktop && (
                <IconButton aria-label={t('Open navigation')} onClick={() => setMobileOpen(true)}>
                  <Menu />
                </IconButton>
              )}
              <Box component="span" sx={{ display: { xs: 'none', md: 'inline' } }}>
                {t('Workspace')}
              </Box>
              <ChevronRight />
              {location.pathname.startsWith('/settings/') && (
                <>
                  <AppLink to="/settings">{t('Settings')}</AppLink>
                  <ChevronRight />
                </>
              )}
              <span>{t(current)}</span>
            </Box>
            <Box
              component="div"
              sx={{ display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: 2 }}
            >
              <Button
                startIcon={<Search />}
                variant="ghost"
                aria-label={t('Search your lab')}
                sx={{ minWidth: '2rem' }}
                onClick={() => setSearchOpen(true)}
              >
                {desktop && <span>{t('Search your lab…')}</span>}
                {desktop && (
                  <Stack component="kbd" direction="row" sx={{ ml: 2, alignItems: 'center' }}>
                    <Command />
                    {t('K')}
                  </Stack>
                )}
              </Button>
              <AppLink to="/alerts" aria-label={t('Browse alerts')}>
                <MuiBadge variant="dot" color="warning" invisible={!firing}>
                  <Bell />
                </MuiBadge>
              </AppLink>
            </Box>
          </Toolbar>
        </AppBar>
        {browserPreferences.recovered && (
          <Alert severity="warning">
            {t('Browser preferences could not be fully restored. Check browser storage.')}
          </Alert>
        )}
        <Box
          component="main"
          id="main"
          tabIndex={-1}
          sx={{ width: '100%', maxWidth: 1440, mx: 'auto', p: { xs: 2, sm: 3, lg: 4 }, flex: 1 }}
        >
          <Outlet />
        </Box>
      </Box>
      <Modal
        open={searchOpen}
        onOpenChange={setSearchOpen}
        title={t('Search your lab')}
        description={t('Open a page or find an agent.')}
      >
        <Box component="div" sx={{ p: 3 }}>
          <SearchField
            value={search}
            onChange={setSearch}
            placeholder={t('An agent, an address, a page…')}
          />
          <List>
            <Typography variant="overline">{t('Pages')}</Typography>
            {navigation
              .filter((n) => t(n.label).toLowerCase().includes(search.toLowerCase()))
              .map(({ to, label, icon: Icon }) => (
                <ListItemButton
                  key={to}
                  onClick={() => {
                    setSearchOpen(false);
                    void navigate({
                      to,
                    });
                  }}
                >
                  <ListItemIcon>
                    <Icon />
                  </ListItemIcon>
                  <ListItemText primary={t(label)} />
                </ListItemButton>
              ))}
            <Divider />
            <Typography variant="overline">{t('Agents')}</Typography>
            {!agentsQuery.isError &&
              matches.slice(0, 6).map((a) => (
                <ListItemButton
                  key={a.instance_uid}
                  onClick={() => {
                    setSearchOpen(false);
                    void navigate({
                      to: '/agents/$agentId',
                      params: {
                        agentId: a.instance_uid,
                      },
                    });
                  }}
                >
                  <ListItemIcon>
                    <Server />
                  </ListItemIcon>
                  <ListItemText
                    primary={a.hostname ?? a.instance_uid}
                    secondary={a.hostname ? a.instance_uid : undefined}
                    sx={{ overflowWrap: 'anywhere' }}
                  />
                  <Badge tone={a.connected ? 'good' : 'neutral'}>
                    {a.connected ? t('Connected') : t('Offline')}
                  </Badge>
                </ListItemButton>
              ))}
            {agentsQuery.isPending && (
              <Typography role="status" color="text.secondary">
                {t('Loading data')}
              </Typography>
            )}
            {agentsQuery.isError && (
              <ErrorState error={agentsQuery.error} retry={() => void agentsQuery.refetch()} />
            )}
            {agentsQuery.isSuccess && !matches.length && (
              <Typography color="text.secondary">{t('No matching agents.')}</Typography>
            )}
          </List>
        </Box>
      </Modal>
    </Box>
  );
}
