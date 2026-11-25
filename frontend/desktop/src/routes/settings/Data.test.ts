import { describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import Data from './Data.svelte';
import type { SettingsView } from '../../mygo';

const { confirmRestore } = vi.hoisted(() => ({ confirmRestore: vi.fn() }));
vi.mock('../../mygo', () => ({ Panel: { confirmRestore } }));

describe('backup selection', () => {
  it('shows the action target and updates it when that backup disappears', async () => {
    const backup = (name: string) => ({ name, when: name, where: '本机', size: '1 KB' });
    const data = { settings: {}, backups: [backup('new.db'), backup('old.db')] } as SettingsView;
    const view = render(Data, { props: { data, reload: vi.fn() } });
    const select = screen.getByRole('combobox', { name: '选择备份' }) as HTMLSelectElement;
    const user = userEvent.setup();

    await waitFor(() => expect(select.value).toBe('new.db'));
    await user.selectOptions(select, 'old.db');
    await user.click(screen.getByRole('button', { name: '恢复' }));
    expect(confirmRestore).toHaveBeenLastCalledWith('old.db');

    await view.rerender({ data: { ...data, backups: [backup('new.db')] } });
    await waitFor(() => expect(select.value).toBe('new.db'));
    await user.click(screen.getByRole('button', { name: '恢复' }));
    expect(confirmRestore).toHaveBeenLastCalledWith('new.db');

    await view.rerender({ data: { ...data, backups: [] } });
    await waitFor(() => expect(select.value).toBe(''));
    expect((screen.getByRole('button', { name: '恢复' }) as HTMLButtonElement).disabled).toBe(true);
  });
});
