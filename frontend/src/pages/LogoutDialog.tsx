import React, { useEffect, useRef, useState } from 'react';
import { friendlyError } from '../lib/api';
export function LogoutDialog({onClose,onLogout}:{onClose:()=>void;onLogout:()=>Promise<void>}) {
  const ref=useRef<HTMLDialogElement>(null);
  const [busy,setBusy]=useState(false),[error,setError]=useState('');
  useEffect(()=>{ref.current?.showModal();},[]);
  return <dialog ref={ref} className="requests-dialog logout-dialog" aria-labelledby="logout-title" onCancel={e=>{e.preventDefault();if(!busy)onClose();}}><div className="requests-body"><h2 id="logout-title">Выйти из аккаунта?</h2><p>Переписки и прогресс адаптации сохранятся</p>{error && <p role="alert">{error}</p>}</div><footer className="requests-footer"><button className="buddy-action-button" autoFocus disabled={busy} onClick={onClose}>Остаться</button><button className="buddy-action-button logout-confirm" disabled={busy} onClick={async()=>{setBusy(true);setError('');try{await onLogout();}catch(e){setError(friendlyError(e));setBusy(false);}}}>{busy?'Выходим…':'Выйти'}</button></footer></dialog>;
}
