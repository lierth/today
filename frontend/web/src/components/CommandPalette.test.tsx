import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { CommandPalette } from './CommandPalette';
import type { Group } from '@today/shared';

/**
 * cmdk filters its own items by `value`, and every row here carries the
 * server's item id there — so the moment a person typed anything, every
 * result vanished. The palette now turns that filter off and shows exactly
 * what the server matched.
 */
const search = vi.fn<(q: string) => Promise<{ groups: Group[] | null; query: string }>>();

vi.mock('@/lib/api', () => ({
  api: {
    search: (q: string) => search(q),
    item: vi.fn(),
  },
}));

const HIT = {
  query: '站会',
  groups: [
    {
      Label: '今天',
      Items: [
        {
          id: '9c1e0c8e-0b7b-4a0e-9b9e-0a1b2c3d4e5f',
          title: '团队站会',
          all_day: false,
          date_local: '2026-11-20',
          status: 'open',
          pinned: false,
          sort_key: 1,
          created_at: '2026-11-19T08:00:00Z',
          updated_at: '2026-11-19T08:00:00Z',
        },
      ],
    },
  ],
} satisfies { groups: Group[] | null; query: string };

function wrap(children: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <QueryClientProvider client={client}>
      <MemoryRouter>{children}</MemoryRouter>
    </QueryClientProvider>
  );
}

describe('CommandPalette', () => {
  beforeEach(() => {
    search.mockReset();
    search.mockResolvedValue(HIT);
  });

  it('keeps server results visible while the query is typed', async () => {
    const user = userEvent.setup();
    render(wrap(<CommandPalette />));

    await user.keyboard('{Meta>}k{/Meta}');
    const field = await screen.findByPlaceholderText(/搜索/);
    await user.type(field, '站会');

    // The row's `value` is the id, which contains none of the typed text.
    expect(await screen.findByText('团队站会')).toBeTruthy();
  });
});
