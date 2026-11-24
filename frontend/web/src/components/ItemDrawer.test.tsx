import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Item } from '@today/shared';
import { ItemDrawer } from './ItemDrawer';

const { mutate, mutateAsync } = vi.hoisted(() => ({ mutate: vi.fn(), mutateAsync: vi.fn() }));
vi.mock('@/lib/queries', () => ({
  keys: { item: (id: string) => ['item', id] },
  usePatchItem: () => ({ mutate, mutateAsync }),
  useDeleteItem: () => ({ mutate: vi.fn() }),
}));

const ITEM: Item = {
  id: 'note-race',
  title: '原标题',
  description: '原备注',
  all_day: true,
  date_local: '2026-11-24',
  status: 'open',
  pinned: false,
  sort_key: 1,
  created_at: '2026-11-24T08:00:00Z',
  updated_at: '2026-11-24T08:00:00Z',
};

describe('ItemDrawer', () => {
  beforeEach(() => {
    mutate.mockReset();
    mutateAsync.mockReset().mockResolvedValue({});
  });
  it('keeps a note draft when the title autosave returns', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } });
    client.setQueryData(['item', ITEM.id], ITEM);
    render(
      <QueryClientProvider client={client}>
        <ItemDrawer id={ITEM.id} onClose={() => {}} />
      </QueryClientProvider>,
    );
    const user = userEvent.setup();
    const title = screen.getByRole('textbox', { name: '标题' });
    const note = screen.getByRole('textbox', { name: '备注' });
    await user.clear(title);
    await user.type(title, '新标题');
    await user.clear(note);
    await user.type(note, '不能丢失的新备注');
    act(() => client.setQueryData(['item', ITEM.id], { ...ITEM, title: '新标题' }));
    await waitFor(() => expect((title as HTMLInputElement).value).toBe('新标题'));
    expect((note as HTMLTextAreaElement).value).toBe('不能丢失的新备注');
    await user.click(screen.getByRole('button', { name: '完成' }));
    expect(mutateAsync).toHaveBeenCalledWith({
      id: ITEM.id,
      patch: { description: '不能丢失的新备注' },
    });
  });

  it('keeps the drawer and draft open when saving fails', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } });
    client.setQueryData(['item', ITEM.id], ITEM);
    const onClose = vi.fn();
    render(
      <QueryClientProvider client={client}>
        <ItemDrawer id={ITEM.id} onClose={onClose} />
      </QueryClientProvider>,
    );
    const user = userEvent.setup();
    const note = screen.getByRole('textbox', { name: '备注' });
    await user.clear(note);
    await user.type(note, '断网也要保留');
    mutateAsync.mockRejectedValueOnce(new Error('网络连接中断'));
    await user.click(screen.getByRole('button', { name: '完成' }));
    await waitFor(() => expect(mutateAsync).toHaveBeenCalled());
    expect(onClose).not.toHaveBeenCalled();
    expect((note as HTMLTextAreaElement).value).toBe('断网也要保留');
  });
});
