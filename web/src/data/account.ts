import { z } from 'zod';

const profileSchema = z.object({
  name: z
    .string()
    .trim()
    .refine((name) => Array.from(name).length >= 2, 'The name must contain at least 2 characters.')
    .refine(
      (name) => Array.from(name).length <= 60,
      'The name must contain between 2 and 60 characters.',
    ),
  email: z
    .string()
    .trim()
    .toLowerCase()
    .max(254, 'The email address must not exceed 254 characters.')
    .pipe(z.email('Enter a valid email address.')),
});
export type AccountProfile = z.infer<typeof profileSchema>;
export function validateProfile(input: AccountProfile): AccountProfile {
  const result = profileSchema.safeParse(input);
  if (!result.success) throw new Error(result.error.issues[0].message);
  return result.data;
}
// Return only the normalized profile; the caller sends the unchanged password to the API.
export function validateSignup(input: AccountProfile & { password: string; confirmation: string }) {
  const profile = validateProfile(input);
  validateNewPassword(input.password);
  if (input.password !== input.confirmation) throw new Error('The passwords do not match.');
  return profile;
}
// Validate only: credential verification and persistence belong to the authentication service.
export function validatePasswordChange(input: {
  currentPassword: string;
  password: string;
  confirmation: string;
}): void {
  if (!input.currentPassword) throw new Error('Enter your current password.');
  validateNewPassword(input.password);
  if (input.password !== input.confirmation) throw new Error('The passwords do not match.');
  if (input.password === input.currentPassword)
    throw new Error('The new password must be different from the current password.');
}

function validateNewPassword(password: string): void {
  const length = Array.from(password).length;
  if (length < 15 || length > 128) throw new Error('Use 15 to 128 characters for this password.');
}

export function initials(name: string) {
  return name
    .trim()
    .split(/\s+/)
    .slice(0, 2)
    .map((part) => part[0])
    .join('')
    .toUpperCase();
}
