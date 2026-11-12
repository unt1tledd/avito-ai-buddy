import React, { useState, useRef, useEffect } from 'react';
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
  onProposalConfirmed: (resourceId: string, issueKey: string) => void;
}

export const ChatPage: React.FC<ChatPageProps> = ({
  steps,
  tasks,
  onProposalConfirmed,
}) => {
  // Какая левая панель выбрана и открыта ли она сейчас
  const [activeDrawer, setActiveDrawer] = useState<'history' | 'roadmap'>('history');
  const [isDrawerOpen, setIsDrawerOpen] = useState(false);

  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [historyItems, setHistoryItems] = useState<HistoryItem[]>(INITIAL_HISTORY_ITEMS);
  const [activeHistoryId, setActiveHistoryId] = useState<string | null>(null);
  const [inputValue, setInputValue] = useState('');
  const [isLoading, setIsLoading] = useState(false);

  // Состояние демонстрации карточки доступа: 'draft' | 'sending' | 'approved'
  const [proposalStage, setProposalStage] = useState<'draft' | 'sending' | 'approved'>('approved');

  // Режим демонстрации ошибки и повтора (Схема 04)
  const [showRetryComparison, setShowRetryComparison] = useState(false);

  // Реф для автоскролла чата вниз при переполнении экрана
  const messagesEndRef = useRef<HTMLDivElement | null>(null);

  // Реф для автоматического увеличения высоты поля ввода (как в Gemini)
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

  const toggleDrawer = (drawer: 'history' | 'roadmap') => {
    if (isDrawerOpen && activeDrawer === drawer) {
      setIsDrawerOpen(false);
      return;
    }
    setActiveDrawer(drawer);
    setIsDrawerOpen(true);
  };

  const openDrawer = (drawer: 'history' | 'roadmap') => {
    setActiveDrawer(drawer);
    setIsDrawerOpen(true);
  };

  const closeDrawer = () => {
    setIsDrawerOpen(false);
  };

  // Создание нового диалога в панели «История запросов»
  const handleStartNewQuestion = () => {
    setMessages([]);
    setShowRetryComparison(false);

    const existingDraft = historyItems.find(
      (item) => item.messages.length === 0 && item.dayGroup === 'СЕГОДНЯ'
    );
    if (existingDraft) {
      setActiveHistoryId(existingDraft.id);
      return;
    }

    const nowLabel = new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    const newId = 'hist-new-' + Date.now();
    const newItem: HistoryItem = {
      id: newId,
      dayGroup: 'СЕГОДНЯ',
      timeLabel: `Сегодня, ${nowLabel}`,
      title: '«Новый вопрос...»',
      badgeText: 'Новый диалог',
      badgeTone: 'blue',
      messages: [],
    };

    setHistoryItems((prev) => [newItem, ...prev]);
    setActiveHistoryId(newId);
  };

  const syncHistoryWithMessages = (
    targetId: string,
    questionText: string,
    nextMessages: ChatMessage[],
    hasApprovedProposal = false
  ) => {
    const nowLabel = new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
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
        timeLabel: `Сегодня, ${nowLabel}`,
        title: formattedTitle,
        badgeText: hasApprovedProposal ? 'Заявка подтверждена' : 'Есть ответ',
        badgeTone: hasApprovedProposal ? 'green' : 'blue',
        messages: nextMessages,
      };
      return [created, ...prev];
    });
  };

  const handleSend = async (rawText?: string, forceErrorDemo = false) => {
    const text = (rawText ?? inputValue).trim();
    if (!text) return;

    setShowRetryComparison(false);
    const idempotencyKey = 'idem-' + Date.now();
    const currentHistId = activeHistoryId ?? 'hist-session-' + Date.now();
    if (!activeHistoryId) {
      setActiveHistoryId(currentHistId);
    }

    const userMsg: ChatMessage = {
      id: 'u-' + Date.now(),
      sender: 'user',
      text,
      timestamp: new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
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
        id: 'err-' + Date.now(),
        sender: 'assistant',
        text: 'Не удалось получить ответ. Твой вопрос сохранён.',
        timestamp: new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
        idempotency_key: idempotencyKey,
        error: true,
      };
      const failedMessages = [...updatedWithUser, errMsg];
      setMessages(failedMessages);
      syncHistoryWithMessages(currentHistId, text, failedMessages, false);
    } finally {
      setIsLoading(false);
    }
  };

  const handleConfirmProposal = async (proposalId: string, resourceId: string) => {
    setProposalStage('sending');
    const res = await confirmAccessProposal(proposalId);
    setProposalStage('approved');
    onProposalConfirmed(resourceId, res.tracker_issue_key);
  };

  const handleRetryMessage = async (
    failedMsgId: string,
    questionText: string,
    idempotencyKey: string
  ) => {
    setIsLoading(true);
    const reply = await sendChatMessage(questionText, idempotencyKey, false);
    setMessages((prev) => prev.map((m) => (m.id === failedMsgId ? reply : m)));
    setIsLoading(false);
  };

  // Быстрые подсказки над строкой ввода: строго по убыванию длины (длинный -> помельче -> ещё помельче)
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

  const suggestions = [...rawSuggestions]
    .sort((first, second) => second.length - first.length)
    .slice(0, 3);

  const shellClassName = [
    'drawer-shell',
    isDrawerOpen
      ? activeDrawer === 'history'
        ? 'drawer-shell--open-history'
        : 'drawer-shell--open-roadmap'
      : '',
  ]
    .filter(Boolean)
    .join(' ');

  return (
    <div style={{ flex: 1, display: 'flex', overflow: 'hidden', position: 'relative', minHeight: 0 }}>
      {/* Плавная левая оболочка */}
      <div className={shellClassName}>
        {activeDrawer === 'history' ? (
          <aside
            className={`drawer-panel${!isDrawerOpen ? ' drawer-panel--closed' : ''}`}
            style={{
              width: '360px',
              minWidth: '360px',
            }}
          >
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                paddingBottom: '14px',
                borderBottom: '2px solid #141414',
                marginBottom: '14px',
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                <svg className="drawer-title-icon" viewBox="0 0 24 24" fill="none" aria-hidden="true">
                  <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="2.2" />
                  <path
                    d="M12 6.2V12l4.2 4.2"
                    stroke="currentColor"
                    strokeWidth="2.2"
                    strokeLinecap="butt"
                    strokeLinejoin="miter"
                  />
                </svg>
                <h2 style={{ fontSize: '18px', fontWeight: 800 }}>История запросов</h2>
              </div>
              <button
                className="ui-icon-circle"
                onClick={closeDrawer}
                style={{
                  width: '32px',
                  height: '32px',
                  borderRadius: '50%',
                  border: '2px solid #141414',
                  background: '#FFFFFF',
                  fontWeight: 800,
                }}
              >
                ✕
              </button>
            </div>

            <button
              className="ui-btn-dark"
              onClick={handleStartNewQuestion}
              style={{
                width: '100%',
                padding: '12px',
                borderRadius: '10px',
                border: '2px solid #141414',
                background: '#141414',
                color: '#FFFFFF',
                fontWeight: 700,
                fontSize: '14px',
                marginBottom: '16px',
              }}
            >
              + Новый вопрос
            </button>

            {(['СЕГОДНЯ', 'ВЧЕРА (ДЕНЬ 1)'] as const).map((group) => (
              <div key={group} style={{ marginBottom: '14px' }}>
                <div
                  style={{
                    fontSize: '11px',
                    fontWeight: 800,
                    color: '#5C5C5C',
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
                            border: '2px solid #141414',
                            borderRadius: '12px',
                            padding: '12px',
                            background: isSelected
                              ? '#EAF4FF'
                              : item.badgeTone === 'green'
                              ? '#F5FAFF'
                              : '#FFFFFF',
                            boxShadow: isSelected ? '0 2px 0 #141414' : undefined,
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
                            <span style={{ fontSize: '11px', color: '#5C5C5C' }}>
                              {item.timeLabel}
                            </span>
                            <span
                              style={{
                                fontSize: '11px',
                                fontWeight: 700,
                                padding: '2px 8px',
                                borderRadius: '999px',
                                border: '1.5px solid #141414',
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
              className="ui-btn"
              onClick={() =>
                setHistoryItems((prev) => [
                  ...prev,
                  {
                    id: 'hist-extra-' + Date.now(),
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
                border: '2px solid #141414',
                background: '#F3F4F6',
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
            onSelectStepQuestion={(q) => {
              handleSend(q);
            }}
          />
        )}
      </div>

      {/* Подложка для закрытия панели тапом вне неё на телефоне */}
      {isDrawerOpen && (
        <div
          className="mobile-drawer-backdrop"
          onClick={closeDrawer}
          aria-hidden="true"
        />
      )}

      {/* ОСНОВНАЯ ПРАВАЯ ОБЛАСТЬ ЧАТА */}
      <main className="chat-main">
        {/* Две увеличенные круглые иконки: Часы (История) и Дерево в круге (Roadmap) */}
        <div className="floating-nav-icons">
          <button
            className={`drawer-toggle-btn${
              isDrawerOpen && activeDrawer === 'history' ? ' drawer-toggle-btn--active' : ''
            }`}
            onClick={() => toggleDrawer('history')}
            title="История запросов"
            aria-label="История запросов"
          >
            <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
              <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="2.2" />
              <path
                d="M12 6.2V12l4.2 4.2"
                stroke="currentColor"
                strokeWidth="2.2"
                strokeLinecap="butt"
                strokeLinejoin="miter"
              />
            </svg>
          </button>

          <button
            className={`drawer-toggle-btn${
              isDrawerOpen && activeDrawer === 'roadmap' ? ' drawer-toggle-btn--active' : ''
            }`}
            onClick={() => toggleDrawer('roadmap')}
            title="Roadmap"
            aria-label="Roadmap"
          >
            <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
              <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="2.2" />
              <path
                d="M12 19.2C12 16.3 11.7 13.4 11.4 9.5M12 16.8C10.4 14.3 7.2 12.8 7.1 6.8M11.6 14.4C14.1 13.5 16.7 11.6 16.7 7.6"
                stroke="currentColor"
                strokeWidth="2.1"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </svg>
          </button>
        </div>

        {/* Прокручиваемая область сообщений (внутри выровнена ровно по ширине строки запроса) */}
        <div className="chat-scroll-area">
          <div className="chat-feed-column">
            {/* Начальный пустой экран */}
            {messages.length === 0 && !showRetryComparison && (
              <div
                style={{
                  flex: 1,
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  color: '#737373',
                  fontSize: '18px',
                  fontWeight: 600,
                  textAlign: 'center',
                }}
              >
                Задай вопрос ниже или выбери одну из быстрых подсказок
              </div>
            )}

            {/* Демонстрация Схемы 04 (Сравнение ошибки и повтора отправки) */}
            {showRetryComparison && (
              <div className="retry-comparison-grid">
                {/* Левая карточка: Сбой */}
                <div
                  style={{
                    border: '2px solid #141414',
                    borderRadius: '16px',
                    padding: '18px',
                    background: '#FFFFFF',
                  }}
                >
                  <div
                    style={{
                      color: '#E11D48',
                      fontWeight: 800,
                      fontSize: '14px',
                      paddingBottom: '10px',
                      borderBottom: '2px solid #141414',
                      marginBottom: '14px',
                    }}
                  >
                    1. Если не удалось получить ответ с первого раза
                  </div>
                  <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: '12px' }}>
                    <div
                      style={{
                        border: '2px solid #141414',
                        borderRadius: '999px',
                        padding: '8px 18px',
                        background: '#F3F4F6',
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
                        <div style={{ fontWeight: 800, fontSize: '13px' }}>
                          Не удалось получить ответ. Твой вопрос сохранён
                        </div>
                        <div style={{ fontSize: '12px', color: '#141414' }}>
                          Нажми кнопку ниже, чтобы отправить его ещё раз.
                        </div>
                      </div>
                    </div>
                    <button
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
                    border: '2px solid #141414',
                    borderRadius: '16px',
                    padding: '18px',
                    background: '#FFFFFF',
                  }}
                >
                  <div
                    style={{
                      display: 'inline-block',
                      background: '#C9FFBF',
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
                        border: '2px solid #141414',
                        borderRadius: '999px',
                        padding: '8px 18px',
                        background: '#F3F4F6',
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
                      background: '#FFFFFF',
                    }}
                  >
                    <div style={{ marginBottom: '10px' }}>
                      <span
                        style={{
                          display: 'inline-block',
                          padding: '5px 14px',
                          borderRadius: '999px',
                          border: '1.5px solid #141414',
                          background: '#04E061',
                          color: '#141414',
                          fontWeight: 800,
                          fontSize: '14px',
                        }}
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

            {/* Список живых сообщений чата */}
            {!showRetryComparison &&
              messages.map((msg) =>
                msg.sender === 'user' ? (
                  <div key={msg.id} style={{ display: 'flex', justifyContent: 'flex-end' }}>
                    <div className="user-msg-bubble">{msg.text}</div>
                  </div>
                ) : (
                  <div key={msg.id} style={{ display: 'flex', justifyContent: 'flex-start' }}>
                    <div
                      className="assistant-msg-card"
                      style={{
                        background: msg.error ? '#F7C1C5' : '#FFFFFF',
                      }}
                    >
                      <div style={{ marginBottom: '12px' }}>
                        <span
                          style={{
                            display: 'inline-block',
                            padding: '6px 16px',
                            borderRadius: '999px',
                            border: '2px solid #141414',
                            background: '#04E061',
                            color: '#141414',
                            fontWeight: 800,
                            fontSize: '15px',
                            letterSpacing: '0.01em',
                          }}
                        >
                          AI-buddy
                        </span>
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

                      {/* Кнопка повтора при сбое (Схема 04) */}
                      {msg.error && (
                        <button
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
                            fontWeight: 800,
                          }}
                        >
                          ↻ Повторить отправку (с тем же idempotency_key)
                        </button>
                      )}

                      {/* Синий блок полезных ссылок и правил (sources) */}
                      {msg.sources && msg.sources.length > 0 && (
                        <div
                          style={{
                            background: '#BADAFB',
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

                      {/* Карточка подтверждения доступа (Proposal Card) */}
                      {msg.proposal && (
                        <div
                          style={{
                            border: '2px solid #141414',
                            borderRadius: '14px',
                            overflow: 'hidden',
                            background: '#FFFFFF',
                          }}
                        >
                          <div style={{ padding: '16px' }}>
                            <div style={{ fontSize: '16px', fontWeight: 800, marginBottom: '4px' }}>
                              {msg.proposal.title}
                            </div>
                            <div style={{ fontSize: '13px', color: '#5C5C5C' }}>
                              Роль: <strong>{msg.proposal.role_label}</strong> • Раздел:{' '}
                              <strong>{msg.proposal.resource_label}</strong>
                            </div>
                          </div>

                          <div
                            style={{
                              padding: '10px 16px',
                              borderTop: '1.5px solid #141414',
                              borderBottom: '1.5px solid #141414',
                              display: 'flex',
                              alignItems: 'center',
                              justifyContent: 'space-between',
                              flexWrap: 'wrap',
                              gap: '8px',
                              background: '#F9FAFB',
                            }}
                          >
                            <span style={{ fontSize: '12px', color: '#5C5C5C', fontWeight: 600 }}>
                              Посмотреть вид карточки:
                            </span>
                            <div style={{ display: 'flex', gap: '6px' }}>
                              <button
                                className={proposalStage === 'draft' ? 'ui-btn-dark' : 'ui-btn'}
                                onClick={() => setProposalStage('draft')}
                                style={{
                                  padding: '5px 10px',
                                  borderRadius: '6px',
                                  border: '1.5px solid #141414',
                                  background: proposalStage === 'draft' ? '#141414' : '#FFFFFF',
                                  color: proposalStage === 'draft' ? '#FFFFFF' : '#141414',
                                  fontSize: '12px',
                                  fontWeight: 700,
                                }}
                              >
                                До нажатия
                              </button>
                              <button
                                className={proposalStage === 'sending' ? 'ui-btn-dark' : 'ui-btn'}
                                onClick={() => setProposalStage('sending')}
                                style={{
                                  padding: '5px 10px',
                                  borderRadius: '6px',
                                  border: '1.5px solid #141414',
                                  background: proposalStage === 'sending' ? '#141414' : '#FFFFFF',
                                  color: proposalStage === 'sending' ? '#FFFFFF' : '#141414',
                                  fontSize: '12px',
                                  fontWeight: 700,
                                }}
                              >
                                Отправка...
                              </button>
                              <button
                                className={proposalStage === 'approved' ? 'ui-btn-dark' : 'ui-btn'}
                                onClick={() => setProposalStage('approved')}
                                style={{
                                  padding: '5px 10px',
                                  borderRadius: '6px',
                                  border: '1.5px solid #141414',
                                  background: proposalStage === 'approved' ? '#141414' : '#FFFFFF',
                                  color: proposalStage === 'approved' ? '#FFFFFF' : '#141414',
                                  fontSize: '12px',
                                  fontWeight: 700,
                                }}
                              >
                                Готово (Доступ получен)
                              </button>
                            </div>
                          </div>

                          {proposalStage === 'draft' && (
                            <div style={{ padding: '14px 16px', background: '#FFFFFF' }}>
                              <button
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
              <div style={{ fontSize: '14px', color: '#5C5C5C', fontWeight: 600 }}>
                AI-buddy сверяется с базой знаний и планом адаптации...
              </div>
            )}

            {/* Якорь для автопрокрутки к последнему ответу */}
            <div ref={messagesEndRef} />
          </div>
        </div>

        {/* Нижняя зона: быстрые подсказки + автоматически растущая многострочная строка ввода */}
        <div className="chat-composer">
          <div className="suggestion-list">
            {suggestions.map((sug) => (
              <button
                key={sug}
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
              style={{
                width: '36px',
                height: '36px',
                flexShrink: 0,
                borderRadius: '50%',
                border: '2px solid #141414',
                background: '#FFFFFF',
                fontSize: '22px',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
              }}
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
