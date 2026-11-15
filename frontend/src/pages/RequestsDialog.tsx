import React, { useEffect, useRef, useState } from 'react';
import { Conversation, ConversationProposal, fetchConversationProposals, statusLabel } from '../lib/api';

export function RequestsDialog({chats,onClose,onChat}:{chats:Conversation[];onClose:()=>void;onChat:(id:string)=>void}) {
  const backdropPress=useRef(false);
  const dialog=useRef<HTMLDialogElement>(null);
  const [items,setItems]=useState<ConversationProposal[]>([]);
  const [loaded,setLoaded]=useState(false);
  const [loading,setLoading]=useState(true);
  const [failed,setFailed]=useState(0);
  const [version,setVersion]=useState(0);
  useEffect(()=>{dialog.current?.showModal();},[]);
  useEffect(()=>{
    let active=true,running=false;
    const refresh=async()=>{
      if(running)return;
      running=true;setLoading(true);
      try { const result=await fetchConversationProposals(chats);if(active){setItems(result.items);setFailed(result.failed);setLoaded(true);} }
      finally { running=false;if(active)setLoading(false); }
    };
    void refresh();
    const visibleRefresh=()=>{if(document.visibilityState==='visible')void refresh();};
    const timer=setInterval(visibleRefresh,60000);
    window.addEventListener('focus',visibleRefresh);
    document.addEventListener('visibilitychange',visibleRefresh);
    return ()=>{active=false;clearInterval(timer);window.removeEventListener('focus',visibleRefresh);document.removeEventListener('visibilitychange',visibleRefresh);};
  },[chats,version]);
  return <dialog ref={dialog} className="requests-dialog" aria-labelledby="requests-title" onCancel={onClose} onPointerDown={e=>{const r=e.currentTarget.getBoundingClientRect();backdropPress.current=e.target===e.currentTarget && (e.clientX<r.left || e.clientX>r.right || e.clientY<r.top || e.clientY>r.bottom);}} onClick={()=>{if(backdropPress.current){backdropPress.current=false;onClose();}}}>
    <header className="requests-header"><div><h2 id="requests-title">Заявки на доступ</h2></div><button className="buddy-icon-button" aria-label="Закрыть заявки" autoFocus onClick={onClose}>×</button></header>
    <div className="requests-body" aria-busy={loading}>
      {loading && !loaded && <p role="status">Загружаем заявки…</p>}
      {failed>0 && <p role="alert">Не удалось прочитать часть переписок. Список может быть неполным.</p>}
      {loaded && !failed && !items.length && <div className="requests-empty"><h3>Заявок пока нет</h3></div>}
      {items.map(({proposal:p,conversationId})=>{
        const waiting=['pending_dispatch','pending','pending_approval'].includes(p.status);
        const stage=['approved','rejected'].includes(p.status)?2:waiting?1:0;
        return <article className={`proposal-card proposal-card--${waiting?'waiting':p.status==='approved'?'approved':['failed','rejected'].includes(p.status)?'failed':'neutral'}`} key={`${conversationId}:${p.proposal_id}`}>
          <span className="proposal-eyebrow">Заявка на доступ{p.tracker_issue_key?` · ${p.tracker_issue_key}`:''}</span><h3>{p.resource_label || p.resource_id || p.title}</h3>
          <p className="requests-access">Уровень доступа: {p.access_level}</p>
          <ol className="proposal-progress" aria-label="Этапы заявки">{['Подготовлена','Отправлена','Решение'].map((label,i)=><li key={label} className={i<=stage?'is-reached':''} aria-current={i===stage?'step':undefined}><span aria-hidden="true"/>{label}</li>)}</ol>
          <div className="proposal-feedback"><h4>{statusLabel(p.status)}</h4><p>{waiting?'Согласование может занять время. Здесь появится решение после обновления статуса.':p.status==='draft'?'Подтверди отправку заявки в переписке.':p.status==='failed'?'Открой переписку, чтобы повторить отправку.':p.status==='rejected'?'Уточни причину отказа и дальнейшие действия у наставника.':p.status==='approved'?'Заявка согласована.':'Последний статус, полученный от сервера.'}</p></div>
          <button className="buddy-action-button" onClick={()=>{onClose();onChat(conversationId);}}>Открыть переписку</button>
        </article>;
      })}
    </div><footer className="requests-footer"><button className="buddy-action-button dialog-refresh" disabled={loading} aria-busy={loading} aria-label={loading?'Обновляем заявки':'Обновить заявки'} onClick={()=>setVersion(v=>v+1)}><span className={`dialog-refresh-mark${loading?' is-loading':''}`} aria-hidden="true"/>Обновить</button></footer>
  </dialog>;
}
