import React from 'react';
import { Proposal, statusLabel } from '../lib/api';

export function AccessProposalCard({proposal, sending, error, onConfirm, onRefresh}: {
  proposal: Proposal; sending: boolean; error?: string; onConfirm: () => void; onRefresh?: () => void;
}) {
  const waiting = ['pending_dispatch','pending','pending_approval'].includes(proposal.status);
  const approved = proposal.status === 'approved';
  const rejected = proposal.status === 'rejected';
  const failed = proposal.status === 'failed';
  const draft = proposal.status === 'draft';
  const canSend = draft || failed;
  const tone = waiting ? 'waiting' : approved ? 'approved' : rejected || failed ? 'failed' : 'neutral';
  const heading = sending ? 'Отправляем заявку' : draft ? 'Готова к отправке'
    : proposal.status === 'pending_dispatch' ? 'Заявка принята'
    : waiting ? 'Ожидаем решения' : approved ? 'Доступ одобрен'
    : rejected ? 'В доступе отказано' : failed ? 'Не удалось отправить' : statusLabel(proposal.status);
  const description = sending ? 'Подожди, пока сервер подтвердит отправку.'
    : draft ? 'Проверь ресурс и уровень доступа. Заявка отправится только после твоего подтверждения.'
    : proposal.status === 'pending_dispatch' ? 'Заявка в очереди на отправку. Её состояние сохранено — можно закрыть чат и вернуться позже.'
    : waiting ? 'Заявка отправлена на согласование. Рассмотрение может занять время. Решение появится здесь, даже если ты закроешь сайт.'
    : approved ? 'Заявка согласована. Решение сохранено в этой переписке.'
    : rejected ? 'Заявка отклонена. Если доступ всё ещё нужен, уточни причину и дальнейшие действия у наставника.'
    : failed ? 'Отправка завершилась ошибкой. Можно повторить попытку для этой же заявки.'
    : 'Здесь отображается последнее состояние заявки, полученное от сервера.';
  const stage = approved || rejected ? 2 : waiting ? 1 : 0;
  return <section className={`proposal-card proposal-card--${tone}`} aria-label="Заявка на доступ">
    <header className="proposal-heading">
      <span className="proposal-icon" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round">
        {approved ? <><circle cx="12" cy="12" r="9"/><path d="m8 12 3 3 5-6"/></> : rejected || failed ? <><circle cx="12" cy="12" r="9"/><path d="m9 9 6 6m0-6-6 6"/></> : waiting ? <><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></> : <><rect x="5" y="10" width="14" height="11" rx="3"/><path d="M8 10V7a4 4 0 0 1 8 0v3M12 14v3"/></>}
      </svg></span>
      <div><span className="proposal-eyebrow">Заявка на доступ</span><h3>{proposal.resource_label || proposal.resource_id}</h3></div>
    </header>
    <dl className="proposal-details"><div><dt>Ресурс</dt><dd>{proposal.resource_id}</dd></div><div><dt>Уровень доступа</dt><dd>{proposal.access_level}</dd></div>{proposal.tracker_issue_key && <div><dt>Номер заявки</dt><dd>{proposal.tracker_issue_key}</dd></div>}</dl>
    <ol className="proposal-progress" aria-label="Этапы заявки">{['Подготовлена','Отправлена','Решение'].map((label,index)=><li key={label} className={index<=stage?'is-reached':''} aria-current={index===stage?'step':undefined}><span aria-hidden="true"/>{label}</li>)}</ol>
    <div className="proposal-feedback" role="status" aria-live="polite"><h4>{heading}</h4><p>{description}</p></div>
    {error && <div className="proposal-error" role="alert"><strong>Не удалось подтвердить отправку</strong><p>{error}</p><p>Повторная попытка проверит состояние этой же заявки.</p></div>}
    {(canSend || (waiting && onRefresh)) && <footer className="proposal-actions">
      {canSend && <button className="proposal-submit" type="button" disabled={sending} aria-busy={sending} onClick={onConfirm}>
        {sending && <span className="proposal-spinner" aria-hidden="true"/>}{sending ? 'Отправляем…' : failed ? 'Повторить отправку' : 'Отправить заявку'}
      </button>}
      {waiting && onRefresh && <button className="proposal-refresh" type="button" onClick={onRefresh}>Обновить статус</button>}
    </footer>}
  </section>;
}
