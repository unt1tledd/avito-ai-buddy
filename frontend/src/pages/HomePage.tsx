import React, { useEffect, useState } from 'react';
import { Conversation, OnboardingRoadmap, OnboardingStep, fetchOnboardingRoadmap, recentConversations } from '../lib/api';

import { RoadmapSummaryDialog } from './RoadmapSummaryDialog';
import { RequestsDialog } from './RequestsDialog';

export function HomePage({userId,name,chats,busy,onChat,onQuestion,onRoadmap}:{userId:string;name:string;chats:Conversation[];busy:boolean;onChat:(id:string)=>void;onQuestion:(question:string)=>void;onRoadmap:(step?:OnboardingStep)=>void}) {
  const [opened,setOpened]=useState<string>(()=>{try{return sessionStorage.getItem(`buddy-home-dialog-${userId}`)||'';}catch{return '';}});
  const setDialog=(value:string)=>{setOpened(value);try{sessionStorage.setItem(`buddy-home-dialog-${userId}`,value);}catch{/* Storage is optional. */}};
  const [data,setData]=useState<OnboardingRoadmap|null>(null);
  const [loading,setLoading]=useState(true);
  const [error,setError]=useState(false);
  const [version,setVersion]=useState(0);
  useEffect(()=>{
    let active=true;
    setLoading(true);setError(false);
    fetchOnboardingRoadmap(userId).then(result=>{if(active)setData(result);}).catch(()=>{if(active)setError(true);}).finally(()=>{if(active)setLoading(false);});
    return ()=>{active=false;};
  },[userId,version]);
  const last=recentConversations(chats)[0];
  const current=data?.steps.filter(step=>step.current && !step.completed) || [];
  const done=data?.steps.filter(step=>step.completed).length || 0;
  const total=data?.steps.length || 0;
  const roadmapLabel=loading?'Загружаем план адаптации…':error?'Не удалось загрузить план адаптации':!data?.assigned?'Твой путь скоро начнётся — бадди ещё не назначил план':total>0 && done===total?'Все шаги выполнены':current.length?current.map(step=>`${step.current_track_name ? step.current_track_name + ': ' : ''}${step.title}`).join(' · '):'Текущий шаг пока не указан';
  return <main className="buddy-page buddy-home">
    <div className="buddy-intro"><h2>Привет{name.trim()?`, ${name.trim().split(/\s+/)[0]}`:''}!</h2><p>Я AI-buddy — твой помощник на стажировке. Помогу освоиться в команде, разобраться в рабочих процессах и найти нужную информацию.</p><p>Задавай вопросы в чате, а свой путь адаптации сможешь увидеть в Roadmap. Будем двигаться шаг за шагом.</p></div>
    <section className="home-activity" aria-labelledby="home-activity-title">
      <h3 id="home-activity-title">Последняя активность</h3>
      <button className="home-activity-row" disabled={busy} onClick={()=>last?onChat(last.id):onQuestion('')}>
        <span className="home-activity-icon home-activity-icon--chat" aria-hidden="true"><svg viewBox="0 0 24 24"><path d="M21 11.5a8.5 8.5 0 0 1-8.5 8.5H4l-2 2V11.5a9.5 9.5 0 0 1 19 0Z"/></svg></span>
        <span className="home-activity-copy"><strong>Последний чат</strong><span>{busy && !last?'Загружаем переписки…':last?.title || 'Пока нет переписок — задай первый вопрос бадди'}</span></span>
        <span className="home-activity-action">{last?'Открыть чат':'Новый чат'}</span><span className="home-activity-chevron" aria-hidden="true">›</span>
      </button>
      <button className="home-activity-row" onClick={()=>setDialog('roadmap')} aria-haspopup="dialog" aria-busy={loading}>
        <span className="home-activity-icon home-activity-icon--roadmap" aria-hidden="true"><svg viewBox="0 0 24 24"><circle cx="6" cy="5" r="2"/><circle cx="18" cy="19" r="2"/><path d="M6 7v6a4 4 0 0 0 4 4h4a4 4 0 0 0 0-8h-3M18 15v2"/></svg></span>
        <span className="home-activity-copy"><strong>Roadmap</strong><span>{roadmapLabel}</span></span>
        {!loading && !error && data?.assigned && total>0 && <span className="home-activity-progress"><span>{done} из {total} шагов выполнено</span><progress value={done} max={total} aria-label="Прогресс адаптации"/></span>}
        <span className="home-activity-chevron" aria-hidden="true">›</span>
      </button>
      <button className="home-activity-row" disabled={busy} onClick={()=>setDialog('requests')} aria-haspopup="dialog">
        <span className="home-activity-icon home-activity-icon--requests" aria-hidden="true"><svg viewBox="0 0 24 24"><path d="M14 3H5v18h14V8Z M14 3v5h5M8 12h8M8 16h5"/></svg></span>
        <span className="home-activity-copy"><strong>Заявки</strong><span>Доступы и статусы согласования</span></span><span className="home-activity-chevron" aria-hidden="true">›</span>
      </button>
      {error && <button className="home-text-button" onClick={()=>setVersion(v=>v+1)}>Повторить загрузку плана</button>}
    </section>
    {opened==='requests' && <RequestsDialog userId={userId} onClose={()=>setDialog('')} onChat={onChat}/>}
    {opened==='roadmap' && <RoadmapSummaryDialog data={data} loading={loading} error={error} onRetry={()=>setVersion(v=>v+1)} onClose={()=>setDialog('')} onOpen={step=>{setDialog('');onRoadmap(step);}}/>}
  </main>;
}
