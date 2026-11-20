import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { QuickAdd } from './QuickAdd';
import { TooltipProvider } from '@/components/ui/tooltip';
import type { QuickAddResult } from '@today/shared';

const parse = vi.fn<(raw: string) => Promise<QuickAddResult>>();
const create = vi.fn<(draft: unknown) => Promise<unknown>>();

vi.mock('@/lib/api', () => ({
  api: {
    parse: (raw: string) => parse(raw),
    create: (draft: unknown) => create(draft),
  },
}));

const TAGS: QuickAddResult = {
  title: '站会',
  date: '2026-11-20',
  time: '10:30',
  rrule: '',
  tags: [
    { kind: 'date', label: '11月20日', text: '明天' },
    { kind: 'time', label: '10:30', text: '10:30' },
  ],
};

/** QuickAdd's chips carry tooltips, and a tooltip needs its provider. */
function show(props: Partial<Parameters<typeof QuickAdd>[0]> = {}) {
  return render(
    <TooltipProvider>
      <QuickAdd {...props} />
    </TooltipProvider>,
  );
}

describe('QuickAdd', () => {
  beforeEach(() => {
    parse.mockReset();
    // Every keystroke asks the parser, so there is always a call beyond the
    // one a test pins: without a default the mock returns undefined and the
    // component's `.then` throws. Silence is the honest default.
    parse.mockResolvedValue({ title: '', date: '', time: '', rrule: '', tags: [] });
    create.mockReset();
    create.mockResolvedValue({});
  });

  it('shows a chip per fragment and deletes only that slice', async () => {
    parse.mockResolvedValue(TAGS);
    const user = userEvent.setup();
    show();

    const field = screen.getByLabelText('添加事项');
    await user.type(field, '明天 10:30 站会');

    await waitFor(() => expect(screen.getByText('11月20日')).toBeTruthy());
    expect(screen.getByText('10:30')).toBeTruthy();

    // Dropping the date leaves the time and the title alone.
    parse.mockResolvedValue({ ...TAGS, tags: [TAGS.tags![1]!] });
    await user.click(screen.getByLabelText('去掉 11月20日'));

    expect((field as HTMLInputElement).value).toBe('10:30 站会');
    await waitFor(() => expect(screen.queryByText('11月20日')).toBeNull());
    expect(screen.getByText('10:30')).toBeTruthy();
  });

  it('ignores a slow answer for text that has since changed', async () => {
    let first: (value: QuickAddResult) => void = () => {};
    parse.mockImplementationOnce(
      () =>
        new Promise<QuickAddResult>((resolve) => {
          first = resolve;
        }),
    );
    parse.mockResolvedValue({ ...TAGS, tags: [] });
    const user = userEvent.setup();
    show();

    const field = screen.getByLabelText('添加事项');
    await user.type(field, '明');
    await user.type(field, '天');

    // The first keystroke's answer arrives last; it must not put "明天" back.
    first(TAGS);
    await waitFor(() => expect(screen.queryByText('11月20日')).toBeNull());
  });

  it('re-parses when the last words were never read', async () => {
    // The chips belong to "明天 10:30 站会". The parse for what follows is
    // still in flight when Enter arrives, so the chips are older than the
    // text: sending them with parsed=true told the server to keep them and
    // the item was saved with the wrong title and date.
    parse.mockImplementation((raw: string) =>
      raw.includes('站会') && raw.trim() === '明天 10:30 站会'
        ? Promise.resolve(TAGS)
        : new Promise<QuickAddResult>(() => {}),
    );
    const user = userEvent.setup();
    show();

    const field = screen.getByLabelText('添加事项');
    await user.type(field, '明天 10:30 站会');
    await waitFor(() => expect(screen.getByText('11月20日')).toBeTruthy());

    await user.type(field, '，再确认一下{Enter}');

    await waitFor(() => expect(create).toHaveBeenCalled());
    const sent = create.mock.calls[0]![0] as { parsed: boolean; raw: string; title: string };
    expect(sent.parsed).toBe(false);
    expect(sent.raw).toBe('明天 10:30 站会，再确认一下');
    expect(sent.title).toBe('明天 10:30 站会，再确认一下');
  });

  it('does not send a stale reading after saving', async () => {
    // Saving clears the line; a parse answer that was still in flight used to
    // land afterwards and put its chips back on an empty box.
    let slow: (value: QuickAddResult) => void = () => {};
    parse.mockImplementation((raw: string) => {
      if (raw === '明天 10:30 站会x') {
        return new Promise<QuickAddResult>((resolve) => {
          slow = resolve;
        });
      }
      return Promise.resolve(TAGS);
    });
    const user = userEvent.setup();
    show();

    const field = screen.getByLabelText('添加事项');
    await user.type(field, '明天 10:30 站会');
    await waitFor(() => expect(screen.getByText('11月20日')).toBeTruthy());

    await user.type(field, 'x{Enter}');
    await waitFor(() => expect(create).toHaveBeenCalled());

    slow(TAGS);
    await waitFor(() => expect(screen.queryByText('11月20日')).toBeNull());
  });

  // Two Enter presses in the same frame both read the old `busy`, which is how
  // one line of text became two rows on the server.
  it('sends one write when Enter is pressed twice in a row', async () => {
    parse.mockResolvedValue(TAGS);
    let finish = () => {};
    create.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = () => resolve({});
        }),
    );
    const user = userEvent.setup();
    show();

    const field = screen.getByLabelText('添加事项');
    await user.type(field, '明天 10:30 站会');
    await user.keyboard('{Enter}');
    await user.keyboard('{Enter}');
    await waitFor(() => expect(create).toHaveBeenCalledTimes(1));
    finish();
    await waitFor(() => expect((field as HTMLInputElement).value).toBe(''));
  });

  it("submits with the parser's reading and clears the line", async () => {
    parse.mockResolvedValue(TAGS);
    const saved = vi.fn();
    const user = userEvent.setup();
    show({ onSaved: saved });

    const field = screen.getByLabelText('添加事项');
    await user.type(field, '明天 10:30 站会{Enter}');

    await waitFor(() => expect(saved).toHaveBeenCalled());
    expect((field as HTMLInputElement).value).toBe('');
  });
});
