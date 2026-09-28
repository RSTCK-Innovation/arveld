import { expect, test } from 'bun:test';
import { createApiClient } from '../src/data/api';

test('account actions use the controller and browser cookies with the expected payloads', async () => {
  const replies = [
    Response.json({ required: true }),
    new Response(null, { status: 201 }),
    new Response('Unauthorized', { status: 401 }),
    new Response(null, { status: 204 }),
    Response.json({ name: 'Camille', email: 'camille@example.com' }),
    new Response(null, { status: 204 }),
    new Response('Forbidden', { status: 403 }),
    new Response(null, { status: 204 }),
    new Response('Unauthorized', { status: 401 }),
    new Response(null, { status: 204 }),
    new Response('internal database details', { status: 500 }),
  ];
  const requests: { path: string; method: string; body: unknown }[] = [];
  const api = createApiClient(async (path, init) => {
    expect(init.credentials).toBe('same-origin');
    expect(init.cache).toBe('no-store');
    const headers = new Headers(init.headers);
    expect(headers.has('Authorization')).toBe(false);
    if (init.body) expect(headers.get('Content-Type')).toBe('application/json');
    requests.push({
      path,
      method: init.method ?? 'GET',
      body: init.body ? JSON.parse(String(init.body)) : null,
    });
    const response = replies.shift();
    if (!response) throw new Error('Unexpected request');
    return response;
  });
  const profile = { name: 'Camille', email: 'camille@example.com' };
  expect(await api.setup()).toEqual({ required: true });
  await api.createAccount({ ...profile, password: '  original password  ' });
  await expect(api.login({ email: profile.email, password: 'wrong' })).rejects.toThrow(
    'Incorrect email address or password.',
  );
  await api.login({ email: profile.email, password: '  original password  ' });
  expect(await api.session()).toEqual(profile);
  await api.saveProfile({ ...profile, name: 'Camille Martin' });
  await expect(
    api.changePassword({ currentPassword: 'wrong', password: 'new long password' }),
  ).rejects.toThrow('The current password is incorrect.');
  await api.changePassword({
    currentPassword: '  original password  ',
    password: 'new long password',
  });
  expect(await api.session()).toBeNull();
  await api.logout();
  await expect(api.session()).rejects.toThrow('Arveld is unavailable. Try again.');
  expect(requests).toEqual([
    { path: '/api/v1/auth/setup', method: 'GET', body: null },
    {
      path: '/api/v1/auth/setup',
      method: 'POST',
      body: { ...profile, password: '  original password  ' },
    },
    {
      path: '/api/v1/auth/login',
      method: 'POST',
      body: { email: profile.email, password: 'wrong' },
    },
    {
      path: '/api/v1/auth/login',
      method: 'POST',
      body: { email: profile.email, password: '  original password  ' },
    },
    { path: '/api/v1/auth/session', method: 'GET', body: null },
    {
      path: '/api/v1/account/profile',
      method: 'PUT',
      body: { ...profile, name: 'Camille Martin' },
    },
    {
      path: '/api/v1/account/password',
      method: 'PUT',
      body: { currentPassword: 'wrong', password: 'new long password' },
    },
    {
      path: '/api/v1/account/password',
      method: 'PUT',
      body: { currentPassword: '  original password  ', password: 'new long password' },
    },
    { path: '/api/v1/auth/session', method: 'GET', body: null },
    { path: '/api/v1/auth/logout', method: 'POST', body: null },
    { path: '/api/v1/auth/session', method: 'GET', body: null },
  ]);
  expect(replies).toHaveLength(0);
});
