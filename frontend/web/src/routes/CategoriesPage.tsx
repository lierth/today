import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus, Trash2 } from 'lucide-react';
import { api } from '@/lib/api';
import { useRefreshLists } from '@/lib/queries';
import { useConfirm } from '@/components/Confirm';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';

export default function CategoriesPage() {
  const query = useQuery({ queryKey: ['categories'], queryFn: api.categories });
  const client = useQueryClient();
  const refresh = useRefreshLists();
  const confirm = useConfirm();
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  async function run(job: () => Promise<unknown>) {
    if (busy) return;
    setBusy(true);
    setError('');
    try {
      await job();
      await client.invalidateQueries({ queryKey: ['categories'] });
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="space-y-4">
      <h1 className="text-[22px] font-[560]">分类</h1>
      <form
        className="flex items-center gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          void run(async () => {
            await api.saveCategory('', name);
            setName('');
          });
        }}
      >
        <Input
          aria-label="新分类名称"
          placeholder="新分类"
          value={name}
          maxLength={30}
          onChange={(e) => setName(e.target.value)}
        />
        <Button disabled={busy || !name.trim()}>
          <Plus className="size-4" />
          添加
        </Button>
      </form>
      {(error || query.error) && (
        <p role="alert" className="text-[var(--error)]">
          {error || query.error?.message}
        </p>
      )}
      <div className="overflow-hidden rounded-xl border bg-[var(--card)]">
        {query.isPending ? (
          <p className="p-4 text-[var(--muted)]">读取中…</p>
        ) : (query.data ?? []).length === 0 ? (
          <p className="p-4 text-center text-[var(--muted)]">还没有分类</p>
        ) : (
          (query.data ?? []).map((category) => (
            <form
              key={category.id + category.name}
              className="flex items-center gap-2 border-t px-3 py-2 first:border-t-0"
              onSubmit={(e) => {
                e.preventDefault();
                const form = new FormData(e.currentTarget);
                void run(() => api.saveCategory(category.id, String(form.get('name'))));
              }}
            >
              <Input
                name="name"
                aria-label={`分类 ${category.name}`}
                defaultValue={category.name}
                maxLength={30}
              />
              <Button variant="secondary" size="sm" disabled={busy}>
                保存
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label={`删除分类 ${category.name}`}
                disabled={busy}
                onClick={() =>
                  void confirm(
                    `删除“${category.name}”？`,
                    '分类中的事项会保留，并变为未分类。',
                  ).then((ok) => {
                    if (ok) void run(() => api.deleteCategory(category.id));
                  })
                }
              >
                <Trash2 className="size-4" />
              </Button>
            </form>
          ))
        )}
      </div>
    </div>
  );
}
