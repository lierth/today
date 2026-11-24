import { useRef, useState } from 'react';
import { Pin, PinOff, Sunrise, Trash2 } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Hint } from './Hint';
import { Tick } from './Tick';
import type { Item } from '@today/shared';

export interface ItemRowProps {
  item: Item;
  onToggle: (done: boolean) => void;
  onOpen?: () => void;
  onPin?: () => void;
  onMoveTomorrow?: () => void;
  onDelete?: () => void;
  busy?: boolean;
  /** The board shows at most this many rows; the page marks the cutoff. */
  dimmed?: boolean;
  dragHandle?: React.ReactNode;
  /** A phone has no hover: a left swipe opens the row's actions instead. */
  swipeable?: boolean;
  /** The spare date at the end of a row in a day that is not its own. */
  trailing?: string;
}

/** "14:05" in the row; all-day rows show nothing, the date is the row's group. */
function clock(item: Item): string {
  if (!item.starts_at || item.all_day) return '';
  const at = new Date(item.starts_at);
  return Number.isNaN(at.getTime())
    ? ''
    : `${String(at.getHours()).padStart(2, '0')}:${String(at.getMinutes()).padStart(2, '0')}`;
}

export function ItemRow({
  item,
  onToggle,
  onOpen,
  onPin,
  onMoveTomorrow,
  onDelete,
  busy,
  dimmed,
  dragHandle,
  swipeable,
  trailing,
}: ItemRowProps) {
  const done = item.status === 'done';
  const time = clock(item);
  const [revealed, setRevealed] = useState(false);
  const startX = useRef(0);

  const swipe = swipeable
    ? {
        onPointerDown: (e: React.PointerEvent) => {
          startX.current = e.clientX;
        },
        onPointerUp: (e: React.PointerEvent) => {
          const dx = e.clientX - startX.current;
          if (dx < -40) setRevealed(true);
          else if (dx > 40) setRevealed(false);
        },
      }
    : {};

  return (
    <div
      className={cn(
        // `relative` is what lets the grip and the hover actions sit over the
        // row instead of taking layout width: reserving a slot for a control
        // that is invisible most of the time is what made the text column look
        // inset by 40 px on both sides.
        'group relative flex h-12 md:h-11 items-center gap-2.5 border-t border-[var(--line-2)] first:border-t-0',
        dragHandle ? 'pl-[22px] pr-3' : 'px-3',
        'hover:bg-[var(--card-2)]',
        dimmed && 'opacity-55',
        revealed && 'bg-[var(--card-2)]',
      )}
      data-item={item.id}
      style={swipeable ? { touchAction: 'pan-y' } : undefined}
      {...swipe}
    >
      {dragHandle ? (
        <span className="absolute left-0.5 top-1/2 -translate-y-1/2">{dragHandle}</span>
      ) : null}
      <Tick item={item} onToggle={onToggle} busy={busy} />
      <button
        type="button"
        data-open-item={item.id}
        aria-label={`打开 ${item.title}`}
        className="flex h-full min-w-0 flex-1 items-center gap-2 text-left"
        onClick={onOpen}
      >
        <span
          className={cn(
            'truncate text-[14px] md:text-[13px]',
            done ? 'text-[var(--muted)] line-through' : 'text-[var(--fg)]',
            'select-text',
          )}
        >
          {item.title}
        </span>
        {item.pinned ? (
          <Pin className="size-3 shrink-0 text-[var(--faint)]" aria-label="已置顶" />
        ) : null}
        {item.rrule ? (
          <span className="shrink-0 text-[11px] text-[var(--faint)]" aria-label="重复事项">
            ↻
          </span>
        ) : null}
      </button>
      {time ? (
        <span className="shrink-0 tabular-nums text-[11.5px] text-[var(--muted)]">{time}</span>
      ) : null}
      {trailing ? (
        <span className="shrink-0 tabular-nums text-[11.5px] text-[var(--faint)]">{trailing}</span>
      ) : null}
      {/* Hover on the desktop, a swipe in the mobile list (see ItemList).
          Overlaid, with the card's own colour fading in behind it, so the row
          needs no reserved gutter and a long title still runs to the edge. */}
      <div
        className={cn(
          'absolute right-1.5 top-1/2 flex -translate-y-1/2 items-center gap-0.5',
          'rounded-[6px] bg-gradient-to-l from-[var(--card-2)] from-70% to-transparent pl-7 pr-1 py-1',
          'transition-opacity',
          'opacity-0 group-focus-within:opacity-100 group-hover:opacity-100',
          revealed && 'opacity-100',
        )}
      >
        {onPin ? (
          <IconAction label={item.pinned ? '取消置顶' : '置顶'} onClick={onPin}>
            {item.pinned ? <PinOff className="size-3.5" /> : <Pin className="size-3.5" />}
          </IconAction>
        ) : null}
        {onMoveTomorrow ? (
          <IconAction label="移到明天" onClick={onMoveTomorrow}>
            <Sunrise className="size-3.5" />
          </IconAction>
        ) : null}
        {onDelete ? (
          <IconAction label="删除" onClick={onDelete}>
            <Trash2 className="size-3.5" />
          </IconAction>
        ) : null}
      </div>
    </div>
  );
}

function IconAction({
  label,
  onClick,
  children,
}: {
  label: string;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <Hint label={label} side="top">
      <button
        type="button"
        aria-label={label}
        onClick={(e) => {
          e.stopPropagation();
          onClick();
        }}
        className="grid size-[26px] place-items-center rounded-[6px] text-[var(--ctl-fg)] hover:bg-[var(--pill)] hover:text-[var(--fg)]"
      >
        {children}
      </button>
    </Hint>
  );
}
