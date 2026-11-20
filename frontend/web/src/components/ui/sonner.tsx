'use client';

import {
  CircleCheckIcon,
  InfoIcon,
  Loader2Icon,
  OctagonXIcon,
  TriangleAlertIcon,
} from 'lucide-react';
import { Toaster as Sonner, type ToasterProps } from 'sonner';

// The app sets data-theme on <html> itself (see App.tsx), so the toaster reads
// it from the DOM instead of pulling in a theme provider of its own.
function currentTheme(): ToasterProps['theme'] {
  const value = document.documentElement.dataset.theme;
  return value === 'dark' || value === 'light' ? value : 'system';
}

const Toaster = (props: ToasterProps) => (
  <Sonner
    theme={currentTheme()}
    className="toaster group"
    icons={{
      success: <CircleCheckIcon className="size-4" />,
      info: <InfoIcon className="size-4" />,
      warning: <TriangleAlertIcon className="size-4" />,
      error: <OctagonXIcon className="size-4" />,
      loading: <Loader2Icon className="size-4 animate-spin" />,
    }}
    style={
      {
        '--normal-bg': 'var(--card)',
        '--normal-text': 'var(--fg)',
        '--normal-border': 'var(--line)',
        '--border-radius': '11px',
      } as React.CSSProperties
    }
    {...props}
  />
);

export { Toaster };
