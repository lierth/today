import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import type { Item, TodayPayload } from '@today/shared';
import { useCarry, useToggleDone } from './queries';

const patch = vi.fn();
const carry = vi.fn();
const undo = vi.fn();

vi.mock('./api', () => ({
  api: {
    patch: (...args: unknown[]) => patch(...args),
    carry: (...args: unknown[]) => carry(...args),
    undo: (...args: unknown[]) => undo(...args),
    today: vi.fn(),
    remove: vi.fn(),
    reorder: vi.fn(),
  },
}));
vi.mock('sonner', () => ({ toast: Object.assign(vi.fn(), { error: vi.fn(), success: vi.fn() }) }));

const ITEM: Item = {
  id: 'a1',
  title: '写周报',
  all_day: true,
  date_local: '2026-11-19',
  status: 'open',
  pinned: false,
  sort_key: 1,
  created_at: '2026-11-19T08:00:00Z',
  updated_at: '2026-11-19T08:00:00Z',
};

const DAY: TodayPayload = {
  day: { key: '2026-11-19', label: '11月19日', weekday: '周四', lunar: '农历九月三十' },
  items: [ITEM],
  total: 1,
  done: 0,
  carried: null,
  auto_carry: true,
  board_limit: 10,
  weather: '',
  preview_id: '',
};

function setup() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  client.setQueryData(['today'], DAY);
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return { client, wrapper };
}

describe('useToggleDone', () => {
  beforeEach(() => {
    patch.mockReset();
    carry.mockReset();
    undo.mockReset();
  });

  it('ticks the row before the write lands', async () => {
    let finish: (value: unknown) => void = () => {};
    patch.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const { client, wrapper } = setup();
    const { result } = renderHook(() => useToggleDone(), { wrapper });

    result.current.mutate({ item: ITEM, done: true });

    // The row is done while the request is still in flight.
    await waitFor(() => {
      const now = client.getQueryData<TodayPayload>(['today']);
      expect(now?.items?.[0]?.status).toBe('done');
    });

    finish({ item: { ...ITEM, status: 'done' } });
    await waitFor(() => expect(result.current.isPending).toBe(false));
  });

  it('puts the row back when the write fails', async () => {
    patch.mockRejectedValue(new Error('这条事项不在了'));
    const { client, wrapper } = setup();
    const { result } = renderHook(() => useToggleDone(), { wrapper });

    result.current.mutate({ item: ITEM, done: true });

    await waitFor(() => {
      const now = client.getQueryData<TodayPayload>(['today']);
      expect(now?.items?.[0]?.status).toBe('open');
    });
  });
});

describe('useCarry', () => {
  beforeEach(() => {
    patch.mockReset();
    carry.mockReset();
    undo.mockReset();
  });

  it('carries everything with all=true and none of the ids', async () => {
    carry.mockResolvedValue({ moved: 2, skipped: {}, undo: { path: '/api/v2/items/uncarry' } });
    const { wrapper } = setup();
    const { result } = renderHook(() => useCarry(), { wrapper });

    result.current.mutate({ ids: [], all: true });

    await waitFor(() => expect(carry).toHaveBeenCalledWith('all'));
  });

  it('carries the given ids only', async () => {
    carry.mockResolvedValue({ moved: 1, skipped: {} });
    const { wrapper } = setup();
    const { result } = renderHook(() => useCarry(), { wrapper });

    result.current.mutate({ ids: ['a1'] });

    await waitFor(() => expect(carry).toHaveBeenCalledWith(['a1']));
  });
});
