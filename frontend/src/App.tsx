import React, { useEffect, useState } from 'react';
import { ChatPage } from './pages/ChatPage';
import { SetupPage } from './pages/SetupPage';
import { currentUser, refreshSession, login, request, ApiError } from './lib/api';
export default function App() {
  const [user, setUser] = useState('');
  const [profile,setProfile] = useState<'loading'|'missing'|'ready'|'error'>('loading');
  const [profileError,setProfileError] = useState('');
  const checkProfile = async (id:string) => {
    setProfile('loading');
    try { await request(`/v1/user/${encodeURIComponent(id)}/metadata`);setProfile('ready'); }
    catch(e) { if(e instanceof ApiError && e.status===404) setProfile('missing');
      else {setProfile('error');setProfileError(e instanceof Error?e.message:'Не удалось загрузить профиль');} }
  };
  useEffect(()=>{if(user)void checkProfile(user);},[user]);
  const [checking, setChecking] = useState(true);
  const [register,setRegister] = useState(false);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [dark, setDark] = useState(false);
  useEffect(() => { document.documentElement.dataset.theme = dark ? 'dark' : 'light'; }, [dark]);
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

    {checking ? <p role="status">Проверяем сессию…</p> : user ? (profile==='ready'?<ChatPage key={user} userId={user}/>:profile==='missing'?<SetupPage userId={user} onDone={()=>void checkProfile(user)}/>:profile==='error'?<div role="alert">{profileError}<button onClick={()=>void checkProfile(user)}>Повторить загрузку профиля</button></div>:<p role="status">Загружаем профиль…</p>) :
      <form className="login-card" onSubmit={async e => { e.preventDefault(); setBusy(true); setError('');
        try { if(register) await request('/v1/auth/register',{method:'POST',body:JSON.stringify({email,password})},false);setUser(await login(email,password)); } catch(e) { setError(e instanceof Error ? e.message : 'Не удалось войти'); }
        finally { setBusy(false); } }}>
        <h1>{register?'Регистрация':'Вход в AI-buddy'}</h1><p>Используй учётную запись, подготовленную командой.</p>
        <label>Почта<input type="email" autoComplete="username" value={email} onChange={e=>setEmail(e.target.value)} required/></label>
        <label>Пароль<input type="password" autoComplete="current-password" value={password} onChange={e=>setPassword(e.target.value)} required/></label>
        {error && <p role="alert">{error}</p>}<button disabled={busy}>{busy?'Подождите…':register?'Зарегистрироваться':'Войти'}</button><button type="button" onClick={()=>{setRegister(!register);setError('');}}>{register?'Уже есть аккаунт? Войти':'Создать аккаунт'}</button>
      </form>}
  </div>;
}
