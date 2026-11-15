

export interface SourceLink {
  id: string;
  title: string;
  url: string;
}

export interface Proposal {
  proposal_id: string;
  title: string;
  role_label: string;
  resource_id: string;
  resource_label: string;
  access_level: string;
  status: string;
  tracker_issue_key?: string;
}

export interface ChatMessage {
  id: string;
  sender: 'user' | 'assistant';
  text: string;
  timestamp: string;
  idempotency_key?: string;
  retry_text?: string;
  sources?: SourceLink[];
  proposal?: Proposal;
  error?: boolean;
}

export interface OnboardingStep {
  step_id: string;
  step_number: number;
  title: string;
  description: string;
  period: string;
  resource_id?: string;
  action_type?: string;
  completed: boolean;
  current?: boolean;
  current_track_name?: string;
  track?: string;
}

export interface AccessTask {
  task_id: string;
  proposal_id: string;
  resource_id: string;
  title: string;
  assignee: string;
  tracker_issue_key: string;
  status: 'pending_approval' | 'approved';
  created_at: string;
}

export interface HistoryItem {
  id: string;
  dayGroup: 'СЕГОДНЯ' | 'ВЧЕРА (ДЕНЬ 1)';
  timeLabel: string;
  title: string;
  badgeText: string;
  badgeTone: 'green' | 'blue' | 'gray';
  messages: ChatMessage[];
}


let token = '';
let refreshPending: Promise<void> | null = null;
let sessionVersion = 0;
let signedOut = (() => { try { return localStorage.getItem('buddy-signed-out') === 'true'; } catch { return false; } })();
export async function logout(): Promise<void> {
  // Finish any token rotation before revoking its resulting session.
  if(refreshPending)await refreshPending.catch(()=>{});
  await request('/v1/auth/logout', {method:'POST'}, false);
  signedOut = true;
  sessionVersion++;
  token = '';
  refreshPending = null;
  try { localStorage.setItem('buddy-signed-out', 'true'); } catch { /* Storage is optional. */ }
}
export class ApiError extends Error {
  constructor(public status: number, message: string) { super(message); }
}
async function decode(res: Response) {
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(res.status, data.message || data.error || `Ошибка сервера (${res.status})`);
  return data;
}
export async function refreshSession(): Promise<void> {
  if (signedOut) throw new Error('Войди в аккаунт');
  const version = sessionVersion;
  if (!refreshPending) refreshPending = fetch('/v1/auth/refresh', {method:'POST', credentials:'same-origin'})
    .then(decode).then(data => { if(version === sessionVersion && !signedOut) token = data.access_token; })
    .finally(() => { if(version === sessionVersion) refreshPending = null; });
  return refreshPending;
}
export async function request(path: string, init: RequestInit = {}, retry = true): Promise<any> {
  const headers = new Headers(init.headers);
  if (init.body) headers.set('Content-Type', 'application/json');
  if (token) headers.set('Authorization', `Bearer ${token}`);
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 120000);
  let res: Response;
  try { res = await fetch(path, {...init, headers, credentials:'same-origin', signal:init.signal || controller.signal}); }
  finally { clearTimeout(timeout); }
  if (res.status === 401 && retry) { await refreshSession(); return request(path, init, false); }
  return decode(res);
}
export async function login(email: string, password: string): Promise<string> {
  const data = await request('/v1/auth/login', {method:'POST', body:JSON.stringify({email,password})}, false);
  sessionVersion++;
  refreshPending = null;
  signedOut = false;
  try { localStorage.removeItem('buddy-signed-out'); } catch { /* Storage is optional. */ }
  token = data.access_token;
  return currentUser();
}
export async function currentUser(): Promise<string> {
  const data = await request('/v1/auth/validate', {method:'POST'});
  if (!data.is_valid || !data.user_id) throw new Error('Сессия недействительна');
  return String(data.user_id);
}
export function messageFromApi(raw: any): ChatMessage {
  const proposal = raw.proposal;
  const access = proposal?.accessRequest;
  return {id:String(raw.id), sender:raw.role === 'MESSAGE_ROLE_USER' ? 'user' : 'assistant',
    text:raw.text || '', timestamp:raw.createdAt || '',
    sources:(raw.sources || []).map((s:any) => ({id:s.documentId, title:s.title, url:s.url})),
    proposal:proposal ? {proposal_id:String(proposal.id), resource_id:proposal.resourceId,
      resource_label:proposal.resourceLabel, title:`Заявка: ${proposal.resourceLabel}`, role_label:'',
      access_level:proposal.accessLevel, status:access?.status || 'draft',
      tracker_issue_key:access?.externalRequestId || access?.taskId} : undefined};
}
export async function fetchHistory(pageToken = '', conversationId?:string): Promise<{messages:ChatMessage[]; next:string}> {
  const query = new URLSearchParams({pageSize:'50', pageToken});
  const data = await request(`/v1/chat/messages?${query}`, {headers:conversationId ? {'X-Conversation-Id':conversationId} : {}});
  return {messages:(data.messages || []).map(messageFromApi), next:data.nextPageToken || ''};
}
export async function sendChatMessage(text:string, key:string, conversationId?:string): Promise<ChatMessage> {
  const data = await request('/v1/chat/messages', {method:'POST', headers:conversationId ? {'X-Conversation-Id':conversationId} : {}, body:JSON.stringify({text,idempotencyKey:key})});
  if (!data.assistantMessage) throw new Error('Сервер не вернул ответ помощника');
  return messageFromApi(data.assistantMessage);
}
export async function confirmAccessProposal(id:string): Promise<{status:string; tracker_issue_key:string}> {
  const data = await request(`/v1/chat/proposals/${encodeURIComponent(id)}/confirm`, {method:'POST'});
  if (!data.accessRequest?.status) throw new Error('Сервер не вернул результат заявки');
  return {status:data.accessRequest.status, tracker_issue_key:data.accessRequest.externalRequestId || data.accessRequest.taskId || ''};
}
export interface OnboardingTrack {
  track: string;
  display_name: string;
  current_step_id?: string;
}
export interface OnboardingRoadmap {
  assigned: boolean;
  steps: OnboardingStep[];
  tracks: OnboardingTrack[];
  groupingAvailable: boolean;
  assignedTrack?: string;
}
function onboardingStepFromApi(s:any, i:number):OnboardingStep {
  return {step_id:String(s.id),step_number:i+1,title:s.title || '',description:s.description || '',
    period:s.period || '',resource_id:s.resource_id,action_type:s.action_type,
    completed:s.status==='completed',current:false,track:s.track || undefined};
}
async function readOnboardingPlan(userId:string):Promise<any|null> {
  try { return await request(`/v1/users/${encodeURIComponent(userId)}/plan`); }
  catch(err) {
    if(err instanceof ApiError && err.status===404)return null;
    throw err;
  }
}
// Retained for callers that need the flat list. A current step must only
// come from plan_context, never from a client-side first-incomplete guess.
export async function fetchOnboardingPlan(userId:string):Promise<OnboardingStep[]> {
  const data=await readOnboardingPlan(userId);
  return (data?.steps || []).map(onboardingStepFromApi);
}
export async function fetchOnboardingRoadmap(userId:string):Promise<OnboardingRoadmap> {
  const data=await readOnboardingPlan(userId);
  if(!data)return {assigned:false,steps:[],tracks:[],groupingAvailable:false};
  const context=await request(`/v1/users/${encodeURIComponent(userId)}/plan_context`);
  const tracks:OnboardingTrack[]=(context.tracks || []).map((track:any)=>({
    track:track.track,display_name:track.display_name || track.track,
    current_step_id:track.current_step?.id ? String(track.current_step.id) : undefined,
  }));
  const currentTracks=new Map(tracks.filter(track=>track.current_step_id).map(track=>[track.current_step_id,track]));
  const steps:OnboardingStep[]=(data.steps || []).map(onboardingStepFromApi).map((step:OnboardingStep)=>({...step,
    current:currentTracks.has(step.step_id),current_track_name:currentTracks.get(step.step_id)?.display_name}));
  // Older servers omit step.track. Keep the full plan usable without
  // inventing track membership or silently hiding unclassified steps.
  const groupingAvailable=steps.length>0 && steps.every(step=>Boolean(step.track));
  // Buttons correspond to the actual groups in the assigned steps.
  // Context only supplies their display names and authoritative current step.
  const assignedTracks=groupingAvailable
    ? [...new Set(steps.map(step=>step.track!))].map(track=>tracks.find(item=>item.track===track) || {track,display_name:track})
    : tracks;
  return {assigned:true,steps,tracks:assignedTracks,groupingAvailable,assignedTrack:data.track || undefined};
}
export async function toggleStep(userId:string, step:OnboardingStep): Promise<void> {
  await request(`/v1/step/${encodeURIComponent(userId)}/${encodeURIComponent(step.step_id)}/completion`,
    step.completed ? {method:'PATCH', body:JSON.stringify({status:'pending'})} : {method:'PUT'});
}
export const statusLabel = (status:string) => ({draft:'Готова к отправке',pending_dispatch:'В очереди',
  pending:'Заявка отправлена · ожидает решения',pending_approval:'Заявка отправлена · ожидает решения',approved:'Доступ одобрен',
  rejected:'Заявка отклонена',failed:'Не удалось отправить'}[status] || status);

export interface Conversation {id:string;title:string;createdAt:string;lastActivityAt?:string}
export function recentConversations(items:Conversation[]):Conversation[] {
  const timestamp=(value:string)=>Date.parse(value)||0;
  return [...items].sort((a,b)=>timestamp(b.lastActivityAt || b.createdAt)-timestamp(a.lastActivityAt || a.createdAt)
    || timestamp(b.createdAt)-timestamp(a.createdAt) || b.id.localeCompare(a.id,undefined,{numeric:true}));
}
export async function listConversations():Promise<Conversation[]> {
  const items:Conversation[]=(await request('/v1/chat/conversations')).conversations || [];
  // The list contract exposes creation time only. Read one latest message
  // per conversation, with bounded concurrency, until it exposes activity.
  let cursor=0;
  await Promise.all(Array.from({length:Math.min(4,items.length)},async()=>{
    while(cursor<items.length) {
      const item=items[cursor++];
      try {
        const data=await request('/v1/chat/messages?pageSize=1',{headers:{'X-Conversation-Id':item.id}});
        const latest=data.messages?.[0]?.createdAt;
        if(latest)item.lastActivityAt=latest;
      } catch { /* An unavailable history must not hide the conversation list. */ }
    }
  }));
  return recentConversations(items);
}
export async function createConversation():Promise<Conversation> { return request('/v1/chat/conversations',{method:'POST'}); }
export async function deleteConversation(id?:string):Promise<void> { await request('/v1/chat/conversations'+(id?`/${encodeURIComponent(id)}`:''),{method:'DELETE'}); }

export function stepQuestion(step: OnboardingStep): string {
  return step.action_type === 'access_request'
    ? `Как получить доступ для шага «${step.title}»?${step.resource_id ? ` Ресурс: ${step.resource_id}.` : ''}`
    : `Как выполнить шаг адаптации «${step.title}»?`;
}

export interface ConversationProposal { proposal: Proposal; conversationId: string; timestamp: string }
export async function fetchConversationProposals(conversations:Conversation[]):Promise<{items:ConversationProposal[]; failed:number}> {
  const items:ConversationProposal[]=[];
  let index=0,failed=0;
  await Promise.all(Array.from({length:Math.min(4,conversations.length)},async()=>{
    while(index<conversations.length) {
      const chat=conversations[index++];
      try {
        const found=new Map<string,ConversationProposal>();
        const tokens=new Set<string>();
        let token='';
        do {
          if(tokens.has(token))throw new Error('Repeated history page');
          tokens.add(token);
          const page=await fetchHistory(token,chat.id);
          for(const message of page.messages)if(message.proposal) {
            const previous=found.get(message.proposal.proposal_id);
            if(!previous || Date.parse(message.timestamp)>Date.parse(previous.timestamp))found.set(message.proposal.proposal_id,{proposal:message.proposal,conversationId:chat.id,timestamp:message.timestamp});
          }
          token=page.next;
        } while(token);
        items.push(...found.values());
      } catch { failed++; }
    }
  }));
  const final=(status:string)=>['approved','rejected'].includes(status);
  items.sort((a,b)=>Number(final(a.proposal.status))-Number(final(b.proposal.status)) || (Date.parse(b.timestamp)||0)-(Date.parse(a.timestamp)||0));
  return {items,failed};
}


export function friendlyError(error:unknown):string {
  if(error instanceof ApiError) {
    if(error.status===401)return 'Сессия истекла. Войди в аккаунт снова.';
    if(error.status===403)return 'Нет доступа к этому действию.';
    if(error.status>=500)return 'Сервис временно недоступен. Попробуй ещё раз чуть позже.';
  }
  if(error instanceof Error && error.name==='AbortError')return 'Не удалось дождаться ответа. Попробуй ещё раз.';
  if(error instanceof TypeError || error instanceof Error && /load failed|failed to fetch|network|send message failed/i.test(error.message))return 'Не удалось получить ответ. Проверь соединение и попробуй ещё раз.';
  return 'Не удалось выполнить действие. Попробуй ещё раз.';
}
export async function fetchUserRequests(userId:string):Promise<ConversationProposal[]> {
  const items:ConversationProposal[]=[];
  let after='0';
  const statuses:Record<string,string>={ACCESS_REQUEST_STATUS_PENDING:'pending_approval',ACCESS_REQUEST_STATUS_APPROVED:'approved',ACCESS_REQUEST_STATUS_REJECTED:'rejected','1':'pending_approval','2':'approved','3':'rejected'};
  for(;;) {
    const page=await request(`/v1/tasks/${encodeURIComponent(userId)}?limit=100&after_id=${after}`);
    if(!Array.isArray(page))throw new Error('Invalid tasks response');
    for(const task of page)items.push({conversationId:'',timestamp:task.createdAt || '',proposal:{proposal_id:String(task.proposalId),title:'Заявка на доступ',role_label:'',resource_id:task.resourceId,resource_label:task.resourceId,access_level:task.accessLevel,status:statuses[String(task.status)] || String(task.status || 'unknown'),tracker_issue_key:task.externalRequestId || String(task.taskId)}});
    if(page.length<100)break;
    const next=String(page[page.length-1].taskId);
    if(!/^\d+$/.test(next) || BigInt(next)<=BigInt(after))throw new Error('Invalid tasks pagination');
    after=next;
  }
  return items.sort((a,b)=>(Date.parse(b.timestamp)||0)-(Date.parse(a.timestamp)||0));
}
