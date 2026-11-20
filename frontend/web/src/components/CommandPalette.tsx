import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { CalendarDays, History, LayoutList, Search } from 'lucide-react';
import { useSearch } from '@/lib/queries';
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from '@/components/ui/command';

/**
 * ⌘K (or `/`) opens search from anywhere. Typing searches all items, not just
 * today's: the reason people open this is usually something they cannot see.
 */
export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const navigate = useNavigate();
  const found = useSearch(query);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const typing =
        e.target instanceof HTMLElement &&
        (e.target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(e.target.tagName));
      if ((e.key === 'k' && (e.metaKey || e.ctrlKey)) || (e.key === '/' && !typing)) {
        e.preventDefault();
        setOpen((v) => !v);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  const go = (path: string) => {
    setOpen(false);
    setQuery('');
    navigate(path);
  };

  const groups = found.data?.groups ?? [];

  return (
    <CommandDialog
      open={open}
      onOpenChange={setOpen}
      title="搜索"
      description="搜索事项或前往页面"
      shouldFilter={false}
    >
      <CommandInput placeholder="搜索事项…" value={query} onValueChange={setQuery} />
      <CommandList>
        <CommandEmpty>{query ? '没有找到。' : '输入几个字开始搜索。'}</CommandEmpty>
        {groups.map((group) => (
          <CommandGroup key={group.Label} heading={group.Label}>
            {(group.Items ?? []).map((item) => (
              <CommandItem key={item.id} value={item.id} onSelect={() => go(`/?item=${item.id}`)}>
                <span className="truncate">{item.title}</span>
                <span className="ml-auto text-[11px] text-[var(--muted)]">{group.Label}</span>
              </CommandItem>
            ))}
          </CommandGroup>
        ))}
        {!query ? (
          <>
            <CommandSeparator />
            <CommandGroup heading="前往">
              <CommandItem onSelect={() => go('/')}>
                <LayoutList className="mr-2 size-4" /> 今日
              </CommandItem>
              <CommandItem onSelect={() => go('/upcoming')}>
                <CalendarDays className="mr-2 size-4" /> 即将到来
              </CommandItem>
              <CommandItem onSelect={() => go('/history')}>
                <History className="mr-2 size-4" /> 历史
              </CommandItem>
              <CommandItem onSelect={() => go('/search')}>
                <Search className="mr-2 size-4" /> 搜索页
              </CommandItem>
            </CommandGroup>
          </>
        ) : null}
      </CommandList>
    </CommandDialog>
  );
}
