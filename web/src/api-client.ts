export interface RepositorySummary {
  id: string;
  name: string;
  host: string;
  branch?: string;
}

interface AccessDescriptor {
  baseUrl: string;
}

interface APIError {
  error?: { message?: string };
}

export class HubClient {
  async repositories(): Promise<RepositorySummary[]> {
    return requestJSON<RepositorySummary[]>("/api/repositories");
  }

  async access(id: string): Promise<AccessDescriptor> {
    return requestJSON<AccessDescriptor>(`/api/repositories/${encodeURIComponent(id)}/access`);
  }
}

export class RepositoryClient {
  readonly baseUrl: string;

  constructor(access: AccessDescriptor) {
    this.baseUrl = access.baseUrl.replace(/\/$/, "");
  }

  async request<T = unknown>(path: string, options: RequestInit = {}): Promise<T> {
    const headers = new Headers(options.headers);
    return requestJSON<T>(this.baseUrl + path, { ...options, headers });
  }

  async events(signal: AbortSignal, receive: (event: string) => void | Promise<void>): Promise<void> {
    const response = await fetch(this.baseUrl + "/api/events", {
      signal,
    });
    if (!response.ok || !response.body) throw new Error(`Repository event stream returned ${response.status}`);

    const reader = response.body.pipeThrough(new TextDecoderStream()).getReader();
    let pending = "";
    while (true) {
      const { value, done } = await reader.read();
      if (done) return;
      const parsed = parseSSE(pending + value);
      pending = parsed.remainder;
      for (const event of parsed.events) await receive(event);
    }
  }
}

export function parseSSE(input: string): { events: string[]; remainder: string } {
  const normalized = input.replace(/\r\n/g, "\n");
  const blocks = normalized.split("\n\n");
  const remainder = blocks.pop() || "";
  const events = blocks.flatMap((block) => {
    const field = block.split("\n").find((line) => line.startsWith("event:"));
    return field ? [field.slice("event:".length).trim()] : [];
  });
  return { events, remainder };
}

async function requestJSON<T>(url: string, options: RequestInit = {}): Promise<T> {
  const response = await fetch(url, options);
  if (!response.ok) {
    const value = (await response.json().catch(() => ({}))) as APIError;
    throw new Error(value.error?.message || `${response.status} ${response.statusText}`);
  }
  if (response.status === 204) return null as T;
  return response.json() as Promise<T>;
}
