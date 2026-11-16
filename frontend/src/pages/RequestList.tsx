import React from 'react';
import { ConversationProposal, statusLabel } from '../lib/api';
export function RequestList({items,limit,onChat}:{items:ConversationProposal[];limit?:number;onChat?:(id:string)=>void}) {
  const visible=limit===undefined?items:items.slice(0,limit);
  return <div className="request-list">      {visible.map(({proposal:p,conversationId})=>{
        const waiting=['pending_dispatch','pending','pending_approval'].includes(p.status);
        const stage=['approved','rejected'].includes(p.status)?2:waiting?1:0;
        return <article className={`proposal-card proposal-card--${waiting?'waiting':p.status==='approved'?'approved':['failed','rejected'].includes(p.status)?'failed':'neutral'}`} key={`${conversationId}:${p.proposal_id}`}>
          <span className="proposal-eyebrow">Заявка на доступ{p.tracker_issue_key?` · ${p.tracker_issue_key}`:''}</span><h3>{p.resource_label || p.resource_id || p.title}</h3>
          <p className="requests-access">Уровень доступа: {p.access_level}</p>
          <ol className="proposal-progress" aria-label="Этапы заявки">{['Подготовлена','Отправлена','Решение'].map((label,i)=><li key={label} className={i<=stage?'is-reached':''} aria-current={i===stage?'step':undefined}><span aria-hidden="true"/>{label}</li>)}</ol>
          <div className="proposal-feedback"><h4>{statusLabel(p.status)}</h4><p>{waiting?'Согласование может занять время. Здесь появится решение после обновления статуса.':p.status==='draft'?'Подтверди отправку заявки в переписке.':p.status==='failed'?'Открой переписку, чтобы повторить отправку.':p.status==='rejected'?'Уточни причину отказа и дальнейшие действия у наставника.':p.status==='approved'?'Заявка согласована.':'Последний статус, полученный от сервера.'}</p></div>
          {conversationId && <button className="buddy-action-button" onClick={()=>{onChat?.(conversationId);}}>Открыть переписку</button>}
        </article>;
      })}</div>;
}
