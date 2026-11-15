import type { ChatMessage, Proposal } from './api';

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

// Decisions are final in Task. A delayed confirmation/history response may
// still carry an earlier dispatch state, but must not revert that decision.
export function reconcileProposal(current:Proposal, incoming:Proposal):Proposal {
  if(current.proposal_id!==incoming.proposal_id)return incoming;
  const final=current.status==='approved' || current.status==='rejected';
  const staleDraft=incoming.status==='draft' && current.status!=='draft';
  return {...incoming,
    status:final || staleDraft ? current.status : incoming.status,
    tracker_issue_key:incoming.tracker_issue_key || current.tracker_issue_key};
}

export function mergeChatMessages(current:ChatMessage[], incoming:ChatMessage[]):ChatMessage[] {
  const merged=new Map(current.map(message=>[message.id,message]));
  for(const message of incoming) {
    if(message.sender==='user' && !message.id.startsWith('local-')) {
      const optimistic=[...merged.values()].find(item=>item.id.startsWith('local-') && item.text===message.text && Math.abs(Date.parse(item.timestamp)-Date.parse(message.timestamp))<120000);
      if(optimistic)merged.delete(optimistic.id);
    }
    const previous=merged.get(message.id);
    merged.set(message.id,previous?.proposal && message.proposal
      ? {...message,proposal:reconcileProposal(previous.proposal,message.proposal)} : message);
  }
  return [...merged.values()].sort((a,b)=>a.timestamp.localeCompare(b.timestamp));
}
