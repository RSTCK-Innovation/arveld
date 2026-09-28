// Keep message identity and interpolation values separate from the active UI language.
export class AppError extends Error {
  constructor(
    message: string,
    readonly values: Record<string, string | number>,
  ) {
    super(message);
    this.name = 'AppError';
  }
}
