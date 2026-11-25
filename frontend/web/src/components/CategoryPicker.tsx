import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus } from 'lucide-react';
import { api } from '@/lib/api';
import { Select, SelectContent, SelectTrigger, SelectValue, SelectItem } from './ui/select';
import { Button } from './ui/button';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogDescription,
} from './ui/dialog';
import { Input } from './ui/input';

export function CategoryPicker({
  value,
  onChange,
}: {
  value: string;
  onChange: (id: string) => void;
}) {
  const categories = useQuery({ queryKey: ['categories'], queryFn: api.categories });
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  async function create() {
    if (!name.trim() || busy) return;
    setBusy(true);
    setError('');
    try {
      const category = await api.saveCategory('', name);
      await client.invalidateQueries({ queryKey: ['categories'] });
      onChange(category.id);
      setOpen(false);
      setName('');
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="flex items-center gap-2">
      <Select value={value || 'none'} onValueChange={(v) => onChange(v === 'none' ? '' : v)}>
        <SelectTrigger aria-label="分类" className="flex-1">
          <SelectValue placeholder="未分类" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="none">未分类</SelectItem>
          {(categories.data ?? []).map((c) => (
            <SelectItem key={c.id} value={c.id}>
              {c.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Button
        size="icon-sm"
        variant="ghost"
        type="button"
        aria-label="新建分类"
        onClick={() => setOpen(true)}
      >
        <Plus className="size-4" />
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-[360px]">
          <DialogHeader>
            <DialogTitle>新建分类</DialogTitle>
            <DialogDescription className="sr-only">为事项创建一个分类</DialogDescription>
          </DialogHeader>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              e.stopPropagation();
              void create();
            }}
            className="space-y-4"
          >
            <Input
              aria-label="分类名称"
              placeholder="分类名称"
              value={name}
              onChange={(e) => setName(e.target.value)}
              maxLength={30}
              autoFocus
            />
            {error && (
              <p role="alert" className="text-[var(--error)]">
                {error}
              </p>
            )}
            <DialogFooter>
              <Button type="button" variant="secondary" onClick={() => setOpen(false)}>
                取消
              </Button>
              <Button disabled={busy || !name.trim()}>{busy ? '创建中…' : '创建'}</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
