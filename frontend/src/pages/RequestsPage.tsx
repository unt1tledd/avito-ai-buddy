import React from 'react';
import { useRequests } from '../lib/useRequests';
import { NavigationIcon } from './NavigationIcon';
import { RequestList } from './RequestList';
export function RequestsPage({userId,onChat}:{userId:string;onChat:(id:string)=>void}) {
  const {items,loaded,loading,failed,refresh}=useRequests(userId);
  return <main className="buddy-page requests-page"><section className="requests-page-content" aria-busy={loading}>
    <div className="requests-page-heading"><div><h2>Твои заявки на доступ</h2><p>Согласования и решения по рабочим ресурсам</p></div><button className="buddy-action-button dialog-refresh" disabled={loading} onClick={refresh}><span className={`dialog-refresh-mark${loading?' is-loading':''}`} aria-hidden="true"/>Обновить</button></div>
    {loading && !loaded && <p role="status">Загружаем заявки…</p>}
    {failed>0 && <p role="alert">Не удалось обновить заявки. Попробуй ещё раз.</p>}
    {loaded && !failed && !items.length && <div className="requests-page-empty"><span className="requests-empty-icon" aria-hidden="true"><NavigationIcon name="requests"/></span><h3>Тут появятся твои заявки</h3><p>Когда отправишь заявку на доступ в чате с бадди, здесь можно будет следить за её согласованием</p></div>}
    <RequestList items={items} onChat={onChat}/>
  </section></main>;
}
