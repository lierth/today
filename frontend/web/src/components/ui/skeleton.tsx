import type { HTMLAttributes } from 'react';
import { cn } from '../../lib/utils';

/**
 * Skeleton is a placeholder with the final layout's footprint, so nothing
 * jumps when the answer arrives. It respects reduced motion by staying still.
 */
export function Skeleton({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      role="presentation"
      className={cn('animate-pulse rounded-[8px] bg-[var(--pill)]', className)}
      {...props}
    />
  );
}
