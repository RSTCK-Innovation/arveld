import { z } from 'zod';
import type { AccountProfile } from './account';

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

const profileSchema = z.object({ name: z.string(), email: z.string() }).strict();
const setupSchema = z.object({ required: z.boolean() }).strict();
const componentReadinessSchema = z.object({ ready: z.boolean() }).strict();
const readinessSchema = z
  .object({
    version: z.string().min(1),
    ready: z.boolean(),
    components: z
      .object({
        arveld: componentReadinessSchema,
        prometheus: componentReadinessSchema,
        alertmanager: componentReadinessSchema,
      })
      .strict(),
  })
  .strict();
const metricSampleSchema = z.tuple([
  z.number(),
  z.string().transform((value) => {
    const number = Number(value);
    return value.trim() !== '' && Number.isFinite(number) ? number : null;
  }),
]);
const metricLabelsSchema = z.record(z.string(), z.string());
const metricsVectorSchema = z.object({
  status: z.literal('success'),
  data: z.object({
    resultType: z.literal('vector'),
    result: z.array(z.object({ metric: metricLabelsSchema, value: metricSampleSchema })),
  }),
});
const metricsMatrixSchema = z.object({
  status: z.literal('success'),
  data: z.object({
    resultType: z.literal('matrix'),
    result: z.array(z.object({ metric: metricLabelsSchema, values: z.array(metricSampleSchema) })),
  }),
});
const agentSchema = z
  .object({
    instance_uid: z.string(),
    hostname: z.string().nullable(),
    version: z.string().nullable(),
    connected: z.boolean(),
    last_seen_at: z.string().nullable(),
  })
  .strict();
export type Agent = z.infer<typeof agentSchema>;
const configApplyStatusSchema = z.enum(['applying', 'applied', 'failed']);
export const httpMethods = ['GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS'] as const;
const httpValidationSchema = z
  .object({
    type: z.enum(['contains', 'not_contains', 'regex', 'json_path', 'min_size', 'max_size']),
    value: z.string().optional(),
    path: z.string().optional(),
    equals: z.string().optional(),
    size: z.number().int().min(0).max(4194304).optional(),
  })
  .strict();
export type HTTPValidation = z.infer<typeof httpValidationSchema>;
const httpMonitorSchema = z
  .object({
    id: z.string(),
    name: z.string(),
    agent_instance_uid: z.string(),
    endpoint: z.string(),
    method: z.enum(httpMethods),
    body: z.string().optional(),
    headers: z.record(z.string(), z.string()).optional(),
    skip_tls_verify: z.boolean().optional(),
    validations: z.array(httpValidationSchema).optional(),
    interval_seconds: z.number().int(),
    timeout_seconds: z.number().int(),
  })
  .strict();
export type HTTPMonitor = z.infer<typeof httpMonitorSchema>;
export type HTTPMonitorInput = Omit<HTTPMonitor, 'id'>;
const networkMonitorSchema = httpMonitorSchema.omit({
  method: true,
  body: true,
  headers: true,
  skip_tls_verify: true,
  validations: true,
});
const monitorSchema = z.discriminatedUnion('protocol', [
  httpMonitorSchema.extend({ protocol: z.literal('http') }),
  networkMonitorSchema.extend({ protocol: z.literal('tcp') }),
  networkMonitorSchema.extend({
    protocol: z.literal('icmp'),
    ping_count: z.number().int().min(1).max(10),
  }),
  networkMonitorSchema.extend({
    protocol: z.literal('dns'),
    dns_server: z.string(),
    record_type: z.enum(['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS']),
    transport: z.enum(['udp', 'tcp', 'tcp-tls']),
  }),
]);
export type Monitor = z.infer<typeof monitorSchema>;
export type MonitorInput =
  | Omit<Extract<Monitor, { protocol: 'http' }>, 'id'>
  | Omit<Extract<Monitor, { protocol: 'tcp' }>, 'id'>
  | Omit<Extract<Monitor, { protocol: 'icmp' }>, 'id'>
  | Omit<Extract<Monitor, { protocol: 'dns' }>, 'id'>;
const configReportSchema = z
  .object({
    revision: z.number().int().positive().nullable(),
    config_hash: z.string(),
    status: configApplyStatusSchema,
    error_message: z.string(),
    reported_at: z.string(),
  })
  .strict();
const configStatusSchema = z
  .object({
    state: configApplyStatusSchema,
    desired: z.object({ revision: z.number().int().positive(), config_hash: z.string() }).strict(),
    reported: configReportSchema.nullable(),
    last_failure: configReportSchema.optional(),
    last_working: configReportSchema.optional(),
  })
  .strict();
export type ConfigApplyStatus = z.infer<typeof configApplyStatusSchema>;
const configRevisionSchema = z
  .object({
    revision: z.number().int().positive(),
    created_at: z.iso.datetime({ offset: true }).nullable(),
  })
  .strict();
const apiKeySchema = z
  .object({
    id: z.string(),
    name: z.string(),
    prefix: z.string(),
    permission: z.enum(['read', 'write']),
    createdAt: z.string(),
    expiresAt: z.string().nullable(),
    revokedAt: z.string().nullable(),
  })
  .strict();
export type ApiKey = z.infer<typeof apiKeySchema>;
export const apiKeyInputSchema = z.object({
  name: z
    .string()
    .trim()
    .refine(
      (name) => Array.from(name).length >= 2 && Array.from(name).length <= 80,
      'The key name must contain 2 to 80 characters.',
    ),
  permission: z.enum(['read', 'write']),
  expiresDays: z
    .union([z.literal(1), z.literal(7), z.literal(30), z.literal(90), z.literal(365)])
    .nullable(),
});
export type ApiKeyInput = z.infer<typeof apiKeyInputSchema>;
const agentKeySchema = z
  .object({
    id: z.string(),
    name: z.string(),
    prefix: z.string(),
    createdAt: z.string(),
    revokedAt: z.string().nullable(),
  })
  .strict();
export type AgentKey = z.infer<typeof agentKeySchema>;
export const agentKeyInputSchema = apiKeyInputSchema.pick({ name: true }).strict();
export type AgentKeyInput = z.infer<typeof agentKeyInputSchema>;
const notificationDeliverySchema = z
  .object({
    group_by: z.enum(['rule', 'resource']),
    group_wait_seconds: z.number().int().min(0).max(3600),
    group_interval_seconds: z.number().int().min(1).max(86400),
    repeat_interval_seconds: z.number().int().min(1).max(432000),
  })
  .strict()
  .refine(
    (value) =>
      value.repeat_interval_seconds >= value.group_interval_seconds &&
      value.repeat_interval_seconds % value.group_interval_seconds === 0,
    'The repeat interval must be a multiple of the update interval.',
  );
export const defaultNotificationDelivery: z.infer<typeof notificationDeliverySchema> = {
  group_by: 'rule',
  group_wait_seconds: 5,
  group_interval_seconds: 30,
  repeat_interval_seconds: 14400,
};
const webhookNotificationSchema = z
  .object({
    name: z
      .string()
      .trim()
      .refine(
        (name) =>
          Array.from(name).length >= 2 && Array.from(name).length <= 80 && !name.includes('\0'),
        'The name must contain 2 to 80 characters.',
      ),
    type: z.enum(['webhook', 'discord', 'slack', 'msteams']),
    delivery: notificationDeliverySchema,
    config: z
      .object({
        url: z
          .string()
          .max(2048)
          .refine((value) => {
            try {
              const url = new URL(value);
              return (
                /^https?:\/\//.test(value) &&
                !/[#\s]/.test(value) &&
                !!url.hostname &&
                !url.username &&
                !url.password &&
                (!url.port || Number(url.port) > 0)
              );
            } catch {
              return false;
            }
          }, 'Enter an HTTP or HTTPS URL without credentials or a fragment.'),
      })
      .strict(),
  })
  .strict();
const mailboxSchema = z
  .string()
  .trim()
  .min(3)
  .max(254)
  .refine(
    (value) =>
      value.includes('@') &&
      !/[\s<>{}]/.test(value) &&
      Array.from(value).every((char) => char.charCodeAt(0) > 32 && char.charCodeAt(0) < 127),
    'Enter one email address without a display name.',
  );
export const smtpServerSchema = z
  .string()
  .trim()
  .refine((value) => {
    const match = /^(?:\[([^\]]+)\]|([^:]+)):([0-9]+)$/.exec(value);
    if (!match) return false;
    const host = match[1] ?? match[2];
    const port = Number(match[3]);
    if (host.length > 253 || port < 1 || port > 65535) return false;
    if (match[1]) return z.ipv6().safeParse(host).success;
    return (
      !/[\s/@?#\\%[\]]/.test(host) &&
      Array.from(host).every((char) => char.charCodeAt(0) > 32 && char.charCodeAt(0) < 127)
    );
  }, 'Enter an SMTP server with a port from 1 to 65535, such as smtp.example.com:465.');
const emailConfigSchema = z
  .object({
    smarthost: smtpServerSchema,
    from: mailboxSchema,
    to: mailboxSchema,
    tls_mode: z.enum(['starttls', 'tls', 'none']),
    auth_username: z.string().max(254),
    auth_password: z.string().max(1024),
  })
  .strict()
  .refine(
    (value) =>
      (!value.auth_username && !value.auth_password) ||
      (value.tls_mode !== 'none' && !!value.auth_username && !!value.auth_password),
    'Provide both SMTP credentials and enable encryption, or leave both empty.',
  );
export type EmailNotificationConfig = z.infer<typeof emailConfigSchema>;
const telegramConfigSchema = z
  .object({
    api_url: webhookNotificationSchema.shape.config.shape.url.refine(
      (value) => !value.includes('?'),
      'Enter an API base URL without a query.',
    ),
    bot_token: z
      .string()
      .max(512)
      .regex(/^[0-9]+:[A-Za-z0-9_-]+$/, 'Enter the bot token provided by BotFather.'),
    chat_id: z
      .number()
      .int()
      .min(-Number.MAX_SAFE_INTEGER)
      .max(Number.MAX_SAFE_INTEGER)
      .refine((value) => value !== 0, 'Enter a nonzero chat ID.'),
    message_thread_id: z.number().int().min(0).max(2147483647),
  })
  .strict();
export type TelegramNotificationConfig = z.infer<typeof telegramConfigSchema>;
const pagerDutyConfigSchema = z
  .object({
    url: webhookNotificationSchema.shape.config.shape.url,
    routing_key: z
      .string()
      .min(1)
      .max(512)
      .refine(
        (value) =>
          !/[{}]/.test(value) &&
          Array.from(value).every((char) => char.charCodeAt(0) > 32 && char.charCodeAt(0) < 127),
        'Enter a routing key without spaces or templates.',
      ),
  })
  .strict();
export type PagerDutyNotificationConfig = z.infer<typeof pagerDutyConfigSchema>;
const pagerDutyNotificationSchema = webhookNotificationSchema
  .omit({ type: true, config: true })
  .extend({
    type: z.literal('pagerduty'),
    config: pagerDutyConfigSchema,
  });
const telegramNotificationSchema = webhookNotificationSchema
  .omit({ type: true, config: true })
  .extend({
    type: z.literal('telegram'),
    config: telegramConfigSchema,
  });
const emailNotificationSchema = webhookNotificationSchema
  .omit({ type: true, config: true })
  .extend({
    type: z.literal('email'),
    config: emailConfigSchema,
  });
export const notificationInputSchema = z.discriminatedUnion('type', [
  webhookNotificationSchema,
  emailNotificationSchema,
  telegramNotificationSchema,
  pagerDutyNotificationSchema,
]);
const notificationSchema = z.discriminatedUnion('type', [
  webhookNotificationSchema.extend({ id: z.string() }),
  emailNotificationSchema.extend({ id: z.string() }),
  telegramNotificationSchema.extend({ id: z.string() }),
  pagerDutyNotificationSchema.extend({ id: z.string() }),
]);
export type NotificationInput = z.infer<typeof notificationInputSchema>;
export type NotificationChannel = z.infer<typeof notificationSchema>;
export const silenceInputSchema = z
  .object({
    duration_seconds: z.number().int().min(1).max(604800),
    starts_at: z.iso
      .datetime({ offset: true })
      .refine((value) => Date.parse(value) > Date.now())
      .optional(),
    comment: z
      .string()
      .trim()
      .min(1)
      .refine((value) => new TextEncoder().encode(value).length <= 1024 && !value.includes('\0')),
  })
  .strict()
  .refine(
    (value) =>
      !value.starts_at ||
      Date.parse(value.starts_at) + value.duration_seconds * 1000 <=
        Date.parse('9999-12-31T23:59:59.999Z'),
  );
const monitorSilenceSchema = z
  .object({
    id: z.string().min(1),
    monitor_id: z.string().min(1),
    starts_at: z.iso.datetime({ offset: true }),
    ends_at: z.iso.datetime({ offset: true }),
    created_by: z.string(),
    comment: z.string(),
    state: z.enum(['pending', 'active', 'expired']),
  })
  .strict();
const agentSilenceSchema = monitorSilenceSchema
  .omit({ monitor_id: true })
  .extend({ agent_instance_uid: z.string().min(1) });
const silenceSchema = z.union([monitorSilenceSchema, agentSilenceSchema]);
export type SilenceInput = z.infer<typeof silenceInputSchema>;
export type MonitorSilence = z.infer<typeof monitorSilenceSchema>;
export type Silence = z.infer<typeof silenceSchema>;
export const alertRuleInputSchema = z
  .object({
    condition: z.enum(['failed', 'no_data', 'latency', 'cpu', 'memory', 'disk']),
    threshold: z.number().positive().max(60000).optional(),
    for_seconds: z.number().int().min(1).max(86400),
    severity: z.enum(['info', 'warning', 'critical']),
    notification_ids: z
      .array(z.string().min(1))
      .max(64)
      .refine((ids) => new Set(ids).size === ids.length),
  })
  .strict()
  .refine((rule) =>
    ['failed', 'no_data'].includes(rule.condition)
      ? rule.threshold === undefined
      : rule.threshold !== undefined &&
        rule.threshold <= (rule.condition === 'latency' ? 60000 : 100),
  );
const alertRuleSchema = alertRuleInputSchema
  .safeExtend({
    id: z.string(),
    monitor_id: z.string().min(1).optional(),
    agent_instance_uid: z.string().min(1).optional(),
  })
  .refine((rule) => (rule.monitor_id !== undefined) !== (rule.agent_instance_uid !== undefined));
export type AlertRuleInput = z.infer<typeof alertRuleInputSchema>;
export type AlertRule = z.infer<typeof alertRuleSchema>;
const alertRuleStateSchema = z
  .object({
    rule_id: z.string(),
    monitor_id: z.string().optional(),
    agent_instance_uid: z.string().optional(),
    sync_status: z.enum(['pending', 'applied']),
    state: z.enum(['unknown', 'inactive', 'pending', 'firing']).optional(),
    health: z.enum(['unknown', 'ok', 'err']).optional(),
    last_error: z.string().optional(),
    last_evaluation: z.iso.datetime({ offset: true }).optional(),
  })
  .strict();

const incidentSchema = z
  .object({
    id: z.string(),
    rule_id: z.string(),
    owner_kind: z.enum(['monitor', 'agent']),
    owner_id: z.string(),
    owner_name: z.string(),
    condition: alertRuleInputSchema.shape.condition,
    threshold: z.number().optional(),
    for_seconds: z.number().int(),
    severity: alertRuleInputSchema.shape.severity,
    status: z.enum(['open', 'closed']),
    opened_at: z.iso.datetime({ offset: true }),
    last_evaluated_at: z.iso.datetime({ offset: true }),
    closed_at: z.iso.datetime({ offset: true }).optional(),
    close_reason: z.enum(['condition_ended', 'rule_changed', 'rule_removed']).optional(),
    acknowledged_at: z.iso.datetime({ offset: true }).optional(),
    acknowledged_by: z.string().optional(),
  })
  .strict();
const incidentPageSchema = z
  .object({
    incidents: z.array(incidentSchema),
    total: z.number().int().nonnegative(),
    next_before: z.string().optional(),
  })
  .strict();
export type Incident = z.infer<typeof incidentSchema>;
export type IncidentFilters = {
  status?: 'open' | 'closed';
  severity?: AlertRuleInput['severity'];
  owner_kind?: 'monitor' | 'agent';
  owner_id?: string;
  limit?: number;
  before?: string;
};

const errorMessages: Record<number, string> = {
  400: 'Check the form values.',
  401: 'Your session has expired. Sign in again.',
  403: 'This action is not allowed.',
  404: 'The requested resource was not found.',
  409: 'An administrator account already exists. Sign in.',
  413: 'The request is too large.',
  422: 'Check the form values.',
  429: 'Too many attempts. Try again in a moment.',
};
type Fetch = (path: string, init: RequestInit) => Promise<Response>;
type RequestOptions = {
  method?: string;
  body?: unknown;
  signal?: AbortSignal;
  errors?: Record<number, string>;
};

export function createApiClient(fetchRequest: Fetch = fetch) {
  async function request(path: string, options: RequestOptions = {}) {
    const form = options.body instanceof URLSearchParams;
    let response: Response;
    try {
      response = await fetchRequest(`/api/v1${path}`, {
        method: options.method ?? 'GET',
        credentials: 'same-origin',
        cache: 'no-store',
        signal: options.signal,
        headers:
          options.body === undefined
            ? undefined
            : { 'Content-Type': form ? 'application/x-www-form-urlencoded' : 'application/json' },
        body:
          options.body === undefined
            ? undefined
            : form
              ? String(options.body)
              : JSON.stringify(options.body),
      });
    } catch (error) {
      if (options.signal?.aborted) throw error;
      throw new Error('Unable to reach Arveld. Check your connection.');
    }
    if (!response.ok) {
      throw new ApiError(
        response.status,
        options.errors?.[response.status] ??
          errorMessages[response.status] ??
          'Arveld is unavailable. Try again.',
      );
    }
    return response;
  }
  async function read<T>(response: Response, schema: z.ZodType<T>): Promise<T> {
    try {
      return schema.parse(await response.json());
    } catch {
      throw new Error('Arveld returned an unexpected response.');
    }
  }
  return {
    async retention(signal?: AbortSignal) {
      return read(
        await request('/settings/retention', { signal }),
        z.object({ storage_retention: z.string() }).strict(),
      );
    },
    async readiness(signal?: AbortSignal) {
      let response: Response;
      try {
        response = await fetchRequest('/readyz', {
          method: 'GET',
          credentials: 'same-origin',
          cache: 'no-store',
          signal,
        });
      } catch (error) {
        if (signal?.aborted) throw error;
        throw new Error('Unable to reach Arveld. Check your connection.');
      }
      // A native 503 contains the component diagnosis, unlike management API errors.
      if (response.status !== 200 && response.status !== 503) {
        throw new ApiError(
          response.status,
          errorMessages[response.status] ?? 'Arveld is unavailable. Try again.',
        );
      }
      return read(response, readinessSchema);
    },
    async listSilences(signal?: AbortSignal) {
      const data = await read(
        await request('/silences', { signal }),
        z.object({ silences: z.array(silenceSchema) }).strict(),
      );
      return data.silences;
    },
    async listAgentSilences(instanceUID: string, signal?: AbortSignal) {
      const data = await read(
        await request(`/agents/${encodeURIComponent(instanceUID)}/silences`, { signal }),
        z
          .object({
            silences: z.array(
              agentSilenceSchema.extend({ agent_instance_uid: z.literal(instanceUID) }),
            ),
          })
          .strict(),
      );
      return data.silences;
    },
    async listMonitorSilences(monitorId: string, signal?: AbortSignal) {
      const data = await read(
        await request(`/monitors/${encodeURIComponent(monitorId)}/silences`, { signal }),
        z
          .object({
            silences: z.array(monitorSilenceSchema.extend({ monitor_id: z.literal(monitorId) })),
          })
          .strict(),
      );
      return data.silences;
    },
    async createMonitorSilence(monitorId: string, input: SilenceInput) {
      return read(
        await request(`/monitors/${encodeURIComponent(monitorId)}/silences`, {
          method: 'POST',
          body: silenceInputSchema.parse(input),
        }),
        z.object({ id: z.string().min(1), monitor_id: z.literal(monitorId) }).strict(),
      );
    },
    async createAgentSilence(instanceUID: string, input: SilenceInput) {
      return read(
        await request(`/agents/${encodeURIComponent(instanceUID)}/silences`, {
          method: 'POST',
          body: silenceInputSchema.parse(input),
        }),
        z.object({ id: z.string().min(1), agent_instance_uid: z.literal(instanceUID) }).strict(),
      );
    },
    async cancelMonitorSilence(monitorId: string, silenceId: string) {
      await request(
        `/monitors/${encodeURIComponent(monitorId)}/silences/${encodeURIComponent(silenceId)}`,
        { method: 'DELETE' },
      );
    },
    async cancelAgentSilence(instanceUID: string, silenceId: string) {
      await request(
        `/agents/${encodeURIComponent(instanceUID)}/silences/${encodeURIComponent(silenceId)}`,
        { method: 'DELETE' },
      );
    },
    async listNotifications(signal?: AbortSignal) {
      const data = await read(
        await request('/notifications', { signal }),
        z.object({ notifications: z.array(notificationSchema) }).strict(),
      );
      return data.notifications;
    },
    async notification(id: string, signal?: AbortSignal) {
      try {
        return await read(
          await request(`/notifications/${encodeURIComponent(id)}`, { signal }),
          notificationSchema,
        );
      } catch (error) {
        if (error instanceof ApiError && error.status === 404) return null;
        throw error;
      }
    },
    async createNotification(input: NotificationInput) {
      return read(
        await request('/notifications', {
          method: 'POST',
          body: notificationInputSchema.parse(input),
        }),
        notificationSchema,
      );
    },
    async updateNotification(id: string, input: NotificationInput) {
      return read(
        await request(`/notifications/${encodeURIComponent(id)}`, {
          method: 'PUT',
          body: notificationInputSchema.parse(input),
        }),
        notificationSchema,
      );
    },
    async deleteNotification(id: string) {
      await request(`/notifications/${encodeURIComponent(id)}`, {
        method: 'DELETE',
        errors: { 409: 'Remove this channel from its alert rules before deleting it.' },
      });
    },
    async listIncidents(filters: IncidentFilters = {}, signal?: AbortSignal) {
      const parameters = new URLSearchParams();
      for (const [name, value] of Object.entries(filters))
        if (value !== undefined) parameters.set(name, String(value));
      return read(
        await request(`/incidents${parameters.size ? `?${parameters}` : ''}`, { signal }),
        incidentPageSchema,
      );
    },
    async incident(id: string, signal?: AbortSignal) {
      try {
        return await read(
          await request(`/incidents/${encodeURIComponent(id)}`, { signal }),
          incidentSchema,
        );
      } catch (error) {
        if (error instanceof ApiError && error.status === 404) return null;
        throw error;
      }
    },
    async acknowledgeIncident(id: string) {
      return read(
        await request(`/incidents/${encodeURIComponent(id)}/acknowledgment`, {
          method: 'POST',
          errors: { 409: 'This incident is already closed.' },
        }),
        incidentSchema,
      );
    },
    async listAlertRules(monitorId: string, signal?: AbortSignal) {
      const data = await read(
        await request(`/monitors/${encodeURIComponent(monitorId)}/alert-rules`, { signal }),
        z.object({ rules: z.array(alertRuleSchema) }).strict(),
      );
      return data.rules;
    },
    async listAgentAlertRules(agentId: string, signal?: AbortSignal) {
      const data = await read(
        await request(`/agents/${encodeURIComponent(agentId)}/alert-rules`, { signal }),
        z.object({ rules: z.array(alertRuleSchema) }).strict(),
      );
      return data.rules;
    },
    async alertRule(id: string, signal?: AbortSignal) {
      try {
        return await read(
          await request(`/alert-rules/${encodeURIComponent(id)}`, { signal }),
          alertRuleSchema,
        );
      } catch (error) {
        if (error instanceof ApiError && error.status === 404) return null;
        throw error;
      }
    },
    async createAlertRule(monitorId: string, input: AlertRuleInput) {
      return read(
        await request(`/monitors/${encodeURIComponent(monitorId)}/alert-rules`, {
          method: 'POST',
          body: alertRuleInputSchema.parse(input),
        }),
        alertRuleSchema,
      );
    },
    async createAgentAlertRule(agentId: string, input: AlertRuleInput) {
      return read(
        await request(`/agents/${encodeURIComponent(agentId)}/alert-rules`, {
          method: 'POST',
          body: alertRuleInputSchema.parse(input),
        }),
        alertRuleSchema,
      );
    },
    async updateAlertRule(id: string, input: AlertRuleInput) {
      return read(
        await request(`/alert-rules/${encodeURIComponent(id)}`, {
          method: 'PUT',
          body: alertRuleInputSchema.parse(input),
        }),
        alertRuleSchema,
      );
    },
    async deleteAlertRule(id: string) {
      await request(`/alert-rules/${encodeURIComponent(id)}`, { method: 'DELETE' });
    },
    async alertRuleState(id: string, signal?: AbortSignal) {
      return read(
        await request(`/alert-rules/${encodeURIComponent(id)}/state`, { signal }),
        alertRuleStateSchema,
      );
    },
    async listMonitors(signal?: AbortSignal) {
      const result = await read(
        await request('/monitors', { signal }),
        z.object({ monitors: z.array(monitorSchema) }).strict(),
      );
      return result.monitors;
    },
    async monitor(id: string, signal?: AbortSignal) {
      try {
        return await read(
          await request(`/monitors/${encodeURIComponent(id)}`, { signal }),
          monitorSchema,
        );
      } catch (error) {
        if (error instanceof ApiError && error.status === 404) return null;
        throw error;
      }
    },
    async createMonitor(input: MonitorInput) {
      const { protocol, ...body } = input;
      const response = await request(`/monitors/${protocol}`, { method: 'POST', body });
      if (protocol === 'http')
        return monitorSchema.parse({ ...(await read(response, httpMonitorSchema)), protocol });
      return read(response, monitorSchema);
    },
    async deleteMonitor(id: string) {
      await request(`/monitors/${encodeURIComponent(id)}`, { method: 'DELETE' });
    },
    async updateMonitor(id: string, input: MonitorInput) {
      const { protocol: _protocol, ...body } = input;
      return read(
        await request(`/monitors/${encodeURIComponent(id)}`, { method: 'PUT', body }),
        monitorSchema,
      );
    },
    async listHTTPMonitors(signal?: AbortSignal) {
      const result = await read(
        await request('/monitors/http', { signal }),
        z.object({ monitors: z.array(httpMonitorSchema) }).strict(),
      );
      return result.monitors;
    },
    async httpMonitor(id: string, signal?: AbortSignal) {
      try {
        return await read(
          await request(`/monitors/http/${encodeURIComponent(id)}`, { signal }),
          httpMonitorSchema,
        );
      } catch (error) {
        if (error instanceof ApiError && error.status === 404) return null;
        throw error;
      }
    },
    async createHTTPMonitor(input: HTTPMonitorInput) {
      return read(
        await request('/monitors/http', { method: 'POST', body: input }),
        httpMonitorSchema,
      );
    },
    async queryMetrics(expression: string, signal?: AbortSignal, lookback?: number) {
      const params = new URLSearchParams({ query: expression });
      if (lookback !== undefined) params.set('lookback_delta', String(lookback));
      const result = await read(
        await request('/metrics/query', { method: 'POST', body: params, signal }),
        metricsVectorSchema,
      );
      return result.data.result;
    },
    async queryMetricsRange(
      expression: string,
      range: { start: number; end: number; step: number; lookback?: number },
      signal?: AbortSignal,
    ) {
      const params = new URLSearchParams({
        query: expression,
        start: String(range.start),
        end: String(range.end),
        step: String(range.step),
      });
      if (range.lookback !== undefined) params.set('lookback_delta', String(range.lookback));
      const result = await read(
        await request('/metrics/query_range', { method: 'POST', body: params, signal }),
        metricsMatrixSchema,
      );
      return result.data.result;
    },
    async listAgents(signal?: AbortSignal) {
      const result = await read(
        await request('/agents', { signal }),
        z.object({ agents: z.array(agentSchema) }).strict(),
      );
      return result.agents;
    },
    async agentConfigStatus(instanceUID: string, signal?: AbortSignal) {
      try {
        return await read(
          await request(`/agents/${encodeURIComponent(instanceUID)}/config/status`, { signal }),
          configStatusSchema,
        );
      } catch (error) {
        if (error instanceof ApiError && error.status === 404) return null;
        throw error;
      }
    },
    async agentConfigRevisions(instanceUID: string, signal?: AbortSignal) {
      const result = await read(
        await request(`/agents/${encodeURIComponent(instanceUID)}/config/revisions`, { signal }),
        z.object({ revisions: z.array(configRevisionSchema) }).strict(),
      );
      return result.revisions;
    },
    async agentConfigYAML(instanceUID: string, revision: number, signal?: AbortSignal) {
      const response = await request(
        `/agents/${encodeURIComponent(instanceUID)}/config/revisions/${revision}`,
        { signal },
      );
      return response.text();
    },
    async setup(signal?: AbortSignal) {
      return read(await request('/auth/setup', { signal }), setupSchema);
    },
    async session(signal?: AbortSignal): Promise<AccountProfile | null> {
      try {
        return await read(await request('/auth/session', { signal }), profileSchema);
      } catch (error) {
        if (error instanceof ApiError && error.status === 401) return null;
        throw error;
      }
    },
    async createAccount({ name, email, password }: AccountProfile & { password: string }) {
      await request('/auth/setup', { method: 'POST', body: { name, email, password } });
    },
    async login({ email, password }: { email: string; password: string }) {
      await request('/auth/login', {
        method: 'POST',
        body: { email, password },
        errors: { 401: 'Incorrect email address or password.' },
      });
    },
    async logout() {
      await request('/auth/logout', { method: 'POST' });
    },
    async saveProfile({ name, email }: AccountProfile) {
      await request('/account/profile', { method: 'PUT', body: { name, email } });
    },
    async changePassword({
      currentPassword,
      password,
    }: {
      currentPassword: string;
      password: string;
    }) {
      await request('/account/password', {
        method: 'PUT',
        body: { currentPassword, password },
        errors: { 403: 'The current password is incorrect.' },
      });
    },
    async listAgentKeys(signal?: AbortSignal) {
      const result = await read(
        await request('/agentkeys', { signal }),
        z.object({ keys: z.array(agentKeySchema) }).strict(),
      );
      return result.keys;
    },
    async createAgentKey(input: AgentKeyInput) {
      const body = agentKeyInputSchema.parse(input);
      return read(
        await request('/agentkeys', { method: 'POST', body }),
        z.object({ key: agentKeySchema, token: z.string().min(1) }).strict(),
      );
    },
    async revokeAgentKey(id: string) {
      await request(`/agentkeys/${encodeURIComponent(id)}`, { method: 'DELETE' });
    },
    async listKeys(signal?: AbortSignal) {
      const result = await read(
        await request('/apikeys', { signal }),
        z.object({ keys: z.array(apiKeySchema) }).strict(),
      );
      return result.keys;
    },
    async createKey({ name, permission, expiresDays }: ApiKeyInput) {
      return read(
        await request('/apikeys', { method: 'POST', body: { name, permission, expiresDays } }),
        z.object({ key: apiKeySchema, token: z.string().min(1) }).strict(),
      );
    },
    async revokeKey(id: string) {
      await request(`/apikeys/${encodeURIComponent(id)}`, { method: 'DELETE' });
    },
  };
}

export const api = createApiClient();
