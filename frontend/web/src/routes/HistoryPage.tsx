import { Skeleton } from '@/components/ui/skeleton';
import { useHistory } from '@/lib/queries';
import { cn } from '@/lib/utils';

/**
 * History: how much got done, then the past in the same day grouping the
 * board uses. The numbers are the week's, because "am I keeping up" is a
 * weekly question, not a daily one.
 */
export default function HistoryPage() {
  const history = useHistory();
  const data = history.data;
  const groups = (data?.groups ?? []).filter((group) => (group.Items ?? []).length > 0);
  const delta = (data?.done_this_week ?? 0) - (data?.done_last_week ?? 0);

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-[19px] font-[560] tracking-tight md:text-[22px]">历史</h1>

      <div className="flex flex-wrap items-baseline gap-x-5 gap-y-1 rounded-[12px] border border-[var(--line)] bg-[var(--card)] px-4 py-3">
        <p className="text-[12.5px] text-[var(--fg-2)]">
          本周完成{' '}
          <span className="text-[17px] font-[560] text-[var(--fg)] tabular-nums">
            {data?.done_this_week ?? 0}
          </span>{' '}
          项
        </p>
        <p className="text-[12px] text-[var(--muted)]">
          上周 {data?.done_last_week ?? 0} 项
          {delta !== 0 ? (delta > 0 ? `，多了 ${delta} 项` : `，少了 ${-delta} 项`) : '，持平'}
        </p>
      </div>

      {history.isPending ? (
        <div className="space-y-2">
          <Skeleton className="h-5 w-28" />
          <Skeleton className="h-10 w-full" />
        </div>
      ) : groups.length === 0 ? (
        <p className="rounded-[12px] border border-[var(--line)] bg-[var(--card)] px-4 py-10 text-center text-[12.5px] text-[var(--muted)]">
          还没有过去的事项。
        </p>
      ) : (
        groups.map((group) => (
          <section key={group.Label} className="flex flex-col gap-1.5">
            <h2 className="sticky top-0 z-10 -mx-1 bg-[var(--bg)] px-1 py-1 text-[11.5px] font-[560] text-[var(--muted)]">
              {group.Label}
            </h2>
            <div className="overflow-hidden rounded-[12px] border border-[var(--line)] bg-[var(--card)]">
              {(group.Items ?? []).map((item) => (
                <div
                  key={item.id}
                  className="flex h-10 items-center gap-2.5 border-t border-[var(--line-2)] px-3 first:border-t-0"
                >
                  <span
                    aria-hidden
                    className={cn(
                      'grid size-[18px] shrink-0 place-items-center rounded-full text-[10px]',
                      item.status === 'done'
                        ? 'bg-[var(--accent)] text-[var(--accent-fg)]'
                        : 'border border-[var(--faint)] text-transparent',
                    )}
                  >
                    ✓
                  </span>
                  <span className="min-w-0 flex-1 truncate text-[12.5px] text-[var(--fg-2)] select-text">
                    {item.title}
                  </span>
                  <span className="shrink-0 text-[11px] text-[var(--faint)]">
                    {item.date_local.slice(5)}
                  </span>
                </div>
              ))}
            </div>
          </section>
        ))
      )}
    </div>
  );
}
