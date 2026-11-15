import React, { useEffect, useRef, useState } from 'react';
export function MessageCopyButton({text}:{text:string}) {
  const [state,setState]=useState('');
  const timer=useRef<ReturnType<typeof setTimeout>>();
  useEffect(()=>()=>clearTimeout(timer.current),[]);
  return <button type="button" className="message-copy" aria-label={state || 'Копировать сообщение'} title={state || 'Копировать сообщение'} onClick={async()=>{
    try {await navigator.clipboard.writeText(text);setState('Скопировано');}catch{setState('Не удалось скопировать');}
    clearTimeout(timer.current);timer.current=setTimeout(()=>setState(''),2200);
  }}>{state==='Скопировано'?<span aria-hidden="true">✓</span>:<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true"><rect x="8" y="3" width="13" height="14" rx="3"/><rect x="3" y="8" width="13" height="13" rx="3"/></svg>}<span className="message-copy-feedback" role="status">{state}</span></button>;
}
