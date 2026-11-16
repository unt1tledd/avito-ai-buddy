import React from 'react';
export type NavigationIconName = 'home' | 'roadmap' | 'requests' | 'chat';

// Shared vector silhouettes follow the supplied references and inherit theme color.
export function NavigationIcon({name}:{name:NavigationIconName}) {
  const shapes:Record<NavigationIconName,React.ReactNode>={
    home:<><path d="M16 6V3h3v5.5"/><path d="m3.7 10 7.2-6.2a1.7 1.7 0 0 1 2.2 0l7.2 6.2a2 2 0 0 1 .7 1.5V20a1 1 0 0 1-1 1h-5v-7H9v7H4a1 1 0 0 1-1-1v-8.5a2 2 0 0 1 .7-1.5Z"/></>,
    roadmap:<><path d="M7.8 4H14c5.2 0 6.8 4.6 4.5 7.8M12 15c-6.6 1.2-6.2 7 .2 7h4"/><circle cx="5" cy="4" r="2.8"/><circle cx="15" cy="13" r="3.7"/><path d="m13.4 13 1.2 1.2 2.1-2.3"/><circle cx="19" cy="22" r="2.8"/></>,
    requests:<><path d="M5 17V3a1 1 0 0 1 1-1h9l4 4v11M15 2v4h4M8 10h7M8 13h5"/><path d="M5 14H4a2 2 0 0 0-2 2v4a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-4a2 2 0 0 0-2-2h-1M2 17h5l2 3h6l2-3h5"/></>,
    chat:<><path d="M3 21v-9a9 9 0 1 1 9 9H6a5 5 0 0 0-3 1Z"/><circle cx="8.7" cy="11" r=".7" fill="currentColor" stroke="none"/><circle cx="15.3" cy="11" r=".7" fill="currentColor" stroke="none"/><path d="M9.5 14a4.5 4.5 0 0 0 5 0"/></>,
  };
  return <svg viewBox="0 0 25 26" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" data-navigation-icon={name}>{shapes[name]}</svg>;
}
