import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import type { TodayView } from './mygo';
import App from './App.svelte';

const { today, loadPrefs, stopTheme, stopAccessibility } = vi.hoisted(() => ({
  today: vi.fn(),
  loadPrefs: vi.fn(),
  stopTheme: vi.fn(),
  stopAccessibility: vi.fn(),
}));

vi.mock('mygo-runtime', () => ({ isMyGo: () => true }));
vi.mock('./mygo', () => ({ Panel: { today, tags: async () => [] } }));
vi.mock('./lib/theme', () => ({
  colorBlind: {
    subscribe: (run: (value: boolean) => void) => {
      run(false);
      return () => {};
    },
  },
  loadPrefs,
  startTheme: () => stopTheme,
  watchAccessibility: () => stopAccessibility,
}));

const initial: TodayView = {
  date: '11月24日',
  weekday: '周二',
  lunar: '农历十月十六',
  total: 0,
  done: 0,
  items: [],
  carried: [],
  devices: [],
  weather: '',
};

beforeEach(() => {
  vi.clearAllMocks();
  today.mockResolvedValue(structuredClone(initial));
  loadPrefs.mockResolvedValue(null);
});

describe('retained tray panel', () => {
  it('resets automatic focus cues on reopen while keeping buttons keyboard-accessible', async () => {
    const user = userEvent.setup();
    render(App);
    await screen.findByRole('heading', { name: '11月24日 周二' });
    const panel = screen.getByRole('main');
    const refresh = screen.getByRole('button', { name: '刷新' });
    refresh.focus();
    await user.tab();
    const settings = screen.getByRole('button', { name: '设置' });
    expect(document.activeElement).toBe(settings);
    expect(panel.classList.contains('keyboard')).toBe(true);

    await fireEvent(window, new Event('blur'));
    await fireEvent(window, new Event('focus'));
    expect(panel.classList.contains('keyboard')).toBe(false);
    expect(document.activeElement).toBe(settings);
    await user.tab();
    expect(document.activeElement).toBe(screen.getByLabelText('快速添加'));
    expect(panel.classList.contains('keyboard')).toBe(true);
    await fireEvent.pointerDown(window);
    expect(panel.classList.contains('keyboard')).toBe(false);
  });

  it('refreshes on reopen without clearing content or the input draft', async () => {
    const user = userEvent.setup();
    render(App);
    await screen.findByRole('heading', { name: '11月24日 周二' });
    const field = screen.getByLabelText('快速添加') as HTMLInputElement;
    await user.type(field, '明天整理文档');

    let finish!: (view: TodayView) => void;
    today.mockImplementationOnce(
      () =>
        new Promise<TodayView>((resolve) => {
          finish = resolve;
        }),
    );
    await fireEvent(window, new Event('focus'));
    await waitFor(() => expect(today).toHaveBeenCalledTimes(2));
    expect(loadPrefs).toHaveBeenCalledTimes(2);
    expect(screen.getByRole('heading', { name: '11月24日 周二' })).toBeTruthy();
    expect(field.value).toBe('明天整理文档');

    finish({ ...initial, date: '11月25日', weekday: '周三' });
    await screen.findByRole('heading', { name: '11月25日 周三' });
    expect(screen.getByLabelText('快速添加')).toBe(field);
    expect(field.value).toBe('明天整理文档');
  });

  it('does not start a duplicate initial read and releases subscriptions', async () => {
    today.mockReturnValueOnce(new Promise(() => {}));
    const { unmount } = render(App);
    await waitFor(() => expect(today).toHaveBeenCalledTimes(1));
    await fireEvent(window, new Event('focus'));
    expect(today).toHaveBeenCalledTimes(1);
    unmount();
    expect(stopTheme).toHaveBeenCalledOnce();
    expect(stopAccessibility).toHaveBeenCalledOnce();
  });
});
