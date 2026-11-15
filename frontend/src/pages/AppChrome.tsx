import React from 'react';

export function AppBrand() {
  return <div className="app-brand" aria-label="Avito — AI Buddy">
    <div className="app-brand-wordmark" aria-hidden="true">
      <svg viewBox="0 0 40 40" className="app-brand-dots"><circle cx="10" cy="29" r="9" fill="#04E061"/><circle cx="28" cy="12" r="11" fill="#00AAFF"/><circle cx="9" cy="9" r="6" fill="#965EEB"/><circle cx="28" cy="31" r="6" fill="#FF4053"/></svg>
      <span>Avito</span>
    </div>
    <span className="app-brand-caption">AI Buddy</span>
  </div>;
}

export function ThemeToggle({dark,onToggle,className=''}:{dark:boolean;onToggle:()=>void;className?:string}) {
  return <button type="button" className={`buddy-icon-button buddy-theme-toggle ${className}`} onClick={onToggle}
    aria-label={dark?'Переключить на светлую тему':'Переключить на тёмную тему'} title={dark?'Светлая тема':'Тёмная тема'}>
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      {dark ? <><circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5"/></> : <path d="M20.5 14a9 9 0 0 1-10.5-10.5A9 9 0 1 0 20.5 14Z"/>}
    </svg>
  </button>;
}
