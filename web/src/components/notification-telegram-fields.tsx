import { TextField } from '@mui/material';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { TelegramNotificationConfig } from '../data/api';

// Keep numeric text while editing so an incomplete negative ID is not rewritten.
export function NotificationTelegramFields({
  config,
  onChange,
}: {
  config: TelegramNotificationConfig;
  onChange: (value: TelegramNotificationConfig) => void;
}) {
  const { t } = useTranslation();
  const [chat, setChat] = useState(config.chat_id ? String(config.chat_id) : '');
  const [thread, setThread] = useState(
    config.message_thread_id ? String(config.message_thread_id) : '',
  );
  return (
    <>
      <TextField
        label={t('Telegram bot token')}
        type="password"
        required
        value={config.bot_token}
        onChange={(event) => onChange({ ...config, bot_token: event.target.value })}
        slotProps={{ htmlInput: { maxLength: 512, autoComplete: 'new-password' } }}
        helperText={t(
          'Use the token from BotFather. Add the bot to the destination chat and allow it to send messages.',
        )}
      />
      <TextField
        label={t('Chat ID')}
        required
        value={chat}
        onChange={(event) => {
          setChat(event.target.value);
          onChange({ ...config, chat_id: Number(event.target.value) });
        }}
        slotProps={{ htmlInput: { pattern: '-?[0-9]+', maxLength: 17 } }}
        helperText={t(
          'Numeric chat ID, including the minus sign for a group. Usernames are not supported.',
        )}
      />
      <TextField
        label={t('Topic ID (optional)')}
        value={thread}
        onChange={(event) => {
          setThread(event.target.value);
          onChange({ ...config, message_thread_id: Number(event.target.value) });
        }}
        slotProps={{ htmlInput: { inputMode: 'numeric', pattern: '[0-9]+', maxLength: 10 } }}
        helperText={t('For a forum topic. Leave empty to send without a topic.')}
      />
      <TextField
        label={t('Telegram API URL')}
        type="url"
        required
        value={config.api_url}
        onChange={(event) => onChange({ ...config, api_url: event.target.value })}
        slotProps={{ htmlInput: { maxLength: 2048 } }}
        helperText={t('Keep the default unless you run your own Telegram Bot API server.')}
      />
    </>
  );
}
