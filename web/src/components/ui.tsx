import Close from '@mui/icons-material/Close';
import Download from '@mui/icons-material/Download';
import Search from '@mui/icons-material/Search';
import {
  Alert,
  Box,
  Chip,
  Dialog,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Divider,
  IconButton,
  InputAdornment,
  LinearProgress,
  MenuItem,
  Button as MuiButton,
  Paper,
  Skeleton,
  Stack,
  TextField,
  Typography,
} from '@mui/material';
import type { ButtonProps } from '@mui/material/Button';
import { type ReactNode, useId } from 'react';
import { useTranslation } from 'react-i18next';
import { rangeLabels } from '../data/metrics';
import type { Range } from '../data/time-range';
import { formatPercent, formatRelativeTime } from '../i18n/format';
import { errorMessage } from '../i18n/messages';
export function Button({
  children,
  variant = 'secondary',
  busy,
  disabled,
  ...props
}: Omit<ButtonProps, 'variant' | 'color'> & {
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger';
  busy?: boolean;
}) {
  return (
    <MuiButton
      type="button"
      {...props}
      loading={busy}
      disabled={disabled || busy}
      variant={variant === 'primary' ? 'contained' : variant === 'ghost' ? 'text' : 'outlined'}
      color={variant === 'danger' ? 'error' : 'primary'}
    >
      {children}
    </MuiButton>
  );
}
export function Badge({
  children,
  tone = 'neutral',
}: {
  children: ReactNode;
  tone?: 'good' | 'warning' | 'danger' | 'neutral' | 'info';
  dot?: boolean;
}) {
  const colors = {
    good: 'success',
    warning: 'warning',
    danger: 'error',
    neutral: 'default',
    info: 'info',
  } as const;
  return <Chip label={children} color={colors[tone]} />;
}
export function PageHeading({
  eyebrow,
  title,
  description,
  actions,
}: {
  eyebrow: string;
  title: string;
  description: string;
  actions?: ReactNode;
}) {
  return (
    <Stack
      sx={{
        mb: 4,
        display: 'flex',
        flexDirection: { xs: 'column', lg: 'row' },
        alignItems: { xs: 'flex-start', lg: 'center' },
        justifyContent: 'space-between',
        gap: 2,
      }}
    >
      <Box sx={{ minWidth: 0, flex: 1 }}>
        <Typography variant="overline" color="text.secondary">
          {eyebrow}
        </Typography>
        <Typography variant="h4" component="h1" gutterBottom>
          {title}
        </Typography>
        <Typography
          color="text.secondary"
          variant="body2"
          sx={{ maxWidth: 720, overflowWrap: 'anywhere' }}
        >
          {description}
        </Typography>
      </Box>
      <Stack
        sx={{
          display: 'flex',
          flexDirection: 'row',
          flexWrap: 'wrap',
          alignItems: 'center',
          gap: 2,
        }}
      >
        {actions}
      </Stack>
    </Stack>
  );
}
export function Panel({
  title,
  subtitle,
  action,
  children,
  className = '',
}: {
  title?: string;
  subtitle?: string;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <Paper
      component="section"
      variant="outlined"
      className={className}
      sx={{ mb: 3, minWidth: 0, overflow: 'hidden', alignSelf: 'start' }}
    >
      {title && (
        <>
          <Stack
            sx={{
              display: 'flex',
              flexDirection: 'row',
              alignItems: 'center',
              justifyContent: 'space-between',
              gap: 2,
              px: 3,
              py: 2.5,
              flexWrap: 'wrap',
            }}
          >
            <Box>
              <Typography variant="h6" component="h2">
                {title}
              </Typography>
              {subtitle && (
                <Typography variant="body2" color="text.secondary">
                  {subtitle}
                </Typography>
              )}
            </Box>
            {action}
          </Stack>
          <Divider />
        </>
      )}
      {children}
    </Paper>
  );
}
export function Modal({
  open,
  onOpenChange,
  title,
  description,
  children,
  wide = false,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  children: ReactNode;
  wide?: boolean;
}) {
  const { t } = useTranslation();
  const id = useId();
  return (
    <Dialog
      fullWidth
      open={open}
      onClose={() => onOpenChange(false)}
      maxWidth={wide ? 'md' : 'sm'}
      aria-labelledby={`${id}-title`}
      aria-describedby={`${id}-description`}
    >
      <DialogTitle id={`${id}-title`} sx={{ pr: 7 }}>
        {title}
      </DialogTitle>
      <IconButton
        aria-label={t('Close')}
        onClick={() => onOpenChange(false)}
        sx={{ position: 'absolute', top: '1rem', right: '1rem' }}
      >
        <Close />
      </IconButton>
      <DialogContent sx={{ p: 0 }}>
        <DialogContentText id={`${id}-description`} sx={{ px: 3, pt: 1, mb: 0 }}>
          {description}
        </DialogContentText>
        {children}
      </DialogContent>
    </Dialog>
  );
}
export function Confirm({
  open,
  onOpenChange,
  title,
  description,
  onConfirm,
  busy,
  error,
  label = 'Confirm',
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  onConfirm: () => void;
  busy?: boolean;
  error?: Error | null;
  label?: string;
}) {
  const { t } = useTranslation();
  return (
    <Modal open={open} onOpenChange={onOpenChange} title={title} description={description}>
      {error && (
        <Box sx={{ px: 3, pt: 2 }}>
          <FormError error={error} />
        </Box>
      )}
      <Stack
        sx={{
          display: 'flex',
          flexDirection: 'row',
          flexWrap: 'wrap',
          justifyContent: 'flex-end',
          gap: 1,
          padding: 3,
        }}
      >
        <Button disabled={busy} onClick={() => onOpenChange(false)}>
          {t('Cancel')}
        </Button>
        <Button variant="danger" onClick={onConfirm} busy={busy}>
          {t(label)}
        </Button>
      </Stack>
    </Modal>
  );
}
export function Empty({
  title,
  description,
  action,
}: {
  title: string;
  description: string;
  action?: ReactNode;
}) {
  return (
    <Stack
      sx={{
        alignItems: 'center',
        justifyContent: 'center',
        gap: 2,
        textAlign: 'center',
        px: 3,
        py: 7,
        minHeight: 280,
      }}
    >
      <Search color="disabled" />
      <Typography variant="h5" component="h2">
        {title}
      </Typography>
      <Typography color="text.secondary" sx={{ maxWidth: 440 }}>
        {description}
      </Typography>
      {action}
    </Stack>
  );
}
export function Loading({ form = false }: { form?: boolean }) {
  const { t } = useTranslation();
  if (form)
    return (
      <Paper
        variant="outlined"
        role="status"
        aria-label={t('Loading data')}
        aria-busy="true"
        sx={{ maxWidth: 480, mx: 'auto', my: { xs: 4, sm: 10 }, p: { xs: 3, sm: 4 } }}
      >
        <Stack spacing={3} aria-hidden="true">
          <Box>
            <Skeleton width="55%" height={40} />
            <Skeleton width="90%" height={20} />
          </Box>
          {[1, 2].map((n) => (
            <Skeleton key={n} variant="rounded" height={40} />
          ))}
          <Skeleton variant="rounded" height={36} />
          <Skeleton width="80%" />
        </Stack>
      </Paper>
    );
  return (
    <Stack role="status" aria-label={t('Loading data')} aria-busy="true" spacing={3}>
      <Stack spacing={1} aria-hidden="true">
        <Skeleton width={96} height={16} />
        <Skeleton width="65%" height={40} sx={{ maxWidth: 360 }} />
        <Skeleton width="90%" height={20} sx={{ maxWidth: 520 }} />
      </Stack>
      <Box
        aria-hidden="true"
        sx={{
          display: 'grid',
          gridTemplateColumns: { xs: 'repeat(2,minmax(0,1fr))', lg: 'repeat(4,minmax(0,1fr))' },
          gap: 2,
        }}
      >
        {[1, 2, 3, 4].map((n) => (
          <Paper variant="outlined" key={n} sx={{ p: 2.5 }}>
            <Skeleton width="70%" height={16} />
            <Skeleton width="45%" height={48} />
            <Skeleton width="85%" height={16} />
          </Paper>
        ))}
      </Box>
      <Paper variant="outlined" aria-hidden="true" sx={{ p: { xs: 2, sm: 3 } }}>
        <Stack spacing={3}>
          <Skeleton variant="rounded" width="60%" height={36} sx={{ maxWidth: 280 }} />
          <Divider />
          {[1, 2, 3, 4, 5].map((n) => (
            <Stack key={n} direction="row" spacing={2} sx={{ alignItems: 'center' }}>
              <Skeleton variant="rounded" width={32} height={32} sx={{ flexShrink: 0 }} />
              <Box sx={{ flex: 1 }}>
                <Skeleton width="60%" />
                <Skeleton width="85%" />
              </Box>
              <Skeleton variant="rounded" width={56} height={20} />
            </Stack>
          ))}
        </Stack>
      </Paper>
    </Stack>
  );
}
export function ChartLoading() {
  const { t } = useTranslation();
  return (
    <Stack
      role="status"
      aria-label={t('Loading measurements…')}
      aria-busy="true"
      spacing={3}
      sx={{ height: 350, p: 3 }}
    >
      <Box sx={{ position: 'relative', flex: 1 }}>
        <Stack aria-hidden="true" sx={{ height: '100%', justifyContent: 'space-between' }}>
          {[1, 2, 3, 4, 5].map((n) => (
            <Skeleton key={n} variant="rectangular" height={1} />
          ))}
        </Stack>
        <Typography
          variant="body2"
          color="text.secondary"
          sx={{ position: 'absolute', inset: 0, display: 'grid', placeItems: 'center' }}
        >
          {t('Loading measurements…')}
        </Typography>
      </Box>
      <Stack direction="row" aria-hidden="true" sx={{ justifyContent: 'space-between' }}>
        {[1, 2, 3, 4].map((n) => (
          <Skeleton key={n} width="15%" height={16} />
        ))}
      </Stack>
    </Stack>
  );
}
export function ErrorState({ error, retry }: { error: Error; retry: () => void }) {
  const { t } = useTranslation();
  return (
    <Alert severity="error" action={<Button onClick={retry}>{t('Retry')}</Button>}>
      {errorMessage(error)}
    </Alert>
  );
}
export function SearchField({
  value,
  onChange,
  placeholder = 'Search…',
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
}) {
  const { t } = useTranslation();
  return (
    <TextField
      fullWidth
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={t(placeholder)}
      sx={{ minWidth: 0, width: { xs: '100%', sm: 280 }, flexShrink: 0 }}
      slotProps={{
        htmlInput: {
          'aria-label': placeholder,
        },
        input: {
          startAdornment: (
            <InputAdornment position="start">
              <Search fontSize="small" />
            </InputAdornment>
          ),
          endAdornment: value ? (
            <InputAdornment position="end">
              <IconButton aria-label={t('Clear search')} onClick={() => onChange('')}>
                <Close fontSize="small" />
              </IconButton>
            </InputAdornment>
          ) : null,
        },
      }}
    />
  );
}
export function RangeSelect({
  value,
  onChange,
}: {
  value: Range;
  onChange: (range: Range) => void;
}) {
  const { t } = useTranslation();
  return (
    <TextField
      fullWidth
      select
      label={t('Time range')}
      value={value}
      onChange={(e) => onChange(e.target.value as Range)}
      sx={{ width: { xs: '100%', sm: 192 }, minWidth: 160 }}
    >
      {Object.entries(rangeLabels).map(([key, label]) => (
        <MenuItem key={key} value={key}>
          {t(label)}
        </MenuItem>
      ))}
    </TextField>
  );
}
export function Progress({ value, label }: { value: number; label?: string }) {
  const { t } = useTranslation();
  return (
    <Box>
      {label && (
        <Stack sx={{ display: 'flex', flexDirection: 'row', justifyContent: 'space-between' }}>
          <span>{label}</span>
          <span>{formatPercent(value)}</span>
        </Stack>
      )}
      <Stack sx={{ display: 'flex', flexDirection: 'row', alignItems: 'center', gap: 1 }}>
        <LinearProgress
          variant="determinate"
          value={value}
          color={value >= 85 ? 'warning' : 'primary'}
          aria-label={label ?? t('Usage')}
          sx={{ flex: 1, minWidth: 48 }}
        />
        {!label && <Typography variant="caption">{formatPercent(value)}</Typography>}
      </Stack>
    </Box>
  );
}
export function FormError({ error }: { error?: Error | null }) {
  useTranslation();
  return error ? <Alert severity="error">{errorMessage(error)}</Alert> : null;
}
export const relativeTime = formatRelativeTime;
export function download(filename: string, contents: string, type: string) {
  const url = URL.createObjectURL(
    new Blob([contents], {
      type,
    }),
  );
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
export function ExportButton({ onClick }: { onClick: () => void }) {
  const { t } = useTranslation();
  return (
    <Button startIcon={<Download />} onClick={onClick}>
      {t('Export')}
    </Button>
  );
}
