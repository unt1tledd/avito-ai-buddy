import React, { useEffect, useState } from 'react';
import { ChatPage } from './pages/ChatPage';
import { AuthForm } from './pages/AuthForm';
import { SetupPage } from './pages/SetupPage';
import { currentUser, refreshSession, login, logout, request, ApiError } from './lib/api';
export default function App() {
  const [user, setUser] = useState('');
  const [metadata,setMetadata] = useState<any>(null);
  const [profile,setProfile] = useState<'loading'|'missing'|'ready'|'error'>('loading');
  const [profileError,setProfileError] = useState('');
  const checkProfile = async (id:string) => {
    setProfile('loading');
    try { setMetadata(await request(`/v1/user/${encodeURIComponent(id)}/metadata`));setProfile('ready'); }
    catch(e) { if(e instanceof ApiError && e.status===404) setProfile('missing');
      else {setProfile('error');setProfileError(e instanceof Error?e.message:'Не удалось загрузить профиль');} }
  };
  useEffect(()=>{if(user)void checkProfile(user);},[user]);
  const [checking, setChecking] = useState(true);
  const [dark, setDark] = useState(() => document.documentElement.dataset.theme === 'dark');
  useEffect(() => {
    const theme = dark ? 'dark' : 'light';
    document.documentElement.dataset.theme = theme;
    try { localStorage.setItem('buddy-theme', theme); } catch { /* Theme switching also works without storage. */ }
  }, [dark]);
  useEffect(() => { let active = true;
    refreshSession().then(currentUser).then(id => { if(active) setUser(id); })
      .catch(() => {}).finally(() => { if(active) setChecking(false); });
    return () => { active = false; };
  }, []);
  return <div className="app-root">
      <header className="app-header">
        <div className="aibuddy-logo-pill">
          <span className="aibuddy-logo-dots" aria-hidden="true">
            {['#04E061', '#00AAFF', '#965EEB', '#FF4053'].map((color) => (
              <span
                key={color}
                className="aibuddy-logo-dot"
                style={{ background: color }}
              />
            ))}
          </span>
          <span>AI BUDDY</span>
        </div>

        <button
          type="button"
          className={`theme-toggle-btn${dark ? ' theme-toggle-btn--dark' : ''}`}
          onClick={() => setDark(prev=>!prev)}
          title={dark ? 'Переключить на светлую тему' : 'Переключить на тёмную тему'}
          aria-label="Переключить тему оформления"
        >
          <span className="theme-toggle-thumb">
            {dark ? (
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <circle cx="12" cy="12" r="4.2" />
                <path d="M12 2.5v2.3M12 19.2v2.3M4.9 4.9l1.6 1.6M17.5 17.5l1.6 1.6M2.5 12h2.3M19.2 12h2.3M4.9 19.1l1.6-1.6M17.5 6.5l1.6-1.6" />
              </svg>
            ) : (
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path d="M20.2 14.8A8.5 8.5 0 0 1 9.2 3.8a8.5 8.5 0 1 0 11 11Z" />
              </svg>
            )}
          </span>
        </button>
      </header>

    {checking ? <p role="status">Проверяем сессию…</p> : user ? (profile==='ready'?<ChatPage key={user} userId={user} onLogout={()=>{logout();setUser('');setMetadata(null);setProfile('loading');setProfileError('');}} profile={{name:[metadata?.intern?.first_name,metadata?.intern?.last_name].filter(Boolean).join(' '),email:localStorage.getItem(`buddy-email-${user}`)||'',team:metadata?.team?.name,track:metadata?.track}}/>:profile==='missing'?<SetupPage userId={user} onDone={()=>void checkProfile(user)}/>:profile==='error'?<div role="alert">{profileError}<button onClick={()=>void checkProfile(user)}>Повторить загрузку профиля</button></div>:<p role="status">Загружаем профиль…</p>) :
      <AuthForm onAuthenticate={async(email,password,register)=>{
        if(register)await request('/v1/auth/register',{method:'POST',body:JSON.stringify({email,password})},false);
        const id=await login(email,password);
        try{localStorage.setItem(`buddy-email-${id}`,email);}catch{/* Login also works without browser storage. */}
        setUser(id);
      }}/>}

  </div>;
}
