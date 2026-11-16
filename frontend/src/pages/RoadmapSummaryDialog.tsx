import React, { useEffect, useRef } from 'react';
import { OnboardingRoadmap, OnboardingStep } from '../lib/api';

export function nextSummaryStep(data:OnboardingRoadmap,step:OnboardingStep):OnboardingStep|undefined {
  // The flat legacy plan does not establish track membership.
  if(!data.groupingAvailable || !step.track)return undefined;
  const steps=data.steps.filter(item=>item.track===step.track);
  const index=steps.findIndex(item=>item.step_id===step.step_id);
  return index<0?undefined:steps.slice(index+1).find(item=>!item.completed);
}
export function RoadmapSummaryDialog({data,loading,error,onRetry,onClose,onOpen}:{data:OnboardingRoadmap|null;loading:boolean;error:boolean;onRetry:()=>void;onClose:()=>void;onOpen:(step?:OnboardingStep)=>void}) {
  const dialog=useRef<HTMLDialogElement>(null);
  const backdrop=useRef(false);
  useEffect(()=>{dialog.current?.showModal();},[]);
  const current=data?.steps.filter(step=>step.current && !step.completed) || [];
  const completed=Boolean(data?.steps.length && data.steps.every(step=>step.completed));
  return <dialog ref={dialog} className="requests-dialog" aria-labelledby="roadmap-summary-title" onCancel={onClose} onPointerDown={e=>{const r=e.currentTarget.getBoundingClientRect();backdrop.current=e.target===e.currentTarget && (e.clientX<r.left || e.clientX>r.right || e.clientY<r.top || e.clientY>r.bottom);}} onClick={()=>{if(backdrop.current){backdrop.current=false;onClose();}}}>
    <header className="requests-header"><h2 id="roadmap-summary-title">Твой Roadmap</h2><button className="buddy-icon-button" aria-label="Закрыть Roadmap" autoFocus onClick={onClose}>×</button></header>
    <div className="requests-body" aria-busy={loading}>
      {error && data && <p role="alert">Не удалось обновить план. Показаны последние загруженные данные.</p>}
      {loading && !data?<p role="status">Загружаем план адаптации…</p>:error && !data?<div role="alert"><p>Не удалось загрузить план адаптации.</p><button className="buddy-action-button" onClick={onRetry}>Повторить</button></div>:!data?.assigned?<div className="requests-empty"><h3>Твой путь скоро начнётся</h3><p>Бадди назначит треки, и здесь появится твой план адаптации</p></div>:current.length?current.map(step=>{
        const next=nextSummaryStep(data,step);
        return <section className="roadmap-summary-track" key={step.step_id}><h3>{step.current_track_name || 'План адаптации'}</h3><span className="proposal-eyebrow">Текущий шаг</span><h4>{step.title}</h4><span className="proposal-eyebrow">Следующий шаг</span><p>{next?.title || (data.groupingAvailable?'Следующего незавершённого шага в этом треке нет':'Следующий шаг пока не определён')}</p><button className="buddy-action-button" onClick={()=>onOpen(step)}>Открыть текущий шаг</button></section>;
      }):<p>{completed?'Все шаги выполнены. Отличная работа!':'Текущий шаг пока не указан. Посмотри назначенные шаги в Roadmap.'}</p>}
    </div><footer className="requests-footer"><button className="buddy-action-button dialog-refresh" disabled={loading} aria-busy={loading} aria-label={loading?'Обновляем Roadmap':'Обновить Roadmap'} onClick={onRetry}><span className={`dialog-refresh-mark${loading?' is-loading':''}`} aria-hidden="true"/>{data && !data.assigned?'Проверить назначение':'Обновить'}</button></footer>
  </dialog>;
}
