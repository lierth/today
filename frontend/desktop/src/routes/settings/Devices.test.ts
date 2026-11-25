import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import Devices from './Devices.svelte';
import type { SettingsView } from '../../mygo';

const { revoke, discover, pair } = vi.hoisted(() => ({
  revoke: vi.fn(),
  discover: vi.fn(),
  pair: vi.fn(),
}));
vi.mock('../../mygo', () => ({
  Panel: { revokeDevice: revoke, discoverBoards: discover, pairBoard: pair },
}));
vi.mock('../../lib/theme', async () => {
  const { writable } = await import('svelte/store');
  return { colorBlind: writable(false) };
});

const data = {
  devices: [
    {
      id: 'desk',
      name: '书桌屏',
      online: true,
      since: '在线',
      battery: '76%',
      climate: '',
      firmware: '0.2.0',
      city: '',
    },
  ],
} as SettingsView;

describe('device pairing', () => {
  beforeEach(() => {
    revoke.mockReset();
    discover.mockReset().mockResolvedValue([]);
    pair.mockReset();
    vi.spyOn(window, 'confirm').mockReturnValue(false);
  });

  it('searches only on manual refresh while device status keeps updating', async () => {
    vi.useFakeTimers();
    const reload = vi.fn();
    const view = render(Devices, { props: { data, reload } });
    try {
      await act(async () => {
        await Promise.resolve();
      });
      expect(discover).not.toHaveBeenCalled();
      await act(async () => {
        await vi.advanceTimersByTimeAsync(60000);
      });
      expect(discover).not.toHaveBeenCalled();
      expect(reload).toHaveBeenCalledTimes(20);
      await fireEvent.click(screen.getByRole('button', { name: '刷新' }));
      expect(discover).toHaveBeenCalledOnce();
    } finally {
      view.unmount();
      vi.useRealTimers();
    }
  });

  it('leaves a connected board paired when cancellation is declined', async () => {
    render(Devices, { props: { data, reload: vi.fn() } });
    await userEvent.setup().click(screen.getByRole('button', { name: '取消配对' }));
    expect(window.confirm).toHaveBeenCalled();
    expect(revoke).not.toHaveBeenCalled();
  });

  it('pairs the discovered hardware identity without asking for an ID or server', async () => {
    discover.mockResolvedValue([
      { id: 'today-aabbccddeeff', name: '显示屏 · ddeeff', pairable: true },
    ]);
    pair.mockResolvedValue({ id: 'today-aabbccddeeff', name: '书房' });
    const reload = vi.fn();
    render(Devices, { props: { data: { devices: [] } as unknown as SettingsView, reload } });
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: '刷新' }));
    await user.click(await screen.findByRole('button', { name: '连接' }));
    expect(document.activeElement).toBe(screen.getByRole('textbox', { name: '屏幕配对码' }));
    expect(screen.queryByRole('textbox', { name: 'ID' })).toBeNull();
    await user.type(screen.getByRole('textbox', { name: '屏幕配对码' }), '123 456');
    await user.type(screen.getByRole('textbox', { name: '名称' }), '书房');
    await user.click(screen.getByRole('button', { name: '配对' }));
    expect(pair).toHaveBeenCalledWith('today-aabbccddeeff', '123456', '书房');
    expect(reload).toHaveBeenCalledOnce();
    expect(screen.queryByRole('textbox', { name: '屏幕配对码' })).toBeNull();
  });

  it('keeps the code and name available when the board refuses pairing', async () => {
    discover.mockResolvedValue([
      { id: 'today-aabbccddeeff', name: '显示屏 · ddeeff', pairable: true },
    ]);
    pair.mockRejectedValue(new Error('配对码不正确或已过期'));
    const reload = vi.fn();
    render(Devices, { props: { data: { devices: [] } as unknown as SettingsView, reload } });
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: '刷新' }));
    await user.click(await screen.findByRole('button', { name: '连接' }));
    await user.type(screen.getByRole('textbox', { name: '屏幕配对码' }), '123456');
    await user.click(screen.getByRole('button', { name: '配对' }));
    expect((await screen.findByRole('alert')).textContent).toContain('配对码不正确或已过期');
    expect((screen.getByRole('textbox', { name: '屏幕配对码' }) as HTMLInputElement).value).toBe(
      '123456',
    );
    expect((screen.getByRole('button', { name: '配对' }) as HTMLButtonElement).disabled).toBe(
      false,
    );
    expect(reload).not.toHaveBeenCalled();
  });
});
