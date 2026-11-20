import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import QuickAdd from './QuickAdd.svelte';

vi.mock('mygo-runtime', () => ({ isMyGo: () => true }));

const tags = vi.fn();

vi.mock('../../mygo', () => ({
  Panel: { tags: (...args: unknown[]) => tags(...args) },
}));

beforeEach(() => {
  tags.mockReset();
});

describe('QuickAdd', () => {
  it('shows a chip per fragment and removes only that slice', async () => {
    tags.mockResolvedValue([
      { kind: 'date', label: '11月20日', text: '明天' },
      { kind: 'time', label: '10:30', text: '10:30' },
    ]);
    const user = userEvent.setup();
    render(QuickAdd, { props: { onsubmit: () => {} } });

    const field = screen.getByLabelText('快速添加') as HTMLInputElement;
    await user.type(field, '明天 10:30 站会');
    await waitFor(() => expect(screen.getByText('11月20日')).toBeTruthy());

    // Removing the date chip must leave the time and the title alone.
    tags.mockResolvedValue([{ kind: 'time', label: '10:30', text: '10:30' }]);
    await user.click(screen.getByLabelText('去掉 11月20日'));
    await waitFor(() => expect(field.value).toBe('10:30 站会'));
    expect(screen.queryByText('11月20日')).toBeNull();
  });

  it('keeps the draft when adding it fails', async () => {
    // Clearing the box on a failed save threw away the words the person had
    // just typed; the panel above shows the error, and the text stays put.
    tags.mockResolvedValue([]);
    const submit = vi.fn(() => Promise.reject(new Error('网络不通')));
    const user = userEvent.setup();
    render(QuickAdd, { props: { onsubmit: submit } });

    const field = screen.getByLabelText('快速添加') as HTMLInputElement;
    await user.type(field, '买牛奶{Enter}');

    await waitFor(() => expect(submit).toHaveBeenCalledWith('买牛奶'));
    expect(field.value).toBe('买牛奶');
  });

  it('submits the draft on Enter and clears it', async () => {
    tags.mockResolvedValue([]);
    const submit = vi.fn();
    const user = userEvent.setup();
    render(QuickAdd, { props: { onsubmit: submit } });

    const field = screen.getByLabelText('快速添加') as HTMLInputElement;
    await user.type(field, '写周报{Enter}');

    await waitFor(() => expect(submit).toHaveBeenCalledWith('写周报'));
    expect(field.value).toBe('');
  });
});
