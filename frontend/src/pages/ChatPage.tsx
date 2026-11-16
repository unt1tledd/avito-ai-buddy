import React, { useCallback, useEffect, useRef, useState } from 'react';
import { ChatMessage, fetchHistory, sendChatMessage, confirmAccessProposal, refreshSession, Conversation, listConversations, createConversation, deleteConversation, recentConversations, friendlyError } from '../lib/api';
import { NavigationIcon } from './NavigationIcon';
import { MessageCopyButton } from './MessageCopyButton';
import { LogoutDialog } from './LogoutDialog';
import { AccessProposalCard } from './AccessProposalCard';
import { MessageText, MessageSources } from './MessageContent';
import { AppBrand, ThemeToggle } from './AppChrome';
import { HelpContent } from './HelpContent';
import { RequestsPage } from './RequestsPage';
import { HomePage } from './HomePage';
import { RoadmapPage } from './OnboardingPage';
import { historyRefreshQueue, shouldRefreshHistory, mergeChatMessages, reconcileProposal } from '../lib/chatEvents';
import { ProfileDialog, readMenuOpen, saveMenuOpen, readProfileDialog, saveProfileDialog } from '../lib/uiPreferences';
type Screen = 'home' | 'roadmap' | 'requests' | 'chat' | 'profile';
type IconName = Screen | 'panel' | 'chevron' | 'plus' | 'help' | 'logout' | 'close' | 'sun' | 'moon' | 'trash';
function Icon({name}: {name: IconName}) {
  if(name==='home' || name==='roadmap' || name==='requests' || name==='chat')return <NavigationIcon name={name}/>;
  const paths: Record<Exclude<IconName,'home'|'roadmap'|'requests'|'chat'>, React.ReactNode> = {
    profile: <><circle cx="12" cy="8" r="4"/><path d="M4 21v-2a8 8 0 0 1 16 0v2"/></>,
    panel: <><rect x="3" y="4" width="18" height="16" rx="3"/><path d="M9 4v16"/></>,
    chevron: <path d="m9 5 7 7-7 7"/>,
    plus: <path d="M12 5v14M5 12h14"/>,
    help: <><circle cx="12" cy="12" r="9"/><path d="M9.5 9a2.5 2.5 0 1 1 4 2c-1 .7-1.5 1-1.5 2.5M12 17h.01"/></>,
    logout: <><path d="M9 4H4v16h5M10 12h11m-4-4 4 4-4 4"/></>,
    close: <path d="m6 6 12 12M18 6 6 18"/>,
    sun: <><circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5"/></>,
    moon: <path d="M20.5 14a9 9 0 0 1-10.5-10.5A9 9 0 1 0 20.5 14Z"/>,
    trash: <><path d="M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7M14 10v7"/></>,
  };
  return <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{paths[name]}</svg>;
}
const screenTitles: Record<Screen, string> = {home:'Главная', roadmap:'Roadmap', requests:'Заявки', chat:'Чат AI-buddy', profile:'Профиль'};

const errorText = friendlyError;
export function ChatPage({userId,profile,onLogout,dark,onToggleTheme}:{userId:string;onLogout:()=>Promise<void>;dark:boolean;onToggleTheme:()=>void;profile:{name:string;email:string;team?:string;track?:string}}) {
  const [profileDialog,setProfileDialog]=useState<ProfileDialog>(()=>readProfileDialog(userId));
  const changeProfileDialog=(dialog:ProfileDialog)=>{saveProfileDialog(userId,dialog);setProfileDialog(dialog);};
  const [messages,setMessages] = useState<ChatMessage[]>([]);
  const [input,setInput] = useState('');
  const [error,setError] = useState('');
  const [busy,setBusy] = useState(false);
  const sending = useRef(false);
  const [proposalErrors,setProposalErrors] = useState<Record<string,string>>({});
  const [pending,setPending] = useState<string[]>([]);
  const locks = useRef(new Set<string>());
  const [screen,setScreen] = useState<Screen>(()=>{
    try {
      const saved=localStorage.getItem(`buddy-screen-${userId}`);
      if(saved && Object.prototype.hasOwnProperty.call(screenTitles,saved))return saved as Screen;
    } catch { /* Navigation also works without storage. */ }
    return 'home';
  });
  const [roadmapVersion,setRoadmapVersion]=useState(0);
  const [menuOpen,setMenuOpen] = useState(()=>readMenuOpen(userId,!window.matchMedia('(max-width: 760px)').matches));
  useEffect(()=>{saveMenuOpen(userId,menuOpen);},[userId,menuOpen]);
  const toggleMenu=()=>setMenuOpen(open=>!open);
  const [historyOpen,setHistoryOpen] = useState(()=>{
    try { return localStorage.getItem(`buddy-history-open-${userId}`)==='true'; } catch { return false; }
  });
  useEffect(()=>{
    try { localStorage.setItem(`buddy-screen-${userId}`,screen); } catch { /* Storage is optional. */ }
  },[screen,userId]);
  useEffect(()=>{
    try { localStorage.setItem(`buddy-history-open-${userId}`,String(historyOpen)); } catch { /* Storage is optional. */ }
  },[historyOpen,userId]);
  const workspace=useRef<HTMLDivElement>(null);
  const drag=useRef<{pointer:number;startX:number;startWidth:number;width:number}|null>(null);
  const resizeFrame=useRef<number|null>(null);
  const [dragging,setDragging]=useState(false);
  const [maxMenuWidth,setMaxMenuWidth]=useState(()=>Math.max(240,Math.min(720,window.innerWidth-360)));
  const [menuWidth,setMenuWidth]=useState(()=>{
    try {
      const saved=Number(localStorage.getItem('buddy-sidebar-width'));
      return Number.isFinite(saved) && saved>=240 ? Math.min(saved,720) : 280;
    } catch { return 280; }
  });
  const displayedMenuWidth=Math.min(menuWidth,maxMenuWidth);
  useEffect(()=>{
    const element=workspace.current;
    if(!element)return;
    const observer=new ResizeObserver(()=>setMaxMenuWidth(Math.max(240,Math.min(720,element.clientWidth-360))));
    observer.observe(element);
    return ()=>observer.disconnect();
  },[]);
  useEffect(()=>{
    const timer=setTimeout(()=>{
      try { localStorage.setItem('buddy-sidebar-width',String(menuWidth)); } catch { /* Storage is optional. */ }
    },150);
    return ()=>clearTimeout(timer);
  },[menuWidth]);
  useEffect(()=>{
    if(!dragging)return;
    document.body.classList.add('sidebar-resizing');
    return ()=>document.body.classList.remove('sidebar-resizing');
  },[dragging]);
  useEffect(()=>{drag.current=null;setDragging(false);},[menuOpen]);
  useEffect(()=>()=>{if(resizeFrame.current!==null)cancelAnimationFrame(resizeFrame.current);},[]);
  const finishResize=()=>{
    if(resizeFrame.current!==null){cancelAnimationFrame(resizeFrame.current);resizeFrame.current=null;}
    if(drag.current){const width=drag.current.width;workspace.current?.style.setProperty('--menu-width',`${width}px`);setMenuWidth(width);}
    drag.current=null;setDragging(false);
  };
  const helpDialog = useRef<HTMLDialogElement>(null);
  useEffect(()=>{
    const dialog=helpDialog.current;
    if(profileDialog==='help'){if(dialog && !dialog.open)dialog.showModal();}
    else if(dialog?.open)dialog.close();
  },[profileDialog]);
  const initials = profile.name.split(/\s+/).filter(Boolean).slice(0,2).map(part=>part[0]).join('') || 'Я';
  const navigate=(nextScreen:Screen)=>{
    setScreen(nextScreen);
    if(window.matchMedia('(max-width: 760px)').matches)setMenuOpen(false);
  };
  useEffect(()=>{
    const onEscape=(event:KeyboardEvent)=>{
      if(event.key==='Escape' && window.matchMedia('(max-width: 760px)').matches)setMenuOpen(false);
    };
    window.addEventListener('keydown',onEscape);
    return ()=>window.removeEventListener('keydown',onEscape);
  },[]);
  const [historyLimit,setHistoryLimit]=useState(10);
  const [suggestionsDismissed,setSuggestionsDismissed]=useState(false);
  const main = useRef<HTMLElement>(null);
  const composer = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const element = composer.current;
    if (!element) return;
    const observer = new ResizeObserver(() => {
      main.current?.style.setProperty('--composer-height', `${element.offsetHeight}px`);
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  const textarea=useRef<HTMLTextAreaElement>(null);
  useEffect(()=>{const el=textarea.current;if(!el)return;el.style.height='0px';el.style.height=`${Math.min(Math.max(el.scrollHeight,28),144)}px`;},[input]);
  const [chats,setChats] = useState<Conversation[]>([]);
  const messagesRef=useRef(messages);
  messagesRef.current=messages;
  const paginated=useRef(false);
  const historyVersion=useRef(0);
  const [active,setActive] = useState('');
  const activeRef = useRef('');
  const [restoring,setRestoring] = useState(true);
  const activateChat=useCallback((id:string)=>{
    if(activeRef.current!==id)historyVersion.current++;
    activeRef.current=id;
    setActive(id);
    try {
      if(id)localStorage.setItem(`buddy-active-chat-${userId}`,id);
      else localStorage.removeItem(`buddy-active-chat-${userId}`);
    } catch { /* Chat navigation also works without browser storage. */ }
  },[userId]);
  const loadChats=useCallback(async()=>{try{setChats(await listConversations());}catch(e){setError(errorText(e));}},[]);
  const [next,setNext] = useState('');
  const [loading,setLoading] = useState(true);
  const end = useRef<HTMLDivElement>(null);
  const loadHistory = useCallback(async (older=false, page='') => {
    const id=activeRef.current;
    const version=historyVersion.current;
    if(!id){setLoading(false);return;}
    try {
      const result=await fetchHistory(page,id);
      if(activeRef.current!==id)return;
      const refreshed=[...result.messages];
      if(!older) {
        // A proposal can be days older than the newest 50 messages. Re-read
        // pages up to every already displayed nonterminal proposal as well.
        const awaiting=new Set(messagesRef.current.filter(m=>m.proposal && !['approved','rejected'].includes(m.proposal.status)).map(m=>m.id));
        let cursor=result.next;
        const seen=new Set<string>();
        result.messages.forEach(m=>awaiting.delete(m.id));
        while(awaiting.size && cursor && !seen.has(cursor)) {
          seen.add(cursor);
          const previous=await fetchHistory(cursor,id);
          if(activeRef.current!==id)return;
          refreshed.push(...previous.messages);
          previous.messages.forEach(m=>awaiting.delete(m.id));
          cursor=previous.next;
        }
      }
      if(activeRef.current!==id)return;
      if(version!==historyVersion.current)return;
      setMessages(prev=>mergeChatMessages(prev,refreshed));
      const newest=result.messages.reduce((latest,m)=>m.timestamp>latest?m.timestamp:latest,'');
      if(!older && newest)setChats(prev=>recentConversations(prev.map(chat=>chat.id===id && Date.parse(newest)>Date.parse(chat.lastActivityAt || chat.createdAt)?{...chat,lastActivityAt:newest}:chat)));
      if(older){paginated.current=true;setNext(result.next);}
      else if(!paginated.current)setNext(result.next);
      setError('');
    } catch(e) {if(activeRef.current===id)setError(errorText(e));}
    finally {if(activeRef.current===id)setLoading(false);}
  },[]);
  useEffect(()=>{
    let stopped=false;
    const restore=async()=>{
      try {
        const conversations=await listConversations();
        if(stopped)return;
        setChats(conversations);
        let saved='';
        try { saved=localStorage.getItem(`buddy-active-chat-${userId}`)||''; } catch { /* Storage is optional. */ }
        if(saved && conversations.some(chat=>String(chat.id)===saved)) {
          activateChat(saved);
          setSuggestionsDismissed(true);
          await loadHistory();
        } else activateChat('');
      } catch(e) { if(!stopped)setError(errorText(e)); }
      finally { if(!stopped){setRestoring(false);setLoading(false);} }
    };
    void restore();
    return ()=>{stopped=true;};
  },[userId,activateChat,loadHistory]);
  useEffect(()=>{end.current?.scrollIntoView({behavior:'smooth'});},[messages.length,busy]);
  useEffect(()=>{
    let stopped=false; let socket:WebSocket|undefined; let timer:ReturnType<typeof setTimeout>; let attempts=0;
    const updates=historyRefreshQueue(()=>loadHistory());
    const chatUpdates=historyRefreshQueue(()=>loadChats(),400);
    const roadmapUpdates=historyRefreshQueue(async()=>{setRoadmapVersion(version=>version+1);},250);
    const resume=()=>{if(document.visibilityState==='visible'){updates.schedule();chatUpdates.schedule();}};
    const poll=setInterval(()=>{
      if(document.visibilityState==='visible' && messagesRef.current.some(m=>m.proposal && ['pending_dispatch','pending','pending_approval'].includes(m.proposal.status)))updates.schedule();
    },60000);
    window.addEventListener('focus',resume);
    document.addEventListener('visibilitychange',resume);
    const connect=()=>{
      if(stopped) return;
      socket=new WebSocket(`${location.protocol==='https:'?'wss:':'ws:'}//${location.host}/v1/chat/events`);
      socket.onopen=()=>{if(stopped)return;attempts=0;updates.schedule();roadmapUpdates.schedule();};
      socket.onmessage=event=>{
        if(stopped)return;
        if(shouldRefreshHistory(event.data,activeRef.current))updates.schedule();
        try {const type=JSON.parse(event.data)?.type;if(type==='message.created')chatUpdates.schedule();if(['message.created','task.status_changed','access_request.failed'].includes(type))roadmapUpdates.schedule();} catch { /* Ignore malformed events. */ }
      };
      socket.onclose=()=>{if(stopped)return;
        timer=setTimeout(()=>{refreshSession().then(connect).catch(()=>{if(!stopped){timer=setTimeout(connect,30000);}});},Math.min(1000*2**attempts++,30000));};
    }; connect();
    return ()=>{stopped=true;updates.stop();chatUpdates.stop();roadmapUpdates.stop();clearTimeout(timer);clearInterval(poll);window.removeEventListener('focus',resume);document.removeEventListener('visibilitychange',resume);socket?.close();};
  },[loadHistory,loadChats]);
  const send=async(text=input, key=String(Date.now()), failedId?:string)=>{
    text=text.trim(); if(!text || sending.current || restoring)return;
    setSuggestionsDismissed(true);sending.current=true;setBusy(true);setError('');
    let chatID=activeRef.current;
    if(!chatID){try{const chat=await createConversation();chatID=chat.id;activateChat(chatID);setMessages([]);setChats(prev=>recentConversations([chat,...prev.filter(item=>item.id!==chat.id)]));}catch(e){setError(errorText(e));sending.current=false;setBusy(false);return;}}
    if(!failedId){setInput('');setMessages(prev=>[...prev,{id:`local-${key}`,sender:'user',text,timestamp:new Date().toISOString()}]);}
    try {const reply=await sendChatMessage(text,key,chatID);
      setMessages(prev=>mergeChatMessages(prev.filter(m=>m.id!==failedId),[reply]));
      await loadHistory();void loadChats();
    } catch(e){
      void loadChats();
      setMessages(prev=>[...prev.filter(m=>m.id!==failedId),{id:`error-${key}`,sender:'assistant',text:errorText(e),timestamp:new Date().toISOString(),error:true,idempotency_key:key,retry_text:text}]);
      await loadHistory();
    } finally {sending.current=false;setBusy(false);}
  };
  const confirm=async(id:string)=>{
    if(locks.current.has(id))return;locks.current.add(id);setPending(p=>[...p,id]);setProposalErrors(prev=>({...prev,[id]:''}));
    try {const result=await confirmAccessProposal(id);
      setMessages(prev=>prev.map(m=>m.proposal?.proposal_id===id?{...m,proposal:reconcileProposal(m.proposal,{...m.proposal,status:result.status,tracker_issue_key:result.tracker_issue_key})}:m));

    }catch(e){setProposalErrors(prev=>({...prev,[id]:errorText(e)}));}finally{locks.current.delete(id);setPending(p=>p.filter(x=>x!==id));void loadHistory();}
  };
  const visibleMessages = messages;
  const selectChat=async(id:string)=>{if(sending.current || restoring)return;activateChat(id);paginated.current=false;setMessages([]);setNext('');setError('');setLoading(true);setSuggestionsDismissed(true);setInput('');navigate('chat');await loadHistory();};
  const newChat=()=>{if(sending.current || restoring)return;activateChat('');paginated.current=false;setMessages([]);setNext('');setError('');setInput('');setSuggestionsDismissed(false);setLoading(false);navigate('chat');};
  const removeChat=async(id?:string)=>{if(sending.current)return;
    try{await deleteConversation(id);if(!id||id===activeRef.current)newChat();await loadChats();}catch(e){setError(errorText(e));}
  };
  return <div ref={workspace} className={`buddy-workspace${menuOpen?' buddy-workspace--menu-open':''}${dragging?' buddy-workspace--resizing':''}`} style={{'--menu-width':`${displayedMenuWidth}px`} as React.CSSProperties}>
    <aside id="workspace-menu" className="buddy-sidebar" aria-label="Основное меню">
      <div className="buddy-brand"><AppBrand/><button className="buddy-icon-button buddy-sidebar-toggle" aria-label={menuOpen?'Свернуть меню':'Открыть меню'} title={menuOpen?'Свернуть меню':'Открыть меню'} aria-expanded={menuOpen} aria-controls="workspace-menu" onClick={toggleMenu}><Icon name="panel"/></button></div>
      <nav className="buddy-navigation">
        <button className={`buddy-nav-item${screen==='home'?' is-active':''}`} aria-label="Главная" title="Главная" aria-current={screen==='home'?'page':undefined} onDoubleClick={()=>setMenuOpen(true)} onClick={()=>navigate('home')}><Icon name="home"/><span>Главная</span></button>
        <button className={`buddy-nav-item${screen==='roadmap'?' is-active':''}`} aria-label="Roadmap" title="Roadmap" aria-current={screen==='roadmap'?'page':undefined} onDoubleClick={()=>setMenuOpen(true)} onClick={()=>navigate('roadmap')}><Icon name="roadmap"/><span>Roadmap</span></button>
        <button className={`buddy-nav-item${screen==='requests'?' is-active':''}`} aria-label="Заявки" title="Заявки" aria-current={screen==='requests'?'page':undefined} onDoubleClick={()=>setMenuOpen(true)} onClick={()=>navigate('requests')}><Icon name="requests"/><span>Заявки</span></button>
        <div className={`buddy-chat-nav${screen==='chat'?' is-active':''}`}>
          <button className="buddy-nav-item" aria-label="Чат AI-buddy" title="Чат AI-buddy" onDoubleClick={()=>setMenuOpen(true)} aria-current={screen==='chat'?'page':undefined} disabled={busy || restoring} onClick={()=>{navigate('chat');setHistoryOpen(true);if(!activeRef.current && chats[0])void selectChat(chats[0].id);}}><Icon name="chat"/><span>Чат AI-buddy</span></button>
          <button className="buddy-icon-button buddy-history-toggle" tabIndex={menuOpen?0:-1} aria-hidden={!menuOpen} aria-label={historyOpen?'Свернуть историю чатов':'Развернуть историю чатов'} aria-expanded={historyOpen && menuOpen} aria-controls="buddy-chat-history" onClick={()=>setHistoryOpen(open=>!open)}><span className={`buddy-chevron${historyOpen?' is-open':''}`}><Icon name="chevron"/></span></button>
        </div>
        <section id="buddy-chat-history" className={`buddy-chat-history${historyOpen && menuOpen?' is-open':''}`} aria-label="История чатов" aria-hidden={!historyOpen || !menuOpen} ref={element=>{if(element)element.inert=!historyOpen || !menuOpen;}}><div className="buddy-history-clip"><div className="buddy-history-content">
          <div className="buddy-history-heading"><span>Твои чаты</span><button className="buddy-icon-button" title="Новый чат" aria-label="Новый чат" disabled={busy || restoring} onClick={newChat}><Icon name="plus"/></button></div>
          {restoring ? <p className="buddy-history-empty">Загружаем историю…</p> : !chats.length ? <p className="buddy-history-empty">Здесь появятся твои чаты после первого вопроса.</p> : <>
            {chats.slice(0,historyLimit).map(chat=><div className={`buddy-history-row${screen==='chat' && active===chat.id?' is-active':''}`} key={chat.id}>
              <button className="buddy-history-link" disabled={busy || restoring} onClick={()=>void selectChat(chat.id)} title={chat.title}>{chat.title}</button>
              <button className="buddy-history-delete" disabled={busy || restoring} aria-label={`Удалить чат ${chat.title}`} onClick={()=>void removeChat(chat.id)}><Icon name="close"/></button>
            </div>)}
            {chats.length>historyLimit && <button className="buddy-text-button" onClick={()=>setHistoryLimit(n=>n+10)}>Загрузить ещё</button>}
            <button className="buddy-clear-history" disabled={busy || restoring} onClick={()=>{if(window.confirm('Удалить все чаты этого аккаунта?'))void removeChat();}}><Icon name="trash"/><span>Удалить все чаты</span></button>
          </>}
        </div></div></section>
      </nav>
      <button className={`buddy-profile-link${screen==='profile'?' is-active':''}`} aria-label="Профиль" title="Профиль" onDoubleClick={()=>setMenuOpen(true)} aria-current={screen==='profile'?'page':undefined} onClick={()=>navigate('profile')}><span className="buddy-avatar buddy-avatar--small">{initials}</span><span><strong>Профиль</strong><small>{profile.name || 'Стажёр'}</small></span></button>
      {menuOpen && <div className="buddy-sidebar-resize" role="separator" tabIndex={0} aria-label="Ширина меню" aria-orientation="vertical" aria-valuemin={240} aria-valuemax={maxMenuWidth} aria-valuenow={displayedMenuWidth}
        title="Потяни, чтобы изменить ширину. Двойной клик — сброс."
        onPointerDown={e=>{if(e.button!==0)return;e.preventDefault();e.currentTarget.setPointerCapture(e.pointerId);drag.current={pointer:e.pointerId,startX:e.clientX,startWidth:displayedMenuWidth,width:displayedMenuWidth};setDragging(true);}}
        onPointerMove={e=>{const current=drag.current;if(!current || current.pointer!==e.pointerId)return;current.width=Math.max(240,Math.min(maxMenuWidth,current.startWidth+e.clientX-current.startX));
          if(resizeFrame.current===null)resizeFrame.current=requestAnimationFrame(()=>{resizeFrame.current=null;if(drag.current)workspace.current?.style.setProperty('--menu-width',`${drag.current.width}px`);});}}
        onPointerUp={e=>{finishResize();if(e.currentTarget.hasPointerCapture(e.pointerId))e.currentTarget.releasePointerCapture(e.pointerId);}}
        onPointerCancel={finishResize} onLostPointerCapture={finishResize} onDoubleClick={()=>setMenuWidth(280)}
        onKeyDown={e=>{if(!['ArrowLeft','ArrowRight','Home','End'].includes(e.key))return;e.preventDefault();setMenuWidth(e.key==='Home'?240:e.key==='End'?maxMenuWidth:Math.max(240,Math.min(maxMenuWidth,displayedMenuWidth+(e.key==='ArrowRight'?20:-20))));}}/>}
    </aside>
    {menuOpen && <button className="buddy-menu-backdrop" aria-label="Закрыть меню" onClick={()=>setMenuOpen(false)}/>}
    <div className="buddy-main">
      <header className="buddy-topbar">

        <h1>{screenTitles[screen]}</h1>
        <ThemeToggle dark={dark} onToggle={onToggleTheme}/>
      </header>
      {screen==='home' && <HomePage userId={userId} name={profile.name} chats={chats} busy={busy || restoring} onChat={id=>void selectChat(id)} onQuestion={question=>{if(sending.current || restoring)return;newChat();setHistoryOpen(true);setInput(question);requestAnimationFrame(()=>textarea.current?.focus());}} onRequests={()=>navigate('requests')} onRoadmap={step=>{
        if(step)try { const key=`buddy-roadmap-${userId}`;localStorage.setItem(key,JSON.stringify({track:step.track || '',step:step.step_id})); } catch { /* Storage is optional. */ }
        navigate('roadmap');
      }}/>}
      {screen==='roadmap' && <main className="buddy-page roadmap-page"><RoadmapPage userId={userId} chatBusy={busy || restoring} refreshVersion={roadmapVersion} onSelectStepQuestion={question=>{if(sending.current || restoring)return;newChat();setHistoryOpen(true);setInput(question);requestAnimationFrame(()=>textarea.current?.focus());}}/></main>}
      {screen==='requests' && <RequestsPage userId={userId} onChat={id=>void selectChat(id)}/>}
      {screen==='profile' && <main className="buddy-page buddy-profile"><div className="buddy-profile-card"><span className="buddy-avatar buddy-avatar--large">{initials}</span><h2>{profile.name || 'Стажёр'}</h2><p className="buddy-profile-caption">Твой профиль стажировки</p><dl className="buddy-profile-details"><div><dt>Почта</dt><dd>{profile.email || 'Войди повторно, чтобы отобразить почту'}</dd></div><div><dt>Команда</dt><dd>{profile.team || 'Не указана'}</dd></div><div><dt>Направление</dt><dd>{profile.track || 'Не указано'}</dd></div></dl></div><footer className="buddy-profile-actions"><button className="buddy-action-button buddy-logout" onClick={()=>changeProfileDialog('logout')}><Icon name="logout"/>Выйти из аккаунта</button><button className="buddy-action-button" onClick={()=>changeProfileDialog('help')}><Icon name="help"/>Помощь</button></footer></main>}
      <main ref={main} hidden={screen!=='chat'} className="chat-main buddy-chat" aria-label="Переписка с AI-buddy">
      <div className="chat-scroll-area"><div className="chat-feed-column">
      {error && <div className="chat-error-banner" role="alert"><span><strong>Не удалось обновить чат</strong><span>{error}</span></span><button className="buddy-action-button" disabled={loading} onClick={()=>{void loadHistory();void loadChats();}}>Повторить</button></div>}
        {loading && <p>Загружаем историю…</p>}
        {next && <button disabled={busy} onClick={()=>void loadHistory(true,next)}>Загрузить предыдущие сообщения</button>}
        {visibleMessages.map(m=><div key={m.id} className={`chat-message chat-message--${m.sender}${m.proposal || m.sources?.length?' chat-message--rich':''}`}><article className={m.sender==='user'?'user-msg-bubble':`assistant-msg-card${m.error?' assistant-msg-card--send-error':''}`}>
          <MessageText text={m.text} assistant={m.sender==='assistant'}/>
          {m.error && <button className="buddy-action-button chat-retry" disabled={busy} onClick={()=>void send(m.retry_text,m.idempotency_key,m.id)}>Повторить</button>}
          {!!m.sources?.length && <MessageSources sources={m.sources}/>}
          {m.proposal && <AccessProposalCard proposal={m.proposal} sending={pending.includes(m.proposal.proposal_id)} error={proposalErrors[m.proposal.proposal_id]} onConfirm={()=>void confirm(m.proposal!.proposal_id)} onRefresh={()=>void loadHistory()}/>}
        </article>{!m.error && <div className="message-actions"><MessageCopyButton text={m.text}/></div>}</div>)}{busy && <p role="status">Готовим ответ…</p>}<div ref={end}/>
      </div></div><div ref={composer} className="chat-composer">{!suggestionsDismissed && !messages.length && !loading && <div className="suggestion-list">{['Как настроить рабочее окружение?','Как оформить отгул?','Какие правила работы с персональными данными?'].map(q=><button key={q} className="suggestion-pill" disabled={busy || restoring} onClick={()=>void send(q)}>{q}</button>)}</div>}<form className="chat-input-bar" onClick={()=>textarea.current?.focus()} onSubmit={e=>{e.preventDefault();void send();}}>
        <textarea ref={textarea} rows={1} aria-label="Вопрос помощнику" placeholder="Введите вопрос" title="Enter — отправить, Shift + Enter — новая строка" disabled={restoring} value={input} onChange={e=>setInput(e.target.value)} onKeyDown={e=>{if(e.key==='Enter'&&!e.shiftKey&&!e.nativeEvent.isComposing){e.preventDefault();void send();}}}/>

      </form></div></main>
    </div>
    {profileDialog==='logout' && <LogoutDialog onClose={()=>changeProfileDialog('')} onLogout={async()=>{await onLogout();saveProfileDialog(userId,'');}}/>}
    <dialog ref={helpDialog} className="buddy-help-dialog" aria-labelledby="buddy-help-title" onCancel={e=>{e.preventDefault();changeProfileDialog('');}} onClick={e=>{if(e.target===e.currentTarget)changeProfileDialog('');}}>
      <HelpContent onClose={()=>changeProfileDialog('')}/>
    </dialog>
  </div>;
}
