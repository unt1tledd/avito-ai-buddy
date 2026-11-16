import React, { useEffect, useRef } from 'react';
import { useRequests } from '../lib/useRequests';
import { RequestList } from './RequestList';

export function RequestsDialog({userId,onClose,onChat,onOpenAll}:{userId:string;onClose:()=>void;onChat:(id:string)=>void;onOpenAll?:()=>void}) {
  const backdropPress=useRef(false);
  const dialog=useRef<HTMLDialogElement>(null);
  const {items,loaded,loading,failed,refresh}=useRequests(userId);
  useEffect(()=>{dialog.current?.showModal();},[]);
  return <dialog ref={dialog} className="requests-dialog" aria-labelledby="requests-title" onCancel={onClose} onPointerDown={e=>{const r=e.currentTarget.getBoundingClientRect();backdropPress.current=e.target===e.currentTarget && (e.clientX<r.left || e.clientX>r.right || e.clientY<r.top || e.clientY>r.bottom);}} onClick={()=>{if(backdropPress.current){backdropPress.current=false;onClose();}}}>
    <header className="requests-header"><div><h2 id="requests-title">Заявки на доступ</h2></div><button className="buddy-icon-button" aria-label="Закрыть заявки" autoFocus onClick={onClose}>×</button></header>
    <div className="requests-body" aria-busy={loading}>
      {loading && !loaded && <p role="status">Загружаем заявки…</p>}
      {failed>0 && <p role="alert">Не удалось обновить заявки. Попробуй ещё раз.</p>}
      {loaded && !failed && !items.length && <div className="requests-empty"><h3>Заявок пока нет</h3></div>}
      <RequestList items={items} limit={3} onChat={id=>{onClose();onChat(id);}}/>
    </div><footer className="requests-footer">{onOpenAll && <button className="buddy-action-button" onClick={()=>{onClose();onOpenAll();}}>Все заявки</button>}<button className="buddy-action-button dialog-refresh" disabled={loading} aria-busy={loading} aria-label={loading?'Обновляем заявки':'Обновить заявки'} onClick={refresh}><span className={`dialog-refresh-mark${loading?' is-loading':''}`} aria-hidden="true"/>Обновить</button></footer>
  </dialog>;
}
