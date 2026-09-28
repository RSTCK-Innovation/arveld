import { ZodError } from 'zod';
import { AppError } from '../data/errors';
import { i18n } from './index';

export function errorMessage(error: Error | string) {
  if (error instanceof AppError) return i18n.t(error.message, error.values);
  if (error instanceof ZodError) return i18n.t('Check the form values.');
  return i18n.t(typeof error === 'string' ? error : error.message);
}
