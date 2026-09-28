import { i18n } from './index';

let activeLocale = 'en-US';
export function setFormatLocale(locale: string) {
  activeLocale = locale;
}
export function formatNumber(value: number, options?: Intl.NumberFormatOptions) {
  return new Intl.NumberFormat(activeLocale, options).format(value);
}
export function formatPercent(value: number) {
  return formatNumber(value / 100, { style: 'percent', maximumFractionDigits: 2 });
}

export function formatNetworkRate(bytesPerSecond: number) {
  const units = ['byte', 'kilobyte', 'megabyte', 'gigabyte', 'terabyte'];
  const index = Math.min(
    units.length - 1,
    Math.max(0, Math.floor(Math.log10(bytesPerSecond || 1) / 3)),
  );
  return formatNumber(bytesPerSecond / 1000 ** index, {
    style: 'unit',
    unit: `${units[index]}-per-second`,
    unitDisplay: 'short',
    maximumFractionDigits: 1,
  });
}

export function formatDuration(seconds: number) {
  const minutes = Math.floor(seconds / 60);
  const parts: [number, string][] = [
    [Math.floor(minutes / 1440), 'day'],
    [Math.floor((minutes % 1440) / 60), 'hour'],
    [minutes % 60, 'minute'],
  ];
  if (minutes === 0) parts.push([Math.floor(seconds), 'second']);
  return parts
    .filter(([value, unit]) => value > 0 || unit === 'second')
    .map(([value, unit]) => formatNumber(value, { style: 'unit', unit, unitDisplay: 'short' }))
    .join(' ');
}
export function formatDate(value: string | number | Date, options?: Intl.DateTimeFormatOptions) {
  return new Intl.DateTimeFormat(activeLocale, options).format(new Date(value));
}
export function formatRelativeTime(value: string, now = Date.now()) {
  const minutes = Math.max(0, Math.floor((now - Date.parse(value)) / 60_000));
  if (!Number.isFinite(minutes)) return '—';
  if (minutes < 1) return i18n.t('Just now');
  const unit = minutes < 60 ? 'minute' : minutes < 1440 ? 'hour' : 'day';
  const amount = minutes < 60 ? minutes : Math.floor(minutes / (minutes < 1440 ? 60 : 1440));
  return new Intl.RelativeTimeFormat(activeLocale, { numeric: 'auto', style: 'short' }).format(
    -amount,
    unit,
  );
}
