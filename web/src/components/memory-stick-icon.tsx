import SvgIcon, { type SvgIconProps } from '@mui/material/SvgIcon';

export function MemoryStickIcon(props: SvgIconProps) {
  return (
    <SvgIcon {...props}>
      <g fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round">
        <rect x="2" y="5" width="20" height="12" rx="1.5" />
        <path d="M5 17v3m3-3v3m3-3v3m3-3v3m3-3v3m3-3v3" />
        <path d="M5 8h3v6H5zm5.5 0h3v6h-3zm5.5 0h3v6h-3z" />
      </g>
    </SvgIcon>
  );
}
