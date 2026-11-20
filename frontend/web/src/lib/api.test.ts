import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

/**
 * `api.call` is the one place that reads a response, so the rules it enforces
 * are worth a test of their own:
 *
 *  - a 401 is a session that ended, and must send the browser to /login even
 *    when the body is plain text (the old code only looked at the HTML case
 *    and then ran JSON.parse on "登录已过期");
 *  - anything that is not JSON must not be parsed as JSON.
 */
const realFetch = globalThis.fetch;
let navigated: string[] = [];

async function loadApi() {
  vi.resetModules();
  return import('./api');
}

function reply(status: number, body: string, type: string) {
  return Promise.resolve(new Response(body, { status, headers: { 'Content-Type': type } }));
}

describe('api.call', () => {
  beforeEach(() => {
    navigated = [];
    // jsdom cannot navigate; record the assignment instead.
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: {
        ...window.location,
        get href() {
          return 'http://localhost/';
        },
        set href(next: string) {
          navigated.push(next);
        },
      },
    });
  });

  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  it('sends a 401 to the login page, plain text or not', async () => {
    globalThis.fetch = () => reply(401, '登录已过期', 'text/plain; charset=utf-8');
    const { api } = await loadApi();

    await expect(api.today()).rejects.toMatchObject({ status: 401 });
    expect(navigated).toEqual(['/login']);
  });

  it('does not parse an HTML error body as JSON', async () => {
    globalThis.fetch = () => reply(500, '<!doctype html><p>boom', 'text/html');
    const { api } = await loadApi();

    await expect(api.today()).rejects.toMatchObject({ status: 500 });
    expect(navigated).toEqual([]);
  });

  it('shows the server’s own message when the body is JSON', async () => {
    globalThis.fetch = () => reply(400, '{"error":"新口令至少 8 位"}', 'application/json');
    const { api } = await loadApi();

    await expect(api.today()).rejects.toMatchObject({ message: '新口令至少 8 位' });
  });
});
