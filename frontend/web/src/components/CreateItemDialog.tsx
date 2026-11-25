import { useState } from 'react';
import { CalendarDays } from 'lucide-react';
import { api } from '@/lib/api';
import { useRefreshLists } from '@/lib/queries';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from './ui/dialog';
import { Button } from './ui/button';
import { Input } from './ui/input';
import { Calendar } from './ui/calendar';
import { Popover, PopoverContent, PopoverTrigger } from './ui/popover';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select';
import { CategoryPicker } from './CategoryPicker';
import { TimePicker } from './TimePicker';

export function CreateItemDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const refresh = useRefreshLists();
  const [title, setTitle] = useState('');
  const [date, setDate] = useState('');
  const [time, setTime] = useState('');
  const [category, setCategory] = useState('');
  const [notes, setNotes] = useState('');
  const [repeat, setRepeat] = useState('none');
  const [calendar, setCalendar] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  async function save() {
    if (!title.trim() || busy) return;
    setBusy(true);
    setError('');
    try {
      await api.create({
        title: title.trim(),
        raw: title.trim(),
        date,
        time,
        description: notes,
        category_id: category,
        rrule: repeat === 'none' ? '' : repeat,
      });
      await refresh();
      onOpenChange(false);
      setTitle('');
      setDate('');
      setTime('');
      setNotes('');
      setRepeat('none');
      setCategory('');
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!busy) onOpenChange(next);
      }}
    >
      <DialogContent className="max-h-[85dvh] overflow-y-auto sm:max-w-[440px]">
        <DialogHeader>
          <DialogTitle>添加事项</DialogTitle>
          <DialogDescription className="sr-only">填写标题、时间和分类</DialogDescription>
        </DialogHeader>
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            void save();
          }}
        >
          <Input
            autoFocus
            aria-label="事项标题"
            placeholder="要做什么？"
            value={title}
            maxLength={200}
            onChange={(e) => setTitle(e.target.value)}
          />
          <div className="flex flex-wrap items-center gap-2">
            <Popover open={calendar} onOpenChange={setCalendar}>
              <PopoverTrigger asChild>
                <Button type="button" variant="outline" size="sm">
                  <CalendarDays className="size-3.5" />
                  {date || '今天'}
                </Button>
              </PopoverTrigger>
              <PopoverContent align="start" className="w-auto p-0">
                <Calendar
                  mode="single"
                  selected={date ? new Date(`${date}T00:00:00`) : undefined}
                  onSelect={(d) => {
                    if (d)
                      setDate(
                        `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`,
                      );
                    setCalendar(false);
                  }}
                />
              </PopoverContent>
            </Popover>
            <TimePicker value={time} onChange={setTime} />
          </div>
          <CategoryPicker value={category} onChange={setCategory} />
          <Select value={repeat} onValueChange={setRepeat}>
            <SelectTrigger aria-label="重复">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="none">不重复</SelectItem>
              <SelectItem value="FREQ=DAILY">每天</SelectItem>
              <SelectItem value="FREQ=WEEKLY">每周</SelectItem>
              <SelectItem value="FREQ=MONTHLY">每月</SelectItem>
            </SelectContent>
          </Select>
          <textarea
            aria-label="备注"
            placeholder="备注"
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
            rows={3}
            className="w-full resize-none rounded-lg border bg-[var(--card-2)] p-2 text-[13px] outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)]"
          />
          {error && (
            <p role="alert" className="text-[var(--error)]">
              {error}
            </p>
          )}
          <DialogFooter>
            <Button
              type="button"
              variant="secondary"
              disabled={busy}
              onClick={() => onOpenChange(false)}
            >
              取消
            </Button>
            <Button disabled={!title.trim() || busy}>{busy ? '添加中…' : '添加'}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
