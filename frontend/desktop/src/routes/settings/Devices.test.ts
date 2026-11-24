import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import Devices from './Devices.svelte';
import type { SettingsView } from '../../mygo';

const { revoke, token } = vi.hoisted(() => ({ revoke: vi.fn(), token: vi.fn() }));
vi.mock('../../mygo', () => ({ Panel: { revokeDevice: revoke, deviceToken: token } }));
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
    token.mockReset();
    vi.spyOn(window, 'confirm').mockReturnValue(false);
  });

  it('leaves a connected board paired when cancellation is declined', async () => {
    render(Devices, { props: { data, reload: vi.fn() } });
    await userEvent.setup().click(screen.getByRole('button', { name: '取消配对' }));
    expect(window.confirm).toHaveBeenCalled();
    expect(revoke).not.toHaveBeenCalled();
  });

  it('does not replace a connected board token without confirmation', async () => {
    render(Devices, { props: { data, reload: vi.fn() } });
    await userEvent.setup().click(screen.getByRole('button', { name: '生成配对口令' }));
    expect(window.confirm).toHaveBeenCalled();
    expect(token).not.toHaveBeenCalled();
  });
});
