import type { SVGProps } from 'react';

type P = SVGProps<SVGSVGElement>;

function Svg({ children, ...rest }: P) {
  return (
    <svg
      width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"
      strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false" {...rest}
    >
      {children}
    </svg>
  );
}

export const LogoMark = (p: P) => (
  <svg width="22" height="22" viewBox="0 0 24 24" aria-hidden="true" focusable="false" {...p}>
    <rect width="24" height="24" rx="6" fill="var(--accent)" />
    <path d="M6 15.5 10.5 11 13.5 14 18 8.5" fill="none" stroke="var(--on-accent)" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" />
    <circle cx="18" cy="8.5" r="1.6" fill="var(--on-accent)" />
  </svg>
);

export const PullRequestIcon = (p: P) => (
  <Svg {...p}><circle cx="6" cy="6" r="2.5" /><circle cx="6" cy="18" r="2.5" /><circle cx="18" cy="18" r="2.5" /><path d="M6 8.5v7M18 15.5V9a3 3 0 0 0-3-3h-4m2-2-2 2 2 2" /></Svg>
);
export const BranchIcon = (p: P) => (
  <Svg {...p}><circle cx="6" cy="5" r="2.5" /><circle cx="6" cy="19" r="2.5" /><circle cx="18" cy="7" r="2.5" /><path d="M6 7.5v9M18 9.5c0 4-6 3-11 7" /></Svg>
);
export const TagIcon = (p: P) => (
  <Svg {...p}><path d="M3 12V4a1 1 0 0 1 1-1h8l9 9-9 9z" /><circle cx="8" cy="8" r="1.5" /></Svg>
);
export const ExternalIcon = (p: P) => (
  <Svg width="13" height="13" {...p}><path d="M14 4h6v6M20 4l-9 9M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5" /></Svg>
);
export const WarningIcon = (p: P) => (
  <Svg {...p}><path d="M12 3 2 21h20z" /><path d="M12 10v4M12 17.5v.01" /></Svg>
);
export const CheckIcon = (p: P) => <Svg {...p}><path d="m5 12 5 5 9-10" /></Svg>;
export const CrossIcon = (p: P) => <Svg {...p}><path d="M6 6l12 12M18 6 6 18" /></Svg>;
export const LockIcon = (p: P) => (
  <Svg {...p}><rect x="5" y="11" width="14" height="10" rx="2" /><path d="M8 11V8a4 4 0 0 1 8 0v3" /></Svg>
);
export const WrenchIcon = (p: P) => (
  <Svg {...p}><path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94z" /></Svg>
);
export const ArrowDownIcon = (p: P) => <Svg {...p}><path d="M12 5v14M6 13l6 6 6-6" /></Svg>;
