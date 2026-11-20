import { cn } from '@/lib/utils';

/**
 * The day's progress as a ring: the number in the middle is the thing people
 * read, the arc is the glance-able part. 28px in the header, 44px on mobile.
 */
export function ProgressRing({
  done,
  total,
  size = 44,
  className,
}: {
  done: number;
  total: number;
  size?: number;
  className?: string;
}) {
  const stroke = size >= 40 ? 3.5 : 2.5;
  const radius = (size - stroke) / 2;
  const circumference = 2 * Math.PI * radius;
  const share = total > 0 ? Math.min(1, done / total) : 0;

  return (
    <div
      className={cn('relative grid shrink-0 place-items-center', className)}
      style={{ width: size, height: size }}
      role="img"
      aria-label={`今天完成 ${done} / ${total}`}
    >
      <svg width={size} height={size} className="-rotate-90">
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          fill="none"
          stroke="var(--line)"
          strokeWidth={stroke}
        />
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          fill="none"
          stroke="var(--accent)"
          strokeWidth={stroke}
          strokeLinecap="round"
          strokeDasharray={circumference}
          strokeDashoffset={circumference * (1 - share)}
          style={{ transition: 'stroke-dashoffset .3s var(--ease)' }}
        />
      </svg>
      <span
        className="absolute tabular-nums text-[var(--fg)]"
        style={{ fontSize: size >= 40 ? 12 : 9.5, fontWeight: 560 }}
      >
        {done}/{total}
      </span>
    </div>
  );
}
