export type UserRole = 'intern' | 'teamlead';

export function getAccessToken(): string {
  const match = document.cookie.match(/(?:^|;\s*)access_token=([^;]*)/);
  return match ? decodeURIComponent(match[1]) : '';
}

export function setAccessToken(token: string): void {
  document.cookie = `access_token=${encodeURIComponent(token)}; path=/; max-age=86400`;
}

export function getSavedRole(): UserRole {
  return (localStorage.getItem('avito_buddy_role') as UserRole) || 'intern';
}

export function setSavedRole(role: UserRole): void {
  localStorage.setItem('avito_buddy_role', role);
}
