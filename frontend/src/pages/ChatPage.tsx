import React, { useState } from 'react';
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
  // Какая левая панель открыта: 'none' | 'history' | 'roadmap'
  const [leftDrawer, setLeftDrawer] = useState<'none' | 'history' | 'roadmap'>('none');
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [historyItems, setHistoryItems] = useState<HistoryItem[]>(INITIAL_HISTORY_ITEMS);
  const [inputValue, setInputValue] = useState('');
  const [isLoading, setIsLoading] = useState(false);

  // Состояние демонстрации карточки доступа: 'draft' | 'sending' | 'approved'
  const [proposalStage, setProposalStage] = useState<'draft' | 'sending' | 'approved'>('approved');

  // Режим демонстрации ошибки и повтора (Схема 04 — как на 9-м скрине из Miro)
  const [showRetryComparison, setShowRetryComparison] = useState(false);

  const toggleDrawer = (drawer: 'history' | 'roadmap') => {
    setLeftDrawer((prev) => (prev === drawer ? 'none' : drawer));
  };

  const handleSend = async (rawText?: string, forceErrorDemo = false) => {
    const text = (rawText ?? inputValue).trim();
    if (!text) return;

    setShowRetryComparison(false);
    const idempotencyKey = 'idem-' + Date.now();

    const userMsg: ChatMessage = {
      id: 'u-' + Date.now(),
      sender: 'user',
      text,
      timestamp: new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
      idempotency_key: idempotencyKey,
    };

    setMessages((prev) => [...prev, userMsg]);
    if (!rawText) setInputValue('');
    setIsLoading(true);

    try {
      const reply = await sendChatMessage(text, idempotencyKey, forceErrorDemo);
      if (reply.proposal) {
        setProposalStage('approved');
        onProposalConfirmed(
          reply.proposal.resource_id,
          reply.proposal.tracker_issue_key || '1042'
        );
      }
      setMessages((prev) => [...prev, reply]);
    } catch {
      const errMsg: ChatMessage = {
        id: 'err-' + Date.now(),
        sender: 'assistant',
        text: 'Не удалось получить ответ. Твой вопрос сохранён.',
        timestamp: new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
        idempotency_key: idempotencyKey,
        error: true,
      };
      setMessages((prev) => [...prev, errMsg]);
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

  const handleRetryMessage = async (failedMsgId: string, questionText: string, idempotencyKey: string) => {
    setIsLoading(true);
    const reply = await sendChatMessage(questionText, idempotencyKey, false);
    setMessages((prev) => prev.map((m) => (m.id === failedMsgId ? reply : m)));
    setIsLoading(false);
  };

  // Быстрые подсказки над полем ввода (меняются по контексту, как на макетах Miro)
  const lastAssistant = [...messages].reverse().find((m) => m.sender === 'assistant');
  const suggestions = lastAssistant?.proposal
    ? ['Перейти к следующему шагу адаптации', 'Открыть мой Roadmap']
    : lastAssistant?.text.includes('отгул')
    ? ['Создать черновик заявки на отгул', 'Как оформить учебный отпуск?']
    : [
        'Оформить отгул',
        'Как выполнить текущий шаг? ...',
        'Правила защиты персональных данных ...',
      ];

  return (
    <div style={{ flex: 1, display: 'flex', overflow: 'hidden', position: 'relative' }}>
      {/* Выдвижная панель 1: ИСТОРИЯ ЗАПРОСОВ (как на скрине 6 из Miro) */}
      {leftDrawer === 'history' && (
        <aside
          style={{
            width: '360px',
            minWidth: '360px',
            height: '100%',
            borderRight: '2px solid #141414',
            background: '#FFFFFF',
            display: 'flex',
            flexDirection: 'column',
            padding: '18px',
            overflowY: 'auto',
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
              <div
                style={{
                  width: '36px',
                  height: '36px',
                  borderRadius: '50%',
                  background: '#141414',
                  color: '#FFFFFF',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  fontWeight: 700,
                }}
              >
                🕒
              </div>
              <h2 style={{ fontSize: '18px', fontWeight: 800 }}>История запросов</h2>
            </div>
            <button
              onClick={() => setLeftDrawer('none')}
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
            onClick={() => {
              setMessages([]);
              setShowRetryComparison(false);
            }}
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
                  .map((item) => (
                    <div
                      key={item.id}
                      onClick={() => {
                        setMessages(item.messages);
                        setShowRetryComparison(false);
                      }}
                      style={{
                        border: '2px solid #141414',
                        borderRadius: '12px',
                        padding: '12px',
                        background: item.badgeTone === 'green' ? '#EAF4FF' : '#FFFFFF',
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
                  ))}
              </div>
            </div>
          ))}

          <button
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
      )}

      {/* Выдвижная панель 2: ROADMAP АДАПТАЦИИ (как на скрине 5 из Miro + Граф) */}
      {leftDrawer === 'roadmap' && (
        <OnboardingPage
          steps={steps}
          tasks={tasks}
          onClose={() => setLeftDrawer('none')}
          onSelectStepQuestion={(q) => {
            handleSend(q);
          }}
        />
      )}

      {/* ОСНОВНАЯ ПРАВАЯ ОБЛАСТЬ ЧАТА */}
      <main
        style={{
          flex: 1,
          display: 'flex',
          flexDirection: 'column',
          justifyContent: 'space-between',
          padding: '22px 32px',
          position: 'relative',
          overflowY: 'auto',
        }}
      >
        {/* Две круглые кнопки в левом верхнем углу рабочей зоны чата (Часы и Дерево Ψ) */}
        <div
          style={{
            position: 'absolute',
            top: '22px',
            left: '24px',
            display: 'flex',
            flexDirection: 'column',
            gap: '14px',
            zIndex: 10,
          }}
        >
          {leftDrawer !== 'history' && (
            <button
              className="circle-icon-btn"
              onClick={() => toggleDrawer('history')}
              title="История запросов"
            >
              <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2">
                <circle cx="12" cy="12" r="9" />
                <polyline points="12 7 12 12 16 14" />
              </svg>
            </button>
          )}

          {leftDrawer !== 'roadmap' && (
            <button
              className="circle-icon-btn"
              onClick={() => toggleDrawer('roadmap')}
              title="Roadmap адаптации"
              style={{ fontWeight: 700, fontSize: '20px' }}
            >
              Ψ
            </button>
          )}
        </div>

        {/* Область сообщений (со сдвигом слева от круглых иконок) */}
        <div
          style={{
            flex: 1,
            marginLeft: '72px',
            display: 'flex',
            flexDirection: 'column',
            gap: '18px',
            overflowY: 'auto',
            paddingRight: '8px',
            paddingBottom: '16px',
          }}
        >
          {/* Начальный пустой экран (как на скрине 4 из Miro) */}
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

          {/* Демонстрация Схемы 04 (Сравнение ошибки и повтора отправки — скрин 9 из Miro) */}
          {showRetryComparison && (
            <div
              style={{
                display: 'grid',
                gridTemplateColumns: '1fr 1fr',
                gap: '20px',
                marginTop: '8px',
              }}
            >
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
                  <div style={{ fontWeight: 800, fontSize: '14px', marginBottom: '6px' }}>
                    AI-buddy
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
                  <div
                    style={{
                      border: '2px solid #141414',
                      borderRadius: '999px',
                      padding: '10px 24px',
                      background: '#F3F4F6',
                      fontWeight: 700,
                      fontSize: '15px',
                    }}
                  >
                    {msg.text}
                  </div>
                </div>
              ) : (
                <div
                  key={msg.id}
                  style={{
                    border: '2px solid #141414',
                    borderRadius: '18px',
                    padding: '20px',
                    background: msg.error ? '#F7C1C5' : '#FFFFFF',
                    maxWidth: '860px',
                  }}
                >
                  <div style={{ marginBottom: '12px' }}>
                    <span
                      style={{
                        display: 'inline-block',
                        padding: '4px 14px',
                        borderRadius: '999px',
                        border: '1.5px solid #141414',
                        background: msg.proposal ? '#00AAFF' : '#04E061',
                        color: msg.proposal ? '#FFFFFF' : '#141414',
                        fontWeight: 800,
                        fontSize: '13px',
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

                  {/* Синий блок полезных ссылок и правил (sources — как на скрине 7 из Miro) */}
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

                  {/* Карточка подтверждения доступа (Proposal Card — как на скрине 8 из Miro) */}
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
              )
            )}

          {isLoading && (
            <div style={{ fontSize: '14px', color: '#5C5C5C', fontWeight: 600 }}>
              AI-buddy сверяется с базой знаний и планом адаптации...
            </div>
          )}
        </div>

        {/* Нижняя зона: быстрые подсказки справа + строка ввода вопроса */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: '14px', marginTop: '12px' }}>
          <div
            style={{
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'flex-end',
              gap: '8px',
            }}
          >
            {suggestions.map((sug) => (
              <button
                key={sug}
                className="suggestion-pill"
                onClick={() => {
                  if (sug === 'Открыть мой Roadmap') {
                    setLeftDrawer('roadmap');
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
              onClick={() => setShowRetryComparison((v) => !v)}
              title="Показать / скрыть демо повтора при сбое (Схема 04)"
              style={{
                width: '36px',
                height: '36px',
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
            <input
              type="text"
              placeholder="Введите вопрос"
              value={inputValue}
              onChange={(e) => setInputValue(e.target.value)}
            />
          </form>
        </div>
      </main>
    </div>
  );
};
