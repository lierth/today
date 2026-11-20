import { snail } from '@today/shared/snail';

/**
 * The app's mark, drawable at any size from the same three parts the tray and
 * the animate use: body (shell and foot), antenna and tail. Monochrome, so
 * `color` decides it: ink on paper, paper on ink.
 */
export function Snail({ size = 20, className }: { size?: number; className?: string }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 44 44"
      role="img"
      aria-label="Today"
      className={className}
      fill="currentColor"
      fillRule="nonzero"
    >
      <path d={snail.body} />
      <path d={snail.antenna} />
      <path d={snail.tail} />
    </svg>
  );
}
