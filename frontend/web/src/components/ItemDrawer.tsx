import { useCallback, useEffect, useState } from 'react';
import { CalendarDays, Clock, Pin, PinOff, Repeat, Trash2 } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { toast } from 'sonner';
import { api } from '@/lib/api';
import { keys, useDeleteItem, usePatchItem } from '@/lib/queries';
import { Button } from '@/components/ui/button';
import { Calendar } from '@/components/ui/calendar';
import { Input } from '@/components/ui/input';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Separator } from '@/components/ui/separator';
import { Toggle } from '@/components/Toggle';

// The stored value for "no rule" is an empty string. The control needs a
// non-empty value to show a label, so the empty case travels as `none` and is
// mapped back on the way out — writing "none" to the database made a repeat
// rule the server could not parse.
const NO_REPEAT = 'none';

const REPEATS = [
  { value: NO_REPEAT, label: '不重复' },
  { value: 'FREQ=DAILY', label: '每天' },
  { value: 'FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR', label: '工作日' },
  { value: 'FREQ=WEEKLY', label: '每周' },
  { value: 'FREQ=MONTHLY', label: '每月' },
  { value: 'custom', label: '自定义…' },
];

/** A date string as a local Date, without the UTC shift of `new Date("2026-11-20")`. */
function localDate(iso: string): Date | undefined {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso);
  if (!m) return undefined;
  return new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
}

/** The inverse: a Date's calendar day in the person's own timezone. */
function isoDate(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/**
 * Item details: a drawer on a wide screen, a full-height sheet on a phone.
 *
 * Every edit saves, but closing is an edit too: pressing Esc, clicking the
 * page behind the sheet and pressing 完成 all flush what is in the fields
 * first. Losing a half-typed note because the person clicked outside is the
 * one bug this panel must not have.
 */
export function ItemDrawer({
  id,
  onClose,
  onChanged,
}: {
  id: string | null;
  onClose: () => void;
  onChanged?: () => void;
}) {
  const item = useQuery({
    queryKey: keys.item(id ?? ''),
    queryFn: () => api.item(id!),
    enabled: Boolean(id),
  });
  const patch = usePatchItem();
  const remove = useDeleteItem();

  const [title, setTitle] = useState('');
  const [notes, setNotes] = useState('');
  const [date, setDate] = useState('');
  const [time, setTime] = useState('');
  const [rrule, setRrule] = useState('');
  const [custom, setCustom] = useState('');
  const [calendarOpen, setCalendarOpen] = useState(false);

  useEffect(() => {
    const it = item.data;
    if (!it) return;
    setTitle(it.title);
    setNotes(it.description ?? '');
    setDate(it.date_local);
    setTime(it.starts_at && !it.all_day ? it.starts_at.slice(11, 16) : '');
    setRrule(it.rrule ?? '');
    setCustom(it.rrule ?? '');
  }, [item.data]);

  const row = item.data;

  /** Everything in the fields that differs from what the server has. */
  const pending = useCallback((): Parameters<typeof api.patch>[1] => {
    if (!row) return {};
    const next: Parameters<typeof api.patch>[1] = {};
    const trimmed = title.trim();
    if (trimmed && trimmed !== row.title) next.title = trimmed;
    if (notes !== (row.description ?? '')) next.description = notes;
    if (
      date !== row.date_local ||
      time !== (row.starts_at && !row.all_day ? row.starts_at.slice(11, 16) : '')
    ) {
      next.date = date;
      next.time = time;
    }
    if (rrule !== (row.rrule ?? '')) next.rrule = rrule;
    return next;
  }, [row, title, notes, date, time, rrule]);

  const close = useCallback(() => {
    if (id) {
      const next = pending();
      if (Object.keys(next).length > 0) {
        // Fire and forget: the panel is going away, and TanStack keeps the
        // mutation alive above it. A failure still raises a toast.
        patch.mutate(
          { id, patch: next },
          {
            onSuccess: () => onChanged?.(),
            onError: (err) => toast.error(err.message),
          },
        );
      }
    }
    onClose();
  }, [id, pending, patch, onChanged, onClose]);

  const updateRRule = (value: string) => {
    if (value === 'custom') {
      setRrule(custom || 'FREQ=WEEKLY');
      return;
    }
    const next = value === NO_REPEAT ? '' : value;
    setRrule(next);
    if (next !== (row?.rrule ?? '')) {
      toast('重复规则已更新', { description: '未来的日子会按新规则生成。' });
    }
  };

  return (
    <Sheet open={Boolean(id)} onOpenChange={(open) => (!open ? close() : undefined)}>
      <SheetContent
        side="right"
        data-drawer="item"
        aria-label="事项详情"
        className="w-full border-l border-[var(--line)] bg-[var(--card)] p-0 sm:max-w-[420px]"
      >
        {row ? (
          <div className="flex h-full flex-col">
            <SheetHeader className="gap-1 border-b border-[var(--line)] px-4 py-4">
              <SheetTitle className="sr-only">事项详情</SheetTitle>
              <Input
                aria-label="标题"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                onBlur={() => {
                  const trimmed = title.trim();
                  if (trimmed && trimmed !== row.title) {
                    patch.mutate({ id: row.id, patch: { title: trimmed } });
                  }
                }}
                className="h-9 rounded-[7px] border-0 bg-transparent px-1 text-[15px] font-[560] shadow-none focus-visible:ring-2 focus-visible:ring-[var(--accent)]"
              />
              <div className="flex flex-wrap items-center gap-1.5 text-[11.5px] text-[var(--muted)]">
                <Popover open={calendarOpen} onOpenChange={setCalendarOpen}>
                  <PopoverTrigger asChild>
                    <button
                      type="button"
                      aria-label="选择日期"
                      className="flex items-center gap-1 rounded-[7px] px-1.5 py-1 hover:bg-[var(--card-2)] focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--accent)]"
                    >
                      <CalendarDays className="size-3.5" />
                      <span className="tabular-nums">{date || '无日期'}</span>
                    </button>
                  </PopoverTrigger>
                  <PopoverContent align="start" className="w-auto p-0">
                    <Calendar
                      mode="single"
                      selected={localDate(date)}
                      defaultMonth={localDate(date)}
                      onSelect={(d) => {
                        if (!d) return;
                        setDate(isoDate(d));
                        setCalendarOpen(false);
                      }}
                    />
                  </PopoverContent>
                </Popover>

                <label className="flex items-center gap-1 rounded-[7px] px-1.5 py-1 hover:bg-[var(--card-2)]">
                  <Clock className="size-3.5" aria-hidden />
                  <span className="sr-only">时间</span>
                  <input
                    type="time"
                    aria-label="时间"
                    value={time}
                    onChange={(e) => setTime(e.target.value)}
                    className="bg-transparent tabular-nums outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--accent)]"
                  />
                </label>

                {row.carried_from ? <span>· 从 {row.carried_from} 顺延</span> : null}
                {row.rrule ? <span>· 重复事项</span> : null}
              </div>
            </SheetHeader>

            <div className="flex-1 space-y-3 overflow-y-auto px-4 py-4">
              <div className="flex items-center gap-2">
                <Repeat className="size-3.5 shrink-0 text-[var(--muted)]" aria-hidden />
                <Select value={rrule === '' ? NO_REPEAT : rrule} onValueChange={updateRRule}>
                  <SelectTrigger aria-label="重复" className="h-8 flex-1">
                    <SelectValue placeholder="不重复" />
                  </SelectTrigger>
                  <SelectContent>
                    {REPEATS.map((r) => (
                      <SelectItem key={r.value} value={r.value}>
                        {r.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              {rrule !== '' && !REPEATS.some((r) => r.value === rrule) ? (
                <Input
                  aria-label="重复规则"
                  value={custom}
                  onChange={(e) => setCustom(e.target.value)}
                  onBlur={() => setRrule(custom.trim())}
                  placeholder="FREQ=WEEKLY;BYDAY=MO"
                  className="h-8 font-mono text-[12px]"
                />
              ) : null}

              <textarea
                aria-label="备注"
                value={notes}
                rows={5}
                placeholder="备注"
                onChange={(e) => setNotes(e.target.value)}
                onBlur={() => {
                  if (notes !== (row.description ?? '')) {
                    patch.mutate(
                      { id: row.id, patch: { description: notes } },
                      { onSuccess: () => onChanged?.() },
                    );
                  }
                }}
                className="w-full resize-none rounded-[8px] border border-[var(--line)] bg-[var(--card-2)] px-2.5 py-2 text-[12.5px] outline-none select-text focus-visible:border-[var(--accent)] focus-visible:ring-2 focus-visible:ring-[var(--accent)]"
              />

              <Separator />
              <Toggle
                label="置顶"
                checked={row.pinned}
                onChange={(next) => {
                  patch.mutate(
                    { id: row.id, patch: { pinned: next } },
                    { onSuccess: () => onChanged?.() },
                  );
                }}
                icon={row.pinned ? <PinOff className="size-3.5" /> : <Pin className="size-3.5" />}
              />
            </div>

            <div className="flex items-center justify-between border-t border-[var(--line)] px-4 py-3">
              <Button
                variant="ghost"
                size="sm"
                className="gap-1.5 text-[var(--error)]"
                aria-label="删除事项"
                onClick={() => {
                  if (!id) return;
                  remove.mutate(id, { onSuccess: () => onClose() });
                }}
              >
                <Trash2 className="size-3.5" /> 删除
              </Button>
              <Button variant="secondary" size="sm" onClick={close}>
                完成
              </Button>
            </div>
          </div>
        ) : null}
      </SheetContent>
    </Sheet>
  );
}
