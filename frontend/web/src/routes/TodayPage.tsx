import { Suspense, lazy, useMemo } from 'react';
import { useSearchParams } from 'react-router-dom';
import { CloudSun, RefreshCw } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { ItemList } from '@/components/ItemList';
import { Hint } from '@/components/Hint';
import { ProgressRing } from '@/components/ProgressRing';

import { Snail } from '@/components/Snail';
import {
  useCarry,
  useDeleteItem,
  usePatchItem,
  useReorder,
  useToday,
  useToggleDone,
} from '@/lib/queries';
import { cn } from '@/lib/utils';
import type { Item } from '@today/shared';

// The drawer is opened by a row, never present at first paint, and it brings
// the calendar, the popover and a whole sheet with it.
const ItemDrawer = lazy(() =>
  import('@/components/ItemDrawer').then((m) => ({ default: m.ItemDrawer })),
);

/**
 * Today: the date, the weather, the ring, one list. Everything the board
 * shows is here. The focus display collapses completed items and its capacity
 * varies with the current title, while this page keeps the full editable list.
 */
export function TodayPage() {
  const [params, setParams] = useSearchParams();
  const today = useToday();
  const toggle = useToggleDone();
  const carry = useCarry();
  const remove = useDeleteItem();
  const patch = usePatchItem();
  const reorder = useReorder();

  const openId = params.get('item');
  const data = today.data;
  const items = data?.items ?? [];
  const carried = data?.carried ?? [];

  const movable = useMemo(() => items.filter((item) => item.status !== 'cancelled'), [items]);

  const setOpen = (id: string | null) => {
    const next = new URLSearchParams(params);
    if (id) next.set('item', id);
    else next.delete('item');
    setParams(next, { replace: true });
  };

  // Local calendar arithmetic, never toISOString(): that returns the UTC day,
  // and east of UTC+12 "tomorrow" was landing on today.
  const tomorrow = () => {
    if (!data) return '';
    const parts = data.day.key.split('-').map(Number);
    const y = parts[0] ?? 1970;
    const m = parts[1] ?? 1;
    const d = parts[2] ?? 1;
    const day = new Date(y, m - 1, d + 1);
    const pad = (n: number) => String(n).padStart(2, '0');
    return `${day.getFullYear()}-${pad(day.getMonth() + 1)}-${pad(day.getDate())}`;
  };

  /** Every row in the list (board or overflow) behaves the same. */
  const rowProps = (item: Item) => ({
    item,
    busy: toggle.isPending && toggle.variables?.item.id === item.id,
    onToggle: (done: boolean) => toggle.mutate({ item, done }),
    onOpen: () => setOpen(item.id),
    onPin: () => patch.mutate({ id: item.id, patch: { pinned: !item.pinned } }),
    onMoveTomorrow: () => patch.mutate({ id: item.id, patch: { date: tomorrow() } }),
    onDelete: () => remove.mutate(item.id),
  });

  /** The quick-add field is the app's front door; an empty state points at it
      rather than leaving the person to find it. */
  const focusAdd = () => window.dispatchEvent(new Event('today:add'));
  return (
    <div className="flex flex-col gap-5">
      {/* No snail beside the date on a phone: the top bar already carries the
          wordmark, and two drawings of the same thing three centimetres apart
          read as a mistake. */}
      <header className="flex items-center gap-3">
        <div className="min-w-0 flex-1">
          <h1 className="text-[19px] font-[560] tracking-tight md:text-[22px]">
            {data ? `${data.day.label} ${data.day.weekday}` : '今天'}
          </h1>
          <p className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[11.5px] text-[var(--muted)]">
            <span>{data?.day.lunar ?? ''}</span>
            {data?.weather ? (
              <span className="flex items-center gap-1 select-text">
                <CloudSun className="size-3.5" />
                {data.weather}
              </span>
            ) : null}
          </p>
        </div>
        <ProgressRing done={data?.done ?? 0} total={data?.total ?? 0} className="hidden sm:grid" />
        <ProgressRing
          done={data?.done ?? 0}
          total={data?.total ?? 0}
          size={34}
          className="sm:hidden"
        />
        <Hint label="刷新">
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="刷新"
            onClick={() => void today.refetch()}
            disabled={today.isFetching}
          >
            <RefreshCw className={cn('size-3.5', today.isFetching && 'animate-spin')} />
          </Button>
        </Hint>
      </header>

      <Button variant="outline" className="justify-start text-[var(--ctl-fg)]" onClick={focusAdd}>
        ＋ 添加事项
      </Button>
      {carried.length > 0 ? (
        <details className="rounded-[12px] border border-[var(--line)] bg-[var(--card-2)] px-3 py-2">
          <summary className="flex cursor-pointer list-none items-center gap-2 text-[12px] text-[var(--muted)]">
            <span className="flex-1">之前还有 {carried.length} 项没完成</span>
            <Button
              variant="ghost"
              size="sm"
              className="h-6 px-2 text-[11.5px] text-[var(--fg)] underline decoration-[var(--line)] underline-offset-2 hover:decoration-[var(--fg)]"
              onClick={(e) => {
                e.preventDefault();
                carry.mutate({ ids: [], all: true });
              }}
              disabled={carry.isPending}
            >
              全部移到今天
            </Button>
          </summary>
          <ul className="mt-1.5 space-y-1">
            {carried.map((item) => (
              <li key={item.id} className="flex items-center gap-2 text-[12px] text-[var(--fg-2)]">
                <span className="min-w-0 flex-1 truncate select-text">{item.title}</span>
                <span className="shrink-0 text-[11px] text-[var(--faint)]">{item.day_label}</span>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6 px-1.5 text-[11px] text-[var(--fg)] underline decoration-[var(--line)] underline-offset-2 hover:decoration-[var(--fg)]"
                  onClick={() => carry.mutate({ ids: [item.id] })}
                  disabled={carry.isPending}
                >
                  移到今天
                </Button>
              </li>
            ))}
          </ul>
        </details>
      ) : null}

      <section
        className="overflow-hidden rounded-[12px] border border-[var(--line)] bg-[var(--card)]"
        aria-label="今天的事项"
      >
        {today.isPending ? (
          <div className="space-y-2 p-3">
            <Skeleton className="h-4 w-2/3" />
            <Skeleton className="h-4 w-1/2" />
            <Skeleton className="h-4 w-3/5" />
          </div>
        ) : items.length === 0 ? (
          <EmptyToday onStart={focusAdd} />
        ) : (
          <>
            <ItemList
              items={movable}
              boardCount={movable.length}
              onReorder={(ids) => reorder.mutate(ids)}
              render={(item: Item) => rowProps(item)}
            />
          </>
        )}
      </section>

      {openId ? (
        <Suspense fallback={null}>
          <ItemDrawer
            id={openId}
            onClose={() => setOpen(null)}
            onChanged={() => void today.refetch()}
          />
        </Suspense>
      ) : null}
    </div>
  );
}

/**
 * An empty list is a dead end unless it offers the one thing to do next: the
 * button puts the caret in the quick-add field, which is the whole app from
 * here.
 */
function EmptyToday({ onStart }: { onStart: () => void }) {
  return (
    <div className="flex flex-col items-center gap-2 px-4 py-10 text-center">
      <Snail size={36} className="text-[var(--faint)]" />
      <p className="text-[13px] text-[var(--fg-2)]">今天还没有事项。</p>
      <p className="text-[11.5px] text-[var(--muted)]">
        写一行就算开始了；日期和时间可以直接写在句子里。
      </p>
      <Button size="sm" className="mt-1 h-7" onClick={onStart}>
        写第一件事
      </Button>
    </div>
  );
}
