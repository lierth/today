import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, it, expect, vi } from 'vitest';
import { CreateItemDialog } from './CreateItemDialog';

const { create, saveCategory } = vi.hoisted(() => ({ create: vi.fn(), saveCategory: vi.fn() }));
vi.mock('@/lib/api', () => ({ api: { categories: async () => [], create, saveCategory } }));
vi.mock('@/lib/queries', () => ({ useRefreshLists: () => async () => {} }));

beforeEach(() => {
  create.mockReset().mockResolvedValue({});
  saveCategory.mockReset().mockResolvedValue({ id: 'work', name: '工作' });
});
it('creates a category inside the task dialog without submitting the task', async () => {
  const user = userEvent.setup();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <CreateItemDialog open onOpenChange={() => {}} />
    </QueryClientProvider>,
  );
  await user.type(screen.getByRole('textbox', { name: '事项标题' }), '阅读');
  await user.click(screen.getByRole('button', { name: '新建分类' }));
  expect(create).not.toHaveBeenCalled();
  await user.type(screen.getByRole('textbox', { name: '分类名称' }), '工作');
  await user.click(screen.getByRole('button', { name: /^创建$/ }));
  await waitFor(() => expect(saveCategory).toHaveBeenCalledWith('', '工作'));
  expect(create).not.toHaveBeenCalled();
});
