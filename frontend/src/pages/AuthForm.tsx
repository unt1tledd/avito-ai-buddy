import React, { useId, useState } from 'react';

export function registrationError(password:string, confirmation:string):string {
  if(password !== confirmation)return 'Пароли не совпадают.';
  if([...password].length < 8)return 'Пароль должен содержать не менее 8 символов.';
  if(new TextEncoder().encode(password).length > 72)return 'Пароль слишком длинный: максимум 72 байта. Кириллица и эмодзи занимают больше одного байта.';
  return '';
}

function PasswordField({label, value, onChange, registration, invalid=false}:{
  label:string;value:string;onChange:(value:string)=>void;registration:boolean;invalid?:boolean;
}) {
  const id=useId();const [visible,setVisible]=useState(false);
  return <div className="auth-password-field"><label htmlFor={id}>{label}</label>
    <div className="auth-password-input"><input id={id} type={visible?'text':'password'}
      autoComplete={registration?'new-password':'current-password'} value={value}
      onChange={e=>onChange(e.target.value)} aria-invalid={invalid || undefined} required/>
      <button type="button" className="password-visibility" aria-label={`${visible?'Скрыть':'Показать'}: ${label.toLowerCase()}`}
        aria-pressed={visible} onClick={()=>setVisible(v=>!v)}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" aria-hidden="true">
          <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Z"/><circle cx="12" cy="12" r="3"/>
          {visible && <path d="m3 3 18 18"/>}
        </svg>
      </button>
    </div>
  </div>;
}

export function AuthForm({onAuthenticate}:{onAuthenticate:(email:string,password:string,register:boolean)=>Promise<void>}) {
  const [register,setRegister]=useState(false);
  const [email,setEmail]=useState('');const [password,setPassword]=useState('');
  const [confirmation,setConfirmation]=useState('');const [busy,setBusy]=useState(false);
  const [error,setError]=useState('');const [mismatch,setMismatch]=useState(false);
  return <form className="login-card" onSubmit={async e=>{
    e.preventDefault();if(busy)return;setError('');setMismatch(false);
    if(register){const validation=registrationError(password,confirmation);if(validation){setError(validation);setMismatch(password!==confirmation);return;}}
    setBusy(true);
    try{await onAuthenticate(email.trim(),password,register);setPassword('');setConfirmation('');}
    catch(e){setError(e instanceof Error?e.message:'Не удалось войти');}
    finally{setBusy(false);}
  }}>
    <h1>{register?'Регистрация':'Вход в AI-buddy'}</h1>
    {register && <p>Создай аккаунт, чтобы начать адаптацию.</p>}
    <fieldset disabled={busy} className="auth-fields">
      <label>Почта<input type="email" autoComplete="username" value={email} onChange={e=>setEmail(e.target.value)} required/></label>
      <PasswordField key={register?'new':'current'} label="Пароль" value={password} registration={register} onChange={value=>{setPassword(value);setMismatch(false);}}/>
      {register && <><p className="auth-hint">Не менее 8 символов.</p><PasswordField label="Подтверждение пароля" value={confirmation} registration onChange={value=>{setConfirmation(value);setMismatch(false);}} invalid={mismatch}/></>}
    </fieldset>
    {error && <p role="alert">{error}</p>}
    <button disabled={busy} type="submit">{busy?'Подождите…':register?'Зарегистрироваться':'Войти'}</button>
    <button disabled={busy} type="button" onClick={()=>{setRegister(v=>!v);setPassword('');setConfirmation('');setError('');setMismatch(false);}}>{register?'Уже есть аккаунт? Войти':'Создать аккаунт'}</button>
  </form>;
}
