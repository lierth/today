import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';

/**
 * Icon-only buttons get their name twice: aria-label for assistive tech, and
 * this tooltip for people who do not recognise the glyph. One provider wraps
 * the whole app (see main.tsx), so the delay is shared.
 */
export function Hint({
  label,
  side = 'bottom',
  children,
}: {
  label: string;
  side?: 'top' | 'bottom' | 'left' | 'right';
  children: React.ReactNode;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>{children}</TooltipTrigger>
      <TooltipContent side={side} sideOffset={6}>
        {label}
      </TooltipContent>
    </Tooltip>
  );
}

export { TooltipProvider };
