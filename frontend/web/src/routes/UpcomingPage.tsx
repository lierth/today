import { useNavigate } from 'react-router-dom';
import { Skeleton } from '@/components/ui/skeleton';
import { useUpcoming, useToggleDone } from '@/lib/queries';
import { ItemRow } from '@/components/ItemRow';
import type { Item } from '@today/shared';

/**
 * Upcoming: grouped by day, with the same rows as Today so nothing has to be
 * relearned. "已逾期" comes first because it is the one group people act on.
 */
export default function UpcomingPage() {
  const upcoming = useUpcoming();
  const toggle = useToggleDone();
  const navigate = useNavigate();
  const groups = (upcoming.data?.groups ?? []).filter((group) => (group.Items ?? []).length > 0);

  // The rows of a group that is named by a date ("今天", "明天", "已逾期") do not
  // need the date repeated on every line; a row in "本周" or "以后" does, or you
  // cannot tell two of them apart.
  const named = (label: string) => label === '今天' || label === '明天' || label === '已逾期';

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-[19px] font-[560] tracking-tight md:text-[22px]">即将到来</h1>

      {upcoming.isPending ? (
        <div className="space-y-2">
          <Skeleton className="h-5 w-24" />
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
        </div>
      ) : groups.length === 0 ? (
        <div className="rounded-[12px] border border-[var(--line)] bg-[var(--card)] px-4 py-10 text-center">
          <p className="text-[12.5px] text-[var(--muted)]">接下来没有安排。</p>
          <button
            type="button"
            className="mt-3 h-8 rounded-[7px] border border-[var(--line)] px-3 text-[12px] hover:bg-[var(--pill)]"
            onClick={() => navigate('/?focus=add')}
          >
            添加一件事
          </button>
        </div>
      ) : (
        groups.map((group) => (
          <section key={group.Label} className="flex flex-col gap-1.5">
            <h2 className="sticky top-0 z-10 -mx-1 bg-[var(--bg)] px-1 py-1 text-[11.5px] font-[560] text-[var(--muted)]">
              {group.Label}
            </h2>
            <div className="overflow-hidden rounded-[12px] border border-[var(--line)] bg-[var(--card)]">
              {(group.Items ?? []).map((item: Item) => (
                <ItemRow
                  key={item.id}
                  item={item}
                  busy={toggle.isPending && toggle.variables?.item.id === item.id}
                  onToggle={(done) => toggle.mutate({ item, done })}
                  onOpen={() => navigate(`/?item=${item.id}`)}
                  trailing={named(group.Label) ? undefined : item.date_local.slice(5)}
                />
              ))}
            </div>
          </section>
        ))
      )}
    </div>
  );
}
