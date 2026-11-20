import { describe, expect, it, vi, beforeEach } from 'vitest';
import type { TodayView } from '../mygo';
import { carryToday, cutFragment, toggleDone } from './panel';

vi.mock('mygo-runtime', () => ({ isMyGo: () => true }));

const setDone = vi.fn();
const today = vi.fn();
const carry = vi.fn();

vi.mock('../mygo', () => ({
  Panel: {
    setDone: (...args: unknown[]) => setDone(...args),
    today: () => today(),
    carry: (...args: unknown[]) => carry(...args),
  },
}));

function view(rows: { id: string; title: string; done: boolean }[]): TodayView {
  return {
    date: '11月19日',
    weekday: '周四',
    lunar: '农历十月初十',
    total: rows.length,
    done: rows.filter((r) => r.done).length,
    items: rows.map((r) => ({ ...r, pinned: false, carried: false, time: '' })),
    carried: [],
    devices: [],
    weather: '',
  };
}

beforeEach(() => {
  setDone.mockReset();
  today.mockReset();
  carry.mockReset();
});

describe('toggleDone', () => {
  it('flips the row before the write lands', async () => {
    const gate = Promise.withResolvers<void>();
    setDone.mockImplementation(() => gate.promise);
    const before = view([{ id: 'a', title: '写周报', done: false }]);
    today.mockResolvedValue(view([{ id: 'a', title: '写周报', done: true }]));

    const pending = toggleDone(before, 'a', true);
    // Optimistic: the row reads done while the call is still open.
    expect(before.items[0]?.done).toBe(true);
    gate.resolve();
    const after = await pending;
    expect(setDone).toHaveBeenCalledWith('a', true);
    expect(after.items[0]?.done).toBe(true);
  });

  it('puts the row back when the write fails', async () => {
    setDone.mockRejectedValue(new Error('这条事项不在了'));
    const before = view([{ id: 'a', title: '写周报', done: false }]);

    await expect(toggleDone(before, 'a', true)).rejects.toThrow('这条事项不在了');
    expect(before.items[0]?.done).toBe(false);
    expect(today).not.toHaveBeenCalled();
  });
});

describe('carryToday', () => {
  it('asks for every leftover when no id is given', async () => {
    carry.mockResolvedValue({ moved: 2, skipped: {} });
    today.mockResolvedValue(view([]));

    await carryToday('');
    expect(carry).toHaveBeenCalledWith([], true);
  });

  it('asks for one when the row names it', async () => {
    carry.mockResolvedValue({ moved: 1, skipped: {} });
    today.mockResolvedValue(view([]));

    await carryToday('abc');
    expect(carry).toHaveBeenCalledWith(['abc'], false);
  });
});

describe('cutFragment', () => {
  it('cuts the slice the chip stands for', () => {
    expect(cutFragment('明天 10:30 站会', '明天')).toBe('10:30 站会');
    expect(cutFragment('明天 10:30 站会', '10:30')).toBe('明天 站会');
  });

  it('leaves the draft alone when the text is gone', () => {
    expect(cutFragment('站会', '明天')).toBe('站会');
  });
});
