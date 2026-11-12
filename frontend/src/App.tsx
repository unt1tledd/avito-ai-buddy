import React, { useState, useEffect } from 'react';
import { ChatPage } from './pages/ChatPage';
import {
  INITIAL_ONBOARDING_STEPS,
  OnboardingStep,
  AccessTask,
  fetchOnboardingPlan,
  fetchAccessTasks,
} from './lib/api';

const normalizeInitialSteps = (rawSteps: OnboardingStep[]): OnboardingStep[] =>
  rawSteps.map((step, index) => ({
    ...step,
    completed: false,
    current: index === 0,
  }));

export const App: React.FC = () => {
  // Изначально все кружочки в Roadmap пустые, чтобы стажёр сам отмечал и снимал галочки
  const [steps, setSteps] = useState<OnboardingStep[]>(() =>
    normalizeInitialSteps(INITIAL_ONBOARDING_STEPS)
  );
  const [tasks, setTasks] = useState<AccessTask[]>([]);
  const [theme, setTheme] = useState<'light' | 'dark'>('light');

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme);
  }, [theme]);

  useEffect(() => {
    fetchOnboardingPlan().then((loaded) => setSteps(normalizeInitialSteps(loaded)));
    fetchAccessTasks().then(setTasks);
  }, []);

  const recomputeCurrentFlags = (list: OnboardingStep[]): OnboardingStep[] => {
    const firstUncompletedIdx = list.findIndex((s) => !s.completed);
    return list.map((s, idx) => ({
      ...s,
      current: firstUncompletedIdx !== -1 && idx === firstUncompletedIdx,
    }));
  };

  // Ручное переключение галочки стажёром (поставить / убрать)
  const handleToggleStep = (stepId: string) => {
    const target = steps.find((s) => s.step_id === stepId);
    const nextCompleted = target ? !target.completed : true;

    setSteps((prev) =>
      recomputeCurrentFlags(
        prev.map((s) =>
          s.step_id === stepId ? { ...s, completed: nextCompleted } : s
        )
      )
    );

    if (target?.resource_id && !nextCompleted) {
      setTasks((prev) => prev.filter((t) => t.resource_id !== target.resource_id));
    }
  };

  const handleProposalConfirmed = (resourceId: string, issueKey: string) => {
    const newTask: AccessTask = {
      task_id: 'task-' + issueKey,
      proposal_id: 'prop-' + issueKey,
      resource_id: resourceId,
      title: 'Выдать стажёру доступ к пространству команды (' + resourceId + ')',
      assignee: 'Тимлид команды',
      tracker_issue_key: issueKey,
      status: 'approved',
      created_at: 'Сегодня',
    };

    setTasks((prev) => {
      const exists = prev.some((t) => t.resource_id === resourceId);
      return exists ? prev : [newTask, ...prev];
    });

    setSteps((prev) =>
      recomputeCurrentFlags(
        prev.map((s) =>
          s.resource_id === resourceId ? { ...s, completed: true } : s
        )
      )
    );
  };

  const isDark = theme === 'dark';

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      {/* Верхняя шапка с логотипом AI BUDDY по центру и переключателем темы справа */}
      <header className="app-header">
        <div className="aibuddy-logo-pill">
          <span className="aibuddy-logo-dots" aria-hidden="true">
            <span className="aibuddy-logo-dot" style={{ background: '#04E061' }} />
            <span className="aibuddy-logo-dot" style={{ background: '#00AAFF' }} />
            <span className="aibuddy-logo-dot" style={{ background: '#965EEB' }} />
            <span className="aibuddy-logo-dot" style={{ background: '#FF4053' }} />
          </span>
          <span>AI BUDDY</span>
        </div>

        {/* Горизонтальный переключатель темы с шариком (Луна справа -> Солнце слева) */}
        <button
          type="button"
          className={`theme-toggle-btn${isDark ? ' theme-toggle-btn--dark' : ''}`}
          onClick={() => setTheme((prev) => (prev === 'light' ? 'dark' : 'light'))}
          title={isDark ? 'Переключить на светлую тему' : 'Переключить на тёмную тему'}
          aria-label="Переключить тему оформления"
        >
          <span className="theme-toggle-thumb">
            {isDark ? (
              /* Минималистичное солнце (на левом конце в тёмной теме) */
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <circle cx="12" cy="12" r="4.2" />
                <path d="M12 2.5v2.3M12 19.2v2.3M4.9 4.9l1.6 1.6M17.5 17.5l1.6 1.6M2.5 12h2.3M19.2 12h2.3M4.9 19.1l1.6-1.6M17.5 6.5l1.6-1.6" />
              </svg>
            ) : (
              /* Минималистичная луна (на правом конце в светлой теме) */
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path d="M20.2 14.8A8.5 8.5 0 0 1 9.2 3.8a8.5 8.5 0 1 0 11 11Z" />
              </svg>
            )}
          </span>
        </button>
      </header>

      {/* Основной экран чата */}
      <ChatPage
        steps={steps}
        tasks={tasks}
        onToggleStep={handleToggleStep}
        onProposalConfirmed={handleProposalConfirmed}
      />
    </div>
  );
};

export default App;
