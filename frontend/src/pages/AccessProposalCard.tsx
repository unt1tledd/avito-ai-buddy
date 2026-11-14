import React from 'react';
import { Proposal, statusLabel } from '../lib/api';

export function AccessProposalCard({proposal, sending, error, onConfirm}: {
  proposal: Proposal; sending: boolean; error?: string; onConfirm: () => void;
}) {
  const canSend = proposal.status === 'draft' || proposal.status === 'failed';
  const tone = ['pending_dispatch','pending','pending_approval'].includes(proposal.status)
    ? 'waiting' : proposal.status === 'approved' ? 'approved'
    : ['failed','rejected'].includes(proposal.status) ? 'failed' : 'neutral';
  return <section className="proposal-card" aria-label="Заявка на доступ">
    <h3>{proposal.resource_label}</h3>
    <p>Ресурс: {proposal.resource_id}</p>
    <p>Уровень доступа: {proposal.access_level}</p>
    {proposal.status !== 'draft' && <p role="status">
      <span className={`status-badge proposal-status proposal-status--${tone}`}>
        {tone === 'waiting' && <svg viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.8"><circle cx="12" cy="12" r="9"/><path d="M12 6v6l4 2"/></svg>}
        {statusLabel(proposal.status)}
      </span>
    </p>}
    {proposal.tracker_issue_key && <p>Заявка: {proposal.tracker_issue_key}</p>}
    {error && <p role="alert">{error}</p>}
    {canSend && <button type="button" disabled={sending} aria-busy={sending} onClick={onConfirm}>
      {sending && <span className="proposal-spinner" aria-hidden="true"/>}
      {sending ? 'Отправляем…' : proposal.status === 'failed' ? 'Повторить отправку заявки' : 'Отправить заявку'}
    </button>}
  </section>;
}
