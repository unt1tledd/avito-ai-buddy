import React, { useEffect, useRef, useState } from 'react';

const MIN_WIDTH = 280;
const DEFAULT_WIDTH = 380;
const STORAGE_KEY = 'buddy-sidebar-width';

export function ResizableSidebar({panel, children}: {
  panel: 'history' | 'roadmap' | 'profile' | null;
  children: React.ReactNode;
}) {
  const shell = useRef<HTMLDivElement>(null);
  const lastContent = useRef(children);
  if (panel) lastContent.current = children;
  const drag = useRef<{pointer:number; startX:number; startWidth:number} | null>(null);
  const [dragging,setDragging] = useState(false);
  const [maxWidth,setMaxWidth] = useState(640);
  const [width,setWidth] = useState(()=>{
    try {
      const saved=Number(localStorage.getItem(STORAGE_KEY));
      return Number.isFinite(saved) && saved >= MIN_WIDTH ? Math.min(saved,640) : DEFAULT_WIDTH;
    } catch { return DEFAULT_WIDTH; }
  });
  const displayedWidth=Math.min(width,maxWidth);
  useEffect(()=>{
    const parent=shell.current?.parentElement;
    if(!parent)return;
    const observer=new ResizeObserver(()=>setMaxWidth(Math.max(MIN_WIDTH,Math.min(640,parent.clientWidth-400))));
    observer.observe(parent);
    return ()=>observer.disconnect();
  },[]);
  useEffect(()=>{
    try {localStorage.setItem(STORAGE_KEY,String(width));} catch { /* Storage is optional. */ }
  },[width]);
  useEffect(()=>{
    if(!dragging)return;
    document.body.classList.add('sidebar-resizing');
    return ()=>document.body.classList.remove('sidebar-resizing');
  },[dragging]);
  useEffect(()=>{drag.current=null;setDragging(false);},[panel]);
  const finish=()=>{drag.current=null;setDragging(false);};
  return <div ref={shell}
    className={`drawer-shell resizable-sidebar${panel?` drawer-shell--open-${panel}`:''}${dragging?' resizable-sidebar--dragging':''}`}
    style={{'--sidebar-width':`${displayedWidth}px`} as React.CSSProperties}>
    <div className="sidebar-content" aria-hidden={!panel}>{panel ? children : lastContent.current}</div>
    {panel && <div className="sidebar-resize-handle" role="separator" tabIndex={0}
      aria-label="Ширина левой панели" aria-orientation="vertical"
      aria-valuemin={MIN_WIDTH} aria-valuemax={maxWidth} aria-valuenow={displayedWidth}
      title="Потяни, чтобы изменить ширину. Двойной клик — сброс."
      onPointerDown={e=>{
        if(e.button!==0)return;
        e.preventDefault();e.currentTarget.setPointerCapture(e.pointerId);
        drag.current={pointer:e.pointerId,startX:e.clientX,startWidth:displayedWidth};setDragging(true);
      }}
      onPointerMove={e=>{
        const current=drag.current;if(!current || current.pointer!==e.pointerId)return;
        setWidth(Math.max(MIN_WIDTH,Math.min(maxWidth,current.startWidth+e.clientX-current.startX)));
      }}
      onPointerUp={e=>{finish();if(e.currentTarget.hasPointerCapture(e.pointerId))e.currentTarget.releasePointerCapture(e.pointerId);}}
      onPointerCancel={finish} onLostPointerCapture={finish}
      onDoubleClick={()=>setWidth(DEFAULT_WIDTH)}
      onKeyDown={e=>{
        if(!['ArrowLeft','ArrowRight','Home','End'].includes(e.key))return;
        e.preventDefault();
        setWidth(e.key==='Home'?MIN_WIDTH:e.key==='End'?maxWidth:Math.max(MIN_WIDTH,Math.min(maxWidth,displayedWidth+(e.key==='ArrowRight'?20:-20))));
      }}/ >}
  </div>;
}
