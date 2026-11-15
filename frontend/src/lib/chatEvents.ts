// The events endpoint serializes Go entities directly (capitalized fields).
export function shouldRefreshHistory(data: unknown, conversationId: string): boolean {
  if (!conversationId || typeof data !== 'string') return false;
  try {
    const event = JSON.parse(data);
    if (event?.type === 'message.created') {
      const id = event.message?.ConversationID;
      return id != null && String(id) === conversationId;
    }
    // These older notifications have no conversation ID. Re-read the active
    // history to obtain authoritative proposal statuses from the server.
    return event?.type === 'task.status_changed' || event?.type === 'access_request.failed';
  } catch { return false; }
}

export function historyRefreshQueue(refresh: () => Promise<void>, delay = 150) {
  let timer: ReturnType<typeof setTimeout> | undefined;
  let running = false;
  let dirty = false;
  let stopped = false;
  const run = async () => {
    timer = undefined;
    if (stopped || running || !dirty) return;
    dirty = false;
    running = true;
    try { await refresh(); }
    finally {
      running = false;
      if (dirty && !stopped) timer = setTimeout(() => { void run(); }, delay);
    }
  };
  return {
    schedule() {
      if (stopped) return;
      dirty = true;
      if (!running && timer === undefined) timer = setTimeout(() => { void run(); }, delay);
    },
    stop() { stopped = true; dirty = false; clearTimeout(timer); },
  };
}
