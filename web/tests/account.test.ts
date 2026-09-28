import { expect, test } from 'bun:test';
import { validatePasswordChange, validateProfile, validateSignup } from '../src/data/account';

const profile = { name: 'Camille Martin', email: 'camille@example.com' };

test('account forms validate profiles and confirmation before sending credentials', () => {
  expect(
    validateSignup({
      ...profile,
      password: 'frontend-password',
      confirmation: 'frontend-password',
    }),
  ).toEqual(profile);
  expect(() =>
    validateSignup({ ...profile, password: 'long-enough-password', confirmation: 'different' }),
  ).toThrow('match');
  expect(() => validateProfile({ ...profile, email: 'bad' })).toThrow('email');
  expect(() => validateProfile({ ...profile, name: ' ' })).toThrow('name');
  expect(validateProfile({ name: '  Camille Martin  ', email: 'CAMILLE@example.com' })).toEqual(
    profile,
  );
});

test('password changes require the current password and a confirmed new value', () => {
  const input = {
    currentPassword: 'current-password',
    password: 'new-password-long-enough',
    confirmation: 'new-password-long-enough',
  };
  expect(() => validatePasswordChange({ ...input, currentPassword: '' })).toThrow('current');
  expect(() =>
    validatePasswordChange({ ...input, password: 'short', confirmation: 'short' }),
  ).toThrow('15 to 128 characters');
  expect(() => validatePasswordChange({ ...input, confirmation: 'different-password' })).toThrow(
    'match',
  );
  expect(() =>
    validatePasswordChange({
      ...input,
      password: input.currentPassword,
      confirmation: input.currentPassword,
    }),
  ).toThrow('different');
  expect(validatePasswordChange(input)).toBeUndefined();
});

test('new passwords accept 15 to 128 Unicode characters without modifying their contents', () => {
  for (const password of ['a'.repeat(14), '🔑'.repeat(14), 'a'.repeat(129), '🔑'.repeat(129)]) {
    expect(() => validateSignup({ ...profile, password, confirmation: password })).toThrow(
      '15 to 128 characters',
    );
  }
  for (const password of ['a'.repeat(15), '🔑'.repeat(15), '🔑'.repeat(128), '  long password  ']) {
    expect(validateSignup({ ...profile, password, confirmation: password })).toEqual(profile);
  }
});
