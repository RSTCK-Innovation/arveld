import { Alert, Box, TextField } from '@mui/material';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, FormError } from './ui';

// The parent form owns the secret and discards it when the dialog closes.
export function KeySecret({ token, onDone }: { token: string; onDone: () => void }) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);
  const [copyError, setCopyError] = useState<Error | null>(null);
  async function copyKey() {
    setCopied(false);
    setCopyError(null);
    try {
      await navigator.clipboard.writeText(token);
      setCopied(true);
    } catch {
      setCopyError(new Error(t('Unable to copy. Select and copy the text manually.')));
    }
  }
  return (
    <>
      <Box sx={{ p: 3, display: 'flex', flexDirection: 'column', gap: 3 }}>
        <Alert severity="success">
          {t('Copy this key now. It will not be shown again after closing.')}
        </Alert>
        <TextField
          fullWidth
          label={t('Access key')}
          value={token}
          multiline
          slotProps={{ input: { readOnly: true } }}
        />
        <Button onClick={() => void copyKey()}>{copied ? t('Copied') : t('Copy')}</Button>
        <FormError error={copyError} />
      </Box>
      <Box sx={{ display: 'flex', justifyContent: 'flex-end', p: 3 }}>
        <Button variant="primary" onClick={onDone}>
          {t('Done')}
        </Button>
      </Box>
    </>
  );
}
