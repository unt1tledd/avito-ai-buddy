import React, { useEffect, useState } from 'react';
import { ChatPage } from './pages/ChatPage';
import { AuthForm } from './pages/AuthForm';
import { SetupPage } from './pages/SetupPage';
import { AppBrand, ThemeToggle } from './pages/AppChrome';
import { currentUser, refreshSession, login, logout, request, ApiError, UserMetadata } from './lib/api';
export default function App() {
  const [user, setUser] = useState('');
  const [metadata,setMetadata] = useState<UserMetadata|null>(null);
  const [profile,setProfile] = useState<'loading'|'missing'|'ready'|'error'>('loading');
  const [profileError,setProfileError] = useState('');
  const checkProfile = async (id:string) => {
    setProfile('loading');
    try { setMetadata(await request<UserMetadata>(`/v1/user/${encodeURIComponent(id)}/metadata`));setProfile('ready'); }
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
  // Wait for session and profile together; never mount the sign-in header
  // between these requests during a reload.
  if(checking || (user && profile==='loading'))return <div className="app-root app-restoring" role="status" aria-label="Восстанавливаем приложение"><span className="app-restoring-indicator" aria-hidden="true"/></div>;
  return <div className="app-root">
      {!(user && profile === 'ready') && <header className="app-header">
        <AppBrand/>
        <ThemeToggle dark={dark} onToggle={()=>setDark(prev=>!prev)} className="auth-theme-toggle"/>
      </header>}

    {checking ? <p role="status">Проверяем сессию…</p> : user ? (profile==='ready'?<ChatPage key={user} userId={user} dark={dark} onToggleTheme={()=>setDark(prev=>!prev)} onLogout={async()=>{await logout();setUser('');setMetadata(null);setProfile('loading');setProfileError('');}} profile={{name:[metadata?.intern?.first_name,metadata?.intern?.last_name].filter(Boolean).join(' '),email:localStorage.getItem(`buddy-email-${user}`)||'',team:metadata?.team?.name,track:metadata?.track}}/>:profile==='missing'?<SetupPage userId={user} onDone={()=>void checkProfile(user)}/>:profile==='error'?<div role="alert">{profileError}<button onClick={()=>void checkProfile(user)}>Повторить загрузку профиля</button></div>:<p role="status">Загружаем профиль…</p>) :
      <AuthForm onAuthenticate={async(email,password,register)=>{
        if(register)await request('/v1/auth/register',{method:'POST',body:JSON.stringify({email,password})},false);
        const id=await login(email,password);
        try{localStorage.setItem(`buddy-email-${id}`,email);}catch{/* Login also works without browser storage. */}
        setUser(id);
      }}/>}

  </div>;
}
