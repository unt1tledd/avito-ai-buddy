import React, { useState } from 'react';
import { request } from '../lib/api';
export function SetupPage({userId,onDone}:{userId:string;onDone:()=>void}) {
  const teams = [{id:'900001',name:'Backend',track:'backend'}, {id:'900002',name:'Frontend',track:'frontend'}, {id:'900003',name:'Data Analytics',track:'data_analytics'}, {id:'900004',name:'Platform',track:'platform'}, {id:'900005',name:'QA',track:'qa'}];
  const [error,setError]=useState('');const [busy,setBusy]=useState(false);
  return <form className="login-card" style={{overflowY:'auto'}} onSubmit={async e=>{
    e.preventDefault();const fields=new FormData(e.currentTarget);setError('');
    if(String(fields.get('end_date')) < String(fields.get('start_date'))) { setError('Дата окончания должна быть не раньше первого дня стажировки.');return; }
    setBusy(true);
    try {await request('/v1/user',{method:'POST',body:JSON.stringify({user_id:userId,
      first_name:fields.get('first_name'),last_name:fields.get('last_name'),team_id:fields.get('team_id'),
      track:teams.find(team=>team.id===fields.get('team_id'))!.track,start_date:fields.get('start_date'),planned_end_date:fields.get('end_date')})});onDone();}
    catch(e){setError(e instanceof Error?e.message:'Не удалось сохранить профиль');}finally{setBusy(false);}
  }}>
    <h1>Профиль стажировки</h1><p>Расскажи немного о себе. Треки адаптации назначит бадди.</p>
    <label>Имя<input name="first_name" required/></label><label>Фамилия<input name="last_name" required/></label>
    <label>Команда<span className="setup-team-select"><select name="team_id">{teams.map(team=><option key={team.id} value={team.id}>{team.name}</option>)}</select><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="m6 9 6 6 6-6"/></svg></span></label>
    <div className="profile-date-fields">
      <label>Первый день стажировки<input name="start_date" type="date" required/></label>
      <label>Плановая дата окончания<input name="end_date" type="date" required/></label>
    </div>
    {error&&<p role="alert">{error}</p>}<button disabled={busy}>{busy?'Сохраняем…':'Сохранить профиль'}</button>
  </form>;
}
