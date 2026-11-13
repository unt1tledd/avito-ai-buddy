import React, { useCallback, useEffect, useRef, useState } from 'react';
import { ChatMessage, OnboardingStep, fetchHistory, fetchOnboardingPlan, toggleStep, sendChatMessage, confirmAccessProposal, statusLabel, refreshSession } from '../lib/api';
import { OnboardingPage } from './OnboardingPage';
const errorText = (e:unknown) => e instanceof Error ? e.message : 'Не удалось выполнить запрос';
const ordered = (list:ChatMessage[]) => [...new Map(list.map(m=>[m.id,m])).values()].sort((a,b)=>a.timestamp.localeCompare(b.timestamp));
export function ChatPage({userId}:{userId:string}) {
  const [messages,setMessages] = useState<ChatMessage[]>([]);
  const [steps,setSteps] = useState<OnboardingStep[]>([]);
  const [input,setInput] = useState('');
  const [error,setError] = useState('');
  const [planError,setPlanError] = useState('');
  const [busy,setBusy] = useState(false);
  const sending = useRef(false);
  const [pending,setPending] = useState<string[]>([]);
  const locks = useRef(new Set<string>());
  const [drawer,setDrawer] = useState(false);
  const [next,setNext] = useState('');
  const [loading,setLoading] = useState(true);
  const [live,setLive] = useState('Подключаем уведомления…');
  const end = useRef<HTMLDivElement>(null);
  const loadPlan = useCallback(async () => {
    try { setSteps(await fetchOnboardingPlan(userId)); setPlanError(''); }
    catch(e) { setPlanError(errorText(e)); }
  },[userId]);
  const loadHistory = useCallback(async (older=false, page='') => {
    try { const result = await fetchHistory(page);
      setMessages(prev=>ordered([...prev,...result.messages]));
      if(older || !page) setNext(result.next); setError('');
    } catch(e) { setError(errorText(e)); } finally { setLoading(false); }
  },[]);
  useEffect(()=>{void loadHistory(); void loadPlan();},[loadHistory,loadPlan]);
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
    sending.current=true;setBusy(true);setError('');
    if(!failedId){setInput('');setMessages(prev=>[...prev,{id:`local-${key}`,sender:'user',text,timestamp:new Date().toISOString()}]);}
    try {const reply=await sendChatMessage(text,key);
      setMessages(prev=>ordered([...prev.filter(m=>m.id!==failedId && m.id!==`local-${key}`),reply]));
      await loadHistory();void loadPlan();
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
    locks.current.add(id);
    try {await toggleStep(userId,step);await loadPlan();}catch(e){setPlanError(errorText(e));}finally{locks.current.delete(id);}
  };
  return <div className="connected-chat">
    <nav className="connected-toolbar"><button className="suggestion-pill" onClick={()=>setDrawer(!drawer)}>Roadmap</button>
      <button className="suggestion-pill" onClick={()=>{void loadHistory();void loadPlan();}}>Обновить данные</button><small role="status">{live}</small></nav>
    {error && <p role="alert">{error}</p>}{planError && <p role="alert">План адаптации: {planError}</p>}
    <div className="connected-content">
      {drawer && <OnboardingPage steps={steps} tasks={[]} isClosing={false} onClose={()=>setDrawer(false)} onToggleStep={changeStep} onSelectStepQuestion={q=>{void send(q);}}/>}
      <main className="connected-main"><div className="connected-messages" aria-live="polite">
        {loading && <p>Загружаем историю…</p>}
        {next && <button disabled={busy} onClick={()=>void loadHistory(true,next)}>Загрузить предыдущие сообщения</button>}
        {!loading && !messages.length && <p>Привет! Спроси о команде, инструментах или следующем шаге адаптации.</p>}
        {messages.map(m=><article key={m.id} className={m.sender==='user'?'user-msg-bubble':'assistant-msg-card'}>
          <strong>{m.sender==='user'?'Ты':'AI-buddy'}</strong><p style={{whiteSpace:'pre-wrap'}}>{m.text}</p>
          {m.error && <button disabled={busy} onClick={()=>void send(m.retry_text,m.idempotency_key,m.id)}>Повторить</button>}
          {!!m.sources?.length && <ul>{m.sources.map((s,i)=><li key={s.id||i}>{/^https?:\/\//i.test(s.url)?<a href={s.url} target="_blank" rel="noreferrer">{s.title}</a>:s.title}</li>)}</ul>}
          {m.proposal && <section className="proposal-card"><h3>{m.proposal.title}</h3><p>Уровень доступа: {m.proposal.access_level}</p>
            <p role="status">{statusLabel(m.proposal.status)}</p>
            {m.proposal.tracker_issue_key && <p>Заявка: {m.proposal.tracker_issue_key}</p>}
            {['draft','failed'].includes(m.proposal.status) && <button disabled={pending.includes(m.proposal.proposal_id)} onClick={()=>void confirm(m.proposal!.proposal_id)}>{pending.includes(m.proposal.proposal_id)?'Отправляем…':'Отправить заявку'}</button>}
          </section>}
        </article>)}{busy && <p role="status">AI-buddy готовит ответ…</p>}<div ref={end}/>
      </div><form className="chat-input-bar" onSubmit={e=>{e.preventDefault();void send();}}>
        <textarea aria-label="Вопрос помощнику" placeholder="Введите вопрос" value={input} onChange={e=>setInput(e.target.value)} onKeyDown={e=>{if(e.key==='Enter'&&!e.shiftKey&&!e.nativeEvent.isComposing){e.preventDefault();void send();}}}/>
        <button disabled={busy||!input.trim()}>Отправить</button>
      </form></main>
    </div>
  </div>;
}
