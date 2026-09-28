import {
  Box,
  Checkbox,
  FormControlLabel,
  MenuItem,
  Stack,
  TextField,
  Typography,
} from '@mui/material';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { type HTTPValidation, httpMethods, type MonitorInput } from '../data/api';
import { Button } from './ui';

type HTTPInput = Extract<MonitorInput, { protocol: 'http' }>;
const validationLabels = {
  contains: 'Contains text',
  not_contains: 'Does not contain text',
  regex: 'Matches regular expression',
  json_path: 'JSON path',
  min_size: 'Minimum body size',
  max_size: 'Maximum body size',
} as const;

export function HTTPMonitorSettings({
  input,
  onChange,
}: {
  input: HTTPInput;
  onChange: (value: HTTPInput) => void;
}) {
  const { t } = useTranslation();
  const [headers, setHeaders] = useState(() =>
    Object.entries(input.headers ?? {}).map(([name, value]) => ({
      id: crypto.randomUUID(),
      name,
      value,
    })),
  );
  const [showValues, setShowValues] = useState(false);
  const rules = input.validations ?? [];
  const [ruleIds, setRuleIds] = useState(() => rules.map(() => crypto.randomUUID()));
  function changeHeaders(next: typeof headers) {
    setHeaders(next);
    onChange({
      ...input,
      headers: Object.fromEntries(
        next.filter((row) => row.name).map((row) => [row.name, row.value]),
      ),
    });
  }
  function changeRule(index: number, rule: HTTPValidation) {
    onChange({ ...input, validations: rules.map((value, i) => (i === index ? rule : value)) });
  }
  return (
    <>
      <TextField
        fullWidth
        select
        label={t('HTTP method')}
        value={input.method}
        onChange={(event) =>
          onChange({ ...input, method: event.target.value as HTTPInput['method'] })
        }
      >
        {httpMethods.map((method) => (
          <MenuItem key={method} value={method}>
            {method}
          </MenuItem>
        ))}
      </TextField>
      <TextField
        fullWidth
        multiline
        minRows={4}
        maxRows={12}
        label={t('Request body')}
        value={input.body ?? ''}
        onChange={(event) => onChange({ ...input, body: event.target.value })}
        slotProps={{ htmlInput: { maxLength: 65536 } }}
        helperText={t(
          'Content-Type is detected automatically: JSON, form data or plain text. An explicit header takes precedence. Maximum 64 KiB.',
        )}
      />
      <FormControlLabel
        control={
          <Checkbox
            checked={input.skip_tls_verify ?? false}
            onChange={(_, checked) => onChange({ ...input, skip_tls_verify: checked })}
          />
        }
        label={t('Ignore TLS certificate verification')}
      />
      <Stack spacing={2}>
        <Typography component="h2" variant="subtitle1">
          {t('Request headers')}
        </Typography>
        <Typography variant="body2" color="text.secondary">
          {t(
            'Use Authorization for Basic or Bearer authentication. The Agent follows redirects and can forward configured headers: use a trusted target and redirect chain.',
          )}
        </Typography>
        {headers.map((header, index) => {
          const duplicate = headers.some(
            (other, i) => i !== index && other.name.toLowerCase() === header.name.toLowerCase(),
          );
          return (
            <Box
              key={header.id}
              sx={{
                display: 'grid',
                gap: 1.5,
                gridTemplateColumns: {
                  xs: 'minmax(0,1fr)',
                  sm: 'minmax(0,1fr) minmax(0,2fr) auto',
                },
                alignItems: 'start',
              }}
            >
              <TextField
                fullWidth
                required
                label={t('Header name')}
                value={header.name}
                error={duplicate}
                helperText={duplicate ? t('Header names must be unique.') : undefined}
                slotProps={{
                  htmlInput: { pattern: duplicate ? '(?!)' : undefined, maxLength: 256 },
                }}
                onChange={(event) =>
                  changeHeaders(
                    headers.map((row, i) =>
                      i === index ? { ...row, name: event.target.value } : row,
                    ),
                  )
                }
              />
              <TextField
                fullWidth
                label={t('Header value')}
                type={showValues ? 'text' : 'password'}
                autoComplete="off"
                value={header.value}
                slotProps={{ htmlInput: { maxLength: 16384 } }}
                onChange={(event) =>
                  changeHeaders(
                    headers.map((row, i) =>
                      i === index ? { ...row, value: event.target.value } : row,
                    ),
                  )
                }
              />
              <Button
                onClick={() => changeHeaders(headers.filter((_, i) => i !== index))}
                aria-label={t('Remove header {{number}}', { number: index + 1 })}
              >
                {t('Remove')}
              </Button>
            </Box>
          );
        })}
        <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 2, alignItems: 'center' }}>
          <Button
            disabled={headers.length >= 32}
            onClick={() =>
              changeHeaders([...headers, { id: crypto.randomUUID(), name: '', value: '' }])
            }
          >
            {t('Add header')}
          </Button>
          {headers.length > 0 && (
            <FormControlLabel
              control={
                <Checkbox checked={showValues} onChange={(_, checked) => setShowValues(checked)} />
              }
              label={t('Show header values')}
            />
          )}
        </Box>
      </Stack>
      <Stack spacing={2}>
        <Typography component="h2" variant="subtitle1">
          {t('Response validations')}
        </Typography>
        <Typography variant="body2" color="text.secondary">
          {t(
            'All validations must pass. Empty or incomplete responses cannot be validated by the current Agent and produce an unknown result.',
          )}
        </Typography>
        {rules.map((rule, index) => (
          <Box
            key={ruleIds[index]}
            sx={{
              border: 1,
              borderColor: 'divider',
              borderRadius: 2,
              p: 2,
              display: 'grid',
              gap: 2,
            }}
          >
            <TextField
              fullWidth
              select
              label={t('Validation {{number}}', { number: index + 1 })}
              value={rule.type}
              onChange={(event) => {
                const type = event.target.value as HTTPValidation['type'];
                changeRule(
                  index,
                  type === 'json_path'
                    ? { type, path: '' }
                    : type === 'min_size' || type === 'max_size'
                      ? { type, size: 0 }
                      : { type, value: '' },
                );
              }}
            >
              {Object.entries(validationLabels).map(([type, label]) => (
                <MenuItem key={type} value={type}>
                  {t(label)}
                </MenuItem>
              ))}
            </TextField>
            {rule.type === 'min_size' || rule.type === 'max_size' ? (
              <TextField
                fullWidth
                required
                type="number"
                label={t('Body size (bytes)')}
                value={rule.size ?? ''}
                slotProps={{ htmlInput: { min: 0, max: 4194304, step: 1 } }}
                onChange={(event) =>
                  changeRule(index, {
                    ...rule,
                    size: event.target.value === '' ? undefined : +event.target.value,
                  })
                }
              />
            ) : rule.type === 'json_path' ? (
              <>
                <TextField
                  fullWidth
                  required
                  label={t('GJSON path')}
                  placeholder="data.ready"
                  value={rule.path ?? ''}
                  onChange={(event) => changeRule(index, { ...rule, path: event.target.value })}
                  helperText={t('Uses GJSON syntax, for example data.ready or items.0.id.')}
                />
                <FormControlLabel
                  control={
                    <Checkbox
                      checked={rule.equals !== undefined}
                      onChange={(_, checked) =>
                        changeRule(
                          index,
                          checked
                            ? { ...rule, equals: 'true' }
                            : { type: 'json_path', path: rule.path },
                        )
                      }
                    />
                  }
                  label={t('Match an expected value')}
                />
                {rule.equals !== undefined && (
                  <TextField
                    fullWidth
                    required
                    label={t('Expected value')}
                    value={rule.equals}
                    onChange={(event) => changeRule(index, { ...rule, equals: event.target.value })}
                    helperText={t(
                      'The Agent compares the string representation. Empty-string equality is not supported.',
                    )}
                  />
                )}
              </>
            ) : (
              <TextField
                fullWidth
                required
                label={rule.type === 'regex' ? t('Regular expression') : t('Expected text')}
                value={rule.value ?? ''}
                slotProps={{ htmlInput: { maxLength: 4096 } }}
                onChange={(event) => changeRule(index, { ...rule, value: event.target.value })}
                helperText={
                  rule.type === 'regex'
                    ? t('Go regular expression syntax (RE2).')
                    : t('Text matching is case-sensitive.')
                }
              />
            )}
            <Button
              sx={{ justifySelf: 'end' }}
              onClick={() => {
                setRuleIds(ruleIds.filter((_, i) => i !== index));
                onChange({ ...input, validations: rules.filter((_, i) => i !== index) });
              }}
              aria-label={t('Remove validation {{number}}', { number: index + 1 })}
            >
              {t('Remove')}
            </Button>
          </Box>
        ))}
        <Button
          sx={{ alignSelf: 'start' }}
          disabled={rules.length >= 32}
          onClick={() => {
            setRuleIds([...ruleIds, crypto.randomUUID()]);
            onChange({ ...input, validations: [...rules, { type: 'contains', value: '' }] });
          }}
        >
          {t('Add validation')}
        </Button>
      </Stack>
    </>
  );
}

export function HTTPOptionsSummary({
  monitor,
}: {
  monitor: Pick<HTTPInput, 'body' | 'headers' | 'skip_tls_verify' | 'validations'>;
}) {
  const { t } = useTranslation();
  return (
    <Stack spacing={1} sx={{ gridColumn: '1 / -1' }}>
      <Typography variant="body2">
        {t('TLS certificate verification: {{mode}}', {
          mode: monitor.skip_tls_verify ? t('Disabled') : t('Enabled'),
        })}
      </Typography>
      <Typography variant="body2">
        {t('Request body: {{bytes}} bytes · {{headers}} headers · {{validations}} validations', {
          bytes: new TextEncoder().encode(monitor.body ?? '').length,
          headers: Object.keys(monitor.headers ?? {}).length,
          validations: monitor.validations?.length ?? 0,
        })}
      </Typography>
      {!!monitor.headers && (
        <Typography variant="body2" color="text.secondary">
          {Object.keys(monitor.headers).join(', ')}
        </Typography>
      )}
      <Typography variant="body2" sx={{ whiteSpace: 'pre-wrap' }}>
        {monitor.validations
          ?.map(
            (rule, index) =>
              `${index + 1}. ${t(validationLabels[rule.type])}: ${rule.type === 'json_path' ? `${rule.path}${rule.equals === undefined ? '' : ` = ${rule.equals}`}` : (rule.size ?? rule.value)}`,
          )
          .join('\n')}
      </Typography>
    </Stack>
  );
}
