import { useRef, useState } from 'react';
import { Loader2 } from 'lucide-react';
import { Hint } from '@/components/Hint';
import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';
import { api } from '@/lib/api';
import type { QuickAddTag } from '@today/shared';

export interface QuickAddProps {
  placeholder?: string;
  autoFocus?: boolean;
  onSaved?: () => void;
  /** When set, the page owns the write (the panel already does). */
  onSubmit?: (text: string, tags: QuickAddTag[]) => Promise<void> | void;
  className?: string;
}

/**
 * One line to add something. The chips are the parser's reading of the text,
 * shown live so a surprise ("明天" picked up from the middle of a sentence) is
 * visible before saving rather than after.
 *
 * A chip's × deletes exactly the slice of text it stands for and re-parses;
 * the hidden fields on save carry parsed=true, so the server will not bring a
 * deleted chip back by re-reading the text.
 */
export function QuickAdd({ placeholder, autoFocus, onSaved, onSubmit, className }: QuickAddProps) {
  const [draft, setDraft] = useState('');
  const [tags, setTags] = useState<QuickAddTag[]>([]);
  const [parsed, setParsed] = useState({ title: '', date: '', time: '', rrule: '' });
  const [busy, setBusy] = useState(false);
  const [composing, setComposing] = useState(false);
  const [error, setError] = useState('');
  const latest = useRef('');
  // Two Enter presses in the same frame both see busy === false, because the
  // state that would stop them has not rendered yet — that is the shape of the
  // duplicate rows the review found. A ref is decided at once.
  const saving = useRef(false);
  // The text the chips in `parsed` were read from. If the person keeps typing
  // and hits Enter before the parse comes back, the chips belong to older
  // text; the save must then let the server parse again instead of sending a
  // stale title and date.
  const parsedFrom = useRef('');

  const read = (text: string) => {
    latest.current = text;
    setDraft(text);
    if (!text.trim()) {
      setTags([]);
      setParsed({ title: '', date: '', time: '', rrule: '' });
      return;
    }
    api
      .parse(text)
      .then((found) => {
        if (latest.current !== text) return; // a slower answer for older text
        parsedFrom.current = text;
        setTags(found.tags ?? []);
        setParsed({ title: found.title, date: found.date, time: found.time, rrule: found.rrule });
      })
      .catch(() => {
        if (latest.current !== text) return;
        parsedFrom.current = '';
        setTags([]);
      });
  };

  const dropTag = (tag: QuickAddTag) => {
    const at = draft.indexOf(tag.text);
    const next = at < 0 ? draft : `${draft.slice(0, at)}${draft.slice(at + tag.text.length)}`;
    read(next.replace(/\s+/g, ' ').trim());
  };

  const save = async () => {
    if (!draft.trim() || saving.current) return;
    saving.current = true;
    setBusy(true);
    setError('');
    try {
      if (onSubmit) {
        await onSubmit(draft.trim(), tags);
      } else {
        // parsed=true only when the chips really describe this text. Once the
        // person edits again, the chips are stale and the server must read the
        // line itself.
        const fresh = tags.length > 0 && parsedFrom.current === draft;
        await api.create({
          raw: draft,
          parsed: fresh,
          title: fresh ? parsed.title || draft : draft,
          date: fresh ? parsed.date : '',
          time: fresh ? parsed.time : '',
          rrule: fresh ? parsed.rrule : '',
        });
      }
      setDraft('');
      setTags([]);
      setParsed({ title: '', date: '', time: '', rrule: '' });
      latest.current = '';
      parsedFrom.current = '';
      onSaved?.();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      saving.current = false;
      setBusy(false);
    }
  };

  return (
    <div className={cn('flex flex-col gap-1.5', className)}>
      <div className="flex h-9 items-center gap-2 rounded-[8px] border border-[var(--line)] bg-[var(--card)] px-2.5 focus-within:border-[var(--accent)]">
        <input
          aria-label="添加事项"
          autoFocus={autoFocus}
          className="min-w-0 flex-1 bg-transparent text-[13px] text-[var(--fg)] outline-none placeholder:text-[var(--muted)]"
          placeholder={placeholder ?? '写一行，回车添加…'}
          value={draft}
          onChange={(e) => read(e.target.value)}
          onCompositionStart={() => setComposing(true)}
          onCompositionEnd={() => setComposing(false)}
          onKeyDown={(e) => {
            const ime = composing || e.nativeEvent.isComposing || e.keyCode === 229;
            if (e.key === 'Enter' && !ime) {
              e.preventDefault();
              void save();
            }
          }}
        />
        {busy ? <Loader2 className="size-4 animate-spin text-[var(--muted)]" /> : null}
      </div>
      {tags.length > 0 ? (
        <div className="flex flex-wrap gap-1" aria-label="识别到的标签">
          {tags.map((tag) => (
            <Badge
              key={`${tag.kind}:${tag.text}`}
              variant="secondary"
              className={cn(
                'h-[22px] gap-1 rounded-full px-2 text-[11px] font-normal',
                tag.kind === 'repeat'
                  ? 'bg-[var(--pill)] text-[var(--fg-2)]'
                  : 'bg-[var(--accent-soft)] text-[var(--accent)]',
              )}
            >
              {tag.label}
              <Hint label={`去掉「${tag.label}」`}>
                <button
                  type="button"
                  aria-label={`去掉 ${tag.label}`}
                  className="grid size-[15px] place-items-center rounded-full hover:bg-black/10 dark:hover:bg-white/15"
                  onClick={() => dropTag(tag)}
                >
                  <span aria-hidden className="text-[11px] leading-none">
                    ×
                  </span>
                </button>
              </Hint>
            </Badge>
          ))}
        </div>
      ) : null}
      {error ? (
        <p role="alert" className="text-[11.5px] text-[var(--error)] select-text">
          {error}
        </p>
      ) : null}
    </div>
  );
}
