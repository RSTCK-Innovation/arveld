import { expect, test } from 'bun:test';
import { createApiClient, defaultNotificationDelivery } from '../src/data/api';

test('chat webhooks reuse delivery settings with their incoming webhook URL', async () => {
  for (const type of ['discord', 'slack', 'msteams'] as const) {
    const input = {
      name: 'Operations',
      type,
      config: { url: 'https://hooks.example.test/events' },
      delivery: defaultNotificationDelivery,
    };
    const api = createApiClient(async (_, init) => {
      expect(JSON.parse(String(init.body))).toEqual(input);
      return Response.json({ id: type, ...input });
    });
    expect((await api.createNotification(input)).type).toBe(type);
    expect((await api.updateNotification(type, input)).type).toBe(type);
    await expect(
      api.createNotification({
        ...input,
        delivery: { ...input.delivery, repeat_interval_seconds: 31 },
      }),
    ).rejects.toThrow('multiple');
  }
});

test('notification channels use the common API without a fixture fallback', async () => {
  const input = {
    name: 'Operations',
    type: 'webhook' as const,
    config: { url: 'http://localhost:9099/events?token=test-only' },
    delivery: {
      group_by: 'resource' as const,
      group_wait_seconds: 0,
      group_interval_seconds: 60,
      repeat_interval_seconds: 3600,
    },
  };
  const channel = { id: 'channel/1', ...input };
  const calls: string[] = [];
  const controller = new AbortController();
  const api = createApiClient(async (path, init) => {
    calls.push(`${init.method} ${path}`);
    expect(init.credentials).toBe('same-origin');
    expect(init.cache).toBe('no-store');
    if (init.method === 'DELETE') return new Response(null, { status: 204 });
    if (init.method === 'POST' || init.method === 'PUT')
      expect(JSON.parse(String(init.body))).toEqual(input);
    else expect(init.signal).toBe(controller.signal);
    return Response.json(
      path === '/api/v1/notifications' && init.method === 'GET'
        ? { notifications: [channel] }
        : channel,
    );
  });
  expect(await api.createNotification(input)).toEqual(channel);
  expect(await api.listNotifications(controller.signal)).toEqual([channel]);
  expect(await api.notification(channel.id, controller.signal)).toEqual(channel);
  expect(await api.updateNotification(channel.id, input)).toEqual(channel);
  await api.deleteNotification(channel.id);
  expect(calls).toEqual([
    'POST /api/v1/notifications',
    'GET /api/v1/notifications',
    'GET /api/v1/notifications/channel%2F1',
    'PUT /api/v1/notifications/channel%2F1',
    'DELETE /api/v1/notifications/channel%2F1',
  ]);
  await expect(
    createApiClient(async () => new Response(null, { status: 503 })).listNotifications(),
  ).rejects.toThrow('Arveld is unavailable');
  await expect(
    createApiClient(async () => new Response(null, { status: 409 })).deleteNotification('used'),
  ).rejects.toThrow('Remove this channel from its alert rules');
  await expect(
    createApiClient(async () =>
      Response.json({ notifications: [{ ...channel, type: 'unknown' }] }),
    ).listNotifications(),
  ).rejects.toThrow('unexpected response');
});

test('Monitor rules persist destination assignments and expose native state', async () => {
  const input = {
    condition: 'failed' as const,
    for_seconds: 60,
    severity: 'critical' as const,
    notification_ids: ['channel'],
  };
  const rule = { id: 'rule', monitor_id: 'monitor', ...input };
  const native = {
    rule_id: 'rule',
    monitor_id: 'monitor',
    sync_status: 'applied' as const,
    state: 'firing' as const,
    health: 'ok' as const,
    last_evaluation: '2026-09-15T00:00:00Z',
  };
  const requests: string[] = [];
  const api = createApiClient(async (path, init) => {
    requests.push(`${init.method} ${path}`);
    if (init.method === 'DELETE') return new Response(null, { status: 204 });
    if (path.endsWith('/state')) return Response.json(native);
    if (init.method === 'POST' || init.method === 'PUT') {
      expect(JSON.parse(String(init.body))).toEqual(input);
      return Response.json(rule);
    }
    return Response.json(path.includes('/monitors/') ? { rules: [rule] } : rule);
  });
  expect(await api.createAlertRule('monitor', input)).toEqual(rule);
  expect(await api.listAlertRules('monitor')).toEqual([rule]);
  expect(await api.alertRule('rule')).toEqual(rule);
  expect(await api.updateAlertRule('rule', input)).toEqual(rule);
  expect(await api.alertRuleState('rule')).toEqual(native);
  await api.deleteAlertRule('rule');
  expect(requests).toEqual([
    'POST /api/v1/monitors/monitor/alert-rules',
    'GET /api/v1/monitors/monitor/alert-rules',
    'GET /api/v1/alert-rules/rule',
    'PUT /api/v1/alert-rules/rule',
    'GET /api/v1/alert-rules/rule/state',
    'DELETE /api/v1/alert-rules/rule',
  ]);
});

test('Monitor latency rules preserve a fractional millisecond threshold', async () => {
  const input = {
    condition: 'latency' as const,
    threshold: 500.5,
    for_seconds: 120,
    severity: 'warning' as const,
    notification_ids: [],
  };
  const rule = { id: 'latency', monitor_id: 'monitor', ...input };
  const api = createApiClient(async (_, init) => {
    expect(JSON.parse(String(init.body))).toEqual(input);
    return Response.json(rule);
  });
  expect(await api.createAlertRule('monitor', input)).toEqual(rule);
  expect(await api.updateAlertRule('latency', input)).toEqual(rule);
});

test('Agent resource rules use the same rule resource and native state', async () => {
  const input = {
    condition: 'cpu' as const,
    threshold: 85,
    for_seconds: 120,
    severity: 'warning' as const,
    notification_ids: [],
  };
  const rule = { id: 'cpu', agent_instance_uid: 'agent', ...input };
  const api = createApiClient(async (path, init) => {
    if (path.endsWith('/state'))
      return Response.json({ rule_id: 'cpu', agent_instance_uid: 'agent', sync_status: 'pending' });
    if (init.method === 'POST') {
      expect(JSON.parse(String(init.body))).toEqual(input);
      return Response.json(rule);
    }
    return Response.json({ rules: [rule] });
  });
  expect(await api.createAgentAlertRule('agent', input)).toEqual(rule);
  expect(await api.listAgentAlertRules('agent')).toEqual([rule]);
  expect((await api.alertRuleState('cpu')).agent_instance_uid).toBe('agent');
});

test('email channel settings use the common API and retain SMTP credentials', async () => {
  const input = {
    name: 'Email',
    type: 'email' as const,
    config: {
      smarthost: 'smtp.example.test:587',
      from: 'arveld@example.test',
      to: 'operations@example.test',
      tls_mode: 'starttls' as const,
      auth_username: 'arveld',
      auth_password: ' test-only password ',
    },
    delivery: defaultNotificationDelivery,
  };
  const channel = { id: 'email', ...input };
  const api = createApiClient(async (path, init) => {
    if (init.method === 'POST' || init.method === 'PUT')
      expect(JSON.parse(String(init.body))).toEqual(input);
    return Response.json(
      init.method === 'GET' && path.endsWith('/notifications')
        ? { notifications: [channel] }
        : channel,
    );
  });
  expect(await api.createNotification(input)).toEqual(channel);
  expect(await api.updateNotification(channel.id, input)).toEqual(channel);
  expect(await api.notification(channel.id)).toEqual(channel);
  expect(await api.listNotifications()).toEqual([channel]);
});

test('email channel saves explain a missing SMTP port before sending a request', async () => {
  const input = {
    name: 'Email',
    type: 'email' as const,
    config: {
      smarthost: 'smtp.example.test',
      from: 'arveld@example.test',
      to: 'operations@example.test',
      tls_mode: 'tls' as const,
      auth_username: 'smtp-user',
      auth_password: 'test-only password',
    },
    delivery: defaultNotificationDelivery,
  };
  let requests = 0;
  const api = createApiClient(async () => {
    requests++;
    return new Response(null, { status: 422 });
  });
  const message = 'Enter an SMTP server with a port from 1 to 65535, such as smtp.example.com:465.';
  for (const smarthost of [
    'smtp.example.test',
    ':465',
    'smtp.example.test:0',
    'smtp.example.test:65536',
    'smtp://smtp.example.test:465',
    'smtp.example.test:465/path',
    'mail host:465',
    '[not-an-ip]:465',
    '::1:465',
  ]) {
    const invalid = { ...input, config: { ...input.config, smarthost } };
    await expect(api.createNotification(invalid)).rejects.toThrow(message);
    await expect(api.updateNotification('email', invalid)).rejects.toThrow(message);
  }
  expect(requests).toBe(0);

  const acceptingApi = createApiClient(async (_, init) => {
    requests++;
    return Response.json({ id: 'email', ...JSON.parse(String(init.body)) });
  });
  for (const smarthost of ['smtp.example.test:465', 'localhost:587', '[::1]:25']) {
    const valid = { ...input, config: { ...input.config, smarthost } };
    expect((await acceptingApi.createNotification(valid)).config).toEqual(valid.config);
    expect((await acceptingApi.updateNotification('email', valid)).config).toEqual(valid.config);
  }
  expect(requests).toBe(6);
});

test('Telegram preserves signed chat IDs, bot credentials and forum topics', async () => {
  const input = {
    name: 'Operations Telegram',
    type: 'telegram' as const,
    config: {
      api_url: 'https://api.telegram.org',
      bot_token: '123456:Test_only-token',
      chat_id: -1001234567890,
      message_thread_id: 42,
    },
    delivery: defaultNotificationDelivery,
  };
  const api = createApiClient(async (_, init) => {
    expect(JSON.parse(String(init.body))).toEqual(input);
    return Response.json({ id: 'telegram', ...input });
  });
  expect(await api.createNotification(input)).toEqual({ id: 'telegram', ...input });
  expect(await api.updateNotification('telegram', input)).toEqual({ id: 'telegram', ...input });
  for (const chat_id of [0, -1.5, 9007199254740992]) {
    await expect(
      api.createNotification({ ...input, config: { ...input.config, chat_id } }),
    ).rejects.toThrow();
  }
});

test('PagerDuty uses Events API v2 credentials and endpoint settings', async () => {
  const input = {
    name: 'Operations PagerDuty',
    type: 'pagerduty' as const,
    config: {
      routing_key: '0123456789abcdef0123456789abcdef',
      url: 'https://events.pagerduty.com/v2/enqueue',
    },
    delivery: defaultNotificationDelivery,
  };
  const api = createApiClient(async (_, init) => {
    expect(JSON.parse(String(init.body))).toEqual(input);
    return Response.json({ id: 'pagerduty', ...input });
  });
  expect(await api.createNotification(input)).toEqual({ id: 'pagerduty', ...input });
  expect(await api.updateNotification('pagerduty', input)).toEqual({ id: 'pagerduty', ...input });
  await expect(
    api.createNotification({ ...input, config: { ...input.config, routing_key: '{{.Receiver}}' } }),
  ).rejects.toThrow();
});
