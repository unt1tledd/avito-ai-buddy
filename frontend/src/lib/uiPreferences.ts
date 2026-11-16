export type ProfileDialog = '' | 'help' | 'logout';

export function readMenuOpen(userId:string, fallback:boolean):boolean {
  try {
    const saved=localStorage.getItem(`buddy-menu-open-${userId}`);
    return saved==='true'?true:saved==='false'?false:fallback;
  } catch { return fallback; }
}
export function saveMenuOpen(userId:string, open:boolean):void {
  try { localStorage.setItem(`buddy-menu-open-${userId}`,String(open)); } catch { /* Storage is optional. */ }
}
export function readProfileDialog(userId:string):ProfileDialog {
  try {
    const saved=sessionStorage.getItem(`buddy-profile-dialog-${userId}`);
    return saved==='help' || saved==='logout'?saved:'';
  } catch { return ''; }
}
export function saveProfileDialog(userId:string, dialog:ProfileDialog):void {
  try {
    const key=`buddy-profile-dialog-${userId}`;
    if(dialog)sessionStorage.setItem(key,dialog);else sessionStorage.removeItem(key);
  } catch { /* Storage is optional. */ }
}
