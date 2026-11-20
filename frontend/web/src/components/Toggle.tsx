import { cn } from '@/lib/utils';

/**
 * A 34×20 switch, the same one the Mac settings window draws: the knob moves
 * and the track darkens to the ink colour. The accent is reserved for status
 * and reminders, so a switch that is merely "on" is not painted with it; the
 * surface colour token keeps the knob right in both themes instead of a
 * hard-coded white.
 */
export function Toggle({
  label,
  hint,
  checked,
  onChange,
  disabled,
  icon,
  className,
}: {
  label: string;
  hint?: string;
  checked: boolean;
  onChange: (next: boolean) => void;
  disabled?: boolean;
  icon?: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={cn('flex min-h-10 items-center gap-3 py-1.5', className)}>
      {icon ? <span className="text-[var(--muted)]">{icon}</span> : null}
      <div className="min-w-0 flex-1">
        <p className="text-[12.5px] text-[var(--fg)]">{label}</p>
        {hint ? <p className="mt-0.5 text-[11px] text-[var(--muted)]">{hint}</p> : null}
      </div>
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        aria-label={label}
        disabled={disabled}
        onClick={() => onChange(!checked)}
        className={cn(
          'relative h-5 w-[34px] shrink-0 rounded-full transition-colors',
          checked ? 'bg-[var(--fg)]' : 'bg-[var(--faint)]',
          disabled && 'opacity-50',
        )}
      >
        <span
          className="absolute top-[2px] size-4 rounded-full bg-[var(--card)] shadow-sm transition-transform"
          style={{ left: 2, transform: `translateX(${checked ? 14 : 0}px)` }}
        />
      </button>
    </div>
  );
}
