import React, { useState, useMemo, useCallback } from 'react';
import { OnboardingStep, AccessTask } from '../lib/api';

interface OnboardingPageProps {
  steps: OnboardingStep[];
  tasks: AccessTask[];
  isClosing: boolean;
  onAnimationEnd?: React.AnimationEventHandler<HTMLElement>;
  onClose: () => void;
  onToggleStep: (stepId: string) => void;
  onSelectStepQuestion: (question: string) => void;
}

export const OnboardingPage: React.FC<OnboardingPageProps> = ({
  steps,
  tasks,
  isClosing,
  onAnimationEnd,
  onClose,
  onToggleStep,
  onSelectStepQuestion,
}) => {
  const [selectedPeriod, setSelectedPeriod] = useState<string>('1 неделя');
  const [viewMode, setViewMode] = useState<'graph' | 'cards'>('graph');

  const periods = [...new Set(steps.map(step => step.period))];
  const activePeriod = periods.includes(selectedPeriod) ? selectedPeriod : periods[0];

  const getStepTask = useCallback(
    (resourceId?: string) =>
      resourceId ? tasks.find((t) => t.resource_id === resourceId) : undefined,
    [tasks]
  );

  const isStepCompleted = useCallback(
    (step: OnboardingStep) => {
      const linkedTask = getStepTask(step.resource_id);
      return step.completed || linkedTask?.status === 'approved';
    },
    [getStepTask]
  );

  const { completedCount, percent } = useMemo(() => {
    const done = steps.filter((s) => isStepCompleted(s)).length;
    return {
      completedCount: done,
      percent: steps.length > 0 ? Math.round((done / steps.length) * 100) : 0,
    };
  }, [steps, isStepCompleted]);

  const filteredSteps = useMemo(
    () => steps.filter((s) => s.period === activePeriod),
    [steps, activePeriod]
  );

  return (
    <aside
      className={`drawer-panel${isClosing ? ' drawer-panel--closed' : ''}`}
      onAnimationEnd={onAnimationEnd}
      style={{ width: '390px', minWidth: '390px' }}
    >
      <div className="drawer-header">
        <div className="drawer-header-title">
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
          <h2>Roadmap</h2>
        </div>

        <button
          type="button"
          className="ui-icon-circle"
          onClick={onClose}
          title="Закрыть Roadmap"
          aria-label="Закрыть Roadmap"
        >
          ✕
        </button>
      </div>

      {/* Блок прогресса */}
      <div
        style={{
          border: '2px solid var(--border-color)',
          borderRadius: '14px',
          padding: '12px 14px',
          marginBottom: '12px',
          background: 'var(--bg-surface)',
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
            Выполнено {completedCount} из {steps.length} шагов
          </span>
          <span>{percent}%</span>
        </div>
        <div
          style={{
            width: '100%',
            height: '12px',
            border: '1.5px solid var(--border-color)',
            borderRadius: '999px',
            overflow: 'hidden',
            background: 'var(--bg-muted)',
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

      {/* Переключатель вида: Этапы / По неделям */}
      <div style={{ display: 'flex', gap: '6px', marginBottom: '12px' }}>
        {(['graph', 'cards'] as const).map((mode) => {
          const isActive = viewMode === mode;
          return (
            <button
              key={mode}
              type="button"
              className={isActive ? 'ui-btn-dark' : 'ui-btn'}
              onClick={() => setViewMode(mode)}
              style={{
                flex: 1,
                padding: '7px 10px',
                borderRadius: '8px',
                border: '2px solid var(--border-color)',
                background: isActive ? 'var(--btn-primary-bg)' : 'var(--bg-surface)',
                color: isActive ? 'var(--btn-primary-text)' : 'var(--text-primary)',
                fontSize: '12px',
                fontWeight: 700,
              }}
            >
              {mode === 'graph' ? 'Этапы' : 'По неделям'}
            </button>
          );
        })}
      </div>

      {viewMode === 'cards' && (
        <div style={{ display: 'flex', gap: '6px', marginBottom: '14px' }}>
          {periods.map((period) => {
            const isSelected = activePeriod === period;
            return (
              <button
                key={period}
                type="button"
                className={isSelected ? 'ui-btn-dark' : 'ui-btn'}
                onClick={() => setSelectedPeriod(period)}
                style={{
                  flex: 1,
                  padding: '6px 4px',
                  borderRadius: '8px',
                  border: '1.5px solid var(--border-color)',
                  background: isSelected ? 'var(--btn-primary-bg)' : 'var(--bg-surface)',
                  color: isSelected ? 'var(--btn-primary-text)' : 'var(--text-primary)',
                  fontSize: '11px',
                  fontWeight: 700,
                }}
              >
                {period}
              </button>
            );
          })}
        </div>
      )}

      {/* Отображение Этапов */}
      {viewMode === 'graph' ? (
        <div style={{ display: 'flex', flexDirection: 'column', paddingLeft: '4px' }}>
          {steps.map((step, idx) => {
            const linkedTask = getStepTask(step.resource_id);
            const isDone = isStepCompleted(step);
            const prevStep = idx > 0 ? steps[idx - 1] : undefined;
            const nextStep = idx < steps.length - 1 ? steps[idx + 1] : undefined;
            const isPrevDone = prevStep ? isStepCompleted(prevStep) : false;
            const isNextDone = nextStep ? isStepCompleted(nextStep) : false;

            const isTopConnectorGreen = isPrevDone && isDone;
            const isBottomConnectorGreen = isDone && isNextDone;
            const isPending = !isDone && linkedTask?.status === 'pending_approval';
            const isCurrent = !isDone && step.current;

            const toggleLabel = isDone
              ? `Снять отметку с шага ${step.step_number}`
              : `Отметить шаг ${step.step_number} выполненным`;

            return (
              <div
                key={step.step_id}
                style={{ display: 'flex', alignItems: 'stretch', gap: '14px' }}
              >
                <div
                  style={{
                    display: 'flex',
                    flexDirection: 'column',
                    alignItems: 'center',
                    width: '34px',
                    flexShrink: 0,
                  }}
                >
                  <div
                    style={{
                      width: '3px',
                      flex: 1,
                      minHeight: '10px',
                      background:
                        idx === 0
                          ? 'transparent'
                          : isTopConnectorGreen
                          ? '#04E061'
                          : 'var(--line-color)',
                      transition: 'background-color 0.25s ease',
                    }}
                  />
                  <button
                    type="button"
                    className={`roadmap-check-circle${isDone ? ' roadmap-check-circle--done' : ''}`}
                    onClick={() => onToggleStep(step.step_id)}
                    title={toggleLabel}
                    aria-label={toggleLabel}
                  >
                    <span className="roadmap-check-icon">✓</span>
                  </button>
                  <div
                    style={{
                      width: '3px',
                      flex: 1,
                      minHeight: '10px',
                      background:
                        idx === steps.length - 1
                          ? 'transparent'
                          : isBottomConnectorGreen
                          ? '#04E061'
                          : 'var(--line-color)',
                      transition: 'background-color 0.25s ease',
                    }}
                  />
                </div>

                <div
                  style={{
                    flex: 1,
                    border: '2px solid var(--border-color)',
                    borderRadius: '12px',
                    padding: '10px 12px',
                    margin: '6px 0',
                    background: isDone
                      ? 'var(--bg-surface)'
                      : isCurrent
                      ? 'var(--bg-active-card)'
                      : 'var(--bg-subtle)',
                    transition: 'background-color 0.25s ease, border-color 0.25s ease',
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
                    <span style={{ fontSize: '11px', fontWeight: 700, color: 'var(--text-secondary)' }}>
                      ШАГ {step.step_number} • {step.period.toUpperCase()}
                    </span>
                    <span
                      className="status-badge"
                      style={{
                        background: isDone
                          ? '#C9FFBF'
                          : isPending
                          ? '#DFCDF3'
                          : isCurrent
                          ? '#BADAFB'
                          : '#F3F4F6',
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
                  <div style={{ fontSize: '12px', color: 'var(--text-secondary)', lineHeight: 1.35 }}>
                    {step.description}
                  </div>

                  {isCurrent && (
                    <button
                      type="button"
                      className="ui-btn"
                      onClick={() => onSelectStepQuestion('Как выполнить текущий шаг адаптации?')}
                      style={{
                        marginTop: '8px',
                        width: '100%',
                        padding: '6px 10px',
                        borderRadius: '8px',
                        border: '1.5px solid var(--border-color)',
                        background: 'var(--bg-surface)',
                        color: 'var(--text-primary)',
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
        <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
          {filteredSteps.map((step) => {
            const isDone = isStepCompleted(step);
            const isCurrent = !isDone && step.current;

            return (
              <div
                key={step.step_id}
                style={{
                  border: '2px solid var(--border-color)',
                  borderRadius: '14px',
                  padding: '14px',
                  background: isCurrent ? 'var(--bg-active-card)' : 'var(--bg-surface)',
                  transition: 'background-color 0.25s ease, border-color 0.25s ease',
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
                  <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                    <button
                      type="button"
                      className={`roadmap-check-circle${isDone ? ' roadmap-check-circle--done' : ''}`}
                      onClick={() => onToggleStep(step.step_id)}
                      style={{ width: '28px', height: '28px', fontSize: '14px' }}
                      title={
                        isDone
                          ? `Снять отметку с шага ${step.step_number}`
                          : `Отметить шаг ${step.step_number} выполненным`
                      }
                    >
                      <span className="roadmap-check-icon">✓</span>
                    </button>
                    <span style={{ fontSize: '12px', fontWeight: 700, color: 'var(--text-secondary)' }}>
                      ШАГ {step.step_number}
                    </span>
                  </div>
                  <span
                    className="status-badge"
                    style={{
                      padding: '3px 10px',
                      background: isDone ? '#C9FFBF' : isCurrent ? '#BADAFB' : '#F3F4F6',
                    }}
                  >
                    {isDone ? '✓ Выполнено' : isCurrent ? 'Текущий шаг' : 'Предстоит'}
                  </span>
                </div>
                <div style={{ fontSize: '14px', fontWeight: 700, marginBottom: '6px' }}>
                  {step.title}
                </div>
                <div style={{ fontSize: '12px', color: 'var(--text-secondary)', lineHeight: 1.4 }}>
                  {step.description}
                </div>
                {isCurrent && (
                  <button
                    type="button"
                    className="ui-btn"
                    onClick={() => onSelectStepQuestion('Как выполнить текущий шаг адаптации?')}
                    style={{
                      marginTop: '10px',
                      padding: '7px 12px',
                      borderRadius: '8px',
                      border: '1.5px solid var(--border-color)',
                      background: 'var(--bg-surface)',
                      color: 'var(--text-primary)',
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
