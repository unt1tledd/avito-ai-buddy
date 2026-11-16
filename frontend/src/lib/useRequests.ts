import { useEffect, useState } from 'react';
import { ConversationProposal, fetchUserRequests } from './api';
export function useRequests(userId:string) {
  const [items,setItems]=useState<ConversationProposal[]>([]);
  const [loaded,setLoaded]=useState(false);
  const [loading,setLoading]=useState(true);
  const [failed,setFailed]=useState(0);
  const [version,setVersion]=useState(0);
  useEffect(()=>{
    let active=true,running=false;
    const refresh=async()=>{
      if(running)return;
      running=true;setLoading(true);
      try { const result=await fetchUserRequests(userId);if(active){setItems(result);setFailed(0);setLoaded(true);} }
      catch {if(active)setFailed(1);}
      finally { running=false;if(active)setLoading(false); }
    };
    void refresh();
    const visibleRefresh=()=>{if(document.visibilityState==='visible')void refresh();};
    const timer=setInterval(visibleRefresh,60000);
    window.addEventListener('focus',visibleRefresh);
    document.addEventListener('visibilitychange',visibleRefresh);
    return ()=>{active=false;clearInterval(timer);window.removeEventListener('focus',visibleRefresh);document.removeEventListener('visibilitychange',visibleRefresh);};
  },[userId,version]);
  return {items,loaded,loading,failed,refresh:()=>setVersion(v=>v+1)};
}
