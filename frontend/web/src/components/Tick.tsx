import { Check, Loader2, X } from 'lucide-react';
import { cn } from '@/lib/utils';
import type { Item } from '@today/shared';

/**
 * A circular tick that is the same shape on the board, in the tray and here.
 * The board draws it 18px, the list uses 20px, and a finished one keeps its
 * check while the title fades: undoing is one click away, not hidden. The
 * check itself is ink, not the accent — a whole list of done rows should not
 * look like a whole list of alerts.
 */
export function Tick({
  item,
  onToggle,
  size = 20,
  busy,
  className,
}: {
  item: Pick<Item, 'id' | 'status'> & { title?: string };
  onToggle: (done: boolean) => void;
  size?: number;
  busy?: boolean;
  className?: string;
}) {
  const done = item.status === 'done';
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={done}
      // The name is part of the label: read on its own, "标为完成" says
      // nothing about which row is about to change.
      aria-label={
        item.title
          ? `${done ? '标为未完成' : '标为完成'}：${item.title}`
          : done
            ? '标为未完成'
            : '标为完成'
      }
      disabled={busy}
      onClick={(e) => {
        e.stopPropagation();
        onToggle(!done);
      }}
      className={cn(
        'grid shrink-0 place-items-center rounded-full border transition-colors',
        done
          ? 'border-[var(--fg)] bg-[var(--fg)] text-[var(--bg)]'
          : 'border-[var(--faint)] bg-transparent text-transparent hover:border-[var(--fg)]',
        busy && 'opacity-60',
        className,
      )}
      style={{ width: size, height: size }}
    >
      {busy ? (
        <Loader2 className="size-3 animate-spin" />
      ) : done ? (
        <Check className="size-3" strokeWidth={3} />
      ) : (
        <X className="size-0" />
      )}
    </button>
  );
}
