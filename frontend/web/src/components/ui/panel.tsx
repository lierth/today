import type { HTMLAttributes } from 'react';
import { cn } from '../../lib/utils';

/**
 * Panel is the list card of the design system: white surface, hairline
 * border, 12 px radius. It is not a shadcn component because shadcn's Card
 * carries padding and header slots we never use; this is the one shape the
 * whole app repeats.
 */
export function Panel({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn('rounded-[12px] border border-[var(--line)] bg-[var(--card)]', className)}
      {...props}
    />
  );
}
