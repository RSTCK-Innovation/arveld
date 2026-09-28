import { enUS, frFR } from '@mui/material/locale';
import { alpha, createTheme } from '@mui/material/styles';
import type {} from '@mui/x-charts/themeAugmentation';

declare module '@mui/material/Paper' {
  interface PaperPropsVariantOverrides {
    soft: true;
  }
}

const textFont = '"Inter Variable", sans-serif';
const headingFont = '"Manrope Variable", sans-serif';
const primary = '#314b77';
const chartColors = ['#557caf', '#91a9c6', '#967eba', '#bd9258', '#4c8a7e'];

// Product identity and component defaults live here; page sx only handles layout.
export const makeTheme = (language: 'en' | 'fr') =>
  createTheme(
    {
      palette: {
        primary: { main: primary, dark: '#253a5d', light: '#91a9c6', contrastText: '#fff' },
        secondary: { main: '#756b85' },
        background: { default: '#f8f7f4', paper: '#faf9f6' },
        grey: { 50: '#faf9f6', 100: '#f1efe9', 200: '#e6e3dc' },
        text: { primary: '#272b33', secondary: '#626772' },
        divider: '#e6e3dc',
        success: { main: '#28704d', light: '#86ac96', contrastText: '#fff' },
        warning: { main: '#926014', contrastText: '#fff' },
        error: { main: '#b03930', light: '#d39187', contrastText: '#fff' },
        info: { main: '#376683', contrastText: '#fff' },
      },
      shape: { borderRadius: 10 },
      typography: {
        fontFamily: textFont,
        fontSize: 13,
        h1: {
          fontFamily: headingFont,
          fontWeight: 650,
          fontSize: '3rem',
          letterSpacing: '-0.04em',
        },
        h2: {
          fontFamily: headingFont,
          fontWeight: 650,
          fontSize: '2.5rem',
          letterSpacing: '-0.04em',
        },
        h3: {
          fontFamily: headingFont,
          fontWeight: 650,
          fontSize: '2rem',
          letterSpacing: '-0.035em',
        },
        h4: {
          fontFamily: headingFont,
          fontWeight: 650,
          fontSize: '1.875rem',
          letterSpacing: '-0.035em',
        },
        h5: {
          fontFamily: headingFont,
          fontWeight: 700,
          fontSize: '1.5rem',
          letterSpacing: '-0.03em',
        },
        h6: { fontFamily: headingFont, fontWeight: 700, fontSize: '1rem' },
        body1: { fontSize: '0.875rem', lineHeight: 1.6 },
        body2: { fontSize: '0.8125rem', lineHeight: 1.6 },
        subtitle2: { fontSize: '0.8125rem', fontWeight: 600 },
        caption: { fontSize: '0.6875rem', lineHeight: 1.6 },
        overline: {
          fontSize: '0.625rem',
          fontWeight: 700,
          letterSpacing: '0.16em',
          lineHeight: 2.8,
        },
        button: { textTransform: 'none', fontWeight: 600 },
      },
      components: {
        MuiButton: {
          defaultProps: { size: 'small', disableElevation: true },
          styleOverrides: { sizeSmall: { minHeight: 36 } },
        },
        MuiLink: { defaultProps: { underline: 'hover' } },
        MuiIconButton: { defaultProps: { size: 'small' } },
        MuiSvgIcon: { defaultProps: { fontSize: 'small' } },
        MuiTextField: { defaultProps: { size: 'small', variant: 'outlined', fullWidth: true } },
        MuiFormControl: { defaultProps: { size: 'small' } },
        MuiOutlinedInput: {
          styleOverrides: {
            root: ({ theme }) => ({ backgroundColor: theme.palette.background.paper }),
          },
        },
        MuiChip: {
          defaultProps: { size: 'small', variant: 'outlined' },
          styleOverrides: { root: { borderRadius: 4 } },
        },
        MuiPaper: {
          defaultProps: { elevation: 0 },
          styleOverrides: {
            outlined: ({ theme }) => ({
              borderColor: theme.palette.divider,
              boxShadow: '0 1px 2px rgb(45 39 28 / 3%)',
            }),
          },
          variants: [
            {
              props: { variant: 'soft' },
              style: ({ theme }) => ({
                backgroundColor: theme.palette.grey[100],
                color: theme.palette.text.primary,
              }),
            },
          ],
        },
        MuiTabs: {
          styleOverrides: {
            root: ({ theme }) => ({
              borderBottom: `1px solid ${theme.palette.divider}`,
              marginBottom: theme.spacing(3),
            }),
          },
        },
        MuiDialogTitle: { styleOverrides: { root: { paddingBottom: 8 } } },
        MuiAppBar: {
          defaultProps: { color: 'inherit', elevation: 0 },
          styleOverrides: {
            root: ({ theme }) => ({ borderBottom: `1px solid ${theme.palette.divider}` }),
          },
        },
        MuiDrawer: {
          styleOverrides: {
            paper: ({ theme }) => ({ backgroundColor: theme.palette.background.default }),
          },
        },
        MuiList: { defaultProps: { dense: true } },
        MuiListItemButton: {
          styleOverrides: {
            root: ({ theme }) => ({
              margin: theme.spacing(0.5, 1),
              borderRadius: theme.shape.borderRadius,
            }),
          },
        },
        MuiListItemIcon: { styleOverrides: { root: { minWidth: 36 } } },
        MuiListSubheader: {
          defaultProps: { disableSticky: true },
          styleOverrides: {
            root: ({ theme }) => ({
              ...theme.typography.overline,
              backgroundColor: 'transparent',
              marginTop: theme.spacing(1),
            }),
          },
        },
        MuiAvatar: {
          styleOverrides: {
            root: ({ theme }) => ({
              width: 32,
              height: 32,
              ...theme.typography.body2,
              color: theme.palette.primary.main,
              backgroundColor: alpha(theme.palette.primary.main, 0.08),
            }),
          },
        },
        MuiTableCell: {
          styleOverrides: {
            root: ({ theme }) => ({
              ...theme.typography.body2,
              padding: theme.spacing(1.75, 2.5),
              variants: [{ props: { size: 'small' }, style: { padding: theme.spacing(1, 2) } }],
            }),
            head: ({ theme }) => ({
              backgroundColor: theme.palette.background.default,
              color: theme.palette.text.secondary,
              fontWeight: 600,
            }),
          },
        },
        MuiToggleButton: { defaultProps: { size: 'small', color: 'primary' } },
        MuiDialog: {
          defaultProps: { fullWidth: true },
          styleOverrides: {
            paper: ({ theme }) => ({
              [theme.breakpoints.down('sm')]: {
                margin: theme.spacing(2),
                width: 'calc(100% - 32px)',
                maxHeight: 'calc(100% - 32px)',
              },
            }),
          },
        },
        MuiAlert: { styleOverrides: { root: { alignItems: 'center' } } },
        MuiLinearProgress: { styleOverrides: { root: { height: 6, borderRadius: 3 } } },
        MuiTooltip: { defaultProps: { arrow: true } },
        MuiSkeleton: {
          defaultProps: { animation: 'wave' },
          styleOverrides: {
            root: ({ theme }) => ({
              backgroundColor: alpha(theme.palette.text.primary, 0.07),
              '@media (prefers-reduced-motion: reduce)': {
                animation: 'none',
                '&::after': { animation: 'none' },
              },
            }),
          },
        },
        MuiLineChart: { defaultProps: { colors: chartColors } },
        MuiAreaPlot: { styleOverrides: { root: { opacity: 0.15 } } },
        MuiChartsDataProvider: { defaultProps: { colors: chartColors } },
      },
    },
    language === 'fr' ? frFR : enUS,
  );
