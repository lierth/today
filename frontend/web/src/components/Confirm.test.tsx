import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { it, expect, vi } from 'vitest';
import { ConfirmProvider } from './Confirm';
import { useDeleteItem } from '@/lib/queries';

const { remove } = vi.hoisted(() => ({ remove: vi.fn().mockResolvedValue({ undo: '/restore' }) }));
vi.mock('@/lib/api', () => ({ api: { remove } }));
function Delete() {
  const mutation = useDeleteItem();
  return <button onClick={() => mutation.mutate('task')}>删除任务</button>;
}
it('requires confirmation before sending a task to the recycle bin', async () => {
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <ConfirmProvider>
        <Delete />
      </ConfirmProvider>
    </QueryClientProvider>,
  );
  await user.click(screen.getByRole('button', { name: '删除任务' }));
  await user.click(screen.getByRole('button', { name: '取消' }));
  expect(remove).not.toHaveBeenCalled();
  await user.click(screen.getByRole('button', { name: '删除任务' }));
  await user.click(screen.getByRole('button', { name: '确认' }));
  await waitFor(() => expect(remove).toHaveBeenCalledWith('task'));
});
