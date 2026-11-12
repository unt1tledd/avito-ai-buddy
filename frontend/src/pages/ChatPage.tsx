import React, { useState, useRef, useEffect, useMemo, useCallback } from 'react';
import {
  ChatMessage,
  HistoryItem,
  INITIAL_HISTORY_ITEMS,
  OnboardingStep,
  AccessTask,
  sendChatMessage,
  confirmAccessProposal,
} from '../lib/api';
import { OnboardingPage } from './OnboardingPage';

interface ChatPageProps {
  steps: OnboardingStep[];
  tasks: AccessTask[];
  onToggleStep: (stepId: string) => void;
  onProposalConfirmed: (resourceId: string, issueKey: string) => void;
}

const HISTORY_GROUPS = ['СЕГОДНЯ', 'ВЧЕРА (ДЕНЬ 1)'] as const;

const ClockIcon: React.FC<{ className?: string }> = ({ className }) => (
  <svg className={className} viewBox="0 0 24 24" fill="none" aria-hidden="true">
    <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="2.2" />
    <path
      d="M12 6.2V12l4.2 4.2"
      stroke="currentColor"
      strokeWidth="2.2"
      strokeLinecap="butt"
      strokeLinejoin="miter"
    />
  </svg>
);

const RoadmapIcon: React.FC<{ className?: string }> = ({ className }) => (
  <svg className={className} viewBox="0 0 24 24" fill="none" aria-hidden="true">
    <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="2.2" />
    <path
      d="M12 19.2C12 16.3 11.7 13.4 11.4 9.5M12 16.8C10.4 14.3 7.2 12.8 7.1 6.8M11.6 14.4C14.1 13.5 16.7 11.6 16.7 7.6"
      stroke="currentColor"
      strokeWidth="2.1"
      strokeLinecap="round"
      strokeLinejoin="round"
    />
  </svg>
);

const formatNowTime = () =>
  new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });

export const ChatPage: React.FC<ChatPageProps> = ({
  steps,
  tasks,
  onToggleStep,
  onProposalConfirmed,
}) => {
  const [activeDrawer, setActiveDrawer] = useState<'history' | 'roadmap'>('history');
  const [isDrawerOpen, setIsDrawerOpen] = useState(false);

  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [historyItems, setHistoryItems] = useState<HistoryItem[]>(INITIAL_HISTORY_ITEMS);
  const [activeHistoryId, setActiveHistoryId] = useState<string | null>(null);
  const [inputValue, setInputValue] = useState('');
  const [isLoading, setIsLoading] = useState(false);

  const [proposalStage, setProposalStage] = useState<'draft' | 'sending' | 'approved'>('approved');
  const [showRetryComparison, setShowRetryComparison] = useState(false);

  const messagesEndRef = useRef<HTMLDivElement | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth', block: 'end' });
  }, [messages, isLoading, showRetryComparison]);

  useEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${Math.min(el.scrollHeight, 160)}px`;
  }, [inputValue]);

  const toggleDrawer = useCallback(
    (drawer: 'history' | 'roadmap') => {
      if (isDrawerOpen && activeDrawer === drawer) {
        setIsDrawerOpen(false);
        return;
      }
      setActiveDrawer(drawer);
      setIsDrawerOpen(true);
    },
    [isDrawerOpen, activeDrawer]
  );

  const openDrawer = useCallback((drawer: 'history' | 'roadmap') => {
    setActiveDrawer(drawer);
    setIsDrawerOpen(true);
  }, []);

  const closeDrawer = useCallback(() => {
    setIsDrawerOpen(false);
  }, []);

  const handleStartNewQuestion = useCallback(() => {
    setMessages([]);
    setShowRetryComparison(false);

    const existingDraft = historyItems.find(
      (item) => item.messages.length === 0 && item.dayGroup === 'СЕГОДНЯ'
    );
    if (existingDraft) {
      setActiveHistoryId(existingDraft.id);
      return;
    }

    const newId = `hist-new-${Date.now()}`;
    const newItem: HistoryItem = {
      id: newId,
      dayGroup: 'СЕГОДНЯ',
      timeLabel: `Сегодня, ${formatNowTime()}`,
      title: '«Новый вопрос...»',
      badgeText: 'Новый диалог',
      badgeTone: 'blue',
      messages: [],
    };

    setHistoryItems((prev) => [newItem, ...prev]);
    setActiveHistoryId(newId);
  }, [historyItems]);

  const syncHistoryWithMessages = useCallback(
    (
      targetId: string,
      questionText: string,
      nextMessages: ChatMessage[],
      hasApprovedProposal = false
    ) => {
      const formattedTitle = `«${questionText}»`;

      setHistoryItems((prev) => {
        const exists = prev.some((item) => item.id === targetId);
        if (exists) {
          return prev.map((item) =>
            item.id === targetId
              ? {
                  ...item,
                  title: item.title === '«Новый вопрос...»' ? formattedTitle : item.title,
                  badgeText: hasApprovedProposal ? 'Заявка подтверждена' : 'Есть ответ',
                  badgeTone: hasApprovedProposal ? 'green' : 'blue',
                  messages: nextMessages,
                }
              : item
          );
        }

        const created: HistoryItem = {
          id: targetId,
          dayGroup: 'СЕГОДНЯ',
          timeLabel: `Сегодня, ${formatNowTime()}`,
          title: formattedTitle,
          badgeText: hasApprovedProposal ? 'Заявка подтверждена' : 'Есть ответ',
          badgeTone: hasApprovedProposal ? 'green' : 'blue',
          messages: nextMessages,
        };
        return [created, ...prev];
      });
    },
    []
  );

  const handleSend = useCallback(
    async (rawText?: string, forceErrorDemo = false) => {
      const text = (rawText ?? inputValue).trim();
      if (!text) return;

      setShowRetryComparison(false);
      const idempotencyKey = `idem-${Date.now()}`;
      const currentHistId = activeHistoryId ?? `hist-session-${Date.now()}`;
      if (!activeHistoryId) {
        setActiveHistoryId(currentHistId);
      }

      const userMsg: ChatMessage = {
        id: `u-${Date.now()}`,
        sender: 'user',
        text,
        timestamp: formatNowTime(),
        idempotency_key: idempotencyKey,
      };

      const updatedWithUser = [...messages, userMsg];
      setMessages(updatedWithUser);
      syncHistoryWithMessages(currentHistId, text, updatedWithUser, false);

      if (!rawText) setInputValue('');
      setIsLoading(true);

      try {
        const reply = await sendChatMessage(text, idempotencyKey, forceErrorDemo);
        const hasProposal = Boolean(reply.proposal);
        if (reply.proposal) {
          setProposalStage('approved');
          onProposalConfirmed(
            reply.proposal.resource_id,
            reply.proposal.tracker_issue_key || '1042'
          );
        }
        const finalMessages = [...updatedWithUser, reply];
        setMessages(finalMessages);
        syncHistoryWithMessages(currentHistId, text, finalMessages, hasProposal);
      } catch {
        const errMsg: ChatMessage = {
          id: `err-${Date.now()}`,
          sender: 'assistant',
          text: 'Не удалось получить ответ. Твой вопрос сохранён.',
          timestamp: formatNowTime(),
          idempotency_key: idempotencyKey,
          error: true,
        };
        const failedMessages = [...updatedWithUser, errMsg];
        setMessages(failedMessages);
        syncHistoryWithMessages(currentHistId, text, failedMessages, false);
      } finally {
        setIsLoading(false);
      }
    },
    [inputValue, activeHistoryId, messages, syncHistoryWithMessages, onProposalConfirmed]
  );

  const handleConfirmProposal = useCallback(
    async (proposalId: string, resourceId: string) => {
      setProposalStage('sending');
      const res = await confirmAccessProposal(proposalId);
      setProposalStage('approved');
      onProposalConfirmed(resourceId, res.tracker_issue_key);
    },
    [onProposalConfirmed]
  );

  const handleRetryMessage = useCallback(
    async (failedMsgId: string, questionText: string, idempotencyKey: string) => {
      setIsLoading(true);
      const reply = await sendChatMessage(questionText, idempotencyKey, false);
      setMessages((prev) => prev.map((m) => (m.id === failedMsgId ? reply : m)));
      setIsLoading(false);
    },
    []
  );

  const suggestions = useMemo(() => {
    const lastAssistant = [...messages].reverse().find((m) => m.sender === 'assistant');
    const rawSuggestions = lastAssistant?.proposal
      ? [
          'Перейти к следующему шагу адаптации',
          'Какие доступы нужны стажёру дальше?',
          'Открыть мой Roadmap',
        ]
      : lastAssistant?.text.includes('отгул')
      ? [
          'Создать черновик заявки на отгул в сервисе',
          'Как оформить учебный отпуск?',
          'Открыть мой Roadmap',
        ]
      : [
          'Правила защиты персональных данных ...',
          'Как выполнить текущий шаг? ...',
          'Оформить отгул',
        ];

    return [...rawSuggestions]
      .sort((first, second) => second.length - first.length)
      .slice(0, 3);
  }, [messages]);

  const shellClassName = useMemo(
    () =>
      [
        'drawer-shell',
        isDrawerOpen
          ? activeDrawer === 'history'
            ? 'drawer-shell--open-history'
            : 'drawer-shell--open-roadmap'
          : '',
      ]
        .filter(Boolean)
        .join(' '),
    [isDrawerOpen, activeDrawer]
  );

  return (
    <div className="workspace-layout">
      {/* Выдвижная левая оболочка */}
      <div className={shellClassName}>
        {activeDrawer === 'history' ? (
          <aside
            className={`drawer-panel${!isDrawerOpen ? ' drawer-panel--closed' : ''}`}
            style={{ width: '360px', minWidth: '360px' }}
          >
            <div className="drawer-header">
              <div className="drawer-header-title">
                <ClockIcon className="drawer-title-icon" />
                <h2>История запросов</h2>
              </div>
              <button
                type="button"
                className="ui-icon-circle"
                onClick={closeDrawer}
                title="Закрыть историю"
                aria-label="Закрыть историю"
              >
                ✕
              </button>
            </div>

            <button
              type="button"
              className="ui-btn-dark"
              onClick={handleStartNewQuestion}
              style={{
                width: '100%',
                padding: '12px',
                borderRadius: '10px',
                border: '2px solid var(--border-color)',
                background: 'var(--btn-primary-bg)',
                color: 'var(--btn-primary-text)',
                fontWeight: 700,
                fontSize: '14px',
                marginBottom: '16px',
              }}
            >
              + Новый вопрос
            </button>

            {HISTORY_GROUPS.map((group) => (
              <div key={group} style={{ marginBottom: '14px' }}>
                <div
                  style={{
                    fontSize: '11px',
                    fontWeight: 800,
                    color: 'var(--text-secondary)',
                    marginBottom: '8px',
                  }}
                >
                  {group}
                </div>
                <div style={{ display: 'flex', flexDirection: 'column', gap: '10px' }}>
                  {historyItems
                    .filter((item) => item.dayGroup === group)
                    .map((item) => {
                      const isSelected = item.id === activeHistoryId;
                      return (
                        <div
                          key={item.id}
                          className="ui-card-interactive"
                          onClick={() => {
                            setActiveHistoryId(item.id);
                            setMessages(item.messages);
                            setShowRetryComparison(false);
                          }}
                          style={{
                            border: '2px solid var(--border-color)',
                            borderRadius: '12px',
                            padding: '12px',
                            background: isSelected
                              ? 'var(--bg-active-card)'
                              : 'var(--bg-surface)',
                            boxShadow: isSelected ? '0 2px 0 var(--shadow-color)' : undefined,
                            cursor: 'pointer',
                          }}
                        >
                          <div
                            style={{
                              display: 'flex',
                              justifyContent: 'space-between',
                              alignItems: 'center',
                              marginBottom: '6px',
                            }}
                          >
                            <span style={{ fontSize: '11px', color: 'var(--text-secondary)' }}>
                              {item.timeLabel}
                            </span>
                            <span
                              className="status-badge"
                              style={{
                                background:
                                  item.badgeTone === 'green'
                                    ? '#C9FFBF'
                                    : item.badgeTone === 'blue'
                                    ? '#BADAFB'
                                    : '#F3F4F6',
                              }}
                            >
                              {item.badgeText}
                            </span>
                          </div>
                          <div style={{ fontSize: '13px', fontWeight: 700 }}>{item.title}</div>
                        </div>
                      );
                    })}
                </div>
              </div>
            ))}

            <button
              type="button"
              className="ui-btn"
              onClick={() =>
                setHistoryItems((prev) => [
                  ...prev,
                  {
                    id: `hist-extra-${Date.now()}`,
                    dayGroup: 'ВЧЕРА (ДЕНЬ 1)',
                    timeLabel: 'День 1, 10:00',
                    title: '«Где найти AvitoTech Playbook?»',
                    badgeText: 'Просмотрено',
                    badgeTone: 'gray',
                    messages: [
                      {
                        id: 'm-ex-1',
                        sender: 'user',
                        text: 'Где найти AvitoTech Playbook?',
                        timestamp: '10:00',
                      },
                      {
                        id: 'm-ex-2',
                        sender: 'assistant',
                        text: 'Открытый справочник AvitoTech Playbook доступен в официальном GitHub-репозитории компании.',
                        timestamp: '10:00',
                      },
                    ],
                  },
                ])
              }
              style={{
                marginTop: 'auto',
                width: '100%',
                padding: '10px',
                borderRadius: '10px',
                border: '2px solid var(--border-color)',
                background: 'var(--bg-muted)',
                color: 'var(--text-primary)',
                fontWeight: 700,
                fontSize: '13px',
              }}
            >
              Загрузить ещё
            </button>
          </aside>
        ) : (
          <OnboardingPage
            steps={steps}
            tasks={tasks}
            isClosing={!isDrawerOpen}
            onClose={closeDrawer}
            onToggleStep={onToggleStep}
            onSelectStepQuestion={handleSend}
          />
        )}
      </div>

      {isDrawerOpen && (
        <div
          className="mobile-drawer-backdrop"
          onClick={closeDrawer}
          aria-hidden="true"
        />
      )}

      {/* ОСНОВНАЯ ПРАВАЯ ОБЛАСТЬ ЧАТА */}
      <main className="chat-main">
        <div className="floating-nav-icons">
          <button
            type="button"
            className={`drawer-toggle-btn${
              isDrawerOpen && activeDrawer === 'history' ? ' drawer-toggle-btn--active' : ''
            }`}
            onClick={() => toggleDrawer('history')}
            title="История запросов"
            aria-label="История запросов"
          >
            <ClockIcon />
          </button>

          <button
            type="button"
            className={`drawer-toggle-btn${
              isDrawerOpen && activeDrawer === 'roadmap' ? ' drawer-toggle-btn--active' : ''
            }`}
            onClick={() => toggleDrawer('roadmap')}
            title="Roadmap"
            aria-label="Roadmap"
          >
            <RoadmapIcon />
          </button>
        </div>

        <div className="chat-scroll-area">
          <div className="chat-feed-column">
            {messages.length === 0 && !showRetryComparison && (
              <div
                style={{
                  flex: 1,
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  color: 'var(--text-muted)',
                  fontSize: '18px',
                  fontWeight: 600,
                  textAlign: 'center',
                }}
              >
                Задай вопрос ниже или выбери одну из быстрых подсказок
              </div>
            )}

            {showRetryComparison && (
              <div className="retry-comparison-grid">
                {/* Левая карточка: Сбой */}
                <div
                  style={{
                    border: '2px solid var(--border-color)',
                    borderRadius: '16px',
                    padding: '18px',
                    background: 'var(--bg-surface)',
                  }}
                >
                  <div
                    style={{
                      color: '#FF4053',
                      fontWeight: 800,
                      fontSize: '14px',
                      paddingBottom: '10px',
                      borderBottom: '2px solid var(--border-color)',
                      marginBottom: '14px',
                    }}
                  >
                    1. Если не удалось получить ответ с первого раза
                  </div>
                  <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: '12px' }}>
                    <div
                      style={{
                        border: '2px solid var(--border-color)',
                        borderRadius: '999px',
                        padding: '8px 18px',
                        background: 'var(--bg-muted)',
                        color: 'var(--text-primary)',
                        fontWeight: 700,
                        fontSize: '13px',
                      }}
                    >
                      Какие правила работы с персональными данными?
                    </div>
                  </div>
                  <div
                    style={{
                      border: '2px solid #141414',
                      borderRadius: '14px',
                      padding: '14px',
                      background: '#F7C1C5',
                      color: '#141414',
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '12px' }}>
                      <span
                        style={{
                          width: '26px',
                          height: '26px',
                          borderRadius: '50%',
                          background: '#FF4053',
                          color: '#FFFFFF',
                          display: 'inline-flex',
                          alignItems: 'center',
                          justifyContent: 'center',
                          fontWeight: 800,
                          border: '1.5px solid #141414',
                        }}
                      >
                        !
                      </span>
                      <div>
                        <div style={{ fontWeight: 800, fontSize: '13px', color: '#141414' }}>
                          Не удалось получить ответ. Твой вопрос сохранён
                        </div>
                        <div style={{ fontSize: '12px', color: '#141414' }}>
                          Нажми кнопку ниже, чтобы отправить его ещё раз.
                        </div>
                      </div>
                    </div>
                    <button
                      type="button"
                      className="ui-btn"
                      onClick={() => {
                        setShowRetryComparison(false);
                        handleSend('Какие правила работы с персональными данными?');
                      }}
                      style={{
                        width: '100%',
                        padding: '10px',
                        borderRadius: '10px',
                        border: '2px solid #141414',
                        background: '#FFFFFF',
                        color: '#141414',
                        fontWeight: 800,
                        fontSize: '13px',
                      }}
                    >
                      ↻ Повторить отправку
                    </button>
                  </div>
                </div>

                {/* Правая карточка: После нажатия */}
                <div
                  style={{
                    border: '2px solid var(--border-color)',
                    borderRadius: '16px',
                    padding: '18px',
                    background: 'var(--bg-surface)',
                  }}
                >
                  <div
                    style={{
                      display: 'inline-block',
                      background: '#C9FFBF',
                      color: '#141414',
                      border: '1.5px solid #141414',
                      borderRadius: '6px',
                      padding: '4px 10px',
                      fontWeight: 800,
                      fontSize: '13px',
                      marginBottom: '12px',
                    }}
                  >
                    2. После нажатия на «↻ Повторить отправку»
                  </div>
                  <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: '12px' }}>
                    <div
                      style={{
                        border: '2px solid var(--border-color)',
                        borderRadius: '999px',
                        padding: '8px 18px',
                        background: 'var(--bg-muted)',
                        color: 'var(--text-primary)',
                        fontWeight: 700,
                        fontSize: '13px',
                      }}
                    >
                      Какие правила работы с персональными данными?
                    </div>
                  </div>
                  <div
                    style={{
                      border: '2px solid var(--border-color)',
                      borderRadius: '14px',
                      padding: '14px',
                      background: 'var(--bg-subtle)',
                    }}
                  >
                    <div style={{ marginBottom: '10px' }}>
                      <span
                        className="ai-name-badge"
                        style={{ padding: '5px 14px', fontSize: '14px', borderWidth: '1.5px' }}
                      >
                        AI-buddy
                      </span>
                    </div>
                    <p style={{ fontSize: '13px', lineHeight: 1.45, marginBottom: '12px' }}>
                      При работе с <strong>персональными данными и внутренней информацией</strong> используй только корпоративные сервисы Авито и не загружай рабочие файлы на сторонние сайты.
                    </p>
                    <div
                      style={{
                        background: '#BADAFB',
                        border: '1.5px solid #141414',
                        borderRadius: '10px',
                        padding: '10px',
                        display: 'flex',
                        flexDirection: 'column',
                        gap: '6px',
                      }}
                    >
                      <a
                        className="ui-btn"
                        href="https://avito.tech/"
                        target="_blank"
                        rel="noreferrer"
                        style={{
                          background: '#FFFFFF',
                          border: '1.5px solid #141414',
                          borderRadius: '6px',
                          padding: '5px 10px',
                          fontSize: '12px',
                          fontWeight: 700,
                          color: '#141414',
                          textDecoration: 'none',
                          width: 'fit-content',
                        }}
                      >
                        [1] Лендинг «Защита данных» ↗
                      </a>
                      <a
                        className="ui-btn"
                        href="https://github.com/avito-tech/playbook"
                        target="_blank"
                        rel="noreferrer"
                        style={{
                          background: '#FFFFFF',
                          border: '1.5px solid #141414',
                          borderRadius: '6px',
                          padding: '5px 10px',
                          fontSize: '12px',
                          fontWeight: 700,
                          color: '#141414',
                          textDecoration: 'none',
                          width: 'fit-content',
                        }}
                      >
                        [2] Памятка по безопасности ↗
                      </a>
                    </div>
                  </div>
                </div>
              </div>
            )}

            {!showRetryComparison &&
              messages.map((msg) =>
                msg.sender === 'user' ? (
                  <div key={msg.id} style={{ display: 'flex', justifyContent: 'flex-end' }}>
                    <div className="user-msg-bubble">{msg.text}</div>
                  </div>
                ) : (
                  <div key={msg.id} style={{ display: 'flex', justifyContent: 'flex-start' }}>
                    <div
                      className={`assistant-msg-card${
                        msg.error ? ' assistant-msg-card--error' : ''
                      }`}
                    >
                      <div style={{ marginBottom: '12px' }}>
                        <span className="ai-name-badge">AI-buddy</span>
                      </div>

                      <div
                        style={{
                          fontSize: '15px',
                          lineHeight: 1.55,
                          whiteSpace: 'pre-line',
                          marginBottom: msg.sources || msg.proposal || msg.error ? '16px' : '0',
                        }}
                      >
                        {msg.text}
                      </div>

                      {msg.error && (
                        <button
                          type="button"
                          className="ui-btn"
                          onClick={() =>
                            handleRetryMessage(
                              msg.id,
                              'Какие правила работы с персональными данными?',
                              msg.idempotency_key || 'idem-retry'
                            )
                          }
                          style={{
                            width: '100%',
                            padding: '10px',
                            borderRadius: '10px',
                            border: '2px solid #141414',
                            background: '#FFFFFF',
                            color: '#141414',
                            fontWeight: 800,
                          }}
                        >
                          ↻ Повторить отправку (с тем же idempotency_key)
                        </button>
                      )}

                      {msg.sources && msg.sources.length > 0 && (
                        <div
                          style={{
                            background: '#BADAFB',
                            color: '#141414',
                            border: '2px solid #141414',
                            borderRadius: '12px',
                            padding: '14px',
                          }}
                        >
                          <div style={{ fontSize: '12px', fontWeight: 800, marginBottom: '10px' }}>
                            Полезные ссылки и правила:
                          </div>
                          <div style={{ display: 'flex', flexWrap: 'wrap', gap: '10px' }}>
                            {msg.sources.map((src) => (
                              <a
                                key={src.id}
                                className="ui-btn"
                                href={src.url}
                                target="_blank"
                                rel="noreferrer"
                                style={{
                                  background: '#FFFFFF',
                                  border: '2px solid #141414',
                                  borderRadius: '8px',
                                  padding: '7px 14px',
                                  fontSize: '13px',
                                  fontWeight: 700,
                                  color: '#141414',
                                  textDecoration: 'none',
                                }}
                              >
                                {src.title} ↗
                              </a>
                            ))}
                          </div>
                        </div>
                      )}

                      {msg.proposal && (
                        <div
                          style={{
                            border: '2px solid var(--border-color)',
                            borderRadius: '14px',
                            overflow: 'hidden',
                            background: 'var(--bg-surface)',
                          }}
                        >
                          <div style={{ padding: '16px' }}>
                            <div style={{ fontSize: '16px', fontWeight: 800, marginBottom: '4px' }}>
                              {msg.proposal.title}
                            </div>
                            <div style={{ fontSize: '13px', color: 'var(--text-secondary)' }}>
                              Роль: <strong>{msg.proposal.role_label}</strong> • Раздел:{' '}
                              <strong>{msg.proposal.resource_label}</strong>
                            </div>
                          </div>

                          <div
                            style={{
                              padding: '10px 16px',
                              borderTop: '1.5px solid var(--border-color)',
                              borderBottom: '1.5px solid var(--border-color)',
                              display: 'flex',
                              alignItems: 'center',
                              justifyContent: 'space-between',
                              flexWrap: 'wrap',
                              gap: '8px',
                              background: 'var(--bg-subtle)',
                            }}
                          >
                            <span style={{ fontSize: '12px', color: 'var(--text-secondary)', fontWeight: 600 }}>
                              Посмотреть вид карточки:
                            </span>
                            <div style={{ display: 'flex', gap: '6px' }}>
                              {(
                                [
                                  { id: 'draft', label: 'До нажатия' },
                                  { id: 'sending', label: 'Отправка...' },
                                  { id: 'approved', label: 'Готово (Доступ получен)' },
                                ] as const
                              ).map((stage) => {
                                const isCurrentStage = proposalStage === stage.id;
                                return (
                                  <button
                                    key={stage.id}
                                    type="button"
                                    className={isCurrentStage ? 'ui-btn-dark' : 'ui-btn'}
                                    onClick={() => setProposalStage(stage.id)}
                                    style={{
                                      padding: '5px 10px',
                                      borderRadius: '6px',
                                      border: '1.5px solid var(--border-color)',
                                      background: isCurrentStage
                                        ? 'var(--btn-primary-bg)'
                                        : 'var(--bg-surface)',
                                      color: isCurrentStage
                                        ? 'var(--btn-primary-text)'
                                        : 'var(--text-primary)',
                                      fontSize: '12px',
                                      fontWeight: 700,
                                    }}
                                  >
                                    {stage.label}
                                  </button>
                                );
                              })}
                            </div>
                          </div>

                          {proposalStage === 'draft' && (
                            <div style={{ padding: '14px 16px', background: 'var(--bg-surface)' }}>
                              <button
                                type="button"
                                className="ui-btn-green"
                                onClick={() =>
                                  handleConfirmProposal(
                                    msg.proposal!.proposal_id,
                                    msg.proposal!.resource_id
                                  )
                                }
                                style={{
                                  width: '100%',
                                  padding: '11px',
                                  borderRadius: '10px',
                                  border: '2px solid #141414',
                                  background: '#04E061',
                                  color: '#141414',
                                  fontWeight: 800,
                                  fontSize: '14px',
                                }}
                              >
                                Подтвердить доступ (POST /api/v1/chat/proposals/{msg.proposal.proposal_id}/confirm)
                              </button>
                            </div>
                          )}

                          {proposalStage === 'sending' && (
                            <div
                              style={{
                                padding: '14px 16px',
                                background: '#BADAFB',
                                color: '#141414',
                                fontWeight: 700,
                                fontSize: '14px',
                              }}
                            >
                              ⏳ Отправляется в Task-трекер (pending_dispatch)...
                            </div>
                          )}

                          {proposalStage === 'approved' && (
                            <div
                              style={{
                                padding: '14px 16px',
                                background: '#C9FFBF',
                                color: '#141414',
                                display: 'flex',
                                alignItems: 'center',
                                justifyContent: 'space-between',
                                fontWeight: 800,
                                fontSize: '14px',
                              }}
                            >
                              <span>
                                ✓ Доступ подтверждён • Заявка №{msg.proposal.tracker_issue_key || '1042'} создана
                              </span>
                            </div>
                          )}
                        </div>
                      )}
                    </div>
                  </div>
                )
              )}

            {isLoading && (
              <div style={{ fontSize: '14px', color: 'var(--text-secondary)', fontWeight: 600 }}>
                AI-buddy сверяется с базой знаний и планом адаптации...
              </div>
            )}

            <div ref={messagesEndRef} />
          </div>
        </div>

        <div className="chat-composer">
          <div className="suggestion-list">
            {suggestions.map((sug) => (
              <button
                key={sug}
                type="button"
                className="suggestion-pill"
                onClick={() => {
                  if (sug === 'Открыть мой Roadmap') {
                    openDrawer('roadmap');
                  } else if (sug.startsWith('Правила защиты')) {
                    setShowRetryComparison(true);
                  } else {
                    handleSend(sug.replace(' ...', ''));
                  }
                }}
              >
                <span>{sug}</span>
              </button>
            ))}
          </div>

          <form
            className="chat-input-bar"
            onSubmit={(e) => {
              e.preventDefault();
              handleSend();
            }}
          >
            <button
              type="button"
              className="ui-icon-circle"
              onClick={() => setShowRetryComparison((v) => !v)}
              title="Показать / скрыть демо повтора при сбое (Схема 04)"
              style={{ width: '36px', height: '36px', fontSize: '22px' }}
            >
              +
            </button>
            <textarea
              ref={textareaRef}
              rows={1}
              placeholder="Введите вопрос"
              value={inputValue}
              onChange={(e) => setInputValue(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.shiftKey) {
                  e.preventDefault();
                  handleSend();
                }
              }}
            />
          </form>
        </div>
      </main>
    </div>
  );
};
