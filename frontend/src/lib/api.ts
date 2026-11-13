

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
  completed: boolean;
  current?: boolean;
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
export class ApiError extends Error {
  constructor(public status: number, message: string) { super(message); }
}
async function decode(res: Response) {
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(res.status, data.message || data.error || `Ошибка сервера (${res.status})`);
  return data;
}
export async function refreshSession(): Promise<void> {
  if (!refreshPending) refreshPending = fetch('/v1/auth/refresh', {method:'POST', credentials:'same-origin'})
    .then(decode).then(data => { token = data.access_token; }).finally(() => { refreshPending = null; });
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
export async function fetchHistory(pageToken = ''): Promise<{messages:ChatMessage[]; next:string}> {
  const query = new URLSearchParams({pageSize:'50', pageToken});
  const data = await request(`/v1/chat/messages?${query}`);
  return {messages:(data.messages || []).map(messageFromApi), next:data.nextPageToken || ''};
}
export async function sendChatMessage(text:string, key:string): Promise<ChatMessage> {
  const data = await request('/v1/chat/messages', {method:'POST', body:JSON.stringify({text,idempotencyKey:key})});
  if (!data.assistantMessage) throw new Error('Сервер не вернул ответ помощника');
  return messageFromApi(data.assistantMessage);
}
export async function confirmAccessProposal(id:string): Promise<{status:string; tracker_issue_key:string}> {
  const data = await request(`/v1/chat/proposals/${encodeURIComponent(id)}/confirm`, {method:'POST'});
  if (!data.accessRequest) throw new Error('Сервер не вернул результат заявки');
  return {status:data.accessRequest.status, tracker_issue_key:data.accessRequest.externalRequestId || data.accessRequest.taskId || ''};
}
export async function fetchOnboardingPlan(userId:string): Promise<OnboardingStep[]> {
  const path = `/v1/users/${encodeURIComponent(userId)}/plan`;
  let data;
  try { data = await request(path); }
  catch (err) { if (!(err instanceof ApiError) || err.status !== 404) throw err;
    data = await request(path, {method:'POST', body:'{}'}); }
  let foundCurrent = false;
  return (data.steps || []).map((s:any, i:number) => {
    const completed = s.status === 'completed';
    const current = !completed && !foundCurrent;
    if (current) foundCurrent = true;
    return {step_id:s.id, step_number:i+1, title:s.title, description:s.description,
      period:s.period, resource_id:s.resource_id, completed, current};
  });
}
export async function toggleStep(userId:string, step:OnboardingStep): Promise<void> {
  await request(`/v1/step/${encodeURIComponent(userId)}/${encodeURIComponent(step.step_id)}/completion`,
    step.completed ? {method:'PATCH', body:JSON.stringify({status:'pending'})} : {method:'PUT'});
}
export const statusLabel = (status:string) => ({draft:'Готова к отправке',pending_dispatch:'Отправляется',
  pending:'Ожидает решения',pending_approval:'Ожидает решения',approved:'Доступ одобрен',
  rejected:'Заявка отклонена',failed:'Не удалось отправить'}[status] || status);
