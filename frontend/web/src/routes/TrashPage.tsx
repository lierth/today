import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Undo2, Trash2 } from 'lucide-react';
import { api } from '@/lib/api';
import { useRefreshLists } from '@/lib/queries';
import { useConfirm } from '@/components/Confirm';
import { Button } from '@/components/ui/button';

export default function TrashPage() {
  const query = useQuery({ queryKey: ['trash'], queryFn: api.trash, refetchInterval: 60000 });
  const client = useQueryClient();
  const refresh = useRefreshLists();
  const confirm = useConfirm();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  async function run(job: () => Promise<unknown>) {
    if (busy) return;
    setBusy(true);
    setError('');
    try {
      await job();
      await client.invalidateQueries({ queryKey: ['trash'] });
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="space-y-4">
      <header>
        <h1 className="text-[22px] font-[560]">回收站</h1>
        <p className="mt-1 text-[12px] text-[var(--muted)]">删除的事项保留 3 天</p>
      </header>
      {(error || query.error) && (
        <p role="alert" className="text-[var(--error)]">
          {error || query.error?.message}
        </p>
      )}
      <div className="overflow-hidden rounded-xl border bg-[var(--card)]">
        {query.isPending ? (
          <p className="p-4 text-[var(--muted)]">读取中…</p>
        ) : (query.data ?? []).length === 0 ? (
          <p className="p-8 text-center text-[var(--muted)]">回收站是空的</p>
        ) : (
          (query.data ?? []).map((item) => (
            <div
              key={item.id}
              className="flex items-center gap-2 border-t px-3 py-3 first:border-t-0"
            >
              <div className="min-w-0 flex-1">
                <p className="truncate">{item.title}</p>
                <p className="mt-0.5 text-[11px] text-[var(--muted)]">
                  {item.deleted_at
                    ? new Date(new Date(item.deleted_at).getTime() + 3 * 86400000).toLocaleString(
                        'zh-CN',
                        { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' },
                      )
                    : ''}{' '}
                  后永久删除
                </p>
              </div>
              <Button
                variant="ghost"
                size="sm"
                disabled={busy}
                onClick={() => void run(() => api.restore(item.id))}
              >
                <Undo2 className="size-3.5" />
                恢复
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                disabled={busy}
                aria-label={`永久删除 ${item.title}`}
                onClick={() =>
                  void confirm('永久删除事项？', `“${item.title}”将无法恢复。`).then((ok) => {
                    if (ok) void run(() => api.deleteForever(item.id));
                  })
                }
              >
                <Trash2 className="size-4 text-[var(--error)]" />
              </Button>
            </div>
          ))
        )}
      </div>
    </div>
  );
}
