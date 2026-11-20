import { useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { Search } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Skeleton } from '@/components/ui/skeleton';
import { useSearch } from '@/lib/queries';

/** The search page: same query as ⌘K, but it can stay open and be read. */
export default function SearchPage() {
  const [params] = useSearchParams();
  const [query, setQuery] = useState(params.get('q') ?? '');
  const found = useSearch(query);
  const navigate = useNavigate();
  const groups = (found.data?.groups ?? []).filter((group) => (group.Items ?? []).length > 0);

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-[19px] font-[560] tracking-tight md:text-[22px]">搜索</h1>
      <div className="flex h-9 items-center gap-2 rounded-[8px] border border-[var(--line)] bg-[var(--card)] px-2.5 focus-within:border-[var(--accent)]">
        <Search className="size-3.5 shrink-0 text-[var(--muted)]" />
        <Input
          autoFocus
          aria-label="搜索事项"
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            // Keep the address in step: a search someone wants to hand over
            // should survive a reload or a bookmark.
            const next = new URLSearchParams(e.target.value ? { q: e.target.value } : {});
            navigate({ pathname: '/search', search: next.toString() }, { replace: true });
          }}
          placeholder="搜标题或备注…"
          className="h-8 border-0 bg-transparent px-0 shadow-none focus-visible:ring-0"
        />
      </div>

      {query.trim() === '' ? (
        <p className="px-1 text-[12px] text-[var(--muted)]">输入几个字，历史里的事项也会一起找。</p>
      ) : found.isPending ? (
        <Skeleton className="h-20 w-full" />
      ) : groups.length === 0 ? (
        <p className="rounded-[12px] border border-[var(--line)] bg-[var(--card)] px-4 py-8 text-center text-[12.5px] text-[var(--muted)]">
          没有找到「{query}」。
        </p>
      ) : (
        groups.map((group) => (
          <section key={group.Label} className="flex flex-col gap-1.5">
            <h2 className="text-[11.5px] font-[560] text-[var(--muted)]">{group.Label}</h2>
            <div className="overflow-hidden rounded-[12px] border border-[var(--line)] bg-[var(--card)]">
              {(group.Items ?? []).map((item) => (
                <button
                  key={item.id}
                  type="button"
                  onClick={() => navigate(`/?item=${item.id}`)}
                  className="flex h-10 w-full items-center gap-2 border-t border-[var(--line-2)] px-3 text-left first:border-t-0 hover:bg-[var(--card-2)]"
                >
                  <span className="min-w-0 flex-1 truncate text-[12.5px] text-[var(--fg)] select-text">
                    {item.title}
                  </span>
                  <span className="shrink-0 text-[11px] text-[var(--faint)]">
                    {item.date_local}
                  </span>
                </button>
              ))}
            </div>
          </section>
        ))
      )}
    </div>
  );
}
