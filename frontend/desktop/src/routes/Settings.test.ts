import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import type { SettingsView } from '../mygo';
import Settings from './Settings.svelte';

const { settingsData, setSetting, settingsReady } = vi.hoisted(() => ({
  settingsData: vi.fn(),
  setSetting: vi.fn(),
  settingsReady: vi.fn(),
}));

vi.mock('mygo-runtime', () => ({ isMyGo: () => true }));
vi.mock('../mygo', () => ({
  Panel: { settingsData, setSetting, settingsReady, discoverBoards: async () => [] },
}));
vi.mock('../lib/theme', async () => {
  const { writable } = await import('svelte/store');
  return {
    colorBlind: writable(false),
    loadPrefs: async () => null,
    startTheme: () => () => {},
    watchAccessibility: () => () => {},
  };
});

let data: SettingsView;
beforeEach(() => {
  vi.clearAllMocks();
  history.replaceState(null, '', '/settings');
  data = {
    version: '0.2.2',
    settings: { open_at_login: 'false' },
    zones: [],
    devices: [],
  } as unknown as SettingsView;
  settingsData.mockImplementation(async () => structuredClone(data));
  setSetting.mockImplementation(async (key: string, value: string) => {
    data.settings[key] = value;
  });
});

describe('settings save', () => {
  it('reveals the window after its initial settings render', async () => {
    let resolve!: (value: SettingsView) => void;
    settingsData.mockReturnValueOnce(
      new Promise<SettingsView>((done) => {
        resolve = done;
      }),
    );
    render(Settings);
    await waitFor(() => expect(settingsData).toHaveBeenCalledOnce());
    expect(settingsReady).not.toHaveBeenCalled();
    resolve(data);
    await waitFor(() => expect(settingsReady).toHaveBeenCalledOnce());
    expect(screen.getByRole('switch', { name: '登录时启动' })).toBeTruthy();
  });

  it('reveals a readable error when initial settings cannot load', async () => {
    settingsData.mockRejectedValueOnce(new Error('读取失败'));
    render(Settings);
    await waitFor(() => expect(settingsReady).toHaveBeenCalledOnce());
    expect(screen.getByRole('alert').textContent).toContain('读取失败');
    expect(screen.getByRole('button', { name: '重新读取' })).toBeTruthy();
  });

  it('keeps keyboard tab navigation visible and clears it after pointer input', async () => {
    const { container } = render(Settings);
    const general = screen.getByRole('tab', { name: '常规' });
    const surface = container.querySelector('.window')!;
    expect(surface.classList.contains('keyboard')).toBe(false);
    await fireEvent.keyDown(general, { key: 'ArrowDown' });
    const devices = screen.getByRole('tab', { name: '设备' });
    expect(devices.getAttribute('aria-selected')).toBe('true');
    expect(document.activeElement).toBe(devices);
    expect(surface.classList.contains('keyboard')).toBe(true);
    await fireEvent.pointerDown(devices);
    expect(surface.classList.contains('keyboard')).toBe(false);
  });

  it('can toggle login at startup on and off without remaining locked', async () => {
    const user = userEvent.setup();
    render(Settings);
    const toggle = await screen.findByRole('switch', { name: '登录时启动' });
    await user.click(toggle);
    await waitFor(() => expect(toggle.getAttribute('aria-checked')).toBe('true'));
    expect((toggle as HTMLButtonElement).matches(':disabled')).toBe(false);
    await user.click(toggle);
    await waitFor(() => expect(toggle.getAttribute('aria-checked')).toBe('false'));
    expect((toggle as HTMLButtonElement).matches(':disabled')).toBe(false);
    expect(setSetting.mock.calls).toEqual([
      ['open_at_login', 'true'],
      ['open_at_login', 'false'],
    ]);
    expect(screen.queryByText('正在保存…')).toBeNull();
  });

  it('allows retry after a failed save and keeps the previous setting', async () => {
    setSetting.mockRejectedValueOnce(new Error('保存失败'));
    const user = userEvent.setup();
    render(Settings);
    const toggle = await screen.findByRole('switch', { name: '登录时启动' });
    await user.click(toggle);
    await screen.findByRole('alert');
    expect(toggle.getAttribute('aria-checked')).toBe('false');
    expect((toggle as HTMLButtonElement).matches(':disabled')).toBe(false);
    await user.click(toggle);
    await waitFor(() => expect(toggle.getAttribute('aria-checked')).toBe('true'));
    expect(screen.queryByRole('alert')).toBeNull();
  });
});
