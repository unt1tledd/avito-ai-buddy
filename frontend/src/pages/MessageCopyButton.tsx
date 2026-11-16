import React, { useEffect, useId, useRef, useState } from 'react';
export function MessageCopyButton({text}:{text:string}) {
  const [state,setState]=useState('');
  const tooltip=useId();
  const timer=useRef<ReturnType<typeof setTimeout>>();
  useEffect(()=>()=>clearTimeout(timer.current),[]);
  return <button type="button" className={`message-copy${state?' has-feedback':''}`} aria-label="Копировать сообщение" aria-describedby={tooltip} onClick={async()=>{
    try {await navigator.clipboard.writeText(text);setState('Скопировано');}catch{setState('Не удалось скопировать');}
    clearTimeout(timer.current);timer.current=setTimeout(()=>setState(''),2200);
  }}>{state==='Скопировано'?<span aria-hidden="true">✓</span>:<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true"><path d="M8 8V6a3 3 0 0 1 3-3h7a3 3 0 0 1 3 3v8a3 3 0 0 1-3 3h-2"/><rect x="3" y="8" width="13" height="13" rx="3"/></svg>}<span id={tooltip} className="message-copy-tooltip" role="tooltip">{state || 'Копировать'}</span><span className="message-copy-status" role="status">{state}</span></button>;
}
