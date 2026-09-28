import { expect, test } from 'bun:test';
import { getContrastRatio } from '@mui/material/styles';
import { makeTheme } from '../src/theme';

test('the Arveld theme centralizes its identity and preserves readable contrast in both languages', () => {
  const french = makeTheme('fr');
  const english = makeTheme('en');
  for (const theme of [french, english]) {
    expect(theme.palette.primary.main).toBe('#314b77');
    expect(theme.palette.background.default).toBe('#f8f7f4');
    expect(theme.typography.fontFamily).toContain('Inter Variable');
    expect(theme.typography.h4.fontFamily).toContain('Manrope Variable');
    expect(theme.typography.button.textTransform).toBe('none');
    expect(theme.components?.MuiButton?.defaultProps?.disableElevation).toBe(true);
    expect(theme.components?.MuiTextField?.defaultProps?.size).toBe('small');
    for (const color of ['primary', 'success', 'warning', 'error', 'info'] as const) {
      const palette = theme.palette[color];
      expect(getContrastRatio(palette.main, palette.contrastText)).toBeGreaterThanOrEqual(4.5);
    }
    expect(
      getContrastRatio(theme.palette.text.secondary, theme.palette.background.default),
    ).toBeGreaterThanOrEqual(4.5);
  }
  for (const key of ['primary', 'secondary', 'text', 'background', 'divider'] as const)
    expect(french.palette[key]).toEqual(english.palette[key]);
  expect(french.components?.MuiTablePagination?.defaultProps?.labelRowsPerPage).not.toBe(
    english.components?.MuiTablePagination?.defaultProps?.labelRowsPerPage,
  );
});
