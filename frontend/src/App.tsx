import React, { useEffect, useState } from 'react';
import { ChatPage } from './pages/ChatPage';
import { currentUser, refreshSession, login } from './lib/api';
export default function App() {
  const [user, setUser] = useState('');
  const [checking, setChecking] = useState(true);
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
    <header className="app-header"><div className="aibuddy-logo-pill">AI BUDDY</div>
      <button className="suggestion-pill" onClick={() => setDark(!dark)}>Сменить тему</button></header>
    {checking ? <p role="status">Проверяем сессию…</p> : user ? <ChatPage userId={user}/> :
      <form className="login-card" onSubmit={async e => { e.preventDefault(); setBusy(true); setError('');
        try { setUser(await login(email,password)); } catch(e) { setError(e instanceof Error ? e.message : 'Не удалось войти'); }
        finally { setBusy(false); } }}>
        <h1>Вход в AI-buddy</h1><p>Используй учётную запись, подготовленную командой.</p>
        <label>Почта<input type="email" autoComplete="username" value={email} onChange={e=>setEmail(e.target.value)} required/></label>
        <label>Пароль<input type="password" autoComplete="current-password" value={password} onChange={e=>setPassword(e.target.value)} required/></label>
        {error && <p role="alert">{error}</p>}<button disabled={busy}>{busy?'Входим…':'Войти'}</button>
      </form>}
  </div>;
}
