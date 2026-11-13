import React, { useCallback, useEffect, useRef, useState } from 'react';
import { ChatMessage, OnboardingStep, fetchHistory, fetchOnboardingPlan, toggleStep, sendChatMessage, confirmAccessProposal, statusLabel, refreshSession, Conversation, listConversations, createConversation, deleteConversation } from '../lib/api';
import { OnboardingPage } from './OnboardingPage';
const ClockIcon: React.FC<{ className?: string }> = ({ className }) => (
  <svg className={className} viewBox="0 0 24 24" fill="none" aria-hidden="true">
    <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="2.2" />
    <path
      d="M12 6.2V12l4.2 4.2"
      stroke="currentColor"
      strokeWidth="2.2"
      strokeLinecap="butt"
      strokeLinejoin="miter"
    />
  </svg>
);

const RoadmapIcon: React.FC<{ className?: string }> = ({ className }) => (
  <svg className={className} viewBox="0 0 24 24" fill="none" aria-hidden="true">
    <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="2.2" />
    <path
      d="M12 19V7M12 15H11C8.8 15 7 13.2 7 11V8M12 12H13C15.2 12 17 10.2 17 8V6"
      stroke="currentColor"
      strokeWidth="2.1"
      strokeLinecap="round"
      strokeLinejoin="round"
    />
  </svg>
);


const errorText = (e:unknown) => e instanceof Error ? (e.message === 'send message failed' ? 'Помощник не смог обработать вопрос. Повтори попытку; если ошибка повторится, требуется проверка сервиса ИИ.' : e.message) : 'Не удалось выполнить запрос';
const ordered = (list:ChatMessage[]) => [...new Map(list.map(m=>[m.id,m])).values()].sort((a,b)=>a.timestamp.localeCompare(b.timestamp));
export function ChatPage({userId,profile}:{userId:string;profile:{name:string;email:string;team?:string;track?:string}}) {
  const [messages,setMessages] = useState<ChatMessage[]>([]);
  const [steps,setSteps] = useState<OnboardingStep[]>([]);
  const [input,setInput] = useState('');
  const [error,setError] = useState('');
  const [planError,setPlanError] = useState('');
  const [busy,setBusy] = useState(false);
  const sending = useRef(false);
  const [pending,setPending] = useState<string[]>([]);
  const locks = useRef(new Set<string>());
  const [savingSteps,setSavingSteps] = useState<string[]>([]);
  const [drawer,setDrawer] = useState<null|'history'|'roadmap'|'profile'>(null);
  const [historyLimit,setHistoryLimit]=useState(10);
  const [suggestionsDismissed,setSuggestionsDismissed]=useState(false);
  const textarea=useRef<HTMLTextAreaElement>(null);
  useEffect(()=>{const el=textarea.current;if(!el)return;el.style.height='0px';el.style.height=`${Math.min(Math.max(el.scrollHeight,28),144)}px`;},[input]);
  const [chats,setChats] = useState<Conversation[]>([]);
  const [active,setActive] = useState('');
  const activeRef = useRef('');
  const loadChats=useCallback(async()=>{try{setChats(await listConversations());}catch(e){setError(errorText(e));}},[]);
  const [next,setNext] = useState('');
  const [loading,setLoading] = useState(true);
  const [live,setLive] = useState('Подключаем уведомления…');
  const end = useRef<HTMLDivElement>(null);
  const loadPlan = useCallback(async () => {
    try { setSteps(await fetchOnboardingPlan(userId)); setPlanError(''); }
    catch(e) { setPlanError(errorText(e)); }
  },[userId]);
  const loadHistory = useCallback(async (older=false, page='') => {
    try { const id=activeRef.current;if(!id){setLoading(false);return;} const result = await fetchHistory(page,id); if(activeRef.current!==id)return;
      setMessages(prev=>ordered([...prev,...result.messages]));
      if(older || !page) setNext(result.next); setError('');
    } catch(e) { setError(errorText(e)); } finally { setLoading(false); }
  },[]);
  useEffect(()=>{void loadChats(); void loadPlan();setLoading(false);},[loadChats,loadPlan]);
  useEffect(()=>{end.current?.scrollIntoView({behavior:'smooth'});},[messages.length,busy]);
  useEffect(()=>{
    let stopped=false; let socket:WebSocket|undefined; let timer:ReturnType<typeof setTimeout>; let attempts=0;
    const connect=()=>{
      if(stopped) return;
      socket=new WebSocket(`${location.protocol==='https:'?'wss:':'ws:'}//${location.host}/v1/chat/events`);
      socket.onopen=()=>{attempts=0;setLive('Уведомления подключены');void loadHistory();void loadPlan();};
      socket.onmessage=()=>{void loadHistory();void loadPlan();};
      socket.onclose=()=>{if(stopped)return;setLive('Восстанавливаем уведомления…');
        timer=setTimeout(()=>{refreshSession().then(connect).catch(()=>{if(!stopped){setLive('Уведомления недоступны. Можно обновить данные вручную.');timer=setTimeout(connect,30000);}});},Math.min(1000*2**attempts++,30000));};
    }; connect();
    return ()=>{stopped=true;clearTimeout(timer);socket?.close();};
  },[loadHistory,loadPlan]);
  const send=async(text=input, key=String(Date.now()), failedId?:string)=>{
    text=text.trim(); if(!text || sending.current)return;
    setSuggestionsDismissed(true);sending.current=true;setBusy(true);setError('');
    let chatID=activeRef.current;
    if(!chatID){try{const chat=await createConversation();chatID=chat.id;activeRef.current=chatID;setActive(chatID);setMessages([]);await loadChats();}catch(e){setError(errorText(e));sending.current=false;setBusy(false);return;}}
    if(!failedId){setInput('');setMessages(prev=>[...prev,{id:`local-${key}`,sender:'user',text,timestamp:new Date().toISOString()}]);}
    try {const reply=await sendChatMessage(text,key,chatID);
      setMessages(prev=>ordered([...prev.filter(m=>m.id!==failedId && m.id!==`local-${key}`),reply]));
      await loadHistory();void loadChats();void loadPlan();
    } catch(e){
      setMessages(prev=>[...prev.filter(m=>m.id!==failedId),{id:`error-${key}`,sender:'assistant',text:errorText(e),timestamp:new Date().toISOString(),error:true,idempotency_key:key,retry_text:text}]);
    } finally {sending.current=false;setBusy(false);}
  };
  const confirm=async(id:string)=>{
    if(locks.current.has(id))return;locks.current.add(id);setPending(p=>[...p,id]);
    try {const result=await confirmAccessProposal(id);
      setMessages(prev=>prev.map(m=>m.proposal?.proposal_id===id?{...m,proposal:{...m.proposal,status:result.status,tracker_issue_key:result.tracker_issue_key}}:m));
      void loadPlan();
    }catch(e){setError(errorText(e));}finally{locks.current.delete(id);setPending(p=>p.filter(x=>x!==id));}
  };
  const changeStep=async(id:string)=>{
    const step=steps.find(s=>s.step_id===id);if(!step||locks.current.has(id))return;
    locks.current.add(id);setSavingSteps(p=>[...p,id]);
    try {await toggleStep(userId,step);await loadPlan();}catch(e){setPlanError(errorText(e));}finally{locks.current.delete(id);setSavingSteps(p=>p.filter(x=>x!==id));}
  };
  const visibleMessages = messages;
  const selectChat=async(id:string)=>{if(sending.current)return;activeRef.current=id;setActive(id);setMessages([]);setNext('');setError('');setLoading(true);setSuggestionsDismissed(true);setInput('');setDrawer(null);await loadHistory();};
  const newChat=()=>{if(sending.current)return;activeRef.current='';setActive('');setMessages([]);setNext('');setError('');setInput('');setSuggestionsDismissed(false);setDrawer(null);};
  const removeChat=async(id?:string)=>{if(sending.current)return;
    try{await deleteConversation(id);if(!id||id===activeRef.current)newChat();await loadChats();}catch(e){setError(errorText(e));}
  };
  return <div className="workspace-layout">
    <div className={`drawer-shell${drawer ? ` drawer-shell--open-${drawer}` : ''}`}>
      {drawer === 'roadmap' && <OnboardingPage savingSteps={savingSteps} steps={steps} tasks={[]} isClosing={false} onClose={()=>setDrawer(null)} onToggleStep={changeStep} onSelectStepQuestion={q=>{void send(q);}}/>}
      {drawer === 'profile' && <aside className="drawer-panel profile-panel"><div className="drawer-header"><h2>Профиль</h2><button className="ui-icon-circle" onClick={()=>setDrawer(null)} aria-label="Закрыть профиль">✕</button></div>
        <div className="profile-avatar">{profile.name?.[0] || 'Я'}</div><h3>{profile.name || 'Стажёр'}</h3><dl><dt>Почта</dt><dd>{profile.email || 'Войди повторно, чтобы отобразить почту'}</dd><dt>Команда</dt><dd>{profile.team || 'Не указана'}</dd><dt>Направление</dt><dd>{profile.track || 'Не указано'}</dd></dl></aside>}
      {drawer === 'history' && <aside className="drawer-panel" style={{width:360,minWidth:360}}>
        <div className="drawer-header"><h2>История запросов</h2><button className="ui-icon-circle" onClick={()=>setDrawer(null)} aria-label="Закрыть историю">✕</button></div>
        <button className="suggestion-pill" disabled={busy} onClick={newChat}>Новый вопрос</button>
        {chats.slice(0,historyLimit).map(chat=><div className="history-row" key={chat.id}>
          <button className={`history-entry${active===chat.id?' history-entry--active':''}`} disabled={busy} onClick={()=>void selectChat(chat.id)} title={chat.title}>{chat.title}</button>
          <button className="delete-chat" disabled={busy} aria-label={`Удалить чат ${chat.title}`} onClick={()=>void removeChat(chat.id)}>×</button>
        </div>)}
        {chats.length>historyLimit && <button className="history-load-more" onClick={()=>setHistoryLimit(n=>n+10)}>Загрузить ещё</button>}
        {!chats.length && <p>История пока пуста</p>}
        {!!chats.length && <button className="history-entry" disabled={busy} onClick={()=>{if(window.confirm('Удалить все чаты этого аккаунта?'))void removeChat();}}>Удалить все чаты</button>}
      </aside>}
    </div>
    {drawer && <div className="mobile-drawer-backdrop" onClick={()=>setDrawer(null)}/>}
    <main className="chat-main">
      <div className="floating-nav-icons"><button className="drawer-toggle-btn" aria-label="История запросов" onClick={()=>setDrawer(drawer==='history'?null:'history')}><ClockIcon/></button><button className="drawer-toggle-btn" aria-label="Roadmap" onClick={()=>setDrawer(drawer==='roadmap'?null:'roadmap')}><RoadmapIcon/></button><button className="drawer-toggle-btn profile-nav" aria-label="Профиль" onClick={()=>setDrawer(drawer==='profile'?null:'profile')}><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="10" strokeWidth="1.8"/><circle cx="12" cy="9" r="3" strokeWidth="1.8"/><path d="M6.5 19c.4-5.5 10.6-5.5 11 0" strokeWidth="1.8"/></svg></button></div>
      <div className="chat-scroll-area"><div className="chat-feed-column">
      {error && <p role="alert">{error}<button onClick={()=>void loadHistory()}>Повторить</button></p>}{planError && <p role="alert">План адаптации: {planError}<button onClick={()=>void loadPlan()}>Повторить</button></p>}
        {loading && <p>Загружаем историю…</p>}
        {next && <button disabled={busy} onClick={()=>void loadHistory(true,next)}>Загрузить предыдущие сообщения</button>}
        {visibleMessages.map(m=><article key={m.id} className={m.sender==='user'?'user-msg-bubble':'assistant-msg-card'}>
          {m.sender==='assistant' && <span className="ai-name-badge">AI-buddy</span>}<p style={{whiteSpace:'pre-wrap'}}>{m.text}</p>
          {m.error && <button disabled={busy} onClick={()=>void send(m.retry_text,m.idempotency_key,m.id)}>Повторить</button>}
          {!!m.sources?.length && <ul>{m.sources.map((s,i)=><li key={s.id||i}>{/^https?:\/\//i.test(s.url)?<a href={s.url} target="_blank" rel="noreferrer">{s.title}</a>:s.title}</li>)}</ul>}
          {m.proposal && <section className="proposal-card"><h3>{m.proposal.title}</h3><p>Уровень доступа: {m.proposal.access_level}</p>
            <p role="status">{statusLabel(m.proposal.status)}</p>
            {m.proposal.tracker_issue_key && <p>Заявка: {m.proposal.tracker_issue_key}</p>}
            {['draft','failed'].includes(m.proposal.status) && <button disabled={pending.includes(m.proposal.proposal_id)} onClick={()=>void confirm(m.proposal!.proposal_id)}>{pending.includes(m.proposal.proposal_id)?'Отправляем…':'Отправить заявку'}</button>}
          </section>}
        </article>)}{busy && <p role="status">AI-buddy готовит ответ…</p>}<div ref={end}/>
      </div></div><div className="chat-composer">{!suggestionsDismissed && !messages.length && !loading && <div className="suggestion-list">{['Как выполнить текущий шаг?','Как оформить отгул?','Какие правила работы с персональными данными?'].map(q=><button key={q} className="suggestion-pill" disabled={busy} onClick={()=>void send(q)}>{q}</button>)}</div>}<form className="chat-input-bar" onClick={()=>textarea.current?.focus()} onSubmit={e=>{e.preventDefault();void send();}}>
        <textarea ref={textarea} rows={1} aria-label="Вопрос помощнику" placeholder="Введите вопрос" title="Enter — отправить, Shift + Enter — новая строка" value={input} onChange={e=>setInput(e.target.value)} onKeyDown={e=>{if(e.key==='Enter'&&!e.shiftKey&&!e.nativeEvent.isComposing){e.preventDefault();void send();}}}/>

      </form></div></main>
  </div>;
}
