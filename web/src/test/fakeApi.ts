// fetch palsu untuk test komponen: rute "METHOD /path" → handler. Path dicocokkan tanpa query;
// `:param` cocok dengan satu segmen. Semua panggilan dicatat (method, path, query, body, header).

export interface Call {
  method: string;
  path: string;
  query: URLSearchParams;
  body: unknown;
  headers: Record<string, string>;
}

export type Reply = { status?: number; body?: unknown; headers?: Record<string, string>; text?: string };
type Handler = (c: Call, params: Record<string, string>) => Reply | Promise<Reply>;

export function fakeApi(routes: Record<string, Handler | Reply>) {
  const calls: Call[] = [];
  const table = Object.entries(routes).map(([k, h]) => {
    const [method, pattern] = k.split(" ") as [string, string];
    const names: string[] = [];
    const re = new RegExp("^" + pattern.replace(/:(\w+)/g, (_m, n: string) => (names.push(n), "([^/]+)")) + "$");
    return { method, re, names, h };
  });
  const fn = vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const url = new URL(String(input), "http://localhost");
    const method = (init.method ?? "GET").toUpperCase();
    const headers = Object.fromEntries(Object.entries((init.headers ?? {}) as Record<string, string>));
    const call: Call = {
      method, path: url.pathname, query: url.searchParams, headers,
      body: typeof init.body === "string" ? JSON.parse(init.body) : undefined,
    };
    calls.push(call);
    for (const r of table) {
      const m = r.method === method ? r.re.exec(url.pathname) : null;
      if (!m) continue;
      const params = Object.fromEntries(r.names.map((n, i) => [n, decodeURIComponent(m[i + 1]!)]));
      const rep = typeof r.h === "function" ? await r.h(call, params) : r.h;
      const status = rep.status ?? 200;
      const body = rep.text ?? (rep.body === undefined || status === 204 ? null : JSON.stringify(rep.body));
      return new Response(body, { status, headers: { "Content-Type": "application/json", ...rep.headers } });
    }
    return new Response(JSON.stringify({ error: { code: "NOT_FOUND", message: `no route ${method} ${url.pathname}` } }), { status: 404 });
  });
  vi.stubGlobal("fetch", fn);
  return {
    calls,
    fn,
    /** Panggilan terakhir ke METHOD path (path persis). */
    last(method: string, path: string): Call | undefined {
      return [...calls].reverse().find((c) => c.method === method && c.path === path);
    },
    count(method: string, path: string | RegExp): number {
      return calls.filter((c) => c.method === method && (typeof path === "string" ? c.path === path : path.test(c.path))).length;
    },
  };
}

export const apiError = (status: number, code: string, details?: Record<string, unknown>): Reply => ({
  status,
  body: { error: { code, message: code, details } },
});
