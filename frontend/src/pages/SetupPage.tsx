import React, { useState } from 'react';
import { request } from '../lib/api';
export function SetupPage({userId,onDone}:{userId:string;onDone:()=>void}) {
  const [demoPeriod,setDemoPeriod]=useState(true);
  const start=new Date();const end=new Date(start);end.setMonth(end.getMonth()+6);
  const date=(d:Date)=>`${d.getFullYear()}-${String(d.getMonth()+1).padStart(2,'0')}-${String(d.getDate()).padStart(2,'0')}`;
  const [error,setError]=useState('');const [busy,setBusy]=useState(false);
  return <form className="login-card" style={{overflowY:'auto'}} onSubmit={async e=>{
    e.preventDefault();const fields=new FormData(e.currentTarget);setBusy(true);setError('');
    try {await request('/v1/user',{method:'POST',body:JSON.stringify({user_id:userId,
      first_name:fields.get('first_name'),last_name:fields.get('last_name'),team_id:fields.get('team_id'),
      track:fields.get('track'),start_date:demoPeriod?date(start):fields.get('start_date'),planned_end_date:demoPeriod?date(end):fields.get('end_date')})});onDone();}
    catch(e){setError(e instanceof Error?e.message:'Не удалось сохранить профиль');}finally{setBusy(false);}
  }}>
    <h1>Профиль стажировки</h1><p>Учётная запись создана. Для чата и плана адаптации заполни данные стажировки. Используй тестовые данные.</p>
    <label>Имя<input name="first_name" required/></label><label>Фамилия<input name="last_name" required/></label>
    <label>Команда<select name="team_id"><option value="900001">Backend</option><option value="900002">Frontend</option><option value="900003">Data Analytics</option><option value="900004">Platform</option><option value="900005">QA</option></select></label>
    <label>План адаптации<select name="track"><option value="backend">Backend</option><option value="qa">QA</option></select></label>
    <label><span><input type="checkbox" checked={demoPeriod} onChange={e=>setDemoPeriod(e.target.checked)}/> Использовать тестовый период: 6 месяцев с сегодняшнего дня</span></label>
    {!demoPeriod && <><label>Начало<input name="start_date" type="date" required/></label><label>Плановое окончание<input name="end_date" type="date" required/></label></>}
    {error&&<p role="alert">{error}</p>}<button disabled={busy}>{busy?'Сохраняем…':'Начать адаптацию'}</button>
  </form>;
}
