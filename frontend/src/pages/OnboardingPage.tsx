import React, { useCallback, useEffect, useRef, useState } from 'react';
import { OnboardingRoadmap, OnboardingStep, fetchOnboardingRoadmap, stepQuestion, toggleStep } from '../lib/api';

type Selection = { track: string; step: string };
const emptySelection:Selection = {track:'',step:''};
const ALL_TRACKS = '__all__';
const errorText = (error:unknown) => error instanceof Error ? error.message : 'Не удалось загрузить план адаптации';

// Advance only the selected step that was just completed, and only to the
// current step of that same track explicitly returned by the server.
export function nextRoadmapStep(before:OnboardingRoadmap|null, after:OnboardingRoadmap|null, selectedId:string):string {
  const previous=before?.steps.find(step=>step.step_id===selectedId);
  const saved=after?.steps.find(step=>step.step_id===selectedId);
  if(!previous || previous.completed || !saved?.completed)return selectedId;
  const track=previous.track || before?.tracks.find(item=>item.current_step_id===selectedId)?.track;
  const next=track?after?.tracks.find(item=>item.track===track)?.current_step_id:undefined;
  return next && after?.steps.some(step=>step.step_id===next)?next:selectedId;
}

function RoadmapIcon({kind='route'}:{kind?:'route'|'refresh'|'chevron'|'buddy'|'check'}) {
  return <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    {kind==='route'?<><circle cx="6" cy="5" r="2"/><circle cx="18" cy="19" r="2"/><path d="M6 7v6a4 4 0 0 0 4 4h4a4 4 0 0 0 0-8h-3M18 15v2"/></>
      :kind==='refresh'?<><path d="M20 7v5h-5M4 17v-5h5M5 7a8 8 0 0 1 13-2l2 2M4 17l2 2a8 8 0 0 0 13-2"/></>
      :kind==='chevron'?<path d="m6 9 6 6 6-6"/>
      :kind==='check'?<path d="m5 12 4 4L19 6"/>
      :<><rect x="4" y="6" width="16" height="14" rx="5"/><path d="M12 3v3M8 12h.01M16 12h.01M9 16h6"/></>}
  </svg>;
}

interface OnboardingPageProps {
  data: OnboardingRoadmap | null;
  loading?: boolean;
  error?: string;
  onRetry: () => void;
  onToggleStep: (step:OnboardingStep) => void;
  onSelectStepQuestion: (question:string) => void;
  savingStep?: string;
  chatBusy?: boolean;
  userId: string;
}

export function OnboardingPage({data,loading=false,error,onRetry,onToggleStep,onSelectStepQuestion,savingStep='',chatBusy=false,userId}:OnboardingPageProps) {
  const [selection,setSelection]=useState<Selection>(()=>{
    try {
      const saved=JSON.parse(localStorage.getItem(`buddy-roadmap-${userId}`)||'null');
      if(saved)return {track:typeof saved.track==='string'?saved.track:'',step:typeof saved.step==='string'?saved.step:''};
    } catch { /* Preferences are optional. */ }
    return emptySelection;
  });
  const detail=useRef<HTMLElement>(null);
  const previousData=useRef(data);
  useEffect(()=>{
    const next=nextRoadmapStep(previousData.current,data,selection.step);
    previousData.current=data;
    if(next!==selection.step)setSelection(previous=>({...previous,step:next}));
  },[data,selection.step]);
  useEffect(()=>{
    try { localStorage.setItem(`buddy-roadmap-${userId}`,JSON.stringify(selection)); } catch { /* Preferences are optional. */ }
  },[selection,userId]);
  const assignedName=data?.tracks.find(track=>track.track===data.assignedTrack)?.display_name || data?.assignedTrack;
  const options = data?.groupingAvailable ? data.tracks : [{track:ALL_TRACKS,display_name:assignedName?`План адаптации · ${assignedName}`:'Назначенный план'}];
  const currentLabel=(step:OnboardingStep)=>step.current_track_name?`Текущий шаг · ${step.current_track_name}`:'Текущий шаг';
  const activeTrack=options.find(track=>track.track===selection.track) || options[0];
  const steps=(data?.steps || []).filter(step=>!data?.groupingAvailable || step.track===activeTrack?.track);
  const selected=steps.find(step=>step.step_id===selection.step);
  const done=steps.filter(step=>step.completed).length;
  const percent=steps.length?Math.round(done/steps.length*100):0;
  const chooseStep=(step:OnboardingStep)=>{
    setSelection(prev=>({...prev,step:step.step_id}));
    if(window.matchMedia('(min-width: 761px) and (max-width: 900px)').matches)requestAnimationFrame(()=>detail.current?.scrollIntoView({behavior:'smooth',block:'start'}));
  };
  const renderStep=(step:OnboardingStep)=>{
    const number=steps.findIndex(item=>item.step_id===step.step_id)+1;
    return <li key={step.step_id} className={`roadmap-step${step.completed?' is-done':''}${step.current && !step.completed?' is-current':''}`}>
      <button type="button" role="checkbox" className="roadmap-step-marker" aria-checked={step.completed} aria-busy={savingStep===step.step_id}
        aria-label={`${step.completed?'Снять отметку':'Отметить выполненным'}: ${step.title}`}
        title={step.completed?'Снять отметку выполнения':'Отметить выполненным'} disabled={loading || Boolean(savingStep) || Boolean(error)} onClick={()=>onToggleStep(step)}>
        {savingStep===step.step_id?<span className="roadmap-check-spinner" aria-hidden="true"/>:<RoadmapIcon kind="check"/>}
      </button>
      <div className="roadmap-step-main"><button type="button" className={`roadmap-step-card${selected?.step_id===step.step_id?' is-selected':''}`} aria-pressed={selected?.step_id===step.step_id} onClick={()=>chooseStep(step)}>
        <span className="roadmap-step-meta">Шаг {number}{step.period?` · ${step.period}`:''}</span>
        <span className="roadmap-step-title">{step.title}</span>
        <span className="roadmap-step-status">{step.completed?'Выполнено':step.current?currentLabel(step):'Предстоит'}</span>
      </button><button className="roadmap-step-ask" disabled={chatBusy} aria-label={`Спросить бадди: ${step.title}`} onClick={()=>onSelectStepQuestion(stepQuestion(step))}><RoadmapIcon kind="buddy"/>Спросить бадди</button></div>
    </li>;
  };
  if(!data && loading)return <div className="roadmap-placeholder" role="status"><span className="roadmap-loading-mark"/><p>Загружаем Roadmap…</p></div>;
  if(!data && error)return <div className="roadmap-placeholder" role="alert"><h2>Не удалось загрузить Roadmap</h2><p>{error}</p><button className="buddy-action-button" onClick={onRetry}>Повторить</button></div>;
  if(!data || !data.assigned)return <div className="roadmap-placeholder"><span className="buddy-state-icon"><RoadmapIcon/></span><h2>Твой путь скоро начнётся</h2><p>Бадди назначит треки, и здесь появится твой план адаптации</p><button className="buddy-action-button" disabled={loading} onClick={onRetry}>Проверить назначение</button>{error && <p role="alert">{error}</p>}</div>;
  return <section className="roadmap-content" aria-label="План адаптации" aria-busy={loading}>
    <div className="roadmap-toolbar"><h2>Твой план адаптации</h2></div>
    {error && <div className="roadmap-error" role="alert"><span>{error}</span><button className="buddy-text-button" disabled={loading || Boolean(savingStep)} onClick={onRetry}>Повторить загрузку</button></div>}
    {data.groupingAvailable ? <div className="roadmap-track-picker"><p>Треки адаптации</p><div className="roadmap-track-buttons" role="group" aria-label="Назначенные треки адаптации">{options.map(track=><button type="button" key={track.track} className={activeTrack?.track===track.track?'is-active':''} aria-pressed={activeTrack?.track===track.track} onClick={()=>setSelection(prev=>({...prev,track:track.track,step:''}))}>{track.display_name}</button>)}</div></div> : <p className="roadmap-flat-note">Общие шаги и шаги твоего направления показаны вместе. Текущий шаг указан отдельно для каждого трека.</p>}
    <section className="roadmap-progress" aria-label="Прогресс адаптации"><div><div><h3>{activeTrack?.display_name || 'План адаптации'}</h3><p>{done} из {steps.length} шагов выполнено</p><div className="roadmap-completion-note">{steps.length>0 && done===steps.length && <p className="roadmap-finished"><RoadmapIcon kind="check"/>Все шаги выполнены. Отличная работа!</p>}</div></div><strong>{percent}%</strong></div><div className="roadmap-progress-track" role="progressbar" aria-label="Выполнено шагов" aria-valuemin={0} aria-valuemax={steps.length || 1} aria-valuenow={done}><span style={{width:`${percent}%`}}/></div></section>
    <div className="roadmap-steps-heading"><h3>Шаги адаптации</h3></div>
    {steps.length? <div className={`roadmap-columns${selected?'':' roadmap-columns--list-only'}`}><div className="roadmap-step-list"><ol className="roadmap-timeline">{steps.map(renderStep)}</ol></div>
      {selected && <aside ref={detail} className="roadmap-detail" aria-label="Подробности шага">
        <span className={`roadmap-detail-status${selected.completed?' is-done':selected.current?' is-current':''}`}>{selected.completed?'Выполнено':selected.current?currentLabel(selected):'Предстоит'}</span><h3>{selected.title}</h3>{selected.period && <p className="roadmap-detail-period">{selected.period}</p>}<div className="roadmap-description">{selected.description || 'Подробного описания пока нет. Уточни порядок выполнения у бадди или спроси AI-buddy.'}</div>{selected.resource_id && <dl className="roadmap-resource"><dt>Ресурс</dt><dd>{selected.resource_id}</dd></dl>}<div className="roadmap-detail-actions"><button className="roadmap-ask" disabled={chatBusy} onClick={()=>onSelectStepQuestion(stepQuestion(selected))}><RoadmapIcon kind="buddy"/>Спросить у AI-buddy</button><p className="roadmap-check-hint">Отмечай выполнение галочкой слева от шага.</p></div>
      </aside>}
    </div>:<p className="roadmap-no-steps">В этом треке пока нет шагов.</p>}
  </section>;
}

// Owns loading and writes; OnboardingPage only renders the server snapshot.
export function RoadmapPage({userId,chatBusy,onSelectStepQuestion,refreshVersion=0}:{userId:string;chatBusy:boolean;onSelectStepQuestion:(question:string)=>void;refreshVersion?:number}) {
  const [data,setData]=useState<OnboardingRoadmap|null>(null);
  const [loading,setLoading]=useState(true);
  const [error,setError]=useState('');
  const [savingStep,setSavingStep]=useState('');
  const mounted=useRef(false);
  const revision=useRef({value:0});
  const saving=useRef(false);
  const refresh=useCallback(async(force=false)=>{
    if(saving.current && !force)return;
    const requestRevision=++revision.current.value;
    setLoading(true);
    try {
      const snapshot=await fetchOnboardingRoadmap(userId);
      if(mounted.current && requestRevision===revision.current.value){setData(snapshot);setError('');}
    } catch(reason) {
      if(mounted.current && requestRevision===revision.current.value)setError(errorText(reason));
    } finally {if(mounted.current && requestRevision===revision.current.value)setLoading(false);}
  },[userId]);
  useEffect(()=>{
    const requestRevision=revision.current;
    mounted.current=true;
    return ()=>{mounted.current=false;requestRevision.value++;};
  },[]);
  useEffect(()=>{void refresh();},[refresh,refreshVersion]);
  useEffect(()=>{
    const resume=()=>{if(document.visibilityState==='visible' && !saving.current)void refresh();};
    window.addEventListener('focus',resume);
    document.addEventListener('visibilitychange',resume);
    const poll=setInterval(resume,60000);
    return ()=>{clearInterval(poll);window.removeEventListener('focus',resume);document.removeEventListener('visibilitychange',resume);};
  },[refresh]);
  const toggle=async(step:OnboardingStep)=>{
    if(saving.current)return;
    saving.current=true;revision.current.value++;setLoading(false);setSavingStep(step.step_id);setError('');
    try {
      await toggleStep(userId,step);
      if(mounted.current)await refresh(true);
    } catch(reason) {if(mounted.current)setError(`Не удалось сохранить шаг: ${errorText(reason)}. Обнови план, чтобы проверить его состояние.`);}
    finally {saving.current=false;if(mounted.current)setSavingStep('');}
  };
  return <OnboardingPage userId={userId} data={data} loading={loading} error={error} savingStep={savingStep} chatBusy={chatBusy} onRetry={()=>void refresh()} onToggleStep={step=>void toggle(step)} onSelectStepQuestion={onSelectStepQuestion}/>;
}
