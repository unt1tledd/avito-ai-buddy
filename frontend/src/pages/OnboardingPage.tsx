import React, { useState } from 'react';
import { OnboardingStep, AccessTask } from '../lib/api';

interface OnboardingPageProps {
  steps: OnboardingStep[];
  tasks: AccessTask[];
  isClosing: boolean;
  onAnimationEnd?: React.AnimationEventHandler<HTMLElement>;
  onClose: () => void;
  onSelectStepQuestion: (question: string) => void;
}

const PERIODS: Array<'1 неделя' | '2 неделя' | '1 месяц' | '3+ месяца'> = [
  '1 неделя',
  '2 неделя',
  '1 месяц',
  '3+ месяца',
];

export const OnboardingPage: React.FC<OnboardingPageProps> = ({
  steps,
  tasks,
  isClosing,
  onAnimationEnd,
  onClose,
  onSelectStepQuestion,
}) => {
  const [selectedPeriod, setSelectedPeriod] = useState<'1 неделя' | '2 неделя' | '1 месяц' | '3+ месяца'>('1 неделя');
  const [viewMode, setViewMode] = useState<'graph' | 'cards'>('graph');

  const completedCount = steps.filter((s) => s.completed).length;
  const percent = Math.round((completedCount / steps.length) * 100);

  // Связываем шаги и заявки по resource_id
  const getStepTask = (resourceId?: string) => {
    if (!resourceId) return undefined;
    return tasks.find((t) => t.resource_id === resourceId);
  };

  const isStepCompleted = (step: OnboardingStep) => {
    const linkedTask = getStepTask(step.resource_id);
    return step.completed || linkedTask?.status === 'approved';
  };

  const filteredSteps = steps.filter((s) => s.period === selectedPeriod);

  return (
    <aside
      className={`drawer-panel${isClosing ? ' drawer-panel--closed' : ''}`}
      onAnimationEnd={onAnimationEnd}
      style={{
        width: '390px',
        minWidth: '390px',
      }}
    >
      {/* Шапка панели Roadmap (без слова "адаптации") */}
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
            <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="2.1" />
            <path
              d="M12 19.2C12 16.3 11.7 13.4 11.4 9.5M12 16.8C10.4 14.3 7.2 12.8 7.1 6.8M11.6 14.4C14.1 13.5 16.7 11.6 16.7 7.6"
              stroke="currentColor"
              strokeWidth="2.1"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          </svg>
          <h2 style={{ fontSize: '18px', fontWeight: 800 }}>Roadmap</h2>
        </div>

        <button
          className="ui-icon-circle"
          onClick={onClose}
          title="Закрыть Roadmap"
          style={{
            width: '32px',
            height: '32px',
            borderRadius: '50%',
            border: '2px solid #141414',
            background: '#FFFFFF',
            fontWeight: 800,
            fontSize: '16px',
          }}
        >
          ✕
        </button>
      </div>

      {/* Блок прогресса */}
      <div
        style={{
          border: '2px solid #141414',
          borderRadius: '14px',
          padding: '12px 14px',
          marginBottom: '12px',
        }}
      >
        <div
          style={{
            display: 'flex',
            justifyContent: 'space-between',
            fontSize: '13px',
            fontWeight: 700,
            marginBottom: '8px',
          }}
        >
          <span>
            1 неделя • Выполнено {completedCount} из {steps.length} шагов
          </span>
          <span>{percent}%</span>
        </div>
        <div
          style={{
            width: '100%',
            height: '12px',
            border: '1.5px solid #141414',
            borderRadius: '999px',
            overflow: 'hidden',
            background: '#F3F4F6',
          }}
        >
          <div
            style={{
              width: `${percent}%`,
              height: '100%',
              background: '#04E061',
              transition: 'width 0.3s ease',
            }}
          />
        </div>
      </div>

      {/* Переключатель вида: Граф пути / Карточки по неделям */}
      <div
        style={{
          display: 'flex',
          gap: '6px',
          marginBottom: '12px',
        }}
      >
        <button
          className={viewMode === 'graph' ? 'ui-btn-dark' : 'ui-btn'}
          onClick={() => setViewMode('graph')}
          style={{
            flex: 1,
            padding: '7px 10px',
            borderRadius: '8px',
            border: '2px solid #141414',
            background: viewMode === 'graph' ? '#141414' : '#FFFFFF',
            color: viewMode === 'graph' ? '#FFFFFF' : '#141414',
            fontSize: '12px',
            fontWeight: 700,
          }}
        >
          Граф пути (вершины)
        </button>
        <button
          className={viewMode === 'cards' ? 'ui-btn-dark' : 'ui-btn'}
          onClick={() => setViewMode('cards')}
          style={{
            flex: 1,
            padding: '7px 10px',
            borderRadius: '8px',
            border: '2px solid #141414',
            background: viewMode === 'cards' ? '#141414' : '#FFFFFF',
            color: viewMode === 'cards' ? '#FFFFFF' : '#141414',
            fontSize: '12px',
            fontWeight: 700,
          }}
        >
          По неделям
        </button>
      </div>

      {viewMode === 'cards' && (
        <div style={{ display: 'flex', gap: '6px', marginBottom: '14px' }}>
          {PERIODS.map((period) => (
            <button
              key={period}
              className={selectedPeriod === period ? 'ui-btn-dark' : 'ui-btn'}
              onClick={() => setSelectedPeriod(period)}
              style={{
                flex: 1,
                padding: '6px 4px',
                borderRadius: '8px',
                border: '1.5px solid #141414',
                background: selectedPeriod === period ? '#141414' : '#FFFFFF',
                color: selectedPeriod === period ? '#FFFFFF' : '#141414',
                fontSize: '11px',
                fontWeight: 700,
              }}
            >
              {period}
            </button>
          ))}
        </div>
      )}

      {/* Отображение в виде графа (связанные вершины-шаги) */}
      {viewMode === 'graph' ? (
        <div style={{ display: 'flex', flexDirection: 'column', paddingLeft: '6px' }}>
          {steps.map((step, idx) => {
            const linkedTask = getStepTask(step.resource_id);
            const isDone = isStepCompleted(step);
            const nextStep = steps[idx + 1];
            const isNextDone = nextStep ? isStepCompleted(nextStep) : false;
            // Зелёная линия идёт только между уже выполненными галочками (за галочкой, а не вперёд)
            const isConnectorGreen = isDone && isNextDone;
            const isPending = !isDone && linkedTask?.status === 'pending_approval';
            const isCurrent = !isDone && step.current;

            return (
              <div key={step.step_id} style={{ display: 'flex', gap: '14px' }}>
                {/* Левая колонка графа: вершина (кружок) и ребро (линия к следующей вершине) */}
                <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
                  <div
                    style={{
                      width: '34px',
                      height: '34px',
                      borderRadius: '50%',
                      border: '2px solid #141414',
                      background: isDone
                        ? '#04E061'
                        : isCurrent
                        ? '#00AAFF'
                        : '#FFFFFF',
                      color: '#141414',
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      fontWeight: 800,
                      fontSize: '13px',
                      flexShrink: 0,
                    }}
                  >
                    {isDone ? '✓' : step.step_number}
                  </div>
                  {idx < steps.length - 1 && (
                    <div
                      style={{
                        width: '3px',
                        flex: 1,
                        minHeight: '28px',
                        background: isConnectorGreen ? '#04E061' : '#141414',
                      }}
                    />
                  )}
                </div>

                {/* Правая карточка вершины графа */}
                <div
                  style={{
                    flex: 1,
                    border: '2px solid #141414',
                    borderRadius: '12px',
                    padding: '10px 12px',
                    marginBottom: '12px',
                    background: isDone
                      ? '#FFFFFF'
                      : isCurrent
                      ? '#EAF4FF'
                      : '#F9FAFB',
                  }}
                >
                  <div
                    style={{
                      display: 'flex',
                      justifyContent: 'space-between',
                      alignItems: 'center',
                      marginBottom: '4px',
                      gap: '6px',
                    }}
                  >
                    <span style={{ fontSize: '11px', fontWeight: 700, color: '#5C5C5C' }}>
                      ШАГ {step.step_number} • {step.period.toUpperCase()}
                    </span>
                    <span
                      style={{
                        fontSize: '11px',
                        fontWeight: 700,
                        padding: '2px 8px',
                        borderRadius: '999px',
                        border: '1.5px solid #141414',
                        background: isDone
                          ? '#C9FFBF'
                          : isPending
                          ? '#DFCDF3'
                          : isCurrent
                          ? '#BADAFB'
                          : '#FFFFFF',
                      }}
                    >
                      {isDone
                        ? '✓ Выполнено'
                        : isPending
                        ? 'Ждёт тимлида'
                        : isCurrent
                        ? 'Текущий шаг'
                        : 'Следующий'}
                    </span>
                  </div>

                  <div style={{ fontSize: '13px', fontWeight: 700, marginBottom: '4px' }}>
                    {step.title}
                  </div>
                  <div style={{ fontSize: '12px', color: '#5C5C5C', lineHeight: 1.35 }}>
                    {step.description}
                  </div>

                  {isCurrent && (
                    <button
                      className="ui-btn"
                      onClick={() =>
                        onSelectStepQuestion('Как выполнить текущий шаг адаптации?')
                      }
                      style={{
                        marginTop: '8px',
                        width: '100%',
                        padding: '6px 10px',
                        borderRadius: '8px',
                        border: '1.5px solid #141414',
                        background: '#FFFFFF',
                        fontSize: '12px',
                        fontWeight: 700,
                        textAlign: 'left',
                      }}
                    >
                      Спросить у AI-buddy →
                    </button>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      ) : (
        /* Отображение карточками выбранной недели */
        <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
          {filteredSteps.map((step) => {
            const isDone = isStepCompleted(step);
            const isCurrent = !isDone && step.current;

            return (
              <div
                key={step.step_id}
                style={{
                  border: '2px solid #141414',
                  borderRadius: '14px',
                  padding: '14px',
                  background: isCurrent ? '#EAF4FF' : '#FFFFFF',
                }}
              >
                <div
                  style={{
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: 'center',
                    marginBottom: '8px',
                  }}
                >
                  <span style={{ fontSize: '12px', fontWeight: 700, color: '#5C5C5C' }}>
                    ШАГ {step.step_number}
                  </span>
                  <span
                    style={{
                      fontSize: '11px',
                      fontWeight: 700,
                      padding: '3px 10px',
                      borderRadius: '999px',
                      border: '1.5px solid #141414',
                      background: isDone ? '#C9FFBF' : isCurrent ? '#BADAFB' : '#F3F4F6',
                    }}
                  >
                    {isDone ? '✓ Выполнено' : isCurrent ? 'Текущий шаг' : 'Предстоит'}
                  </span>
                </div>
                <div style={{ fontSize: '14px', fontWeight: 700, marginBottom: '6px' }}>
                  {step.title}
                </div>
                <div style={{ fontSize: '12px', color: '#5C5C5C', lineHeight: 1.4 }}>
                  {step.description}
                </div>
                {isCurrent && (
                  <button
                    className="ui-btn"
                    onClick={() =>
                      onSelectStepQuestion('Как выполнить текущий шаг адаптации?')
                    }
                    style={{
                      marginTop: '10px',
                      padding: '7px 12px',
                      borderRadius: '8px',
                      border: '1.5px solid #141414',
                      background: '#FFFFFF',
                      fontSize: '12px',
                      fontWeight: 700,
                    }}
                  >
                    Спросить у AI-buddy →
                  </button>
                )}
              </div>
            );
          })}
        </div>
      )}
    </aside>
  );
};
